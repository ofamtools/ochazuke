package ochazuke

import (
	"slices"
	"testing"
)

// Items, Selected and Marked hand out copies of the rows' cells and
// tones, and SetItems keeps its own: an app that edits a row's cells in
// place (Items()[i].Cells[j] = ...) or reuses the slice it gave SetItems
// and calls SetItems again gets the table re-measured, never stale
// widths (found by table-fit).
func TestListItemsDoNotShareCells(t *testing.T) {
	r := NewRuntime()
	r.Instance("page")
	r.Define("row")
	given := []ListItem{{ID: "a", Label: "a", Noun: "row", Cells: []string{"x", "y"}, Tones: []ToneSpan{{Tone: "error"}}}}
	l, err := r.AddList("t", given, Parent("page"), Columns(Column{Title: "A"}, Column{Title: "B"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := l.cellWidths(); !slices.Equal(got, []int{1, 1}) {
		t.Fatalf("widths %v", got)
	}
	given[0].Cells[0] = "given-changed"
	if l.Items()[0].Cells[0] != "x" {
		t.Fatal("SetItems kept the caller's cell slice")
	}
	items := l.Items()
	items[0].Cells[0] = "a much wider cell"
	items[0].Tones[0].Tone = "ok"
	if l.Items()[0].Cells[0] != "x" || l.Items()[0].Tones[0].Tone != "error" {
		t.Fatal("Items shares the rows' cells or tones")
	}
	if sel, _ := l.Selected(); true {
		sel.Cells[1] = "zz"
		if l.Items()[0].Cells[1] != "y" {
			t.Fatal("Selected shares the row's cells")
		}
	}
	if err := l.SetItems(items); err != nil {
		t.Fatal(err)
	}
	if got := l.cellWidths(); got[0] != len("a much wider cell") {
		t.Fatalf("an in-place edit then SetItems kept stale widths %v", got)
	}
}
