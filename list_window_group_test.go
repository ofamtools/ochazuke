package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func groupedWindowFixture(t *testing.T, size int) (*Runtime, *Collection, *MemDocTable, *[]ListQuery) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	for name, value := range map[string]any{ListWindowSetting: 3, ListGroupBySetting: "status"} {
		if _, err := r.Settings().SetSession(name, value); err != nil {
			t.Fatal(err)
		}
	}
	table := NewMemDocTable(r, "parcel")
	store := DocStore(table, "parcel")
	query := store.Query
	var asked []ListQuery
	store.Query = func(c Ctx, q ListQuery) (ListPage, error) {
		asked = append(asked, q)
		return query(c, q)
	}
	store.List = func(Ctx, ListQuery) ([]Thing, error) {
		t.Fatal("a grouped window must not read the whole store")
		return nil, nil
	}
	r.Define("parcel").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "status", Parameter: Parameter{Enum: []string{"alpha", "beta", "gamma"}}},
	).Store(store)
	statuses := []string{"alpha", "beta", "alpha", "gamma", "alpha", "beta", "alpha", "gamma"}
	for i := range size {
		id := fmt.Sprintf("p%d", i+1)
		if err := table.Put(id, Thing{"id": id, "title": fmt.Sprintf("Parcel %05d", i+1), "status": statuses[i%len(statuses)]}); err != nil {
			t.Fatal(err)
		}
	}
	var list *Collection
	if _, err := r.AddPage("work", "Work", func(_ *Runtime, width, height int) Block {
		return Text(list.View(width, height))
	}); err != nil {
		t.Fatal(err)
	}
	var err error
	list, err = r.AddList("parcels", nil, Parent("work"), From("parcel"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("work"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus(list.items[0].ID); err != nil {
		t.Fatal(err)
	}
	return r, list, table, &asked
}

func TestWindowGroupingPushesQueryAndKeepsBoundedPages(t *testing.T) {
	t.Parallel()
	r, list, table, asked := groupedWindowFixture(t, 20000)
	if q := (*asked)[0]; !slices.Equal(q.GroupBy, []string{"status"}) || q.Page != 1 || q.PerPage != 3 {
		t.Fatalf("source query lost grouping or bounds: %+v", q)
	}
	if table.Reads != 3 || list.WindowTotal() != 20000 {
		t.Fatalf("initial read=%d total=%d", table.Reads, list.WindowTotal())
	}
	if got := []string{list.items[0].ID, list.items[1].ID, list.items[2].ID}; !slices.Equal(got, []string{"parcels/p1", "parcels/p3", "parcels/p5"}) {
		t.Fatalf("group order: %v", got)
	}
	if text := ansi.Strip(list.View(80, 12)); !strings.Contains(text, "alpha (3 on page)") {
		t.Fatalf("missing bounded group count:\n%s", text)
	}
	mustRun(t, r, Request{Noun: "ochazuke.collection", Verb: "group", Args: Arguments{"id": list.id, "by": "status", "order": "status:desc"}})
	q := (*asked)[len(*asked)-1]
	if !slices.Equal(q.GroupSort, []SortKey{{Field: "status", Desc: true}}) || q.Page != 1 || q.PerPage != 3 {
		t.Fatalf("group order was not pushed down: %+v", q)
	}
	if list.items[0].Section != "gamma" || table.Reads != 6 {
		t.Fatalf("regrouped first=%+v reads=%d", list.items[0], table.Reads)
	}
	before := table.Reads
	if _, err := r.Settings().SetSession(ListGroupBySetting, ""); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "ochazuke.collection", Verb: "group-clear", Args: Arguments{"id": list.id}})
	if list.grouped || table.Reads > before+3 || list.items[0].Section != "" {
		t.Fatalf("group opt-out grouped=%v reads=%d first=%+v", list.grouped, table.Reads-before, list.items[0])
	}
}

func TestWindowGroupingFoldsAcrossPageAndGroupBoundaries(t *testing.T) {
	t.Parallel()
	r, list, table, _ := groupedWindowFixture(t, 8)
	mustRun(t, r, Request{Noun: "ochazuke.collection", Verb: "group-fold", Args: Arguments{"id": list.id}})
	if list.Shown() != 1 {
		t.Fatalf("an all-folded page must keep one navigable heading, got %d rows", list.Shown())
	}
	if item, _ := list.Selected(); item.Noun != "ochazuke.collection" || !strings.Contains(item.Label, "alpha (3 on page)") {
		t.Fatalf("the fold must not impersonate a stored record: %+v", item)
	}
	if text := ansi.Strip(list.View(80, 12)); strings.Contains(text, "Parcel 00001") || !strings.Contains(text, "alpha (3 on page)") {
		t.Fatalf("closed group:\n%s", text)
	}
	press(t, r, "j")
	if page, _ := list.Page(); page != 2 || list.Shown() != 3 {
		t.Fatalf("j must leave an all-folded page, page=%d shown=%d", page, list.Shown())
	}
	if item, _ := list.Selected(); !strings.Contains(item.Label, "alpha (1 on page)") {
		t.Fatalf("the continued group must stay folded: %+v", item)
	}
	press(t, r, "j")
	if item, _ := list.Selected(); item.ID != "parcels/p2" || r.Focus != item.ID {
		t.Fatalf("motion across groups selected=%+v focus=%s", item, r.Focus)
	}
	press(t, r, "k", "k")
	if page, _ := list.Page(); page != 1 || list.Shown() != 1 {
		t.Fatalf("k must cross back to folded page, page=%d shown=%d", page, list.Shown())
	}
	mustRun(t, r, Request{Noun: "ochazuke.collection", Verb: "group-fold", Args: Arguments{"id": list.id, "closed": false}})
	if item, _ := list.Selected(); list.Shown() != 3 || item.ID != "parcels/p1" || r.Focus != item.ID {
		t.Fatalf("unfold selection=%+v focus=%s shown=%d", item, r.Focus, list.Shown())
	}
	if table.Reads != 9 {
		t.Fatalf("folds should not read; two page motions should read six more rows, got %d", table.Reads)
	}
}

func TestWindowGroupingSortPreservesSourceGroupOrder(t *testing.T) {
	t.Parallel()
	r, list, _, asked := groupedWindowFixture(t, 8)
	mustRun(t, r, Request{Noun: "ochazuke.collection", Verb: "sort", Args: Arguments{"id": list.id, "by": "-title"}})
	if got := []string{list.items[0].ID, list.items[1].ID, list.items[2].ID}; !slices.Equal(got, []string{"parcels/p8", "parcels/p4", "parcels/p7"}) {
		t.Fatalf("source sorts then groups, widget must not sort that page again: %v", got)
	}
	if q := (*asked)[len(*asked)-1]; q.PerPage != 3 || !q.Grouped() || len(q.Sort) != 1 {
		t.Fatalf("sort query: %+v", q)
	}
}
