package core

import (
	"maps"
	"reflect"
	"testing"
)

func TestRegisters(t *testing.T) {
	var r Registers
	r.Yank(0, "one", false)
	if v, _ := r.Get('"'); v.Text != "one" {
		t.Fatalf(`"" = %q`, v.Text)
	}
	if v, _ := r.Get('0'); v.Text != "one" {
		t.Fatalf(`"0 = %q`, v.Text)
	}
	r.Yank('a', "two", false)
	r.Yank('A', "three", false)
	if v, _ := r.Get('a'); v.Text != "twothree" {
		t.Fatalf(`"a = %q`, v.Text)
	}
	if v, _ := r.Get('"'); v.Text != "twothree" {
		t.Fatalf(`"" after "A = %q`, v.Text)
	}
	r.Yank('A', "line", true)
	if v, _ := r.Get('A'); v != (RegisterValue{"twothree\nline", true}) {
		t.Fatalf(`"A linewise append = %+v`, v)
	}

	snap := r // value copy is an independent snapshot
	r.Delete(0, "gone", false)
	if v, _ := r.Get('"'); v.Text != "gone" {
		t.Fatalf(`"" after delete = %q`, v.Text)
	}
	if v, _ := r.Get('0'); v.Text != "line" {
		t.Fatalf(`delete clobbered "0: %q`, v.Text)
	}
	if v, _ := r.Get('-'); v.Text != "gone" {
		t.Fatalf(`"- = %q`, v.Text)
	}
	if v, _ := snap.Get('"'); v.Text != "twothree\nline" {
		t.Fatalf("snapshot mutated: %q", v.Text)
	}
	r.Delete(0, "l1", true)
	r.Delete(0, "l2", true)
	if v, _ := r.Get('1'); v.Text != "l2" {
		t.Fatalf(`"1 = %q`, v.Text)
	}
	if v, _ := r.Get('2'); v.Text != "l1" {
		t.Fatalf(`"2 = %q`, v.Text)
	}
	r.Yank('_', "void", false)
	if v, _ := r.Get('"'); v.Text != "l2" {
		t.Fatalf("black hole wrote: %q", v.Text)
	}

	for reg, text := range map[rune]string{'%': "notes/a.md", '/': "needle", ':': "w", '+': "clip"} {
		if !r.SetSpecial(reg, text) {
			t.Fatalf("SetSpecial(%q) refused", reg)
		}
		if v, ok := r.Get(reg); !ok || v.Text != text {
			t.Fatalf("%q = %q", reg, v.Text)
		}
	}
	if r.SetSpecial('a', "x") {
		t.Fatal("SetSpecial accepted a named register")
	}
	want := []rune{'"', '%', '+', '-', '/', '0', '1', '2', ':', 'a'}
	if got := r.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names = %q, want %q", got, want)
	}
	s := r.Snapshot()
	s['a'] = RegisterValue{Text: "changed"}
	if v, _ := r.Get('a'); v.Text == "changed" {
		t.Fatal("Snapshot shares storage")
	}
}

// counting returns a handler that increments Data[name] by the command's
// count on the active frame.
func counting(name string) Handler {
	return func(s State, cmd Command) StepResult {
		f := s.ActiveFrame()
		data, _ := f.Data.(map[string]int)
		data = maps.Clone(data)
		if data == nil {
			data = map[string]int{}
		}
		data[name] += cmd.Times()
		f.Data = data
		return Next(s.WithActiveFrame(f))
	}
}

func TestMacroReplayThroughEngine(t *testing.T) {
	bindings := append(StandardBindings("normal"),
		Binding{Mode: "normal", Keys: []string{"x"}, Command: "char.delete"},
		Binding{Mode: "normal", Keys: []string{"d", "d"}, Command: "line.delete"},
		Binding{Mode: "normal", Keys: []string{"i"}, Command: CmdModeSet, Args: map[string]any{"mode": "insert"}},
		Binding{Mode: "insert", Keys: []string{"esc"}, Command: CmdModeSet, Args: map[string]any{"mode": "normal"}},
		Binding{Scope: "list", Mode: "normal", Keys: []string{"x"}, Command: "row.delete"},
	)
	km := mustKeymap(t, []Mode{{Name: "normal"}, {Name: "insert", Passthrough: true}}, bindings)
	e := NewEngine(km)
	e.Handle("char.delete", counting("chars"))
	e.Handle("line.delete", counting("lines"))
	e.Handle("row.delete", counting("rows"))
	s := NewState(Frame{ID: "main", FocusID: "editor"})

	var rec MacroRecorder
	if !rec.Start('q') {
		t.Fatal("Start refused")
	}
	if rec.Start('w') {
		t.Fatal("Start while recording")
	}
	if reg, ok := rec.Recording(); !ok || reg != 'q' {
		t.Fatalf("Recording = %q %v", reg, ok)
	}
	for _, k := range []string{"2", "x", "d", "d", "i", "a", "esc"} {
		ev := KeyEvent{Key: k, Text: k}
		rec.Record(ev)
		s = e.Step(s, nil, ev).State
	}
	keys := rec.Stop()
	if len(keys) != 7 {
		t.Fatalf("recorded %d keys", len(keys))
	}
	if _, ok := rec.Recording(); ok {
		t.Fatal("still recording")
	}
	data := s.ActiveFrame().Data.(map[string]int)
	if data["chars"] != 2 || data["lines"] != 1 {
		t.Fatalf("live data %v", data)
	}

	replay, ok := rec.Replay('q', 2)
	if !ok || len(replay) != 14 || rec.LastPlayed() != 'q' {
		t.Fatalf("Replay = %d keys %v last %q", len(replay), ok, rec.LastPlayed())
	}
	res := e.ReplayKeys(s, nil, replay)
	data = res.State.ActiveFrame().Data.(map[string]int)
	if data["chars"] != 6 || data["lines"] != 3 || e.Mode(res.State) != "normal" {
		t.Fatalf("replayed data %v mode %s", data, e.Mode(res.State))
	}
	passes := 0
	for _, ef := range res.Effects {
		switch ef := ef.(type) {
		case TimeoutEffect:
			t.Fatalf("stale timeout kept: %+v", ef)
		case PassToWidgetEffect:
			if ef.Key != "a" || ef.NodeID != "editor" {
				t.Fatalf("pass %+v", ef)
			}
			passes++
		}
	}
	if passes != 2 {
		t.Fatalf("insert-mode passthrough %d, want 2", passes)
	}

	// The chain is recomputed per key, so the same macro acts on whatever
	// scope is focused when it replays.
	listChain := func(State) []ContextFrame { return []ContextFrame{{Scope: "list"}} }
	again, ok := rec.ReplayLast(1)
	if !ok {
		t.Fatal("ReplayLast failed")
	}
	res = e.ReplayKeys(res.State, listChain, again)
	data = res.State.ActiveFrame().Data.(map[string]int)
	if data["rows"] != 2 || data["chars"] != 6 {
		t.Fatalf("list replay data %v", data)
	}

	// A prefix left pending at the end keeps its timeout.
	res = e.ReplayKeys(res.State, nil, []KeyEvent{{Key: "x"}, {Key: "d"}})
	var timeouts int
	for _, ef := range res.Effects {
		if _, ok := ef.(TimeoutEffect); ok {
			timeouts++
		}
	}
	if timeouts != 1 || len(res.State.Input.Prefix) != 1 {
		t.Fatalf("pending timeouts %d prefix %v", timeouts, res.State.Input.Prefix)
	}

	if _, ok := rec.Replay('z', 1); ok {
		t.Fatal("replayed empty register")
	}
	rec.Start('Q')
	rec.Record(KeyEvent{Key: "x"})
	rec.Stop()
	if got, _ := rec.Macro('q'); len(got) != 8 {
		t.Fatalf("append macro has %d keys", len(got))
	}
}
