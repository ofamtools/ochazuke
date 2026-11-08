package ochazuke

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// A noun-typed parameter's picker shows each thing's human label first and
// its short id dim at the end, matches on either, and opens on the value the
// field holds (a Focus parameter holds the focused thing).
func TestNounPickerShowsHumanLabels(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 90, 24
	var ed *RecordEditor
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return ed.Block(w, h) }); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	target := Parameter{Type: "string", Noun: "question", Free: true,
		Enum:       []string{"question/q-aaaa", "question/q-bbbb", "question/q-cccc"},
		EnumLabels: map[string]string{"question/q-aaaa": "Ship the Decisions page?", "question/q-bbbb": "Rename the verb?", "question/q-cccc": "Drop the alias?"}}
	spec := RecordSpec{
		Fields: []RecordField{{Name: "target", Parameter: target}},
		Load:   func() (Record, error) { return Record{"target": "q-bbbb"}, nil },
		Save:   func(RecordWrite) Result { return Done(nil) },
	}
	var err error
	if ed, err = r.AddRecordEditor("ed", spec, Parent("main")); err != nil {
		t.Fatal(err)
	}
	if err := ed.Focus(); err != nil {
		t.Fatal(err)
	}
	m.View()
	keytest.Press(m, "enter")
	c := r.Chooser(FormSuggestionsKind)
	if c == nil {
		t.Fatalf("no chooser, top modal %q", r.TopModal())
	}
	s := screen(m)
	for _, want := range []string{"Ship the Decisions page?  q-aaaa", "Rename the verb?  q-bbbb"} {
		if !strings.Contains(s, want) {
			t.Errorf("picker lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "question/q-") {
		t.Errorf("picker shows a prefixed id:\n%s", s)
	}
	if row, ok := c.Highlighted(); !ok || row.ID != "question/q-bbbb" {
		t.Errorf("highlight %v %v, want the held thing q-bbbb", row, ok)
	}
	for _, row := range c.Matches() {
		if row.ID == "question/q-aaaa" && (len(row.Tones) != 1 || row.Tones[0].Tone != ToneChoiceID) {
			t.Errorf("the id is not toned dim: %+v", row.Tones)
		}
	}
	for query, want := range map[string]string{"alias": "question/q-cccc", "q-aaaa": "question/q-aaaa", "decisions": "question/q-aaaa"} {
		keytest.Type(m, query)
		got := c.Matches()
		if len(got) == 0 || got[0].ID != want {
			t.Errorf("query %q: matches %v, want first %s", query, labels(got), want)
		}
		for range query {
			keytest.Press(m, "backspace")
		}
	}
}

// Choice.Row is the one place choices become picker rows: a registered noun
// picker's rows carry the label and the dim short id too.
func TestChoiceRowsCarryLabelAndShortID(t *testing.T) {
	rows := choiceRows([]Choice{{ID: "question/q-1", Label: "Ship it"}, {ID: "plain", Label: "plain"}})
	if rows[0].Label != "Ship it  q-1" || len(rows[0].Tones) != 1 || rows[0].Tones[0].From != len("Ship it  ") {
		t.Errorf("labelled row %+v", rows[0])
	}
	if rows[1].Label != "plain" || len(rows[1].Tones) != 0 {
		t.Errorf("unlabelled row %+v", rows[1])
	}
}
