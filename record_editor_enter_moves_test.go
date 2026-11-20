package ochazuke

import (
	"strings"
	"testing"
)

// A form with EnterMoves never runs on enter: enter in its text area is a
// new line, in a single-line field it moves on (insert and normal), and
// alt+enter runs it once from any field, shown in the hints.
func TestRecordEnterMovesRunsOnlyOnAltEnter(t *testing.T) {
	r := NewRuntime()
	r.Instance("a", Kind("x"))
	_ = r.SetFocus("a")
	var runs []Record
	spec := RecordSpec{Insert: true, EnterMoves: true,
		Fields: []RecordField{
			{Name: "q", Multiline: true, Parameter: Parameter{Type: "string", Required: true}},
			{Name: "h", Parameter: Parameter{Type: "string"}},
			{Name: "m", Parameter: Parameter{Type: "string"}},
		},
		Submit: func(rec Record) Result { runs = append(runs, rec); return Done(nil) }}
	if _, err := r.OpenRecordEditor("f", spec); err != nil {
		t.Fatal(err)
	}
	typeKeys(t, r, "one")
	formKeys(t, r, "enter")
	typeKeys(t, r, "two")
	formKeys(t, r, "tab", "enter")
	if r.Focus != "f/m" || r.Mode.Name != "insert" {
		t.Fatalf("enter in a field moves on: focus=%q mode=%q", r.Focus, r.Mode.Name)
	}
	formKeys(t, r, "esc", "k", "enter")
	if len(runs) != 0 || r.TopModal() != "f" {
		t.Fatalf("enter ran the form: runs=%v top=%q", runs, r.TopModal())
	}
	if r.Focus != "f/m" {
		t.Fatalf("normal enter moves on: focus=%q", r.Focus)
	}
	if hints := BindingHints(r, r.Focus); !strings.Contains(hints, "alt+enter run") {
		t.Fatalf("hints %q lack alt+enter run", hints)
	}
	formKeys(t, r, "alt+enter")
	if len(runs) != 1 || runs[0]["q"] != "one\ntwo" || r.TopModal() != "" {
		t.Fatalf("alt+enter: runs=%v top=%q", runs, r.TopModal())
	}
}

// alt+enter runs from the text area in insert too.
func TestRecordEnterMovesAltEnterFromTextArea(t *testing.T) {
	r := NewRuntime()
	r.Instance("a", Kind("x"))
	_ = r.SetFocus("a")
	runs := 0
	spec := RecordSpec{Insert: true, EnterMoves: true,
		Fields: []RecordField{{Name: "q", Multiline: true, Parameter: Parameter{Type: "string", Required: true}}},
		Submit: func(Record) Result { runs++; return Done(nil) }}
	if _, err := r.OpenRecordEditor("f", spec); err != nil {
		t.Fatal(err)
	}
	typeKeys(t, r, "hi")
	formKeys(t, r, "alt+enter")
	if runs != 1 || r.TopModal() != "" || r.Focus != "a" {
		t.Fatalf("runs=%d top=%q focus=%q", runs, r.TopModal(), r.Focus)
	}
}
