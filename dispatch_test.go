package ochazuke

import (
	"reflect"
	"testing"
	"time"

	"ochazuke/internal/keytest"
)

// dispatchTestModel is one focused single-line editor holding text, with each
// key bound in the app layer (any mode) to a recorded test operation. These
// tests lock the order of Model.dispatchKey as it stands before the migration.
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
		r.Layers["app"].Bindings = append(r.Layers["app"].Bindings, Binding{Keys: key, Request: Request{Verb: key, Noun: "test"}})
	}
	return NewModel(r), in, &calls
}

func dispatchTestState(t *testing.T, m *Model, in *Input, text string, calls *[]string, want ...string) {
	t.Helper()
	if m.Status != "" || string(in.Text) != text || !reflect.DeepEqual(*calls, want) {
		t.Fatalf("status=%q text=%q calls=%v; want no status, text %q, calls %v", m.Status, string(in.Text), *calls, text, want)
	}
}

// Rule: a pending macro prefix consumes the next key before bindings or the editor see it.
func TestDispatchMacroPendingBeatsLayerBinding(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "abc", "x")
	keytest.Press(m, "q")
	if m.Runtime.Macro.Pending != "q" {
		t.Fatalf("q did not start a macro prefix: %+v", m.Runtime.Macro)
	}
	keytest.Press(m, "x")
	dispatchTestState(t, m, in, "abc", calls)
	if m.Runtime.Macro.Recording != "x" || m.Runtime.Macro.Pending != "" {
		t.Fatalf("bound key was not taken as the register name: %+v", m.Runtime.Macro)
	}
}

// Rule: an active palette takes every key; page bindings and the editor grammar wait.
func TestDispatchPaletteActiveTakesKeys(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "abc")
	m.Runtime.Layers["app"].Bindings = append(m.Runtime.Layers["app"].Bindings, Binding{Mode: "normal", Keys: "x", Request: Request{Verb: "quit", Noun: "app"}})
	if err := m.Palette.Open(); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "x")
	dispatchTestState(t, m, in, "abc", calls)
	if m.Palette.query != "x" || m.Runtime.Quit {
		t.Fatalf("palette did not take the key: query=%q quit=%t", m.Palette.query, m.Runtime.Quit)
	}
	keytest.Paste(m, "yz")
	if m.Palette.query != "xyz" {
		t.Fatalf("paste into the palette: query=%q", m.Palette.query)
	}
}

// Rule: a paste is one literal input event. It never matches a binding, a
// macro prefix, or an editor command, and normal mode swallows it.
func TestDispatchPasteIsOneLiteralInput(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "", "x")
	keytest.Press(m, "i")
	keytest.Paste(m, "xq@")
	dispatchTestState(t, m, in, "xq@", calls)
	if m.Runtime.Macro.Pending != "" {
		t.Fatalf("paste started a macro prefix: %+v", m.Runtime.Macro)
	}
	keytest.Press(m, "esc")
	keytest.Paste(m, "x")
	dispatchTestState(t, m, in, "xq@", calls)
}

// Rule: an editor with a pending operator takes the next key before layer bindings.
func TestDispatchInputPendingOperatorBeatsLayerBinding(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "one two", "w")
	keytest.Press(m, "d")
	if in.Pending != "d" {
		t.Fatalf("d did not start an operator: pending=%q", in.Pending)
	}
	keytest.Press(m, "w")
	dispatchTestState(t, m, in, "two", calls)
}

// Rule: an editor with a pending count takes the next key before layer bindings.
func TestDispatchInputCountBeatsLayerBinding(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "one two three", "w")
	keytest.Press(m, "2")
	if in.Count != 2 {
		t.Fatalf("2 did not start a count: count=%d", in.Count)
	}
	keytest.Press(m, "w")
	dispatchTestState(t, m, in, "one two three", calls)
	if in.Cursor != 8 || in.Count != 0 {
		t.Fatalf("2w did not move two words: cursor=%d count=%d", in.Cursor, in.Count)
	}
}

// Rule: a fresh key that matches a layer binding runs it instead of the editor grammar.
func TestDispatchFreshKeyPrefersLayerBinding(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "one two", "w")
	keytest.Press(m, "w")
	dispatchTestState(t, m, in, "one two", calls, "w")
	if in.Cursor != 0 {
		t.Fatalf("bound w also moved the cursor to %d", in.Cursor)
	}
}

// Rule: esc on a modal in normal mode pops it; on the main frame it is a plain editor key.
func TestDispatchEscPopsModalInNormalMode(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "abc")
	keytest.Press(m, "esc")
	dispatchTestState(t, m, in, "abc", calls)
	if err := m.Runtime.Push(&Frame{ID: "modal", Context: Context{Path: []string{"app"}}}); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "esc")
	if len(m.Runtime.Frames) != 1 || m.Status != "" {
		t.Fatalf("esc did not pop the modal: frames=%d status=%q", len(m.Runtime.Frames), m.Status)
	}
}

// Rule: inside a modal, esc first leaves insert mode or clears a pending count;
// only a quiet normal-mode esc pops the modal.
func TestDispatchEscSettlesModalInputBeforePopping(t *testing.T) {
	r := NewRuntime()
	in := NewInput("modal-input", "abc", false)
	if err := r.AddInput(in, []string{"app"}); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.Push(&Frame{ID: "modal", Context: Context{Path: []string{"app"}, InputID: in.ID}}); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "i")
	keytest.Press(m, "esc")
	if in.Mode != "normal" || len(r.Frames) != 2 {
		t.Fatalf("esc in insert mode: mode=%q frames=%d", in.Mode, len(r.Frames))
	}
	keytest.Press(m, "3")
	keytest.Press(m, "esc")
	if in.Count != 0 || len(r.Frames) != 2 {
		t.Fatalf("esc with a pending count: count=%d frames=%d", in.Count, len(r.Frames))
	}
	keytest.Press(m, "esc")
	if len(r.Frames) != 1 || m.Status != "" {
		t.Fatalf("quiet esc did not pop: frames=%d status=%q", len(r.Frames), m.Status)
	}
}

// Rule: a key no binding claims falls through to the editor grammar.
func TestDispatchUnmatchedKeyFallsThroughToEditor(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "abc")
	keytest.Press(m, "x")
	dispatchTestState(t, m, in, "bc", calls)
}

// Current behavior, changes deliberately in phase 5: a binding prefix that the
// next key fails to complete is dropped. Only the mismatched key reaches the
// editor; the prefix itself is never offered to the editor grammar. Phase 5
// hands the unmatched sequence to the innermost grammar before dropping it.
func TestDispatchDropsUnmatchedBindingPrefix(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "abc", "g g")
	keytest.Press(m, "g")
	dispatchTestState(t, m, in, "abc", calls)
	if m.Runtime.Pending != "g" || in.Pending != "" {
		t.Fatalf("g did not wait as a binding prefix: pending=%q editor=%q", m.Runtime.Pending, in.Pending)
	}
	keytest.Press(m, "x")
	dispatchTestState(t, m, in, "bc", calls)
	if m.Runtime.Pending != "" || in.Pending != "" {
		t.Fatalf("prefix survived the mismatch: pending=%q editor=%q", m.Runtime.Pending, in.Pending)
	}
}

// Current behavior: the palette frame's path still includes app, so an app
// binding declared for any mode runs inside the open palette and the key
// never reaches the query. Mode-specific page bindings do not (see above).
func TestDispatchPaletteHonorsAnyModeAppBindings(t *testing.T) {
	m, in, calls := dispatchTestModel(t, "abc", "x")
	if err := m.Palette.Open(); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "x")
	dispatchTestState(t, m, in, "abc", calls, "x")
	if m.Palette.query != "" || !m.Palette.IsActive() {
		t.Fatalf("any-mode binding did not win over the palette query: query=%q active=%t", m.Palette.query, m.Palette.IsActive())
	}
}
