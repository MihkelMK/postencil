package proxy_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MihkelMK/postencil/internal/config"
	"github.com/MihkelMK/postencil/internal/fieldset"
	"github.com/MihkelMK/postencil/internal/proxy"
)

// newTestConfig returns a minimal config pointing at the given target URL.
func newTestConfig(targetURL string) *config.Config {
	return &config.Config{
		TargetURL:           targetURL,
		ListenAddr:          ":8080",
		TemplateQueryParams: fieldset.Parse("false"),
		TemplateHeaders:     fieldset.Parse("false"),
		TemplateBody:        false,
		TemplateStrict:      false,
		CensorAuthTokens:    true,
		CensoredHeaders:     []string{"Authorization"},
		CensoredQueryParams: []string{"auth", "token"},
		LogLevel:            slog.LevelError, // silence logs during tests
	}
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestTransparentPassthrough(t *testing.T) {
	// Target server that records what it received
	var gotBody string
	var gotHeader string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotHeader = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader(`{"foo":"bar"}`))
	req.Header.Set("X-Custom", "hello")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotBody != `{"foo":"bar"}` {
		t.Errorf("body = %q, want original JSON", gotBody)
	}
	if gotHeader != "hello" {
		t.Errorf("X-Custom = %q, want %q", gotHeader, "hello")
	}
}

func TestQueryParamTemplating(t *testing.T) {
	var gotID string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = r.URL.Query().Get("sequence-id")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateQueryParams = fieldset.Parse("sequence-id")

	h := proxy.NewHandler(cfg, newLogger())

	body := `{"repository":{"full_name":"myorg/myrepo"},"pull_request":{"number":42}}`
	req := httptest.NewRequest(http.MethodPost, "/topic?sequence-id=pr-{{.repository.full_name}}-{{.pull_request.number}}", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotID != "pr-myorg/myrepo-42" {
		t.Errorf("sequence-id = %q, want %q", gotID, "pr-myorg/myrepo-42")
	}
}

func TestTemplateFailPassthrough(t *testing.T) {
	// Forwarding a raw "{{.missing_key}}" string to ntfy is recoverable; silently
	// dropping the webhook is not. Non-strict mode should always forward something.
	var gotID string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = r.URL.Query().Get("sequence-id")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateQueryParams = fieldset.Parse("sequence-id")
	cfg.TemplateStrict = false

	h := proxy.NewHandler(cfg, newLogger())

	// Template references a key not present in the JSON — should passthrough original
	body := `{"foo":"bar"}`
	originalID := "pr-{{.missing_key}}"
	req := httptest.NewRequest(http.MethodPost, "/topic?sequence-id="+originalID, strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (passthrough mode)", rr.Code)
	}
	if gotID != originalID {
		t.Errorf("sequence-id = %q, want original %q", gotID, originalID)
	}
}

func TestNonJSONBodyWithTemplatingEnabled(t *testing.T) {
	var gotBody string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateQueryParams = fieldset.Parse("sequence-id")
	cfg.TemplateStrict = false

	h := proxy.NewHandler(cfg, newLogger())

	// Non-JSON body — should still forward, just without template rendering
	req := httptest.NewRequest(http.MethodPost, "/topic?sequence-id=static-id", strings.NewReader("not json"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotBody != "not json" {
		t.Errorf("body = %q, want original", gotBody)
	}
}

func TestHeaderTemplating(t *testing.T) {
	var gotHeader string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateHeaders = fieldset.Parse("X-Custom")

	h := proxy.NewHandler(cfg, newLogger())

	body := `{"repo":"myrepo"}`
	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader(body))
	req.Header.Set("X-Custom", "{{.repo}}")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotHeader != "myrepo" {
		t.Errorf("X-Custom = %q, want %q", gotHeader, "myrepo")
	}
}

func TestBodyTemplating(t *testing.T) {
	var gotBody string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateBody = true

	h := proxy.NewHandler(cfg, newLogger())

	// .number is float64(42) after JSON unmarshal; template renders it as "42"
	body := `{"title":"PR {{.number}} opened","number":42}`
	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotBody != `{"title":"PR 42 opened","number":42}` {
		t.Errorf("body = %q, want rendered body", gotBody)
	}
}

func TestQueryParamStrictReturns400(t *testing.T) {
	// With TEMPLATE_STRICT=true, a render failure in a query param should return
	// 400 — the same behaviour as body failures in strict mode.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateQueryParams = fieldset.Parse("sequence-id")
	cfg.TemplateStrict = true

	h := proxy.NewHandler(cfg, newLogger())

	body := `{"foo":"bar"}`
	req := httptest.NewRequest(http.MethodPost, "/topic?sequence-id={{.missing_key}}", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestBodyTemplateFailReturns400(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateBody = true
	cfg.TemplateStrict = true

	h := proxy.NewHandler(cfg, newLogger())

	// .missing is not a key in the JSON body — render will fail
	body := `{"title":"{{.missing}}"}`
	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestNonJSONBodyPassthroughFalseReturns400(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateQueryParams = fieldset.Parse("sequence-id")
	cfg.TemplateStrict = true

	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic?sequence-id=foo", strings.NewReader("not json"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestResponsePassthrough(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Response-Header", "from-target")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("target response body"))
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rr.Code)
	}
	if rr.Body.String() != "target response body" {
		t.Errorf("body = %q, want %q", rr.Body.String(), "target response body")
	}
	if rr.Header().Get("X-Response-Header") != "from-target" {
		t.Errorf("X-Response-Header = %q, want %q", rr.Header().Get("X-Response-Header"), "from-target")
	}
}

func TestHopByHopHeadersStripped(t *testing.T) {
	// RFC 7230 §6.1: hop-by-hop headers are connection-scoped and must not be
	// forwarded to the next hop. Forwarding them can confuse the target server.
	var gotHeaders http.Header
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader("{}"))
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Set("X-Custom", "preserved")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if gotHeaders.Get("Connection") != "" {
		t.Errorf("Connection header should be stripped, got %q", gotHeaders.Get("Connection"))
	}
	if gotHeaders.Get("Transfer-Encoding") != "" {
		t.Errorf("Transfer-Encoding header should be stripped, got %q", gotHeaders.Get("Transfer-Encoding"))
	}
	if gotHeaders.Get("X-Custom") != "preserved" {
		t.Errorf("X-Custom = %q, want %q", gotHeaders.Get("X-Custom"), "preserved")
	}
}

func TestPathForwarding(t *testing.T) {
	var gotPath string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/some/nested/path", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if gotPath != "/some/nested/path" {
		t.Errorf("path = %q, want %q", gotPath, "/some/nested/path")
	}
}

func TestTargetErrorReturns502(t *testing.T) {
	// Point at a server that isn't there
	cfg := newTestConfig("http://127.0.0.1:1") // port 1 should be unreachable
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rr.Code)
	}
}

func TestUntouchedQueryParamNotTemplated(t *testing.T) {
	var gotTopic string
	var gotID string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTopic = r.URL.Query().Get("topic")
		gotID = r.URL.Query().Get("sequence-id")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateQueryParams = fieldset.Parse("sequence-id") // only sequence-id, not topic

	h := proxy.NewHandler(cfg, newLogger())

	body := `{"repo":"myrepo"}`
	// topic has template syntax but should NOT be rendered
	req := httptest.NewRequest(http.MethodPost, "/topic?sequence-id={{.repo}}&topic={{.repo}}", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if gotID != "myrepo" {
		t.Errorf("sequence-id = %q, want %q", gotID, "myrepo")
	}
	if gotTopic != "{{.repo}}" {
		t.Errorf("topic = %q, want literal %q", gotTopic, "{{.repo}}")
	}
}
