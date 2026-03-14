// Package tmpl provides Go template rendering against a JSON data map.
// It uses text/template — the same engine ntfy uses for title/body templating —
// so any template that works in ntfy works identically here.
package tmpl

import (
	"bytes"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

// safeFuncMap returns the Sprig text funcmap with environment-access functions removed.
// env/expandenv are excluded because TEMPLATE_BODY renders the request body as a template,
// meaning an attacker who controls the body could read arbitrary process environment
// variables (e.g. secrets injected via env vars) using {{ env "SECRET" }}.
func safeFuncMap() template.FuncMap {
	fm := sprig.TxtFuncMap()
	delete(fm, "env")
	delete(fm, "expandenv")
	return fm
}

// Render parses and executes tmpl as a Go text/template with data as its dot context.
// Returns an error if the template is invalid or if a key referenced in the template
// is missing from data.
//
// Most Sprig functions are available: https://masterminds.github.io/sprig/
// env and expandenv are excluded — see safeFuncMap.
func Render(tmpl string, data map[string]any) (string, error) {
	t, err := template.New("").
		Funcs(safeFuncMap()).
		Option("missingkey=error").
		Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
