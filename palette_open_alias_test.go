package ochazuke

import "testing"

// openAliasFixture is a record row in focus (a parcel with a label, another
// prop and a file) with the palette open over it, and a fake opener.
func openAliasFixture(t *testing.T) (*Runtime, *Palette, *fakeOpener) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	f := &fakeOpener{}
	r.SetOpener(f.opener("darwin"))
	s := &derivedStore{key: "id", things: []Thing{{"id": "t1", "title": "Original", "status": "todo", "file": "/tmp/notes.md"}}}
	r.Define("parcel").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "status"},
		PropSpec{Name: "file", Parameter: Parameter{Type: "path"}},
	).Store(s.store())
	r.Instance("row", Kind("parcel")).Data = Thing{"id": "t1"}
	if err := r.SetFocus("row"); err != nil {
		t.Fatal(err)
	}
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	return r, p, f
}

// edit and open are verbs, never words for the Open line: with a record row
// in focus the palette reads them, bare or followed by arguments, as that
// row's own verbs. v0.123.0 made them bare aliases of the Open line, so
// edit --field @label opened the Open line prefilled with "--field @label"
// instead of the row's one-field form.
func TestPaletteEditAndOpenStayTheFocusedVerbs(t *testing.T) {
	t.Parallel()
	for line, want := range map[string]string{
		"edit --field @label": "parcel.edit",
		"edit":                "parcel.edit",
		"open":                "parcel.open",
	} {
		_, p, _ := openAliasFixture(t)
		if got := parsed(p, line); got != want {
			t.Errorf("%q read as %s, want %s", line, got, want)
		}
	}

	r, _, _ := openAliasFixture(t)
	paletteLine(t, r, "edit --field @label")
	e := r.RecordEditor(editFormID)
	if r.RecordEditor(OpenModalID) != nil || e == nil || len(e.fields) != 1 || e.Values()["title"] != "Original" {
		t.Fatalf("edit --field @label: top modal %q, edit form %+v", r.TopModal(), e)
	}

	r, _, _ = openAliasFixture(t)
	paletteLine(t, r, "edit")
	e = r.RecordEditor(editFormID)
	if r.RecordEditor(OpenModalID) != nil || e == nil || len(e.fields) < 2 || e.Values()["title"] != "Original" {
		t.Fatalf("bare edit: top modal %q, edit form %+v", r.TopModal(), e)
	}

	r, _, f := openAliasFixture(t)
	paletteLine(t, r, "open")
	if r.RecordEditor(OpenModalID) != nil || len(f.runs) != 1 || f.runs[0][len(f.runs[0])-1] != "/tmp/notes.md" {
		t.Fatalf("bare open: top modal %q, opener ran %v", r.TopModal(), f.runs)
	}
}

// e is the Open line's palette word (Vim's :e): alone it opens the line
// empty, and the text after it prefills it.
func TestPaletteEOpensTheOpenLine(t *testing.T) {
	t.Parallel()
	for line, want := range map[string]string{"e": "", "e /some/path": "/some/path"} {
		r, _, f := openAliasFixture(t)
		paletteLine(t, r, line)
		ed := r.RecordEditor(OpenModalID)
		if ed == nil {
			t.Fatalf("%q did not open the Open line: top modal %q", line, r.TopModal())
		}
		if text := ed.Values()["text"]; text != want || len(f.runs) != 0 {
			t.Fatalf("%q prefilled %q (want %q), opener ran %v", line, text, want, f.runs)
		}
	}
}
