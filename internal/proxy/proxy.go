// Package proxy implements the core HTTP handler: it optionally renders Go
// templates in query params, headers, and/or the body, then forwards the
// request to the configured target URL and streams the response back.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/MihkelMK/postencil/internal/config"
	"github.com/MihkelMK/postencil/internal/fieldset"
	"github.com/MihkelMK/postencil/internal/tmpl"
)

// hopByHopHeaders must not be forwarded to the target per RFC 7230 §6.1.
var hopByHopHeaders = []string{
	"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"TE", "Trailers", "Transfer-Encoding", "Upgrade",
}

// Handler is the core HTTP handler.
type Handler struct {
	cfg    *config.Config
	logger *slog.Logger
	client *http.Client
}

// NewHandler constructs a Handler with the given config and logger.
func NewHandler(cfg *config.Config, logger *slog.Logger) *Handler {
	return &Handler{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// ── Read body ──────────────────────────────────────────────────────────
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to read request body", "error", err)
		http.Error(w, "failed to read request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	// ── Log incoming request ───────────────────────────────────────────────
	h.logger.InfoContext(ctx, "incoming request",
		"method", r.Method,
		"path", r.URL.Path,
		"remote_addr", r.RemoteAddr,
		"query", h.censorQuery(r.URL.Query()),
	)
	if h.logger.Enabled(ctx, slog.LevelDebug) {
		h.logger.DebugContext(ctx, "incoming headers",
			"headers", flattenHeaders(h.censorHeaders(r.Header)),
		)
	}

	// ── Parse JSON body if templating is enabled anywhere ──────────────────
	var data map[string]any
	needsParsing := h.cfg.TemplateQueryParams.Enabled() ||
		h.cfg.TemplateHeaders.Enabled() ||
		h.cfg.TemplateBody

	if needsParsing && len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &data); err != nil {
			h.logger.WarnContext(ctx, "failed to parse JSON body for templating — forwarding untouched",
				"error", err,
			)
			if h.cfg.TemplateStrict {
				http.Error(w, fmt.Sprintf("failed to parse JSON body: %v", err), http.StatusBadRequest)
				return
			}
			// data stays nil; all render calls below treat nil data as passthrough
		}
	}

	// ── Render query params ────────────────────────────────────────────────
	outQuery := url.Values{}
	for key, values := range r.URL.Query() {
		rendered, err := h.renderValues("query param", key, values, h.cfg.TemplateQueryParams, data)
		if err != nil {
			h.logger.WarnContext(ctx, "template render failed", "error", err)
			if h.cfg.TemplateStrict {
				http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusBadRequest)
				return
			}
		}
		outQuery[key] = rendered
	}

	// ── Render headers ─────────────────────────────────────────────────────
	outHeaders := r.Header.Clone()
	for key, values := range r.Header {
		rendered, err := h.renderValues("header", key, values, h.cfg.TemplateHeaders, data)
		if err != nil {
			h.logger.WarnContext(ctx, "template render failed", "error", err)
			if h.cfg.TemplateStrict {
				http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusBadRequest)
				return
			}
		}
		outHeaders[key] = rendered
	}
	for _, hop := range hopByHopHeaders {
		outHeaders.Del(hop)
	}

	// ── Render body ────────────────────────────────────────────────────────
	outBody := bodyBytes
	if h.cfg.TemplateBody && data != nil {
		rendered, err := tmpl.Render(string(bodyBytes), data)
		if err != nil {
			h.logger.WarnContext(ctx, "template render failed", "location", "body", "error", err)
			if h.cfg.TemplateStrict {
				http.Error(w, fmt.Sprintf("template error in body: %v", err), http.StatusBadRequest)
				return
			}
			// non-strict: keep original body
		} else {
			outBody = []byte(rendered)
		}
	}

	// ── Build target URL ───────────────────────────────────────────────────
	targetURL, err := url.Parse(h.cfg.TargetURL)
	if err != nil {
		h.logger.ErrorContext(ctx, "invalid TARGET_URL", "error", err)
		http.Error(w, "proxy misconfiguration", http.StatusInternalServerError)
		return
	}
	targetURL.Path = strings.TrimRight(targetURL.Path, "/") + r.URL.Path
	targetURL.RawQuery = outQuery.Encode()

	// ── Forward request ────────────────────────────────────────────────────
	outReq, err := http.NewRequestWithContext(ctx, r.Method, targetURL.String(), bytes.NewReader(outBody))
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to build outgoing request", "error", err)
		http.Error(w, "failed to build outgoing request", http.StatusInternalServerError)
		return
	}
	outReq.Header = outHeaders

	h.logger.DebugContext(ctx, "forwarding request",
		"url", targetURL.String(),
		"method", r.Method,
	)

	resp, err := h.client.Do(outReq) //nolint:gosec // G704: forwarding to TARGET_URL is the proxy's purpose; URL is operator-configured, not user-supplied
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to reach target",
			"error", err,
			"target", targetURL.String(),
		)
		http.Error(w, "failed to reach target", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	h.logger.InfoContext(ctx, "target response",
		"status", resp.StatusCode,
		"target", targetURL.String(),
	)
	if h.logger.Enabled(ctx, slog.LevelDebug) {
		h.logger.DebugContext(ctx, "response headers",
			"headers", flattenHeaders(h.censorHeaders(resp.Header)),
		)
	}

	// ── Stream response back to caller ─────────────────────────────────────
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		h.logger.WarnContext(ctx, "error streaming response body to caller", "error", err)
	}
}

// renderValues renders template values for all keys matched by fs.
// Returns the original values unchanged if the key is not matched or data is nil.
// On render failure, returns the original values and a non-nil error — the caller
// decides whether to log a warning and continue or abort the request.
func (h *Handler) renderValues(
	location, key string,
	values []string,
	fs fieldset.FieldSet,
	data map[string]any,
) ([]string, error) {
	if !fs.Matches(key) || data == nil {
		return values, nil
	}
	out := make([]string, len(values))
	for i, v := range values {
		rendered, err := tmpl.Render(v, data)
		if err != nil {
			return values, fmt.Errorf("%s %q: %w", location, key, err)
		}
		out[i] = rendered
	}
	return out, nil
}

// censorHeaders returns a copy of headers with configured names redacted.
func (h *Handler) censorHeaders(headers http.Header) http.Header {
	if !h.cfg.CensorAuthTokens {
		return headers
	}
	out := make(http.Header, len(headers))
	for k, values := range headers {
		if matchesAnyCI(k, h.cfg.CensoredHeaders) {
			out[k] = []string{"[REDACTED]"}
		} else {
			out[k] = values
		}
	}
	return out
}

// censorQuery returns a display-safe encoded query string.
func (h *Handler) censorQuery(q url.Values) string {
	if !h.cfg.CensorAuthTokens {
		return q.Encode()
	}
	out := url.Values{}
	for k, values := range q {
		if matchesAnyCI(k, h.cfg.CensoredQueryParams) {
			out[k] = []string{"[REDACTED]"}
		} else {
			out[k] = values
		}
	}
	return out.Encode()
}

// matchesAnyCI reports whether key case-insensitively matches any item in list.
func matchesAnyCI(key string, list []string) bool {
	for _, item := range list {
		if strings.EqualFold(key, item) {
			return true
		}
	}
	return false
}

// flattenHeaders converts http.Header to a flat string map for structured logging.
func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = strings.Join(v, ", ")
	}
	return out
}
