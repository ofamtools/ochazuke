package ochazuke

import (
	"fmt"
	"strings"
	"testing"
)

// cellTable is columnTable in cell mode with stateDir as the state
// directory ("" keeps the table's view in memory).
func cellTable(t *testing.T, width int, stateDir string) (*Runtime, *Model, *List) {
	t.Helper()
	r := NewRuntime()
	r.Config().StateDir = stateDir
	m := NewModel(r)
	r.Define("row")
	cols := []Column{{Title: "name"}, {Title: "alpha", Priority: 1}, {Title: "beta", Priority: 2}, {Title: "gamma", Priority: 3}, {Title: "delta", Priority: 4}}
	var items []ListItem
	for i := 1; i <= 30; i++ {
		id := fmt.Sprintf("r%d", i)
		items = append(items, ListItem{ID: id, Label: id, Noun: "row", Cells: []string{id, fmt.Sprintf("a%09d", i), fmt.Sprintf("b%09d", i), fmt.Sprintf("c%09d", i), fmt.Sprintf("d%09d", i)}})
	}
	var l *List
	if _, err := r.AddPage("p", "P", func(_ *Runtime, w, _ int) Block { return Text(l.View(w, 12)) }); err != nil {
		t.Fatal(err)
	}
	var err error
	if l, err = r.AddList("t", items, Parent("p"), Columns(cols...)); err != nil {
		t.Fatal(err)
	}
	_ = r.OpenPage("p")
	_ = l.Focus()
	m.Width, m.Height = width, 20
	_ = m.View()
	return r, m, l
}

func TestCellMotionsMoveByColumnAndScrollItIntoView(t *testing.T) {
	r, m, l := cellTable(t, 36, "")
	for _, step := range []struct {
		keys        string
		col, offset int
		header      string
	}{
		{"l", 1, 0, "name alpha beta"},
		{"l", 2, 0, "name alpha beta"},
		{"l", 3, 2, "name beta gamma"}, // gamma was dropped by Priority: the view scrolls as little as shows it
		{"right", 4, 3, "name gamma delta"},
		{"l", 4, 3, "name gamma delta"}, // the last column: the edge is quiet
		{"h", 3, 3, "name gamma delta"},
		{"left h", 1, 0, "name alpha beta"}, // left of the view and shown unscrolled: back to the start
		{"$", 4, 3, "name gamma delta"},
		{"0", 0, 0, "name alpha beta"},
		{"end", 4, 3, "name gamma delta"},
		{"^", 0, 0, "name alpha beta"},
		{"3 |", 2, 0, "name alpha beta"},
		{"4 |", 3, 2, "name beta gamma"},
		{"|", 0, 0, "name alpha beta"},
		{"9 |", 4, 3, "name gamma delta"}, // clamped
		{"home 2 l", 2, 0, "name alpha beta"},
		{"z l", 2, 1, "name alpha beta"},      // still shown: the column stays
		{"z l z l", 3, 3, "name gamma delta"}, // beta scrolled off: the column is carried to the nearest shown
	} {
		motionKeys(t, r, m, step.keys)
		if got := header(m); l.Column() != step.col || l.ColumnOffset() != step.offset || got != step.header {
			t.Fatalf("%s: column %d offset %d header %q, want %d %d %q", step.keys, l.Column(), l.ColumnOffset(), got, step.col, step.offset, step.header)
		}
	}
	// j and k keep the column; 10j is a count, not 1 then 0.
	motionKeys(t, r, m, "j 1 0 j")
	if r.Focus != "r12" || l.Column() != 3 || r.Mode.Name != "normal" {
		t.Fatalf("j: focus %s column %d mode %s", r.Focus, l.Column(), r.Mode.Name)
	}
	if hints := BindingHints(r, r.Focus); strings.Contains(hints, "list.column") || strings.Contains(hints, "list.yank") {
		t.Fatalf("the footer shows the cell keys: %s", hints)
	}
}

// A plain list and a row-select table (a tree's list) keep h and l free
// for the levels outside the list.
func TestCellKeysAnswerOnlyOnACellModeTable(t *testing.T) {
	r, _, _, _ := motionPage(t)
	for _, key := range []string{"h", "l", "|", "y", "Y", "V"} {
		if ok, _ := r.Dispatch(MacroKey{Key: key}); ok {
			t.Fatalf("%s answered on a plain list", key)
		}
	}
	r2, _, _ := columnTable(t, 120, RowSelect())
	for _, key := range []string{"h", "l", "|", "y", "V", "home"} {
		if ok, _ := r2.Dispatch(MacroKey{Key: key}); ok {
			t.Fatalf("%s answered on a row-select table", key)
		}
	}
}

func TestCellSpanAndOffsetShowing(t *testing.T) {
	if x, w, ok := cellSpan([]int{0, 2, 3}, []int{5, 4, 6}, 3, 2); !ok || x != 2+5+2+4+2 || w != 6 {
		t.Fatalf("span of column 3: %d %d %v", x, w, ok)
	}
	if _, _, ok := cellSpan([]int{0, 2}, []int{5, 4}, 1, 0); ok {
		t.Fatal("a column not shown has a span")
	}
	_, _, l := cellTable(t, 36, "")
	cols, widest := l.layoutColumns(), l.cellWidths()
	most := l.maxColumnOffset(34)
	for _, c := range []struct{ col, cur, want int }{
		{0, 3, 0}, // the first column is the start of the row
		{1, 0, 0}, // shown unscrolled
		{3, 0, 2}, // right of the view: the smallest offset reaching it
		{1, 3, 0}, // left of the view, shown unscrolled
		{4, 1, 3},
	} {
		l.col = c.col
		if got := l.offsetShowing(cols, 34, c.cur, most); got != c.want {
			t.Fatalf("column %d from offset %d: %d, want %d", c.col, c.cur, got, c.want)
		}
		if shown, _ := layoutAt(cols, widest, 34, c.want); !containsInt(shown, c.col) {
			t.Fatalf("column %d not shown at offset %d: %v", c.col, c.want, shown)
		}
	}
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestCellYankCopiesTheCellAndTheRow(t *testing.T) {
	r, m, _ := cellTable(t, 80, "")
	motionKeys(t, r, m, "j 3 | y")
	if y, _ := r.ReadYank(""); y.Text != "b000000002" {
		t.Fatalf("y: %q", y.Text)
	}
	motionKeys(t, r, m, "Y")
	if y, _ := r.ReadYank(""); y.Text != "r2\ta000000002\tb000000002\tc000000002\td000000002" {
		t.Fatalf("Y: %q", y.Text)
	}
	if y, _ := r.ReadYank("0"); y.Text != "r2\ta000000002\tb000000002\tc000000002\td000000002" {
		t.Fatalf("Y did not reach register 0: %q", y.Text)
	}
}
