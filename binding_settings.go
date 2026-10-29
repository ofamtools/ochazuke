package ochazuke

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// A binding row may name a setting where it would write a key: its keys,
// its hint and help, and any string argument (nested ones too, such as
// the request mode.enter stores) take "{name}" for the setting's current
// value. The terminal leader is such a row:
//
//	{"key": "{terminal-leader}", "action": "ochazuke.mode.enter",
//	 "args": {"mode": "leader_pending", "prefix": "{terminal-leader}", ...}}
//
// The row is resolved when it is installed (validateRow) and again
// whenever a setting it names is declared or changes, so a key that
// follows a setting is configured like any other row: in a defaults file,
// in Go, or in the user's bindings.json. "{{label}}" (a confirm template)
// is not a setting name, and a hint or argument naming no known setting
// is left as written; keys naming an unknown setting are refused.

// namedRow is the text of a row as written, for the fields that name a
// setting; nil fields name none.
type namedRow struct {
	keys, hint, help *string
	args             Arguments // the top-level arguments that name a setting, as written
	names            []string  // the settings named
}

// settingRefs are the setting names a text holds as "{name}", in order,
// each once. Double braces ("{{label}}") are not names.
func settingRefs(text string) []string {
	var out []string
	for i := 0; i < len(text); i++ {
		if text[i] != '{' || i > 0 && text[i-1] == '{' || i+1 < len(text) && text[i+1] == '{' {
			continue
		}
		end := strings.IndexByte(text[i+1:], '}')
		if end <= 0 {
			continue
		}
		name := text[i+1 : i+1+end]
		after := i + 2 + end
		if after < len(text) && text[after] == '}' || strings.ContainsAny(name, " \t{") {
			continue
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// valueRefs are the setting names a string argument holds, at any depth.
func valueRefs(v any) []string {
	var out []string
	add := func(names []string) {
		for _, n := range names {
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	switch t := v.(type) {
	case string:
		add(settingRefs(t))
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(t)) {
			add(valueRefs(t[k]))
		}
	case Arguments:
		add(valueRefs(map[string]any(t)))
	case []any:
		for _, x := range t {
			add(valueRefs(x))
		}
	case Request:
		add(valueRefs(map[string]any(t.Args)))
	case *Request:
		if t != nil {
			add(valueRefs(map[string]any(t.Args)))
		}
	}
	return out
}

// substituteText replaces each known "{name}" in text with the setting's
// value.
func (r *Runtime) substituteText(text string) string {
	for _, name := range settingRefs(text) {
		if v, ok := r.settingText(name); ok {
			text = strings.ReplaceAll(text, "{"+name+"}", v)
		}
	}
	return text
}

// substituteValue is substituteText through an argument value, copying
// every map, list and request it changes.
func (r *Runtime) substituteValue(v any) any {
	switch t := v.(type) {
	case string:
		return r.substituteText(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = r.substituteValue(x)
		}
		return out
	case Arguments:
		return Arguments(r.substituteValue(map[string]any(t)).(map[string]any))
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = r.substituteValue(x)
		}
		return out
	case Request:
		if t.Args != nil {
			t.Args = r.substituteValue(t.Args).(Arguments)
		}
		return t
	case *Request:
		if t == nil {
			return t
		}
		c := r.substituteValue(*t).(Request)
		return &c
	}
	return v
}

// knownSetting reports whether a name is a setting the runtime can read:
// declared, or one of the framework's.
func (r *Runtime) knownSetting(name string) bool {
	_, ok := r.settingValue(name)
	return ok
}

// resolveNamedRow resolves a row that names settings and remembers its
// text as written; a row naming none is returned as it is (a stored,
// already resolved row keeps what it remembers).
func (r *Runtime) resolveNamedRow(row Binding) (Binding, error) {
	n := namedRow{}
	note := func(names []string) bool {
		found := false
		for _, name := range names {
			if r.knownSetting(name) {
				found = true
				if !slices.Contains(n.names, name) {
					n.names = append(n.names, name)
				}
			}
		}
		return found
	}
	if refs := settingRefs(row.Keys); len(refs) > 0 {
		for _, name := range refs {
			if !r.knownSetting(name) {
				return row, fmt.Errorf("keys %q name the unknown setting %q", row.Keys, name)
			}
		}
		note(refs)
		keys := row.Keys
		n.keys = &keys
	}
	if note(settingRefs(row.Hint)) {
		hint := row.Hint
		n.hint = &hint
	}
	if note(settingRefs(row.Help)) {
		help := row.Help
		n.help = &help
	}
	for _, k := range slices.Sorted(maps.Keys(row.Request.Args)) {
		if note(valueRefs(row.Request.Args[k])) {
			if n.args == nil {
				n.args = Arguments{}
			}
			n.args[k] = row.Request.Args[k]
		}
	}
	if len(n.names) == 0 {
		return row, nil
	}
	row.named = &n
	return r.applyNamed(row), nil
}

// applyNamed is a named row with the settings' current values in the
// fields that name them.
func (r *Runtime) applyNamed(row Binding) Binding {
	n := row.named
	if n.keys != nil {
		row.Keys = strings.Join(strings.Fields(r.substituteText(*n.keys)), " ")
	}
	if n.hint != nil {
		row.Hint = r.substituteText(*n.hint)
	}
	if n.help != nil {
		row.Help = r.substituteText(*n.help)
	}
	if len(n.args) > 0 {
		args := maps.Clone(row.Request.Args)
		if args == nil {
			args = Arguments{}
		}
		for k, v := range n.args {
			args[k] = r.substituteValue(v)
		}
		row.Request.Args = args
	}
	return row
}

// rebindSettingRows resolves again every row naming setting name, on every
// noun and instance table: a row whose keys moved takes the place of the
// row of its table that had its new identity (that row gives way, as a
// file row replaces one), and a key sequence in progress is cancelled.
// DeclareSetting and every change of a setting's value call it.
func (r *Runtime) rebindSettingRows(name string) {
	changed := false
	for _, noun := range slices.Sorted(maps.Keys(r.Nouns)) {
		n := r.Nouns[noun]
		for _, mode := range slices.Sorted(maps.Keys(n.Bindings)) {
			rows, moved := r.renameRows(n.Bindings[mode], name, func(old Binding, keys string) {
				r.moveDefaultSource(noun, old, keys)
			})
			if moved {
				n.Bindings[mode], changed = rows, true
			}
		}
	}
	for _, inst := range r.instancesInOrder() {
		if rows, moved := r.renameRows(inst.Bindings, name, func(Binding, string) {}); moved {
			inst.Bindings, changed = rows, true
		}
	}
	if changed {
		r.CancelPendingBinding()
	}
}

// renameRows is one table with its rows naming setting name resolved
// again; moved reports a change. moveSource follows a row whose keys
// changed (bindings defaults' record).
func (r *Runtime) renameRows(rows []Binding, name string, moveSource func(old Binding, keys string)) ([]Binding, bool) {
	moved := map[int]bool{}
	for i, b := range rows {
		if b.named == nil || !slices.Contains(b.named.names, name) {
			continue
		}
		nb := r.applyNamed(b)
		if nb.Keys == b.Keys && nb.Hint == b.Hint && nb.Help == b.Help && reflect.DeepEqual(nb.Request.Args, b.Request.Args) {
			continue
		}
		if nb.Keys != b.Keys {
			moveSource(b, nb.Keys)
		}
		rows[i], moved[i] = nb, true
	}
	if len(moved) == 0 {
		return rows, false
	}
	taken := func(b Binding) bool {
		for i := range moved {
			if rows[i].same(b) {
				return true
			}
		}
		return false
	}
	out := make([]Binding, 0, len(rows))
	for i, b := range rows {
		if !moved[i] && taken(b) {
			continue
		}
		out = append(out, b)
	}
	return out, true
}
