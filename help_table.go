package ochazuke

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// HelpTableUnboundSetting puts the noun's unbound verbs in the help's table
// view, as rows under an "unbound" heading below the keys (default on, as
// the other views list them; off leaves the table to the bound keys). Which
// verbs count is help.unbound-scope, and help.unbound-verbs off lists none.
const HelpTableUnboundSetting = "help.table-unbound"

// helpTableUnboundSection heads the table's unbound rows. It is also the
// section sort keeps them in: a sort orders each run alone.
const helpTableUnboundSection = "unbound"

// TableUnboundVerbs are the operations of place's noun that no row binds in
// any of modes (the table's columns; a verb bound in one mode is not
// unbound): PlaceUnboundVerbs for each mode, kept when every mode lists it.
// With no modes, mode normal stands for them.
func (r *Runtime) TableUnboundVerbs(place NounPlace, modes []string) []Operation {
	if len(modes) == 0 {
		modes = []string{"normal"}
	}
	var out []Operation
	for i, name := range modes {
		ops := r.PlaceUnboundVerbs(place, ModeState{Name: name})
		if i == 0 {
			out = ops
			continue
		}
		out = slices.DeleteFunc(out, func(o Operation) bool {
			return !slices.ContainsFunc(ops, func(p Operation) bool { return p.Key == o.Key })
		})
	}
	return out
}

// helpTableKeyProps are a table row's props: the key's record, with the key
// as the column shows it (so sorting by key orders what is drawn) and
// unbound false, so the standard prop filter (z /) can keep only the
// unbound rows.
func helpTableKeyProps(label string, k HelpKey) Thing {
	props := Thing{}
	if record, err := ThingOf(k); err == nil {
		maps.Copy(props, record)
	}
	props["key"], props["unbound"] = label, false
	return props
}

// helpTableUnboundItems are the unbound rows below the table's keys: the
// key cell blank, the verb in the first mode column, the description in the
// detail under the list.
func (r *Runtime) helpTableUnboundItems(noun string, modes []string, ops []Operation) []ListItem {
	rows := r.unboundHelpRows(ops)
	items := make([]ListItem, len(rows))
	for i, row := range rows {
		cells := make([]string, len(modes)+1)
		text := helpOperationText(noun, row.Operation)
		cells[min(1, len(cells)-1)] = text
		props := Thing{"key": "", "unbound": true, "request": row.Request}
		items[i] = ListItem{ID: fmt.Sprintf("__bindings-help/unbound/%d", i), Label: strings.Join(cells, "  "), Noun: "ochazuke.binding", Section: helpTableUnboundSection, Data: row, Props: props, Cells: cells, Search: []string{row.Operation, row.Description}}
	}
	return items
}

// tableItems are the table view's rows: a row per key, then, with
// help.table-unbound, the unbound verbs.
func (h *helpState) tableItems(keys []HelpKey) []ListItem {
	noun := h.r.DisplayNoun(h.noun)
	items := helpTableItems(noun, h.modes, keys)
	if h.r.helpFlag(HelpTableUnboundSetting, true) {
		items = append(items, h.r.helpTableUnboundItems(noun, h.modes, h.r.TableUnboundVerbs(h.at(), h.modes))...)
	}
	return items
}

// helpUnboundDetail is the selected unbound row under the list: the
// operation, then its description.
func helpUnboundDetail(row HelpRow, width int) []string {
	lines := []string{ansi.Truncate(row.Operation+" · unbound", width, "…")}
	if row.Description != "" {
		lines = append(lines, ansi.Truncate(row.Description, width, "…"))
	}
	return lines
}

func registerHelpTable(r *Runtime) {
	r.DefineGuard("help-table", func(r *Runtime, _ KeyEvent) bool {
		h := r.helpOpen()
		return h != nil && h.view == HelpTable
	})
}
