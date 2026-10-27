package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// columnTable is a page holding one table of 30 rows and five columns, the
// first naming the row, drawn at width cells.
func columnTable(t *testing.T, width int, opts ...InstanceOption) (*Runtime, *Model, *List) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	r.Define("row")
	cols := []Column{{Title: "name"}, {Title: "alpha", Priority: 1}, {Title: "beta", Priority: 2}, {Title: "gamma", Priority: 3}, {Title: "delta", Priority: 4}}
	var items []ListItem
	for i := 1; i <= 30; i++ {
		id := fmt.Sprintf("r%d", i)
		items = append(items, ListItem{ID: id, Label: id, Noun: "row", Cells: []string{id, "aaaaaaaaaa", "bbbbbbbbbb", "cccccccccc", "dddddddddd"}})
	}
	var l *List
	if _, err := r.AddPage("p", "P", func(_ *Runtime, w, _ int) Block { return Text(l.View(w, 12)) }); err != nil {
		t.Fatal(err)
	}
	var err error
	if l, err = r.AddList("t", items, append([]InstanceOption{Parent("p"), Columns(cols...)}, opts...)...); err != nil {
		t.Fatal(err)
	}
	_ = r.OpenPage("p")
	_ = l.Focus()
	m.Width, m.Height = width, 20
	_ = m.View()
	return r, m, l
}

func header(m *Model) string {
	for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.Contains(line, "name") {
			return strings.Join(strings.Fields(line), " ")
		}
	}
	return ""
}

// A table without CellSelect selects rows and scrolls its columns with $,
// ^, 0 and z l/z h, as before v0.12.0; table_cells_test covers cell mode.
func TestColumnMotionsScrollTheHiddenColumnsIntoView(t *testing.T) {
	r, m, l := columnTable(t, 36)
	if got := header(m); got != "name alpha beta" {
		t.Fatalf("unscrolled header %q", got)
	}
	for _, step := range []struct {
		keys, header string
		offset       int
	}{
		{"$", "name gamma delta", 3},
		{"z h", "name beta gamma", 2},
		{"z h", "name alpha beta", 1},
		{"z h", "name alpha beta", 0}, // the unscrolled view; the edge is quiet
		{"2 z l", "name beta gamma", 2},
		{"z l z l z l", "name gamma delta", 3}, // never past the last column
		{"^", "name alpha beta", 0},
		{"$ 0", "name alpha beta", 0},
	} {
		motionKeys(t, r, m, step.keys)
		if got := header(m); got != step.header || l.ColumnOffset() != step.offset {
			t.Fatalf("%s: header %q offset %d, want %q offset %d", step.keys, got, l.ColumnOffset(), step.header, step.offset)
		}
		if r.Focus != "r1" || r.Mode.Name != "normal" {
			t.Fatalf("%s moved the focus or mode: %s %s", step.keys, r.Focus, r.Mode.Name)
		}
	}
	// After a digit 0 is the count's: 10j moves ten rows.
	motionKeys(t, r, m, "$ 1 0 j")
	if r.Focus != "r11" || l.ColumnOffset() != 3 {
		t.Fatalf("1 0 j: focus %s offset %d, want r11 at offset 3", r.Focus, l.ColumnOffset())
	}
	if !r.Operations["list.scroll-columns"].Motion {
		t.Fatal("list.scroll-columns is not a motion")
	}
	if hints := BindingHints(r, r.Focus); strings.Contains(hints, "scroll-columns") {
		t.Fatalf("footer shows the column motion: %s", hints)
	}
}

// A width that shows every column puts a scrolled table back to offset 0.
func TestColumnOffsetClampsToTheWidth(t *testing.T) {
	r, m, l := columnTable(t, 36)
	motionKeys(t, r, m, "$")
	m.Width = 120
	_ = m.View()
	if l.ColumnOffset() != 0 || header(m) != "name alpha beta gamma delta" {
		t.Fatalf("wide: offset %d header %q", l.ColumnOffset(), header(m))
	}
}

// On a table that fits and on a plain list the column keys do not answer,
// and z does not wait for a second key.
func TestColumnMotionsAnswerOnlyWhereColumnsAreHidden(t *testing.T) {
	r, m, _ := columnTable(t, 120)
	for _, key := range []string{"$", "^", "0", "z"} {
		if ok, err := r.Dispatch(MacroKey{Key: key}); ok || err != nil {
			t.Fatalf("%s on a table that fits answered (%v, %v)", key, ok, err)
		}
		if r.Mode.Name != "normal" {
			t.Fatalf("%s left mode %q", key, r.Mode.Name)
		}
	}
	_ = m
	r2, _, _, _ := motionPage(t)
	if ok, _ := r2.Dispatch(MacroKey{Key: "$"}); ok {
		t.Fatal("$ answered on a plain list")
	}
}

func TestScrolledLayoutKeepsTheFirstColumn(t *testing.T) {
	cols := []Column{{Title: "name"}, {Title: "a"}, {Title: "b"}, {Title: "c"}}
	widest := []int{10, 10, 10, 10}
	if shown, _ := scrolledLayout(cols, widest, 34, 2); fmt.Sprint(shown) != "[0 2 3]" {
		t.Fatalf("offset 2 at 34: %v", shown)
	}
	// Too narrow for the first column beside column k: k alone.
	if shown, widths := scrolledLayout(cols, widest, 15, 3); fmt.Sprint(shown) != "[3]" || widths[0] != 10 {
		t.Fatalf("offset 3 at 15: %v %v", shown, widths)
	}
}
