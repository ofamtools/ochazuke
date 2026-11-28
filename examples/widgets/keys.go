package main

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/bindings"
)

//go:embed keyboard_bindings.json
var keyboardBindingsJSON []byte

// keysBindings drive the editor. Every other binding of the app is a row in
// it, and each edit recompiles the engine keymap, so changes are live.
func keysBindings() []core.Binding {
	b := func(mode string, keys []string, cmd, hint string) core.Binding {
		row := core.Binding{Scope: "tab.keys", Mode: mode, Keys: keys, Command: cmd, Hint: hint}
		if mode == "normal" {
			row.Scope, row.When = "bindings", `facts.viewMode == "table"`
		}
		return row
	}
	return []core.Binding{
		b("normal", k("r"), "keys.capture.start", "rebind (press the new key)"),
		b("normal", k("a"), "keys.add", "copy to a free key"),
		b("normal", k("x"), "bindings.unbind", "unbind"),
		b("normal", k("R"), "bindings.reset", "reset row"),
		b("normal", k("u"), "bindings.undo", "undo"),
		b("normal", k("ctrl+r"), "bindings.redo", "redo"),
		b("normal", k("/"), "keys.filter.start", "filter (key: verb: mode: scope: conflict:)"),
		b("normal", k("m"), "table.filter.mode.cycle", "match mode"),
		b("keys.filter", k("enter"), "keys.filter.done", "keep filter"),
		b("keys.filter", k("esc"), "keys.filter.clear", "clear filter"),
		b("keys.filter", k("backspace"), "bindings.filter.backspace", ""),
		{Scope: "tab.keys", Mode: "keys.filter", Class: core.ClassPrintable, Command: "bindings.filter.append", Args: map[string]any{"text": "$text"}},
		b("keys.capture", k("esc"), "keys.capture.cancel", "cancel"),
	}
}

func (a *app) keysHandlers() {
	m := a.keys
	a.route("tab.keys", a.toggleKeysView, bindings.CmdViewToggle)
	a.route("tab.keys", func(s core.State, c core.Command) core.State {
		m.HandleCommand(c)
		return s
	}, bindings.CmdPrefixNext, bindings.CmdPrefixPrev, bindings.CmdPrefixSelect,
		bindings.CmdPrefixClear, bindings.CmdShiftToggle, bindings.CmdKeySelect)
	a.route("tab.keys", func(s core.State, c core.Command) core.State {
		switch c.ID {
		case "keys.capture.start":
			return setMode(s, "keys.capture")
		case "keys.capture.cancel", "keys.filter.done":
			return setMode(s, "normal")
		case "keys.filter.start":
			return setMode(s, "keys.filter")
		case "keys.filter.clear":
			m.SetQuery("")
			return setMode(s, "normal")
		case "keys.rebind":
			if i, ok := m.Focused(); ok && m.Rebind(i, []string{c.Arg("key")}) {
				a.note = fmt.Sprintf("row %d now on %s", i, c.Arg("key"))
			}
			s = setMode(s, "normal")
		case "keys.add":
			i, ok := m.Focused()
			if !ok {
				return s
			}
			b := m.Bindings()[i]
			mode := b.Mode
			if mode == "" {
				mode = "normal"
			}
			free, ok := bindings.FindFreeBinding(modes, a.all(), b.Scope, mode, false, nil)
			if !ok {
				a.note = "no free key"
				return s
			}
			b.Keys, b.Class, b.Unbind = free, "", false
			m.Add(b)
			a.note = fmt.Sprintf("%s also on %s", b.Command, strings.Join(free, " "))
		default:
			m.HandleCommand(c)
		}
		a.applyKeys()
		return s
	}, slices.Concat(motions, k("keys.capture.start", "keys.capture.cancel", "keys.filter.start", "keys.filter.done", "keys.filter.clear",
		"keys.rebind", "keys.add", "bindings.unbind", "bindings.reset", "bindings.undo", "bindings.redo",
		"bindings.filter.append", "bindings.filter.backspace", "table.filter.mode.cycle"))...)
}

func (a *app) toggleKeysView(s core.State, c core.Command) core.State {
	a.keys.HandleCommand(c)
	f := s.ActiveFrame()
	f.FocusID = "keys.table"
	if a.keys.ViewMode == bindings.ViewKeyboard {
		f.FocusID = "keys.keyboard"
	}
	return s.WithActiveFrame(f)
}

// diagnostics explains the focused row: every conflict with the row it
// involves, and the first key FindFreeBinding offers in the same context.
func (a *app) diagnostics() []string {
	i, ok := a.keys.Focused()
	if !ok {
		return []string{"no row"}
	}
	rows := a.keys.Bindings()
	b := rows[i]
	describe := func(b core.Binding) string {
		key := strings.Join(b.Keys, " ")
		if b.Class != "" {
			key = "<" + b.Class + ">"
		}
		return fmt.Sprintf("%s %s/%s %s", key, or(b.Scope, "*"), or(b.Mode, "*"), or(b.Command, "layer"))
	}
	out := []string{fmt.Sprintf("row %d: %s", i, describe(b))}
	for _, c := range bindings.AnalyzeBindings(modes, rows, nil, a.specs...)[i].Conflicts {
		line := "  " + c.Kind
		if c.Other >= 0 {
			line += fmt.Sprintf(" with row %d: %s", c.Other, describe(rows[c.Other]))
		}
		if c.Message != "" {
			line += " (" + c.Message + ")"
		}
		out = append(out, line)
	}
	if len(out) == 1 {
		out = append(out, "  no conflicts")
	}
	if free, ok := bindings.FindFreeBinding(modes, a.all(), b.Scope, or(b.Mode, "normal"), false, nil); ok {
		out = append(out, "  first free key here: "+strings.Join(free, " ")+" (a copies the row onto it)")
	}
	return out
}

func (a *app) keysView(s core.State) []ui.ViewNode {
	ev := a.keys.View("keys")
	ev.Box = flex1
	if a.keys.ViewMode == bindings.ViewKeyboard {
		return []ui.ViewNode{ev}
	}
	diag := ui.ViewNode{Kind: ui.KindPanel, Title: "Diagnostics", Border: true}
	for _, line := range a.diagnostics() {
		tone := "text"
		if strings.HasPrefix(line, "  ") && !strings.Contains(line, "no conflicts") && !strings.Contains(line, "free key") {
			tone = "warning"
		}
		diag.Children = append(diag.Children, text(line, tone))
	}
	kids := []ui.ViewNode{ev, diag}
	if a.eng.Mode(s) == "keys.capture" {
		kids = append([]ui.ViewNode{text(" press the new key for the focused row, esc cancels", "warning")}, kids...)
	}
	return kids
}
