// Command widgets shows every Tier 2 widget live in five tabs: Table,
// Form, Paths, Vim input and Bindings. Each tab is a core page, so it keeps
// its own focus and mode. The footer lists the keys of the current context
// and ? opens contextual help built by widgets/bindings.
//
//	go run ./examples/widgets           interactive
//	go run ./examples/widgets --smoke   drive every tab headlessly in a temporary folder, exit 0 on success
//
// tab and shift+tab or alt+1 to alt+5 switch tabs in every mode. d cycles
// density, t cycles the theme presets (each brings its own density and
// border style), ? shows help, q quits.
package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/layout"
	otea "ofam.dev/ochazuke/tea"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/bindings"
	"ofam.dev/ochazuke/widgets/form"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/pathpicker"
	"ofam.dev/ochazuke/widgets/table"
)

// tab is one page: its ID, label, the node focused on entry and its mode.
type tab struct{ id, title, focus, mode string }

var tabs = []tab{
	{"table", "1 Table", "table", "normal"},
	{"form", "2 Form", "form", "normal"},
	{"paths", "3 Paths", "paths", "normal"},
	{"vim", "4 Vim input", "vim", "vim"},
	{"keys", "5 Bindings", "keys.table", "normal"},
}

var modes = []core.Mode{
	{Name: "normal"}, {Name: "modal"},
	{Name: "table.filter"}, {Name: "form.edit"}, {Name: "paths.query"}, {Name: "paths.mkdir"},
	{Name: "vim", Passthrough: true}, {Name: "keys.filter"}, {Name: "keys.capture", Passthrough: true},
}

func k(keys ...string) []string { return keys }

// appBindings are the global keys. They are editable on the Bindings tab.
func appBindings() []core.Binding {
	bs := []core.Binding{
		{Keys: k("tab"), Command: core.CmdPageNext, Hint: "next tab"},
		{Keys: k("shift+tab"), Command: core.CmdPagePrev, Hidden: true},
		{Mode: "normal", Keys: k("?"), Command: "app.help", Hint: "help"},
		{Mode: "normal", Keys: k("d"), Command: "app.density.cycle", Hint: "density"},
		{Mode: "normal", Keys: k("t"), Command: "app.theme.cycle", Hint: "theme"},
		{Mode: "normal", Keys: k("q"), Command: core.CmdAppQuit, Hint: "quit"},
		{Mode: "normal", Keys: k("j"), Command: "motion.down", Hint: "down"},
		{Mode: "normal", Keys: k("k"), Command: "motion.up", Hint: "up"},
		{Mode: "normal", Keys: k("G"), Command: "motion.bottom", Hint: "bottom"},
		{Mode: "normal", Keys: k("ctrl+d"), Command: "motion.half.down", Hidden: true},
		{Mode: "normal", Keys: k("ctrl+u"), Command: "motion.half.up", Hidden: true},
		{Mode: "normal", Class: core.ClassDigit, Command: core.CmdCountPush, Args: map[string]any{"digit": "$key"}, Hidden: true},
	}
	for i, t := range tabs {
		bs = append(bs, core.Binding{Keys: k(fmt.Sprintf("alt+%d", i+1)), Command: core.CmdPageOpen, Args: map[string]any{"id": t.id}, Hidden: true})
	}
	// g alone groups the table, so g g only means top on the other tabs.
	for _, scope := range []string{"tab.form", "tab.paths", "tab.keys"} {
		bs = append(bs, core.Binding{Scope: scope, Mode: "normal", Keys: k("g", "g"), Command: "motion.top", Hidden: true})
	}
	return bs
}

// fixedBindings drive the Bindings tab and the help modal. They stay out of
// the editor so an edit can never lock the editor itself.
func fixedBindings() []core.Binding {
	return slices.Concat(keysBindings(), []core.Binding{
		{Mode: "modal", Keys: k("esc"), Command: core.CmdModalClose, Hint: "close"},
		{Mode: "modal", Keys: k("?"), Command: core.CmdModalClose, Hidden: true},
		{Mode: "modal", Keys: k("q"), Command: core.CmdModalClose, Hidden: true},
	})
}

// specsFor registers every app command a binding names, so the editor's
// analysis knows them. Built-ins and motions come from core.DefaultRegistry.
func specsFor(bs []core.Binding) []core.CommandSpec {
	reg := core.DefaultRegistry()
	var out []core.CommandSpec
	for _, b := range bs {
		if _, ok := reg.Lookup(b.Command); b.Command != "" && !ok {
			desc := b.Hint
			if desc == "" {
				desc = b.Command
			}
			spec := core.CommandSpec{ID: b.Command, Description: desc}
			reg.Register(spec)
			out = append(out, spec)
		}
	}
	return out
}

type app struct {
	sh    *otea.Shell
	eng   *core.Engine
	specs []core.CommandSpec
	fixed []core.Binding
	keys  *bindings.Model // live editor over every binding except fixed

	table *table.Model
	form  *form.Model
	paths *pathpicker.Model
	vim   *input.Model

	themes []ui.Theme
	mkdir  string // folder name typed after +
	note   string // last action, shown in the status line
	kmErr  error  // keymap compile error after an edit
}

// options are the parts smoke replaces: the file system and its roots.
type options struct {
	fs    pathpicker.FS
	start string
	roots []pathpicker.Root
}

func newApp(o options) (*app, error) {
	editable := slices.Concat(appBindings(), tableBindings(), formBindings(), pathBindings())
	a := &app{fixed: fixedBindings(), themes: ui.ThemePresets()}
	a.specs = specsFor(slices.Concat(editable, a.fixed))
	a.keys = bindings.New(modes, editable, a.specs...)
	a.table = newTable()
	a.form = newForm()
	a.vim = input.New(vimText, true)
	var err error
	if a.paths, err = pathpicker.New(o.fs, o.start, pathpicker.PickAny); err != nil {
		return nil, err
	}
	a.paths.Roots = o.roots

	a.eng = core.NewEngine(nil)
	a.applyKeys()
	a.handlers()
	pages := make([]core.Frame, len(tabs))
	for i, t := range tabs {
		pages[i] = core.Frame{ID: t.id, FocusID: t.focus, Mode: t.mode}
	}
	th := ui.DefaultDarkTheme()
	if len(a.themes) > 0 {
		th = a.themes[0]
	}
	th.Density = ui.DensityBalanced
	a.sh = otea.New(a.eng, core.NewState(pages...), a.view, th)
	a.sh.OnPassThrough = a.passThrough
	a.sh.OnCustom = a.applyTheme
	return a, nil
}

// all is the live binding set: editor rows first, then the fixed rows.
func (a *app) all() []core.Binding { return slices.Concat(a.keys.Bindings(), a.fixed) }

// applyKeys recompiles the engine keymap from the editor, so an edit is live
// on the next key. NewKeymap keeps the valid rows when one row fails.
func (a *app) applyKeys() {
	km, err := core.NewKeymap(modes, a.all(), a.specs...)
	a.eng.Keymap, a.kmErr = km, err
}

// route sends commands in a tab scope to a widget.
func (a *app) route(scope string, fn func(core.State, core.Command) core.State, ids ...string) {
	for _, id := range ids {
		a.eng.HandleScope(scope, id, func(s core.State, c core.Command) core.StepResult { return core.Next(fn(s, c)) })
	}
}

var motions = []string{"motion.down", "motion.up", "motion.top", "motion.bottom", "motion.half.down", "motion.half.up"}

func setMode(s core.State, mode string) core.State {
	f := s.ActiveFrame()
	f.Mode = mode
	return s.WithActiveFrame(f)
}

func (a *app) handlers() {
	a.tableHandlers()
	a.formHandlers()
	a.pathHandlers()
	a.vimHandlers()
	a.keysHandlers()
	a.eng.Handle("app.density.cycle", func(s core.State, _ core.Command) core.StepResult {
		th := a.sh.Theme
		switch th.Density {
		case ui.DensityCompact:
			th.Density = ui.DensityBalanced
		case ui.DensityBalanced:
			th.Density = ui.DensitySpacious
		default:
			th.Density = ui.DensityCompact
		}
		return core.Next(s, core.CustomEffect{Kind: "theme", Data: th})
	})
	a.eng.Handle("app.theme.cycle", func(s core.State, _ core.Command) core.StepResult {
		if len(a.themes) == 0 {
			return core.Next(s)
		}
		i := slices.IndexFunc(a.themes, func(t ui.Theme) bool { return t.Name == a.sh.Theme.Name })
		return core.Next(s, core.CustomEffect{Kind: "theme", Data: a.themes[(i+1)%len(a.themes)]})
	})
	a.eng.Handle("app.help", a.openHelp)
}

// applyTheme performs the "theme" effect. Name, Version, Density and
// BorderStyle key the renderer cache, so no version bump is needed.
func (a *app) applyTheme(e core.CustomEffect) tea.Cmd {
	if th, ok := e.Data.(ui.Theme); ok && e.Kind == "theme" {
		a.sh.Theme = th
	}
	return nil
}

// openHelp captures the keys that resolve here, before the modal opens.
func (a *app) openHelp(s core.State, _ core.Command) core.StepResult {
	q := core.KeyQuery{Chain: ui.ContextChain(a.view(s), s.ActiveFrame().FocusID, nil), Mode: a.eng.Mode(s), Input: s.Input}
	help := bindings.New(modes, a.all(), a.specs...).HelpView("help.table", q)
	s.Modals = append(slices.Clone(s.Modals), core.Frame{ID: "help", Kind: core.FrameModal, Mode: "modal", FocusID: "help.table", Data: help})
	return core.Next(s)
}

// passThrough turns keys the keymap left alone into commands for the
// passthrough modes: the vim editor and key capture on the Bindings tab.
func (a *app) passThrough(e core.PassToWidgetEffect) tea.Cmd {
	var id string
	switch {
	case e.Mouse != nil:
		return nil
	case e.NodeID == "vim":
		id = "vim.key"
	case e.NodeID == "keys.table":
		id = "keys.rebind"
	default:
		return nil
	}
	return func() tea.Msg {
		return core.CommandEvent{Command: core.Command{ID: id, Args: map[string]any{"key": e.Key, "text": e.Text}}}
	}
}

// ---- view -------------------------------------------------------------------

func text(t, tone string) ui.ViewNode {
	return ui.ViewNode{Kind: ui.KindText, Tone: tone, Props: map[string]any{"text": t}}
}

func (a *app) view(s core.State) ui.ViewNode {
	active := slices.IndexFunc(tabs, func(t tab) bool { return t.id == s.ActivePage })
	strip := ui.ViewNode{ID: "tabs", Kind: ui.KindTabs, Box: flex1, Props: map[string]any{"active": active}}
	for i, t := range tabs {
		body := ui.ViewNode{ID: "tab." + t.id + ".idle", Kind: ui.KindText}
		if i == active {
			body = a.tabView(t.id, s)
		}
		body.Title = t.title
		strip.Children = append(strip.Children, body)
	}
	root := ui.ViewNode{Kind: ui.KindVStack, Children: []ui.ViewNode{strip}}
	if n := len(s.Modals); n > 0 {
		root.Children = append(root.Children, a.helpOverlay(s.Modals[n-1]))
	}
	chain := ui.ContextChain(root, s.ActiveFrame().FocusID, nil)
	root.Children = append(root.Children, a.footer(s, chain)...)
	return root
}

var flex1 = layout.Constraints{Flex: 1}

func (a *app) tabView(id string, s core.State) ui.ViewNode {
	var kids []ui.ViewNode
	switch id {
	case "table":
		kids = a.tableView(s)
	case "form":
		kids = a.formView(s)
	case "paths":
		kids = a.pathsView(s)
	case "vim":
		kids = a.vimView()
	case "keys":
		kids = a.keysView(s)
	}
	return ui.ViewNode{ID: "tab." + id, Kind: ui.KindVStack, Scope: "tab." + id, Box: flex1, Children: kids}
}

func (a *app) helpOverlay(f core.Frame) ui.ViewNode {
	help, _ := f.Data.(ui.ViewNode)
	help.Box = flex1
	return ui.ViewNode{ID: "help", Kind: ui.KindOverlay, Scope: "modal", Title: "Keys that resolve here · esc closes",
		Props: map[string]any{"widthPct": 80, "heightPct": 70}, Children: []ui.ViewNode{help}}
}

// footer shows the status line and the hints of the focused context.
func (a *app) footer(s core.State, chain []core.ContextFrame) []ui.ViewNode {
	scopes := make([]string, len(chain))
	for i, c := range chain {
		scopes[i] = c.Scope
	}
	mode := a.eng.Mode(s)
	active := a.eng.Keymap.ModeChain(mode)
	var local, general []string
	seen := map[string]bool{}
	for _, b := range a.all() {
		if b.Hint == "" || b.Hidden || b.Unbind || len(b.Keys) == 0 || b.Mode != "" && !slices.Contains(active, b.Mode) {
			continue
		}
		if b.Scope != "" && !slices.Contains(scopes, b.Scope) {
			continue
		}
		label := strings.Join(b.Keys, " ") + " " + b.Hint
		if seen[label] {
			continue
		}
		seen[label] = true
		if b.Scope != "" {
			local = append(local, label)
		} else {
			general = append(general, label)
		}
	}
	if m, _ := a.eng.Keymap.Mode(mode); m.Passthrough {
		local = append(local, "other keys go to the focused widget")
	}
	status := fmt.Sprintf(" theme %s · density %v · mode %s", a.sh.Theme.Name, a.sh.Theme.Density, mode)
	if a.note != "" {
		status += " · " + a.note
	}
	if a.kmErr != nil {
		status += " · keymap: " + a.kmErr.Error()
	}
	w := max(a.sh.W-1, 0)
	return []ui.ViewNode{
		text(clip(status, w), "accent"),
		text(clip(" "+strings.Join(local, " · "), w), "text"),
		text(clip(" "+strings.Join(general, " · ")+" · alt+1..5 tab", w), "muted"),
	}
}

func clip(s string, w int) string {
	if r := []rune(s); w > 0 && len(r) > w {
		return string(r[:w-1]) + "…"
	}
	return s
}

func main() {
	smoke := flag.Bool("smoke", false, "drive every tab headlessly and exit")
	flag.Parse()
	var err error
	if *smoke {
		err = runSmoke()
	} else {
		err = runLive()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "widgets:", err)
		os.Exit(1)
	}
}

func runLive() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	roots := []pathpicker.Root{{Label: "cwd", Path: wd}}
	if home != "" {
		roots = append(roots, pathpicker.Root{Label: "home", Path: home})
	}
	roots = append(roots, pathpicker.Root{Label: "tmp", Path: os.TempDir()}, pathpicker.Root{Label: "/", Path: "/"})
	a, err := newApp(options{start: wd, roots: roots})
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(a.sh).Run()
	return err
}
