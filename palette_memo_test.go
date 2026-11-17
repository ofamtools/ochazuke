package ochazuke

import (
	"context"
	"strings"
	"testing"
)

// TestPaletteRedrawReadsNoStore: the palette decides what it offers once
// (Palette.index); drawing it again with nothing changed asks no guard and
// reads no store, and a change the runtime is told of reads again. The
// stores have no change token, as an app's own store often has none, so the
// graph walk behind a by-association verb holds only until such a change.
func TestPaletteRedrawReadsNoStore(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Config().Disable()
	gets := 0
	tables := map[string]DocTable{}
	add := func(noun string, extra []PropSpec, things ...Thing) *NounBuilder {
		table := NewMemDocTable(r, noun)
		tables[noun] = table
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
	// tour is usual only while pane a is titled A: its guard reads the
	// store, which has no change token.
	r.DefineGuard("pane-a-titled", func(r *Runtime, _ KeyEvent) bool {
		thing, err := r.Noun("pane").Store.Get(Ctx{Runtime: r}, "a")
		return err == nil && thing["title"] == "A"
	})
	r.Define("terminal").Props(PropSpec{Name: "pane", Association: true, Parameter: Parameter{Noun: "pane"}}).
		Verb("tour", func(Ctx) Result { return Done(nil) }).When("pane-a-titled")
	r.Instance("term", Kind("terminal")).Data = map[string]any{"pane": "a"}
	// cache.load is a fetch that loads pane a into its store, as a periodic
	// daemon listing does (Writes pane), titled loadTitle; a watch of pane
	// tells whether the listing changed.
	loadTitle := "A"
	r.Define("cache").Kind(NounKindPart).Verb("load", func(Ctx) Result {
		return Pending(func(context.Context) (any, error) { return nil, nil }).Then(func(Ctx, any, error) {
			if err := tables["pane"].Put("a", Thing{"id": "a", "title": loadTitle, "project": "p1"}); err != nil {
				t.Error(err)
			}
		})
	}, "Load the panes").Fetch().Writes("pane")
	if _, err := r.Watch("pane", nil, func(WatchEvent) {}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("term"); err != nil {
		t.Fatal(err)
	}
	// Left out, not listed after the usual verbs: a guard's answer shows.
	if err := r.Settings().Set(PaletteReachSetting, false); err != nil {
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
	// A key that only edits the line filters what the open computed: no
	// guard runs again, no store is read (the typing writes no store).
	for _, key := range []string{"t", "o"} {
		if _, err := p.HandleKey(key); err != nil {
			t.Fatal(err)
		}
		p.View()
	}
	if p.Text() != "to" || gets != read {
		t.Fatalf("typing %q read the stores %d more times", p.Text(), gets-read)
	}
	typed := p.View()
	if !strings.Contains(typed, "tour") {
		t.Fatalf("tour is not offered before the change:\n%s", typed)
	}
	retype := func() string {
		t.Helper()
		for _, key := range []string{"u", "backspace"} {
			if _, err := p.HandleKey(key); err != nil {
				t.Fatal(err)
			}
		}
		return p.View()
	}
	load := func(title string) {
		t.Helper()
		loadTitle = title
		mustRun(t, r, Request{Noun: "cache", Verb: "load"})
		settleJob(t, r)
	}
	// A fetch that loads the same listing keeps what the open computed.
	load("A")
	if same := retype(); gets != read || !strings.Contains(same, "tour") {
		t.Fatalf("an unchanged load read the stores %d more times:\n%s", gets-read, same)
	}
	// A fetch that changes the listing shows on the next key: pane a is
	// retitled, so tour's guard answers false.
	load("B")
	if after := retype(); strings.Contains(after, "tour") {
		t.Fatalf("the palette still shows the state from before the load:\n%s", after)
	}
	// So does a change the runtime is told of: titled A again, tour is back.
	if err := tables["pane"].Put("a", Thing{"id": "a", "title": "A", "project": "p1"}); err != nil {
		t.Fatal(err)
	}
	r.Changed("pane")
	after := retype()
	if !strings.Contains(after, "tour") {
		t.Fatalf("the palette still shows the state from before the change:\n%s", after)
	}
}
