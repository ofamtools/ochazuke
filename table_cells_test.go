package ochazuke

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	if l, err = r.AddList("t", items, Parent("p"), Columns(cols...), CellSelect()); err != nil {
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

// A plain list and a table without CellSelect (row-select, the default;
// RowSelect is a deprecated no-op) keep h, l and the cell keys free for
// the levels outside the list; a row table still answers Y and V.
func TestCellKeysAnswerOnlyOnACellModeTable(t *testing.T) {
	r, _, _, _ := motionPage(t)
	for _, key := range []string{"h", "l", "|", "y", "Y", "V"} {
		if ok, _ := r.Dispatch(MacroKey{Key: key}); ok {
			t.Fatalf("%s answered on a plain list", key)
		}
	}
	for name, opts := range map[string][]InstanceOption{"default": nil, "RowSelect": {RowSelect()}} {
		r2, m2, l2 := columnTable(t, 36, opts...)
		for _, key := range []string{"h", "l", "|", "y", "home", "end", "g c", "z >", "z <", "z ="} {
			motionKeys(t, r2, m2, key)
			if r2.Mode.Name != "normal" || l2.ColumnOffset() != 0 || len(l2.ColumnWidths()) != 0 {
				t.Fatalf("%s: %s answered on a row-select table (mode %s offset %d widths %v)", name, key, r2.Mode.Name, l2.ColumnOffset(), l2.ColumnWidths())
			}
		}
		if _, ok := l2.state()["column"]; ok || l2.cellMode() {
			t.Fatalf("%s: a row-select table has a current column", name)
		}
		motionKeys(t, r2, m2, "$")
		if l2.ColumnOffset() != 3 {
			t.Fatalf("%s: $ offset %d, want the last view (3)", name, l2.ColumnOffset())
		}
		for _, key := range []string{"Y", "V"} {
			if ok, err := r2.Dispatch(MacroKey{Key: key}); !ok || err != nil {
				t.Fatalf("%s: %s did not answer on a row table (%v, %v)", name, key, ok, err)
			}
		}
		if !l2.Visual() {
			t.Fatalf("%s: V did not start a visual selection on a row table", name)
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

func TestColumnWidthsResizeResetAndPersist(t *testing.T) {
	dir := t.TempDir()
	r, m, l := cellTable(t, 80, dir)
	motionKeys(t, r, m, "l")
	natural := l.drawnWidth(1)
	motionKeys(t, r, m, "z >")
	if got := l.ColumnWidths()[1]; got != natural+ColumnWidthStep {
		t.Fatalf("z >: width %d, want %d", got, natural+ColumnWidthStep)
	}
	motionKeys(t, r, m, "3 z >")
	if got := l.ColumnWidths()[1]; got != natural+4*ColumnWidthStep {
		t.Fatalf("3 z >: width %d", got)
	}
	if got := l.drawnWidth(1); got != natural+4*ColumnWidthStep {
		t.Fatalf("drawn at %d", got)
	}
	motionKeys(t, r, m, "9 z <")
	if got := l.ColumnWidths()[1]; got != minColumnWidth {
		t.Fatalf("9 z <: width %d, want the least %d", got, minColumnWidth)
	}
	motionKeys(t, r, m, "z > l l")
	data, err := os.ReadFile(filepath.Join(dir, TablesFile))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]TableState
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if s := saved["t"]; s.Column != 3 || s.Widths[1] != minColumnWidth+ColumnWidthStep {
		t.Fatalf("tables.json: %s", data)
	}
	// A new runtime over the same state directory comes back to them.
	_, _, l2 := cellTable(t, 80, dir)
	if l2.Column() != 3 || l2.ColumnWidths()[1] != minColumnWidth+ColumnWidthStep {
		t.Fatalf("restored column %d widths %v", l2.Column(), l2.ColumnWidths())
	}
	motionKeys(t, r, m, "z =")
	if len(l.ColumnWidths()) != 0 {
		t.Fatalf("z =: %v", l.ColumnWidths())
	}
}

// visualTable is cellTable whose rows answer x (row.zap, an application
// verb) and c (row.check, with a confirm); calls collects the rows each ran on.
func visualTable(t *testing.T) (*Runtime, *Model, *List, *[]string) {
	t.Helper()
	r, m, l := cellTable(t, 80, "")
	var calls []string
	r.Define("row").
		Verb("zap", func(c Ctx) Result { calls = append(calls, "zap "+c.Instance.ID); return Done(nil) }, "zap a row").
		Verb("check", func(c Ctx) Result { calls = append(calls, "check "+c.Instance.ID); return Done(nil) }, "check a row").Confirm("Check {{label}}?").
		Mode("normal").Bind("x", Do("row.zap")).Bind("c", Do("row.check"))
	return r, m, l, &calls
}

func TestVisualRowsAreMarksAndAVerbRunsForEach(t *testing.T) {
	r, m, l, calls := visualTable(t)
	motionKeys(t, r, m, "j space j V j j")
	if !l.Visual() || strings.Join(ids(l.Marked()), " ") != "r2 r3 r4 r5" {
		t.Fatalf("V j j: visual %v marks %v", l.Visual(), ids(l.Marked()))
	}
	motionKeys(t, r, m, "k")
	if strings.Join(ids(l.Marked()), " ") != "r2 r3 r4" {
		t.Fatalf("k shrinks the range: %v", ids(l.Marked()))
	}
	motionKeys(t, r, m, "x")
	if strings.Join(*calls, ", ") != "zap r2, zap r3, zap r4" || l.Visual() {
		t.Fatalf("x: %v visual %v", *calls, l.Visual())
	}
	if strings.Join(ids(l.Marked()), " ") != "r2" {
		t.Fatalf("after the verb the marks V found come back: %v", ids(l.Marked()))
	}
	// esc ends a selection without running anything.
	motionKeys(t, r, m, "V k esc")
	if l.Visual() || strings.Join(ids(l.Marked()), " ") != "r2" || len(*calls) != 3 {
		t.Fatalf("esc: visual %v marks %v calls %v", l.Visual(), ids(l.Marked()), *calls)
	}
	// j past the last row clamps instead of spilling out of the selection.
	motionKeys(t, r, m, "G V j")
	if r.Focus != "r30" || !l.Visual() {
		t.Fatalf("j at the end: focus %s visual %v", r.Focus, l.Visual())
	}
	motionKeys(t, r, m, "V")
	if l.Visual() {
		t.Fatal("V again did not end the selection")
	}
}

func TestVisualVerbWithAConfirmAsksOnce(t *testing.T) {
	r, m, l, calls := visualTable(t)
	motionKeys(t, r, m, "V j j c")
	if r.TopModal() == "" || len(*calls) != 0 {
		t.Fatalf("c: modal %q calls %v", r.TopModal(), *calls)
	}
	motionKeys(t, r, m, "y")
	if strings.Join(*calls, ", ") != "check r1, check r2, check r3" || r.TopModal() != "" || l.Visual() {
		t.Fatalf("y: calls %v modal %q visual %v", *calls, r.TopModal(), l.Visual())
	}
}
