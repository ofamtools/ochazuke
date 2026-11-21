package bindings

import (
	"reflect"
	"slices"
	"unicode"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/table"
)

// HelpView renders the live help table for a host's ? action. Pass the query
// from before opening help so the caller's mode, frames, facts, modal, layer,
// and pending prefix participate in resolution. Hidden entries stay hidden.
func (m *Model) HelpView(id string, q core.KeyQuery) ui.ViewNode {
	var rows []table.Row
	seen := map[*core.Binding]bool{}
	var samples []string
	for i, b := range m.current {
		if b.Hidden || b.Unbind {
			continue
		}
		var hit *core.Binding
		if b.Class == "" {
			hit = winner(m.km, q, b.Keys)
		} else if len(q.Input.Prefix) == 0 {
			if samples == nil {
				samples = classSamples(m.current)
			}
			for _, key := range samples {
				if !core.ClassMatches(b.Class, key) {
					continue
				}
				found := winner(m.km, q, []string{key})
				if reflect.DeepEqual(found, &b) {
					hit = found
					break
				}
			}
		}
		if hit != nil && !seen[hit] && reflect.DeepEqual(hit, &b) {
			seen[hit] = true
			rows = append(rows, m.rows[i])
		}
	}
	return table.New(columns(), rows).View(id)
}

// Exact first strokes may mask a class. For the remaining keys, resolution
// depends only on class membership. Include the finite digit and paste sets,
// and one unoccupied representative of each open-ended class intersection.
func classSamples(bindings []core.Binding) []string {
	used := map[string]bool{}
	var keys []string
	for _, b := range bindings {
		if len(b.Keys) > 0 && !used[b.Keys[0]] {
			keys = append(keys, b.Keys[0])
			used[b.Keys[0]] = true
		}
	}
	keys = append(keys, "paste", "space")
	for _, r := range "0123456789" {
		keys = append(keys, string(r))
	}
	for _, predicate := range []func(rune) bool{unicode.IsLetter, func(r rune) bool { return unicode.IsPrint(r) && !unicode.IsLetter(r) && (r < '0' || r > '9') }} {
		for r := rune('!'); r <= unicode.MaxRune; r++ {
			if predicate(r) && !used[string(r)] {
				keys = append(keys, string(r))
				break
			}
		}
	}
	other := "f1"
	for slices.Contains(keys, other) {
		other += "-other"
	}
	return append(keys, other)
}
