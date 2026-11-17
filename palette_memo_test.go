package ochazuke

import (
	"context"
	"strings"
	"testing"
)

// staticFixture is a runtime whose verb tour is behind a guard that reads a
// store with no change token (true while pane a is titled A), a fetch that
// loads pane a as a daemon listing does, and the focus on a console whose
// association names pane a. guards counts the guard's calls, gets the
// store's reads.
type staticFixture struct {
	r            *Runtime
	p            *Palette
	guards, gets *int
	load, put    func(title string)
}

func newStaticFixture(t *testing.T) staticFixture {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	guards, gets := 0, 0
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
	r.DefineGuard("pane-a-titled", func(r *Runtime, _ KeyEvent) bool {
		guards++
		thing, err := r.Noun("pane").Store.Get(Ctx{Runtime: r}, "a")
		return err == nil && thing["title"] == "A"
	})
	r.Define("console").Props(PropSpec{Name: "pane", Association: true, Parameter: Parameter{Noun: "pane"}}).
		Verb("tour", func(Ctx) Result { return Done("toured") }).When("pane-a-titled").
		Verb("zap", func(Ctx) Result { return Done(nil) })
	r.Instance("term", Kind("console")).Data = map[string]any{"pane": "a"}
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
	f := staticFixture{r: r, p: NewPalette(r), guards: &guards, gets: &gets}
	f.load = func(title string) {
		t.Helper()
		loadTitle = title
		mustRun(t, r, Request{Noun: "cache", Verb: "load"})
		settleJob(t, r)
	}
	f.put = func(title string) {
		t.Helper()
		if err := tables["pane"].Put("a", Thing{"id": "a", "title": title, "project": "p1"}); err != nil {
			t.Fatal(err)
		}
		r.Changed("pane")
	}
	return f
}

// reopen opens the palette afresh, types text a key at a time and draws.
func (f staticFixture) reopen(t *testing.T, text string) string {
	t.Helper()
	if f.p.IsOpen() {
		paletteRequest(t, f.r, "close", "palette", nil)
	}
	paletteRequest(t, f.r, "open", "palette", nil)
	for _, key := range strings.Split(text, "") {
		if key == " " {
			key = "space"
		}
		if _, err := f.p.HandleKey(key); err != nil {
			t.Fatal(err)
		}
		f.p.View()
	}
	return f.p.View()
}

// TestPaletteStaticIndex: the palette's list is built from the registry
// alone, once per shape. Opening it, typing, drawing and reopening ask no
// guard and read no store; a store change, a fetch that loads the stores
// and a focus move build nothing again; a verb whose guard is false at the
// focus is listed in its place (palette.list static) and answers when run
// with no error on screen. A registered verb (an extension mounted), an
// action alias, a noun alias and a setting the index reads each show on
// the next open.
func TestPaletteStaticIndex(t *testing.T) {
	t.Parallel()
	f := newStaticFixture(t)
	r, p := f.r, f.p
	f.put("B") // tour's guard is false here
	*f.guards, *f.gets = 0, 0
	view := f.reopen(t, "console ")
	if *f.guards != 0 || *f.gets != 0 {
		t.Fatalf("an open and its typing asked %d guards and read %d things", *f.guards, *f.gets)
	}
	if !strings.Contains(view, "tour") || strings.Contains(view, NotUsualHere) {
		t.Fatalf("tour is not listed in its place:\n%s", view)
	}
	if p.builds != 1 {
		t.Fatalf("builds %d, want 1", p.builds)
	}
	// Data moves: nothing is built again.
	f.load("A")
	f.put("B")
	paletteRequest(t, r, "close", "palette", nil)
	if err := r.SetFocus("term"); err != nil {
		t.Fatal(err)
	}
	f.reopen(t, "t")
	if p.builds != 1 || *f.guards != 0 {
		t.Fatalf("a store change or a load built the index again (builds %d, guards %d)", p.builds, *f.guards)
	}
	// The shape moves: each kind shows on the next open.
	r.Define("console").Verb("mounted", func(Ctx) Result { return Done(nil) })
	if view := f.reopen(t, "console "); !strings.Contains(view, "mounted") || p.builds != 2 {
		t.Fatalf("a registered verb is not listed (builds %d):\n%s", p.builds, view)
	}
	if err := r.AliasAction("console.trip", "console.tour"); err != nil {
		t.Fatal(err)
	}
	if view := f.reopen(t, "console "); !strings.Contains(view, "also trip") || p.builds != 3 {
		t.Fatalf("an action alias is not listed (builds %d):\n%s", p.builds, view)
	}
	if err := r.Alias("tm", "console"); err != nil {
		t.Fatal(err)
	}
	if view := f.reopen(t, "tm "); !strings.Contains(view, "tour") || p.builds != 4 {
		t.Fatalf("a noun alias does not read (builds %d):\n%s", p.builds, view)
	}
	if err := r.Settings().Set(PaletteNounTailsSetting, false); err != nil {
		t.Fatal(err)
	}
	f.reopen(t, "")
	if p.builds != 5 {
		t.Fatalf("palette.noun-tails did not build the index again (builds %d)", p.builds)
	}
	// Run time stays honest: tour, listed while its guard is false here,
	// answers with its outcome and puts no error on screen.
	f.reopen(t, "console tour")
	if _, err := r.Execute(Request{Noun: "ochazuke.palette", Verb: "run"}); err != nil || p.message != "" {
		t.Fatalf("running tour from the palette: %v %q", err, p.message)
	}
}

// TestPaletteListLive: palette.list live is the list before the static
// index: each open asks the guards, and a verb whose guard is false at the
// focus comes after the others, marked; typing within the open asks none.
func TestPaletteListLive(t *testing.T) {
	t.Parallel()
	f := newStaticFixture(t)
	if err := f.r.Settings().Set(PaletteListSetting, PaletteListLive); err != nil {
		t.Fatal(err)
	}
	f.put("B")
	*f.guards = 0
	view := f.reopen(t, "console ")
	if *f.guards == 0 || !strings.Contains(view, NotUsualHere) || strings.Index(view, "tour") < strings.Index(view, "zap") {
		t.Fatalf("live: tour is not after the usual verbs, marked (guards %d):\n%s", *f.guards, view)
	}
	asked := *f.guards
	for _, key := range []string{"t", "backspace"} {
		if _, err := f.p.HandleKey(key); err != nil {
			t.Fatal(err)
		}
		f.p.View()
	}
	if *f.guards != asked {
		t.Fatalf("typing asked %d guards", *f.guards-asked)
	}
}

// TestPaletteChoicesFetchedOnce: an argument's values are fetched once when
// its step opens and typing filters them; a source that sees the typed text
// is asked with none first, and with the word only when nothing it gave
// starts with that word (a completer); another verb's parameter over the
// same source shares the answer; the next open asks again.
func TestPaletteChoicesFetchedOnce(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	m := NewModel(r)
	var asked []string
	r.Define("names").Verb("complete", func(ctx Ctx) Result {
		q, _ := ctx.Args[ChoicesQueryParameter].(string)
		asked = append(asked, q)
		return Done([]string{"alpha", "alps", "beta"})
	})
	src := r.Operations.At(Key("names.complete"))
	src.Parameters = map[string]Parameter{ChoicesQueryParameter: {Type: "string"}}
	r.Operations.Set(src)
	r.Define("doc").Verb("open", func(Ctx) Result { return Done(nil) })
	op := r.Operations.At(Key("doc.open"))
	op.Parameters = map[string]Parameter{"name": {Type: "string", Required: true, Source: "names.complete"}}
	r.Operations.Set(op)
	r.Define("doc").Verb("rename", func(Ctx) Result { return Done(nil) })
	other := r.Operations.At(Key("doc.rename"))
	other.Parameters = map[string]Parameter{"name": {Type: "string", Required: true, Source: "names.complete"}}
	r.Operations.Set(other)
	if err := m.Palette.Open(); err != nil {
		t.Fatal(err)
	}
	typeText(m, "doc open alp")
	m.View()
	if len(asked) != 1 || asked[0] != "" {
		t.Fatalf("asked %q, want one ask with no text", asked)
	}
	var got []string
	for _, e := range m.Palette.completions() {
		got = append(got, e.Value)
	}
	if strings.Join(got, " ") != "alpha alps" {
		t.Fatalf("listed %v", got)
	}
	typeText(m, "zz")
	m.View()
	if strings.Join(asked, " ") != " alpzz" {
		t.Fatalf("a word nothing starts with asks the source with it: asked %q", asked)
	}
	if err := m.Palette.line.Replace(0, len(m.Palette.line.Text), ""); err != nil {
		t.Fatal(err)
	}
	typeText(m, "doc rename alp")
	m.View()
	if strings.Join(asked, " ") != " alpzz" {
		t.Fatalf("another verb over the same source asked it again: asked %q", asked)
	}
}
