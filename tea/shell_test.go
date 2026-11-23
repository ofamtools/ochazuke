package otea

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/recorder"
	"ofam.dev/ochazuke/ui"
)

func card(id string) ui.ViewNode {
	return ui.ViewNode{ID: id, Kind: ui.KindCard, Title: id, Focusable: true, Scope: "card", CacheKey: 1}
}

func cards(s core.State) ui.ViewNode {
	return ui.ViewNode{Kind: ui.KindVStack, Children: []ui.ViewNode{card("a"), card("b"), card("c")}}
}

// newShell builds a shell with a normal mode, a passthrough "term" mode and a
// 40x20 screen already rendered.
func newShell(t *testing.T, view ViewFunc) *Shell {
	t.Helper()
	km, err := core.NewKeymap(
		[]core.Mode{{Name: "normal"}, {Name: "term", Passthrough: true}},
		append(core.StandardBindings("normal"),
			core.Binding{Mode: "normal", Keys: []string{"n"}, Command: core.CmdFocusNext},
			core.Binding{Mode: "normal", Keys: []string{"q"}, Command: core.CmdAppQuit},
		))
	if err != nil {
		t.Fatal(err)
	}
	sh := New(core.NewEngine(km), core.NewState(core.Frame{ID: "main", FocusID: "a"}), view, ui.DefaultDarkTheme())
	sh.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	return sh
}

// run executes cmd (flattening batches) and returns every message produced.
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, run(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestKeyNormalization(t *testing.T) {
	for _, tc := range []struct {
		in   tea.KeyPressMsg
		want core.KeyEvent
	}{
		{tea.KeyPressMsg{Code: 'j', Text: "j"}, core.KeyEvent{Key: "j", Text: "j"}},
		{tea.KeyPressMsg{Code: 'g', ShiftedCode: 'G', Text: "G", Mod: tea.ModShift}, core.KeyEvent{Key: "G", Text: "G"}},
		{tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}, core.KeyEvent{Key: "ctrl+a"}},
		{tea.KeyPressMsg{Code: tea.KeyEscape}, core.KeyEvent{Key: "esc"}},
		{tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, core.KeyEvent{Key: "space", Text: " "}},
		{tea.KeyPressMsg{Code: tea.KeyEnter}, core.KeyEvent{Key: "enter"}},
		{tea.KeyPressMsg{Code: 'x', Text: "x", Mod: tea.ModAlt}, core.KeyEvent{Key: "alt+x", Text: "x"}},
	} {
		ev, ok := Translate(tc.in)
		if !ok || ev != tc.want {
			t.Errorf("Translate(%v) = %#v, want %#v", tc.in, ev, tc.want)
		}
	}
}

func TestTranslateOtherMessages(t *testing.T) {
	for _, tc := range []struct {
		in   tea.Msg
		want core.Event
	}{
		{tea.PasteMsg{Content: "hi"}, core.PasteEvent{Text: "hi"}},
		{tea.MouseClickMsg{X: 3, Y: 4, Button: tea.MouseLeft}, core.MouseEvent{X: 3, Y: 4, Button: "left", Action: "press"}},
		{tea.MouseReleaseMsg{X: 3, Y: 4, Button: tea.MouseLeft}, core.MouseEvent{X: 3, Y: 4, Button: "left", Action: "release"}},
		{tea.MouseWheelMsg{Button: tea.MouseWheelDown}, core.MouseEvent{Button: "wheeldown", Action: "motion"}},
		{tea.MouseMotionMsg{X: 1}, core.MouseEvent{X: 1, Action: "motion"}},
		{tea.WindowSizeMsg{Width: 80, Height: 24}, core.ResizeEvent{W: 80, H: 24}},
		{core.TimeoutEvent{Token: 7}, core.TimeoutEvent{Token: 7}},
	} {
		if ev, ok := Translate(tc.in); !ok || ev != tc.want {
			t.Errorf("Translate(%#v) = %#v, want %#v", tc.in, ev, tc.want)
		}
	}
	if _, ok := Translate(tea.KeyReleaseMsg{Code: 'j'}); ok {
		t.Error("key release must not translate")
	}
}

func TestUpdateStepsEngineAndMovesFocus(t *testing.T) {
	sh := newShell(t, cards)
	sh.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if got := sh.State.ActiveFrame().FocusID; got != "b" {
		t.Fatalf("focus after n = %q, want b (trace %+v)", got, sh.LastTrace)
	}
	if sh.LastTrace.Event != "key:n" {
		t.Errorf("trace event = %q", sh.LastTrace.Event)
	}
	if len(sh.State.Layout.FocusOrder) != 3 {
		t.Errorf("layout not fed back to state: %+v", sh.State.Layout.FocusOrder)
	}
}

func TestQuitEffect(t *testing.T) {
	sh := newShell(t, cards)
	_, cmd := sh.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	msgs := run(cmd)
	if len(msgs) != 1 {
		t.Fatalf("msgs = %v", msgs)
	}
	if _, ok := msgs[0].(tea.QuitMsg); !ok {
		t.Fatalf("want QuitMsg, got %T", msgs[0])
	}
}

func TestTimeoutEffect(t *testing.T) {
	sh := newShell(t, cards)
	sh.Engine.PrefixTimeout = time.Millisecond
	km, _ := core.NewKeymap([]core.Mode{{Name: "normal"}}, []core.Binding{
		{Mode: "normal", Keys: []string{"g", "g"}, Command: "motion.top"},
	})
	sh.Engine.Keymap = km
	_, cmd := sh.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	msgs := run(cmd)
	if len(msgs) != 1 {
		t.Fatalf("msgs = %v", msgs)
	}
	to, ok := msgs[0].(core.TimeoutEvent)
	if !ok || to.Token != sh.State.Input.Seq || to.Token == 0 {
		t.Fatalf("want TimeoutEvent{Seq}, got %#v (seq %d)", msgs[0], sh.State.Input.Seq)
	}
	sh.Update(to)
	if len(sh.State.Input.Prefix) != 0 {
		t.Errorf("prefix not flushed by timeout: %v", sh.State.Input.Prefix)
	}
}

func TestAsyncSlotCancellation(t *testing.T) {
	sh := newShell(t, cards)
	var done atomic.Int32
	var gotCancel atomic.Bool
	started := make(chan struct{})
	sh.Engine.Handle("job.done", func(s core.State, c core.Command) core.StepResult {
		done.Add(1)
		return core.Next(s)
	})
	sh.Engine.Handle("job.slow", func(s core.State, c core.Command) core.StepResult {
		return core.Next(s, core.AsyncEffect{Slot: "job", Command: "job.done", Run: func(ctx context.Context) (any, error) {
			close(started)
			<-ctx.Done()
			gotCancel.Store(true)
			return nil, ctx.Err()
		}})
	})
	sh.Engine.Handle("job.fast", func(s core.State, c core.Command) core.StepResult {
		return core.Next(s, core.AsyncEffect{Slot: "job", Command: "job.done", Run: func(context.Context) (any, error) { return "ok", nil }})
	})

	_, slow := sh.Update(core.CommandEvent{Command: core.Command{ID: "job.slow"}})
	slowOut := make(chan []tea.Msg, 1)
	go func() { slowOut <- run(slow) }()
	<-started

	_, fast := sh.Update(core.CommandEvent{Command: core.Command{ID: "job.fast"}})
	for _, m := range run(fast) {
		sh.Update(m)
	}
	select {
	case msgs := <-slowOut: // returns only once its context was cancelled
		for _, m := range msgs {
			sh.Update(m) // superseded result: must be dropped
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow job was not cancelled by the newer effect in its slot")
	}
	if !gotCancel.Load() {
		t.Error("older context in the same slot was not cancelled")
	}
	if n := done.Load(); n != 1 {
		t.Errorf("job.done ran %d times, want 1 (superseded result dropped)", n)
	}
	if len(sh.slots) != 0 {
		t.Errorf("slot not released: %v", sh.slots)
	}
}

func TestPassThroughAndHooks(t *testing.T) {
	sh := newShell(t, cards)
	var passed []core.PassToWidgetEffect
	var emitted []core.Command
	sh.OnPassThrough = func(e core.PassToWidgetEffect) tea.Cmd { passed = append(passed, e); return nil }
	sh.OnEmit = func(c core.Command) tea.Cmd { emitted = append(emitted, c); return nil }
	var custom []core.CustomEffect
	sh.OnCustom = func(e core.CustomEffect) tea.Cmd { custom = append(custom, e); return nil }
	sh.Engine.Handle("x.custom", func(s core.State, c core.Command) core.StepResult {
		return core.Next(s, core.CustomEffect{Kind: "k", Data: 1})
	})

	sh.Update(core.CommandEvent{Command: core.Command{ID: "mode.set", Args: map[string]any{"mode": "term"}}})
	sh.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if len(passed) != 1 || passed[0].Key != "z" || passed[0].NodeID != "a" {
		t.Fatalf("pass-through = %+v", passed)
	}
	sh.Update(core.CommandEvent{Command: core.Command{ID: "unclaimed.thing"}})
	sh.Update(core.CommandEvent{Command: core.Command{ID: "x.custom"}})
	if len(emitted) != 1 || emitted[0].ID != "unclaimed.thing" {
		t.Errorf("emitted = %+v", emitted)
	}
	if len(custom) != 1 || custom[0].Kind != "k" {
		t.Errorf("custom = %+v", custom)
	}
}

func TestExecEffectBuildsCommand(t *testing.T) {
	sh := newShell(t, cards)
	sh.Engine.Handle("run.it", func(s core.State, c core.Command) core.StepResult {
		return core.Next(s, core.ExecEffect{Name: "true", Slot: "x", Command: "run.done"})
	})
	if _, cmd := sh.Update(core.CommandEvent{Command: core.Command{ID: "run.it"}}); cmd == nil {
		t.Fatal("ExecEffect produced no tea.Cmd")
	}
}

func TestDirtyCoalescingAndCachedView(t *testing.T) {
	sh := newShell(t, cards)
	base := sh.Renders
	if base != 1 {
		t.Fatalf("resize should render once, got %d", base)
	}

	// Idle: repeated View calls reuse the cached frame.
	first := sh.View()
	for range 5 {
		sh.View()
	}
	if sh.Renders != base {
		t.Fatalf("idle View re-rendered: %d", sh.Renders)
	}

	// A burst of background results coalesces behind one frame tick.
	ticks := 0
	for range 4 {
		_, cmd := sh.Update(core.EffectResultEvent{Slot: "bg"})
		ticks += len(run(cmd))
	}
	if ticks != 1 {
		t.Errorf("burst produced %d frame ticks, want 1", ticks)
	}
	if sh.Renders != base || sh.View().Content != first.Content {
		t.Errorf("background burst rendered early: renders=%d", sh.Renders)
	}
	sh.Update(frameMsg{})
	if sh.Renders != base+1 {
		t.Errorf("frame tick renders = %d, want %d", sh.Renders, base+1)
	}
	if sh.Stats.CacheHits == 0 || sh.Stats.CacheMisses != 0 {
		t.Errorf("unchanged frame should be all cache hits: %+v", sh.Stats)
	}
	sh.Update(frameMsg{}) // clean tick: no work
	if sh.Renders != base+1 {
		t.Errorf("clean frame tick rendered")
	}

	// Input renders immediately and only the cards whose focus changed miss.
	sh.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if sh.Renders != base+2 {
		t.Errorf("key did not render immediately: %d", sh.Renders)
	}
	if sh.Stats.CacheMisses != 2 || sh.Stats.CacheHits != 1 {
		t.Errorf("focus move should miss 2 cards and hit 1: %+v", sh.Stats)
	}
	if sh.Stats.RenderUS < 0 || sh.Stats.StepUS < 0 {
		t.Errorf("stats = %+v", sh.Stats)
	}

	// MaxFPS 0 renders background results immediately.
	sh.MaxFPS = 0
	sh.Update(core.EffectResultEvent{Slot: "bg"})
	if sh.Renders != base+3 {
		t.Errorf("MaxFPS=0 should render now: %d", sh.Renders)
	}
}

func TestViewChromeAndCursor(t *testing.T) {
	raw := func(s core.State) ui.ViewNode {
		return ui.ViewNode{Kind: ui.KindVStack, Children: []ui.ViewNode{{
			ID: "r", Kind: ui.KindRaw, Focusable: true,
			Props: map[string]any{"lines": []string{"hello"}, "cursor": map[string]any{"x": 2, "y": 0, "shape": "bar"}},
		}}}
	}
	sh := newShell(t, raw)
	sh.State = sh.State.WithActiveFrame(core.Frame{ID: "main", FocusID: "r"})
	sh.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	v := sh.View()
	if !v.AltScreen || v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("view chrome: alt=%v mouse=%v", v.AltScreen, v.MouseMode)
	}
	if v.Cursor == nil || v.Cursor.X != 2 || v.Cursor.Y != 0 || v.Cursor.Shape != tea.CursorBar {
		t.Fatalf("cursor = %+v", v.Cursor)
	}
	sh.State = sh.State.WithActiveFrame(core.Frame{ID: "main", FocusID: ""})
	sh.DisableMouse = true
	sh.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	if v := sh.View(); v.Cursor != nil || v.MouseMode != tea.MouseModeNone {
		t.Errorf("unfocused raw node should hide cursor and honour DisableMouse: %+v", v)
	}
}

func TestShellRecorderCaptureAndFrameDedupe(t *testing.T) {
	sh := newShell(t, cards)
	if sh.Recorder == nil || !sh.Recorder.Enabled() {
		t.Fatalf("expected enabled default recorder on New")
	}

	// newShell already sent 40x20 WindowSizeMsg and rendered once.
	hdr, evs := sh.Recorder.Snapshot()
	if hdr.Width != 40 || hdr.Height != 20 {
		t.Fatalf("initial header size = %dx%d, want 40x20", hdr.Width, hdr.Height)
	}
	if len(evs) != 2 || evs[0].Kind != recorder.KindResize || evs[1].Kind != recorder.KindFrame {
		t.Fatalf("expected [resize, frame] after initial WindowSizeMsg, got %+v", evs)
	}

	// Bare hover mouse motion (empty button) must not be recorded, and unchanged render dedupes frame.
	sh.Update(tea.MouseMotionMsg{X: 5, Y: 5})
	if sh.Recorder.Len() != 2 {
		t.Fatalf("hover motion + unchanged frame should not add events, got len=%d", sh.Recorder.Len())
	}

	// Key press moves focus a -> b, recording key, command, and new frame.
	sh.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	// Paste and mouse click are also recorded.
	sh.Update(tea.PasteMsg{Content: "hello"})
	sh.Update(tea.MouseClickMsg{X: 2, Y: 1, Button: tea.MouseLeft})

	_, evs = sh.Recorder.Snapshot()
	var sawKey, sawCmd, sawPaste, sawMouse bool
	frameCount := 0
	for _, ev := range evs {
		switch ev.Kind {
		case recorder.KindKey:
			if ev.Key == "n" && ev.Text == "n" {
				sawKey = true
			}
		case recorder.KindCommand:
			if ev.Command == core.CmdFocusNext && ev.Mode == "normal" && ev.Page == "main" && ev.FocusID == "b" {
				sawCmd = true
			}
		case recorder.KindPaste:
			if ev.Text == "hello" {
				sawPaste = true
			}
		case recorder.KindMouse:
			if ev.Button == "left" && ev.Action == "press" && ev.X == 2 && ev.Y == 1 {
				sawMouse = true
			}
		case recorder.KindFrame:
			frameCount++
		}
	}
	if !sawKey || !sawCmd || !sawPaste || !sawMouse {
		t.Fatalf("missing recorded events: key=%v cmd=%v paste=%v mouse=%v; all=%+v", sawKey, sawCmd, sawPaste, sawMouse, evs)
	}
	if frameCount < 2 {
		t.Fatalf("expected at least 2 distinct frames recorded, got %d", frameCount)
	}

	// EnableRecorder seeds current shell size.
	r2 := sh.EnableRecorder(recorder.Options{App: "test-app"})
	if r2.Header().Width != 40 || r2.Header().Height != 20 || r2.Header().App != "test-app" {
		t.Fatalf("EnableRecorder header = %+v", r2.Header())
	}
}

func TestShellNilAndDisabledRecorderNoOp(t *testing.T) {
	sh := newShell(t, cards)

	// Nil recorder must be a clean no-op.
	sh.SetRecorder(nil)
	sh.Update(tea.WindowSizeMsg{Width: 50, Height: 25})
	sh.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if got := sh.State.ActiveFrame().FocusID; got != "b" {
		t.Fatalf("focus with nil recorder = %q, want b", got)
	}

	// Disabled recorder must record zero events.
	disabled := recorder.New(recorder.Options{Enabled: false})
	sh.SetRecorder(disabled)
	sh.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	sh.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	if disabled.Len() != 0 || disabled.Stats().Total != 0 {
		t.Fatalf("disabled recorder captured events: %+v", disabled.Stats())
	}
}
