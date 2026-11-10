package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

// indexFixture is an application with a 20,000-row paged store (big) and
// the Nouns page, on the Work page.
func indexFixture(t *testing.T) (*Runtime, *Model, *countingStore) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	big := newCountingStore(20000, 300)
	r.Define("big").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "done", Parameter: Parameter{Type: SettingBoolean}},
		PropSpec{Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
		PropSpec{Name: "notes", Multiline: true},
		PropSpec{Name: "meta", Parameter: Parameter{Type: PropObject}},
	).Store(big.store(true, true))
	if _, err := r.AddPage("work", "Work", func(*Runtime, int, int) Block { return Text("work") }); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Init()
	m.Width, m.Height = 120, 40
	return r, m, big
}

// A 20,000-row store draws its index with one page read: no List (no full
// read), at most a page of rows and one archived count; the page keys read
// the next page, sort and filter read page 1 of the new query, and an
// unchanged change token reads nothing.
func TestIndexOfABigStoreReadsOnePage(t *testing.T) {
	r, m, big := indexFixture(t)
	mustRun(t, r, Request{Noun: "ochazuke.noun", Verb: "show", Args: Arguments{"noun": "big"}})
	if r.Page != IndexPageID("big") {
		t.Fatalf("page %s", r.Page)
	}
	list := r.List(IndexPageID("big") + "/list")
	if big.lists != 0 {
		t.Fatalf("the index read the whole store: %d lists", big.lists)
	}
	per := r.Settings().Int(ListWindowSetting)
	if big.rows > per+1 || list.Len() != per || list.WindowTotal() != 19700 {
		t.Fatalf("rows read %d, shown %d, total %d", big.rows, list.Len(), list.WindowTotal())
	}
	for range 3 {
		_ = m.View()
	}
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"page 1/99 of 19700", "300 archived hidden", "big · 19700"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame lacks %q:\n%s", want, frame)
		}
	}
	reads := big.queries
	mustRun(t, r, Request{Noun: "ochazuke.collection", Verb: "page-next", Args: Arguments{"id": list.id}})
	if page, _ := list.Page(); page != 2 || big.queries == reads || big.lists != 0 {
		t.Fatalf("page-next: page %d queries %d lists %d", page, big.queries, big.lists)
	}
	if item, _ := list.Selected(); !strings.HasPrefix(item.Label, "thing ") {
		t.Fatalf("selected %+v", item)
	}
	// An unchanged token: a change notice reads nothing.
	reads = big.queries
	r.Changed("big")
	if big.queries != reads {
		t.Fatalf("an unchanged token read the store: %d -> %d", reads, big.queries)
	}
	// Sorting reads page 1 of the sorted query.
	mustRun(t, r, Request{Noun: "ochazuke.collection", Verb: "sort", Args: Arguments{"id": list.id, "by": "-title"}})
	if page, _ := list.Page(); page != 1 || big.lists != 0 {
		t.Fatalf("sort: page %d lists %d", page, big.lists)
	}
	if first := list.Items()[0]; first.Label != "thing 9999" {
		t.Fatalf("sorted first row %q", first.Label)
	}
	// The archive toggle reads the archived rows in.
	mustRun(t, r, Request{Noun: "ochazuke.collection", Verb: "show-archived", Args: Arguments{"id": list.id}})
	if list.WindowTotal() != 20000 || !list.ShowsArchived() || big.lists != 0 {
		t.Fatalf("show-archived: total %d", list.WindowTotal())
	}
	if !slices.Contains(r.Operations.ActionNames(r.Nouns["ochazuke.collection"]), "show-archived") {
		t.Fatal("collection.show-archived is a verb")
	}
}

// The derived columns: the label, then short choosable props; never a
// multi-line or object prop. index.columns.NOUN, written by index.columns,
// names them instead.
func TestIndexColumns(t *testing.T) {
	r, _, _ := indexFixture(t)
	var titles []string
	for _, c := range r.IndexColumns("big") {
		titles = append(titles, c.Title)
	}
	if !slices.Equal(titles, []string{"title", "done", "archived", "updated"}) {
		t.Fatalf("derived columns %v", titles)
	}
	mustRun(t, r, Request{Noun: "ochazuke.noun", Verb: "show", Args: Arguments{"noun": "big"}})
	mustRun(t, r, Request{Noun: indexNoun, Verb: "columns", Args: Arguments{"props": []any{"id", "title"}}})
	if got := r.Settings().Strings(IndexColumnsSetting + "big"); !slices.Equal(got, []string{"id", "title"}) {
		t.Fatalf("setting %v", got)
	}
	if cols := r.List(IndexPageID("big") + "/list").Columns(); len(cols) != 2 || cols[0].Title != "id" {
		t.Fatalf("list columns %v", cols)
	}
	if _, err := r.Execute(Request{Noun: indexNoun, Verb: "columns", Args: Arguments{"props": "nosuch"}}); err == nil {
		t.Fatal("a prop the noun lacks is refused")
	}
}

// Opening a generated index page and leaving it leaves the tab row as it
// was; pages.pinned puts one in the tabs.
func TestIndexPagesAreNotTabs(t *testing.T) {
	r, m, _ := indexFixture(t)
	before := nounTabIDs(r.TabPages())
	if err := r.OpenPage(NounsPageID); err != nil {
		t.Fatal(err)
	}
	if list := r.List(NounsListID); list == nil || list.Select(nounsRowPrefix+"big") != nil {
		t.Fatal("no big row on the Nouns page")
	}
	if err := r.SetFocus(nounsRowPrefix + "big"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "enter")
	if r.Page != IndexPageID("big") {
		t.Fatalf("enter on a Nouns row opens its index: %s", r.Page)
	}
	if during := nounTabIDs(r.TabPages()); !slices.Equal(during, before) {
		t.Fatalf("tabs while open %v, before %v", during, before)
	}
	keytest.Press(m, "ctrl+j")
	if after := nounTabIDs(r.TabPages()); !slices.Equal(after, before) || r.Page == IndexPageID("big") {
		t.Fatalf("tabs after %v (page %s), before %v", after, r.Page, before)
	}
	if err := r.Settings().Set(PagesPinnedSetting, []string{IndexPageID("big")}); err != nil {
		t.Fatal(err)
	}
	if ids := nounTabIDs(r.TabPages()); !slices.Contains(ids, IndexPageID("big")) {
		t.Fatalf("pinned: %v", ids)
	}
}

// A command noun has no index: show says what it is and what it does.
func TestIndexOfACommandNoun(t *testing.T) {
	r, _, _ := indexFixture(t)
	r.Define("setup").Verb("run", func(Ctx) Result { return Done(nil) }, "Run the setup")
	_, err := r.Execute(Request{Noun: "ochazuke.noun", Verb: "show", Args: Arguments{"noun": "setup"}})
	if err == nil || !strings.Contains(err.Error(), "command") || !strings.Contains(err.Error(), "run") {
		t.Fatalf("show of a command noun: %v", err)
	}
}

// A remote store's index draws its loading line while the read runs, then
// the empty line: never the empty line while loading.
func TestIndexLoadingThenEmpty(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	release := make(chan struct{})
	box := &remoteBox{items: map[string]Thing{}, hold: func(string) { <-release }}
	r.Define("box").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(box.store())
	if _, err := r.AddPage("work", "Work", func(*Runtime, int, int) Block { return Text("work") }); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Init()
	m.Width, m.Height = 100, 30
	if err := r.OpenIndex("box", ""); err != nil {
		t.Fatal(err)
	}
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "loading boxes…") || strings.Contains(frame, "no boxes") {
		t.Fatalf("while loading:\n%s", frame)
	}
	close(release)
	landLoads(m, r.PendingCommands())
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "no boxes") {
		t.Fatalf("after loading:\n%s", frame)
	}
}

// j on the last row of a windowed list's page reads the next page and
// selects its first row; k on the first row goes back to the previous
// page's last row; at the store's ends the list spills as any list does.
func TestWindowEdgeLoadsTheNeighbouringPage(t *testing.T) {
	r, m, _ := indexFixture(t)
	if _, err := r.Settings().SetSession(ListWindowSetting, 5); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "ochazuke.noun", Verb: "show", Args: Arguments{"noun": "big"}})
	list := r.List(IndexPageID("big") + "/list")
	for range 4 {
		keytest.Press(m, "j")
	}
	keytest.Press(m, "j")
	if page, _ := list.Page(); page != 2 {
		t.Fatalf("j past the last row: page %d", page)
	}
	if item, _ := list.Selected(); item.ID != list.Items()[0].ID || !strings.HasSuffix(r.Focus, item.ID[strings.LastIndex(item.ID, "/"):]) {
		t.Fatalf("first row of page 2 selected and focused: %s focus %s", item.ID, r.Focus)
	}
	keytest.Press(m, "k")
	if page, _ := list.Page(); page != 1 {
		t.Fatalf("k past the first row: page %d", page)
	}
	if item, _ := list.Selected(); item.ID != list.Items()[len(list.Items())-1].ID {
		t.Fatalf("last row of page 1 selected: %s", item.ID)
	}
}
