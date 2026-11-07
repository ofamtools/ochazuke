package ochazuke

import (
	"reflect"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// A step the input takes through a state that was never written (not valid)
// still lands on the record's step, so nothing is taken twice.
func TestRecordUndoChainThroughAnInvalidState(t *testing.T) {
	_, m, ed, _ := recordFixture(t, nil)
	keytest.Press(m, "j") // status
	keytest.Press(m, "j") // estimate
	field := ed.field("estimate")
	typed := func(text string) {
		keytest.Press(m, "A")
		m.Update(keytest.Msg(text, true, false))
		keytest.Press(m, "esc")
	}
	typed("5")
	typed("x") // 5x is not a whole number: not written, no record step
	keytest.Press(m, "x")
	typed("6")
	if ed.Written()["estimate"] != "56" || len(ed.undo) != 2 {
		t.Fatalf("written %q, undo %+v", ed.Written()["estimate"], ed.undo)
	}
	for _, want := range []struct{ shown, written string }{{"5", "5"}, {"5x", "5"}, {"5", "5"}, {"", ""}, {"", ""}} {
		keytest.Press(m, "u")
		if field.text() != want.shown || ed.Written()["estimate"] != want.written {
			t.Fatalf("after u: shown %q written %q, want %+v", field.text(), ed.Written()["estimate"], want)
		}
	}
	if len(ed.undo) != 0 || len(ed.redo) != 2 {
		t.Fatalf("undo %+v redo %+v", ed.undo, ed.redo)
	}
	for _, want := range []string{"5", "5x", "5", "56", "56"} {
		keytest.Press(m, "ctrl+r")
		if field.text() != want {
			t.Fatalf("after ctrl+r: shown %q, want %q", field.text(), want)
		}
	}
	if len(ed.undo) != 2 || len(ed.redo) != 0 {
		t.Fatalf("undo %+v redo %+v", ed.undo, ed.redo)
	}
}

// A new edit after an undo starts a branch: redo has nothing to bring back
// and the chain still ends where it began.
func TestRecordUndoChainNewEditDropsTheRedo(t *testing.T) {
	_, m, ed, _ := recordFixture(t, nil)
	field := ed.field("title")
	typed := func(text string) {
		keytest.Press(m, "A")
		m.Update(keytest.Msg(text, true, false))
		keytest.Press(m, "esc")
	}
	typed("1")
	keytest.Press(m, "u")
	typed("2")
	if len(ed.redo) != 0 || len(ed.undo) != 1 {
		t.Fatalf("undo %+v redo %+v", ed.undo, ed.redo)
	}
	keytest.Press(m, "ctrl+r")
	if field.text() != "buy milk2" {
		t.Fatalf("redo past the new edit: %q", field.text())
	}
	keytest.Press(m, "u")
	keytest.Press(m, "u")
	if field.text() != "buy milk" || len(ed.undo) != 0 {
		t.Fatalf("shown %q, undo %+v", field.text(), ed.undo)
	}
}

// A form undo walks the input's tree rather than adding to it, so the
// input has its redo and no new undo.
func TestRecordFormUndoWalksTheInput(t *testing.T) {
	r, m, ed, _ := recordFixture(t, nil)
	field := ed.field("title")
	keytest.Press(m, "A")
	m.Update(keytest.Msg("!", true, false))
	keytest.Press(m, "esc")
	keytest.Press(m, "j") // status: a choice, so u is the form's
	nodes := len(field.input.History)
	keytest.Press(m, "u")
	if r.Focus != field.id || field.text() != "buy milk" {
		t.Fatalf("focus %s, shown %q", r.Focus, field.text())
	}
	if len(field.input.History) != nodes || field.input.undoChangesText() || !field.input.redoChangesText() {
		t.Fatalf("tree %+v head %d", field.input.History, field.input.Head)
	}
}

// Text a person sees undone never comes back from before an outside write.
func TestRecordOutsideWriteStartsTheInputOver(t *testing.T) {
	r := NewRuntime()
	loaded := Record{"title": "one"}
	ed, err := r.AddRecordEditor("ed", RecordSpec{Fields: []RecordField{{Name: "title"}}, Load: func() (Record, error) { return loaded, nil }, Save: func(RecordWrite) Result { return Done(nil) }})
	if err != nil {
		t.Fatal(err)
	}
	field := ed.field("title")
	if err := ed.set(field, "two"); err != nil {
		t.Fatal(err)
	}
	if err := ed.seal(field); err != nil {
		t.Fatal(err)
	}
	loaded = Record{"title": "three"}
	if err := ed.Reload(); err != nil {
		t.Fatal(err)
	}
	if field.text() != "three" || field.input.undoChangesText() || len(ed.undo) != 0 {
		t.Fatalf("shown %q, tree %+v, undo %+v", field.text(), field.input.History, ed.undo)
	}
}

func TestTakeStep(t *testing.T) {
	step := func(field, from, to string) recordStep { return recordStep{field: field, from: from, to: to} }
	tests := []struct {
		name  string
		steps []recordStep
		want  []recordStep
		found bool
	}{
		{"the whole step, under another field's", []recordStep{step("f", "a", "b"), step("g", "x", "y")}, []recordStep{step("g", "x", "y")}, true},
		{"part of a step", []recordStep{step("f", "z", "b")}, []recordStep{step("f", "z", "a")}, true},
		{"a step that does not start here", []recordStep{step("f", "a", "c")}, []recordStep{step("f", "a", "c")}, false},
		{"none of the field", []recordStep{step("g", "x", "y")}, []recordStep{step("g", "x", "y")}, false},
		{"one change of a group", []recordStep{{changes: []recordStep{step("f", "a", "b"), step("g", "x", "y")}}}, []recordStep{{changes: []recordStep{step("g", "x", "y")}}}, true},
		{"the last change of a group", []recordStep{{changes: []recordStep{step("f", "a", "b")}}}, []recordStep{}, true},
	}
	for _, tt := range tests {
		got, found := takeStep(tt.steps, "f", "b", "a")
		if found != tt.found || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %+v, %v", tt.name, got, found)
		}
	}
}

// Going back to an equal text takes the direction the history is moving in.
func TestInputWalkToFollowsTheDirection(t *testing.T) {
	in := NewInput("in", "a", false)
	for _, text := range []string{"ab", "a", "ab"} {
		if err := in.Replace(0, len(in.Text), text); err != nil {
			t.Fatal(err)
		}
	}
	if !in.walkTo("a", false) || in.Head != 2 {
		t.Fatalf("back to a: head %d", in.Head)
	}
	if !in.walkTo("ab", true) || in.Head != 3 {
		t.Fatalf("forward to ab: head %d", in.Head)
	}
	if in.walkTo("nowhere", false) || string(in.Text) != "ab" || in.walk != 0 {
		t.Fatalf("no such state: text %q head %d walk %d", string(in.Text), in.Head, in.walk)
	}
}
