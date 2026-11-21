// Command cards is a small Ochazuke v2 demo: a heterogeneous card grid driven
// end to end by core (modal engine), ui (view tree + renderer) and tea
// (Bubble Tea adapter).
//
//	go run ./examples/cards            interactive (alt screen)
//	go run ./examples/cards --smoke    scripted run without a TTY, exits 0 when every check holds
//	go run ./examples/cards --smoke --dump   also print the final frame as plain text
//
// Keys: h j k l move focus (a count works: 3j), n / shift+tab cycle, enter opens a
// detail modal (enter or i on the terminal card enters it), ? opens help,
// esc closes, ctrl+a is the leader (works inside the terminal card; ctrl+a r
// is a sticky resize layer, ctrl+a x leaves the terminal), q quits.
package main

import (
	"flag"
	"fmt"
	"hash/fnv"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/layout"
	otea "ofam.dev/ochazuke/tea"
	"ofam.dev/ochazuke/ui"
)

// ---- domain: one immutable value kept in the page frame's Data ----------------

type model struct {
	Done  map[string]bool
	Last  string
	ColW  int
	Lines []string // terminal history
	Typed string   // terminal input line
}

func pageModel(s core.State) model {
	m, _ := s.Pages["main"].Data.(model)
	return m
}

func withModel(s core.State, edit func(*model)) core.State {
	m := pageModel(s)
	m.Done = maps.Clone(m.Done)
	edit(&m)
	f := s.Pages["main"]
	f.Data = m
	s.Pages = maps.Clone(s.Pages)
	s.Pages["main"] = f
	return s
}

type cardDef struct {
	id, scope, kind, title, summary string
	badges                          []ui.Badge
	unread                          bool
}

var cardDefs = []cardDef{
	{"branch-cards", "git.branch", "git.branch", "feature/cards", "3 commits ahead of main", []ui.Badge{{Text: "↑3", Tone: "badge.success"}}, false},
	{"branch-leader", "git.branch", "git.branch", "fix/leader-layer", "rebase needed, 1 conflict", []ui.Badge{{Text: "conflict", Tone: "badge.error"}}, true},
	{"task-adapter", "task", "task", "Wire the Wave 2 adapter", "tea.Shell drives core + ui", []ui.Badge{{Text: "in progress", Tone: "badge.accent"}}, true},
	{"question-theme", "question", "question", "Which theme ships first?", "dark or light as the default", []ui.Badge{{Text: "needs input", Tone: "badge.warning"}}, true},
	{"task-docs", "task", "task", "Document the cache key", "ID + CacheKey + size + focus", nil, false},
	{"term", "terminal", "pane", "pane p1 (passthrough)", "", []ui.Badge{{Text: "pty", Tone: "badge"}}, false},
}

func cardByID(id string) (cardDef, bool) {
	i := slices.IndexFunc(cardDefs, func(c cardDef) bool { return c.id == id })
	if i < 0 {
		return cardDef{}, false
	}
	return cardDefs[i], true
}

// ---- keymap ---------------------------------------------------------------------

var (
	leader = core.Layer{Name: "leader", Modes: []string{"terminal-leader", "normal"}, ExcludeTags: []string{"motion", "editing"}}
	resize = core.Layer{Name: "resize", Modes: []string{"resize"}, Sticky: true}
)

func k(keys ...string) []string { return keys }

var bindings = []core.Binding{
	// normal mode, any card
	{Mode: "normal", Keys: k("h"), Command: core.CmdFocusLeft, Hint: "left"},
	{Mode: "normal", Keys: k("j"), Command: core.CmdFocusDown, Hint: "down"},
	{Mode: "normal", Keys: k("k"), Command: core.CmdFocusUp, Hint: "up"},
	{Mode: "normal", Keys: k("l"), Command: core.CmdFocusRight, Hint: "right"},
	{Mode: "normal", Keys: k("n"), Command: core.CmdFocusNext, Hint: "next"},
	{Mode: "normal", Keys: k("shift+tab"), Command: core.CmdFocusPrev, Hint: "prev"},
	{Mode: "normal", Class: core.ClassDigit, Command: core.CmdCountPush, Args: map[string]any{"digit": "$key"}, Hidden: true},
	{Mode: "normal", Keys: k("enter"), Command: core.CmdModalOpen, Args: map[string]any{"id": "detail", "mode": "modal", "focus": "detail", "data": "$subject.id"}, Hint: "detail"},
	{Mode: "normal", Keys: k("?"), Command: core.CmdModalOpen, Args: map[string]any{"id": "help", "mode": "modal", "focus": "help"}, Hint: "help"},
	{Mode: "normal", Keys: k("q"), Command: core.CmdAppQuit, Hint: "quit"},
	{Mode: "normal", Keys: k("ctrl+a"), Layer: &leader, Hint: "leader"},
	// one binding per scope: heterogeneous cards, one keymap
	{Scope: "git.branch", Mode: "normal", Keys: k("R"), Command: "git.branch.rebase", Hint: "rebase"},
	{Scope: "task", Mode: "normal", Keys: k("d"), When: "facts.unread", Command: "task.done", Hint: "done"},
	{Scope: "question", Mode: "normal", Keys: k("a"), Command: "question.answer", Hint: "answer"},
	{Scope: "terminal", Mode: "normal", Keys: k("enter"), Command: core.CmdModeSet, Args: map[string]any{"mode": "terminal"}, Hint: "open terminal"},
	{Scope: "terminal", Mode: "normal", Keys: k("i"), Command: core.CmdModeSet, Args: map[string]any{"mode": "terminal"}, Hidden: true},
	// terminal mode: everything else passes through to the pane
	{Mode: "terminal", Keys: k("ctrl+a"), Layer: &leader, Hint: "leader"},
	// reachable only through the leader layer
	{Mode: "terminal-leader", Keys: k("r"), Layer: &resize, Hint: "resize"},
	{Mode: "terminal-leader", Keys: k("x"), Command: core.CmdModeSet, Args: map[string]any{"mode": "normal"}, Hint: "leave terminal"},
	// sticky resize layer, left with esc
	{Mode: "resize", Keys: k("h"), Command: "grid.resize", Args: map[string]any{"delta": -4}, Hint: "narrower"},
	{Mode: "resize", Keys: k("l"), Command: "grid.resize", Args: map[string]any{"delta": 4}, Hint: "wider"},
	{Mode: "resize", Keys: k("esc"), Command: core.CmdLayerPop, Hint: "done"},
	// modal
	{Mode: "modal", Keys: k("esc"), Command: core.CmdModalClose, Hint: "close"},
	{Mode: "modal", Keys: k("enter"), Command: core.CmdModalClose, Hidden: true},
}

func newEngine() (*core.Engine, *core.Keymap, error) {
	tag := func(id string, tags ...string) core.CommandSpec {
		spec, ok := core.DefaultRegistry().Lookup(id)
		if !ok {
			spec = core.CommandSpec{ID: id, Description: id}
		}
		spec.Tags = append(slices.Clone(spec.Tags), tags...)
		return spec
	}
	km, err := core.NewKeymap(
		[]core.Mode{{Name: "normal"}, {Name: "terminal", Passthrough: true}, {Name: "terminal-leader"}, {Name: "resize"}, {Name: "modal"}},
		bindings,
		// Grid focus moves are motions, so the leader layer filters them out.
		tag(core.CmdFocusLeft, "motion"), tag(core.CmdFocusRight, "motion"),
		tag(core.CmdFocusUp, "motion"), tag(core.CmdFocusDown, "motion"),
		tag("task.done", "editing"), tag("question.answer", "editing"),
		tag("git.branch.rebase", "destructive"), tag("grid.resize", "layout"),
	)
	if err != nil {
		return nil, nil, err
	}
	e := core.NewEngine(km)

	note := func(s core.State, c core.Command, msg string) core.State {
		return withModel(s, func(m *model) { m.Last = msg })
	}
	target := func(c core.Command) string {
		if c.Target != nil {
			return c.Target.ID
		}
		return "-"
	}
	e.Handle("git.branch.rebase", func(s core.State, c core.Command) core.StepResult {
		return core.Next(note(s, c, "rebase "+target(c)))
	})
	e.Handle("task.done", func(s core.State, c core.Command) core.StepResult {
		return core.Next(withModel(s, func(m *model) {
			if m.Done == nil {
				m.Done = map[string]bool{}
			}
			m.Done[target(c)], m.Last = true, "done "+target(c)
		}))
	})
	e.Handle("question.answer", func(s core.State, c core.Command) core.StepResult {
		return core.Next(note(s, c, "answered "+target(c)))
	})
	e.Handle("grid.resize", func(s core.State, c core.Command) core.StepResult {
		d, _ := c.Args["delta"].(int)
		return core.Next(withModel(s, func(m *model) { m.ColW = max(16, min(m.ColW+d, 80)) }))
	})
	e.Handle("term.input", func(s core.State, c core.Command) core.StepResult {
		return core.Next(withModel(s, func(m *model) {
			switch key, text := c.Arg("key"), c.Arg("text"); {
			case key == "enter":
				m.Lines = append(slices.Clone(m.Lines), "$ "+m.Typed)
				if m.Typed != "" {
					m.Lines = append(m.Lines, "ran: "+m.Typed)
				}
				m.Typed = ""
				m.Lines = m.Lines[max(0, len(m.Lines)-40):]
			case key == "backspace":
				if r := []rune(m.Typed); len(r) > 0 {
					m.Typed = string(r[:len(r)-1])
				}
			case text != "":
				m.Typed += text
			}
		}))
	})
	return e, km, nil
}

// ---- view -----------------------------------------------------------------------

func hash(parts ...string) uint64 {
	h := fnv.New64a()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return h.Sum64() | 1 // never zero: zero disables caching
}

func text(t, tone string) ui.ViewNode {
	return ui.ViewNode{Kind: ui.KindText, Tone: tone, Props: map[string]any{"text": t}}
}

func card(d cardDef, m model, s core.State, eng *core.Engine) ui.ViewNode {
	n := ui.ViewNode{
		ID: d.id, Kind: ui.KindCard, Scope: d.scope, Ref: &core.Ref{Kind: d.kind, ID: d.id},
		Focusable: true, Title: d.title, Badges: d.badges, Unread: d.unread,
		Facts: map[string]any{"unread": d.unread},
		Props: map[string]any{"summary": d.summary},
	}
	if m.Done[d.id] {
		n.Unread, n.Facts["unread"] = false, false
		n.Badges = append(slices.Clone(n.Badges), ui.Badge{Text: "done", Tone: "badge.success"})
	}
	if d.id == "term" {
		live := eng.Mode(s) == "terminal"
		lines := slices.Clone(m.Lines[max(0, len(m.Lines)-4):])
		prompt := "$ " + m.Typed
		if live {
			prompt += "█"
		}
		lines = append(lines, prompt)
		n.Props = nil
		n.Children = []ui.ViewNode{{Kind: ui.KindTerminal, Props: map[string]any{"lines": lines}, Box: layout.Constraints{FixedH: 5}}}
		n.CacheKey = hash(append(lines, fmt.Sprint(live))...)
		return n
	}
	n.CacheKey = hash(d.id, fmt.Sprint(n.Unread, m.Done[d.id]))
	return n
}

func hints(km *core.Keymap, modeName string, layer *core.Layer, scopes []string, limit int) string {
	var modes []string
	if layer != nil {
		for _, name := range layer.Modes {
			modes = append(modes, km.ModeChain(name)...)
		}
	} else {
		modes = km.ModeChain(modeName)
	}
	var scoped, general []string
	seen := map[string]bool{}
	for _, b := range bindings {
		if b.Hint == "" || b.Hidden || len(b.Keys) == 0 || !slices.Contains(modes, b.Mode) {
			continue
		}
		if b.Scope != "" && !slices.Contains(scopes, b.Scope) {
			continue
		}
		if layer != nil && km.Commands.HasAnyTag(b.Command, layer.ExcludeTags) {
			continue
		}
		label := strings.Join(b.Keys, " ") + " " + b.Hint
		if seen[label] {
			continue
		}
		seen[label] = true
		if b.Scope != "" {
			scoped = append(scoped, label)
		} else {
			general = append(general, label)
		}
	}
	all := append(scoped, general...)
	if len(all) > limit {
		all = all[:limit]
	}
	return strings.Join(all, "  ·  ")
}

func clip(s string, w int) string {
	if r := []rune(s); w > 0 && len(r) > w {
		return string(r[:w-1]) + "…"
	}
	return s
}

func buildView(sh func() *otea.Shell, eng *core.Engine, km *core.Keymap) otea.ViewFunc {
	return func(s core.State) ui.ViewNode {
		m := pageModel(s)
		cards := make([]ui.ViewNode, len(cardDefs))
		for i, d := range cardDefs {
			cards[i] = card(d, m, s, eng)
		}
		body := ui.ViewNode{Kind: ui.KindVStack, Children: []ui.ViewNode{
			text(" ochazuke v2 · cards demo", "accent"),
			{Kind: ui.KindGrid, Gap: 1, MinColW: m.ColW, Box: layout.Constraints{Flex: 1}, Children: cards},
		}}
		if modal := s.Modals; len(modal) > 0 {
			body.Children = append(body.Children, overlay(modal[len(modal)-1]))
		}

		f := s.ActiveFrame()
		chain := ui.ContextChain(body, f.FocusID, nil)
		scopes := make([]string, len(chain))
		for i, c := range chain {
			scopes[i] = c.Scope
		}
		layerName := "-"
		if s.Input.Layer != nil {
			layerName = s.Input.Layer.Name
		}
		line := fmt.Sprintf(" Mode %s · Layer %s · Scope %s · Focus %s", eng.Mode(s), layerName, scopes[0], f.FocusID)
		if p := s.Input.Prefix; len(p) > 0 {
			line += " · pending " + strings.Join(p, " ")
		}
		if m.Last != "" {
			line += " · last: " + m.Last
		}
		hint := " " + hints(km, eng.Mode(s), s.Input.Layer, scopes, 9)
		if eng.Mode(s) == "terminal" && s.Input.Layer == nil {
			hint = " typing goes to the pane  ·  " + strings.TrimSpace(hint)
		}
		var st otea.FrameStats
		var frames int
		if shell := sh(); shell != nil {
			st, frames = shell.Stats, shell.Renders
		}
		stats := fmt.Sprintf(" step %dµs · render %dµs · cache %d hit / %d miss · frames %d", st.StepUS, st.RenderUS, st.CacheHits, st.CacheMisses, frames)
		w := 0
		if shell := sh(); shell != nil {
			w = shell.W - 1
		}
		body.Children = append(body.Children, text(clip(line, w), "accent"), text(clip(hint, w), "muted"), text(clip(stats, w), "dim"))
		return body
	}
}

func overlay(f core.Frame) ui.ViewNode {
	o := ui.ViewNode{ID: f.ID, Kind: ui.KindOverlay, Scope: "modal", Focusable: true, Props: map[string]any{"widthPct": 50}}
	switch f.ID {
	case "detail":
		id, _ := f.Data.(string)
		d, _ := cardByID(id)
		o.Title = "Detail · " + d.title
		o.Children = []ui.ViewNode{text(d.kind+" "+id, "accent"), text(d.summary, "text"), text("", ""), text("esc or enter closes", "muted")}
	default:
		o.Title = "Help"
		o.Children = []ui.ViewNode{
			text("h j k l  move (3j counts)    n  next    enter  detail", "text"),
			text("ctrl+a  leader (also inside the terminal card)", "text"),
			text("ctrl+a r  resize layer (h/l, esc leaves)    ctrl+a x  leave terminal", "text"),
			text("esc closes this", "muted"),
		}
	}
	return o
}

// ---- wiring ---------------------------------------------------------------------

func newShell() (*otea.Shell, error) {
	eng, km, err := newEngine()
	if err != nil {
		return nil, err
	}
	initial := core.NewState(core.Frame{ID: "main", FocusID: cardDefs[0].id, Mode: "normal", Data: model{ColW: 36}})
	var sh *otea.Shell
	sh = otea.New(eng, initial, buildView(func() *otea.Shell { return sh }, eng, km), ui.DefaultDarkTheme())
	// Keys the engine could not bind reach the terminal card as pure commands.
	sh.OnPassThrough = func(e core.PassToWidgetEffect) tea.Cmd {
		if e.NodeID != "term" {
			return nil
		}
		return func() tea.Msg {
			return core.CommandEvent{Command: core.Command{ID: "term.input", Args: map[string]any{"key": e.Key, "text": e.Text}}}
		}
	}
	return sh, nil
}

func main() {
	smoke := flag.Bool("smoke", false, "run a scripted session without a TTY and exit")
	dump := flag.Bool("dump", false, "with --smoke, print the final frame as plain text")
	flag.Parse()

	sh, err := newShell()
	if err == nil {
		if *smoke {
			err = runSmoke(sh, *dump)
		} else {
			_, err = tea.NewProgram(sh).Run()
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cards:", err)
		os.Exit(1)
	}
}

// ---- smoke ----------------------------------------------------------------------

type driver struct {
	sh    *otea.Shell
	fails []string
}

// send delivers msg and feeds back the messages its commands produce.
func (d *driver) send(msg tea.Msg) {
	_, cmd := d.sh.Update(msg)
	d.pump(cmd)
}

func (d *driver) pump(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()
	select {
	case m := <-out:
		switch m := m.(type) {
		case tea.BatchMsg:
			for _, c := range m {
				d.pump(c)
			}
		case tea.QuitMsg:
		default:
			d.send(m)
		}
	case <-time.After(40 * time.Millisecond): // long timers (prefix timeout) are not waited for
	}
}

func (d *driver) key(names ...string) {
	for _, n := range names {
		switch {
		case n == "esc":
			d.send(tea.KeyPressMsg{Code: tea.KeyEscape})
		case n == "enter":
			d.send(tea.KeyPressMsg{Code: tea.KeyEnter})
		case strings.HasPrefix(n, "ctrl+"):
			d.send(tea.KeyPressMsg{Code: rune(n[len("ctrl+")]), Mod: tea.ModCtrl})
		default:
			r := []rune(n)[0]
			d.send(tea.KeyPressMsg{Code: r, Text: n})
		}
	}
}

func (d *driver) check(name string, ok bool) {
	status := "ok  "
	if !ok {
		status = "FAIL"
		d.fails = append(d.fails, name)
	}
	f := d.sh.State.ActiveFrame()
	fmt.Printf("%s %-46s mode=%-9s layer=%-7s focus=%s\n", status, name, d.sh.Engine.Mode(d.sh.State), layerName(d.sh.State), f.FocusID)
}

func layerName(s core.State) string {
	if s.Input.Layer == nil {
		return "-"
	}
	return s.Input.Layer.Name
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:]*m|\x1b\]8;[^\a]*\a`)

func runSmoke(sh *otea.Shell, dump bool) error {
	d := &driver{sh: sh}
	d.send(tea.WindowSizeMsg{Width: 120, Height: 36})
	focus := func() string { return sh.State.ActiveFrame().FocusID }
	mode := func() string { return sh.Engine.Mode(sh.State) }

	d.check("initial frame rendered", sh.Renders == 1 && focus() == "branch-cards")
	d.key("l", "l")
	d.check("l l moves focus across the first row", focus() == "task-adapter")
	d.key("j")
	d.check("j moves down to the terminal card", focus() == "term")
	d.key("k", "h")
	d.check("k h: up then left", focus() == "branch-leader")
	d.key("d")
	d.check("task-only key d ignored on a git.branch card", sh.State.ActiveFrame().FocusID == "branch-leader" && len(pageModel(sh.State).Done) == 0)
	d.key("l", "d")
	d.check("d on a task card runs task.done (scope binding)", pageModel(sh.State).Done["task-adapter"])
	d.key("j")

	d.key("?")
	d.check("? opens the help modal", len(sh.State.Modals) == 1 && focus() == "help" && mode() == "modal")
	d.key("esc")
	d.check("esc closes it, focus and mode restored", len(sh.State.Modals) == 0 && focus() == "term" && mode() == "normal")
	d.key("enter")
	d.check("enter on the terminal card enters passthrough", mode() == "terminal")

	d.key("l", "s", "enter")
	m := pageModel(sh.State)
	d.check("typed keys pass through to the pane", slices.Contains(m.Lines, "$ ls") && slices.Contains(m.Lines, "ran: ls"))

	d.key("ctrl+a", "h")
	d.check("leader filters motion commands (ctrl+a h)", focus() == "term" && mode() == "terminal" && sh.State.Input.Layer == nil)
	d.key("ctrl+a")
	d.check("ctrl+a pushes the leader layer in passthrough", layerName(sh.State) == "leader")
	d.key("?")
	d.check("leader falls through to normal binding (?)", len(sh.State.Modals) == 1 && focus() == "help")
	d.key("esc")
	d.check("modal closed, terminal mode restored", len(sh.State.Modals) == 0 && mode() == "terminal" && focus() == "term")

	before := pageModel(sh.State).ColW
	d.key("ctrl+a", "r")
	d.check("ctrl+a r pushes the sticky resize layer", layerName(sh.State) == "resize")
	d.key("l", "l")
	d.check("resize layer stays active across keys", layerName(sh.State) == "resize" && pageModel(sh.State).ColW == before+8)
	d.key("esc")
	d.check("esc leaves the resize layer", layerName(sh.State) == "-")
	d.key("ctrl+a", "x")
	d.check("ctrl+a x leaves the terminal", mode() == "normal")

	d.sh.Update(tea.WindowSizeMsg{Width: 120, Height: 36}) // clean re-render: all cache hits
	v := sh.View()
	plain := ansiRe.ReplaceAllString(v.Content, "")
	rows := strings.Split(v.Content, "\n")
	d.check("frame is 120x36 with ANSI styling", len(rows) == 36 && strings.Contains(v.Content, "\x1b[") && v.AltScreen)
	d.check("footer shows mode, layer, scope and focus", strings.Contains(plain, "Mode normal") && strings.Contains(plain, "Layer -") && strings.Contains(plain, "Scope terminal") && strings.Contains(plain, "Focus term"))
	d.check("cards are cached across frames", sh.Stats.CacheHits > 0)
	fmt.Printf("\nframe: %d rows, %d bytes · step %dµs · render %dµs · cache %d hit / %d miss · %d renders\n",
		len(rows), len(v.Content), sh.Stats.StepUS, sh.Stats.RenderUS, sh.Stats.CacheHits, sh.Stats.CacheMisses, sh.Renders)
	if dump {
		for _, r := range strings.Split(plain, "\n") {
			fmt.Println(strings.TrimRight(r, " "))
		}
	}
	if len(d.fails) > 0 {
		return fmt.Errorf("%d smoke checks failed: %s", len(d.fails), strings.Join(d.fails, "; "))
	}
	fmt.Println("smoke: all checks passed")
	return nil
}
