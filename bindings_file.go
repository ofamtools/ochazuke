package ochazuke

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// A bindings file (bindings.json, bindings load, config export's
// bindings.reference.json) is JSONC in one of two formats:
//
// The grouped format (BindingsGrouped), written by FormatBindings: an
// object with "bindings", an array of groups, and optional "modes":
//
//	{
//	  "modes": {"register_pending": {"pending": true}},
//	  "bindings": [
//	    {"context": "md-doc/normal", "bindings": {
//	      "l": "ochazuke.focus.next",
//	      "enter": {"action": "ochazuke.focus.next", "hint": "edit"},
//	      "x": null
//	    }},
//	    {"instance": "work/agent", "mode": "normal", "bindings": {
//	      "ctrl+r": "pane.restart"
//	    }}
//	  ]
//	}
//
// A group's "context" is "noun/mode", split at the last "/"; a bare
// "noun" is the noun's any-mode table (mode ""). An instance's table is
// {"instance", "mode"} instead. "extends" on a noun group names the mode
// its table reads after its own (ModeBuilder.Extends). In "bindings" a key
// is a key or sequence ("d d", "ctrl+a v") or, when isPattern holds, a
// pattern ("digit", "any", "one-of:h j k l"); a value is the action
// string (noun.verb, split at the last "."), an object {action, args,
// hint, when, from, hidden, help, replay, capture, target, confirmed,
// unbind} for anything more, null to unbind the default, or an array of
// those, in table order, when one key has several rows in the table
// (guarded alternatives). A group's keys are its table's rows in table
// order: a key that comes back after other keys starts another group for
// the same table (keyRuns), and the groups of one table read back in file
// order, so the exported order is the stored order exactly. A row spelled
// by an alias is written as spelled. "modes" holds properties global to a mode:
// {"pending": true}, {"passthrough": true}.
//
// The rows format (BindingsRows), the older one, still read: a JSON array
// of Binding rows in any field order.
//
// The format is told by the top level: an object with "bindings" is
// grouped, an array is rows.

// Bindings file formats, as ParseBindings and bindings check report them.
const (
	BindingsGrouped = "grouped"
	BindingsRows    = "rows"
)

// BindingsHeader is the one comment line FormatBindings starts a file with.
const BindingsHeader = `// ochazuke bindings (JSONC): groups by "noun/mode" context; each key maps to an action, {action, args, hint, when, ...}, null (unbind) or an array of those`

// ModeSpec is one mode's entry in a bindings file's "modes".
type ModeSpec struct {
	Pending     bool `json:"pending,omitempty"`
	Passthrough bool `json:"passthrough,omitempty"`
}

// BindingGroup is one table of a grouped bindings file: a noun's or an
// instance's mode table and its rows in table order. Every row carries the
// group's Noun or Instance and Mode.
type BindingGroup struct {
	Noun     string
	Instance string
	Mode     string
	Extends  string
	Rows     []Binding
}

// Context is the group's "context" spelling, "noun/mode" or a bare noun;
// "" for an instance group.
func (g BindingGroup) Context() string {
	if g.Instance != "" {
		return ""
	}
	if g.Mode == "" {
		return g.Noun
	}
	return g.Noun + "/" + g.Mode
}

// label names the group in a problem: its context, or instance ID/mode.
func (g BindingGroup) label() string {
	if g.Instance != "" {
		return "instance " + g.Instance + " mode " + g.Mode
	}
	return g.Context()
}

// BindingsDoc is a parsed bindings file: its modes and its groups, in file
// order. A rows-format file is one group per row run of the same table.
type BindingsDoc struct {
	Modes  map[string]ModeSpec
	Groups []BindingGroup
}

// Rows are the doc's rows, group by group in file order.
func (d BindingsDoc) Rows() []Binding {
	var out []Binding
	for _, g := range d.Groups {
		out = append(out, g.Rows...)
	}
	return out
}

// ParseBindings reads a bindings file in either format (JSONC), with the
// format it found. A malformed file is an error with its line and column;
// a bad group or value is an error naming the group and key.
func ParseBindings(data []byte) (BindingsDoc, string, error) {
	doc, format, problems, err := parseBindingsFile(data)
	if err == nil && len(problems) > 0 {
		err = problems[0]
	}
	if err != nil {
		return BindingsDoc{}, format, err
	}
	return doc, format, nil
}

// parseBindingsFile reads either format. err is a file that cannot be
// read at all; problems are the groups and keys of a grouped file that
// were skipped (the rest is in doc).
func parseBindingsFile(data []byte) (doc BindingsDoc, format string, problems []error, err error) {
	std, err := StandardizeJSONC(data)
	if err != nil {
		return BindingsDoc{}, "", nil, err
	}
	trimmed := bytes.TrimSpace(std)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var rows []Binding
		if err := jsonPositionError(data, json.Unmarshal(std, &rows)); err != nil {
			return BindingsDoc{}, BindingsRows, nil, err
		}
		return BindingsDoc{Groups: GroupBindings(rows)}, BindingsRows, nil, nil
	}
	var top struct {
		Modes    map[string]ModeSpec `json:"modes"`
		Bindings *[]json.RawMessage  `json:"bindings"`
	}
	dec := json.NewDecoder(bytes.NewReader(std))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&top); err != nil {
		return BindingsDoc{}, "", nil, jsonPositionError(data, err)
	}
	if top.Bindings == nil {
		return BindingsDoc{}, "", nil, errors.New(`not a bindings file: want {"bindings": [groups]} or an array of rows`)
	}
	doc = BindingsDoc{Modes: top.Modes}
	for i, raw := range *top.Bindings {
		g, bad, err := parseBindingGroup(raw)
		where := fmt.Sprintf("group %d", i+1)
		if g.label() != "" {
			where = g.label()
		}
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w (group skipped)", where, err))
			continue
		}
		for _, e := range bad {
			problems = append(problems, fmt.Errorf("%s %w (skipped)", where, e))
		}
		doc.Groups = append(doc.Groups, g)
	}
	return doc, BindingsGrouped, problems, nil
}

// parseBindingGroup reads one group; err is a group that cannot be read,
// bad the keys skipped ("KEY: why").
func parseBindingGroup(raw json.RawMessage) (g BindingGroup, bad []error, err error) {
	var head struct {
		Context  *string         `json:"context"`
		Instance string          `json:"instance"`
		Mode     string          `json:"mode"`
		Extends  string          `json:"extends"`
		Bindings json.RawMessage `json:"bindings"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&head); err != nil {
		return g, nil, err
	}
	switch {
	case head.Context != nil && head.Instance != "":
		return g, nil, errors.New("a group takes context or instance, not both")
	case head.Context != nil:
		if head.Mode != "" {
			return g, nil, errors.New(`a context group names its mode in "context" ("noun/mode"), not "mode"`)
		}
		g.Noun, g.Mode = *head.Context, ""
		if i := strings.LastIndex(*head.Context, "/"); i >= 0 {
			g.Noun, g.Mode = (*head.Context)[:i], (*head.Context)[i+1:]
		}
		if g.Noun == "" {
			return g, nil, fmt.Errorf("context %q names no noun", *head.Context)
		}
	case head.Instance != "":
		g.Instance, g.Mode = head.Instance, head.Mode
		if head.Extends != "" {
			return g, nil, errors.New("extends applies to a noun's table, not an instance's")
		}
	default:
		return g, nil, errors.New(`a group needs "context" ("noun/mode") or "instance"`)
	}
	g.Extends = head.Extends
	members, err := orderedMembers(head.Bindings)
	if err != nil {
		return g, nil, fmt.Errorf("bindings: %w", err)
	}
	for _, m := range members {
		rows, err := parseBindingValue(m.raw, true)
		if err != nil {
			bad = append(bad, fmt.Errorf("%s: %w", m.key, err))
			continue
		}
		for _, row := range rows {
			row.Noun, row.Instance, row.Mode = g.Noun, g.Instance, g.Mode
			if isPattern(m.key) {
				row.Pattern = m.key
			} else {
				row.Keys = m.key
			}
			g.Rows = append(g.Rows, row)
		}
	}
	return g, bad, nil
}

// bindingValue is a binding value's object form.
type bindingValue struct {
	Action    *string           `json:"action"`
	Args      Arguments         `json:"args,omitempty"`
	Hint      string            `json:"hint,omitempty"`
	When      string            `json:"when,omitempty"`
	From      map[string]string `json:"from,omitempty"`
	Hidden    bool              `json:"hidden,omitempty"`
	Help      string            `json:"help,omitempty"`
	Replay    bool              `json:"replay,omitempty"`
	Capture   bool              `json:"capture,omitempty"`
	Target    string            `json:"target,omitempty"`
	Confirmed bool              `json:"confirmed,omitempty"`
	Unbind    bool              `json:"unbind,omitempty"`
}

// parseBindingValue reads one key's value: an action string, an object,
// null, or (when list) an array of those.
func parseBindingValue(raw json.RawMessage, list bool) ([]Binding, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("no value")
	}
	switch raw[0] {
	case 'n':
		return []Binding{{Unbind: true}}, nil
	case '"':
		var action string
		if err := json.Unmarshal(raw, &action); err != nil {
			return nil, err
		}
		q, err := actionRequest(action)
		if err != nil {
			return nil, err
		}
		return []Binding{{Request: q}}, nil
	case '[':
		if !list {
			return nil, errors.New("an array inside an array")
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		var out []Binding
		for i, item := range items {
			rows, err := parseBindingValue(item, false)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out = append(out, rows...)
		}
		return out, nil
	case '{':
		var v bindingValue
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		row := Binding{Hint: v.Hint, When: v.When, From: v.From, Hidden: v.Hidden, Help: v.Help, Replay: v.Replay, Capture: v.Capture, Unbind: v.Unbind}
		switch {
		case v.Action != nil && *v.Action != "":
			if v.Unbind {
				return nil, errors.New("an unbind takes no action")
			}
			q, err := actionRequest(*v.Action)
			if err != nil {
				return nil, err
			}
			q.Args, q.Target, q.Confirmed = v.Args, v.Target, v.Confirmed
			row.Request = q
		case v.Unbind:
		default:
			return nil, errors.New(`no "action" (null, or "unbind": true, unbinds)`)
		}
		return []Binding{row}, nil
	}
	return nil, fmt.Errorf("want an action string, an object, null or an array, got %s", raw)
}

// actionRequest is the request an action string names: noun.verb, split
// at the last ".".
func actionRequest(action string) (Request, error) {
	i := strings.LastIndex(action, ".")
	if i <= 0 || i == len(action)-1 {
		return Request{}, fmt.Errorf("action %q is not noun.verb", action)
	}
	return Request{Noun: action[:i], Verb: action[i+1:]}, nil
}

type member struct {
	key string
	raw json.RawMessage
}

// orderedMembers are an object's members in file order (a repeated key
// is an error: one key's rows go in an array).
func orderedMembers(raw json.RawMessage) ([]member, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("want an object of key: value")
	}
	var out []member
	seen := map[string]bool{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, fmt.Errorf("key %q twice: put its rows in one array", key)
		}
		seen[key] = true
		out = append(out, member{key, v})
	}
	return out, nil
}

// GroupBindings groups rows into tables: nouns by name, then each noun's
// modes by name ("" first), then instances by ID and mode, each group's
// rows in their given order.
func GroupBindings(rows []Binding) []BindingGroup {
	type id struct{ instance, noun, mode string }
	groups := map[id]*BindingGroup{}
	for _, row := range rows {
		k := id{row.Instance, row.Noun, row.Mode}
		if row.Instance != "" {
			k.noun = ""
		}
		g := groups[k]
		if g == nil {
			g = &BindingGroup{Noun: k.noun, Instance: k.instance, Mode: k.mode}
			groups[k] = g
		}
		g.Rows = append(g.Rows, row)
	}
	keys := slices.SortedFunc(maps.Keys(groups), func(a, b id) int {
		if (a.instance == "") != (b.instance == "") {
			if a.instance == "" {
				return -1
			}
			return 1
		}
		return strings.Compare(a.instance+"\x00"+a.noun+"\x00"+a.mode, b.instance+"\x00"+b.noun+"\x00"+b.mode)
	})
	out := make([]BindingGroup, 0, len(keys))
	for _, k := range keys {
		out = append(out, *groups[k])
	}
	return out
}

// BindingsDoc is every stored table as a bindings file: the pending and
// passthrough modes, every noun's mode tables (with their extends) and
// every instance's, in GroupBindings order, rows in table order.
func (r *Runtime) BindingsDoc() BindingsDoc {
	doc := BindingsDoc{Modes: map[string]ModeSpec{}}
	for name := range r.pendingModes {
		doc.Modes[name] = ModeSpec{Pending: true}
	}
	for name := range r.passthroughModes {
		m := doc.Modes[name]
		m.Passthrough = true
		doc.Modes[name] = m
	}
	var rows []Binding
	extends := map[[2]string]string{}
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		noun := r.Nouns[name]
		for _, mode := range slices.Sorted(maps.Keys(noun.Bindings)) {
			for _, row := range noun.Bindings[mode] {
				row.Noun, row.Instance, row.Mode = name, "", mode
				row.Aliases = nil
				rows = append(rows, row)
			}
		}
		for mode, parent := range noun.Extends {
			extends[[2]string{name, mode}] = parent
		}
	}
	for _, id := range slices.Sorted(maps.Keys(r.Instances)) {
		for _, row := range r.Instances[id].Bindings {
			row.Noun, row.Instance = "", id
			row.Aliases = nil
			rows = append(rows, row)
		}
	}
	doc.Groups = GroupBindings(rows)
	for i, g := range doc.Groups {
		if g.Instance == "" {
			doc.Groups[i].Extends = extends[[2]string{g.Noun, g.Mode}]
		}
	}
	return doc
}

// FormatBindings writes doc in the grouped format: the BindingsHeader
// line, then "modes" (names sorted, one per line) when there are any, then
// "bindings" with one group per context, its keys one per line in table
// order (a key with several rows is an array at its first row's place). A
// value is the action string when the row has nothing else, else an
// object with its fields in the order action, args, hint, when, from,
// hidden, help, replay, capture, target, confirmed, unbind; an unguarded
// unbind is null.
func FormatBindings(doc BindingsDoc) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(BindingsHeader)
	b.WriteString("\n{\n")
	if len(doc.Modes) > 0 {
		b.WriteString("  \"modes\": {\n")
		names := slices.Sorted(maps.Keys(doc.Modes))
		for i, name := range names {
			key, _ := marshalPlain(name)
			value, err := marshalPlain(doc.Modes[name])
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&b, "    %s: %s", key, spaceJSON(value))
			if i < len(names)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString("  },\n")
	}
	b.WriteString("  \"bindings\": [")
	written := 0
	for _, g := range doc.Groups {
		for _, run := range keyRuns(g.Rows) {
			if written > 0 {
				b.WriteByte(',')
			}
			written++
			b.WriteString("\n    ")
			var head orderedObject
			if g.Instance != "" {
				head.add("instance", g.Instance, true)
				head.add("mode", g.Mode, g.Mode != "")
			} else {
				head.add("context", g.Context(), true)
			}
			head.add("extends", g.Extends, g.Extends != "")
			h, err := head.bytes()
			if err != nil {
				return nil, err
			}
			b.Write(h[:len(h)-1]) // the object without its "}"
			b.WriteString(", \"bindings\": {")
			lines, err := formatGroupKeys(run)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", g.label(), err)
			}
			for j, line := range lines {
				if j > 0 {
					b.WriteByte(',')
				}
				b.WriteString("\n      ")
				b.Write(line)
			}
			if len(lines) > 0 {
				b.WriteString("\n    ")
			}
			b.WriteString("}}")
		}
	}
	if written > 0 {
		b.WriteString("\n  ")
	}
	b.WriteString("]\n}\n")
	return b.Bytes(), nil
}

// keyRuns splits a table's rows into the groups FormatBindings writes for
// it, so reading them back gives the rows in exactly their table order: a
// key's adjacent rows share one array, and a key that comes back after
// other keys starts a new group for the same table (JSON keys are unique
// in one object). An empty table is one empty group.
func keyRuns(rows []Binding) [][]Binding {
	runs := [][]Binding{nil}
	seen := map[string]bool{}
	last := ""
	for i, row := range rows {
		key := row.Keys + row.Pattern
		if i > 0 && key != last && seen[key] {
			runs = append(runs, nil)
			seen = map[string]bool{}
		}
		runs[len(runs)-1] = append(runs[len(runs)-1], row)
		seen[key], last = true, key
	}
	return runs
}

// formatGroupKeys is one line per key of a run (keyRuns): "key": value,
// a key's rows (adjacent) one array.
func formatGroupKeys(rows []Binding) ([][]byte, error) {
	var order []string
	byKey := map[string][]Binding{}
	for _, row := range rows {
		key := row.Keys + row.Pattern
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = append(byKey[key], row)
	}
	lines := make([][]byte, 0, len(order))
	for _, key := range order {
		name, _ := marshalPlain(key)
		var value []byte
		group := byKey[key]
		if len(group) == 1 {
			v, err := formatBindingValue(group[0], true)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", key, err)
			}
			value = v
		} else {
			parts := make([][]byte, len(group))
			for i, row := range group {
				v, err := formatBindingValue(row, false)
				if err != nil {
					return nil, fmt.Errorf("key %q: %w", key, err)
				}
				parts[i] = v
			}
			value = append(append([]byte{'['}, bytes.Join(parts, []byte(", "))...), ']')
		}
		lines = append(lines, append(append(name, ':', ' '), value...))
	}
	return lines, nil
}

// formatBindingValue is one row's value: a plain action string or null
// when short is set and the row has nothing else, else the object.
func formatBindingValue(row Binding, short bool) ([]byte, error) {
	q := row.Request
	// A row spelled by an alias (Via) is written as it was spelled, so it
	// loads back to the same action with the same Via.
	if q.Via != "" {
		if spelled, err := actionRequest(q.Via); err == nil {
			q.Noun, q.Verb = spelled.Noun, spelled.Verb
		}
	}
	// A row with no request is an unbind, as validateRow reads it.
	row.Unbind = row.Unbind || q.Verb == ""
	plain := row.Hint == "" && row.When == "" && len(row.From) == 0 && !row.Hidden && row.Help == "" &&
		!row.Replay && !row.Capture && len(q.Args) == 0 && q.Target == "" && !q.Confirmed
	if row.Unbind {
		if short && plain {
			return []byte("null"), nil
		}
	} else if short && plain {
		return marshalPlain(q.Key())
	}
	var o orderedObject
	if !row.Unbind {
		o.add("action", q.Key(), true)
		o.add("args", q.Args, len(q.Args) > 0)
	}
	o.add("hint", row.Hint, row.Hint != "")
	o.add("when", row.When, row.When != "")
	o.add("from", row.From, len(row.From) > 0)
	o.add("hidden", row.Hidden, row.Hidden)
	o.add("help", row.Help, row.Help != "")
	o.add("replay", row.Replay, row.Replay)
	o.add("capture", row.Capture, row.Capture)
	if !row.Unbind {
		o.add("target", q.Target, q.Target != "")
		o.add("confirmed", q.Confirmed, q.Confirmed)
	}
	o.add("unbind", true, row.Unbind)
	return o.bytes()
}
