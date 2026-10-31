package ochazuke

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fitTable is a table whose note column holds one 40-cell cell and is
// capped at 12 by Max, so the automatic layout cuts it, and whose size
// column is narrow; stateDir as in cellTable.
func fitTable(t *testing.T, width int, stateDir string, opts ...InstanceOption) (*Runtime, *Model, *List) {
	t.Helper()
	r := NewRuntime()
	r.Config().StateDir = stateDir
	m := NewModel(r)
	r.Define("row")
	cols := []Column{{Title: "name"}, {Title: "note", Max: 12}, {Title: "size"}, {Title: "kind"}}
	var items []ListItem
	for i := 1; i <= 8; i++ {
		id := fmt.Sprintf("r%d", i)
		note := "short"
		if i == 5 {
			note = strings.Repeat("n", 40)
		}
		items = append(items, ListItem{ID: id, Label: id, Noun: "row", Cells: []string{id, note, "7", "file-of-kind-" + id}})
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

// drawnMap is the widths the last frame drew, by column.
func drawnMap(l *List) map[int]int {
	out := map[int]int{}
	for j, i := range l.drawn {
		out[i] = l.drawnWidths[j]
	}
	return out
}

func TestFitColumnFitsTheCurrentColumnAndStays(t *testing.T) {
	r, m, l := fitTable(t, 80, "", CellSelect())
	motionKeys(t, r, m, "l")
	if got := drawnMap(l)[1]; got != 12 {
		t.Fatalf("automatic note width %d, want its Max 12", got)
	}
	motionKeys(t, r, m, "z f")
	if got := l.ColumnWidths(); !maps.Equal(got, map[int]int{1: 40}) {
		t.Fatalf("z f: widths %v, want note at its widest cell 40", got)
	}
	frame := drawnMap(l)
	// Sticky: moving the cursor, across rows and columns, reflows nothing.
	motionKeys(t, r, m, "j j l l h h k G g g")
	if got := drawnMap(l); !maps.Equal(got, frame) {
		t.Fatalf("the cursor moved the widths: %v, was %v", got, frame)
	}
	// The title counts: size's cells are 1 wide, its title 4.
	motionKeys(t, r, m, "l z f")
	if got := l.ColumnWidths()[2]; got != 4 {
		t.Fatalf("z f on size: %d, want its title's 4", got)
	}
	// A count gives the width, as N ctrl-w | does.
	motionKeys(t, r, m, "2 0 z f")
	if got := l.ColumnWidths()[2]; got != 20 {
		t.Fatalf("20 z f: %d, want 20", got)
	}
	motionKeys(t, r, m, "z =")
	if len(l.ColumnWidths()) != 0 {
		t.Fatalf("z =: %v", l.ColumnWidths())
	}
}

func TestFitColumnIsCappedAtTheTableWidth(t *testing.T) {
	r, m, l := fitTable(t, 30, "", CellSelect())
	motionKeys(t, r, m, "l z f")
	if got := l.ColumnWidths()[1]; got != l.colWidth || got >= 40 {
		t.Fatalf("z f at 30 cells: %d, want the table's width %d", got, l.colWidth)
	}
}

func TestFitColumnsWithRoom(t *testing.T) {
	r, m, l := fitTable(t, 120, "", CellSelect())
	motionKeys(t, r, m, "z F")
	want := map[int]int{0: 4, 1: 40, 2: 4, 3: 15}
	if got := l.ColumnWidths(); !maps.Equal(got, want) {
		t.Fatalf("z F: %v, want %v", got, want)
	}
	if got := drawnMap(l); !maps.Equal(got, want) {
		t.Fatalf("z F drew %v, want %v", got, want)
	}
	motionKeys(t, r, m, "l l j j")
	if got := drawnMap(l); !maps.Equal(got, want) {
		t.Fatalf("the cursor moved the widths: %v", got)
	}
}

func TestFitColumnsWithoutRoomNarrowsTheWidestFirst(t *testing.T) {
	r, m, l := fitTable(t, 60, "", CellSelect())
	motionKeys(t, r, m, "z F")
	got := l.ColumnWidths()
	total := tableGap * 3
	for _, w := range got {
		total += w
	}
	if total > l.colWidth {
		t.Fatalf("z F at %d cells: widths %v take %d", l.colWidth, got, total)
	}
	// Only the widest (note) is cut; the others keep their content's width.
	if got[0] != 4 || got[2] != 4 || got[3] != 15 || got[1] >= 40 || got[1] < 15 {
		t.Fatalf("z F narrowed the wrong columns: %v", got)
	}
	// Narrower than every column's least width: they stop at their floors
	// (never below defaultColumnMin unless the content is narrower) and the
	// table scrolls.
	r2, m2, l2 := fitTable(t, 20, "", CellSelect())
	motionKeys(t, r2, m2, "z F")
	if got := l2.ColumnWidths(); got[1] != defaultColumnMin || got[3] != defaultColumnMin || got[0] != 4 {
		t.Fatalf("z F at 20 cells: %v", got)
	}
}

func TestFitFollowsCursorSetting(t *testing.T) {
	for _, on := range []bool{false, true} {
		r, m, l := fitTable(t, 80, "", CellSelect())
		if err := r.Settings().Set(FitFollowsCursorSetting, on); err != nil {
			t.Fatal(err)
		}
		_ = m.View()
		motionKeys(t, r, m, "l")
		note := drawnMap(l)[1]
		if on && note != 40 || !on && note != 12 {
			t.Fatalf("on=%v: note under the cursor drawn %d", on, note)
		}
		motionKeys(t, r, m, "l")
		if got := drawnMap(l)[1]; got != 12 {
			t.Fatalf("on=%v: note after the cursor left drawn %d, want 12", on, got)
		}
		if len(l.ColumnWidths()) != 0 {
			t.Fatalf("on=%v: the setting wrote widths %v", on, l.ColumnWidths())
		}
		// A width the user gave wins over the setting.
		motionKeys(t, r, m, "h 2 0 z f l h")
		if got := drawnMap(l)[1]; got != 20 {
			t.Fatalf("on=%v: given width drawn %d, want 20", on, got)
		}
	}
}

func TestFitOnARowSelectTable(t *testing.T) {
	dir := t.TempDir()
	r, m, l := fitTable(t, 120, dir)
	motionKeys(t, r, m, "z f")
	if len(l.ColumnWidths()) != 0 {
		t.Fatalf("z f answered on a row-select table: %v", l.ColumnWidths())
	}
	if r.Available(Request{Noun: "ochazuke.list", Verb: "fit-column"}, r.Focus, r.Mode) {
		t.Fatal("fit-column available on a row-select table")
	}
	motionKeys(t, r, m, "z F")
	want := map[int]int{0: 4, 1: 40, 2: 4, 3: 15}
	if got := l.ColumnWidths(); !maps.Equal(got, want) {
		t.Fatalf("z F on a row-select table: %v, want %v", got, want)
	}
	// Remembered across a restart, like a cell table's widths.
	_, _, l2 := fitTable(t, 120, dir)
	if got := l2.ColumnWidths(); !maps.Equal(got, want) {
		t.Fatalf("restored widths %v", got)
	}
	motionKeys(t, r, m, "z =")
	if len(l.ColumnWidths()) != 0 {
		t.Fatalf("z =: %v", l.ColumnWidths())
	}
	_, _, l3 := fitTable(t, 120, dir)
	if len(l3.ColumnWidths()) != 0 {
		t.Fatalf("z = not remembered: %v", l3.ColumnWidths())
	}
}

func TestRowSelectTableIgnoresUnversionedWidths(t *testing.T) {
	dir := t.TempDir()
	// Widths left by v0.12.0 (every table cell-based) carry no version.
	data, _ := json.Marshal(map[string]TableState{"t": {Widths: map[int]int{1: 30}}})
	if err := os.WriteFile(filepath.Join(dir, TablesFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, l := fitTable(t, 120, dir)
	if len(l.ColumnWidths()) != 0 {
		t.Fatalf("a row-select table took unversioned widths %v", l.ColumnWidths())
	}
	_, _, c := fitTable(t, 120, dir, CellSelect())
	if c.ColumnWidths()[1] != 30 {
		t.Fatalf("a cell table lost its widths %v", c.ColumnWidths())
	}
}

func TestNarrowToFit(t *testing.T) {
	for _, tc := range []struct {
		widths, floors []int
		avail          int
		want           []int
	}{
		{[]int{4, 40, 4}, []int{4, 10, 4}, 100, []int{4, 40, 4}},
		{[]int{4, 40, 4}, []int{4, 10, 4}, 30, []int{4, 18, 4}},
		{[]int{20, 20}, []int{10, 10}, 32, []int{15, 15}},
		{[]int{20, 20}, []int{10, 10}, 33, []int{16, 15}}, // ties narrow the rightmost
		{[]int{20, 20}, []int{10, 10}, 5, []int{10, 10}},
	} {
		if got := narrowToFit(tc.widths, tc.floors, tc.avail); !slices.Equal(got, tc.want) {
			t.Fatalf("narrowToFit(%v, %v, %d) = %v, want %v", tc.widths, tc.floors, tc.avail, got, tc.want)
		}
	}
}

func TestFitAndResetAnswerOnlyWhenTheyChangeSomething(t *testing.T) {
	r, m, _ := columnTable(t, 120) // every column at its content's width
	for _, verb := range []string{"fit-columns", "reset-columns"} {
		if r.Available(Request{Noun: "ochazuke.list", Verb: verb}, r.Focus, r.Mode) {
			t.Fatalf("%s available on a table with nothing to change", verb)
		}
	}
	if rows := zColumnRows(t, r); len(rows) != 0 {
		t.Fatalf("z column rows answer on a row-select table that fits: %v", rows)
	}
	r2, m2, l2 := fitTable(t, 120, "")
	if rows := zColumnRows(t, r2); !slices.Contains(rows, "z F fit-columns") || slices.Contains(rows, "z = reset-columns") {
		t.Fatalf("z column rows before z F: %v", rows)
	}
	motionKeys(t, r2, m2, "z F")
	if r2.Available(Request{Noun: "ochazuke.list", Verb: "fit-columns"}, r2.Focus, r2.Mode) {
		t.Fatal("z F available again right after z F")
	}
	if !r2.Available(Request{Noun: "ochazuke.list", Verb: "reset-columns"}, r2.Focus, r2.Mode) {
		t.Fatal("z = unavailable after z F")
	}
	// New rows wider than a fitted column: it stays (sticky) and z F
	// answers again.
	items := l2.Items()
	items[0].Cells = []string{"r1", "short", "7", strings.Repeat("k", 30)}
	l2.SetItems(items)
	_ = m2.View()
	if got := l2.ColumnWidths()[3]; got != 15 {
		t.Fatalf("new rows reflowed the fitted kind column: %d", got)
	}
	if !r2.Available(Request{Noun: "ochazuke.list", Verb: "fit-columns"}, r2.Focus, r2.Mode) {
		t.Fatal("z F unavailable with a wider cell to fit")
	}
	_ = m
}
