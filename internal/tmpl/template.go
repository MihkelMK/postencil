// Package tmpl provides Go template rendering against a JSON data map.
// It uses text/template — the same engine ntfy uses for title/body templating —
// so any template that works in ntfy works identically here.
package tmpl

import (
	"bytes"
	"text/template"
)

// Render parses and executes tmpl as a Go text/template with data as its dot context.
// Returns an error if the template is invalid or if a key referenced in the template
// is missing from data.
func Render(tmpl string, data map[string]any) (string, error) {
	t, err := template.New("").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
