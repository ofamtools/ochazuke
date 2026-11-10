package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWindowGroupingTableCountsAndFoldHeading(t *testing.T) {
	t.Parallel()
	r, list, table, _ := groupedWindowFixture(t, 8)
	list.SetColumns(Column{Title: "title", Min: 12})
	if err := list.SetPage(2); err != nil {
		t.Fatal(err)
	}
	if text := ansi.Strip(list.View(80, 14)); !strings.Contains(text, "alpha (1 on page)") || !strings.Contains(text, "beta (2 on page)") {
		t.Fatalf("table group counts:\n%s", text)
	}
	mustRun(t, r, Request{Noun: "ochazuke.collection", Verb: "group-fold", Args: Arguments{"id": list.id, "group": "beta", "closed": true}})
	if text := ansi.Strip(list.View(80, 14)); !strings.Contains(text, "beta (2 on page)") || strings.Contains(text, "Parcel 00002") || strings.Contains(text, "Parcel 00006") {
		t.Fatalf("folded table:\n%s", text)
	}
	if list.Shown() != 2 || table.Reads != 6 {
		t.Fatalf("folded table shown=%d reads=%d", list.Shown(), table.Reads)
	}
	if err := list.SetPage(1); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "ochazuke.collection", Verb: "group-fold", Args: Arguments{"id": list.id, "group": "alpha", "closed": true}})
	if text := ansi.Strip(list.View(80, 14)); !strings.Contains(text, "▸ alpha (3 on page)") || strings.Contains(text, "Parcel 00001") {
		t.Fatalf("all-folded table heading must span columns too:\n%s", text)
	}
	if list.Shown() != 1 || table.Reads != 9 {
		t.Fatalf("all-folded table shown=%d reads=%d", list.Shown(), table.Reads)
	}
}

func TestWindowGroupingCountsFollowQueryFilters(t *testing.T) {
	t.Parallel()
	r, list, table, asked := groupedWindowFixture(t, 8)
	if err := list.SetPropFilters([]PropFilter{{Prop: "status", Value: "beta"}}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "ochazuke.collection", Verb: "group", Args: Arguments{"id": list.id, "by": "status"}})
	if list.WindowTotal() != 2 || list.Shown() != 2 || table.Reads != 5 {
		t.Fatalf("filtered page total=%d shown=%d reads=%d", list.WindowTotal(), list.Shown(), table.Reads)
	}
	if text := ansi.Strip(list.View(80, 12)); !strings.Contains(text, "beta (2 on page)") || strings.Contains(text, "alpha (") {
		t.Fatalf("filtered group counts:\n%s", text)
	}
	if q := (*asked)[len(*asked)-1]; q.Page != 1 || q.PerPage != 3 || len(q.Filters) != 1 || !q.Grouped() {
		t.Fatalf("filtered bounded query: %+v", q)
	}
}

func TestWindowGroupingDerivesMissingPageLabelsWithoutReadingMore(t *testing.T) {
	t.Parallel()
	r, list, table, _ := groupedWindowFixture(t, 8)
	store := r.Noun("parcel").Store
	query := store.Query
	store.Query = func(c Ctx, q ListQuery) (ListPage, error) {
		page, err := query(c, q)
		page.Groups = nil // optional labels; rows still arrive in source group order
		return page, err
	}
	if err := list.SetPage(2); err != nil {
		t.Fatal(err)
	}
	if text := ansi.Strip(list.View(80, 14)); !strings.Contains(text, "alpha (1 on page)") || !strings.Contains(text, "beta (2 on page)") {
		t.Fatalf("labels derived from the bounded page:\n%s", text)
	}
	if table.Reads != 6 {
		t.Fatalf("label derivation read %d rows, want 6", table.Reads)
	}
}

func TestWindowGroupingSelectRevealsFoldedRecord(t *testing.T) {
	t.Parallel()
	r, list, table, _ := groupedWindowFixture(t, 8)
	mustRun(t, r, Request{Noun: "ochazuke.collection", Verb: "group-fold", Args: Arguments{"id": list.id}})
	if err := list.Select("parcels/p3"); err != nil {
		t.Fatal(err)
	}
	if item, _ := list.Selected(); item.ID != "parcels/p3" || list.Shown() != 3 || r.Focus != item.ID {
		t.Fatalf("explicit selection should unfold the held record: %+v shown=%d focus=%s", item, list.Shown(), r.Focus)
	}
	if table.Reads != 3 {
		t.Fatalf("revealing a held record must not read more rows: %d", table.Reads)
	}
}
