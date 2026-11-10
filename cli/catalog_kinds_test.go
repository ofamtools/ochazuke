package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// kindsRuntime is a tool with a record noun over a paging store, a
// command noun and a part noun, and a verb that creates.
func kindsRuntime() *ochazuke.Runtime {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	done := func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	things := []ochazuke.Thing{{"id": "a"}}
	r.Define("parcel").Props(ochazuke.PropSpec{Name: "id", Key: true}).Store(ochazuke.Store{
		Kind: ochazuke.KindCollection,
		Get:  func(ochazuke.Ctx, string) (ochazuke.Thing, error) { return things[0], nil },
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) { return things, nil },
		Query: func(ochazuke.Ctx, ochazuke.ListQuery) (ochazuke.ListPage, error) {
			return ochazuke.ListPage{Items: things, Total: 1}, nil
		},
		ChangeToken: func(ochazuke.Ctx) (string, error) { return "1", nil },
		Undo:        true,
	})
	r.Define("lane").Verb("define", done, "Define lanes").Creates()
	r.Define("setup").Verb("run", done, "Run the setup")
	r.Define("turn-line").Verb("fold", func(*ochazuke.Runtime, ochazuke.Context, ochazuke.Arguments) (any, error) { return nil, nil }, "Fold")
	r.Define("daemon-row").Kind(ochazuke.NounKindPart).Verb("restart", done, "Restart")
	return r
}

func catalogOf(t *testing.T, r *ochazuke.Runtime) CatalogDoc {
	t.Helper()
	out, err := Catalog(r, "tool", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// The catalog publishes every noun's kind, a stored noun's store
// capabilities and facts mark, and the verbs that create.
func TestCatalogPublishesKindsAndStores(t *testing.T) {
	doc := catalogOf(t, kindsRuntime())
	want := map[string]string{"parcel": ochazuke.NounKindRecord, "lane": ochazuke.NounKindCommand, "setup": ochazuke.NounKindCommand, "turn-line": ochazuke.NounKindPart, "daemon-row": ochazuke.NounKindPart, "ochazuke.noun": ochazuke.NounKindFacts, "ochazuke.page": ochazuke.NounKindInstances}
	for _, n := range doc.Nouns {
		if n.NounKind == "" {
			t.Errorf("%s publishes no kind", n.Name)
		}
		if k, ok := want[n.Name]; ok && n.NounKind != k {
			t.Errorf("%s kind %q, want %q", n.Name, n.NounKind, k)
		}
		if (n.Kind != "") != (n.Store != nil) {
			t.Errorf("%s: a store kind %q needs store capabilities %+v", n.Name, n.Kind, n.Store)
		}
	}
	parcel, _ := doc.noun("parcel")
	if s := parcel.Store; s == nil || !s.Query || !s.ChangeToken || !s.Undo || s.Remote || s.Watch {
		t.Fatalf("parcel store %+v", parcel.Store)
	}
	if facts, _ := doc.noun("ochazuke.noun"); !facts.Facts {
		t.Fatal("ochazuke.noun is facts")
	}
	creates := map[string]bool{}
	for _, n := range doc.Nouns {
		for _, v := range n.Verbs {
			if v.Creates {
				creates[n.Name+"."+v.Name] = true
			}
		}
	}
	if !creates["lane.define"] || !creates["parcel.create"] || creates["setup.run"] {
		t.Fatalf("creating verbs %v", creates)
	}
}

// A catalog written before the fields (v0.117 and older) has no nounKind,
// facts, store or creates: KindOf reads the same kind from the rest of
// each entry, and the nouns mount as they always did.
func TestOldCatalogReadsTheSameKinds(t *testing.T) {
	r := kindsRuntime()
	ochazuke.NewModel(r)
	doc := catalogOf(t, r)
	old := doc
	old.Nouns = make([]CatalogNoun, len(doc.Nouns))
	for i, n := range doc.Nouns {
		n.NounKind, n.Facts, n.Store = "", false, nil
		verbs := make([]CatalogVerb, len(n.Verbs))
		for j, v := range n.Verbs {
			v.Creates = false
			verbs[j] = v
		}
		n.Verbs = verbs
		old.Nouns[i] = n
	}
	for i, n := range old.Nouns {
		if n.Name == "daemon-row" {
			continue // a declared kind is new data an older catalog cannot hold
		}
		if got, want := n.KindOf(), doc.Nouns[i].NounKind; got != want {
			t.Errorf("%s: an old catalog reads %q, the new one says %q", n.Name, got, want)
		}
	}
	// Mounted, both read the same kinds in the host.
	for _, d := range []CatalogDoc{doc, old} {
		host := ochazuke.NewRuntime()
		host.Config().Disable()
		got := MountNouns(host, d.AppNouns(), HostMount{
			Local: Namespace("tool"),
			Proxy: func(string, CatalogVerb) any { return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) } },
		})
		if len(got.Warnings) > 0 {
			t.Fatalf("warnings %v", got.Warnings)
		}
		for noun, kind := range map[string]string{"tool.parcel": ochazuke.NounKindRecord, "tool.setup": ochazuke.NounKindCommand, "tool.turn-line": ochazuke.NounKindPart} {
			if host.NounKind(noun) != kind {
				t.Errorf("mounted %s: %q, want %q", noun, host.NounKind(noun), kind)
			}
		}
	}
}

// A mounted extension's nouns are on the host's Nouns page from its
// catalog alone: records with a remote store, command nouns with their
// actions, parts left out.
func TestMountedNounsOnTheNounsPage(t *testing.T) {
	doc := catalogOf(t, kindsRuntime())
	host := ochazuke.NewRuntime()
	host.Config().Disable()
	got := MountNouns(host, doc.AppNouns(), HostMount{
		Local: Namespace("tool"),
		Proxy: func(string, CatalogVerb) any { return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) } },
	})
	if len(got.Warnings) > 0 {
		t.Fatal(got.Warnings)
	}
	if _, err := host.AddPage("home", "Home", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Text("") }); err != nil {
		t.Fatal(err)
	}
	m := ochazuke.NewModel(host)
	m.Init()
	if err := host.OpenPage(ochazuke.NounsPageID); err != nil {
		t.Fatal(err)
	}
	rows := map[string]ochazuke.ListItem{}
	for _, it := range host.List(ochazuke.NounsListID).Items() {
		rows[it.Label] = it
	}
	if r, ok := rows["tool.parcel"]; !ok || r.Section != "records" || !strings.HasPrefix(r.Cells[3], "remote") {
		t.Fatalf("tool.parcel row %+v", r)
	}
	if r, ok := rows["tool.setup"]; !ok || r.Section != "commands" || r.Cells[4] != "run" {
		t.Fatalf("tool.setup row %+v", r)
	}
	if _, ok := rows["tool.turn-line"]; ok {
		t.Fatal("a mounted part is left out")
	}
	// Its generated index page opens from the catalog alone and adds no tab.
	tabs := func() []string {
		var ids []string
		for _, p := range host.TabPages() {
			ids = append(ids, p.ID)
		}
		return ids
	}
	before := tabs()
	if err := host.OpenIndex("tool.parcel", ""); err != nil {
		t.Fatal(err)
	}
	if host.Page != ochazuke.IndexPageID("tool.parcel") || host.List(ochazuke.IndexPageID("tool.parcel")+"/list") == nil {
		t.Fatalf("index page %s", host.Page)
	}
	if err := host.OpenPage("home"); err != nil {
		t.Fatal(err)
	}
	if after := tabs(); strings.Join(after, " ") != strings.Join(before, " ") {
		t.Fatalf("tabs %v, before %v", after, before)
	}
}

// A data page's list of kind index is the ochazuke.index component over
// the host's mounted noun: it fills itself from the noun's (proxy) store,
// in the derived columns, and takes no rows from the page's answers.
func TestCatalogIndexList(t *testing.T) {
	doc := catalogOf(t, kindsRuntime())
	host := ochazuke.NewRuntime()
	host.Config().Disable()
	ns := Namespace("tool")
	if got := MountNouns(host, doc.AppNouns(), HostMount{Local: ns, Proxy: func(string, CatalogVerb) any {
		return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	}}); len(got.Warnings) > 0 {
		t.Fatal(got.Warnings)
	}
	m := ochazuke.NewModel(host)
	page := CatalogPage{ID: "parcels", Title: "Parcels", Lists: []CatalogList{{ID: "parcels/list", Noun: "parcel", Kind: ListKindIndex}}}
	v, err := MountPage(m, doc, page, PageConfig{Noun: ns})
	if err != nil {
		t.Fatal(err)
	}
	list := host.List("parcels/list")
	if list == nil || list.From() != "tool.parcel" {
		t.Fatalf("index list %v", list)
	}
	if cols := list.Columns(); len(cols) == 0 || cols[0].Title != "id" {
		t.Fatalf("derived columns %v", cols)
	}
	if err := v.SetRows("parcels/list", nil); err == nil {
		t.Fatal("an index list takes no rows from the page")
	}
}

// A data page asks for the show form by data alone: detail kind
// record-form mounts an ochazuke.ShowForm on the page (the selected
// thing's form, its relation sections, its history), as a generated index
// page has.
func TestCatalogRecordFormDetail(t *testing.T) {
	doc := catalogOf(t, kindsRuntime())
	host := ochazuke.NewRuntime()
	host.Config().Disable()
	ns := Namespace("tool")
	if got := MountNouns(host, doc.AppNouns(), HostMount{Local: ns, Proxy: func(string, CatalogVerb) any {
		return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	}}); len(got.Warnings) > 0 {
		t.Fatal(got.Warnings)
	}
	m := ochazuke.NewModel(host)
	page := CatalogPage{ID: "parcels", Title: "Parcels", Lists: []CatalogList{{ID: "parcels/list", Noun: "parcel", Kind: ListKindIndex}},
		Detail: &CatalogDetail{Kind: DetailRecordForm}}
	if _, err := MountPage(m, doc, page, PageConfig{Noun: ns}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, inst := range host.Children("parcels") {
		if _, ok := inst.Component.(*ochazuke.ShowForm); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("record-form mounted no show form on the page")
	}
}
