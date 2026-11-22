package main

import (
	"fmt"
	"slices"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/table"
)

// Every package of the module with its tier, role and how many module
// packages it imports.
var packages = [][4]any{
	{"trie", 0, "primitive", 0}, {"expr", 0, "primitive", 0}, {"layout", 0, "primitive", 0}, {"buffer", 0, "primitive", 1},
	{"core", 1, "kernel", 3}, {"ui", 1, "render", 3}, {"tea", 1, "adapter", 3},
	{"config", 2, "loader", 3}, {"widgets/table", 2, "widget", 2}, {"widgets/picker", 2, "widget", 2},
	{"widgets/pathpicker", 2, "widget", 3}, {"widgets/form", 2, "widget", 2}, {"widgets/input", 2, "widget", 2},
	{"widgets/bindings", 2, "widget", 4},
	{"examples/minimal", 3, "example", 3}, {"examples/views-json", 3, "example", 4},
	{"examples/cards", 3, "example", 4}, {"examples/widgets", 3, "example", 9},
}

func newTable() *table.Model {
	cols := []table.Column{{Key: "name", Title: "Package"}, {Key: "tier", Title: "Tier", Align: "right"}, {Key: "role", Title: "Role"}, {Key: "deps", Title: "Deps", Align: "right"}}
	rows := make([]table.Row, len(packages))
	for i, p := range packages {
		name := p[0].(string)
		rows[i] = table.Row{ID: name, Ref: &core.Ref{Kind: "package", ID: name}, Cells: map[string]any{"name": name, "tier": p[1], "role": p[2], "deps": p[3]}}
	}
	return table.New(cols, rows)
}

func tableBindings() []core.Binding {
	t := func(mode string, keys []string, cmd, hint string) core.Binding {
		return core.Binding{Scope: "tab.table", Mode: mode, Keys: keys, Command: cmd, Hint: hint}
	}
	return []core.Binding{
		t("normal", k("/"), "table.filter.start", "filter"),
		t("normal", k("m"), "table.filter.mode.cycle", "match mode"),
		t("normal", k("f"), "table.filter.field.next", "filter column"),
		t("normal", k("s"), "table.sort.next", "sort"),
		t("normal", k("g"), "table.group.next", "group"),
		t("normal", k("z"), "table.group.toggle", "fold group"),
		t("normal", k("space"), "table.mark.toggle", "mark"),
		t("table.filter", k("enter"), "table.filter.done", "keep filter"),
		t("table.filter", k("esc"), "table.filter.clear", "clear filter"),
		t("table.filter", k("backspace"), "table.filter.backspace", ""),
		{Scope: "tab.table", Mode: "table.filter", Class: core.ClassPrintable, Command: "table.filter.append", Args: map[string]any{"text": "$text"}},
	}
}

// next returns the item after cur in list, wrapping around.
func next(list []string, cur string) string {
	return list[(slices.Index(list, cur)+1)%len(list)]
}

func (a *app) columnKeys() []string {
	keys := make([]string, len(a.table.Columns))
	for i, c := range a.table.Columns {
		keys[i] = c.Key
	}
	return keys
}

func (a *app) tableHandlers() {
	m := a.table
	widget := func(s core.State, c core.Command) core.State {
		if r, ok := s.Layout.Rects["table"]; ok {
			m.PageSize = max(1, r.H-1) // one header row
		}
		m.HandleCommand(c)
		return s
	}
	a.route("tab.table", widget, slices.Concat(motions, k("table.mark.toggle", "table.filter.mode.cycle", "table.group.toggle"))...)
	a.route("tab.table", func(s core.State, c core.Command) core.State {
		switch c.ID {
		case "table.filter.start":
			return setMode(s, "table.filter")
		case "table.filter.append":
			m.Query += c.Arg("text")
		case "table.filter.backspace":
			if r := []rune(m.Query); len(r) > 0 {
				m.Query = string(r[:len(r)-1])
			}
		case "table.filter.clear":
			m.Query = ""
			return setMode(s, "normal")
		case "table.filter.done":
			return setMode(s, "normal")
		case "table.filter.field.next":
			m.FilterField = next(append([]string{""}, a.columnKeys()...), m.FilterField)
		case "table.group.next":
			// Any column groups, even one whose values are all unique.
			m.GroupField, m.Collapsed = next(append([]string{""}, a.columnKeys()...), m.GroupField), nil
		case "table.sort.next":
			a.nextSort()
		}
		m.SelectedIdx = 0
		return s
	}, "table.filter.start", "table.filter.append", "table.filter.backspace", "table.filter.clear", "table.filter.done",
		"table.filter.field.next", "table.group.next", "table.sort.next")
}

// nextSort walks name ▲, name ▼, tier ▲, ... and back to unsorted.
func (a *app) nextSort() {
	m, keys := a.table, a.columnKeys()
	i := slices.Index(keys, m.SortField)
	switch {
	case m.SortField == "":
		m.CycleSort(keys[0])
	case m.SortDir == table.SortAsc, i == len(keys)-1:
		m.CycleSort("")
	default:
		m.CycleSort(keys[i+1])
	}
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (a *app) tableView(s core.State) []ui.ViewNode {
	m := a.table
	prompt := "/" + m.Query
	if a.eng.Mode(s) == "table.filter" {
		prompt += "▏"
	}
	sort := "none"
	if m.SortField != "" {
		sort = m.SortField + map[table.SortDir]string{table.SortAsc: " ▲", table.SortDesc: " ▼"}[m.SortDir]
	}
	line := fmt.Sprintf(" %s · %s in %s · sort %s · group %s · %d marked · %d of %d rows",
		prompt, or(string(m.MatchMode), "substring"), or(m.FilterField, "all columns"), sort, or(m.GroupField, "none"), len(m.Marked), len(m.Filtered()), len(m.Rows))
	kids := []ui.ViewNode{text(line, "accent")}
	if m.Filtered(); m.FilterErr != "" {
		kids = append(kids, text(" regex: "+m.FilterErr, "error"))
	}
	tv := m.View("table")
	tv.Box = flex1
	if marked := m.MarkedRows(); len(marked) > 0 {
		names := make([]string, len(marked))
		for i, r := range marked {
			names[i] = r.ID
		}
		return append(kids, tv, text(" marked: "+strings.Join(names, ", "), "muted"))
	}
	return append(kids, tv)
}
