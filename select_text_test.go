package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// selectFixture is a focused single-line input holding text with its whole
// text selected.
func selectFixture(t *testing.T, text string) (*Runtime, *Input) {
	t.Helper()
	r := NewRuntime()
	in := NewInput("field", text, false)
	if err := r.AddInput(in); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus(in.ID); err != nil {
		t.Fatal(err)
	}
	if err := in.SelectAll(); err != nil {
		t.Fatal(err)
	}
	return r, in
}

func runInput(t *testing.T, r *Runtime, verb string, args Arguments) {
	t.Helper()
	if _, err := r.Execute(Request{Noun: "ochazuke.input", Verb: verb, Args: args}); err != nil {
		t.Fatalf("%s: %v", verb, err)
	}
}

// SelectText enters insert with the range open and the cursor at its end;
// the snapshot reports it.
func TestSelectTextOpensInInsert(t *testing.T) {
	r, in := selectFixture(t, "new-key")
	if in.Mode != "insert" || r.Mode.Name != "insert" || in.Cursor != 7 {
		t.Fatalf("mode %q/%q cursor %d", in.Mode, r.Mode.Name, in.Cursor)
	}
	if lo, hi, ok := in.SelectedText(); !ok || lo != 0 || hi != 7 {
		t.Fatalf("selected %d %d %v", lo, hi, ok)
	}
	if got := in.Snapshot()["typeOver"]; got == nil {
		t.Fatal("snapshot has no typeOver")
	}
	if err := in.SelectText(3, 2); err == nil {
		t.Fatal("a reversed range is accepted")
	}
}

// Typing replaces the range and continues in insert; one undo restores
// the placeholder.
func TestSelectTextTypingReplaces(t *testing.T) {
	r, in := selectFixture(t, "new-key")
	runInput(t, r, "insert", Arguments{"text": "i"})
	runInput(t, r, "insert", Arguments{"text": "d"})
	if string(in.Text) != "id" || in.Mode != "insert" || in.Cursor != 2 {
		t.Fatalf("text %q mode %q cursor %d", string(in.Text), in.Mode, in.Cursor)
	}
	if _, _, ok := in.SelectedText(); ok {
		t.Fatal("range still open after typing")
	}
	in.SetMode("normal")
	runInput(t, r, "undo", nil)
	if string(in.Text) != "new-key" {
		t.Fatalf("undo gave %q", string(in.Text))
	}
}

// Backspace and delete remove the range; a literal paste replaces it.
func TestSelectTextDeleteAndPaste(t *testing.T) {
	for _, direction := range []string{"backward", "forward"} {
		r, in := selectFixture(t, "new-key")
		runInput(t, r, "delete", Arguments{"direction": direction})
		if string(in.Text) != "" || in.Mode != "insert" {
			t.Fatalf("%s: text %q mode %q", direction, string(in.Text), in.Mode)
		}
	}
	r, in := selectFixture(t, "new-key")
	if _, err := r.Execute(Request{Noun: "ochazuke.register", Verb: "set", Args: Arguments{"name": "a", "text": "name"}}); err != nil {
		t.Fatal(err)
	}
	runInput(t, r, "paste", Arguments{"register": "a", "before": true, "literal": true})
	if string(in.Text) != "name" || in.Cursor != 4 {
		t.Fatalf("paste gave %q cursor %d", string(in.Text), in.Cursor)
	}
}

// A motion, esc or an undo closes the range and keeps the text; left and
// right land on its edges.
func TestSelectTextLeavingKeepsText(t *testing.T) {
	for _, tc := range []struct {
		motion string
		cursor int
	}{{"left", 0}, {"right", 7}, {"line-start", 0}, {"word-previous", 4}} {
		r, in := selectFixture(t, "new-key")
		runInput(t, r, "move", Arguments{"motion": tc.motion})
		if _, _, ok := in.SelectedText(); ok || string(in.Text) != "new-key" || in.Cursor != tc.cursor || in.Mode != "insert" {
			t.Fatalf("%s: text %q cursor %d mode %q open %v", tc.motion, string(in.Text), in.Cursor, in.Mode, ok)
		}
		runInput(t, r, "insert", Arguments{"text": "x"})
		if strings.Count(string(in.Text), "x") != 1 || len(in.Text) != 8 {
			t.Fatalf("%s: typing after the motion replaced: %q", tc.motion, string(in.Text))
		}
	}
	r, in := selectFixture(t, "new-key")
	if err := r.SetMode("normal"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := in.SelectedText(); ok || string(in.Text) != "new-key" {
		t.Fatalf("esc: text %q open %v", string(in.Text), ok)
	}
}

// The range is drawn reverse, as a visual selection is; nothing is
// reversed once it closes.
func TestSelectTextDrawsReverse(t *testing.T) {
	r, in := selectFixture(t, "new-key ok")
	if err := in.SelectText(0, 7); err != nil {
		t.Fatal(err)
	}
	text := InputBlock(in, 20, 1).Text
	if !strings.HasPrefix(text, "\x1b[7mn") || !strings.Contains(text, "y\x1b[27m ok") {
		t.Fatalf("drawn %q", text)
	}
	runInput(t, r, "move", Arguments{"motion": "right"})
	if text := InputBlock(in, 20, 1).Text; strings.Contains(text, "\x1b[7m") {
		t.Fatalf("closed range still drawn %q", text)
	}
}

// outline.add {text, select} adds a node holding the placeholder and opens
// it selected: typing replaces it, esc keeps it; plain o is unchanged.
func TestOutlineAddSelectPlaceholder(t *testing.T) {
	r, m, tree, writes := treeFixture(t)
	add := Request{Noun: "ochazuke.outline", Verb: "add", Args: Arguments{"where": "below", "text": "new-key", "select": true}}
	if _, err := r.Execute(add); err != nil {
		t.Fatal(err)
	}
	in := r.Input(r.Focus)
	if in == nil || string(in.Text) != "new-key" {
		t.Fatalf("focus %q", r.Focus)
	}
	if lo, hi, ok := in.SelectedText(); !ok || lo != 0 || hi != 7 {
		t.Fatalf("selected %d %d %v", lo, hi, ok)
	}
	for _, k := range []string{"i", "d"} {
		m.Update(keytest.Msg(k, true, false))
	}
	keytest.Press(m, "esc")
	want := []string{"add t-n1 under root at 1: new-key", "rename t-n1: id"}
	if !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	// esc keeps the placeholder: no rename is written.
	if _, err := r.Execute(add); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "esc")
	if n, _ := tree.Selected(); n.Label != "new-key" || len(*writes) != 3 {
		t.Fatalf("selected %+v writes %v", n, *writes)
	}
	// o without select opens an empty line with nothing selected.
	keytest.Press(m, "o")
	if in := r.Input(r.Focus); in == nil || len(in.Text) != 0 {
		t.Fatalf("o opened %q", r.Focus)
	} else if _, _, ok := in.SelectedText(); ok {
		t.Fatal("o opened a select range")
	}
}

// outline.edit {select} and Tree.Edit open an existing line selected.
func TestOutlineEditSelect(t *testing.T) {
	r, m, tree, writes := treeFixture(t)
	if _, err := r.Execute(Request{Noun: "ochazuke.outline", Verb: "edit", Args: Arguments{"select": true}}); err != nil {
		t.Fatal(err)
	}
	m.Update(keytest.Msg("z", true, false))
	keytest.Press(m, "esc")
	if want := []string{"rename a: z"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	if err := tree.Edit("a", true); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := r.Input(r.Focus).SelectedText(); !ok {
		t.Fatal("Tree.Edit opened no select range")
	}
	tree.OnChange = nil
	if err := tree.Edit("a", true); err == nil {
		t.Fatal("Tree.Edit on a read-only tree")
	}
}
