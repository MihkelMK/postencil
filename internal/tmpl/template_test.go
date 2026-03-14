package tmpl_test

import (
	"testing"

	"github.com/MihkelMK/postencil/internal/tmpl"
)

func TestRender(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		data    map[string]any
		want    string
		wantErr bool
	}{
		{
			name: "no template markers — passthrough",
			tmpl: "plain string",
			data: map[string]any{},
			want: "plain string",
		},
		{
			name: "simple key substitution",
			tmpl: "pr-{{.repo}}-{{.number}}",
			data: map[string]any{"repo": "myorg/myrepo", "number": 42},
			want: "pr-myorg/myrepo-42",
		},
		{
			name: "nested key",
			tmpl: "{{.repository.full_name}}",
			data: map[string]any{
				"repository": map[string]any{"full_name": "myorg/myrepo"},
			},
			want: "myorg/myrepo",
		},
		{
			name:    "missing key returns error",
			tmpl:    "{{.missing}}",
			data:    map[string]any{},
			wantErr: true,
		},
		{
			name:    "invalid template syntax returns error",
			tmpl:    "{{.unclosed",
			data:    map[string]any{},
			wantErr: true,
		},
		{
			name: "empty template",
			tmpl: "",
			data: map[string]any{},
			want: "",
		},
		{
			// replace is the primary documented use case: sanitize repository.full_name
			// for use in sequence-id, which cannot contain slashes.
			name: "sprig replace function",
			tmpl: `{{.repository.full_name | replace "/" "_"}}`,
			data: map[string]any{"repository": map[string]any{"full_name": "myorg/myrepo"}},
			want: "myorg_myrepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tmpl.Render(tt.tmpl, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Render() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Render() = %q, want %q", got, tt.want)
			}
		})
	}
}
