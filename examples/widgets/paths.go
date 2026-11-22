package main

import (
	"fmt"
	"slices"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/pathpicker"
)

func pathBindings() []core.Binding {
	p := func(mode string, keys []string, cmd, hint string) core.Binding {
		return core.Binding{Scope: "tab.paths", Mode: mode, Keys: keys, Command: cmd, Hint: hint}
	}
	return []core.Binding{
		p("normal", k("enter"), pathpicker.CmdOpen, "open"),
		p("normal", k("l"), pathpicker.CmdOpen, ""),
		p("normal", k("h"), pathpicker.CmdParent, "parent"),
		p("normal", k("backspace"), pathpicker.CmdParent, ""),
		p("normal", k("u"), pathpicker.CmdSelectCurrent, "use this folder"),
		p("normal", k("/"), "paths.query.start", "filter or path"),
		p("normal", k("~"), "paths.query.home", "type a ~ path"),
		p("normal", k("r"), pathpicker.CmdRootCycle, "next root (2r jumps)"),
		p("normal", k("+"), "paths.mkdir.start", "new folder"),
		p("normal", k("p"), pathpicker.CmdPreviewToggle, "preview"),
		p("normal", k("."), pathpicker.CmdHiddenToggle, "hidden"),
		p("normal", k("m"), pathpicker.CmdModeCycle, "match mode"),
		p("paths.query", k("enter"), "paths.query.open", "open"),
		p("paths.query", k("esc"), "paths.query.clear", "clear"),
		p("paths.query", k("backspace"), "paths.query.backspace", ""),
		p("paths.query", k("down"), "motion.down", ""),
		p("paths.query", k("up"), "motion.up", ""),
		{Scope: "tab.paths", Mode: "paths.query", Class: core.ClassPrintable, Command: "paths.query.append", Args: map[string]any{"text": "$text"}},
		p("paths.mkdir", k("enter"), "paths.mkdir.done", "create"),
		p("paths.mkdir", k("esc"), "paths.mkdir.cancel", "cancel"),
		p("paths.mkdir", k("backspace"), "paths.mkdir.backspace", ""),
		{Scope: "tab.paths", Mode: "paths.mkdir", Class: core.ClassPrintable, Command: "paths.mkdir.append", Args: map[string]any{"text": "$text"}},
	}
}

func (a *app) pathHandlers() {
	m := a.paths
	// The widget's own commands. A pick (enter on a file or on the pinned
	// "[Use this folder]" row, u for the current folder) lands in the status
	// line, and the widget records it in its recents. A count on the root
	// cycle jumps to that root instead.
	widget := func(s core.State, c core.Command) core.State {
		switch {
		case c.ID == "paths.query.open":
			c.ID, s = pathpicker.CmdOpen, setMode(s, "normal")
		case c.ID == "paths.mkdir.done":
			c, s = core.Command{ID: pathpicker.CmdMkdir, Args: map[string]any{"name": a.mkdir}}, setMode(s, "normal")
		case c.ID == pathpicker.CmdRootCycle && c.Count > 0:
			m.JumpRoot(c.Count - 1)
			return s
		}
		if picked, _ := m.HandleCommand(c); picked != nil {
			a.note = "picked " + picked.Path
		} else if c.ID == pathpicker.CmdMkdir && m.Err == "" {
			a.note = "created " + a.mkdir
		}
		return s
	}
	a.route("tab.paths", widget, slices.Concat(motions, k(pathpicker.CmdOpen, pathpicker.CmdParent, pathpicker.CmdSelectCurrent,
		pathpicker.CmdPreviewToggle, pathpicker.CmdHiddenToggle, pathpicker.CmdModeCycle, pathpicker.CmdRootCycle,
		"paths.query.open", "paths.mkdir.done"))...)
	a.route("tab.paths", func(s core.State, c core.Command) core.State {
		switch c.ID {
		case "paths.query.start":
			return setMode(s, "paths.query")
		case "paths.query.home":
			m.SetQuery("~")
			return setMode(s, "paths.query")
		case "paths.query.append":
			m.AppendQuery(c.Arg("text"))
		case "paths.query.backspace":
			m.BackspaceQuery()
		case "paths.query.clear":
			m.SetQuery("")
			return setMode(s, "normal")
		case "paths.mkdir.start":
			a.mkdir = ""
			return setMode(s, "paths.mkdir")
		case "paths.mkdir.append":
			a.mkdir += c.Arg("text")
		case "paths.mkdir.backspace":
			if r := []rune(a.mkdir); len(r) > 0 {
				a.mkdir = string(r[:len(r)-1])
			}
		case "paths.mkdir.cancel":
			return setMode(s, "normal")
		}
		return s
	}, "paths.query.start", "paths.query.home", "paths.query.append", "paths.query.backspace", "paths.query.clear",
		"paths.mkdir.start", "paths.mkdir.append", "paths.mkdir.backspace", "paths.mkdir.cancel")
}

func (a *app) pathsView(s core.State) []ui.ViewNode {
	m := a.paths
	labels := make([]string, len(m.Roots))
	for i, r := range m.Roots {
		labels[i] = fmt.Sprintf("%d %s", i+1, r.Label)
		if r.Path == m.Dir {
			labels[i] = "[" + labels[i] + "]"
		}
	}
	kids := []ui.ViewNode{text(" roots "+strings.Join(labels, "  "), "accent")}
	switch a.eng.Mode(s) {
	case "paths.query":
		kids = append(kids, text(" typing filters, / ~ ./ ../ start a path, enter opens: "+m.Query+"▏", "text"))
	case "paths.mkdir":
		kids = append(kids, text(" new folder in "+m.Dir+": "+a.mkdir+"▏", "warning"))
	}
	pv := m.View("paths")
	pv.Box = flex1
	return append(kids, pv)
}
