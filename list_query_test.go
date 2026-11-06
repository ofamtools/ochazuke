package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// lqFixture is a noun "item" of eight things over three statuses, listed
// by a store that records the query its List and Query were given.
type lqFixture struct {
	r      *Runtime
	things []Thing
	listed []ListQuery
	asked  []ListQuery
}

func newLQFixture(t *testing.T, own func(Ctx, ListQuery) (ListPage, error)) *lqFixture {
	t.Helper()
	f := &lqFixture{r: NewRuntime()}
	statuses := []string{"todo", "doing", "done", "todo", "done", "todo", "doing", "todo"}
	for i, s := range statuses {
		f.things = append(f.things, Thing{"id": fmt.Sprintf("a%d", i+1), "title": fmt.Sprintf("Item %d", i+1), "status": s, "n": 8 - i})
	}
	store := Store{
		Get: func(_ Ctx, id string) (Thing, error) {
			for _, th := range f.things {
				if th["id"] == id {
					return th, nil
				}
			}
			return nil, fmt.Errorf("no item %q", id)
		},
		List: func(_ Ctx, q ListQuery) ([]Thing, error) { f.listed = append(f.listed, q); return f.things, nil },
	}
	if own != nil {
		store.Query = func(c Ctx, q ListQuery) (ListPage, error) { f.asked = append(f.asked, q); return own(c, q) }
	}
	f.r.Define("item").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "status", Parameter: Parameter{Enum: []string{"todo", "doing", "done"}}},
		PropSpec{Name: "n", Parameter: Parameter{Type: SettingInteger}},
	).Store(store)
	return f
}

func (f *lqFixture) list(t *testing.T, args Arguments) ListAnswer {
	t.Helper()
	v, err := f.r.Execute(Request{Noun: "item", Verb: "list", Args: args})
	if err != nil {
		t.Fatalf("list %v: %v", args, err)
	}
	a, ok := v.(ListAnswer)
	if !ok {
		t.Fatalf("list answered %T, not a ListAnswer", v)
	}
	return a
}

func answerIDs(a ListAnswer) []string {
	out := make([]string, len(a.Items))
	for i, row := range a.Items {
		out[i] = row["id"].(string)
	}
	return out
}

func TestListAnswerShape(t *testing.T) {
	f := newLQFixture(t, nil)
	a := f.list(t, nil)
	if a.Page != 1 || a.PerPage != 0 || a.Total != 8 || len(a.Items) != 8 || a.Prev != 0 || a.Next != 0 {
		t.Fatalf("unpaged answer %+v", a)
	}
	if _, grouped := a.Items[0]["group"]; grouped {
		t.Fatal("an ungrouped answer names groups")
	}
	a = f.list(t, Arguments{"per-page": 3, "page": 2})
	if a.Page != 2 || a.PerPage != 3 || a.Total != 8 || a.Prev != 1 || a.Next != 3 || !slices.Equal(answerIDs(a), []string{"a4", "a5", "a6"}) {
		t.Fatalf("page 2 %+v", a)
	}
	a = f.list(t, Arguments{"per-page": 3, "page": 9})
	if a.Page != 3 || a.Next != 0 || a.Prev != 2 || !slices.Equal(answerIDs(a), []string{"a7", "a8"}) {
		t.Fatalf("a page past the end shows the last: %+v", a)
	}
	a = f.list(t, Arguments{"limit": 2})
	if a.Total != 8 || !slices.Equal(answerIDs(a), []string{"a1", "a2"}) {
		t.Fatalf("limit %+v", a)
	}
}

// A page counts items: a group runs on across the page edge and is named
// on both sides of it.
func TestListQueryPagingCountsItemsNotGroups(t *testing.T) {
	f := newLQFixture(t, nil)
	args := Arguments{"group-by": "status", "per-page": 3}
	var seen []string
	groupsOn := [][]string{}
	for page := 1; page <= 3; page++ {
		args["page"] = page
		a := f.list(t, args)
		if a.Total != 8 {
			t.Fatalf("page %d total %d: groups were counted", page, a.Total)
		}
		if page < 3 && len(a.Items) != 3 {
			t.Fatalf("page %d holds %d rows, want 3", page, len(a.Items))
		}
		var g []string
		for _, row := range a.Items {
			seen = append(seen, row["id"].(string))
			g = append(g, row["group"].(string))
		}
		groupsOn = append(groupsOn, g)
	}
	// todo: a1 a4 a6 a8, doing: a2 a7, done: a3 a5 (first appearance order).
	if want := []string{"a1", "a4", "a6", "a8", "a2", "a7", "a3", "a5"}; !slices.Equal(seen, want) {
		t.Fatalf("order %v want %v", seen, want)
	}
	if !slices.Equal(groupsOn[0], []string{"todo", "todo", "todo"}) || !slices.Equal(groupsOn[1], []string{"todo", "doing", "doing"}) || !slices.Equal(groupsOn[2], []string{"done", "done"}) {
		t.Fatalf("groups per page %v", groupsOn)
	}
}

func TestListQueryFilterSortGroupPage(t *testing.T) {
	f := newLQFixture(t, nil)
	// not done, newest n first within each group, groups by status desc
	// (todo before doing... enum order: todo, doing, done), two per page.
	args := Arguments{"filter": "status!=done", "sort": "n:desc", "group-by": "status", "group-sort": "status:desc", "per-page": 2, "page": 2}
	a := f.list(t, args)
	// rows not done: todo a1(8) a4(5) a6(3) a8(1), doing a2(7) a7(2); groups by enum rank desc: doing then todo.
	// order: doing a2 a7, todo a1 a4 a6 a8; page 2 of 2: a1 a4.
	if a.Total != 6 || !slices.Equal(answerIDs(a), []string{"a1", "a4"}) || a.Prev != 1 || a.Next != 3 {
		t.Fatalf("%+v", a)
	}
	for _, row := range a.Items {
		if row["group"] != "todo" {
			t.Fatalf("group %v", row)
		}
	}
	if len(f.listed) == 0 || f.listed[len(f.listed)-1].PerPage != 0 || f.listed[len(f.listed)-1].Page != 0 || len(f.listed[len(f.listed)-1].Filters) != 1 {
		t.Fatalf("a store's List is handed the conditions, never the paging: %+v", f.listed)
	}
}

func TestListQueryWordsFilter(t *testing.T) {
	f := newLQFixture(t, nil)
	f.r.Define("item").Filters(SavedFilter{Name: "open", Conditions: []PropFilter{{Prop: "status", Op: "!=", Value: "done"}}})
	for _, tc := range []struct {
		args Arguments
		want []string
	}{
		{Arguments{"filter": "status=doing"}, []string{"a2", "a7"}},
		{Arguments{"filter": "status=doing|done"}, []string{"a2", "a3", "a5", "a7"}},
		{Arguments{"filter": "status!=todo|doing"}, []string{"a3", "a5"}},
		{Arguments{"filter": "title~Item,n=8"}, []string{"a1"}},
		{Arguments{"filter": "open", "where": "n=7"}, []string{"a2"}},
		{Arguments{"filter": "open status=todo", "sort": "n"}, []string{"a8", "a6", "a4", "a1"}},
	} {
		got := answerIDs(f.list(t, tc.args))
		if !slices.Equal(got, tc.want) {
			t.Fatalf("%v: %v want %v", tc.args, got, tc.want)
		}
	}
	for _, bad := range []Arguments{{"filter": "nosuch"}, {"filter": "ghost=1"}, {"sort": "ghost"}, {"group-by": "status:desc"}, {"group-by": "status", "group-sort": "n"}} {
		if _, err := f.r.Execute(Request{Noun: "item", Verb: "list", Args: bad}); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
}

// A store with its own Query runs the whole query; the runtime does not
// filter, sort or page what it answers.
func TestListQueryStoreQuery(t *testing.T) {
	var f *lqFixture
	f = newLQFixture(t, func(_ Ctx, q ListQuery) (ListPage, error) {
		// The store answers page 2 of a longer result by itself.
		return ListPage{Items: []Thing{f.things[7], f.things[6]}, Total: 40, Groups: []string{"g", "g"}}, nil
	})
	a := f.list(t, Arguments{"filter": "status=todo", "sort": "n:desc", "group-by": "status", "per-page": 2, "page": 2})
	if a.Total != 40 || a.Page != 2 || a.Prev != 1 || a.Next != 3 || !slices.Equal(answerIDs(a), []string{"a8", "a7"}) || a.Items[0]["group"] != "g" {
		t.Fatalf("%+v", a)
	}
	if len(f.listed) != 0 || len(f.asked) != 1 {
		t.Fatalf("List %d Query %d: a store with Query is asked once, through Query", len(f.listed), len(f.asked))
	}
	q := f.asked[0]
	if q.Page != 2 || q.PerPage != 2 || len(q.Filters) != 1 || q.Filters[0].Prop != "status" || len(q.Sort) != 1 || !q.Sort[0].Desc || !slices.Equal(q.GroupBy, []string{"status"}) {
		t.Fatalf("query %+v", q)
	}
}

func TestListQueryArchivedFoldsIntoQuery(t *testing.T) {
	r := NewRuntime()
	things := []Thing{{"id": "a"}, {"id": "b", "gone": true}, {"id": "c", "gone": false}}
	r.Define("box").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "gone", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}}).Store(Store{List: func(Ctx, ListQuery) ([]Thing, error) { return things, nil }})
	ids := func(args Arguments) []string {
		v, err := r.Execute(Request{Noun: "box", Verb: "list", Args: args})
		if err != nil {
			t.Fatal(err)
		}
		return answerIDs(v.(ListAnswer))
	}
	if got := ids(nil); !slices.Equal(got, []string{"a", "c"}) {
		t.Fatal(got)
	}
	if got := ids(Arguments{"archived": true}); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatal(got)
	}
	if got := ids(Arguments{"per-page": 1, "page": 2}); !slices.Equal(got, []string{"c"}) {
		t.Fatalf("archived things are not counted: %v", got)
	}
}

// widgetFixture is a list of the same eight things as rows of noun "item".
func (f *lqFixture) widget(t *testing.T) *List {
	t.Helper()
	items := make([]ListItem, len(f.things))
	for i, th := range f.things {
		items[i] = ListItem{ID: th["id"].(string), Label: th["title"].(string), Noun: "item", Props: th}
	}
	f.r.Instance("p")
	l, err := f.r.AddList("p/l", items, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.AddPage("p", "P", func(r *Runtime, w, h int) Block { return Text(l.View(w, h)) }, Focus("a1")); err != nil {
		t.Fatal(err)
	}
	if err := f.r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	return l
}

func widgetIDs(l *List) []string {
	l.refilter("")
	out := make([]string, len(l.items))
	for i, it := range l.items {
		out[i] = it.ID
	}
	return out
}

// The widget and the CLI's list give the same rows for the same view.
func TestListQueryWidgetMatchesList(t *testing.T) {
	f := newLQFixture(t, nil)
	l := f.widget(t)
	for _, tc := range []struct {
		filters []PropFilter
		sort    []SortKey
		args    Arguments
	}{
		{nil, nil, nil},
		{[]PropFilter{{Prop: "status", Op: "=", Value: "todo"}}, []SortKey{{Field: "n", Desc: true}}, Arguments{"filter": "status=todo", "sort": "n:desc"}},
		{[]PropFilter{{Prop: "status", Op: "!=", Value: "todo"}}, []SortKey{{Field: "status"}, {Field: "n"}}, Arguments{"filter": "status!=todo", "sort": "status,n"}},
		{[]PropFilter{{Prop: "title", Op: "contains", Value: "Item"}, {Prop: "n", Op: "in", Value: []any{1, 2, 3}}}, []SortKey{{Field: "n"}}, Arguments{"filter": "title~Item n=1|2|3", "sort": "n"}},
	} {
		if err := l.SetPropFilters(tc.filters); err != nil {
			t.Fatal(err)
		}
		if err := l.SetSort(tc.sort); err != nil {
			t.Fatal(err)
		}
		got, want := widgetIDs(l), answerIDs(f.list(t, tc.args))
		if !slices.Equal(got, want) {
			t.Fatalf("%v: widget %v, list %v", tc.args, got, want)
		}
	}
}

// A widget groups and pages by the same function, from the settings.
func TestListQueryWidgetGroupedPaged(t *testing.T) {
	f := newLQFixture(t, nil)
	l := f.widget(t)
	if page, pages := l.Page(); page != 1 || pages != 1 || l.pageSummary() != "" || l.grouped {
		t.Fatalf("defaults page: %d/%d grouped %v", page, pages, l.grouped)
	}
	if got := shownIDs(l); len(got) != 8 {
		t.Fatalf("defaults show every row, got %v", got)
	}
	if err := f.r.Settings().Set(ListGroupBySetting, "status"); err != nil {
		t.Fatal(err)
	}
	if err := f.r.Settings().Set(ListPerPageSetting, 3); err != nil {
		t.Fatal(err)
	}
	if got, want := widgetIDs(l), []string{"a1", "a4", "a6"}; !slices.Equal(got, want) {
		t.Fatalf("page 1 %v want %v", got, want)
	}
	if page, pages := l.Page(); page != 1 || pages != 3 || l.pageSummary() != "page 1/3" || l.Shown() != 3 {
		t.Fatalf("page %d/%d shown %d", page, pages, l.Shown())
	}
	if frame := ansi.Strip(l.View(40, 10)); !strings.Contains(frame, "todo") || !strings.Contains(frame, "page 1/3") {
		t.Fatalf("frame lacks the group heading or the page indicator:\n%s", frame)
	}
	if err := l.SetPage(2); err != nil {
		t.Fatal(err)
	}
	if got, want := widgetIDs(l), []string{"a8", "a2", "a7"}; !slices.Equal(got, want) {
		t.Fatalf("page 2 %v want %v", got, want)
	}
	if l.items[0].Section != "todo" || l.items[1].Section != "doing" {
		t.Fatalf("sections %q %q", l.items[0].Section, l.items[1].Section)
	}
	if err := l.SetGroupBy([]string{"status"}, []SortKey{{Field: "status", Desc: true}}); err != nil {
		t.Fatal(err)
	}
	if got, want := widgetIDs(l), []string{"a3", "a5", "a2"}; !slices.Equal(got, want) {
		t.Fatalf("groups ordered by status desc: %v want %v", got, want)
	}
	// Turning the settings off restores the plain list.
	if err := l.SetGroupBy(nil, nil); err != nil {
		t.Fatal(err)
	}
	f.r.Settings().Set(ListGroupBySetting, "")
	f.r.Settings().Set(ListPerPageSetting, 0)
	if got := widgetIDs(l); len(got) != 8 || l.grouped || l.items[0].Section != "" {
		t.Fatalf("settings off: %v", got)
	}
}

// A page reveals the row the selection follows: filtering keeps the
// selected row on whichever page holds it.
func TestListQueryWidgetPageFollowsSelection(t *testing.T) {
	f := newLQFixture(t, nil)
	l := f.widget(t)
	f.r.Settings().Set(ListPerPageSetting, 3)
	l.refilter("a7")
	if page, _ := l.Page(); page != 3 {
		t.Fatalf("page %d, want 3 (a7 is the 7th row)", page)
	}
	if sel, _ := l.Selected(); sel.ID != "a7" {
		t.Fatalf("selected %q", sel.ID)
	}
}
