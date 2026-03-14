package config_test

import (
	"log/slog"
	"testing"

	"github.com/MihkelMK/postencil/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("TARGET_URL", "http://example.com")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.TemplateQueryParams.Enabled() {
		t.Errorf("TemplateQueryParams should be disabled by default")
	}
	if cfg.TemplateHeaders.Enabled() {
		t.Errorf("TemplateHeaders should be disabled by default")
	}
	if cfg.TemplateBody {
		t.Errorf("TemplateBody should be false by default")
	}
	if cfg.TemplateStrict {
		t.Errorf("TemplateStrict should be false by default")
	}
	if !cfg.CensorAuthTokens {
		t.Errorf("CensorAuthTokens should be true by default")
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
}

func TestLoadMissingTargetURL(t *testing.T) {
	// TARGET_URL is the only required variable — omitting it must be an error.
	cfg, err := config.Load()
	if err == nil {
		t.Errorf("Load() with no TARGET_URL should return error, got cfg = %+v", cfg)
	}
}

func TestLoadTargetURL(t *testing.T) {
	t.Setenv("TARGET_URL", "https://ntfy.example.com")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.TargetURL != "https://ntfy.example.com" {
		t.Errorf("TargetURL = %q, want %q", cfg.TargetURL, "https://ntfy.example.com")
	}
}

func TestLoadLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		// Invalid value falls back to default (info)
		{"banana", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Setenv("TARGET_URL", "http://example.com")
			t.Setenv("LOG_LEVEL", tt.input)

			cfg, err := config.Load()
			if tt.input == "banana" {
				if err == nil {
					t.Errorf("Load() with invalid LOG_LEVEL should return error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.LogLevel != tt.want {
				t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, tt.want)
			}
		})
	}
}

func TestLoadCensoredLists(t *testing.T) {
	t.Setenv("TARGET_URL", "http://example.com")
	t.Setenv("CENSORED_HEADERS", "Authorization, X-Token")
	t.Setenv("CENSORED_QUERY_PARAMS", "auth, token, key")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.CensoredHeaders) != 2 {
		t.Errorf("CensoredHeaders = %v, want 2 entries", cfg.CensoredHeaders)
	}
	if len(cfg.CensoredQueryParams) != 3 {
		t.Errorf("CensoredQueryParams = %v, want 3 entries", cfg.CensoredQueryParams)
	}
}

func TestLoadTemplateStrict(t *testing.T) {
	t.Setenv("TARGET_URL", "http://example.com")
	t.Setenv("TEMPLATE_STRICT", "true")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.TemplateStrict {
		t.Errorf("TemplateStrict = false, want true")
	}
}

func TestLoadInvalidBoolFallsBackToDefault(t *testing.T) {
	// An unparseable bool value should silently fall back to the default
	// rather than failing — env vars set by other tools may contain unexpected values.
	t.Setenv("TARGET_URL", "http://example.com")
	t.Setenv("TEMPLATE_STRICT", "yes-please") // not a valid bool

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.TemplateStrict {
		t.Errorf("TemplateStrict = true, want false (default fallback)")
	}
}
