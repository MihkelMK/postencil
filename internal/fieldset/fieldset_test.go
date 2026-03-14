package fieldset_test

import (
	"testing"

	"github.com/MihkelMK/postencil/internal/fieldset"
)

func TestParse(t *testing.T) {
	tests := []struct {
		input      string
		enabled    bool
		matchTrue  []string
		matchFalse []string
	}{
		{
			input:      "",
			enabled:    false,
			matchFalse: []string{"sequence-id", "Authorization", "anything"},
		},
		{
			input:      "false",
			enabled:    false,
			matchFalse: []string{"sequence-id", "Authorization"},
		},
		{
			input:     "true",
			enabled:   true,
			matchTrue: []string{"sequence-id", "Authorization", "anything"},
		},
		{
			input:      "sequence-id",
			enabled:    true,
			matchTrue:  []string{"sequence-id"},
			matchFalse: []string{"x-id", "Authorization", "Topic"},
		},
		{
			input:      "sequence-id, Topic, Tags",
			enabled:    true,
			matchTrue:  []string{"sequence-id", "Topic", "Tags"},
			matchFalse: []string{"x-id", "Authorization", "title"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			fs := fieldset.Parse(tt.input)

			if fs.Enabled() != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", fs.Enabled(), tt.enabled)
			}
			for _, key := range tt.matchTrue {
				if !fs.Matches(key) {
					t.Errorf("Matches(%q) = false, want true", key)
				}
			}
			for _, key := range tt.matchFalse {
				if fs.Matches(key) {
					t.Errorf("Matches(%q) = true, want false", key)
				}
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"false", "false"},
		{"", "false"},
		{"true", "true"},
		{"sequence-id", "[sequence-id]"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			fs := fieldset.Parse(tt.input)
			if got := fs.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
