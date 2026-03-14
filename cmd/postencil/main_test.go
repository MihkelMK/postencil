package main_test

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

// newTestMux mirrors the mux setup in main.go.
func newTestMux(targetURL string) http.Handler {
	cfg := &config.Config{
		TargetURL:           targetURL,
		ListenAddr:          ":8080",
		TemplateQueryParams: fieldset.Parse("false"),
		TemplateHeaders:     fieldset.Parse("false"),
		CensorAuthTokens:    true,
		CensoredHeaders:     []string{"Authorization"},
		CensoredQueryParams: []string{"auth", "token"},
		LogLevel:            slog.LevelError,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := proxy.NewHandler(cfg, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/", handler)
	return mux
}

func TestHealthzReturns200(t *testing.T) {
	// /healthz must not be proxied — verified by also asserting the target was never called.
	var targetCalled bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	mux := newTestMux(target.URL)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", rr.Code)
	}
	if targetCalled {
		t.Error("/healthz was forwarded to target, should be handled locally")
	}
}

func TestNonHealthzPathStillProxied(t *testing.T) {
	// Sanity check: adding the /healthz route must not break normal proxy behaviour.
	var gotPath string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	mux := newTestMux(target.URL)
	req := httptest.NewRequest(http.MethodPost, "/topic", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotPath != "/topic" {
		t.Errorf("target path = %q, want /topic", gotPath)
	}
}
