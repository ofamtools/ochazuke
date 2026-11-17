package ochazuke

import (
	"fmt"
	"testing"
)

// BenchmarkPaletteKeystroke is one key typed into the open palette over a
// registry of the size an app with its tools mounted has (2,400 verbs,
// each behind a guard): typing "noun042 ver" a key at a time, then erasing
// it. A key that only edits the line filters what the open computed, so
// guards/key stays 0; it was every verb's guard per key while typing
// moved the palette's change counter.
func BenchmarkPaletteKeystroke(b *testing.B) {
	r := NewRuntime()
	r.Config().Disable()
	guards := 0
	r.DefineGuard("bench-offered", func(*Runtime, KeyEvent) bool { guards++; return true })
	for i := range 150 {
		n := r.Define(fmt.Sprintf("noun%03d", i))
		for j := range 16 {
			n.Verb(fmt.Sprintf("verb%02d", j), func(Ctx) Result { return Done(nil) }).When("bench-offered")
		}
	}
	p := NewPalette(r)
	if _, err := r.Execute(Request{Noun: "ochazuke.palette", Verb: "open"}); err != nil {
		b.Fatal(err)
	}
	p.View()
	line := "noun042 ver"
	keys := make([]string, 0, 2*len(line))
	for _, c := range line {
		keys = append(keys, string(c))
	}
	for range line {
		keys = append(keys, "backspace")
	}
	guards = 0
	b.ReportAllocs()
	b.ResetTimer()
	n := 0
	for b.Loop() {
		if _, err := p.HandleKey(keys[n%len(keys)]); err != nil {
			b.Fatal(err)
		}
		p.View()
		n++
	}
	b.ReportMetric(float64(guards)/float64(n), "guards/key")
}

// BenchmarkPaletteOpenByAssociation opens the palette with the focus on a
// live terminal whose association names a pane, over 1,200 by-association
// verbs of nouns not on the focus path (an app's tools mounted): each verb
// asks for the nearest thing of its noun, which walks the relations from the
// terminal. The pane inherits its owner through its session (Inherit) and
// no store has a change token, as an app's own store often has none. The
// walk is read once per open (reads/open stays at one walk's reads); it was
// once per verb, seconds per open in the app.
func BenchmarkPaletteOpenByAssociation(b *testing.B) {
	r := NewRuntime()
	r.Config().Disable()
	reads := 0
	add := func(noun string, extra []PropSpec, things ...Thing) {
		table := NewMemDocTable(r, noun)
		store := DocStore(table, noun)
		get := store.Get
		store.ChangeToken = nil
		store.Get = func(c Ctx, key string) (Thing, error) { reads++; return get(c, key) }
		r.Define(noun).Props(append([]PropSpec{{Name: "id", Key: true}, {Name: "title", Role: RoleLabel}}, extra...)...).Store(store)
		for _, thing := range things {
			if err := table.Put(thing["id"].(string), thing); err != nil {
				b.Fatal(err)
			}
		}
	}
	add("project", nil, Thing{"id": "p1", "title": "Home"})
	add("session", []PropSpec{{Name: "owner", Parameter: Parameter{Noun: "project"}}}, Thing{"id": "s1", "title": "main", "owner": "p1"})
	add("pane", []PropSpec{
		{Name: "session", Parameter: Parameter{Noun: "session"}},
		{Name: "owner", Inherit: "session", Parameter: Parameter{Noun: "project"}},
	}, Thing{"id": "a", "title": "A", "session": "s1"})
	for i := range 150 {
		n := r.Define(fmt.Sprintf("noun%03d", i))
		for j := range 8 {
			n.Verb(fmt.Sprintf("verb%02d", j), func(Ctx) Result { return Done(nil) }).ByAssociation()
		}
	}
	r.Define("terminal").Props(PropSpec{Name: "pane", Association: true, Parameter: Parameter{Noun: "pane"}})
	r.Instance("term", Kind("terminal")).Data = map[string]any{"pane": "a"}
	if err := r.SetFocus("term"); err != nil {
		b.Fatal(err)
	}
	p := NewPalette(r)
	reads = 0
	b.ReportAllocs()
	b.ResetTimer()
	n := 0
	for b.Loop() {
		if _, err := r.Execute(Request{Noun: "ochazuke.palette", Verb: "open"}); err != nil {
			b.Fatal(err)
		}
		p.View()
		if _, err := r.Execute(Request{Noun: "ochazuke.palette", Verb: "close"}); err != nil {
			b.Fatal(err)
		}
		r.Changed("pane") // what a refresh of the listing tells between opens
		n++
	}
	b.ReportMetric(float64(reads)/float64(n), "reads/open")
}
