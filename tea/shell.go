// Package otea is the Bubble Tea v2 adapter for Ochazuke: a thin Shell that
// turns Bubble Tea messages into core events, runs core effects as tea
// commands, and renders ui view trees with input-first frame coalescing.
//
// otea imports core, ui, buffer, layout, recorder and charm.land/bubbletea/v2 only.
package otea

import (
	"context"
	"os"
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/recorder"
	"ofam.dev/ochazuke/ui"
)

// ViewFunc builds the view tree for a state. It must be pure.
type ViewFunc func(s core.State) ui.ViewNode

// FrameStats describes the most recent work of a Shell.
type FrameStats struct {
	StepUS      int64 // last Engine.Step
	LayoutUS    int64 // last view-tree build (ViewFn); the layout solve is inside RenderUS
	RenderUS    int64 // last Renderer.Render (solve + draw)
	CacheHits   int   // subtree cache hits of the last render
	CacheMisses int   // subtree cache misses of the last render
}

// DefaultMaxFPS is the frame rate New gives background coalescing.
const DefaultMaxFPS = 60

// Shell is a tea.Model driving a core.Engine over a ui view tree.
type Shell struct {
	Engine   *core.Engine
	State    core.State
	ViewFn   ViewFunc
	Theme    ui.Theme
	Renderer *ui.Renderer
	Recorder *recorder.Recorder
	Stats    FrameStats
	MaxFPS   int // background frame rate; 0 renders every update immediately
	W, H     int

	// AppFacts supplies the facts of the outermost "app" context frame.
	AppFacts func(core.State) map[string]any
	// Effect hooks run inside Update when the engine emits the effect. A hook
	// may return a command (for example one yielding a core.CommandEvent).
	OnPassThrough func(core.PassToWidgetEffect) tea.Cmd
	OnEmit        func(core.Command) tea.Cmd
	OnCustom      func(core.CustomEffect) tea.Cmd
	// DisableMouse turns off mouse reporting in the view.
	DisableMouse bool

	// Observability: the last step, and how many real renders have run.
	LastTrace   core.StepTrace
	LastEffects []core.Effect
	Renders     int

	view         tea.View
	lastFrame    recorder.FrameSnapshot
	valid, dirty bool
	framePending bool
	gen          uint64
	slots        map[string]slot
}

type slot struct {
	gen    uint64
	cancel context.CancelFunc
}

// frameMsg is the coalesced frame tick.
type frameMsg struct{}

// asyncDone carries an async result back with the generation that started it.
type asyncDone struct {
	gen  uint64
	slot string
	ev   core.EffectResultEvent
}

// New returns a Shell with a bounded subtree cache and DefaultMaxFPS.
func New(engine *core.Engine, initial core.State, viewFn ViewFunc, theme ui.Theme) *Shell {
	return &Shell{
		Engine: engine, State: initial, ViewFn: viewFn, Theme: theme,
		Renderer: ui.NewRenderer(ui.DefaultCacheEntries),
		Recorder: recorder.New(recorder.DefaultOptions()),
		MaxFPS:   DefaultMaxFPS,
		slots:    map[string]slot{},
	}
}

// SetRecorder replaces the shell's session recorder. Passing nil disables
// recording without affecting Shell behavior.
func (sh *Shell) SetRecorder(r *recorder.Recorder) {
	sh.Recorder = r
	if r != nil && sh.W > 0 && sh.H > 0 {
		r.SetInitialSize(sh.W, sh.H)
	}
}

// EnableRecorder attaches a new enabled session recorder with opts, seeding
// Width and Height from the shell's current size when unset.
func (sh *Shell) EnableRecorder(opts recorder.Options) *recorder.Recorder {
	opts.Enabled = true
	if opts.Width <= 0 && opts.Height <= 0 && sh.W > 0 && sh.H > 0 {
		opts.Width, opts.Height = sh.W, sh.H
	}
	r := recorder.New(opts)
	sh.Recorder = r
	return r
}

// LastFrame returns the most recently rendered terminal frame cached by the
// shell's recorder (or by the shell itself if no recorder is attached).
func (sh *Shell) LastFrame() recorder.FrameSnapshot {
	if sh == nil {
		return recorder.FrameSnapshot{}
	}
	if sh.Recorder != nil {
		if snap := sh.Recorder.LastFrame(); !snap.Empty() {
			return snap
		}
	}
	return sh.lastFrame
}

// Init implements tea.Model.
func (sh *Shell) Init() tea.Cmd { return nil }

// Translate converts a Bubble Tea message into a core event. Any core.Event
// is accepted as is, so hosts can inject commands with a tea.Cmd.
func Translate(msg tea.Msg) (core.Event, bool) {
	switch m := msg.(type) {
	case core.Event:
		return m, true
	case tea.KeyPressMsg:
		k := tea.Key(m)
		return core.KeyEvent{Key: KeyName(k), Text: k.Text}, true
	case tea.PasteMsg:
		return core.PasteEvent{Text: m.Content}, true
	case tea.MouseClickMsg:
		return mouse(tea.Mouse(m), "press"), true
	case tea.MouseReleaseMsg:
		return mouse(tea.Mouse(m), "release"), true
	case tea.MouseMotionMsg:
		return mouse(tea.Mouse(m), "motion"), true
	case tea.MouseWheelMsg:
		// Wheel turns are motion, never "press": they must not move focus.
		return mouse(tea.Mouse(m), "motion"), true
	case tea.WindowSizeMsg:
		return core.ResizeEvent{W: m.Width, H: m.Height}, true
	}
	return nil, false
}

// KeyName returns the core key name of k: the typed text for printable keys
// (shift is already folded in: "G"), "space" for a space, otherwise the
// keystroke ("ctrl+a", "esc", "enter", "alt+x").
func KeyName(k tea.Key) string {
	if k.Text != "" && k.Mod&^tea.ModShift == 0 {
		if k.Text == " " {
			return "space"
		}
		return k.Text
	}
	return k.Keystroke()
}

func mouse(m tea.Mouse, action string) core.MouseEvent {
	b := ""
	switch m.Button {
	case tea.MouseLeft:
		b = "left"
	case tea.MouseMiddle:
		b = "middle"
	case tea.MouseRight:
		b = "right"
	case tea.MouseWheelUp:
		b = "wheelup"
	case tea.MouseWheelDown:
		b = "wheeldown"
	case tea.MouseWheelLeft:
		b = "wheelleft"
	case tea.MouseWheelRight:
		b = "wheelright"
	}
	return core.MouseEvent{X: m.X, Y: m.Y, Button: b, Action: action}
}

// Update implements tea.Model: translate, build the context chain, Step,
// perform effects, then render now (interactive input) or behind one frame
// tick (background results).
func (sh *Shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var ev core.Event
	switch m := msg.(type) {
	case frameMsg:
		sh.framePending = false
		if sh.dirty {
			sh.render()
		}
		return sh, nil
	case asyncDone:
		if !sh.finish(m) {
			return sh, nil // superseded by a newer effect in the same slot
		}
		ev = m.ev
	case tea.WindowSizeMsg:
		sh.W, sh.H = m.Width, m.Height
		if sh.Recorder != nil {
			sh.Recorder.SetInitialSize(m.Width, m.Height)
			sh.Recorder.Record(recorder.Event{Kind: recorder.KindResize, W: m.Width, H: m.Height})
		}
		ev, _ = Translate(msg)
	default:
		var ok bool
		if ev, ok = Translate(msg); !ok {
			return sh, nil
		}
		sh.recordInput(ev)
	}

	var chain []core.ContextFrame
	if sh.ViewFn != nil {
		chain = ui.ContextChain(sh.ViewFn(sh.State), sh.State.ActiveFrame().FocusID, sh.facts())
	}
	t0 := time.Now()
	res := sh.Engine.Step(sh.State, chain, ev)
	sh.Stats.StepUS = time.Since(t0).Microseconds()
	sh.State, sh.LastTrace, sh.LastEffects = res.State, res.Trace, res.Effects
	sh.dirty = true

	if sh.Recorder != nil && sh.Recorder.Enabled() && len(res.Trace.Commands) > 0 {
		mode := sh.Engine.Mode(res.State)
		page := res.State.ActivePage
		focusID := res.State.ActiveFrame().FocusID
		for _, ct := range res.Trace.Commands {
			sh.Recorder.Record(recorder.Event{
				Kind:    recorder.KindCommand,
				Command: ct.Command.ID,
				Source:  ct.Command.Source,
				Handler: ct.Handler,
				Mode:    mode,
				Page:    page,
				FocusID: focusID,
			})
		}
	}

	cmds := sh.perform(res.Effects)
	if _, background := ev.(core.EffectResultEvent); !background || sh.MaxFPS <= 0 {
		sh.render()
	} else if !sh.framePending {
		sh.framePending = true
		cmds = append(cmds, tea.Tick(time.Second/time.Duration(sh.MaxFPS), func(time.Time) tea.Msg { return frameMsg{} }))
	}
	return sh, tea.Batch(cmds...)
}

func (sh *Shell) recordInput(ev core.Event) {
	if sh.Recorder == nil || !sh.Recorder.Enabled() {
		return
	}
	switch e := ev.(type) {
	case core.KeyEvent:
		sh.Recorder.Record(recorder.Event{Kind: recorder.KindKey, Key: e.Key, Text: e.Text})
	case core.PasteEvent:
		sh.Recorder.Record(recorder.Event{Kind: recorder.KindPaste, Text: e.Text})
	case core.MouseEvent:
		if e.Action == "motion" && e.Button == "" {
			return
		}
		sh.Recorder.Record(recorder.Event{
			Kind:   recorder.KindMouse,
			Button: e.Button,
			Action: e.Action,
			X:      e.X,
			Y:      e.Y,
		})
	}
}

func (sh *Shell) facts() map[string]any {
	if sh.AppFacts == nil {
		return nil
	}
	return sh.AppFacts(sh.State)
}

// View implements tea.Model. It returns the cached view; only the first call,
// before any render, draws.
func (sh *Shell) View() tea.View {
	if !sh.valid {
		sh.render()
	}
	return sh.view
}

func (sh *Shell) render() {
	t0 := time.Now()
	var tree ui.ViewNode
	if sh.ViewFn != nil {
		tree = sh.ViewFn(sh.State)
	}
	t1 := time.Now()
	sh.Stats.LayoutUS = t1.Sub(t0).Microseconds()

	var frame ui.Frame
	if sh.W > 0 && sh.H > 0 {
		if sh.Renderer == nil {
			sh.Renderer = ui.NewRenderer(ui.DefaultCacheEntries)
		}
		fr, solved := sh.Renderer.Render(tree, sh.Theme, sh.State.ActiveFrame().FocusID, sh.W, sh.H)
		frame, sh.State.Layout = fr, solved
		sh.Stats.CacheHits, sh.Stats.CacheMisses = sh.Renderer.Stats.CacheHits, sh.Renderer.Stats.CacheMisses
		sh.lastFrame = recorder.FrameSnapshot{W: sh.W, H: sh.H, ANSI: frame.ANSI}
		if sh.Recorder != nil {
			sh.Recorder.RecordFrame(sh.W, sh.H, frame.ANSI)
		}
	}
	sh.Stats.RenderUS = time.Since(t1).Microseconds()

	v := tea.NewView(frame.ANSI)
	v.AltScreen = true
	if !sh.DisableMouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	if c := frame.Cursor; c.Shape != buffer.CursorHidden {
		v.Cursor = &tea.Cursor{Position: tea.Position{X: c.X, Y: c.Y}, Shape: cursorShape(c.Shape)}
	}
	sh.view, sh.valid, sh.dirty = v, true, false
	sh.Renders++
}

func cursorShape(s buffer.CursorShape) tea.CursorShape {
	switch s {
	case buffer.CursorBar:
		return tea.CursorBar
	case buffer.CursorUnderline:
		return tea.CursorUnderline
	}
	return tea.CursorBlock
}

// perform turns effects into commands. Hook-handled effects are never
// dropped silently: they stay in LastEffects either way.
func (sh *Shell) perform(effects []core.Effect) []tea.Cmd {
	var cmds []tea.Cmd
	add := func(c tea.Cmd) {
		if c != nil {
			cmds = append(cmds, c)
		}
	}
	for _, e := range effects {
		switch e := e.(type) {
		case core.TimeoutEffect:
			add(tea.Tick(e.After, func(time.Time) tea.Msg { return core.TimeoutEvent{Token: e.Token} }))
		case core.AsyncEffect:
			add(sh.async(e))
		case core.ExecEffect:
			add(execCmd(e))
		case core.QuitEffect:
			sh.cancelAll()
			add(tea.Quit)
		case core.PassToWidgetEffect:
			if sh.OnPassThrough != nil {
				add(sh.OnPassThrough(e))
			}
		case core.EmitEffect:
			if sh.OnEmit != nil {
				add(sh.OnEmit(e.Command))
			}
		case core.CustomEffect:
			if sh.OnCustom != nil {
				add(sh.OnCustom(e))
			}
		}
	}
	return cmds
}

// async starts e on its own goroutine. A newer effect in the same non-empty
// slot cancels the older one's context and its result is dropped.
func (sh *Shell) async(e core.AsyncEffect) tea.Cmd {
	if e.Run == nil {
		return nil
	}
	if sh.slots == nil {
		sh.slots = map[string]slot{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	sh.gen++
	gen := sh.gen
	if e.Slot != "" {
		if old, ok := sh.slots[e.Slot]; ok {
			old.cancel()
		}
		sh.slots[e.Slot] = slot{gen: gen, cancel: cancel}
	}
	return func() tea.Msg {
		v, err := e.Run(ctx)
		if e.Slot == "" {
			cancel()
		}
		return asyncDone{gen: gen, slot: e.Slot, ev: core.EffectResultEvent{Slot: e.Slot, Command: e.Command, Value: v, Err: err}}
	}
}

// finish releases the slot of a completed async effect and reports whether
// its result is still current.
func (sh *Shell) finish(m asyncDone) bool {
	if m.slot == "" {
		return true
	}
	cur, ok := sh.slots[m.slot]
	if !ok || cur.gen != m.gen {
		return false
	}
	cur.cancel()
	delete(sh.slots, m.slot)
	return true
}

func (sh *Shell) cancelAll() {
	for k, s := range sh.slots {
		s.cancel()
		delete(sh.slots, k)
	}
}

func execCmd(e core.ExecEffect) tea.Cmd {
	c := exec.Command(e.Name, e.Args...)
	c.Dir = e.Dir
	if len(e.Env) > 0 {
		c.Env = append(os.Environ(), e.Env...)
	}
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return core.EffectResultEvent{Slot: e.Slot, Command: e.Command, Err: err}
	})
}
