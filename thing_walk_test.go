package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func thingWalkFixture(t *testing.T) (*Runtime, *Model, Context) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	add := func(noun string, extra []PropSpec, things ...Thing) {
		table := NewMemDocTable(r, noun)
		props := []PropSpec{{Name: "id", Key: true}, {Name: "title", Role: RoleLabel}}
		r.Define(noun).Props(append(props, extra...)...).Store(DocStore(table, noun))
		for _, thing := range things {
			if err := table.Put(thing["id"].(string), thing); err != nil {
				t.Fatal(err)
			}
		}
	}
	add("project", nil, Thing{"id": "p1", "title": "Home"})
	add("task", []PropSpec{{Name: "project", Role: RoleOwner, Parameter: Parameter{Noun: "project"}}},
		Thing{"id": "t1", "title": "Write guide", "project": "p1"})
	add("directory", []PropSpec{{Name: "path", Parameter: Parameter{Type: PropPath, Path: PathDir}}},
		Thing{"id": "d1", "title": "Reference", "path": "/fixture/reference"})
	add("document", []PropSpec{
		{Name: "about", Parameter: Parameter{Noun: AnyNoun}},
		{Name: "file", Parameter: Parameter{Type: PropPath}},
		{Name: "url", Parameter: Parameter{Type: PropURL}},
	}, Thing{"id": "x1", "title": "Handbook", "about": "directory:d1", "file": "/fixture/reference/guide.md", "url": "https://example.test/guide"})
	if err := r.DeclareRelationKind(RelationKind{Name: "workspace", From: "task", To: "directory", Prop: "directories", Inverse: "tasks"}); err != nil {
		t.Fatal(err)
	}
	relations := NewMemRelations()
	r.SetRelationStore(relations)
	if err := relations.Put(Relation{Kind: "workspace", From: "task:t1", To: "directory:d1"}); err != nil {
		t.Fatal(err)
	}
	r.Instance("origin", Kind("project")).Data = Thing{"id": "p1", "title": "Home"}
	if err := r.SetFocus("origin"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 120, 30
	m.Appearance.Theme = "mono"
	return r, m, r.Context()
}

func thingWalkPicker(t *testing.T, r *Runtime, p Parameter, origin Context, row func(Choice) PickerRow) (*Chooser, *PickerRow) {
	t.Helper()
	var chosen PickerRow
	c, err := r.OpenPicker(PickerSpec{
		Kind: "walk-test", Sources: []PickerSource{r.walkSource(p, origin, row)}, NoSort: true, NoPropFilters: true,
		Choose: func(row PickerRow) Result { chosen = row; return Done(row.ID) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, &chosen
}

func thingWalkKey(t *testing.T, r *Runtime, key string) {
	t.Helper()
	if _, err := r.HandleBinding(key); err != nil {
		t.Fatalf("key %s: %v", key, err)
	}
}

func thingWalkFind(t *testing.T, c *Chooser, query, noun, id string) PickerRow {
	t.Helper()
	c.SetQuery(query)
	for _, row := range c.Matches() {
		if !row.Header && row.Noun == c.r.CanonicalNoun(noun) && row.ID == id {
			return row
		}
	}
	t.Fatalf("%q has no %s %s: %+v", query, noun, id, c.Matches())
	return PickerRow{}
}

func TestThingWalkThreeHopSubjectAndOpaqueChoice(t *testing.T) {
	r, _, origin := thingWalkFixture(t)
	c, chosen := thingWalkPicker(t, r, Parameter{Noun: "document"}, origin, func(ch Choice) PickerRow {
		row := ch.Row()
		row.ID, row.Data = "take:"+ch.ID, "handle:"+ch.ID
		return row
	})
	if r.BaseMode() != "normal" {
		t.Fatal("walk did not open ready for navigation")
	}
	task := thingWalkFind(t, c, "Write guide", "task", "t1")
	if !task.Opens || !task.NoPick {
		t.Fatalf("unmatched task cannot be walked through: %+v", task)
	}
	thingWalkKey(t, r, "enter") // a non-fitting thing walks, rather than refusing
	thingWalkFind(t, c, "Reference", "directory", "d1")
	thingWalkKey(t, r, "l")
	row := thingWalkFind(t, c, "Handbook", "document", "take:x1")
	if row.Opens || row.NoPick || !strings.Contains(row.Label, "✓") || row.Props["file"] != "/fixture/reference/guide.md" {
		t.Fatalf("needed thing is not marked and takeable: %+v", row)
	}
	if crumb := c.current().Breadcrumb(); !strings.Contains(crumb, "Home › Write guide › Reference") {
		t.Fatalf("walk breadcrumb %q", crumb)
	}
	// Even a fitting thing can still be traversed using l, then h restores
	// both its parent filter and its selected callback-defined row id.
	thingWalkKey(t, r, "l")
	if !strings.Contains(c.current().Breadcrumb(), "Handbook") {
		t.Fatal("l did not traverse the matching thing")
	}
	thingWalkKey(t, r, "h")
	if c.Query() != "Handbook" {
		t.Fatalf("back lost the focused column filter: %q", c.Query())
	}
	thingWalkKey(t, r, "enter")
	if chosen.ID != "take:x1" || chosen.Data != "handle:x1" || chosen.Props["title"] != "Handbook" || r.Chooser("walk-test") != nil {
		t.Fatalf("take lost the choice or did not close: %+v", chosen)
	}
}

func TestThingWalkRelatedPathAndValueKinds(t *testing.T) {
	r, _, origin := thingWalkFixture(t)
	c, chosen := thingWalkPicker(t, r, Parameter{Type: PropPath}, origin, func(ch Choice) PickerRow {
		row := ch.Row()
		row.Data = ch.ID
		return row
	})
	type step struct{ query, noun, id string }
	for _, s := range []step{{"Write guide", "task", "t1"}, {"Reference", "directory", "d1"}, {"Handbook", "document", "x1"}} {
		thingWalkFind(t, c, s.query, s.noun, s.id)
		thingWalkKey(t, r, "l")
	}
	c.SetQuery("")
	var file, url PickerRow
	for _, row := range c.Matches() {
		if row.ID == "/fixture/reference/guide.md" {
			file = row
		}
		if row.ID == "https://example.test/guide" {
			url = row
		}
	}
	if file.NoPick || !strings.Contains(file.Label, "✓") || !strings.Contains(file.Label, "file:") {
		t.Fatalf("path prop is not marked by name: %+v", file)
	}
	if !url.NoPick || url.Refuse == "" {
		t.Fatalf("link should remain visible but not satisfy path: %+v", url)
	}
	thingWalkFind(t, c, "guide.md", "document", "/fixture/reference/guide.md")
	thingWalkKey(t, r, "enter")
	if chosen.ID != "/fixture/reference/guide.md" || chosen.Data != "/fixture/reference/guide.md" {
		t.Fatalf("path argument was not the property's value: %+v", chosen)
	}
}

func TestThingWalkRelationsBothDirectionsAndAnyNounKinds(t *testing.T) {
	r, _, origin := thingWalkFixture(t)
	c, _ := thingWalkPicker(t, r, Parameter{Noun: AnyNoun}, origin, nil)
	thingWalkFind(t, c, "Write guide", "task", "task:t1")
	thingWalkKey(t, r, "l")
	thingWalkFind(t, c, "Home", "project", "project:p1") // outgoing owner prop
	thingWalkFind(t, c, "Reference", "directory", "directory:d1")
	thingWalkKey(t, r, "l")
	thingWalkFind(t, c, "Write guide", "task", "task:t1")      // inbound relation kind
	thingWalkFind(t, c, "Handbook", "document", "document:x1") // inbound any-noun prop
	// Undeclared relation kinds, and kinds with any-noun ends, are graph
	// edges too; they must not disappear just because Referrers is static.
	if err := r.RelationStore().Put(Relation{Kind: "mentions", From: "project:p1", To: "directory:d1"}); err != nil {
		t.Fatal(err)
	}
	c.SetQuery("Home")
	thingWalkFind(t, c, "Home", "project", "project:p1")
	thingWalkKey(t, r, "l")
	thingWalkFind(t, c, "Reference", "directory", "directory:d1")
}

func TestThingWalkQueryWindowsAndColumnEdges(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	big := newCountingStore(10000, 0)
	store := big.store(true, false)
	query := store.Query
	var asked []ListQuery
	store.Query = func(c Ctx, q ListQuery) (ListPage, error) {
		if q.PerPage <= 0 || q.PerPage > thingWalkPageSize {
			t.Fatalf("walk read a whole or unbounded table: %+v", q)
		}
		asked = append(asked, q)
		return query(c, q)
	}
	r.Define("big").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(store)
	c, _ := thingWalkPicker(t, r, Parameter{Noun: "big"}, r.Context(), nil)
	c.SetQuery("big")
	rows := c.Matches()
	if len(rows) != 1 || rows[0].Label != "big (10000)" {
		t.Fatalf("noun column counts: %+v", rows)
	}
	thingWalkKey(t, r, "l")
	if n := len(c.Matches()); n != thingWalkPageSize+1 {
		t.Fatalf("first window rows %d", n)
	}
	thingWalkKey(t, r, "G")
	thingWalkKey(t, r, "enter") // Next page is a row, not a read-all continuation
	if row, _ := c.Highlighted(); row.Label != "Previous page" {
		t.Fatalf("second window did not expose backwards paging: %+v", row)
	}
	thingWalkFind(t, c, "thing 9999", "big", "t09999")
	if q := asked[len(asked)-1]; q.Page != 1 || q.Text != "thing 9999" {
		t.Fatalf("column filter did not reset and search at the store: %+v", q)
	}
	thingWalkKey(t, r, "0")
	if crumb := c.current().Breadcrumb(); crumb != "Nouns" {
		t.Fatalf("first column %q", crumb)
	}
	thingWalkKey(t, r, "$")
	if c.Query() != "thing 9999" || !strings.Contains(c.current().Breadcrumb(), "big") {
		t.Fatalf("last column lost visited state: %q / %q", c.Query(), c.current().Breadcrumb())
	}
	if big.lists != 0 || big.rows > 3*thingWalkPageSize {
		t.Fatalf("windowed store read %d whole lists / %d rows", big.lists, big.rows)
	}
}

func TestThingWalkColumnsSettingAndFocusedRecentNouns(t *testing.T) {
	r, m, origin := thingWalkFixture(t)
	c, _ := thingWalkPicker(t, r, Parameter{Noun: "document"}, origin, nil)
	thingWalkFind(t, c, "Write guide", "task", "t1")
	thingWalkKey(t, r, "l")
	thingWalkFind(t, c, "Reference", "directory", "d1")
	thingWalkKey(t, r, "l")
	landLoads(m, r.PendingCommands())
	for n := 1; n <= 3; n++ {
		if _, err := r.Settings().SetSession(WalkColumnsSetting, n); err != nil {
			t.Fatal(err)
		}
		text := ansi.Strip(c.View(120, 20).Text)
		separators := 0
		for _, line := range strings.Split(text, "\n") {
			separators = max(separators, strings.Count(line, " │ "))
		}
		if separators != n-1 || !strings.Contains(text, "Nouns › project › Home › Write guide › Reference") {
			t.Fatalf("%d columns have %d boundaries or lost breadcrumb:\n%s", n, separators, text)
		}
	}
	thingWalkKey(t, r, "0")
	c.SetQuery("")
	if row := c.Matches()[0]; !strings.HasPrefix(row.Label, "project (") {
		t.Fatalf("focused noun not first: %+v", row)
	}
	thingWalkKey(t, r, "esc")
	if err := r.SetFocus("app"); err != nil {
		t.Fatal(err)
	}
	c, _ = thingWalkPicker(t, r, Parameter{}, r.Context(), nil)
	if row := c.Matches()[0]; !strings.HasPrefix(row.Label, "directory (") {
		t.Fatalf("recent noun not first with no focused thing: %+v", row)
	}
}

func TestThingWalkPlainReferencesWithoutRelationStore(t *testing.T) {
	r, _, origin := thingWalkFixture(t)
	r.SetRelationStore(nil)
	c, _ := thingWalkPicker(t, r, Parameter{Noun: "task"}, origin, nil)
	thingWalkFind(t, c, "Write guide", "task", "t1")
	thingWalkKey(t, r, "l")
	thingWalkFind(t, c, "Home", "project", "p1")
	if crumb := c.current().Breadcrumb(); !strings.HasSuffix(crumb, "Write guide") {
		t.Fatalf("plain-reference walk failed: %s", crumb)
	}
}

func TestThingWalkPreviewKeepsMixedNounIdentity(t *testing.T) {
	r, m, origin := thingWalkFixture(t)
	for _, noun := range []string{"bookmark", "chapter"} {
		table := NewMemDocTable(r, noun)
		r.Define(noun).Props(
			PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel},
			PropSpec{Name: "project", Role: RoleOwner, Parameter: Parameter{Noun: "project"}},
			PropSpec{Name: "file", Parameter: Parameter{Type: PropPath}},
		).Store(DocStore(table, noun))
		if err := table.Put("same", Thing{"id": "same", "title": "Shared", "project": "p1", "file": "/" + noun + ".md"}); err != nil {
			t.Fatal(err)
		}
	}
	c, _ := thingWalkPicker(t, r, Parameter{Type: PropPath}, origin, nil)
	c.SetQuery("Shared")
	landLoads(m, r.PendingCommands())
	first, _ := c.Highlighted()
	if first.ID != "same" || !strings.Contains(c.Preview.Text, "/bookmark.md") {
		t.Fatalf("first mixed-noun preview: %+v / %s", first, c.Preview.Text)
	}
	thingWalkKey(t, r, "j")
	landLoads(m, r.PendingCommands())
	second, _ := c.Highlighted()
	if second.ID != "same" || first.Noun == second.Noun || !strings.Contains(c.Preview.Text, "/chapter.md") {
		t.Fatalf("same foreign key reused the wrong noun's preview: %+v / %s", second, c.Preview.Text)
	}
	thingWalkKey(t, r, "l")
	landLoads(m, r.PendingCommands())
	thingWalkKey(t, r, "h")
	restored, _ := c.Highlighted()
	if restored.Noun != second.Noun || c.Query() != "Shared" {
		t.Fatalf("backtracking lost the selected noun with the same key: %+v", restored)
	}
}

func TestThingWalkRelationWindowsKeepPositionAndBoundEndpointReads(t *testing.T) {
	r, _, origin := thingWalkFixture(t)
	big := newCountingStore(205, 0)
	big.things[40]["title"] = "Last-window needle"
	store := big.store(true, false)
	store.Remote = true
	query := store.Query
	store.Query = func(c Ctx, q ListQuery) (ListPage, error) {
		for _, filter := range q.Filters {
			if filter.Op != "" && filter.Op != "=" {
				t.Fatalf("relation window needs an unsupported whole-table filter: %+v", filter)
			}
		}
		return query(c, q)
	}
	r.Define("ledger").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(store)
	if err := r.DeclareRelationKind(RelationKind{Name: "reading", From: "project", To: "ledger"}); err != nil {
		t.Fatal(err)
	}
	for i := range big.things {
		key := big.things[len(big.things)-1-i]["id"].(string)
		if err := r.RelationStore().Put(Relation{Kind: "reading", From: "project:p1", To: "ledger:" + key, Position: i}); err != nil {
			t.Fatal(err)
		}
	}
	c, chosen := thingWalkPicker(t, r, Parameter{Noun: "ledger"}, origin, nil)
	window := func(count int, first, last string) {
		t.Helper()
		var rows []PickerRow
		for _, row := range c.Matches() {
			if row.Noun == r.CanonicalNoun("ledger") {
				rows = append(rows, row)
			}
		}
		if len(rows) != count || rows[0].ID != first || rows[len(rows)-1].ID != last {
			t.Fatalf("relation position window: %+v; want %d rows %s through %s", rows, count, first, last)
		}
	}
	window(80, "t00204", "t00125")
	if big.gets != 80 {
		t.Fatalf("first relation window fetched %d endpoints, want 80", big.gets)
	}
	thingWalkKey(t, r, "G")
	thingWalkKey(t, r, "enter")
	window(80, "t00124", "t00045")
	if big.gets != 160 {
		t.Fatalf("second relation window fetched %d endpoints in all, want 160", big.gets)
	}
	thingWalkKey(t, r, "G")
	thingWalkKey(t, r, "enter")
	window(45, "t00044", "t00000")
	// A filtered remote relation searches every candidate key, not only the
	// current page. No whole-table list is needed to find a later match.
	c.SetQuery("needle")
	if big.gets != 410 || big.lists != 0 {
		t.Fatalf("global endpoint search read %d keys / %d whole tables, want 410 / 0", big.gets, big.lists)
	}
	window(1, "t00040", "t00040")
	thingWalkKey(t, r, "G")
	thingWalkKey(t, r, "enter")
	if chosen.ID != "t00040" || chosen.Props["title"] != "Last-window needle" {
		t.Fatalf("later filtered relation endpoint could not be selected: %+v", chosen)
	}
}
