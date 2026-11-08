package ochazuke

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func sortFixture(t *testing.T, opts ...InstanceOption) (*Runtime, *Collection) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	r.Define("sort-row").Props(
		PropSpec{Name: "status", Parameter: Parameter{Enum: []string{"todo", "doing", "done"}}},
		PropSpec{Name: "score", Aliases: []string{"estimate"}, Parameter: Parameter{Type: "number"}},
		PropSpec{Name: "updated"}, PropSpec{Name: "title"},
		PropSpec{Name: "secret", Secret: true}, PropSpec{Name: "hidden", NoFilter: true},
	)
	items := []ListItem{
		{ID: "a", Noun: "sort-row", Label: "beta", Props: Thing{"score": 10, "status": "doing", "title": "beta", "updated": "2026-10-01T02:00:00+03:00"}, Cells: []string{"10", "doing"}},
		{ID: "b", Noun: "sort-row", Label: "Alpha", Props: Thing{"score": 2, "status": "todo", "title": "Alpha", "updated": "2026-09-30T23:30:00Z"}, Cells: []string{"2", "todo"}},
		{ID: "c", Noun: "sort-row", Label: "alpha", Props: Thing{"score": 2, "status": "done", "title": "alpha", "updated": "2026-10-02"}, Cells: []string{"2", "done"}},
		{ID: "d", Noun: "sort-row", Label: "blank", Props: Thing{"score": nil, "title": "", "updated": ""}, Cells: []string{"", ""}},
	}
	opts = append([]InstanceOption{Columns(Column{Title: "Score"}, Column{Title: "Status"}), CellSelect()}, opts...)
	l, err := r.AddList("sorted", items, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return r, l
}

func mustSort(t *testing.T, l *Collection, keys ...SortKey) {
	t.Helper()
	if err := l.SetSort(keys); err != nil {
		t.Fatal(err)
	}
}

func TestListSortOrder(t *testing.T) {
	_, l := sortFixture(t)
	for _, tc := range []struct {
		name string
		keys []SortKey
		want []string
	}{
		{"number", []SortKey{{Field: "score"}}, []string{"b", "c", "a", "d"}},
		{"number-desc", []SortKey{{Field: "score", Desc: true}}, []string{"a", "b", "c", "d"}},
		{"enum", []SortKey{{Field: "status"}}, []string{"b", "a", "c", "d"}},
		{"enum-desc", []SortKey{{Field: "status", Desc: true}}, []string{"c", "a", "b", "d"}},
		{"date-offset", []SortKey{{Field: "updated"}}, []string{"a", "b", "c", "d"}},
		{"date-desc", []SortKey{{Field: "updated", Desc: true}}, []string{"c", "b", "a", "d"}},
		{"text-stable", []SortKey{{Field: "title"}}, []string{"b", "c", "a", "d"}},
		{"multi", []SortKey{{Field: "score"}, {Field: "status", Desc: true}}, []string{"c", "b", "a", "d"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mustSort(t, l, tc.keys...)
			if got := shownIDs(l); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestListSortKeepsAppOrderSelectionAndRefresh(t *testing.T) {
	r, l := sortFixture(t)
	original := l.Items()
	_ = l.Select("b")
	_ = l.Focus()
	_ = l.ToggleMark("b")
	mustSort(t, l, SortKey{Field: "score", Desc: true})
	if r.Focus != "b" || l.items[l.selected].ID != "b" || !l.marks["b"] {
		t.Fatal("sort lost selection, focus or mark")
	}
	if !reflect.DeepEqual(l.Items(), original) {
		t.Fatal("sort changed app rows")
	}
	keys := l.Sort()
	keys[0].Field = "title"
	if l.Sort()[0].Field != "score" {
		t.Fatal("Sort returned internal storage")
	}
	// mtm's scopeExtList repeatedly replaces columns and rows on the same list.
	l.SetColumns(Column{Title: "Score"}, Column{Title: "Status"})
	refreshed := []ListItem{original[2], original[1], original[0], original[3]}
	refreshed[0].Props = Thing{"score": 30, "status": "done"}
	if err := l.SetItems(refreshed); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(shownIDs(l), []string{"c", "a", "b", "d"}) || r.Focus != "b" {
		t.Fatal(l.Snapshot())
	}
	if err := l.Patch(ListPatch{Upsert: []ListItem{{ID: "b", Noun: "sort-row", Label: "B", Props: Thing{"score": 40}}}}); err != nil {
		t.Fatal(err)
	}
	if shownIDs(l)[0] != "b" {
		t.Fatal("Patch forgot sort")
	}
	mustSort(t, l)
	if !slices.Equal(shownIDs(l), []string{"c", "b", "a", "d"}) {
		t.Fatal("clear did not restore latest app order")
	}
	mustSort(t, l, SortKey{Field: "score"})
	l.SetFilter("B")
	_ = l.Select("a") // revealing a hidden row clears filtering, but keeps sorting
	if !slices.Equal(shownIDs(l), []string{"a", "c", "b", "d"}) {
		t.Fatal("reveal bypassed sorting", shownIDs(l))
	}
}

func TestListSortFilterAndInvalidUpdates(t *testing.T) {
	r, l := sortFixture(t)
	_ = r.Settings().Set(ListFilterLingerSetting, "0")
	mustSort(t, l, SortKey{Field: "score", Desc: true})
	if err := l.SetPropFilters([]PropFilter{{Prop: "score", Op: "in", Value: []any{2}}}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(shownIDs(l), []string{"b", "c"}) {
		t.Fatal(shownIDs(l))
	}
	if err := l.SetSort([]SortKey{{Field: "missing"}}); err == nil {
		t.Fatal("accepted unknown field")
	}
	for _, name := range []string{"secret", "hidden"} {
		if err := l.SetSort([]SortKey{{Field: name}}); err == nil {
			t.Fatalf("off prop %s was sortable", name)
		}
	}
	if len(l.Sort()) != 1 || !l.Sort()[0].Desc {
		t.Fatal("invalid sort mutated state")
	}
	_ = l.SetPropFilters(nil)
	if !slices.Equal(shownIDs(l), []string{"a", "b", "c", "d"}) {
		t.Fatal("clearing filter lost descending sort")
	}
	if l.Filtering() {
		t.Fatal("sorting counted as a filter")
	}
}

func TestListSortDerivedPropsAndColumnFallback(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	r.Define("plain")
	l, err := r.AddList("columns", []ListItem{{ID: "a", Noun: "plain", Data: "opaque-a", Cells: []string{"Zulu"}}, {ID: "b", Noun: "plain", Data: "opaque-b", Cells: []string{"Alpha"}}}, Columns(Column{Title: "Display name"}))
	if err != nil {
		t.Fatal(err)
	}
	mustSort(t, l, SortKey{Field: "display-name"})
	if !slices.Equal(shownIDs(l), []string{"b", "a"}) {
		t.Fatal(shownIDs(l))
	}
	// Changing columns invalidates fallback values without losing the sort state.
	l.SetColumns(Column{Title: "Other"})
	l.SetColumns(Column{Title: "Display name"})
	if !slices.Equal(shownIDs(l), []string{"b", "a"}) {
		t.Fatal("SetColumns forgot sorting")
	}
	items := []ListItem{{ID: "a", Noun: "plain", Props: Thing{"n": json.Number("9007199254740993")}}, {ID: "b", Noun: "plain", Props: Thing{"n": int64(9007199254740992)}}}
	if err := l.SetItems(items); err != nil {
		t.Fatal(err)
	}
	mustSort(t, l, SortKey{Field: "n"})
	if !slices.Equal(shownIDs(l), []string{"b", "a"}) {
		t.Fatal("large numbers lost precision")
	}
	// Date-valued derived props use chronological order as well.
	items[0].Props = Thing{"when": time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
	items[1].Props = Thing{"when": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	_ = l.SetItems(items)
	mustSort(t, l, SortKey{Field: "when"})
	if shownIDs(l)[0] != "b" {
		t.Fatal("time.Time order")
	}
}

func TestListSortTreeSiblingsAndGuides(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	r.Define("node")
	nodes := []TreeNode{{ID: "z", Label: "Zulu", Noun: "node", Props: Thing{"order": 2}}, {ID: "zb", Parent: "z", Label: "B", Noun: "node", Props: Thing{"order": 2}}, {ID: "za", Parent: "z", Label: "A", Noun: "node", Props: Thing{"order": 1}}, {ID: "a", Label: "Alpha", Noun: "node", Props: Thing{"order": 1}}, {ID: "ac", Parent: "a", Label: "C", Noun: "node", Props: Thing{"order": 3}}}
	tree, err := r.AddTree("tree", nodes, Guides())
	if err != nil {
		t.Fatal(err)
	}
	l := tree.List()
	_ = l.Select("za")
	mustSort(t, l, SortKey{Field: "order"})
	if !slices.Equal(shownIDs(l), []string{"a", "ac", "z", "za", "zb"}) {
		t.Fatal(shownIDs(l))
	}
	if l.items[l.selected].ID != "za" {
		t.Fatal("tree selection lost")
	}
	if tree.guideLast["z"] != "zb" {
		t.Fatal("guides still use source sibling order")
	}
	if !reflect.DeepEqual(tree.Nodes(), nodes) {
		t.Fatal("sort changed source nodes")
	}
	_ = tree.SetCollapsed("z", true)
	_ = tree.SetCollapsed("z", false)
	if !slices.Equal(shownIDs(l), []string{"a", "ac", "z", "za", "zb"}) {
		t.Fatal("collapse forgot sorting")
	}
	_ = tree.SetNodes(nodes)
	mustSort(t, l)
	if !slices.Equal(shownIDs(l), []string{"z", "zb", "za", "a", "ac"}) {
		t.Fatal("clear did not restore tree")
	}
}

func TestListSortVerbsCycleIndicatorsAndOptOut(t *testing.T) {
	r, l := sortFixture(t)
	_ = l.Focus()
	run := func(verb string, args Arguments) error {
		_, err := r.Execute(Request{Noun: "collection", Verb: verb, Args: args})
		return err
	}
	for _, want := range []string{"Score ▲", "Score ▼", "Score"} {
		if err := run("sort-column", nil); err != nil {
			t.Fatal(err)
		}
		view := ansi.Strip(l.View(80, 8))
		if !strings.Contains(view, want) {
			t.Fatal(view)
		}
	}
	if len(l.Sort()) != 0 {
		t.Fatal("third sort-column did not clear")
	}
	if err := run("sort", Arguments{"by": "score,-status"}); err != nil {
		t.Fatal(err)
	}
	if view := ansi.Strip(l.View(80, 8)); !strings.Contains(view, "Status ▼2") {
		t.Fatal(view)
	}
	_ = r.Settings().Set(ListSortIndicatorSetting, false)
	if view := ansi.Strip(l.View(80, 8)); strings.ContainsAny(view, "▲▼") {
		t.Fatal(view)
	}
	if err := run("sort-column", Arguments{"desc": false}); err != nil {
		t.Fatal(err)
	}
	if len(l.Sort()) != 1 || l.Sort()[0].Desc {
		t.Fatal("explicit desc=false should set ascending")
	}
	if err := run("sort-clear", nil); err != nil {
		t.Fatal(err)
	}
	if err := run("sort", Arguments{"by": "score,score"}); err == nil {
		t.Fatal("accepted duplicate keys")
	}
	blockedRuntime, blocked := sortFixture(t, NoSort())
	_ = blocked.Focus()
	if _, err := blockedRuntime.Execute(Request{Noun: "collection", Verb: "sort", Args: Arguments{"by": "score"}}); err == nil {
		t.Fatal("source-owned list offered the sort verb")
	}
	if blocked.Sortable() {
		t.Fatal("source-owned list offers sort")
	}
	if err := blocked.SetSort([]SortKey{{Field: "score"}}); err == nil {
		t.Fatal("source-owned list sorted")
	}
	if err := blocked.SetSort(nil); err != nil {
		t.Fatal("clearing should be harmless")
	}
}

func TestListSortChooserController(t *testing.T) {
	r, m, c := chooserPropFixture(t)
	pressKeys(m, "ctrl+n") // keep the moved highlight by ID
	if _, err := r.Execute(Request{Noun: "collection", Verb: "sort", Args: Arguments{"by": "-score"}}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(chooserIDs(c), []string{"c", "b", "a"}) || highlighted(c) != "b" {
		t.Fatal(c.state())
	}
	c.SetRows([]ChooserRow{{ID: "a", Label: "alpha", Props: Thing{"score": 9}}, {ID: "b", Label: "beta", Props: Thing{"score": 2}}})
	if !slices.Equal(chooserIDs(c), []string{"a", "b"}) {
		t.Fatal("SetRows forgot sort")
	}
	c.spec.NoSort = true
	if c.filters.Sortable() {
		t.Fatal("NoSort chooser offers sort")
	}
	c.spec.NoSort = false
	c.spec.Sources = []ChooserSource{{Name: "live", Live: true}}
	c.source = 0
	if c.filters.Sortable() {
		t.Fatal("live source offers partial sorting")
	}
}

func TestListSortClearsAfterPropsDisappearAndAliases(t *testing.T) {
	r, l := sortFixture(t)
	l.SetColumns(Column{Title: "Estimate"})
	_ = l.Focus()
	if _, err := r.Execute(Request{Noun: "collection", Verb: "sort-column"}); err != nil {
		t.Fatal(err)
	}
	if l.Sort()[0].Field != "score" || !strings.Contains(ansi.Strip(l.View(80, 8)), "Estimate ▲") {
		t.Fatal(l.Snapshot())
	}
	r2 := NewRuntime()
	r2.Config().Disable()
	r2.Define("plain")
	derived, err := r2.AddList("derived", []ListItem{{ID: "only", Noun: "plain", Props: Thing{"value": 1}}})
	if err != nil {
		t.Fatal(err)
	}
	mustSort(t, derived, SortKey{Field: "value"})
	_ = derived.SetItems(nil)
	if _, err := r2.Execute(Request{Noun: "collection", Verb: "sort", Args: Arguments{"id": "derived", "by": ""}}); err != nil {
		t.Fatal(err)
	}
	if len(derived.Sort()) != 0 {
		t.Fatal("empty by could not clear an empty refreshed list")
	}
	for _, verb := range []string{"sort", "sort-column", "sort-clear"} {
		op, ok := r.Operation("collection." + verb)
		if !ok || !op.Builtin {
			t.Fatal("sort operation is not a framework builtin", verb)
		}
	}
}

func TestListSortMixedDateTextAndValueKinds(t *testing.T) {
	for _, tc := range []struct {
		p      PropSpec
		values []any
		want   []string
	}{
		{PropSpec{Name: "v", Parameter: Parameter{Type: "boolean"}}, []any{true, false, nil}, []string{"b", "a", "c"}},
		{PropSpec{Name: "v", Parameter: Parameter{Type: "duration"}}, []any{"20s", time.Second, nil}, []string{"b", "a", "c"}},
		{PropSpec{Name: "v"}, []any{"2026-10-01T01:00:00+03:00", "not a date", "2026-09-30T23:00:00Z"}, []string{"c", "a", "b"}},
	} {
		r := NewRuntime()
		r.Config().Disable()
		r.Define("row").Props(tc.p)
		var items []ListItem
		for i, v := range tc.values {
			items = append(items, ListItem{ID: string(rune('a' + i)), Noun: "row", Props: Thing{"v": v}})
		}
		l, err := r.AddList("l", items)
		if err != nil {
			t.Fatal(err)
		}
		mustSort(t, l, SortKey{Field: "v"})
		if !slices.Equal(shownIDs(l), tc.want) {
			t.Fatalf("%s: %v want %v", tc.p.Type, shownIDs(l), tc.want)
		}
	}
}

func TestListSortUnmappedColumnUsesFieldPicker(t *testing.T) {
	r, l := sortFixture(t)
	l.SetColumns(Column{Title: "Display only"})
	_ = l.Focus()
	if _, err := r.Execute(Request{Noun: "collection", Verb: "sort-column"}); err != nil {
		t.Fatal(err)
	}
	if r.Chooser("list-sort-field") == nil {
		t.Fatal("unmapped column did not offer existing props")
	}
	if _, err := r.Execute(Request{Noun: "chooser", Verb: "pick"}); err != nil {
		t.Fatal(err)
	}
	if len(l.Sort()) != 1 || l.Sort()[0].Field != "score" {
		t.Fatal(l.Sort())
	}
}
