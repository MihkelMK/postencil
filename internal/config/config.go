// Package config loads and validates postencil runtime configuration
// from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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

	// TemplateMethod is a Go template rendered against body data to override the forwarded HTTP method.
	// Empty string (default) means use the method from the incoming request.
	TemplateMethod string

	// TemplatePath is a Go template rendered against body data to override the forwarded request path.
	// Empty string (default) means use the path from the incoming request.
	TemplatePath string

	// TemplateStrict controls behaviour when template rendering fails.
	//   false (default): log a warning and forward the original unmodified value.
	//   true: return HTTP 400 to the caller.
	TemplateStrict bool

	// LogLevel is the minimum log level emitted. Default: info.
	LogLevel slog.Level

	// CensorAuthTokens controls whether auth-related values are redacted in logs.
	CensorAuthTokens bool

	// CensoredHeaders is the list of header names to redact when CensorAuthTokens is true.
	CensoredHeaders []string

	// CensoredQueryParams is the list of query param names to redact when CensorAuthTokens is true.
	CensoredQueryParams []string

	// TargetHeaders is a map of headers always set on the forwarded request,
	// regardless of what headers the incoming request carries.
	// Parsed from TARGET_HEADERS (comma-separated Key=Value pairs).
	TargetHeaders map[string]string
}

// Load reads all configuration from environment variables.
func Load() (*Config, error) {
	targetHeaders, err := parseHeaderMap(getEnv("TARGET_HEADERS", ""))
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		TargetURL:           getEnv("TARGET_URL", ""),
		ListenAddr:          getEnv("LISTEN_ADDR", ":8080"),
		TemplateQueryParams: fieldset.Parse(getEnv("TEMPLATE_QUERY_PARAMS", "false")),
		TemplateHeaders:     fieldset.Parse(getEnv("TEMPLATE_HEADERS", "false")),
		TemplateBody:        getEnvBool("TEMPLATE_BODY", false),
		TemplateMethod:      getEnv("TEMPLATE_METHOD", ""),
		TemplatePath:        getEnv("TEMPLATE_PATH", ""),
		TemplateStrict:      getEnvBool("TEMPLATE_STRICT", false),
		CensorAuthTokens:    getEnvBool("CENSOR_AUTH_TOKENS", true),
		CensoredHeaders:     parseList(getEnv("CENSORED_HEADERS", "Authorization")),
		CensoredQueryParams: parseList(getEnv("CENSORED_QUERY_PARAMS", "auth,token")),
		TargetHeaders:       targetHeaders,
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

// parseHeaderMap parses a comma-separated list of Key=Value pairs into a header
// map. The key is split at the first '=' only, so values may contain '='.
// Header names are canonicalised (e.g. "authorization" → "Authorization").
//
// Values starting with '@' are treated as file paths — the file contents are
// read and used as the value (whitespace trimmed). This supports Docker secrets:
//
//	TARGET_HEADERS="Authorization=Bearer @/run/secrets/my-token"
//
// A missing or unreadable file returns an error, failing startup rather than
// silently forwarding requests without the intended header.
//
// Limitation: values cannot contain commas — a comma always starts a new entry.
// This is not an issue for Bearer tokens (Base64 uses no commas), but be aware
// if injecting other header values.
func parseHeaderMap(s string) (map[string]string, error) {
	out := make(map[string]string)
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		k = http.CanonicalHeaderKey(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "@") {
			contents, err := os.ReadFile(strings.TrimPrefix(v, "@"))
			if err != nil {
				return nil, fmt.Errorf("TARGET_HEADERS: reading file for %q: %w", k, err)
			}
			v = strings.TrimSpace(string(contents))
		}
		out[k] = v
	}
	return out, nil
}

func parseList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
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
