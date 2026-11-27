package core

import (
	"encoding/json"
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
	if v, _ := r.Get('A'); !reflect.DeepEqual(v, RegisterValue{Text: "twothree\nline", Linewise: true}) {
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

func TestRegistersTypedYankAndAppend(t *testing.T) {
	var r Registers

	// Single-ref yank into unnamed and "0; Cut is always cleared on yank.
	single := RegisterValue{
		Text:   "Fix login",
		Kind:   "refs",
		Refs:   []Ref{{Kind: "task", ID: "t1"}},
		Cut:    true,
		Source: "tasks.list",
		Meta:   map[string]any{"project": "p1"},
	}
	r.YankValue(0, single)
	for _, reg := range []rune{RegUnnamed, RegYank} {
		got, ok := r.Get(reg)
		if !ok {
			t.Fatalf("Get(%q) missing", reg)
		}
		if got.Cut {
			t.Fatalf("Get(%q) kept Cut=true on yank", reg)
		}
		if !got.HasRefs() || !reflect.DeepEqual(got.Refs, []Ref{{Kind: "task", ID: "t1"}}) {
			t.Fatalf("Get(%q) refs = %+v", reg, got.Refs)
		}
		if got.Kind != "refs" || got.Source != "tasks.list" || got.Meta["project"] != "p1" {
			t.Fatalf("Get(%q) = %+v", reg, got)
		}
	}

	// Multi-select refs into named register 'a', then uppercase append 'A'.
	multi := RegisterValue{
		Text:     "Task 1\nTask 2",
		Linewise: true,
		Kind:     "refs",
		Refs:     []Ref{{Kind: "task", ID: "t1"}, {Kind: "task", ID: "t2"}},
		Source:   "tasks.list",
		Meta:     map[string]any{"batch": 1},
	}
	r.YankValue('a', multi)
	r.YankValue('A', RegisterValue{
		Text:   "Task 3",
		Kind:   "refs",
		Refs:   []Ref{{Kind: "task", ID: "t3"}},
		Source: "tasks.list",
		Meta:   map[string]any{"extra": true},
	})

	wantRefs := []Ref{{Kind: "task", ID: "t1"}, {Kind: "task", ID: "t2"}, {Kind: "task", ID: "t3"}}
	for _, reg := range []rune{'a', RegUnnamed} {
		got, ok := r.Get(reg)
		if !ok {
			t.Fatalf("Get(%q) missing", reg)
		}
		if got.Text != "Task 1\nTask 2\nTask 3" || !got.Linewise || got.Kind != "refs" {
			t.Fatalf("Get(%q) merged = %+v", reg, got)
		}
		if !reflect.DeepEqual(got.Refs, wantRefs) {
			t.Fatalf("Get(%q) merged refs = %+v, want %+v", reg, got.Refs, wantRefs)
		}
		if got.Source != "tasks.list" || got.Meta["batch"] != 1 || got.Meta["extra"] != true {
			t.Fatalf("Get(%q) merged meta/source = %+v", reg, got)
		}
	}
	// "0 keeps only the latest yanked item, not the accumulated register.
	if y0, _ := r.Get(RegYank); y0.Text != "Task 3" || !reflect.DeepEqual(y0.Refs, []Ref{{Kind: "task", ID: "t3"}}) {
		t.Fatalf(`"0 after "A = %+v`, y0)
	}

	// Non-linewise refs+refs append still separates Text cleanly with newline.
	r.YankValue('b', RegisterValue{Text: "Alpha", Kind: "refs", Refs: []Ref{{Kind: "note", ID: "n1"}}})
	r.YankValue('B', RegisterValue{Text: "Beta", Kind: "refs", Refs: []Ref{{Kind: "note", ID: "n2"}}})
	if got, _ := r.Get('b'); got.Text != "Alpha\nBeta" || len(got.Refs) != 2 || got.Linewise {
		t.Fatalf(`"B charwise refs append = %+v`, got)
	}

	// Appending plain text onto a refs register degrades cleanly to text.
	r.YankValue('B', RegisterValue{Text: " plain", Kind: "text"})
	if got, _ := r.Get('b'); got.Text != "Alpha\nBeta plain" || got.Kind != "" || got.HasRefs() {
		t.Fatalf(`"B mixed append = %+v`, got)
	}

	// Black hole discards typed yanks without touching "" or "0.
	beforeUnnamed, _ := r.Get(RegUnnamed)
	r.YankValue(RegBlackHole, single)
	if after, _ := r.Get(RegUnnamed); !reflect.DeepEqual(after, beforeUnnamed) {
		t.Fatalf("black hole mutated unnamed register: %+v", after)
	}
}

func TestRegistersDeleteValueRingAndCut(t *testing.T) {
	var r Registers
	r.YankValue(0, RegisterValue{Text: "keep yank", Kind: "field", Field: "title"})

	// Structured ref delete sets Cut=true and shifts "1.."9 even when single-line.
	for i := 1; i <= 10; i++ {
		id := string(rune('a' + i - 1))
		r.DeleteValue(0, RegisterValue{
			Text:   "item-" + id,
			Kind:   "refs",
			Refs:   []Ref{{Kind: "task", ID: id}},
			Source: "tasks",
		})
	}
	if y0, _ := r.Get(RegYank); y0.Text != "keep yank" || y0.Cut {
		t.Fatalf(`DeleteValue clobbered "0: %+v`, y0)
	}
	if u, _ := r.Get(RegUnnamed); u.Text != "item-j" || !u.Cut || !u.HasRefs() {
		t.Fatalf(`"" = %+v`, u)
	}
	if d1, _ := r.Get('1'); d1.Text != "item-j" || !d1.Cut || !reflect.DeepEqual(d1.Refs, []Ref{{Kind: "task", ID: "j"}}) {
		t.Fatalf(`"1 = %+v`, d1)
	}
	if d9, _ := r.Get('9'); d9.Text != "item-b" || !d9.Cut || !reflect.DeepEqual(d9.Refs, []Ref{{Kind: "task", ID: "b"}}) {
		t.Fatalf(`"9 = %+v`, d9)
	}
	if _, ok := r.Get(RegSmallDel); ok {
		t.Fatal("ref delete should shift delete ring, not write small delete register")
	}

	// Small unnamed plain-text DeleteValue writes "- with Cut=true.
	r.DeleteValue(0, RegisterValue{Text: "word", Kind: "text"})
	if sd, ok := r.Get(RegSmallDel); !ok || sd.Text != "word" || !sd.Cut {
		t.Fatalf(`"- = %+v ok=%v`, sd, ok)
	}

	// Named cut + uppercase append preserves Cut=true and merges Refs.
	r.DeleteValue('c', RegisterValue{Text: "C1", Kind: "refs", Refs: []Ref{{Kind: "task", ID: "c1"}}})
	r.DeleteValue('C', RegisterValue{Text: "C2", Kind: "refs", Refs: []Ref{{Kind: "task", ID: "c2"}}})
	if c, _ := r.Get('c'); !c.Cut || c.Text != "C1\nC2" || len(c.Refs) != 2 {
		t.Fatalf(`"C cut append = %+v`, c)
	}
	if d1, _ := r.Get('1'); !d1.Cut || d1.Text != "C2" || len(d1.Refs) != 1 {
		t.Fatalf(`"1 after "C cut append = %+v`, d1)
	}
}

func TestRegistersCopyOnWriteIsolation(t *testing.T) {
	var r Registers
	refs := []Ref{{Kind: "task", ID: "t1"}}
	nested := map[string]any{"tag": "orig"}
	tags := []string{"v1"}
	meta := map[string]any{"nested": nested, "tags": tags}

	r.YankValue('a', RegisterValue{
		Text: "Task 1",
		Kind: "refs",
		Refs: refs,
		Meta: meta,
	})

	// Mutate caller inputs after YankValue.
	refs[0].ID = "mutated-input"
	nested["tag"] = "mutated-nested"
	tags[0] = "mutated-tag"
	meta["injected"] = true

	got, ok := r.Get('a')
	if !ok {
		t.Fatal(`"a missing`)
	}
	if got.Refs[0].ID != "t1" {
		t.Fatalf("caller slice mutation leaked into register: %+v", got.Refs)
	}
	if got.Meta["injected"] != nil {
		t.Fatalf("caller map mutation leaked into register: %+v", got.Meta)
	}
	if got.Meta["nested"].(map[string]any)["tag"] != "orig" {
		t.Fatalf("caller nested map mutation leaked: %+v", got.Meta)
	}
	if got.Meta["tags"].([]string)[0] != "v1" {
		t.Fatalf("caller nested slice mutation leaked: %+v", got.Meta)
	}

	// Mutate returned value from Get and Snapshot.
	got.Refs[0].ID = "mutated-get"
	got.Meta["nested"].(map[string]any)["tag"] = "mutated-get"
	snapMap := r.Snapshot()
	snapMap['a'].Refs[0].ID = "mutated-snapshot"
	snapMap['a'].Meta["tags"].([]string)[0] = "mutated-snapshot"

	again, _ := r.Get('a')
	if again.Refs[0].ID != "t1" ||
		again.Meta["nested"].(map[string]any)["tag"] != "orig" ||
		again.Meta["tags"].([]string)[0] != "v1" {
		t.Fatalf("reader mutation leaked into bank: %+v", again)
	}

	// Value copy of Registers stays isolated across SetValue / SetSpecialValue.
	bankSnap := r
	r.SetValue('a', RegisterValue{Text: "replaced", Kind: "text"})
	if !r.SetSpecialValue(RegPath, RegisterValue{Text: "docs/a.md", Kind: "path", Refs: []Ref{{Kind: "doc", ID: "d1"}}}) {
		t.Fatal("SetSpecialValue('%') refused")
	}
	if r.SetSpecialValue('z', RegisterValue{Text: "nope"}) {
		t.Fatal("SetSpecialValue('z') accepted non-special register")
	}
	if old, _ := bankSnap.Get('a'); old.Text != "Task 1" || !old.HasRefs() {
		t.Fatalf("Registers value snapshot mutated: %+v", old)
	}
	if p, ok := r.Get(RegPath); !ok || p.Kind != "path" || !p.HasRefs() {
		t.Fatalf(`"%% = %+v ok=%v`, p, ok)
	}
}

func TestRegisterValueJSONRoundTrip(t *testing.T) {
	rich := RegisterValue{
		Text:     "Fix auth\nAdd tests",
		Linewise: true,
		Kind:     "refs",
		Refs:     []Ref{{Kind: "task", ID: "t1"}, {Kind: "task", ID: "t2"}},
		Field:    "title",
		Cut:      true,
		Source:   "tasks.tree",
		Meta:     map[string]any{"count": float64(2), "view": "tree"},
	}
	raw, err := json.Marshal(rich)
	if err != nil {
		t.Fatalf("marshal rich: %v", err)
	}
	var decoded RegisterValue
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal rich: %v", err)
	}
	if !reflect.DeepEqual(decoded, rich) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", decoded, rich)
	}

	// Plain-text RegisterValue keeps minimal JSON representation.
	plainRaw, err := json.Marshal(RegisterValue{Text: "hello"})
	if err != nil {
		t.Fatalf("marshal plain: %v", err)
	}
	if string(plainRaw) != `{"text":"hello"}` {
		t.Fatalf("plain json = %s, want %s", plainRaw, `{"text":"hello"}`)
	}
	if (RegisterValue{Text: "hello"}).HasRefs() {
		t.Fatal("plain RegisterValue reported HasRefs=true")
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
