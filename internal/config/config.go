// Package config loads and validates postencil runtime configuration
// from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/MihkelMK/postencil/internal/fieldset"
)

// Config holds all runtime configuration.
type Config struct {
	// TargetURL is the base URL all requests are forwarded to. Required.
	TargetURL string

	// ListenAddr is the address the proxy listens on. Default: :8080
	ListenAddr string

	// TemplateQueryParams controls which query params are rendered as Go templates.
	TemplateQueryParams fieldset.FieldSet

	// TemplateHeaders controls which headers are rendered as Go templates.
	TemplateHeaders fieldset.FieldSet

	// TemplateBody controls whether the request body is rendered as a Go template.
	TemplateBody bool

	// TemplateErrorPassthrough controls behaviour when template rendering fails.
	//   true (default): log a warning and forward the original unmodified value.
	//   false: return HTTP 400 to the caller.
	TemplateErrorPassthrough bool

	// LogLevel is the minimum log level emitted. Default: info.
	LogLevel slog.Level

	// CensorAuthTokens controls whether auth-related values are redacted in logs.
	CensorAuthTokens bool

	// CensoredHeaders is the list of header names to redact when CensorAuthTokens is true.
	CensoredHeaders []string

	// CensoredQueryParams is the list of query param names to redact when CensorAuthTokens is true.
	CensoredQueryParams []string
}

// Load reads all configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		TargetURL:                getEnv("TARGET_URL", ""),
		ListenAddr:               getEnv("LISTEN_ADDR", ":8080"),
		TemplateQueryParams:      fieldset.Parse(getEnv("TEMPLATE_QUERY_PARAMS", "false")),
		TemplateHeaders:          fieldset.Parse(getEnv("TEMPLATE_HEADERS", "false")),
		TemplateBody:             getEnvBool("TEMPLATE_BODY", false),
		TemplateErrorPassthrough: getEnvBool("TEMPLATE_ERROR_PASSTHROUGH", true),
		CensorAuthTokens:         getEnvBool("CENSOR_AUTH_TOKENS", true),
		CensoredHeaders:          parseList(getEnv("CENSORED_HEADERS", "Authorization")),
		CensoredQueryParams:      parseList(getEnv("CENSORED_QUERY_PARAMS", "auth,token")),
	}

	level, err := parseLogLevel(getEnv("LOG_LEVEL", "info"))
	if err != nil {
		return nil, fmt.Errorf("invalid LOG_LEVEL: %w", err)
	}
	cfg.LogLevel = level

	if cfg.TargetURL == "" {
		return nil, errors.New("TARGET_URL is required")
	}

	return cfg, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func parseList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown level %q (valid: debug, info, warn, error)", s)
	}
}
