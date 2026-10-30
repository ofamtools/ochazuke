package ochazuke

import (
	"strings"
	"testing"
)

func readOnlyTestRuntime(t *testing.T, text string, multiline bool) (*Runtime, *Input) {
	t.Helper()
	r := NewRuntime()
	in := NewInput("viewer", text, multiline)
	if err := r.AddInput(in, ReadOnly()); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus(in.ID); err != nil {
		t.Fatal(err)
	}
	if !in.ReadOnly {
		t.Fatal("ReadOnly option did not mark the input")
	}
	return r, in
}

// Every edit key is a silent no-op at a read-only input: the text, the
// undo tree and the registers stay, insert mode is never entered, and no
// key reports an error.
func TestReadOnlyInputRefusesEditsSilently(t *testing.T) {
	const text = "one two\nthree\nfour"
	for _, keys := range [][]string{
		{"i", "X", "esc"}, {"a", "X"}, {"o", "X"}, {"O"}, {"A"}, {"I"},
		{"x"}, {"delete"}, {"d", "d"}, {"d", "w"}, {"c", "c", "X"}, {"c", "w"},
		{"v", "l", "d"}, {"V", "x"}, {"v", "c"}, {"p"}, {"P"}, {"u"}, {"ctrl+r"},
		{"ctrl+v", "j", "d"},
	} {
		t.Run(strings.Join(keys, " "), func(t *testing.T) {
			r, in := readOnlyTestRuntime(t, text, true)
			if err := r.storeYank(Yank{Text: "kept", Shape: "char"}, "", false); err != nil {
				t.Fatal(err)
			}
			for _, key := range keys {
				if _, err := editorTestDispatch(r, key); err != nil {
					t.Fatalf("key %q reported %v", key, err)
				}
				if in.Mode == "insert" || r.Mode.Name == "insert" {
					t.Fatalf("key %q entered insert mode", key)
				}
			}
			if string(in.Text) != text {
				t.Fatalf("text changed to %q", string(in.Text))
			}
			if len(in.History) != 1 {
				t.Fatalf("undo tree grew: %d nodes", len(in.History))
			}
			if got := r.Registers().Named["\""].Yank.Text; got != "kept" {
				t.Fatalf("unnamed register %q: a refused edit yanked", got)
			}
		})
	}
}

// Motions, the visual modes and yanks work as in any input.
func TestReadOnlyInputMovesSelectsAndYanks(t *testing.T) {
	r, in := readOnlyTestRuntime(t, "one two\nthree\nfour", true)
	editorTestKeys(t, r, "w")
	if in.Cursor != 4 {
		t.Fatalf("w: cursor %d", in.Cursor)
	}
	editorTestKeys(t, r, "v", "e", "y")
	if got := r.Registers().Named["\""].Yank; got.Text != "two" || got.Shape != "char" {
		t.Fatalf("v e y yanked %+v", got)
	}
	if in.Mode != "normal" {
		t.Fatalf("mode after yank %q", in.Mode)
	}
	editorTestKeys(t, r, "j", "y", "y")
	if got := r.Registers().Named["\""].Yank; got.Text != "three\n" || got.Shape != "line" {
		t.Fatalf("yy yanked %+v", got)
	}
	editorTestKeys(t, r, "g", "g", "V", "j", "y")
	if got := r.Registers().Named["0"].Yank.Text; got != "one two\nthree\n" {
		t.Fatalf("V j y yanked %q", got)
	}
	editorTestKeys(t, r, "\"", "a", "y", "w")
	if got := r.Registers().Named["a"].Yank.Text; got != "one " {
		t.Fatalf("\"ayw yanked %q", got)
	}
	editorTestKeys(t, r, "G", "$")
	if in.Cursor != len(in.Text)-1 {
		t.Fatalf("G $: cursor %d", in.Cursor)
	}
}

// The editing verbs are unavailable at a read-only input, so their rows
// leave the hints and the walk; yank and select stay. A writable input
// keeps them all.
func TestReadOnlyInputHidesEditingRows(t *testing.T) {
	r, in := readOnlyTestRuntime(t, "text", false)
	NewModel(r)
	hints := BindingHints(r, in.ID)
	for _, op := range []string{"input.begin", "input.delete", "input.paste", "input.undo", "input.redo"} {
		if strings.Contains(hints, op) {
			t.Errorf("read-only hints name %s: %q", op, hints)
		}
	}
	if !strings.Contains(hints, "input.select") {
		t.Errorf("read-only hints lost input.select: %q", hints)
	}
	if r.Available(Request{Noun: "ochazuke.input", Verb: "operator", Args: Arguments{"operator": "d"}}, in.ID, r.Mode) {
		t.Error("a delete operator is available at a read-only input")
	}
	if !r.Available(Request{Noun: "ochazuke.input", Verb: "operator", Args: Arguments{"operator": "y"}}, in.ID, r.Mode) {
		t.Error("the yank operator is unavailable at a read-only input")
	}
	in.ReadOnly = false
	if hints := BindingHints(r, in.ID); !strings.Contains(hints, "input.begin") {
		t.Errorf("a writable input lost input.begin: %q", hints)
	}
}

// The API: Replace and SelectText leave a read-only input as it was, and
// SetMode("insert") keeps its mode.
func TestReadOnlyInputAPI(t *testing.T) {
	in := NewInput("viewer", "abc", false)
	in.ReadOnly = true
	if err := in.Replace(0, 3, "x"); err != nil || string(in.Text) != "abc" {
		t.Fatalf("Replace: %v %q", err, string(in.Text))
	}
	if err := in.SelectAll(); err != nil {
		t.Fatal(err)
	}
	if _, _, open := in.SelectedText(); open || in.Mode != "normal" {
		t.Fatalf("SelectAll opened a range or changed the mode to %q", in.Mode)
	}
	in.SetMode("insert")
	if in.Mode != "normal" {
		t.Fatalf("SetMode(insert) gave %q", in.Mode)
	}
	if in.Snapshot()["readOnly"] != true {
		t.Fatal("snapshot lacks readOnly")
	}
}

// SetText refreshes a read-only input from the program: the new text is
// the undo root, the cursor and anchor are clamped into it as normal mode
// leaves them, the same text again keeps the history, and a single-line
// input keeps its first line.
func TestInputSetText(t *testing.T) {
	in := NewInput("viewer", "alpha beta gamma", false)
	in.ReadOnly = true
	in.Cursor, in.Anchor = 12, 14
	in.SetText("short")
	if string(in.Text) != "short" || in.Cursor != 4 || in.Anchor != 4 || len(in.History) != 1 || in.History[0].Text != "short" || in.Head != 0 {
		t.Fatalf("SetText: text %q cursor %d anchor %d history %+v head %d", string(in.Text), in.Cursor, in.Anchor, in.History, in.Head)
	}
	in.Cursor = 1
	in.SetText("short")
	if in.Cursor != 1 || len(in.History) != 1 {
		t.Fatalf("same text: cursor %d history %d", in.Cursor, len(in.History))
	}
	in.SetText("one\ntwo")
	if string(in.Text) != "one" {
		t.Fatalf("single-line SetText kept %q", string(in.Text))
	}
	multi := NewInput("m", "", true)
	multi.SetText("one\ntwo")
	if string(multi.Text) != "one\ntwo" || multi.Cursor != 0 {
		t.Fatalf("multiline SetText: %q cursor %d", string(multi.Text), multi.Cursor)
	}
}

// A focused read-only input owns the editor's keys: a page row on a key
// the editor binds (p, an edit the input refuses; w, a motion) never runs
// from it, the refused key is consumed, and the hint line does not offer
// the page row. A key the editor does not bind still reaches the page.
func TestReadOnlyInputOwnsEditorKeys(t *testing.T) {
	r := NewRuntime()
	if _, err := r.AddPage("p", "P", func(*Runtime, int, int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	in := NewInput("viewer", "one two", false)
	if err := r.AddInput(in, Parent("p"), ReadOnly()); err != nil {
		t.Fatal(err)
	}
	ran := map[string]int{}
	for _, verb := range []string{"paste", "write", "reload"} {
		r.Define("app").Verb(verb, func(Ctx) Result { ran[verb]++; return Done(nil) })
	}
	r.Define("app").Mode("normal").Bind("p", Do("app.paste")).Bind("w", Do("app.write")).Bind("R", Do("app.reload"))
	if err := r.SetFocus(in.ID); err != nil {
		t.Fatal(err)
	}
	NewModel(r)
	if hints := BindingHints(r, in.ID); strings.Contains(hints, "app.paste") || strings.Contains(hints, "app.write") || !strings.Contains(hints, "app.reload") {
		t.Fatalf("hints: %q", hints)
	}
	if outcome, err := r.DispatchOutcome(MacroKey{Key: "p"}); err != nil || outcome != KeyConsumed {
		t.Fatalf("p: %v %v", outcome, err)
	}
	if _, err := r.Dispatch(MacroKey{Key: "w"}); err != nil || in.Cursor != 4 {
		t.Fatalf("w: cursor %d, %v", in.Cursor, err)
	}
	if _, err := r.Dispatch(MacroKey{Key: "R"}); err != nil {
		t.Fatal(err)
	}
	if ran["paste"] != 0 || ran["write"] != 0 || ran["reload"] != 1 {
		t.Fatalf("ran %v", ran)
	}
	in.ReadOnly = false
	if _, err := r.Dispatch(MacroKey{Key: "p"}); err != nil || ran["paste"] != 1 {
		t.Fatalf("a writable input's page row: ran %v, %v", ran, err)
	}
}
