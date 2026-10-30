package ochazuke

import (
	"fmt"
	"maps"
	"slices"
)

// rowConditions are the guards a row answers under: its own (When, the
// older form, the editor's implicit input guard included) and its
// action's availability (Available's verb guard). A row with none answers
// whenever its key reaches its table.
func (r *Runtime) rowConditions(b Binding) []string {
	var out []string
	if b.When != "" {
		out = append(out, b.When)
	}
	if !b.Unbind && !b.Request.Empty() {
		for _, g := range r.VerbAvailability(requestOperation(b.Request)) {
			if !slices.Contains(out, g) {
				out = append(out, g)
			}
		}
	}
	return out
}

// unreachableRow is a row that can never answer, and the earlier row on
// its keys that answers whenever it could.
type unreachableRow struct {
	Row, By Binding
}

// unreachableRows reads one table's rows (a noun's mode table, or an
// instance's rows, which span modes) in table order and reports every row
// an earlier row on the same mode, keys or pattern and pass (capture or
// not) always answers before: one whose conditions are a subset of the
// later row's, so it is available whenever the later one is (two rows on
// always-available actions: the second never answers). Conditions that
// differ cannot be compared statically and are not reported.
func (r *Runtime) unreachableRows(rows []Binding) []unreachableRow {
	var out []unreachableRow
	for j, later := range rows {
		if later.Unbind || later.Request.Empty() {
			continue
		}
		need := r.rowConditions(later)
		for _, earlier := range rows[:j] {
			if earlier.Unbind || earlier.Request.Empty() || !earlier.sameKeys(later) || earlier.Capture != later.Capture {
				continue
			}
			if !slices.ContainsFunc(r.rowConditions(earlier), func(g string) bool { return !slices.Contains(need, g) }) {
				out = append(out, unreachableRow{Row: later, By: earlier})
				break
			}
		}
	}
	return out
}

// tableLabel names a row's table the way bindings files do: "noun/mode",
// a bare noun, or "instance ID mode M".
func tableLabel(b Binding) string {
	g := BindingGroup{Noun: b.Noun, Instance: b.Instance, Mode: b.Mode}
	return g.label()
}

// describeUnreachable is the report line for one unreachable row.
func describeUnreachable(u unreachableRow) string {
	why := "always available"
	if c := u.By.When; c != "" {
		why = "when " + c
	}
	return fmt.Sprintf("%s %q: %s never answers: the row before it on the key, %s (%s), answers whenever it could", tableLabel(u.Row), u.Row.Keys+u.Row.Pattern, u.Row.Request.Key(), u.By.Request.Key(), why)
}

// storedTables are every stored table's rows, each row carrying its noun
// or instance and mode: nouns by name and mode, then instances by id.
func (r *Runtime) storedTables() [][]Binding {
	var out [][]Binding
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		noun := r.Nouns[name]
		for _, mode := range slices.Sorted(maps.Keys(noun.Bindings)) {
			var rows []Binding
			for _, row := range noun.Bindings[mode] {
				row.Noun, row.Instance, row.Mode = name, "", mode
				rows = append(rows, row)
			}
			out = append(out, rows)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(r.Instances)) {
		var rows []Binding
		for _, row := range r.Instances[id].Bindings {
			row.Noun, row.Instance = "", id
			rows = append(rows, row)
		}
		out = append(out, rows)
	}
	return out
}

// UnreachableBindings reports every stored row that can never answer
// (unreachableRows), table by table.
func (r *Runtime) UnreachableBindings() []string {
	var out []string
	for _, rows := range r.storedTables() {
		for _, u := range r.unreachableRows(rows) {
			out = append(out, describeUnreachable(u))
		}
	}
	return out
}

// BindingDebts lists the stored rows that carry a guard of their own
// (Binding.When; the editor's implicit input guard aside): the older way
// to say when a key answers, kept working while areas move the condition
// onto the action's availability (Available), so the row needs none.
func (r *Runtime) BindingDebts() []string {
	var out []string
	for _, rows := range r.storedTables() {
		for _, b := range rows {
			if b.identityGuard() == "" || b.Unbind {
				continue
			}
			out = append(out, fmt.Sprintf("%s %q: %s when %s is a guard row (a debt: make the condition the action's availability)", tableLabel(b), b.Keys+b.Pattern, b.Request.Key(), b.When))
		}
	}
	return out
}

// checkFileReachability places a file's rows into scratch tables the way
// a load does (one batch) and reports the rows that would never answer.
func (r *Runtime) checkFileReachability(rows []Binding) []string {
	tables := map[string][]Binding{}
	var order []string
	batch := rowBatch{}
	for _, row := range rows {
		id := row.Noun + "\x00" + row.Instance + "\x00" + row.Mode
		if _, ok := tables[id]; !ok {
			order = append(order, id)
		}
		tables[id] = bindRow(tables[id], row, batch)
	}
	var out []string
	for _, id := range order {
		for _, u := range r.unreachableRows(tables[id]) {
			out = append(out, describeUnreachable(u))
		}
	}
	return out
}
