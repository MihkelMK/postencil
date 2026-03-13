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
		TargetURL:                targetURL,
		ListenAddr:               ":8080",
		TemplateQueryParams:      fieldset.Parse("false"),
		TemplateHeaders:          fieldset.Parse("false"),
		TemplateBody:             false,
		TemplateErrorPassthrough: true,
		CensorAuthTokens:         true,
		CensoredHeaders:          []string{"Authorization"},
		CensoredQueryParams:      []string{"auth", "token"},
		LogLevel:                 slog.LevelError, // silence logs during tests
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
		gotID = r.URL.Query().Get("X-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateQueryParams = fieldset.Parse("X-ID")

	h := proxy.NewHandler(cfg, newLogger())

	body := `{"repository":{"full_name":"myorg/myrepo"},"pull_request":{"number":42}}`
	req := httptest.NewRequest(http.MethodPost, "/topic?X-ID=pr-{{.repository.full_name}}-{{.pull_request.number}}", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotID != "pr-myorg/myrepo-42" {
		t.Errorf("X-ID = %q, want %q", gotID, "pr-myorg/myrepo-42")
	}
}

func TestTemplateFailPassthrough(t *testing.T) {
	var gotID string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = r.URL.Query().Get("X-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateQueryParams = fieldset.Parse("X-ID")
	cfg.TemplateErrorPassthrough = true

	h := proxy.NewHandler(cfg, newLogger())

	// Template references a key not present in the JSON — should passthrough original
	body := `{"foo":"bar"}`
	originalID := "pr-{{.missing_key}}"
	req := httptest.NewRequest(http.MethodPost, "/topic?X-ID="+originalID, strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (passthrough mode)", rr.Code)
	}
	if gotID != originalID {
		t.Errorf("X-ID = %q, want original %q", gotID, originalID)
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
	cfg.TemplateQueryParams = fieldset.Parse("X-ID")
	cfg.TemplateErrorPassthrough = true

	h := proxy.NewHandler(cfg, newLogger())

	// Non-JSON body — should still forward, just without template rendering
	req := httptest.NewRequest(http.MethodPost, "/topic?X-ID=static-id", strings.NewReader("not json"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotBody != "not json" {
		t.Errorf("body = %q, want original", gotBody)
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
		gotID = r.URL.Query().Get("X-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := newTestConfig(target.URL)
	cfg.TemplateQueryParams = fieldset.Parse("X-ID") // only X-ID, not topic

	h := proxy.NewHandler(cfg, newLogger())

	body := `{"repo":"myrepo"}`
	// topic has template syntax but should NOT be rendered
	req := httptest.NewRequest(http.MethodPost, "/topic?X-ID={{.repo}}&topic={{.repo}}", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if gotID != "myrepo" {
		t.Errorf("X-ID = %q, want %q", gotID, "myrepo")
	}
	if gotTopic != "{{.repo}}" {
		t.Errorf("topic = %q, want literal %q", gotTopic, "{{.repo}}")
	}
}
