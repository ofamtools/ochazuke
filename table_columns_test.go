package ochazuke

import (
	"fmt"
	"slices"
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
	r, m, l := columnTable(t, 44) // 36 for the columns, 8 for the edge markers
	if got := header(m); got != "name alpha beta 2›" {
		t.Fatalf("unscrolled header %q", got)
	}
	for _, step := range []struct {
		keys, header string
		offset       int
	}{
		{"$", "name ‹2 gamma delta", 3},
		{"z h", "name ‹1 beta gamma 1›", 2},
		{"z h", "name alpha beta 2›", 0}, // offset 1 draws what 0 does: it is 0
		{"z h", "name alpha beta 2›", 0}, // the unscrolled view; the edge is quiet
		{"2 z l", "name ‹2 gamma delta", 3},
		{"z l z l z l", "name ‹2 gamma delta", 3}, // never past the last column
		{"^", "name alpha beta 2›", 0},
		{"$ 0", "name alpha beta 2›", 0},
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
	if !r.Operations["ochazuke.list.scroll-columns"].Motion {
		t.Fatal("list.scroll-columns is not a motion")
	}
	if hints := BindingHints(r, r.Focus); strings.Contains(hints, "scroll-columns") {
		t.Fatalf("footer shows the column motion: %s", hints)
	}
}

// A width that shows every column puts a scrolled table back to offset 0.
func TestColumnOffsetClampsToTheWidth(t *testing.T) {
	r, m, l := columnTable(t, 44)
	motionKeys(t, r, m, "$")
	m.Width = 120
	_ = m.View()
	if l.ColumnOffset() != 0 || header(m) != "name alpha beta gamma delta" {
		t.Fatalf("wide: offset %d header %q", l.ColumnOffset(), header(m))
	}
}

// columnVerbs are the list verbs that act on a table's columns: the z
// column rows (z l, z h, z <, z >, z f, z F, z =, z p) and $ ^ 0 run them.
var columnVerbs = []string{"scroll-columns", "resize-column", "fit-column", "fit-columns", "reset-columns", "toggle-pin-column"}

// zColumnRows are the z sequences at the focus that answer there (their
// verb available and their own guard holding) and run a column verb.
func zColumnRows(t *testing.T, r *Runtime) []string {
	t.Helper()
	var out []string
	for _, b := range r.ListBindings(r.Focus) {
		if !strings.HasPrefix(b.Keys, "z ") || b.Request.Noun != "ochazuke.list" || !slices.Contains(columnVerbs, b.Request.Verb) {
			continue
		}
		if b.When != "" {
			if holds, known := r.GuardHolds(b.When, r.Focus); !known || !holds {
				continue
			}
		}
		out = append(out, b.Keys+" "+b.Request.Verb)
	}
	return out
}

// On a table that fits and on a plain list the column keys do not answer:
// $ ^ 0 fall through, and no z sequence runs a column verb. z itself may
// still wait, for the rows every list answers (z / filters by a prop).
func TestColumnMotionsAnswerOnlyWhereColumnsAreHidden(t *testing.T) {
	r, m, _ := columnTable(t, 120)
	for _, key := range []string{"$", "^", "0"} {
		if ok, err := r.Dispatch(MacroKey{Key: key}); ok || err != nil {
			t.Fatalf("%s on a table that fits answered (%v, %v)", key, ok, err)
		}
		if r.Mode.Name != "normal" {
			t.Fatalf("%s left mode %q", key, r.Mode.Name)
		}
	}
	if rows := zColumnRows(t, r); len(rows) != 0 {
		t.Fatalf("z column rows answer on a table that fits: %v", rows)
	}
	// The same table drawn too narrow: z l and z h answer.
	narrow, _, _ := columnTable(t, 44)
	if rows := zColumnRows(t, narrow); !slices.Contains(rows, "z l scroll-columns") || !slices.Contains(rows, "z h scroll-columns") {
		t.Fatalf("z column rows on a table with hidden columns: %v", rows)
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
	v := colView{cols: cols, widest: widest, pins: []int{0}, rest: []int{1, 2, 3}, width: 34}
	if shown, _ := v.at(2); fmt.Sprint(shown) != "[0 2 3]" {
		t.Fatalf("offset 2 at 34: %v", shown)
	}
	// Too narrow for the first column beside column k: k alone.
	v.width = 15
	if shown, widths := v.at(3); fmt.Sprint(shown) != "[3]" || widths[0] != 10 {
		t.Fatalf("offset 3 at 15: %v %v", shown, widths)
	}
}
