// Package fieldset provides a type representing whether template rendering is
// disabled, enabled for all keys, or enabled for a specific named set of keys.
package fieldset

import (
	"fmt"
	"strings"
)

type mode int

const (
	modeNone mode = iota
	modeAll
	modeSpecific
)

// FieldSet represents the three states a template rendering option can be in:
// disabled, enabled for all keys, or enabled for a specific named set.
type FieldSet struct {
	m     mode
	names map[string]bool
}

// Parse parses an env var value into a FieldSet.
//
//	"false" or "" → none (default, touch nothing)
//	"true"        → all keys
//	"A,B,C"       → only those exact key names
//
// Matching is case-sensitive and literal — no alias resolution.
func Parse(s string) FieldSet {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "false", "":
		return FieldSet{m: modeNone}
	case "true":
		return FieldSet{m: modeAll}
	default:
		names := make(map[string]bool)
		for _, part := range strings.Split(s, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				names[part] = true
			}
		}
		return FieldSet{m: modeSpecific, names: names}
	}
}

// Matches reports whether the given key should have its value templated.
func (f FieldSet) Matches(key string) bool {
	switch f.m {
	case modeNone:
		return false
	case modeAll:
		return true
	case modeSpecific:
		return f.names[key]
	}
	return false
}

// Enabled reports whether any templating is active for this FieldSet.
func (f FieldSet) Enabled() bool {
	return f.m != modeNone
}

// String implements fmt.Stringer for logging.
func (f FieldSet) String() string {
	switch f.m {
	case modeNone:
		return "false"
	case modeAll:
		return "true"
	case modeSpecific:
		keys := make([]string, 0, len(f.names))
		for k := range f.names {
			keys = append(keys, k)
		}
		return fmt.Sprintf("[%s]", strings.Join(keys, ", "))
	}
	return "unknown"
}
