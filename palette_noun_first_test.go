package ochazuke

import "testing"

// A first word that is both a verb (the focused doc's own note verb) and a
// noun (note) reads as the noun when the next word is that noun's verb or
// prop: on mtm's Notes page :note rename ran the doc's note verb with
// "rename" as its text. A next word the noun does not know keeps the
// verb reading (:note hello writes a note), and one word alone stays the
// verb, as before.
func TestPaletteTypedNounWinsOverAFocusedVerb(t *testing.T) {
	r := NewRuntime()
	run := func(*Runtime, Context, Arguments) (any, error) { return nil, nil }
	r.Register(Operation{Noun: "doc", Verb: "note", Parameters: map[string]Parameter{"text": {Type: "string", Required: true}}, Run: run})
	r.Register(Operation{Noun: "note", Verb: "rename", Parameters: map[string]Parameter{"name": {Type: "string", Required: true}}, Run: run})
	r.Register(Operation{Noun: "note", Verb: "archive", Run: run})
	r.Instance("page")
	r.Instance("doc1", Kind("doc"), Parent("page"))
	r.Instance("note1", Kind("note"), Parent("page"))
	if err := r.SetFocus("doc1"); err != nil {
		t.Fatal(err)
	}
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	for _, c := range []struct{ line, want string }{
		{"note rename", "note.rename"},
		{"note rename Groceries", "note.rename"},
		{"note archive", "note.archive"},
		{"note hello", "doc.note"},
		{"note", "doc.note"},
	} {
		if got := p.parse(c.line).Op.Key(); got != c.want {
			t.Errorf("%q: %s, want %s", c.line, got, c.want)
		}
	}
}
