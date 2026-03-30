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

func TestBodyTemplateFromRequestMetadata(t *testing.T) {
	// TEMPLATE_BODY can reference .request.* metadata, not just body fields.
	// This lets operators rewrite the forwarded body based on incoming query params
	// without the sender needing to include those values in the JSON payload.
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

	// Dot notation works here because "event" has no hyphens. A hyphenated param
	// name would require index syntax, but that can't be expressed inside a JSON
	// string (unescaped quotes would break JSON).
	body := `{"action":"{{.request.params.event}}"}`
	req := httptest.NewRequest(http.MethodPost, "/topic?event=push", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotBody != `{"action":"push"}` {
		t.Errorf("body = %q, want rendered body", gotBody)
	}
}

func TestNullJSONBodyWithMethodTemplate(t *testing.T) {
	// A JSON null body is valid — Unmarshal produces nil which the proxy resets to an
	// empty map. Template data then contains only .request.* metadata, which should
	// still be accessible.
	var gotMethod string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateMethod = `{{if eq (index .request.params "action") "close"}}DELETE{{else}}POST{{end}}`
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic?action=close", strings.NewReader("null"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
}

func TestMethodTemplating(t *testing.T) {
	var gotMethod string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateMethod = "DELETE"
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
}

func TestConditionalMethodFromRequestParams(t *testing.T) {
	// Forgejo use case: ntfy DISMISS requires DELETE, other actions use POST.
	// TEMPLATE_METHOD lets the operator encode this routing without changing the
	// sender — the decision is made purely from the incoming query parameter.
	var gotMethod string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateMethod = `{{if eq (index .request.params "action") "close"}}DELETE{{else}}POST{{end}}`
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic?action=close", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
}

func TestInvalidRenderedMethodNonStrict(t *testing.T) {
	// A rendered method containing whitespace is not a valid HTTP token (RFC 7230 §3.1.1).
	// Non-strict mode should fall back to the original method rather than forward a
	// broken request.
	var gotMethod string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateMethod = "PUT PATCH" // space makes this an invalid method token
	cfg.TemplateStrict = false
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (non-strict)", rr.Code)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want original POST", gotMethod)
	}
}

func TestInvalidRenderedMethodStrict(t *testing.T) {
	// CRLF in a rendered method would allow header injection into the outgoing request.
	// Strict mode must reject it with 400 rather than forward the broken method.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateMethod = "GET\r\nX-Injected: evil"
	cfg.TemplateStrict = true
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestPathTemplating(t *testing.T) {
	var gotPath string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplatePath = "/items/{{.id}}"
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader(`{"id":"abc123"}`))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotPath != "/items/abc123" {
		t.Errorf("path = %q, want %q", gotPath, "/items/abc123")
	}
}

func TestInvalidRenderedPathNonStrict(t *testing.T) {
	// A rendered path without a leading / cannot be joined onto the target base URL
	// correctly. Non-strict mode should fall back to the original path.
	var gotPath string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplatePath = "items/abc123" // missing leading /
	cfg.TemplateStrict = false
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/original", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (non-strict)", rr.Code)
	}
	if gotPath != "/original" {
		t.Errorf("path = %q, want original /original", gotPath)
	}
}

func TestInvalidRenderedPathStrict(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplatePath = "items/abc123" // missing leading /
	cfg.TemplateStrict = true
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/original", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestRequestKeyCollisionNonStrict(t *testing.T) {
	// The proxy injects request metadata under the top-level "request" key.
	// If the body already has a "request" key it is overwritten — non-strict mode
	// warns but still forwards so the injected metadata wins.
	var gotMethod string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateMethod = `{{.request.method}}`
	cfg.TemplateStrict = false
	h := proxy.NewHandler(cfg, newLogger())

	// Body "request" key is overwritten; .request.method resolves to the real incoming method.
	body := `{"request":{"method":"SHOULD_BE_OVERWRITTEN"}}`
	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (non-strict)", rr.Code)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST (from injected request metadata)", gotMethod)
	}
}

func TestRequestKeyCollisionStrict(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateMethod = `{{.request.method}}`
	cfg.TemplateStrict = true
	h := proxy.NewHandler(cfg, newLogger())

	body := `{"request":{"method":"DELETE"}}`
	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestTargetHeaderInjected(t *testing.T) {
	// Use case: the target requires an auth token that the sender (e.g. beszel)
	// has no way to include. TARGET_HEADERS injects it on every forwarded request.
	var gotAuth string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TargetHeaders = map[string]string{"Authorization": "Bearer secret-token"}
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer secret-token")
	}
}

func TestTargetHeaderOverwritesIncoming(t *testing.T) {
	// TARGET_HEADERS always wins over whatever the incoming request carries.
	// This ensures the target's auth token cannot be spoofed by the sender.
	var gotAuth string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TargetHeaders = map[string]string{"Authorization": "Bearer target-token"}
	h := proxy.NewHandler(cfg, newLogger())

	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer incoming-token")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if gotAuth != "Bearer target-token" {
		t.Errorf("Authorization = %q, want target token %q", gotAuth, "Bearer target-token")
	}
}

func TestTargetHeaderTakesPrecedenceOverTemplateHeader(t *testing.T) {
	// TARGET_HEADERS is applied after TEMPLATE_HEADERS rendering, so it always
	// wins if both configure the same header. Operators should not set the same
	// header in both — TARGET_HEADERS will silently overwrite the rendered value.
	var gotHeader string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateHeaders = fieldset.Parse("X-Custom")
	cfg.TargetHeaders = map[string]string{"X-Custom": "from-target"}
	h := proxy.NewHandler(cfg, newLogger())

	body := `{"value":"from-template"}`
	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader(body))
	req.Header.Set("X-Custom", "{{.value}}")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if gotHeader != "from-target" {
		t.Errorf("X-Custom = %q, want %q (TARGET_HEADERS should win)", gotHeader, "from-target")
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
