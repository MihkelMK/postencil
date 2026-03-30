package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/MihkelMK/postencil/internal/config"
	"github.com/MihkelMK/postencil/internal/proxy"
)

func main() {
	// Healthcheck mode: ping the local /healthz endpoint and exit 0/1.
	// Runs before config loading so TARGET_URL being unset does not block the check.
	// Used by the Dockerfile HEALTHCHECK instruction.
	if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
		addr := os.Getenv("LISTEN_ADDR")
		if addr == "" {
			addr = ":8080"
		}
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:"+port+"/healthz", nil) //nolint:gosec // G704: always localhost, operator-configured port
		if err != nil {
			cancel()
			os.Exit(1)
		}
		resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: see above
		cancel()
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		resp.Body.Close()
		os.Exit(0)
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	// Collect TARGET_HEADERS key names for the startup log. Values are not logged
	// as they typically contain secrets (e.g. Bearer tokens).
	targetHeaderKeys := make([]string, 0, len(cfg.TargetHeaders))
	for k := range cfg.TargetHeaders {
		targetHeaderKeys = append(targetHeaderKeys, k)
	}

	logger.Info("postencil starting",
		"listen", cfg.ListenAddr,
		"target", cfg.TargetURL,
		"template_query_params", cfg.TemplateQueryParams.String(),
		"template_headers", cfg.TemplateHeaders.String(),
		"template_body", cfg.TemplateBody,
		"template_method", cfg.TemplateMethod,
		"template_path", cfg.TemplatePath,
		"template_strict", cfg.TemplateStrict,
		"target_headers", targetHeaderKeys,
		"censor_auth_tokens", cfg.CensorAuthTokens,
		"log_level", cfg.LogLevel.String(),
	)

	proxyHandler := proxy.NewHandler(cfg, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/", proxyHandler)

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
