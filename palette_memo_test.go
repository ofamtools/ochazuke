package ochazuke

import "testing"

// TestPaletteRedrawReadsNoStore: the palette decides what it offers once
// (Palette.index); drawing it again with nothing changed asks no guard and
// reads no store, and a change the runtime is told of reads again. The
// stores have no change token, as an app's own store often has none, so the
// graph walk behind a by-association verb is never cached below the palette.
func TestPaletteRedrawReadsNoStore(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Config().Disable()
	gets := 0
	add := func(noun string, extra []PropSpec, things ...Thing) *NounBuilder {
		table := NewMemDocTable(r, noun)
		store := DocStore(table, noun)
		get := store.Get
		store.ChangeToken = nil
		store.Get = func(c Ctx, key string) (Thing, error) { gets++; return get(c, key) }
		b := r.Define(noun).Props(append([]PropSpec{{Name: "id", Key: true}, {Name: "title", Role: RoleLabel}}, extra...)...).Store(store)
		for _, thing := range things {
			if err := table.Put(thing["id"].(string), thing); err != nil {
				t.Fatal(err)
			}
		}
		return b
	}
	add("project", nil, Thing{"id": "p1", "title": "Home"}).Verb("touch", func(Ctx) Result { return Done(nil) }).ByAssociation()
	add("pane", []PropSpec{{Name: "project", Parameter: Parameter{Noun: "project"}}}, Thing{"id": "a", "title": "A", "project": "p1"})
	r.Define("terminal").Props(PropSpec{Name: "pane", Association: true, Parameter: Parameter{Noun: "pane"}})
	r.Instance("term", Kind("terminal")).Data = map[string]any{"pane": "a"}
	if err := r.SetFocus("term"); err != nil {
		t.Fatal(err)
	}
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	first := p.View()
	if gets == 0 {
		t.Fatal("the first draw read no store: the fixture proves nothing")
	}
	read := gets
	if again := p.View(); gets != read || again != first {
		t.Fatalf("a second draw read the stores %d more times (or drew differently)", gets-read)
	}
	r.Changed("pane")
	p.View()
	if gets == read {
		t.Fatal("a change the runtime was told of was not read again")
	}
}
