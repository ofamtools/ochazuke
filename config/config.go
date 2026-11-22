// Package config loads and validates Ochazuke v2 configuration: JSONC
// bindings, partial themes, layered settings and static views.json
// templates. It reads no files; callers pass bytes.
package config

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/expr"
	"ofam.dev/ochazuke/ui"
)

// StripJSONC removes // and /* */ comments and trailing commas outside of
// string literals. Newlines inside comments are kept so error offsets line up.
func StripJSONC(src []byte) []byte {
	out := make([]byte, 0, len(src))
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '"':
			j := i + 1
			for j < len(src) && src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			end := min(j+1, len(src))
			out = append(out, src[i:end]...)
			i = end - 1
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i < len(src) && !(src[i] == '*' && i+1 < len(src) && src[i+1] == '/') {
				if src[i] == '\n' {
					out = append(out, '\n')
				}
				i++
			}
			i++
		case c == ',':
			j := i + 1
			for j < len(src) {
				if src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r' {
					j++
				} else if src[j] == '/' && j+1 < len(src) && src[j+1] == '/' {
					for j < len(src) && src[j] != '\n' {
						j++
					}
				} else if src[j] == '/' && j+1 < len(src) && src[j+1] == '*' {
					j += 2
					for j+1 < len(src) && !(src[j] == '*' && src[j+1] == '/') {
						j++
					}
					j += 2
				} else {
					break
				}
			}
			if j < len(src) && (src[j] == '}' || src[j] == ']') {
				continue // trailing comma: drop it; whitespace/comments follow normally
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

// LoadBindings parses a JSONC array of bindings, or an object with a
// "bindings" array.
func LoadBindings(data []byte) ([]core.Binding, error) {
	clean := StripJSONC(data)
	var list []core.Binding
	if err := json.Unmarshal(clean, &list); err != nil {
		var wrap struct {
			Bindings []core.Binding `json:"bindings"`
		}
		if err2 := json.Unmarshal(clean, &wrap); err2 != nil || wrap.Bindings == nil {
			return nil, fmt.Errorf("bindings: %w", err)
		}
		list = wrap.Bindings
	}
	return list, nil
}

// MergeBindings returns overrides followed by defaults. NewKeymap keeps
// declaration order for priority ties, so overrides (including Unbind rows)
// win over defaults.
func MergeBindings(defaults, overrides []core.Binding) []core.Binding {
	out := make([]core.Binding, 0, len(defaults)+len(overrides))
	out = append(out, overrides...)
	return append(out, defaults...)
}

// Diagnostic severities.
const (
	SevError   = "error"
	SevWarning = "warning"
)

// BindingDiagnostic is one problem found by CheckBindings.
type BindingDiagnostic struct {
	Index    int
	Severity string
	Message  string
}

// CheckBindings validates bindings against modes and the command registry
// (core.DefaultRegistry plus specs).
func CheckBindings(modes []core.Mode, bindings []core.Binding, specs ...core.CommandSpec) []BindingDiagnostic {
	reg := core.DefaultRegistry()
	reg.Register(specs...)
	known := map[string]bool{}
	for _, m := range modes {
		known[m.Name] = true
	}
	var out []BindingDiagnostic
	add := func(i int, sev, format string, a ...any) {
		out = append(out, BindingDiagnostic{Index: i, Severity: sev, Message: fmt.Sprintf(format, a...)})
	}
	seen := map[string]int{}
	for i, b := range bindings {
		if len(b.Keys) == 0 && b.Class == "" {
			add(i, SevError, "needs keys or class")
		}
		if len(b.Keys) > 0 && b.Class != "" {
			add(i, SevError, "keys and class are exclusive")
		}
		if b.When != "" {
			if _, err := expr.Parse(b.When); err != nil {
				add(i, SevError, "bad when %q: %v", b.When, err)
			}
		}
		if b.Mode != "" && !known[b.Mode] {
			add(i, SevError, "unknown mode %q", b.Mode)
		}
		if b.Command != "" {
			if _, ok := reg.Lookup(b.Command); !ok {
				add(i, SevError, "unknown command %q", b.Command)
			}
		} else if !b.Unbind && b.Layer == nil {
			add(i, SevError, "needs command, layer or unbind")
		}
		sig := strings.Join([]string{b.Scope, b.Mode, strings.Join(b.Keys, " "), b.Class, b.When, b.Phase}, "\x00")
		if len(b.Keys) > 0 || b.Class != "" {
			if first, dup := seen[sig]; dup {
				add(i, SevWarning, "shadowed by identical binding %d", first)
			} else {
				seen[sig] = i
			}
		}
	}
	return out
}

// LoadTheme merges a partial JSONC theme onto base and bumps Version. Roles
// merge by name and a supplied tone replaces the base tone of that name.
// density and borderStyle change only when present; an explicit "" selects
// the default (balanced, rounded).
func LoadTheme(data []byte, base ui.Theme) (ui.Theme, error) {
	var p ui.Theme
	if err := json.Unmarshal(StripJSONC(data), &p); err != nil {
		return base, fmt.Errorf("theme: %w", err)
	}
	out := base
	out.Roles = make(map[string]string, len(base.Roles)+len(p.Roles))
	for k, v := range base.Roles {
		out.Roles[k] = v
	}
	for k, v := range p.Roles {
		out.Roles[k] = v
	}
	out.Tones = make(map[string]ui.ToneStyle, len(base.Tones)+len(p.Tones))
	for k, v := range base.Tones {
		out.Tones[k] = v
	}
	for k, v := range p.Tones {
		out.Tones[k] = v
	}
	if p.Name != "" {
		out.Name = p.Name
	}
	var flags struct {
		Dark        *bool   `json:"dark"`
		Density     *string `json:"density"`
		BorderStyle *string `json:"borderStyle"`
	}
	if json.Unmarshal(StripJSONC(data), &flags) == nil {
		if flags.Dark != nil {
			out.Dark = *flags.Dark
		}
		if flags.Density != nil {
			out.Density = *flags.Density
		}
		if flags.BorderStyle != nil {
			out.BorderStyle = *flags.BorderStyle
		}
	}
	out.Version = base.Version + 1
	return out, nil
}

// Settings is a flat key/value store with defaults.
type Settings struct {
	defaults map[string]any
	values   map[string]any
}

// LoadSettings parses a JSONC object over defaults.
func LoadSettings(data []byte, defaults map[string]any) (*Settings, error) {
	s := &Settings{defaults: defaults, values: map[string]any{}}
	if len(strings.TrimSpace(string(StripJSONC(data)))) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(StripJSONC(data), &s.values); err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	return s, nil
}

// Raw returns the value for key (user value first, then default).
func (s *Settings) Raw(key string) (any, bool) {
	if v, ok := s.values[key]; ok {
		return v, true
	}
	v, ok := s.defaults[key]
	return v, ok
}

// Merged returns defaults overlaid with user values.
func (s *Settings) Merged() map[string]any {
	out := make(map[string]any, len(s.defaults)+len(s.values))
	for k, v := range s.defaults {
		out[k] = v
	}
	for k, v := range s.values {
		out[k] = v
	}
	return out
}

// Bool returns key as a bool, or def.
func (s *Settings) Bool(key string, def bool) bool {
	if v, ok := s.Raw(key); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

// Float returns key as a float64, or def.
func (s *Settings) Float(key string, def float64) float64 {
	v, _ := s.Raw(key)
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	}
	return def
}

// Int returns key as an int (numbers truncate), or def.
func (s *Settings) Int(key string, def int) int {
	v, _ := s.Raw(key)
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	}
	return def
}

// String returns key as a string, or def.
func (s *Settings) String(key, def string) string {
	if v, ok := s.Raw(key); ok {
		if str, ok := v.(string); ok {
			return str
		}
	}
	return def
}

// Strings returns key as a string slice, or def.
func (s *Settings) Strings(key string, def []string) []string {
	v, _ := s.Raw(key)
	switch t := v.(type) {
	case []string:
		return slices.Clone(t)
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			str, ok := e.(string)
			if !ok {
				return def
			}
			out = append(out, str)
		}
		return out
	}
	return def
}

// LoadView parses a Level 1 static view template and checks that every node
// has a Kind.
func LoadView(data []byte) (ui.ViewNode, error) {
	var n ui.ViewNode
	if err := json.Unmarshal(StripJSONC(data), &n); err != nil {
		return ui.ViewNode{}, fmt.Errorf("view: %w", err)
	}
	if err := checkKinds(n, "root"); err != nil {
		return ui.ViewNode{}, err
	}
	return n, nil
}

func checkKinds(n ui.ViewNode, path string) error {
	if n.Kind == "" {
		return fmt.Errorf("view: node %s has no kind", path)
	}
	for i, c := range n.Children {
		if err := checkKinds(c, fmt.Sprintf("%s/%d", path, i)); err != nil {
			return err
		}
	}
	return nil
}
