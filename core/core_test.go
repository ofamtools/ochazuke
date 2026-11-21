package core

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"ofam.dev/ochazuke/layout"
)

func mustKeymap(t *testing.T, modes []Mode, bindings []Binding, specs ...CommandSpec) *Keymap {
	t.Helper()
	km, err := NewKeymap(modes, bindings, specs...)
	if err != nil {
		t.Fatal(err)
	}
	return km
}

// play feeds keys through Step and logs dispatched commands ("id" or
// "id#count", count.push omitted) and passthrough keys ("pass:k").
func play(e *Engine, s State, chain []ContextFrame, keys ...string) (State, []string) {
	var log []string
	for _, k := range keys {
		res := e.Step(s, chain, KeyEvent{Key: k, Text: k})
		s = res.State
		for _, c := range res.Trace.Commands {
			switch {
			case c.Command.ID == CmdCountPush:
			case c.Command.Count > 0:
				log = append(log, fmt.Sprintf("%s#%d", c.Command.ID, c.Command.Count))
			default:
				log = append(log, c.Command.ID)
			}
		}
		for _, ef := range res.Effects {
			if p, ok := ef.(PassToWidgetEffect); ok {
				log = append(log, "pass:"+p.Key)
			}
		}
	}
	return s, log
}

type replayCase struct {
	name string
	keys []string
	want []string
}

func runReplay(t *testing.T, e *Engine, s State, chain []ContextFrame, cases []replayCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			end, got := play(e, s, chain, tc.keys...)
			if len(got) == 0 {
				got = nil
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("keys %v: got %v, want %v", tc.keys, got, tc.want)
			}
			if !end.Input.Idle() && end.Input.Layer == nil {
				t.Fatalf("input left pending: %+v", end.Input)
			}
		})
	}
}

func TestLeaderInPassthroughTerminal(t *testing.T) {
	leader := &Layer{Name: "leader", Modes: []string{"terminal-leader", "normal"}, ExcludeTags: []string{"editing", "motion"}}
	bindings := append(StandardBindings("normal"),
		Binding{Mode: "normal", Keys: []string{"W"}, Command: "pane.new"},
		Binding{Mode: "normal", Keys: []string{"x"}, Command: "text.delete"},
		Binding{Mode: "normal", Keys: []string{"d"}, Command: "pane.close"},
		Binding{Mode: "terminal", Keys: []string{"ctrl+a"}, Layer: leader},
		Binding{Mode: "terminal-leader", Keys: []string{"d"}, Command: "pane.detach"},
	)
	km := mustKeymap(t, []Mode{{Name: "normal"}, {Name: "terminal", Passthrough: true}, {Name: "terminal-leader"}}, bindings,
		CommandSpec{ID: "text.delete", Tags: []string{"editing"}})
	e := NewEngine(km)
	s := NewState(Frame{ID: "main", Mode: "terminal", FocusID: "term"})
	chain := []ContextFrame{{Scope: "terminal", NodeID: "term"}, {Scope: "app"}}

	runReplay(t, e, s, chain, []replayCase{
		{"plain key passes to widget", []string{"a"}, []string{"pass:a"}},
		{"clean esc passes to widget", []string{"esc"}, []string{"pass:esc"}},
		{"falls through to normal", []string{"ctrl+a", "W"}, []string{CmdLayerPush, "pane.new"}},
		{"terminal-leader overrides normal", []string{"ctrl+a", "d"}, []string{CmdLayerPush, "pane.detach"}},
		{"motion excluded, layer pops", []string{"ctrl+a", "j", "j"}, []string{CmdLayerPush, "pass:j"}},
		{"editing excluded", []string{"ctrl+a", "x"}, []string{CmdLayerPush}},
		{"esc cancels leader", []string{"ctrl+a", "esc", "a"}, []string{CmdLayerPush, "pass:a"}},
	})

	s, _ = play(e, s, chain, "ctrl+a")
	res := e.Step(s, chain, KeyEvent{Key: "j"})
	kt := res.Trace.Keys[0]
	if kt.Layer != "leader" || kt.Outcome != Unmatched || len(kt.Candidates) != 1 || !kt.Candidates[0].Excluded {
		t.Fatalf("trace = %+v", kt)
	}
}

func TestStickyVersusOneShotLayer(t *testing.T) {
	bindings := append(StandardBindings("normal"),
		Binding{Mode: "normal", Keys: []string{"ctrl+w"}, Layer: &Layer{Name: "window", Modes: []string{"window"}}},
		Binding{Mode: "window", Keys: []string{"v"}, Command: "pane.split"},
		Binding{Mode: "window", Keys: []string{"r"}, Layer: &Layer{Name: "resize", Modes: []string{"resize"}, Sticky: true}},
		Binding{Mode: "resize", Keys: []string{"h"}, Command: "pane.shrink"},
		Binding{Mode: "resize", Keys: []string{"l"}, Command: "pane.grow"},
	)
	e := NewEngine(mustKeymap(t, nil, bindings))
	s := NewState(Frame{ID: "main"})
	runReplay(t, e, s, nil, []replayCase{
		{"one-shot pops after match", []string{"ctrl+w", "v", "j"}, []string{CmdLayerPush, "pane.split", "motion.down"}},
		{"one-shot pops on unmatched", []string{"ctrl+w", "q", "j"}, []string{CmdLayerPush, "motion.down"}},
		{"sticky stays until esc", []string{"ctrl+w", "r", "h", "h", "j", "l", "esc", "h"},
			[]string{CmdLayerPush, CmdLayerPush, "pane.shrink", "pane.shrink", "pane.grow", "motion.left"}},
	})
}

func TestPrecedenceUnbindAndInterpolation(t *testing.T) {
	bindings := []Binding{
		{Scope: "app", Keys: []string{"ctrl+q"}, Command: "app.close"},
		{Scope: "editor", Keys: []string{"ctrl+q"}, Unbind: true},
		{Scope: "app", Keys: []string{"ctrl+s"}, Phase: PhaseCapture, Command: "app.save"},
		{Scope: "editor", Keys: []string{"ctrl+s"}, Command: "editor.save"},
		{Scope: "app", Keys: []string{"p"}, Command: "palette.open"},
		{Scope: "editor", Keys: []string{"p"}, Command: "editor.paste"},
		{Scope: "page", Class: ClassPrintable, Command: "page.type"},
		{Scope: "editor", Class: ClassAlpha, Unbind: true},
		{Scope: "editor", Keys: []string{"y"}, When: `facts.readonly && subject.kind == "file"`, Command: "file.yank",
			Args: map[string]any{"n": "$count", "id": "$subject.id", "lang": "$facts.lang", "k": "$key", "lit": "x"}},
	}
	e := NewEngine(mustKeymap(t, nil, bindings))
	s := NewState(Frame{ID: "main"})
	chain := []ContextFrame{
		{Scope: "editor", Subject: &Ref{Kind: "file", ID: "a.go"}, Facts: map[string]any{"lang": "go", "readonly": true}},
		{Scope: "page"},
		{Scope: "app", Facts: map[string]any{"readonly": false}},
	}
	runReplay(t, e, s, chain, []replayCase{
		{"inner unbind suppresses outer", []string{"ctrl+q"}, nil},
		{"capture beats inner bubble", []string{"ctrl+s"}, []string{"app.save"}},
		{"inner bubble beats outer", []string{"p"}, []string{"editor.paste"}},
		{"class unbind falls to less specific class", []string{"z"}, []string{"page.type"}},
	})

	res := e.Step(s, chain, KeyEvent{Key: "y"})
	got := res.Trace.Commands[0].Command
	want := map[string]any{"n": 1, "id": "a.go", "lang": "go", "k": "y", "lit": "x"}
	if !reflect.DeepEqual(got.Args, want) || got.Target == nil || got.Target.ID != "a.go" || got.Source != "key:y" {
		t.Fatalf("command = %+v", got)
	}
	if _, ok := res.Effects[0].(EmitEffect); !ok {
		t.Fatalf("unhandled command should be emitted, got %#v", res.Effects)
	}
}

func TestAmbiguousPrefixFallback(t *testing.T) {
	bindings := append(StandardBindings("normal"), Binding{Mode: "normal", Keys: []string{"g"}, Command: "goto.menu"})
	e := NewEngine(mustKeymap(t, nil, bindings))
	s := NewState(Frame{ID: "main"})
	runReplay(t, e, s, nil, []replayCase{
		{"longer sequence wins", []string{"g", "g"}, []string{"motion.top"}},
		{"non-matching key flushes then replays", []string{"g", "j"}, []string{"goto.menu", "motion.down"}},
		{"count reaches sequence", []string{"3", "g", "g"}, []string{"motion.top#3"}},
	})

	res := e.Step(s, nil, KeyEvent{Key: "g"})
	to, ok := res.Effects[0].(TimeoutEffect)
	if !ok || res.Trace.Keys[0].Outcome != Pending || res.Trace.Keys[0].Winner == nil {
		t.Fatalf("pending result = %+v", res)
	}
	if fb := res.State.Input.Fallback; fb == nil || fb.ID != "goto.menu" {
		t.Fatalf("fallback = %+v", fb)
	}
	if b, ok := (e.Keymap.Resolve(KeyQuery{Mode: "normal", Key: "g"})).NextKeys["g"]; !ok || b.Command != "motion.top" {
		t.Fatalf("next keys missing g g: %+v", b)
	}
	if stale := e.Step(res.State, nil, TimeoutEvent{Token: to.Token - 1}); len(stale.Trace.Commands) != 0 || len(stale.State.Input.Prefix) != 1 {
		t.Fatalf("stale timeout acted: %+v", stale)
	}
	fired := e.Step(res.State, nil, TimeoutEvent{Token: to.Token})
	if len(fired.Trace.Commands) != 1 || fired.Trace.Commands[0].Command.ID != "goto.menu" || !fired.State.Input.Idle() {
		t.Fatalf("timeout = %+v", fired)
	}
}

func TestCountAccumulation(t *testing.T) {
	e := NewEngine(mustKeymap(t, nil, StandardBindings("normal")))
	runReplay(t, e, NewState(Frame{ID: "main"}), nil, []replayCase{
		{"single digit", []string{"3", "j"}, []string{"motion.down#3"}},
		{"multi digit", []string{"1", "2", "j"}, []string{"motion.down#12"}},
		{"zero without count is a motion", []string{"0"}, []string{"motion.start"}},
		{"zero inside count", []string{"1", "0", "j"}, []string{"motion.down#10"}},
		{"esc clears count", []string{"3", "esc", "j"}, []string{"motion.down"}},
	})
}

func TestModalRestoresFocusAndMode(t *testing.T) {
	bindings := []Binding{
		{Mode: "normal", Keys: []string{":"}, Command: CmdModalOpen, Args: map[string]any{"id": "palette", "mode": "insert", "focus": "input"}},
		{Mode: "insert", Keys: []string{"esc"}, Command: CmdModalClose},
		{Mode: "insert", Class: ClassPrintable, Command: "palette.type", Args: map[string]any{"text": "$text"}},
	}
	e := NewEngine(mustKeymap(t, nil, bindings))
	s0 := NewState(Frame{ID: "main", FocusID: "list", Mode: "normal"})

	s, _ := play(e, s0, nil, ":")
	if f := s.ActiveFrame(); f.ID != "palette" || f.Mode != "insert" || f.FocusID != "input" || f.Kind != FrameModal {
		t.Fatalf("modal frame = %+v", f)
	}
	s, log := play(e, s, nil, "a", "esc")
	if f := s.ActiveFrame(); f.ID != "main" || f.Mode != "normal" || f.FocusID != "list" || len(s.Modals) != 0 {
		t.Fatalf("restored frame = %+v", f)
	}
	if !reflect.DeepEqual(log, []string{"palette.type", CmdModalClose}) {
		t.Fatalf("log = %v", log)
	}
	if len(s0.Modals) != 0 || s0.ActiveFrame().FocusID != "list" {
		t.Fatal("Step mutated its input state")
	}
}

func TestHandleScopeOverridesMotion(t *testing.T) {
	e := NewEngine(mustKeymap(t, nil, StandardBindings("normal")))
	for _, scope := range []string{"editor", "grid"} {
		e.HandleScope(scope, "motion.word.next", func(s State, c Command) StepResult {
			return Next(s, CustomEffect{Kind: scope + ".word", Data: c.Count})
		})
	}
	app := ContextFrame{Scope: "app"}
	cases := []struct {
		chain   []ContextFrame
		handler string
		want    Effect
	}{
		{[]ContextFrame{{Scope: "editor"}, app}, "scope:editor", CustomEffect{Kind: "editor.word", Data: 2}},
		{[]ContextFrame{{Scope: "card"}, {Scope: "grid"}, app}, "scope:grid", CustomEffect{Kind: "grid.word", Data: 2}},
		{[]ContextFrame{{Scope: "tree"}, app}, "emit", nil},
	}
	for _, tc := range cases {
		s := NewState(Frame{ID: "main"})
		s = e.Step(s, tc.chain, KeyEvent{Key: "2"}).State
		res := e.Step(s, tc.chain, KeyEvent{Key: "w"})
		if got := res.Trace.Commands[0].Handler; got != tc.handler {
			t.Fatalf("%s: handler %q", tc.chain[0].Scope, got)
		}
		if tc.want != nil && !reflect.DeepEqual(res.Effects, []Effect{tc.want}) {
			t.Fatalf("%s: effects %#v", tc.chain[0].Scope, res.Effects)
		}
	}
}

func TestBuiltinFocusPagesAndMouse(t *testing.T) {
	leaf := func(id string) layout.Box { return layout.Box{ID: id, Kind: layout.BoxLeaf, Focusable: true} }
	solved := layout.Solve(layout.Box{ID: "root", Kind: layout.BoxStack, Axis: layout.Horizontal,
		Children: []layout.Box{leaf("a"), leaf("b"), leaf("c")}}, 30, 10)
	e := NewEngine(nil)
	s := e.Step(NewState(Frame{ID: "main", FocusID: "a"}), nil, ResizeEvent{W: 30, H: 10, Layout: &solved}).State
	do := func(id string, count int, args map[string]any) {
		s = e.Step(s, nil, CommandEvent{Command: Command{ID: id, Count: count, Args: args}}).State
	}
	steps := []struct {
		id    string
		count int
		args  map[string]any
		focus string
	}{
		{CmdFocusNext, 0, nil, "b"},
		{CmdFocusPrev, 2, nil, "c"},
		{CmdFocusLeft, 0, nil, "b"},
		{CmdFocusRight, 5, nil, "c"},
		{CmdFocusTo, 0, map[string]any{"id": "a"}, "a"},
	}
	for _, st := range steps {
		if do(st.id, st.count, st.args); s.ActiveFrame().FocusID != st.focus {
			t.Fatalf("%s: focus %q, want %q", st.id, s.ActiveFrame().FocusID, st.focus)
		}
	}

	r := solved.Rects["c"]
	res := e.Step(s, nil, MouseEvent{X: r.X + 1, Y: r.Y, Button: "left", Action: "press"})
	if p, ok := res.Effects[0].(PassToWidgetEffect); !ok || p.NodeID != "c" || p.Mouse.X != 1 || res.State.ActiveFrame().FocusID != "c" {
		t.Fatalf("mouse = %+v", res)
	}

	do(CmdPageOpen, 0, map[string]any{"id": "logs", "mode": "copy"})
	if s.ActivePage != "logs" || e.Mode(s) != "copy" {
		t.Fatalf("page.open: %q %q", s.ActivePage, e.Mode(s))
	}
	if do(CmdPageNext, 0, nil); s.ActivePage != "main" || s.ActiveFrame().FocusID != "a" {
		t.Fatalf("page.next: %q", s.ActivePage)
	}
	if res := e.Step(s, nil, CommandEvent{Command: Command{ID: CmdAppQuit}}); !reflect.DeepEqual(res.Effects, []Effect{QuitEffect{}}) {
		t.Fatalf("quit = %#v", res.Effects)
	}
}

func TestNewKeymapRejectsBadBindings(t *testing.T) {
	_, err := NewKeymap(nil, []Binding{
		{Keys: []string{"a"}, Command: "ok"},
		{Keys: []string{"a"}, Class: ClassDigit, Command: "x"},
		{Class: "vowel", Command: "x"},
		{Keys: []string{"a"}},
		{Keys: []string{"a"}, When: "(", Command: "x"},
	})
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) || len(joined.Unwrap()) != 4 {
		t.Fatalf("err = %v", err)
	}
}
