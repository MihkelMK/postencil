package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/MihkelMK/postencil/internal/config"
	"github.com/MihkelMK/postencil/internal/proxy"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	logger.Info("postencil starting",
		"listen", cfg.ListenAddr,
		"target", cfg.TargetURL,
		"template_query_params", cfg.TemplateQueryParams.String(),
		"template_headers", cfg.TemplateHeaders.String(),
		"template_body", cfg.TemplateBody,
		"template_error_passthrough", cfg.TemplateErrorPassthrough,
		"censor_auth_tokens", cfg.CensorAuthTokens,
		"log_level", cfg.LogLevel.String(),
	)

	handler := proxy.NewHandler(cfg, logger)

	if err := http.ListenAndServe(cfg.ListenAddr, handler); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
