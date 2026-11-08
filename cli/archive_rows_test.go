package cli

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// archiveDoc is an extension whose lanes arrive as catalog rows (an
// extension verb, not a store), the archive state riding in each row's data
// as the archive-role prop, the way o-lane feeds its Lanes page.
func archiveDoc(tree bool) CatalogDoc {
	return CatalogDoc{
		Schema: Schema, Name: "lanes", Version: "0.1.0",
		Nouns: []CatalogNoun{{
			Name:  "lane",
			Props: []ochazuke.PropSpec{{Name: "id", Key: true}, {Name: "state"}, {Name: "archived", Role: ochazuke.RoleArchive, Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean}}},
			Verbs: []CatalogVerb{{Name: "rows", Rows: "lane"}},
		}},
		Pages: []CatalogPage{{ID: "lanes", Title: "Lanes", Layout: "stack", Lists: []CatalogList{{ID: "lanes/list", Noun: "lane", Source: "lane.rows", Title: "Lanes", Tree: tree}}}},
	}
}

const archiveRows = `[{"id":"a","label":"alpha","data":{"state":"running","archived":false}},` +
	`{"id":"b","label":"beta","data":{"state":"archived","archived":true}},` +
	`{"id":"c","label":"gamma","data":{"state":"done","archived":false}}]`

const archiveTree = `[{"id":"a","label":"alpha","data":{"archived":false}},` +
	`{"id":"a1","parent":"a","label":"alpha-child","data":{"archived":false}},` +
	`{"id":"b","label":"beta","data":{"archived":true}},` +
	`{"id":"b1","parent":"b","label":"beta-child","data":{"archived":false}},` +
	`{"id":"c","label":"gamma","data":{"archived":false}},` +
	`{"id":"c1","parent":"c","label":"gamma-child","data":{"archived":true}}]`

func mountArchive(t *testing.T, tree bool, rows string) (*ochazuke.Model, *ochazuke.Collection) {
	t.Helper()
	doc := archiveDoc(tree)
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	mounted := MountNouns(r, doc.Nouns, HostMount{Local: func(name string) string { return name }, Proxy: func(string, CatalogVerb) any { return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) } }})
	if len(mounted.Warnings) > 0 {
		t.Fatal(mounted.Warnings)
	}
	m := ochazuke.NewModel(r)
	m.Width, m.Height = 100, 30
	m.Appearance.Theme = "mono"
	views, err := MountPages(m, doc, PageConfig{Fetch: answers(map[string]string{"lane.rows": rows})})
	if err != nil || len(views) != 1 {
		t.Fatalf("MountPages = %v, %v", views, err)
	}
	v := views[0]
	if err := r.OpenPage(v.Page()); err != nil {
		t.Fatal(err)
	}
	if err := v.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := Settle(m.Runtime, nil); err != nil {
		t.Fatal(err)
	}
	if tr := r.Tree("lanes/list"); tr != nil {
		return m, tr.List()
	}
	return m, r.List("lanes/list")
}

func shownLabels(l *ochazuke.Collection) []string {
	var out []string
	for _, row := range l.Snapshot()["items"].([]ochazuke.ListItem) {
		out = append(out, strings.TrimLeft(row.Label, " ├└─│▾▸"))
	}
	return out
}

func eq(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

// TestArchiveRowsHideAndSavedFilterReveal drives the page the owner uses:
// archived rows hide by default, the saved "archived" filter shows them on
// the page (rows, frame and title), and clearing hides them again.
func TestArchiveRowsHideAndSavedFilterReveal(t *testing.T) {
	t.Parallel()
	m, l := mountArchive(t, false, archiveRows)
	if got := shownLabels(l); !eq(got, []string{"alpha", "gamma"}) {
		t.Fatalf("default: %v", got)
	}
	f := frameOf(m)
	if strings.Contains(f, "beta") || !strings.Contains(f, "Lanes (2)") {
		t.Fatalf("default frame hides beta and counts 2:\n%s", f)
	}
	if err := l.ApplyFilter("archived"); err != nil {
		t.Fatal(err)
	}
	if got := shownLabels(l); !eq(got, []string{"beta"}) {
		t.Fatalf("archived filter: %v", got)
	}
	if f = frameOf(m); !strings.Contains(f, "beta") || strings.Contains(f, "alpha") || !strings.Contains(f, "Lanes (1/3)") {
		t.Fatalf("archived frame:\n%s", f)
	}
	if err := l.SetPropFilters(nil); err != nil {
		t.Fatal(err)
	}
	if got := shownLabels(l); !eq(got, []string{"alpha", "gamma"}) {
		t.Fatalf("cleared: %v", got)
	}
	if f = frameOf(m); strings.Contains(f, "beta") || !strings.Contains(f, "Lanes (2)") {
		t.Fatalf("cleared frame:\n%s", f)
	}
}

func TestArchiveRowsSettingOffAndTextFilter(t *testing.T) {
	t.Parallel()
	m, l := mountArchive(t, false, archiveRows)
	l.SetFilter("beta") // a text filter alone does not reveal archived rows
	if l.Shown() != 0 {
		t.Fatalf("text filter revealed an archived row: %v", shownLabels(l))
	}
	l.SetFilter("")
	if err := m.Runtime.Settings().Set("list.hide-archived", false); err != nil {
		t.Fatal(err)
	}
	if got := shownLabels(l); !eq(got, []string{"alpha", "beta", "gamma"}) {
		t.Fatalf("setting off shows all, live: %v", got)
	}
	if err := m.Runtime.Settings().Set("list.hide-archived", true); err != nil {
		t.Fatal(err)
	}
	if got := shownLabels(l); !eq(got, []string{"alpha", "gamma"}) {
		t.Fatalf("setting on hides again, live: %v", got)
	}
}

func TestArchiveRowsAllArchivedShowsEmptyState(t *testing.T) {
	t.Parallel()
	m, l := mountArchive(t, false, `[{"id":"b","label":"beta","data":{"archived":true}}]`)
	if l.Len() != 0 || l.Shown() != 0 {
		t.Fatalf("len %d shown %d", l.Len(), l.Shown())
	}
	if f := frameOf(m); strings.Contains(f, "beta") || !strings.Contains(f, "Lanes (0)") || strings.Contains(f, "rror") {
		t.Fatalf("empty state expected:\n%s", f)
	}
	if err := l.ApplyFilter("archived"); err != nil {
		t.Fatal(err)
	}
	if got := shownLabels(l); !eq(got, []string{"beta"}) {
		t.Fatal(got)
	}
}

// A tree: an archived node hides its whole subtree; the saved filter shows
// the archived nodes with their ancestors as context, as any prop filter.
func TestArchiveRowsTree(t *testing.T) {
	t.Parallel()
	_, l := mountArchive(t, true, archiveTree)
	if got := shownLabels(l); !eq(got, []string{"alpha", "alpha-child", "gamma"}) {
		t.Fatalf("default tree (archived parent hides its unarchived child): %v", got)
	}
	if err := l.ApplyFilter("archived"); err != nil {
		t.Fatal(err)
	}
	got := shownLabels(l)
	if !eq(got, []string{"beta", "gamma", "gamma-child"}) {
		t.Fatalf("archived filter on tree: %v", got)
	}
	if err := l.SetPropFilters(nil); err != nil {
		t.Fatal(err)
	}
	if got := shownLabels(l); !eq(got, []string{"alpha", "alpha-child", "gamma"}) {
		t.Fatalf("cleared tree: %v", got)
	}
}
