package ochazuke

import (
	"maps"
	"strings"
)

// PickerScopesSetting turns picker scopes on (the default) or off: off opens
// every picker over its parameter's own choices, as before scopes.
const PickerScopesSetting = "picker.scopes"

// Scope is one named slice of the values a picker offers for a parameter, as
// data: the picker shows one scope at a time and S (picker.next-scope)
// steps through them, so a picker is never a wall around the values it
// opens on and the widest scope always holds every possible value. The
// search line's tag names the scope in effect and how many more the widest
// one holds ("project matomeru · 212 more in all"). A parameter or a
// declared picker lists its scopes narrowest first; the first is the one it
// opens on, and a picker with one scope shows no tag.
//
//	"scopes": [
//	  {"name": "project", "fixed": {"project": "{project}"}, "needs": "project"},
//	  {"name": "all"},
//	  {"name": "archived", "fixed": {"archived": true}}
//	]
type Scope struct {
	// Name is what the tag shows ("project", "all", "archived").
	Name string `json:"name"`
	// Source, when set, is the verb listing this scope's values, else the
	// parameter's own (its Source, or its noun's list).
	Source string `json:"source,omitempty"`
	// Fixed gives the source arguments for this scope (the parameter's
	// SourceFixed, then these). A string value "{name}" is the argument of
	// that name from the request being filled (its siblings), so a scope
	// can follow a value in context.
	Fixed map[string]any `json:"fixed,omitempty"`
	// Needs names a sibling argument that must hold a value for the scope to
	// exist: without it the scope is left out (a project scope with no
	// project in context).
	Needs string `json:"needs,omitempty"`
}

// scopesOf are the scopes a picker over p offers, given the arguments of the
// request being filled: p.Scopes with those whose Needs is unset left out,
// else, for a noun that archives its things, "all" then "archived" (the
// list asked to include them), else none. Fewer than two is no scoping.
func (r *Runtime) scopesOf(p Parameter, siblings Arguments) []Scope {
	if !r.settingOn(PickerScopesSetting) {
		return nil
	}
	var out []Scope
	for _, s := range p.Scopes {
		if s.Needs != "" && emptyProp(siblings[s.Needs]) {
			continue
		}
		out = append(out, s)
	}
	if len(p.Scopes) == 0 && p.Noun != "" && p.Noun != AnyNoun && p.Source == "" {
		if n := r.Noun(r.CanonicalNoun(p.Noun)); n != nil && n.roleProp(RoleArchive) != nil {
			out = []Scope{{Name: "all"}, {Name: "archived", Fixed: map[string]any{"archived": true}}}
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// within is p narrowed to scope s: its Source and fixed source arguments
// (with "{name}" values read from siblings), and no scopes of its own.
func (s Scope) within(p Parameter, siblings Arguments) Parameter {
	if s.Source != "" {
		p.Source = s.Source
	}
	fixed := maps.Clone(p.SourceFixed)
	if fixed == nil {
		fixed = map[string]any{}
	}
	for name, v := range s.Fixed {
		if text, ok := v.(string); ok && strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}") {
			v = siblings[text[1:len(text)-1]]
		}
		fixed[name] = v
	}
	p.SourceFixed, p.Scopes = fixed, nil
	return p
}
