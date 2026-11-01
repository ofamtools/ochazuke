package ochazuke

import (
	"reflect"
	"testing"
	"time"

	"ofam.dev/ochazuke/keytest"
)

// dispatchTestModel is one focused single-line editor holding text, with each
// key bound on the app noun (any mode) to a recorded test operation. These
// tests lock the stack walk: application rows beat the editor defaults for a
// fresh key, pending modes are exclusive, modals answer esc, and the palette
// owns typing while it is open.
func dispatchTestModel(t *testing.T, text string, keys ...string) (*Model, *Input, *[]string) {
	t.Helper()
	r, in := editorTestRuntime(t, text, false)
	r.BindingTimeout = time.Hour
	var calls []string
	for _, key := range keys {
		r.Register(Operation{Verb: key, Noun: "test", Run: func(_ *Runtime, _ Context, _ Arguments) (any, error) {
			calls = append(calls, key)
			return nil, nil
		}})
		r.Define("app").Mode("").Bind(key, Do("test."+key))
	}
	return NewModel(r), in, &calls
}

func dispatchTestState(t *testing.T, m *Model, in *Input, text string, calls *[]string, want ...string) {
	t.Helper()
	if m.Status != "" || string(in.Text) != text || !reflect.DeepEqual(*calls, want) {
		t.Fatalf("status=%q text=%q calls=%v; want no status, text %q, calls %v", m.Status, string(in.Text), *calls, text, want)
	}
}

// modeOf is the focused instance's full mode name (pending modes included).
func modeOf(m *Model) string { return m.Runtime.Mode.Name }

// Rule: macro_pending is exclusive; the next key names the register, even
// when an any-mode app row binds it.
func TestDispatchMacroPendingBeatsLayerBinding(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "abc", "x")
	for _, step := range []struct{ key, mode string }{{"Q", "macro_pending"}, {"x", "normal"}} {
		keytest.Press(m, step.key)
		if got := modeOf(m); got != step.mode {
			t.Fatalf("after %s: mode %q, want %q", step.key, got, step.mode)
		}
	}
	dispatchTestState(t, m, in, "abc", calls)
	if m.Runtime.Registers().Macro.Recording != "x" {
		t.Fatalf("bound key was not taken as the register name: %+v", m.Runtime.Registers().Macro)
	}
}

// Rule: an active palette takes every key; page rows and the editor grammar wait.
func TestDispatchPaletteActiveTakesKeys(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "abc")
	m.Runtime.Define("app").Mode("normal").Bind("x", Do("app.quit"))
	if err := m.Palette.Open(); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "x")
	dispatchTestState(t, m, in, "abc", calls)
	if m.Palette.Text() != "x" || m.Runtime.Quit {
		t.Fatalf("palette did not take the key: query=%q quit=%t", m.Palette.Text(), m.Runtime.Quit)
	}
	keytest.Paste(m, "yz")
	if m.Palette.Text() != "xyz" {
		t.Fatalf("paste into the palette: query=%q", m.Palette.Text())
	}
}

// Rule: a paste is one literal input event. It matches only paste rows, so
// insert mode inserts it and normal mode swallows it.
func TestDispatchPasteIsOneLiteralInput(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "", "x")
	keytest.Press(m, "i")
	keytest.Paste(m, "xq@")
	dispatchTestState(t, m, in, "xq@", calls)
	if got := modeOf(m); got != "insert" {
		t.Fatalf("paste changed the mode to %q", got)
	}
	keytest.Press(m, "esc")
	keytest.Paste(m, "x")
	dispatchTestState(t, m, in, "xq@", calls)
}

// Rule: operator_pending is exclusive; the motion reaches the editor before
// any app row. Table: (mode, key) -> (next mode, text).
func TestDispatchInputPendingOperatorBeatsLayerBinding(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "one two", "w")
	for _, step := range []struct{ key, mode, text string }{{"d", "operator_pending", "one two"}, {"w", "normal", "two"}} {
		keytest.Press(m, step.key)
		if got := modeOf(m); got != step.mode || string(in.Text) != step.text {
			t.Fatalf("after %s: mode %q text %q; want %q %q", step.key, got, string(in.Text), step.mode, step.text)
		}
	}
	dispatchTestState(t, m, in, "two", calls)
	if in.Pending() != "" {
		t.Fatalf("operator survived: %q", in.Pending())
	}
}

// Rule: count_pending is exclusive; the counted motion reaches the editor.
func TestDispatchInputCountBeatsLayerBinding(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "one two three", "w")
	keytest.Press(m, "2")
	if modeOf(m) != "count_pending" || in.Count() != 2 {
		t.Fatalf("2 did not start a count: mode=%q count=%d", modeOf(m), in.Count())
	}
	keytest.Press(m, "w")
	dispatchTestState(t, m, in, "one two three", calls)
	if in.Cursor != 8 || in.Count() != 0 || modeOf(m) != "normal" {
		t.Fatalf("2w did not move two words: cursor=%d count=%d mode=%s", in.Cursor, in.Count(), modeOf(m))
	}
}

// Rule: a fresh key that matches an app row runs it; the editor noun is the
// outermost fallback.
func TestDispatchFreshKeyPrefersLayerBinding(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "one two", "w")
	keytest.Press(m, "w")
	dispatchTestState(t, m, in, "one two", calls, "w")
	if in.Cursor != 0 {
		t.Fatalf("bound w also moved the cursor to %d", in.Cursor)
	}
}

// Rule: the modal noun binds esc in normal mode to modal.pop; on the main
// frame esc finds no row.
func TestDispatchEscPopsModalInNormalMode(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "abc")
	keytest.Press(m, "esc")
	dispatchTestState(t, m, in, "abc", calls)
	if err := m.Runtime.Push("modal", "app"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "esc")
	if len(m.Runtime.Modals()) != 0 || m.Status != "" {
		t.Fatalf("esc did not pop the modal: frames=%d status=%q", len(m.Runtime.Modals()), m.Status)
	}
}

// Rule: inside a modal, esc first leaves insert mode or a pending count (the
// editor's rows for those modes are inner); only a quiet normal esc reaches
// the modal noun.
func TestDispatchEscSettlesModalInputBeforePopping(t *testing.T) {
	r := NewRuntime()
	in := NewInput("modal-input", "abc", false)
	if err := r.AddInput(in); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.Push("modal", in.ID); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		key, mode string
		modals    int
	}{{"i", "insert", 1}, {"esc", "normal", 1}, {"3", "count_pending", 1}, {"esc", "normal", 1}, {"esc", "normal", 0}} {
		keytest.Press(m, step.key)
		if got := modeOf(m); got != step.mode || len(r.Modals()) != step.modals || m.Status != "" {
			t.Fatalf("after %s: mode %q modals %d status %q; want %q %d", step.key, got, len(r.Modals()), m.Status, step.mode, step.modals)
		}
	}
	if in.Count() != 0 {
		t.Fatalf("count survived esc: %d", in.Count())
	}
}

// Rule: a key no application row claims falls through to the editor.
func TestDispatchUnmatchedKeyFallsThroughToEditor(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "abc")
	keytest.Press(m, "x")
	dispatchTestState(t, m, in, "bc", calls)
}

// Phase 5 rule (changed deliberately from phase 0): a key sequence waits in
// a prefix mode named after the base mode and the keys so far. A key that
// fails to complete it leaves the prefix mode, runs the innermost exact
// match on the whole stack (typeahead: here the editor's own g, which enters
// g_pending), and is dispatched again, fresh: g_pending's any row cancels,
// so g x leaves the text alone, as Vim's unmapped gx would. (Before the
// whole-stack scan the editor's g was not the fallback and x deleted.) The
// prefix key itself is answered by the prefix mode, so an app any row sees
// the mismatched key, never the prefix.
func TestDispatchDropsUnmatchedBindingPrefix(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "abc", "g g")
	keytest.Press(m, "g")
	dispatchTestState(t, m, in, "abc", calls)
	if m.Runtime.Pending != "g" || modeOf(m) != "normal g" || in.Pending() != "" {
		t.Fatalf("g did not wait as a sequence prefix: pending=%q mode=%q editor=%q", m.Runtime.Pending, modeOf(m), in.Pending())
	}
	keytest.Press(m, "x")
	dispatchTestState(t, m, in, "abc", calls)
	if m.Runtime.Pending != "" || modeOf(m) != "normal" {
		t.Fatalf("prefix survived the mismatch: pending=%q mode=%q", m.Runtime.Pending, modeOf(m))
	}
	// With an any row on the app noun, the mismatched key reaches it and the
	// prefix key does not.
	m.Runtime.Register(Operation{Verb: "any", Noun: "test", Run: func(_ *Runtime, _ Context, _ Arguments) (any, error) {
		*calls = append(*calls, "any")
		return nil, nil
	}})
	m.Runtime.Define("app").Mode("normal").Pattern(Any, Do("test.any"))
	keytest.Press(m, "g")
	keytest.Press(m, "x")
	dispatchTestState(t, m, in, "abc", calls, "any")
}

// Phase 5 rule (the phase 0 finding resolved): the palette field's own rows
// are innermost, so typing reaches the query even when an any-mode app row
// binds the same key. Table: (open palette) key x -> query "x", no app call.
func TestDispatchPaletteHonorsAnyModeAppBindings(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "abc", "x")
	if err := m.Palette.Open(); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "x")
	dispatchTestState(t, m, in, "abc", calls)
	if m.Palette.Text() != "x" || !m.Palette.IsActive() {
		t.Fatalf("palette row did not win over the app any-mode row: query=%q active=%t", m.Palette.Text(), m.Palette.IsActive())
	}
}
