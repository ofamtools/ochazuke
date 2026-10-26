package ochazuke

import (
	"context"
	"fmt"
	"slices"
	"testing"
)

func ids(items []ListItem) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID
	}
	return out
}

func TestPatchItemsPlacesUpsertsAndKeepsTheRest(t *testing.T) {
	rows := []ListItem{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	for _, tc := range []struct {
		name string
		p    ListPatch
		want []string
	}{
		{"replace in place", ListPatch{Upsert: []ListItem{{ID: "b", Label: "B"}}}, []string{"a", "b", "c", "d"}},
		{"append new", ListPatch{Upsert: []ListItem{{ID: "e"}}, Remove: []string{"a"}}, []string{"b", "c", "d", "e"}},
		{"insert at", ListPatch{Upsert: []ListItem{{ID: "e"}}, At: []int{1}}, []string{"a", "e", "b", "c", "d"}},
		{"move to top", ListPatch{Upsert: []ListItem{{ID: "d", Label: "D"}}, At: []int{0}}, []string{"d", "a", "b", "c"}},
		{"several", ListPatch{Upsert: []ListItem{{ID: "x"}, {ID: "c", Label: "C"}, {ID: "y"}}, At: []int{0, 2, 4}, Remove: []string{"b"}}, []string{"x", "a", "c", "d", "y"}},
	} {
		got, err := PatchItems(rows, tc.p)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !slices.Equal(ids(got), tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, ids(got), tc.want)
		}
	}
	if got, _ := PatchItems(rows, ListPatch{Upsert: []ListItem{{ID: "b", Label: "B"}}}); got[1].Label != "B" || rows[1].Label != "" {
		t.Fatalf("the upsert replaces the row in the result only: %+v / %+v", got[1], rows[1])
	}
	for _, bad := range []ListPatch{
		{Upsert: []ListItem{{ID: "e"}}, At: []int{9}},
		{Upsert: []ListItem{{ID: "e"}, {ID: "f"}}, At: []int{2, 1}},
		{Upsert: []ListItem{{ID: "e"}}, At: []int{0, 1}},
		{Upsert: []ListItem{{ID: "e"}, {ID: "e"}}},
		{Upsert: []ListItem{{ID: "a"}}, Remove: []string{"a"}},
	} {
		if _, err := PatchItems(rows, bad); err == nil {
			t.Errorf("PatchItems(%+v) accepted", bad)
		}
	}
}

// A patch reuses every kept row's instance, keeps the selection and marks
// by id, and closes only the removed row's instance (its job is cancelled;
// a kept row's job keeps running).
func TestListPatchKeepsInstancesSelectionMarksAndJobs(t *testing.T) {
	r, list, _, items := listFixture(t)
	if err := list.Select("two"); err != nil {
		t.Fatal(err)
	}
	if err := list.ToggleMark("three"); err != nil {
		t.Fatal(err)
	}
	before := map[string]*Instance{}
	for _, item := range items {
		before[item.ID] = r.Instances[item.ID]
	}
	wait := func(ctx context.Context) (any, error) { <-ctx.Done(); return nil, ctx.Err() }
	kept, err := r.Launch("two", "test", Pending(wait), Ctx{Runtime: r})
	if err != nil {
		t.Fatal(err)
	}
	dropped, err := r.Launch("one", "test", Pending(wait), Ctx{Runtime: r})
	if err != nil {
		t.Fatal(err)
	}
	patch := ListPatch{Upsert: []ListItem{{ID: "two", Label: "Second, renamed", Noun: "b"}, {ID: "four", Label: "Fourth", Noun: "a"}}, At: []int{0, 2}, Remove: []string{"one"}}
	if err := list.Patch(patch); err != nil {
		t.Fatal(err)
	}
	if got := ids(list.Items()); !slices.Equal(got, []string{"two", "three", "four"}) {
		t.Fatalf("rows = %v", got)
	}
	if r.Instances["two"] != before["two"] || r.Instances["three"] != before["three"] {
		t.Fatal("a kept row's instance was recreated")
	}
	if r.Instances["one"] != nil || r.Instances["four"] == nil {
		t.Fatal("the removed row's instance stays or the added one is missing")
	}
	if r.Instances["two"].Label != "Second, renamed" {
		t.Fatalf("the upserted row's label = %q", r.Instances["two"].Label)
	}
	if sel, _ := list.Selected(); sel.ID != "two" {
		t.Fatalf("selection = %s, want two (by id)", sel.ID)
	}
	if marked := list.Marked(); len(marked) != 1 || marked[0].ID != "three" {
		t.Fatalf("marks = %v", marked)
	}
	states := map[uint64]string{}
	for _, j := range r.Jobs() {
		states[j.Job] = j.State
	}
	if states[kept.Job] != JobRunning || states[dropped.Job] != JobCancelled {
		t.Fatalf("jobs = %v: the kept row's runs, the removed row's is cancelled", states)
	}
	if err := list.Patch(ListPatch{Upsert: []ListItem{{ID: "x", Noun: "nope"}}}); err == nil || list.Len() != 3 {
		t.Fatalf("an invalid patch must leave the list unchanged: %v, %d rows", err, list.Len())
	}
}

// SetItems with rows drawing the same cells keeps a table's measured
// widths; a changed cell remeasures.
func TestSetItemsKeepsWidthsWhileCellsStay(t *testing.T) {
	r := NewRuntime()
	r.Define("row")
	items := []ListItem{{ID: "1", Noun: "row", Cells: []string{"short", "x"}}, {ID: "2", Noun: "row", Cells: []string{"a much longer cell", "y"}}}
	list, err := r.AddList("t", items, Columns(Column{Title: "A"}, Column{Title: "B"}))
	if err != nil {
		t.Fatal(err)
	}
	list.View(80, 5)
	widths := list.widest
	if widths == nil {
		t.Fatal("drawing measures the widths")
	}
	again := slices.Clone(items)
	again[0].Data = "changed data, same cells"
	if err := list.SetItems(again); err != nil {
		t.Fatal(err)
	}
	if list.widest == nil || &list.widest[0] != &widths[0] {
		t.Fatal("unchanged cells must keep the measured widths")
	}
	again[1].Cells = []string{"tiny", "y"}
	if err := list.SetItems(again); err != nil {
		t.Fatal(err)
	}
	if list.widest != nil {
		t.Fatal("a changed cell must remeasure")
	}
}

func BenchmarkListPatchOneRow(b *testing.B) {
	r := NewRuntime()
	r.Define("row")
	items := make([]ListItem, 8000)
	for i := range items {
		items[i] = ListItem{ID: fmt.Sprint(i), Label: fmt.Sprint("row ", i), Noun: "row", Cells: []string{fmt.Sprint(i), "x"}}
	}
	list, err := r.AddList("t", items, Columns(Column{Title: "A"}, Column{Title: "B"}))
	if err != nil {
		b.Fatal(err)
	}
	b.Run("SetItems unchanged", func(b *testing.B) {
		for b.Loop() {
			_ = list.SetItems(items)
		}
	})
	b.Run("Patch one row", func(b *testing.B) {
		for i := 0; b.Loop(); i++ {
			_ = list.Patch(ListPatch{Upsert: []ListItem{{ID: "17", Label: fmt.Sprint("row 17 v", i), Noun: "row", Cells: []string{"17", "x"}}}})
		}
	})
}
