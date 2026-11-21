package core

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"ofam.dev/ochazuke/layout"
)

// FrameKind distinguishes pages from modals.
type FrameKind uint8

// Frame kinds.
const (
	FramePage FrameKind = iota
	FrameModal
)

// Frame is one page or modal with its own focus and mode.
type Frame struct {
	ID      string    `json:"id"`
	Kind    FrameKind `json:"kind"`
	FocusID string    `json:"focusId"`
	Mode    string    `json:"mode"`           // "" = Engine.DefaultMode
	Data    any       `json:"data,omitempty"` // local UI state (scroll offsets, picker query, form draft)
}

// InputState is the pending key state.
type InputState struct {
	Prefix   []string `json:"prefix,omitempty"`
	Fallback *Command `json:"fallback,omitempty"` // fires when an ambiguous prefix times out or breaks
	Count    *int     `json:"count,omitempty"`
	Register rune     `json:"register,omitempty"`
	Operator *Command `json:"operator,omitempty"`
	Layer    *Layer   `json:"layer,omitempty"`
	Seq      uint64   `json:"seq,omitempty"` // bumped on every Pending; matches TimeoutEvent.Token
}

// Idle reports whether nothing is pending (cancel would be a no-op).
func (in InputState) Idle() bool {
	return len(in.Prefix) == 0 && in.Layer == nil && in.Count == nil && in.Operator == nil
}

func (in *InputState) popLayer() {
	if in.Layer != nil && !in.Layer.Sticky {
		in.Layer = nil
	}
}

// State is the whole modal UI state. Treat it as a value: Engine never
// mutates the maps or slices of a State it was given.
type State struct {
	Pages      map[string]Frame `json:"pages"`
	PageOrder  []string         `json:"pageOrder,omitempty"` // page cycling order (missing pages follow, sorted)
	ActivePage string           `json:"activePage"`
	Modals     []Frame          `json:"modals,omitempty"`
	Input      InputState       `json:"input"`
	Width      int              `json:"width,omitempty"`
	Height     int              `json:"height,omitempty"`
	Layout     layout.Solved    `json:"-"` // last solved geometry, set by the adapter
}

// NewState returns a State with pages in order; the first is active.
func NewState(pages ...Frame) State {
	s := State{Pages: map[string]Frame{}}
	for _, p := range pages {
		p.Kind = FramePage
		s.Pages[p.ID] = p
		s.PageOrder = append(s.PageOrder, p.ID)
	}
	if len(pages) > 0 {
		s.ActivePage = pages[0].ID
	}
	return s
}

// ActiveFrame returns the top modal, or the active page.
func (s State) ActiveFrame() Frame {
	if n := len(s.Modals); n > 0 {
		return s.Modals[n-1]
	}
	return s.Pages[s.ActivePage]
}

// WithActiveFrame returns s with the active frame replaced (copy-on-write).
func (s State) WithActiveFrame(f Frame) State {
	if n := len(s.Modals); n > 0 {
		s.Modals = slices.Clone(s.Modals)
		s.Modals[n-1] = f
		return s
	}
	s.Pages = maps.Clone(s.Pages)
	if s.Pages == nil {
		s.Pages = map[string]Frame{}
	}
	s.Pages[s.ActivePage] = f
	return s
}

// Event is an input to Engine.Step.
type Event interface{ isEvent() }

// KeyEvent is a key press. Key is the normalized name ("j", "ctrl+a",
// "space", "esc"); Text is the inserted text, if any.
type KeyEvent struct{ Key, Text string }

// PasteEvent is bracketed paste; it resolves as PasteKey.
type PasteEvent struct{ Text string }

// MouseEvent is a mouse action at screen (or, inside PassToWidgetEffect,
// node-local) coordinates.
type MouseEvent struct {
	X, Y   int
	Button string // "left", "right", "middle", "wheelup", "wheeldown", ""
	Action string // "press", "release", "motion"
}

// ResizeEvent reports a new terminal size and, optionally, solved layout.
type ResizeEvent struct {
	W, H   int
	Layout *layout.Solved
}

// TimeoutEvent reports that a TimeoutEffect elapsed.
type TimeoutEvent struct{ Token uint64 }

// EffectResultEvent carries the result of an ExecEffect or AsyncEffect. When
// Command is set, it is dispatched with Args {"slot", "value", "error"}.
type EffectResultEvent struct {
	Slot    string
	Command string
	Value   any
	Err     error
}

// CommandEvent dispatches a command directly (palette, CLI, MCP).
type CommandEvent struct{ Command Command }

func (KeyEvent) isEvent()          {}
func (PasteEvent) isEvent()        {}
func (MouseEvent) isEvent()        {}
func (ResizeEvent) isEvent()       {}
func (TimeoutEvent) isEvent()      {}
func (EffectResultEvent) isEvent() {}
func (CommandEvent) isEvent()      {}

// Effect is work for the adapter to perform after Step.
type Effect interface{ isEffect() }

// TimeoutEffect asks for TimeoutEvent{Token} after After.
type TimeoutEffect struct {
	Token uint64
	After time.Duration
}

// ExecEffect runs a foreground process (the adapter suspends the UI). Its
// result returns as EffectResultEvent{Slot, Command}.
type ExecEffect struct {
	Name    string
	Args    []string
	Dir     string
	Env     []string
	Slot    string
	Command string
}

// AsyncEffect runs Run off the UI loop. A newer effect in the same Slot
// cancels the older one. Its result returns as EffectResultEvent.
type AsyncEffect struct {
	Slot    string
	Command string
	Run     func(ctx context.Context) (any, error)
}

// QuitEffect ends the program.
type QuitEffect struct{}

// PassToWidgetEffect forwards input to the focused (or hit) widget.
type PassToWidgetEffect struct {
	NodeID string
	Key    string
	Text   string
	Mouse  *MouseEvent // node-local coordinates
}

// EmitEffect carries a command no handler claimed, for the host to run.
type EmitEffect struct{ Command Command }

// CustomEffect is an open effect for handlers and hosts.
type CustomEffect struct {
	Kind string
	Data any
}

func (TimeoutEffect) isEffect()      {}
func (ExecEffect) isEffect()         {}
func (AsyncEffect) isEffect()        {}
func (QuitEffect) isEffect()         {}
func (PassToWidgetEffect) isEffect() {}
func (EmitEffect) isEffect()         {}
func (CustomEffect) isEffect()       {}

// CommandTrace records one dispatched command and who handled it.
type CommandTrace struct {
	Command Command `json:"command"`
	Handler string  `json:"handler"` // "scope:<scope>", "global", or "emit"
}

// StepTrace records everything one Step decided.
type StepTrace struct {
	Event    string         `json:"event"`
	Keys     []KeyTrace     `json:"keys,omitempty"`
	Commands []CommandTrace `json:"commands,omitempty"`
	Notes    []string       `json:"notes,omitempty"`
}

// StepResult is the output of Engine.Step and of a Handler.
type StepResult struct {
	State   State
	Effects []Effect
	Trace   StepTrace
}

// Next is a Handler helper returning s with effects.
func Next(s State, effects ...Effect) StepResult { return StepResult{State: s, Effects: effects} }

// Handler runs a command. It must be pure and return the full next State.
type Handler func(s State, cmd Command) StepResult

// Engine is the pure modal state machine.
type Engine struct {
	Keymap        *Keymap
	DefaultMode   string        // mode for frames with Mode == ""; default "normal"
	PrefixTimeout time.Duration // pending-prefix timeout; <= 0 waits forever; default 1s

	global map[string]Handler
	scoped map[string]map[string]Handler
}

// NewEngine returns an Engine over km (nil = empty keymap) with the
// built-in commands registered as global handlers.
func NewEngine(km *Keymap) *Engine {
	if km == nil {
		km, _ = NewKeymap(nil, nil)
	}
	e := &Engine{Keymap: km, DefaultMode: "normal", PrefixTimeout: time.Second, global: map[string]Handler{}, scoped: map[string]map[string]Handler{}}
	e.registerBuiltins()
	return e
}

// Handle sets the global handler for cmdID, replacing any built-in.
func (e *Engine) Handle(cmdID string, h Handler) { e.global[cmdID] = h }

// HandleScope sets the handler for cmdID within scope. Dispatch searches the
// context chain inner to outer before falling back to the global handler.
func (e *Engine) HandleScope(scope, cmdID string, h Handler) {
	if e.scoped[scope] == nil {
		e.scoped[scope] = map[string]Handler{}
	}
	e.scoped[scope][cmdID] = h
}

// Mode returns the effective mode of the active frame.
func (e *Engine) Mode(s State) string {
	if m := s.ActiveFrame().Mode; m != "" {
		return m
	}
	return e.DefaultMode
}

// Step applies one event. It performs no I/O and starts no goroutines.
func (e *Engine) Step(s State, chain []ContextFrame, ev Event) StepResult {
	r := StepResult{State: s}
	switch ev := ev.(type) {
	case KeyEvent:
		r.Trace.Event = "key:" + ev.Key
		e.key(&r, chain, ev.Key, ev.Text)
	case PasteEvent:
		r.Trace.Event = "paste"
		e.key(&r, chain, PasteKey, ev.Text)
	case MouseEvent:
		r.Trace.Event = "mouse:" + ev.Action
		e.mouse(&r, ev)
	case ResizeEvent:
		r.Trace.Event = fmt.Sprintf("resize:%dx%d", ev.W, ev.H)
		r.State.Width, r.State.Height = ev.W, ev.H
		if ev.Layout != nil {
			r.State.Layout = *ev.Layout
		}
	case TimeoutEvent:
		r.Trace.Event = "timeout"
		in := r.State.Input
		if ev.Token != in.Seq || len(in.Prefix) == 0 {
			r.Trace.Notes = append(r.Trace.Notes, "stale timeout")
			break
		}
		fb := in.Fallback
		in.Prefix, in.Fallback = nil, nil
		if fb != nil {
			in.popLayer()
			in.Count = nil
		}
		r.State.Input = in
		if fb != nil {
			e.dispatch(&r, chain, *fb)
		}
	case EffectResultEvent:
		r.Trace.Event = "result:" + ev.Slot
		if ev.Command != "" {
			e.dispatch(&r, chain, Command{ID: ev.Command, Args: map[string]any{"slot": ev.Slot, "value": ev.Value, "error": ev.Err}, Source: "effect:" + ev.Slot, Actor: Actor{Kind: "system"}})
		}
	case CommandEvent:
		r.Trace.Event = "command:" + ev.Command.ID
		e.dispatch(&r, chain, ev.Command)
	}
	return r
}

func (e *Engine) key(r *StepResult, chain []ContextFrame, key, text string) {
	q := KeyQuery{Chain: chain, Mode: e.Mode(r.State), Input: r.State.Input, Key: key, Text: text}
	if n := len(r.State.Modals); n > 0 {
		q.Modal = r.State.Modals[n-1].ID
	}
	res := e.Keymap.Resolve(q)
	r.State.Input = res.Input
	r.Trace.Keys = append(r.Trace.Keys, res.Trace)
	switch res.Outcome {
	case Pending:
		if e.PrefixTimeout > 0 {
			r.Effects = append(r.Effects, TimeoutEffect{Token: res.Input.Seq, After: e.PrefixTimeout})
		}
	case PassThrough:
		r.Effects = append(r.Effects, PassToWidgetEffect{NodeID: r.State.ActiveFrame().FocusID, Key: key, Text: text})
	case Matched, PushedLayer:
		e.dispatch(r, chain, *res.Command)
		if res.Replay {
			e.key(r, chain, key, text)
		}
	}
}

func (e *Engine) mouse(r *StepResult, ev MouseEvent) {
	id, lx, ly := r.State.Layout.HitTest(ev.X, ev.Y)
	if id == "" {
		return
	}
	if ev.Action == "press" && slices.Contains(focusables(r.State), id) {
		r.State = setFocus(r.State, id)
	}
	local := ev
	local.X, local.Y = lx, ly
	r.Effects = append(r.Effects, PassToWidgetEffect{NodeID: id, Mouse: &local})
}

func (e *Engine) dispatch(r *StepResult, chain []ContextFrame, cmd Command) {
	h, who := e.handler(chain, cmd.ID)
	r.Trace.Commands = append(r.Trace.Commands, CommandTrace{Command: cmd, Handler: who})
	if h == nil {
		r.Effects = append(r.Effects, EmitEffect{Command: cmd})
		return
	}
	out := h(r.State, cmd)
	r.State = out.State
	r.Effects = append(r.Effects, out.Effects...)
	r.Trace.Notes = append(r.Trace.Notes, out.Trace.Notes...)
}

func (e *Engine) handler(chain []ContextFrame, id string) (Handler, string) {
	for _, f := range chain {
		if h := e.scoped[f.Scope][id]; h != nil {
			return h, "scope:" + f.Scope
		}
	}
	if h := e.global[id]; h != nil {
		return h, "global"
	}
	return nil, "emit"
}
