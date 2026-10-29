// Package demo is the runnable example behind `ochazuke run`: seven pages, nested
// draft modals, the picker and the keyboard guide, built only from the public
// API. The framework package holds no application pages.
package demo

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/markdown"
)

// checklistText is the Checklist page's document. The page keeps its
// toggles in memory (a markdown.Source of its own), so running the demo
// never rewrites the file; markdown.File(path) is the Source that does.
//
//go:embed testdata/checklist.md
var checklistText string

// New supplies the demo application: example pages, modals and bindings on a
// fresh runtime. None of these page names are required by the framework.
func New() *ochazuke.Model { return newWith(newDemoTask()) }

// newWith is New over a task store (a test holds its own).
func newWith(task *demoTask) *ochazuke.Model {
	r := ochazuke.NewRuntime()
	for _, id := range []string{"overview", "playground", "commands", "appearance", "library", "task", "checklist", "help"} {
		r.Instance(id)
	}
	r.Define("entry")
	entries, err := r.AddList("library/entries", libraryItems(), ochazuke.Parent("library"))
	if err != nil {
		panic(err)
	}
	detail, err := r.AddDetail("library/detail", func() string {
		selected, _ := entries.Selected()
		return selected.ID
	}, ochazuke.Parent("library"))
	if err != nil {
		panic(err)
	}
	stored := checklistText
	checklist, err := markdown.New(r, "checklist/doc", "checklist", markdown.Source{
		Load: func() (string, error) { return stored, nil },
		Save: func(text string) error { stored = text; return nil },
	})
	if err != nil {
		panic(err)
	}
	r.Instance("agent", ochazuke.Parent("playground"))
	for _, item := range []struct {
		in     *ochazuke.Input
		parent string
	}{
		{ochazuke.NewInput("directory", "/tmp", false), "agent"},
		{ochazuke.NewInput("notes", "A small workspace, with state that stays.\nTry i, type a few words, then Escape and u.\nType @, $ or [[ for suggestions.\nSwitch pages and come back: your place is kept.", true), "agent"},
		{ochazuke.NewInput("paths", "/tmp\n.", true), "agent"},
		{ochazuke.NewInput("dialog", "This draft belongs to the modal.\nThe page remains underneath it.", true), "app"},
		{ochazuke.NewInput("nested-dialog", "A second modal, with its own history.", true), "app"},
	} {
		switch item.in.ID {
		case "directory":
			item.in.Kind, item.in.PathFilter = "path", "dir"
		case "paths":
			item.in.Kind, item.in.PathFilter = "paths", "any"
		}
		opts := []ochazuke.InstanceOption{ochazuke.Parent(item.parent)}
		if item.in.ID == "notes" {
			opts = append(opts, ochazuke.Completions(demoCompletions()...))
		}
		if err := r.AddInput(item.in, opts...); err != nil {
			panic(err)
		}
	}
	var m *ochazuke.Model
	lastNoun := ""
	for _, spec := range []struct {
		id, title, focus string
		render           func(*ochazuke.Runtime, int, int) ochazuke.Block
	}{
		{"overview", "Overview", "overview", func(r *ochazuke.Runtime, w, h int) ochazuke.Block { return ochazuke.Text(demoOverview(m, w)) }},
		{"playground", "Playground", "directory", func(r *ochazuke.Runtime, w, h int) ochazuke.Block { return demoPlayground(m, w, h, lastNoun) }},
		{"commands", "Commands", "commands", func(r *ochazuke.Runtime, w, h int) ochazuke.Block { return ochazuke.Text(demoCommands(m, w)) }},
		{"appearance", "Appearance", "appearance", func(r *ochazuke.Runtime, w, h int) ochazuke.Block { return ochazuke.Text(demoAppearance(m, w)) }},
		{"library", "Library", "library/entries", func(r *ochazuke.Runtime, w, h int) ochazuke.Block { return demoLibrary(m, entries, detail, w, h) }},
		{"task", "Record", "task", func(r *ochazuke.Runtime, w, h int) ochazuke.Block { return task.view(m, w, h) }},
		{"checklist", "Checklist", checklist.FocusTarget(), func(r *ochazuke.Runtime, w, h int) ochazuke.Block {
			return demoChecklist(m, checklist, w, h)
		}},
	} {
		if _, err := r.AddPage(spec.id, spec.title, spec.render, ochazuke.Focus(spec.focus)); err != nil {
			panic(err)
		}
	}
	m = ochazuke.NewModel(r)
	picker := ochazuke.NewPicker(r)
	m.ModalViews["picker"] = ochazuke.ModalView{Title: "Choose a path", Render: func(r *ochazuke.Runtime, f *ochazuke.Instance, w, h int) ochazuke.Block {
		return picker.ViewBlock(w, h)
	}}
	for _, id := range []string{"draft", "nested-draft"} {
		title := "Draft editor"
		if id == "nested-draft" {
			title = "Nested draft"
		}
		m.ModalViews[id] = ochazuke.ModalView{Title: title, Render: func(r *ochazuke.Runtime, f *ochazuke.Instance, w, h int) ochazuke.Block {
			s := ochazuke.NewStyles(*m.Appearance)
			hint := "i edit · Escape normal / close · Ctrl-G close"
			if f.ID == "draft" {
				hint += " · Ctrl-N nest"
			}
			editor := ochazuke.InputBlock(r.Input(r.ModalFocus(f.ID)), w, max(1, h-2))
			return editor.Offset(0, 2).WithText(s.Muted.Render(safeText(hint)) + "\n\n" + editor.Text)
		}}
	}
	m.ModalViews["help"] = ochazuke.ModalView{Title: "Keyboard guide", Render: func(r *ochazuke.Runtime, f *ochazuke.Instance, w, h int) ochazuke.Block {
		return ochazuke.Text("NAVIGATION\nCtrl-J / K    Next / previous page (normal mode)\n:             Command line (palette)\nCtrl-Space    Palette in any editing mode\nPgUp / PgDn   Scroll the active surface\n\nPLAYGROUND\nTab / S-Tab   Next / previous field\nCtrl-N        Open a draft; repeat inside to nest\nCtrl-O        Choose a path using the field's filter\nEnter         Open picker in path / paths fields\n\nNORMAL INPUT\nh j k l       Move (same as arrows)\nw b e         Word motions\n0 $ / gg G    Line / document bounds\ni a I A       Begin inserting\no O           Open a line\nv / V / C-v   Character / line / block selection\nyy / p / P    Yank line / paste after / before\ng y           Yank history\n\"a            Select register a\nq then a      Start recording immediately in register a\nq / @a        Stop recording / replay macro a\nu / Ctrl-R    Undo / redo\n\nINSERT INPUT\nCtrl-A / E    Line start / end\nCtrl-P / N    Previous / next line (picker: result)\nAlt-B / F     Word movement\nCtrl-W/U/K    Kill path word / line start / line end\nCtrl-Y        Paste app yank\n@ $ [[        Notes: suggestions (Ctrl-N / P move, Tab or Enter take, Escape hides)\nEscape        Return to normal")
	}}
	register := func(verb, noun, description string, params map[string]ochazuke.Parameter, fn ochazuke.Handler) {
		r.Register(ochazuke.Operation{Verb: verb, Noun: noun, Description: description, Parameters: params, Result: "JSON value", Run: fn})
	}
	register("next", "input", "Cycle playground field focus", map[string]ochazuke.Parameter{"delta": {Type: "integer"}}, func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
		if r.Page != "playground" || len(r.Modals()) > 0 {
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
		index = ((index+ochazuke.IntArg(a, "delta", 1))%len(ids) + len(ids)) % len(ids)
		err := r.SetFocus(ids[index])
		return r.Context(), err
	})
	register("inspect", "selection", "Report the innermost noun handling this action", nil, func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
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
		r.Define(id).Action("selection.inspect", func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
			lastNoun = name
			return name, nil
		})
	}
	register("open", "help", "Open the keyboard guide without losing your place", nil, func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
		err := r.Push("help", "help")
		return r.Context(), err
	})
	// Theme and density are user settings: settings.json keeps them across
	// runs, settings.edit (s on the Appearance page) opens the Settings
	// page, which saves each change as it is made (q goes back here),
	// and t and d step them. theme is the framework's setting (its choices
	// are the built-in themes and the user's themes.json), declared here so
	// it exists before the Model starts; the Model applies it. density is
	// the demo's own: the observer applies every change, wherever it came
	// from (the Settings page, d, the CLI's settings set).
	r.EnsureConfigSettings()
	if err := r.DeclareSetting(
		ochazuke.Setting{Name: "density", Type: ochazuke.SettingString, Default: "comfortable", Description: "Spacing", Enum: []string{"comfortable", "compact"}},
		// Two list settings show the Settings page's item rows: one with
		// items, one empty. The demo reads neither; they are there to edit.
		ochazuke.Setting{Name: "projects.paths", Type: ochazuke.SettingList, Default: []string{"~/proj", "~/hub"}, Description: "Directories searched for projects, in order"},
		ochazuke.Setting{Name: "projects.ignore", Type: ochazuke.SettingList, Default: []string{}, Description: "Names left out of every project listing"},
	); err != nil {
		panic(err)
	}
	applyDensity := func(_ string, value any) {
		// The page instance "appearance" (created before NewModel) is a noun
		// of the demo's own, so the framework's appearance noun is reached by
		// its canonical name.
		_, _ = r.Execute(ochazuke.Request{Verb: "set", Noun: "ochazuke.appearance", Args: ochazuke.Arguments{"density": value}})
	}
	r.Settings().Observe("density", applyDensity)
	applyDensity("density", r.Settings().String("density"))
	register("cycle", "theme", "Cycle through the colour schemes: the built-in themes, then themes.json's", nil, func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
		themes := m.Appearance.ThemeNames()
		next := 0
		for i, name := range themes {
			if name == m.Appearance.Theme {
				next = (i + 1) % len(themes)
			}
		}
		return r.ExecuteAt(c, ochazuke.Request{Verb: "set", Noun: "settings", Args: ochazuke.Arguments{"name": "theme", "value": themes[next]}})
	})
	register("cycle", "accent", "Toggle an orange accent over the theme (Appearance.Accent), as an app does for a pin state", nil, func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
		if m.Appearance.Accent != nil {
			m.Appearance.Accent = nil
			return "theme", nil
		}
		m.Appearance.Accent = lipgloss.Color(demoAccent)
		return demoAccent, nil
	})
	register("cycle", "density", "Toggle comfortable and compact spacing", nil, func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
		density := "compact"
		if m.Appearance.Density == "compact" {
			density = "comfortable"
		}
		return r.ExecuteAt(c, ochazuke.Request{Verb: "set", Noun: "settings", Args: ochazuke.Arguments{"name": "density", "value": density}})
	})
	// bind puts a row on a noun, or on an input instance when id names one.
	bind := func(id, mode, key, verb, noun string, args ochazuke.Arguments) {
		row := ochazuke.Binding{Noun: id, Mode: mode, Keys: key, Request: ochazuke.Request{Verb: verb, Noun: noun, Args: args}}
		if r.Input(id) != nil {
			row.Noun, row.Instance = "", id
		}
		if err := r.Bind(row); err != nil {
			panic(err)
		}
	}
	bind("app", "normal", "?", "open", "help", nil)
	for _, id := range []string{"overview", "commands", "appearance"} {
		bind(id, "normal", "j", "scroll", "surface", ochazuke.Arguments{"lines": 1})
		bind(id, "normal", "k", "scroll", "surface", ochazuke.Arguments{"lines": -1})
		bind(id, "normal", "down", "scroll", "surface", ochazuke.Arguments{"lines": 1})
		bind(id, "normal", "up", "scroll", "surface", ochazuke.Arguments{"lines": -1})
	}
	bind("playground", "", "tab", "next", "input", nil)
	bind("playground", "", "shift+tab", "next", "input", ochazuke.Arguments{"delta": -1})
	bind("playground", "normal", "ctrl+o", "open", "picker", nil)
	for _, id := range []string{"directory", "paths"} {
		bind(id, "normal", "enter", "open", "picker", nil)
	}
	bind("playground", "normal", "ctrl+n", "push", "modal", ochazuke.Arguments{"id": "draft", "input": "dialog"})
	bind("dialog", "normal", "ctrl+n", "push", "modal", ochazuke.Arguments{"id": "nested-draft", "input": "nested-dialog"})
	for _, id := range []string{"dialog", "nested-dialog", "help"} {
		bind(id, "", "ctrl+g", "pop", "modal", nil)
	}
	bind("help", "", "ctrl+c", "pop", "modal", nil)
	bind("playground", "normal", "ctrl+x", "inspect", "selection", nil)
	bind("appearance", "normal", "t", "cycle", "theme", nil)
	bind("appearance", "normal", "d", "cycle", "density", nil)
	bind("appearance", "normal", "s", "edit", "settings", nil)
	bind("appearance", "normal", "a", "cycle", "accent", nil)
	r.Define("task").Verb("edit", func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
		return nil, task.edit(r)
	}, "Edit the sample record again")
	bind("task", "normal", "e", "edit", "task", nil)
	task.define(r)
	if err := task.edit(r); err != nil {
		panic(err)
	}
	if err := r.OpenPage("overview"); err != nil {
		panic(err)
	}
	return m
}

// demoTask is the Record page's application state: three stored tasks
// (the demo's "database"), the one the page edits, and the editor over it
// while it is open.
type demoTask struct {
	tasks   map[string]ochazuke.Record
	order   []string
	editing string
	writes  int
	editor  *ochazuke.RecordEditor
}

// newDemoTask is the demo's store: the task the Record page edits, one in
// progress, and one with no title, which lists show by its summary.
func newDemoTask() *demoTask {
	return &demoTask{
		tasks: map[string]ochazuke.Record{
			"t1": {"title": "Water the plants", "status": "todo", "estimate": "2"},
			"t2": {"title": "Write the release notes", "status": "doing", "estimate": "3", "notes": "Start from the CHANGELOG."},
			"t3": {"title": "", "status": "todo", "estimate": "1", "notes": "Call the plumber about the sink\nbefore Friday"},
		},
		order:   []string{"t1", "t2", "t3"},
		editing: "t1",
	}
}

// taskProps are the task noun's props: the record's fields (taskFields)
// as data, its key id, and a read-only summary (the first line of the
// notes) that stands in for an empty title.
func taskProps() []ochazuke.PropSpec {
	props := []ochazuke.PropSpec{{Name: "id", Key: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The task's key"}}}
	for _, f := range taskFields() {
		p := ochazuke.PropSpec{Name: f.Name, Parameter: f.Parameter, Multiline: f.Multiline}
		if f.Name == "title" {
			// Optional as a prop: an empty title shows the summary.
			p.Role, p.Fallback, p.Required = ochazuke.RoleLabel, "summary", false
		}
		props = append(props, p)
	}
	return append(props, ochazuke.PropSpec{Name: "summary", ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The first line of the notes"}})
}

// define gives the task noun its props and its store: task get and task
// list are then standard verbs, with no handler of the demo's own. The
// Record page stands for the task it edits, so task get there reads it.
func (t *demoTask) define(r *ochazuke.Runtime) {
	r.Define("task").Props(taskProps()...).Store(ochazuke.Store{
		Kind: ochazuke.Collection,
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			if _, ok := t.tasks[id]; !ok {
				return nil, fmt.Errorf("no task %q", id)
			}
			return t.thing(id), nil
		},
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			out := make([]ochazuke.Thing, 0, len(t.order))
			for _, id := range t.order {
				out = append(out, t.thing(id))
			}
			return out, nil
		},
	})
	r.Instances["task"].Data = ochazuke.Thing{"id": t.editing}
}

// thing is one stored task as its props: the estimate a number, the
// summary derived from the notes.
func (t *demoTask) thing(id string) ochazuke.Thing {
	rec := t.tasks[id]
	out := ochazuke.Thing{"id": id, "title": rec["title"], "status": rec["status"], "notes": rec["notes"]}
	if n, err := strconv.Atoi(rec["estimate"]); err == nil {
		out["estimate"] = n
	}
	summary, _, _ := strings.Cut(rec["notes"], "\n")
	out["summary"] = summary
	return out
}

// taskFields is the record's schema: a required title, a status chosen
// from three, a whole-number estimate that must be under 100, and notes.
func taskFields() []ochazuke.RecordField {
	return []ochazuke.RecordField{
		{Name: "title", Parameter: ochazuke.Parameter{Type: "string", Required: true}},
		{Name: "status", Parameter: ochazuke.Parameter{Type: "string", Required: true, Enum: []string{"todo", "doing", "done"}}},
		{Name: "estimate", Label: "estimate (hours)", Parameter: ochazuke.Parameter{Type: "integer"}, Validate: func(v string) error {
			if n, err := strconv.Atoi(v); err == nil && n >= 100 {
				return errors.New("under 100")
			}
			return nil
		}},
		{Name: "notes", Multiline: true},
	}
}

// edit opens the record editor on the Record page; leaving it keeps the
// page, which offers e to edit again.
func (t *demoTask) edit(r *ochazuke.Runtime) error {
	if t.editor != nil && !t.editor.Closed() {
		return t.editor.Focus()
	}
	ed, err := r.AddRecordEditor("task-form", ochazuke.RecordSpec{
		Fields: taskFields(),
		Load:   func() (ochazuke.Record, error) { return maps.Clone(t.tasks[t.editing]), nil },
		Save: func(w ochazuke.RecordWrite) ochazuke.Result {
			t.tasks[t.editing] = maps.Clone(w.Values)
			t.writes++
			// The store changed through the editor, not a verb: watches
			// of task list again.
			r.Changed("task")
			return ochazuke.Done(nil)
		},
	}, ochazuke.Parent("task"), ochazuke.FocusOnClose("task"))
	if err != nil {
		return err
	}
	t.editor = ed
	if r.Page != "task" {
		// The page opens on the first field.
		r.PageOf("task").Remembered = ed.FocusTarget()
		return nil
	}
	return ed.Focus()
}

func (t *demoTask) view(m *ochazuke.Model, width, height int) ochazuke.Block {
	s := ochazuke.NewStyles(*m.Appearance)
	head := s.Title.Render("Record editor") + "\n" + s.Muted.Render("Every change is written as you type; there is no save key.") + "\n\n"
	var stored []string
	for _, f := range taskFields() {
		stored = append(stored, f.Name+"="+strconv.Quote(t.tasks[t.editing][f.Name]))
	}
	foot := "\n\n" + s.Muted.Render(ansi.Truncate(fmt.Sprintf("stored (%d writes): %s", t.writes, strings.Join(stored, " ")), width, "…"))
	if t.editor == nil || t.editor.Closed() {
		return ochazuke.Text(head + s.Muted.Render("The editor is closed; e edits the record again.") + foot)
	}
	t.editor.Styles = s
	block := t.editor.Block(width, max(1, height-6))
	return block.Offset(0, 3).WithText(head + block.Text + foot)
}

// demoChecklist draws the Checklist page: a Markdown document (Glamour)
// edited in place, windowed to the page.
func demoChecklist(m *ochazuke.Model, doc *markdown.Doc, width, height int) ochazuke.Block {
	s := ochazuke.NewStyles(*m.Appearance)
	head := s.Muted.Render(ansi.Truncate("testdata/checklist.md · x toggles · i edits · o opens · n notes · u / ctrl+r undo and redo", width, "…")) + "\n"
	block := doc.Block(width, max(1, height-1), *m.Appearance)
	return block.Offset(0, 1).WithText(head + block.Text)
}

func demoOverview(m *ochazuke.Model, width int) string {
	s := ochazuke.NewStyles(*m.Appearance)
	var b strings.Builder
	b.WriteString(s.Title.Render("A framework. Your application.") + "\n")
	b.WriteString(s.Muted.Render("Pages keep their place. Actions share one interface.") + "\n\n")
	for _, page := range m.Runtime.PageList() {
		descriptions := map[string]string{"overview": "Start here: navigation and the framework's structure.", "playground": "Edit text, branch undo history, and open nested modals.", "commands": "Inspect real operations, argument schemas, and bindings.", "appearance": "Change the live theme and spacing defaults.", "task": "Edit a record written as you type: choices, undo, no save key.", "checklist": "A Markdown checklist: x toggles an item, saved back to its source."}
		fmt.Fprintf(&b, "%s\n  %s\n\n", s.Subtitle.Render(page.Title), s.Muted.Render(descriptions[page.ID]))
	}
	b.WriteString(s.Panel.Width(max(1, width-2)).Render("TRY IT\nCtrl-J → i → type → Escape → u\nCtrl-N opens a modal without replacing the page.\n: searches the same actions available through the CLI."))
	b.WriteString("\n\n" + s.Muted.Render("? keyboard guide · Ctrl-J/K switch pages · Ctrl-C quit"))
	return b.String()
}
func demoPlayground(m *ochazuke.Model, width, height int, lastNoun string) ochazuke.Block {
	r := m.Runtime
	s := ochazuke.NewStyles(*m.Appearance)
	focus := r.PageFocus("playground")
	// Only the focused field carries a cursor; the panel border, padding and
	// label row move it.
	field := func(id, label string, w, h int) ochazuke.Block {
		active := focus == id
		style := s.Panel
		if active {
			style = s.FocusedPanel
		}
		marker := "  "
		if active {
			marker = "› "
		}
		body := ochazuke.InputBlock(r.Input(id), max(1, w-2-2*s.Padding), h)
		if !active {
			body.Cursor = nil
		}
		return body.Offset(1+s.Padding, 2).WithText(style.Padding(0, s.Padding).Width(max(1, w-2)).Render(s.Subtitle.Render(marker+label) + "\n" + body.Text))
	}
	var fields ochazuke.Block
	if height < 18 {
		labels := map[string]string{"directory": "Directory · path", "notes": "Notes · text", "paths": "Paths · one per line"}
		id := focus
		fields = field(id, labels[id], width, max(1, height-9))
	} else if width >= 76 {
		left := width / 3
		right := width - left - 2
		directory := field("directory", "Directory · path", left, 1)
		paths := field("paths", "Paths · one per line", left, 3).Offset(0, lipgloss.Height(directory.Text))
		notes := field("notes", "Notes · text", right, 7).Offset(left+2, 0)
		fields = ochazuke.Block{Text: lipgloss.JoinHorizontal(lipgloss.Top, directory.Text+"\n"+paths.Text, "  ", notes.Text), Cursor: firstCursor(directory, paths, notes)}
	} else {
		directory := field("directory", "Directory · path", width, 1)
		notes := field("notes", "Notes · text", width, 2).Offset(0, lipgloss.Height(directory.Text))
		paths := field("paths", "Paths · one per line", width, 2).Offset(0, lipgloss.Height(directory.Text)+lipgloss.Height(notes.Text))
		fields = ochazuke.Block{Text: directory.Text + "\n" + notes.Text + "\n" + paths.Text, Cursor: firstCursor(directory, notes, paths)}
	}
	status := "App → Playground → Agent → " + strings.Title(focus)
	if lastNoun != "" {
		status = "Ctrl-X action handled by: " + lastNoun
	}
	// Title, subtitle and a blank row sit above the fields.
	return fields.Offset(0, 3).WithText(s.Title.Render("Widget playground") + "\n" + s.Muted.Render("Shared yanks and registers. Independent edit histories.") + "\n\n" + fields.Text + "\n\n" + s.Accent.Render(safeText(status)) + "\n" + s.Muted.Render("Tab field · i insert · v/V/C-v select · Ctrl-Y yank history\nCtrl-N draft · Ctrl-O path picker · Ctrl-X inspect"))
}
func demoCommands(m *ochazuke.Model, width int) string {
	r := m.Runtime
	s := ochazuke.NewStyles(*m.Appearance)
	var b strings.Builder
	b.WriteString(s.Title.Render("Commands & bindings") + "\n" + s.Muted.Render("Live registry · every action is available as CLI: NOUN VERB") + "\n\n")
	focus := r.PageFocus("playground")
	b.WriteString(s.Subtitle.Render("Remembered Playground bindings") + "\n")
	b.WriteString(s.Muted.Render(strings.Join(r.InstancePath(focus), " → ")+" · "+r.Mode.Name) + "\n")
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
func demoAppearance(m *ochazuke.Model, width int) string {
	s := ochazuke.NewStyles(*m.Appearance)
	accent := "theme"
	if m.Appearance.Accent != nil {
		accent = demoAccent
	}
	panel := s.Panel.Width(max(1, width-2))
	return s.Title.Render("Make it feel like your terminal") + "\n" + s.Muted.Render("These settings apply to every page and modal immediately.") + "\n\n" +
		panel.Render(s.Subtitle.Render("Theme   ")+s.Accent.Render(m.Appearance.Theme)+"\nAdaptive · Dark · Light · Mono\n\n"+s.Subtitle.Render("Density ")+s.Accent.Render(m.Appearance.Density)+"\nComfortable · Compact\n\n"+s.Subtitle.Render("Accent  ")+s.Accent.Render(accent)+"\nThe theme's, or an override (mono stays colourless)") + "\n\n" +
		s.Accent.Render("t") + " cycle theme    " + s.Accent.Render("d") + " toggle density    " + s.Accent.Render("a") + " toggle accent    " + s.Accent.Render("s") + " edit settings\n" +
		s.Muted.Render("Saved in settings.json as you change them: "+settingsPath(m.Runtime)) + "\n\n" +
		s.FocusedPanel.Width(max(1, width-2)).Render("Focused surface\n"+s.Muted.Render("Muted context stays readable.")+"  "+s.Badge.Render("NORMAL")) + "\n\n" +
		s.Muted.Render("Palette: settings set theme mono\nCLI: settings set theme mono")
}

// settingsPath is where the settings live, or why they do not persist.
func settingsPath(r *ochazuke.Runtime) string {
	if path := r.Config().Path(ochazuke.SettingsFile); path != "" {
		return path
	}
	return "(no config directory: this session only)"
}

// libraryEntries are the Library page's rows and the long text each one's
// detail shows.
var libraryEntries = []struct{ id, title, text string }{
	{"pages", "Pages", "A page is an instance under app. It remembers its focus and mode while another page is open, and its renderer draws the body for the cell bounds it is given."},
	{"lists", "Lists", "List rows are instances of their nouns. The focused row's noun decides what keys mean, so a list of tasks and a list of files answer different verbs to the same key."},
	{"modals", "Modals", "A modal opens over the focus and gives it back when it closes. Keys inside a modal never reach the page beneath it, and the page stays drawn, dimmed, behind it."},
	{"details", "Details", "A detail is the text beside a list about its selected row. J and K scroll it a line while the list keeps the focus; counts work, so 10J moves ten lines. A new row starts at the top."},
}

func libraryItems() []ochazuke.ListItem {
	items := make([]ochazuke.ListItem, 0, len(libraryEntries))
	for _, e := range libraryEntries {
		items = append(items, ochazuke.ListItem{ID: e.id, Label: e.title, Noun: "entry"})
	}
	return items
}

// demoCompletions are the notes field's completion triggers: @ names a
// library entry, $ a skill and [[ a page. Each search runs off the loop,
// so a slow one (a file index, a daemon) would never stall typing.
func demoCompletions() []ochazuke.CompletionTrigger {
	matching := func(query, text string) bool {
		return strings.Contains(strings.ToLower(text), strings.ToLower(query))
	}
	entries := func(_ context.Context, query string) ([]ochazuke.Suggestion, error) {
		var out []ochazuke.Suggestion
		for _, e := range libraryEntries {
			if matching(query, e.id) || matching(query, e.title) {
				out = append(out, ochazuke.Suggestion{Text: "library/" + e.id, Detail: e.title})
			}
		}
		return out, nil
	}
	skills := ochazuke.SearchStrings(func(_ context.Context, query string) ([]string, error) {
		var out []string
		for _, skill := range []string{"review", "summarize", "translate", "deploy", "explain"} {
			if matching(query, skill) {
				out = append(out, skill)
			}
		}
		return out, nil
	})
	pages := ochazuke.SearchStrings(func(_ context.Context, query string) ([]string, error) {
		var out []string
		for _, title := range []string{"Overview", "Playground", "Commands", "Appearance", "Library", "Record", "Checklist"} {
			if matching(query, title) {
				out = append(out, title+"]]")
			}
		}
		return out, nil
	})
	return []ochazuke.CompletionTrigger{{Trigger: "@", Search: entries}, {Trigger: "$", Search: skills}, {Trigger: "[[", Search: pages}}
}

// libraryText is a long detail: the entry's summary, then numbered notes,
// so every entry overflows a terminal-sized panel.
func libraryText(id string) string {
	for _, e := range libraryEntries {
		if e.id != id {
			continue
		}
		var b strings.Builder
		b.WriteString(strings.ToUpper(e.title) + "\n\n" + e.text + "\n")
		for i := 1; i <= 60; i++ {
			fmt.Fprintf(&b, "\n%2d. %s note %d.", i, e.title, i)
		}
		return b.String()
	}
	return ""
}

// demoLibrary draws the entries on the left and the selected entry's
// detail on the right (stacked below 72 columns), the detail's position
// marker in its panel title when it overflows. The divider follows the
// page's pane keys (> and <, = resets) through PaneLayout and Divide.
func demoLibrary(m *ochazuke.Model, entries *ochazuke.List, detail *ochazuke.Detail, width, height int) ochazuke.Block {
	layout := m.Runtime.PaneLayout("library", ochazuke.Layout{Styles: ochazuke.NewStyles(*m.Appearance)})
	listPanel := func(w, h int) string {
		cw, ch := ochazuke.PanelContentSize(w, h)
		return layout.Panel("Entries", entries.View(cw, ch), w, h, entries.Focused())
	}
	detailPanel := func(w, h int) string {
		cw, ch := ochazuke.PanelContentSize(w, h)
		selected, _ := entries.Selected()
		body, marker := detail.Window(libraryText(selected.ID), cw, ch)
		title := "Detail"
		if marker != "" {
			title += " · " + marker
		}
		return layout.Panel(title, body, w, h, false)
	}
	if width >= 72 {
		left := layout.Divide((width-1)*2/5, width-1, ochazuke.PaneMinWidth)
		return ochazuke.Text(lipgloss.JoinHorizontal(lipgloss.Top, listPanel(left, height), " ", detailPanel(width-left-1, height)))
	}
	top := layout.Divide(min(len(libraryEntries)+2, height/2), height, ochazuke.PaneMinHeight)
	return ochazuke.Text(listPanel(width, top) + "\n" + detailPanel(width, height-top))
}

// demoAccent is the override the Appearance page toggles: the orange an
// application might use for "showing everything".
const demoAccent = "#E8833A"

// focusOf is the instance a handler context addresses.
func focusOf(c ochazuke.Context) string {
	if c.InputID != "" {
		return c.InputID
	}
	if len(c.Path) == 0 {
		return ""
	}
	return c.Path[len(c.Path)-1]
}

// firstCursor returns the first block cursor among candidates, each already
// offset into the composer's coordinates.
func firstCursor(blocks ...ochazuke.Block) *ochazuke.Pos {
	for _, b := range blocks {
		if b.Cursor != nil {
			return b.Cursor
		}
	}
	return nil
}

// safeText keeps newlines, turns tabs into spaces and replaces other control
// characters so application text cannot inject terminal sequences.
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return '\uFFFD'
		}
		return r
	}, s)
}
