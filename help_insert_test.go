package ochazuke

import (
	"slices"
	"testing"

	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// helpInsertFixture is a page holding an insert-mode input, focused.
func helpInsertFixture(t *testing.T) (*Runtime, *Model, *Input) {
	t.Helper()
	r := NewRuntime()
	if _, err := r.AddPage("p", "Page", func(r *Runtime, w, h int) Block { return Text("page") }); err != nil {
		t.Fatal(err)
	}
	in := NewInput("note", "hi", false)
	if err := r.AddInput(in, Parent("p")); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 100, 30
	if err := r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("note"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetMode("insert"); err != nil {
		t.Fatal(err)
	}
	return r, m, in
}

// openHelpInInsert presses HelpInsertKey with focus in insert mode: the help
// opens over it, the input's text is untouched, and esc gives back the focus
// in insert mode.
func openHelpInInsert(t *testing.T, r *Runtime, m *Model, in *Input, where string) {
	t.Helper()
	focus, text := r.Focus, string(in.Text)
	if r.Mode.Name != "insert" {
		t.Fatalf("%s: mode %q before the key", where, r.Mode.Name)
	}
	keytest.Press(m, HelpInsertKey)
	if r.TopModal() != helpID {
		t.Fatalf("%s: %s opened %q (status %q)", where, HelpInsertKey, r.TopModal(), m.Status)
	}
	if got := string(in.Text); got != text {
		t.Fatalf("%s: the key typed: %q, was %q", where, got, text)
	}
	h := r.Instances[helpID].Component.(*helpState)
	if !slices.ContainsFunc(h.here, func(row HelpRow) bool { return row.Key == HelpInsertKey && row.Operation == "bindings.help" }) {
		t.Fatalf("%s: the help does not list its own insert key: %+v", where, h.here)
	}
	keytest.Press(m, "esc")
	if r.TopModal() == helpID || r.Focus != focus || r.Mode.Name != "insert" {
		t.Fatalf("%s: after esc modal %q focus %q mode %q", where, r.TopModal(), r.Focus, r.Mode.Name)
	}
}

func TestHelpInsertKeyOnAPageInput(t *testing.T) {
	r, m, in := helpInsertFixture(t)
	openHelpInInsert(t, r, m, in, "page input")
	// Normal mode keeps ? and has no insert row answering.
	if err := r.SetMode("normal"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "?")
	if r.TopModal() != helpID {
		t.Fatalf("? in normal opened %q", r.TopModal())
	}
}

func TestHelpInsertKeyInAModalPrompt(t *testing.T) {
	r, m, _ := helpInsertFixture(t)
	ask := NewInput("answer", "yes", false)
	if _, err := r.OpenModal("ask", Payload{}, WithInput(ask, Kind("ochazuke.prompt"))); err != nil {
		t.Fatal(err)
	}
	openHelpInInsert(t, r, m, ask, "modal prompt")
	if r.TopModal() != "ask" {
		t.Fatalf("the prompt closed: %q", r.TopModal())
	}
}

func TestHelpInsertKeyInThePalette(t *testing.T) {
	r, m, _ := helpInsertFixture(t)
	if err := r.SetMode("normal"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, ":")
	keytest.Type(m, "pa")
	if r.Focus != paletteLineID {
		t.Fatalf("palette focus %q", r.Focus)
	}
	openHelpInInsert(t, r, m, r.Input(paletteLineID), "palette")
}

func TestHelpInsertKeyInAChooser(t *testing.T) {
	r, m, _ := helpInsertFixture(t)
	if _, err := r.OpenChooser(ChooserSpec{Kind: "k", Rows: []ChooserRow{{ID: "a", Label: "alpha"}}}); err != nil {
		t.Fatal(err)
	}
	keytest.Type(m, "al")
	in := r.Input(r.Focus)
	if in == nil {
		t.Fatalf("chooser focus %q is no input", r.Focus)
	}
	openHelpInInsert(t, r, m, in, "chooser")
}

// No noun but the editor binds the key, and no noun answers every key in
// insert mode (an any row) above the editor, so no inner level of an input
// swallows it: the framework's, the model's and the demo-free runtime's
// nouns (forms, outlines, completion, the palette, choosers, settings).
func TestHelpInsertKeyIsNotShadowed(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	for name, noun := range r.Nouns {
		for mode, rows := range noun.Bindings {
			if mode != "insert" && mode != "" {
				continue
			}
			for _, b := range rows {
				if b.Keys == HelpInsertKey && (name != "ochazuke.editor" || b.Request.Key() != "ochazuke.bindings.help") {
					t.Errorf("%s binds %s in %q: %s", name, HelpInsertKey, mode, b.Request.Key())
				}
				if b.Pattern != "" && matchesHelpKey(b.Pattern) && name != "ochazuke.editor" && name != "ochazuke.confirm" {
					t.Errorf("%s's %q row %s answers %s", name, mode, b.Pattern, HelpInsertKey)
				}
			}
		}
	}
}

func matchesHelpKey(pattern string) bool {
	return slices.Contains(classOf(MacroKey{Key: HelpInsertKey}), pattern)
}

func TestHelpInsertKeyInAFormField(t *testing.T) {
	r, m, _, _ := recordFixture(t, nil)
	keytest.Press(m, "q")
	if _, err := r.OpenRecordEditor("edit-task", RecordSpec{Title: "Edit task", Fields: taskFields(), Insert: true, Save: func(RecordWrite) Result { return Done(nil) }}); err != nil {
		t.Fatal(err)
	}
	openHelpInInsert(t, r, m, r.Input(r.Focus), "form field")
}
