package ochazuke

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

// NewDemo supplies example pages; none of these page names are required by the framework.
func NewDemo() *Model {
	r := NewRuntime()
	for _, id := range []string{"overview", "playground", "commands", "appearance", "help"} {
		r.Instance(id)
	}
	r.Instance("agent", Parent("playground"))
	for _, item := range []struct {
		in     *Input
		parent string
	}{
		{NewInput("directory", "/tmp", false), "agent"},
		{NewInput("notes", "A small workspace, with state that stays.\nTry i, type a few words, then Escape and u.\nSwitch pages and come back: your place is kept.", true), "agent"},
		{NewInput("paths", "/tmp\n.", true), "agent"},
		{NewInput("dialog", "This draft belongs to the modal.\nThe page remains underneath it.", true), "app"},
		{NewInput("nested-dialog", "A second modal, with its own history.", true), "app"},
	} {
		switch item.in.ID {
		case "directory":
			item.in.Kind, item.in.PathFilter = "path", "dir"
		case "paths":
			item.in.Kind, item.in.PathFilter = "paths", "any"
		}
		if err := r.AddInput(item.in, Parent(item.parent)); err != nil {
			panic(err)
		}
	}
	var m *Model
	lastNoun := ""
	for _, spec := range []struct {
		id, title, focus string
		render           func(*Runtime, int, int) Block
	}{
		{"overview", "Overview", "overview", func(r *Runtime, w, h int) Block { return Text(demoOverview(m, w)) }},
		{"playground", "Playground", "directory", func(r *Runtime, w, h int) Block { return demoPlayground(m, w, h, lastNoun) }},
		{"commands", "Commands", "commands", func(r *Runtime, w, h int) Block { return Text(demoCommands(m, w)) }},
		{"appearance", "Appearance", "appearance", func(r *Runtime, w, h int) Block { return Text(demoAppearance(m, w)) }},
	} {
		if err := r.RegisterPage(&Page{ID: spec.id, Title: spec.title, Focus: spec.focus, Render: spec.render}); err != nil {
			panic(err)
		}
	}
	m = NewModel(r)
	picker := NewPicker(r)
	m.ModalViews["picker"] = ModalView{Title: "Choose a path", Render: func(r *Runtime, f *Frame, w, h int) Block { return picker.ViewBlock(w, h) }}
	for _, id := range []string{"draft", "nested-draft"} {
		title := "Draft editor"
		if id == "nested-draft" {
			title = "Nested draft"
		}
		m.ModalViews[id] = ModalView{Title: title, Render: func(r *Runtime, f *Frame, w, h int) Block {
			s := NewStyles(*m.Appearance)
			hint := "i edit · Escape normal / close · Ctrl-G close"
			if f.ID == "draft" {
				hint += " · Ctrl-N nest"
			}
			editor := InputBlock(r.Input(f.Focus), w, max(1, h-2))
			return editor.Offset(0, 2).WithText(s.Muted.Render(safeText(hint)) + "\n\n" + editor.Text)
		}}
	}
	m.ModalViews["help"] = ModalView{Title: "Keyboard guide", Render: func(r *Runtime, f *Frame, w, h int) Block {
		return Text("NAVIGATION\nCtrl-J / K    Next / previous page (normal mode)\n:             Search command palette\nCtrl-Space    Palette in any editing mode\nPgUp / PgDn   Scroll the active surface\n\nPLAYGROUND\nTab / S-Tab   Next / previous field\nCtrl-N        Open a draft; repeat inside to nest\nCtrl-O        Choose a path using the field's filter\nEnter         Open picker in path / paths fields\n\nNORMAL INPUT\nh j k l       Move (same as arrows)\nw b e         Word motions\n0 $ / gg G    Line / document bounds\ni a I A       Begin inserting\no O           Open a line\nv / V / C-v   Character / line / block selection\nyy / p / P    Yank line / paste after / before\nCtrl-Y        Yank history\n\"a            Select register a\nq then a      Start recording immediately in register a\nq / @a        Stop recording / replay macro a\nu / Ctrl-R    Undo / redo\n\nINSERT INPUT\nCtrl-A / E    Line start / end\nCtrl-P / N    Previous / next line (picker: result)\nAlt-B / F     Word movement\nCtrl-W/U/K    Kill path word / line start / line end\nCtrl-Y        Paste app yank\nEscape        Return to normal")
	}}
	register := func(verb, noun, description string, params map[string]Parameter, fn Handler) {
		r.Register(Operation{Verb: verb, Noun: noun, Description: description, Parameters: params, Result: "JSON value", Run: fn})
	}
	register("next", "input", "Cycle playground field focus", map[string]Parameter{"delta": {Type: "integer"}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		if r.Page != "playground" || len(r.Frames) > 1 {
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
	register("inspect", "selection", "Report the innermost noun handling this action", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		// No noun on the stack overrides this verb: report the focused
		// instance's noun (a row reports its noun, not its row ID).
		name := "app"
		if inst := r.Instances[focusOf(c)]; inst != nil && inst.Noun != "" {
			name = inst.Noun
		}
		lastNoun = name
		return name, nil
	})
	for _, id := range []string{"playground", "agent"} {
		name := id
		r.Define(id).Action("inspect.selection", func(r *Runtime, c Context, a Arguments) (any, error) {
			lastNoun = name
			return name, nil
		})
	}
	register("open", "help", "Open the keyboard guide without losing your place", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.Push(&Frame{ID: "help", Focus: "help"})
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
	// bind puts a row on a noun, or on an input instance when id names one.
	bind := func(id, mode, key, verb, noun string, args Arguments) {
		row := Binding{Noun: id, Mode: mode, Keys: key, Request: Request{Verb: verb, Noun: noun, Args: args}}
		if r.Input(id) != nil {
			row.Noun, row.Instance = "", id
		}
		if err := r.Bind(row); err != nil {
			panic(err)
		}
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
	bind("playground", "normal", "ctrl+x", "inspect", "selection", nil)
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
	for _, page := range m.Runtime.PageList() {
		descriptions := map[string]string{"overview": "Start here: navigation and the framework's structure.", "playground": "Edit text, branch undo history, and open nested modals.", "commands": "Inspect real operations, argument schemas, and bindings.", "appearance": "Change the live theme and spacing defaults."}
		fmt.Fprintf(&b, "%s\n  %s\n\n", s.Subtitle.Render(page.Title), s.Muted.Render(descriptions[page.ID]))
	}
	b.WriteString(s.Panel.Width(max(1, width-2)).Render("TRY IT\nCtrl-J → i → type → Escape → u\nCtrl-N opens a modal without replacing the page.\n: searches the same actions available through the CLI."))
	b.WriteString("\n\n" + s.Muted.Render("? keyboard guide · Ctrl-J/K switch pages · Ctrl-C quit"))
	return b.String()
}
func demoPlayground(m *Model, width, height int, lastNoun string) Block {
	r := m.Runtime
	s := NewStyles(*m.Appearance)
	frame := r.PageOf("playground").Frame
	// Only the focused field carries a cursor; the panel border, padding and
	// label row move it.
	field := func(id, label string, w, h int) Block {
		active := frame.Focus == id
		style := s.Panel
		if active {
			style = s.FocusedPanel
		}
		marker := "  "
		if active {
			marker = "› "
		}
		body := InputBlock(r.Input(id), max(1, w-2-2*s.Padding), h)
		if !active {
			body.Cursor = nil
		}
		return body.Offset(1+s.Padding, 2).WithText(style.Padding(0, s.Padding).Width(max(1, w-2)).Render(s.Subtitle.Render(marker+label) + "\n" + body.Text))
	}
	var fields Block
	if height < 18 {
		labels := map[string]string{"directory": "Directory · path", "notes": "Notes · text", "paths": "Paths · one per line"}
		id := frame.Focus
		fields = field(id, labels[id], width, max(1, height-9))
	} else if width >= 76 {
		left := width / 3
		right := width - left - 2
		directory := field("directory", "Directory · path", left, 1)
		paths := field("paths", "Paths · one per line", left, 3).Offset(0, lipgloss.Height(directory.Text))
		notes := field("notes", "Notes · text", right, 7).Offset(left+2, 0)
		fields = Block{Text: lipgloss.JoinHorizontal(lipgloss.Top, directory.Text+"\n"+paths.Text, "  ", notes.Text), Cursor: firstCursor(directory, paths, notes)}
	} else {
		directory := field("directory", "Directory · path", width, 1)
		notes := field("notes", "Notes · text", width, 2).Offset(0, lipgloss.Height(directory.Text))
		paths := field("paths", "Paths · one per line", width, 2).Offset(0, lipgloss.Height(directory.Text)+lipgloss.Height(notes.Text))
		fields = Block{Text: directory.Text + "\n" + notes.Text + "\n" + paths.Text, Cursor: firstCursor(directory, notes, paths)}
	}
	status := "App → Playground → Agent → " + strings.Title(frame.Focus)
	if lastNoun != "" {
		status = "Ctrl-X action handled by: " + lastNoun
	}
	// Title, subtitle and a blank row sit above the fields.
	return fields.Offset(0, 3).WithText(s.Title.Render("Widget playground") + "\n" + s.Muted.Render("Shared yanks and registers. Independent edit histories.") + "\n\n" + fields.Text + "\n\n" + s.Accent.Render(safeText(status)) + "\n" + s.Muted.Render("Tab field · i insert · v/V/C-v select · Ctrl-Y yank history\nCtrl-N draft · Ctrl-O path picker · Ctrl-X inspect"))
}
func demoCommands(m *Model, width int) string {
	r := m.Runtime
	s := NewStyles(*m.Appearance)
	var b strings.Builder
	b.WriteString(s.Title.Render("Commands & bindings") + "\n" + s.Muted.Render("Live registry · every action is available as CLI: NOUN VERB") + "\n\n")
	focus := r.PageOf("playground").Frame.Focus
	b.WriteString(s.Subtitle.Render("Remembered Playground bindings") + "\n")
	b.WriteString(s.Muted.Render(strings.Join(r.InstancePath(focus), " → ")+" · "+r.modeState(focus).Name) + "\n")
	for _, binding := range r.ListBindings(focus) {
		key := binding.Keys
		if key == "" {
			key = "<" + binding.Pattern + ">"
		}
		fmt.Fprintf(&b, "%-13s %s\n", safeText(key), s.Accent.Render(safeText(binding.Request.Key())))
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
