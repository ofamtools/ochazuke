package ochazuke

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// NewDemo supplies example pages; none of these page names are required by the framework.
func NewDemo() *Model {
	r := NewRuntime()
	for _, id := range []string{"overview", "playground", "commands", "appearance", "agent", "directory", "notes", "paths", "dialog", "nested-dialog", "help"} {
		r.Layers[id] = &Layer{ID: id, Noun: id, Actions: map[string]Handler{}}
	}
	for _, item := range []struct {
		in   *Input
		path []string
	}{
		{NewInput("directory", "/tmp", false), []string{"app", "playground", "agent", "directory"}},
		{NewInput("notes", "A small workspace, with state that stays.\nTry i, type a few words, then Escape and u.\nSwitch pages and come back: your place is kept.", true), []string{"app", "playground", "agent", "notes"}},
		{NewInput("paths", "/tmp\n.", true), []string{"app", "playground", "agent", "paths"}},
		{NewInput("dialog", "This draft belongs to the modal.\nThe page remains underneath it.", true), []string{"app", "dialog"}},
		{NewInput("nested-dialog", "A second modal, with its own history.", true), []string{"app", "nested-dialog"}},
	} {
		switch item.in.ID {
		case "directory":
			item.in.Kind, item.in.PathFilter = "path", "dir"
		case "paths":
			item.in.Kind, item.in.PathFilter = "paths", "any"
		}
		if err := r.AddInput(item.in, item.path); err != nil {
			panic(err)
		}
	}
	var m *Model
	for _, spec := range []struct {
		id, title string
		context   Context
		render    func(*Runtime, int, int) string
	}{
		{"overview", "Overview", Context{Path: []string{"app", "overview"}}, func(r *Runtime, w, h int) string { return demoOverview(m, w) }},
		{"playground", "Playground", Context{Path: []string{"app", "playground", "agent", "directory"}, InputID: "directory"}, func(r *Runtime, w, h int) string { return demoPlayground(m, w, h) }},
		{"commands", "Commands", Context{Path: []string{"app", "commands"}}, func(r *Runtime, w, h int) string { return demoCommands(m, w) }},
		{"appearance", "Appearance", Context{Path: []string{"app", "appearance"}}, func(r *Runtime, w, h int) string { return demoAppearance(m, w) }},
	} {
		if err := r.RegisterPage(&Page{ID: spec.id, Title: spec.title, Frame: &Frame{ID: spec.id, Context: spec.context, State: map[string]any{}}, Render: spec.render}); err != nil {
			panic(err)
		}
	}
	m = NewModel(r)
	picker := NewPicker(r)
	m.Pickers = append(m.Pickers, picker)
	m.ModalViews["picker"] = ModalView{Title: "Choose a path", Render: func(r *Runtime, f *Frame, w, h int) string { return picker.View(w, h) }}
	for _, id := range []string{"draft", "nested-draft"} {
		title := "Draft editor"
		if id == "nested-draft" {
			title = "Nested draft"
		}
		m.ModalViews[id] = ModalView{Title: title, Render: func(r *Runtime, f *Frame, w, h int) string {
			s := NewStyles(*m.Appearance)
			hint := "i edit · Escape normal / close · Ctrl-G close"
			if f.ID == "draft" {
				hint += " · Ctrl-N nest"
			}
			return s.Muted.Render(safeText(hint)) + "\n\n" + InputView(r.Inputs[f.Context.InputID], w, max(1, h-2))
		}}
	}
	m.ModalViews["help"] = ModalView{Title: "Keyboard guide", Render: func(r *Runtime, f *Frame, w, h int) string {
		return "NAVIGATION\nCtrl-J / K    Next / previous page (normal mode)\n:             Search command palette\nCtrl-Space    Palette in any editing mode\nPgUp / PgDn   Scroll the active surface\n\nPLAYGROUND\nTab / S-Tab   Next / previous field\nCtrl-N        Open a draft; repeat inside to nest\nCtrl-O        Choose a path using the field's filter\nEnter         Open picker in path / paths fields\n\nNORMAL INPUT\nh j k l       Move (same as arrows)\nw b e         Word motions\n0 $ / gg G    Line / document bounds\ni a I A       Begin inserting\no O           Open a line\nv / V / C-v   Character / line / block selection\nyy / p / P    Yank line / paste after / before\nCtrl-Y        Yank history\n\"a            Select register a\nq then a      Start recording immediately in register a\nq / @a        Stop recording / replay macro a\nu / Ctrl-R    Undo / redo\n\nINSERT INPUT\nCtrl-A / E    Line start / end\nCtrl-P / N    Previous / next line (picker: result)\nAlt-B / F     Word movement\nCtrl-W/U/K    Kill path word / line start / line end\nCtrl-Y        Paste app yank\nEscape        Return to normal"
	}}
	register := func(verb, noun, description string, params map[string]Parameter, fn Handler) {
		r.Register(Operation{Verb: verb, Noun: noun, Description: description, Parameters: params, Result: "JSON value", Run: fn})
	}
	register("next", "input", "Cycle playground field focus", map[string]Parameter{"delta": {Type: "integer"}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		if r.ActivePage != "playground" || len(r.Frames) > 1 {
			return nil, errors.New("switch fields on the playground page, with modals closed")
		}
		ids := []string{"directory", "notes", "paths"}
		index := 0
		for i, id := range ids {
			if id == c.InputID {
				index = i
				break
			}
		}
		index = ((index+IntArg(a, "delta", 1))%len(ids) + len(ids)) % len(ids)
		err := r.Focus(ids[index])
		return r.Context(), err
	})
	register("inspect", "selection", "Report the innermost layer handling this action", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		r.Frames[len(r.Frames)-1].State["lastLayer"] = "app"
		return "app", nil
	})
	for _, id := range []string{"playground", "agent", "directory"} {
		name := id
		r.Layers[id].Actions["inspect.selection"] = func(r *Runtime, c Context, a Arguments) (any, error) {
			r.Frames[len(r.Frames)-1].State["lastLayer"] = name
			return name, nil
		}
	}
	register("open", "help", "Open the keyboard guide without losing your place", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.Push(&Frame{ID: "help", Context: Context{Path: []string{"app", "help"}}, State: map[string]any{}})
		return r.Context(), err
	})
	register("cycle", "theme", "Cycle through the available default themes", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		themes := []string{"adaptive", "dark", "light", "mono"}
		next := 0
		for i, name := range themes {
			if name == m.Appearance.Theme {
				next = (i + 1) % len(themes)
			}
		}
		return r.ExecuteAt(c, Request{Verb: "set", Noun: "appearance", Args: Arguments{"theme": themes[next]}})
	})
	register("cycle", "density", "Toggle comfortable and compact spacing", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		density := "compact"
		if m.Appearance.Density == "compact" {
			density = "comfortable"
		}
		return r.ExecuteAt(c, Request{Verb: "set", Noun: "appearance", Args: Arguments{"density": density}})
	})
	bind := func(layer, mode, key, verb, noun string, args Arguments) {
		r.Layers[layer].Bindings = append(r.Layers[layer].Bindings, Binding{Mode: mode, Keys: key, Request: Request{Verb: verb, Noun: noun, Args: args}})
	}
	bind("app", "normal", "?", "open", "help", nil)
	for _, id := range []string{"overview", "commands", "appearance"} {
		bind(id, "normal", "j", "scroll", "surface", Arguments{"lines": 1})
		bind(id, "normal", "k", "scroll", "surface", Arguments{"lines": -1})
		bind(id, "normal", "down", "scroll", "surface", Arguments{"lines": 1})
		bind(id, "normal", "up", "scroll", "surface", Arguments{"lines": -1})
	}
	bind("playground", "", "tab", "next", "input", nil)
	bind("playground", "", "shift+tab", "next", "input", Arguments{"delta": -1})
	bind("playground", "normal", "ctrl+o", "open", "picker", nil)
	for _, id := range []string{"directory", "paths"} {
		bind(id, "normal", "enter", "open", "picker", nil)
	}
	bind("playground", "normal", "ctrl+n", "push", "modal", Arguments{"id": "draft", "input": "dialog"})
	bind("dialog", "normal", "ctrl+n", "push", "modal", Arguments{"id": "nested-draft", "input": "nested-dialog"})
	for _, id := range []string{"dialog", "nested-dialog", "help"} {
		bind(id, "", "ctrl+g", "pop", "modal", nil)
	}
	bind("help", "", "ctrl+c", "pop", "modal", nil)
	bind("playground", "normal", "f2", "inspect", "selection", nil)
	bind("appearance", "normal", "t", "cycle", "theme", nil)
	bind("appearance", "normal", "d", "cycle", "density", nil)
	if err := r.OpenPage("overview"); err != nil {
		panic(err)
	}
	return m
}

func demoOverview(m *Model, width int) string {
	s := NewStyles(*m.Appearance)
	var b strings.Builder
	b.WriteString(s.Title.Render("A framework. Your application.") + "\n")
	b.WriteString(s.Muted.Render("Pages keep their place. Actions share one interface.") + "\n\n")
	for _, id := range m.Runtime.PageOrder {
		descriptions := map[string]string{"overview": "Start here: navigation and the framework's structure.", "playground": "Edit text, branch undo history, and open nested modals.", "commands": "Inspect real operations, argument schemas, and bindings.", "appearance": "Change the live theme and spacing defaults."}
		fmt.Fprintf(&b, "%s\n  %s\n\n", s.Subtitle.Render(m.Runtime.Pages[id].Title), s.Muted.Render(descriptions[id]))
	}
	b.WriteString(s.Panel.Width(max(1, width-2)).Render("TRY IT\nCtrl-J → i → type → Escape → u\nCtrl-N opens a modal without replacing the page.\n: searches the same actions available through the CLI."))
	b.WriteString("\n\n" + s.Muted.Render("? keyboard guide · Ctrl-J/K switch pages · Ctrl-C quit"))
	return b.String()
}
func demoPlayground(m *Model, width, height int) string {
	r := m.Runtime
	s := NewStyles(*m.Appearance)
	frame := r.Pages["playground"].Frame
	field := func(id, label string, w, h int) string {
		active := frame.Context.InputID == id
		style := s.Panel
		if active {
			style = s.FocusedPanel
		}
		marker := "  "
		if active {
			marker = "› "
		}
		body := InputView(r.Inputs[id], max(1, w-2-2*s.Padding), h)
		return style.Padding(0, s.Padding).Width(max(1, w-2)).Render(s.Subtitle.Render(marker+label) + "\n" + body)
	}
	var fields string
	if height < 18 {
		labels := map[string]string{"directory": "Directory · path", "notes": "Notes · text", "paths": "Paths · one per line"}
		id := frame.Context.InputID
		fields = field(id, labels[id], width, max(1, height-9))
	} else if width >= 76 {
		left := width / 3
		right := width - left - 2
		paths := field("directory", "Directory · path", left, 1) + "\n" + field("paths", "Paths · one per line", left, 3)
		fields = lipgloss.JoinHorizontal(lipgloss.Top, paths, "  ", field("notes", "Notes · text", right, 7))
	} else {
		fields = field("directory", "Directory · path", width, 1) + "\n" + field("notes", "Notes · text", width, 2) + "\n" + field("paths", "Paths · one per line", width, 2)
	}
	status := "App → Playground → Agent → " + strings.Title(frame.Context.Path[len(frame.Context.Path)-1])
	if layer, ok := frame.State["lastLayer"].(string); ok {
		status = "F2 action handled by: " + layer
	}
	return s.Title.Render("Widget playground") + "\n" + s.Muted.Render("Shared yanks and registers. Independent edit histories.") + "\n\n" + fields + "\n\n" + s.Accent.Render(safeText(status)) + "\n" + s.Muted.Render("Tab field · i insert · v/V/C-v select · Ctrl-Y yank history\nCtrl-N draft · Ctrl-O path picker · F2 inspect")
}
func demoCommands(m *Model, width int) string {
	r := m.Runtime
	s := NewStyles(*m.Appearance)
	var b strings.Builder
	b.WriteString(s.Title.Render("Commands & bindings") + "\n" + s.Muted.Render("Live registry · every action is available as CLI: NOUN VERB") + "\n\n")
	context := r.Pages["playground"].Frame.Context
	b.WriteString(s.Subtitle.Render("Remembered Playground bindings") + "\n")
	b.WriteString(s.Muted.Render(strings.Join(context.Path, " → ")+" · "+r.Mode(context)) + "\n")
	for _, binding := range r.EffectiveBindings(context) {
		fmt.Fprintf(&b, "%-13s %s\n", safeText(binding.Keys), s.Accent.Render(safeText(binding.Request.Key())))
	}
	b.WriteString("\n" + s.Subtitle.Render("Operation catalog") + "\n")
	for _, op := range r.Catalog() {
		names := make([]string, 0, len(op.Parameters))
		for name := range op.Parameters {
			names = append(names, name)
		}
		sort.Strings(names)
		params := make([]string, 0, len(names))
		for _, name := range names {
			p := op.Parameters[name]
			suffix := "?"
			if p.Required {
				suffix = ""
			}
			params = append(params, name+suffix+":"+p.Type)
		}
		b.WriteString(s.Accent.Render(safeText(op.Noun+" "+op.Verb)) + "  " + s.Muted.Render(safeText(strings.Join(params, " · "))) + "\n")
		b.WriteString("  " + safeText(op.Description) + "\n")
	}
	return b.String()
}
func demoAppearance(m *Model, width int) string {
	s := NewStyles(*m.Appearance)
	panel := s.Panel.Width(max(1, width-2))
	return s.Title.Render("Make it feel like your terminal") + "\n" + s.Muted.Render("These settings apply to every page and modal immediately.") + "\n\n" +
		panel.Render(s.Subtitle.Render("Theme   ")+s.Accent.Render(m.Appearance.Theme)+"\nAdaptive · Dark · Light · Mono\n\n"+s.Subtitle.Render("Density ")+s.Accent.Render(m.Appearance.Density)+"\nComfortable · Compact") + "\n\n" +
		s.Accent.Render("t") + " cycle theme    " + s.Accent.Render("d") + " toggle density\n\n" +
		s.FocusedPanel.Width(max(1, width-2)).Render("Focused surface\n"+s.Muted.Render("Muted context stays readable.")+"  "+s.Badge.Render("NORMAL")) + "\n\n" +
		s.Muted.Render("Palette: set → appearance\nCLI: appearance set --json '{\"theme\":\"mono\",\"density\":\"compact\"}'")
}
