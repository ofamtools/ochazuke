package ochazuke

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// edgeTitles are edgePage's table columns, each eight cells wide.
var edgeTitles = []string{"name", "alpha", "bravo", "charlie", "delta", "echo"}

// edgePage is a page of a plain list a, a table t of six fixed-width
// columns (58 cells with the gaps) and a plain list b, drawn at width
// cells, in drawing order a, t, b; the focus on t's first row.
func edgePage(t *testing.T, width int, stateDir string, opts ...InstanceOption) (*Runtime, *Model, *List) {
	t.Helper()
	r := NewRuntime()
	r.Config().StateDir = stateDir
	m := NewModel(r)
	r.Define("row")
	var cols []Column
	for _, title := range edgeTitles {
		cols = append(cols, Column{Title: title, Width: 8})
	}
	rows := func(prefix string, n int, cells bool) []ListItem {
		var out []ListItem
		for i := 1; i <= n; i++ {
			item := ListItem{ID: fmt.Sprintf("%s%d", prefix, i), Label: fmt.Sprintf("%s%d", prefix, i), Noun: "row"}
			if cells {
				item.Cells = []string{item.ID}
				for _, title := range edgeTitles[1:] {
					item.Cells = append(item.Cells, fmt.Sprintf("%c-%d", title[0], i))
				}
			}
			out = append(out, item)
		}
		return out
	}
	var a, tb, b *List
	if _, err := r.AddPage("p", "P", func(_ *Runtime, w, _ int) Block {
		return Text(a.View(w, 2) + "\n" + tb.View(w, 4) + "\n" + b.View(w, 2))
	}); err != nil {
		t.Fatal(err)
	}
	var err error
	if a, err = r.AddList("a", rows("a", 2, false), Parent("p")); err != nil {
		t.Fatal(err)
	}
	if tb, err = r.AddList("t", rows("t", 3, true), append([]InstanceOption{Parent("p"), Columns(cols...)}, opts...)...); err != nil {
		t.Fatal(err)
	}
	if b, err = r.AddList("b", rows("b", 2, false), Parent("p")); err != nil {
		t.Fatal(err)
	}
	_ = r.OpenPage("p")
	_ = tb.Focus()
	m.Width, m.Height = width, 20
	_ = m.View()
	return r, m, tb
}

// tableHeader is t's header line as words.
func tableHeader(m *Model) string {
	for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.Contains(line, "name") || strings.Contains(line, "alpha") {
			return strings.Join(strings.Fields(line), " ")
		}
	}
	return ""
}

// R5.11: on a row-select table l scrolls the columns one at a time until
// the last shows, then moves the focus to the next stop; h scrolls back to
// the start, then moves it to the previous stop. A count scrolls that many
// and stops at the edge, as a count of j stops at the last row.
func TestRowTableHLScrollThenMoveTheFocus(t *testing.T) {
	r, m, l := edgePage(t, 40, "")
	for _, step := range []struct {
		keys, focus, header string
		offset              int
	}{
		{"", "t1", "name alpha bravo 3›", 0},
		{"l", "t1", "name ‹1 bravo charlie 2›", 2},
		{"l", "t1", "name ‹2 charlie delta 1›", 3},
		{"l", "t1", "name ‹3 delta echo", 4},
		{"l", "b1", "", 4},
		{"{ h", "t1", "name ‹2 charlie delta 1›", 3},
		{"9 h", "t1", "name alpha bravo 3›", 0}, // the count stops at the edge
		{"h", "a1", "", 0},
	} {
		motionKeys(t, r, m, step.keys)
		if r.Focus != step.focus || l.ColumnOffset() != step.offset {
			t.Fatalf("%q: focus %s offset %d, want %s %d", step.keys, r.Focus, l.ColumnOffset(), step.focus, step.offset)
		}
		if step.header != "" {
			if got := tableHeader(m); got != step.header {
				t.Fatalf("%q: header %q, want %q", step.keys, got, step.header)
			}
		}
	}
	// The arrows do the same.
	motionKeys(t, r, m, "} right right right right")
	if r.Focus != "b1" {
		t.Fatalf("right at the edge: focus %s, want b1", r.Focus)
	}
}

// R5.11: a cell-mode table's h on the first column and l on the last move
// the focus; in between they move the column, in drawing order.
func TestCellTableEdgeMovesTheFocus(t *testing.T) {
	r, m, l := edgePage(t, 40, "", CellSelect())
	motionKeys(t, r, m, "l")
	if l.Column() != 1 || r.Focus != "t1" {
		t.Fatalf("l: column %d focus %s", l.Column(), r.Focus)
	}
	motionKeys(t, r, m, "h h")
	if r.Focus != "a1" {
		t.Fatalf("h on the first column: focus %s, want a1", r.Focus)
	}
	motionKeys(t, r, m, "} $ l")
	if r.Focus != "b1" {
		t.Fatalf("l on the last column: focus %s, want b1", r.Focus)
	}
	// 9l short of the edge stops on the last column: a count never spills.
	motionKeys(t, r, m, "{ 0 9 l")
	if r.Focus != "t1" || l.Column() != 5 {
		t.Fatalf("9 l: focus %s column %d, want t1 on the last", r.Focus, l.Column())
	}
}

// R5.11 and R3.6: a NoSpill table and a table in a modal clamp at their
// edges; at the page's last stop l clamps too (motions never wrap).
func TestTableEdgesClampWithoutAStop(t *testing.T) {
	r, m, l := edgePage(t, 40, "", CellSelect(), NoSpill())
	motionKeys(t, r, m, "h $ l l")
	if r.Focus != "t1" || l.Column() != 5 {
		t.Fatalf("NoSpill: focus %s column %d", r.Focus, l.Column())
	}
	for _, cell := range []bool{true, false} {
		r := NewRuntime()
		m := NewModel(r)
		r.Define("row")
		body, err := r.OpenModal("ask", Payload{})
		if err != nil {
			t.Fatal(err)
		}
		var cols []Column
		for _, title := range edgeTitles {
			cols = append(cols, Column{Title: title, Width: 8})
		}
		opts := []InstanceOption{Parent(body.ID), Columns(cols...)}
		if cell {
			opts = append(opts, CellSelect())
		}
		ml, err := r.AddList("m", []ListItem{{ID: "m1", Label: "m1", Noun: "row", Cells: []string{"m1", "a", "b", "c", "d", "e"}}}, opts...)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.SetFocus("m1"); err != nil {
			t.Fatal(err)
		}
		_ = ml.View(40, 4)
		for _, key := range strings.Fields("h h l l l l l l l l l") {
			if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
				t.Fatalf("cell %v %s: %v", cell, key, err)
			}
			_ = ml.View(40, 4)
			if r.Focus != "m1" || r.TopModal() != body.ID {
				t.Fatalf("cell %v %s: focus %s modal %s", cell, key, r.Focus, r.TopModal())
			}
		}
		if cell && ml.Column() != 5 || !cell && ml.ColumnOffset() != ml.maxColumnOffset(40) {
			t.Fatalf("cell %v: column %d offset %d", cell, ml.Column(), ml.ColumnOffset())
		}
		_ = m
	}
	// The last stop of the page: l at the right edge stays.
	r2, m2, l2 := edgePage(t, 40, "")
	_ = l2
	motionKeys(t, r2, m2, "$")
	if err := r2.SetFocus("b1"); err != nil {
		t.Fatal(err)
	}
	_ = r2.SetFocus("t1")
	_ = m2.View()
	r2.Instances["b"].Unfocusable = true
	motionKeys(t, r2, m2, "l")
	if r2.Focus != "t1" {
		t.Fatalf("l at the last stop: focus %s", r2.Focus)
	}
}

// R5.11: a row noun's own l answers before the table's scroll and spill.
func TestRowNounKeyWinsOverTableScroll(t *testing.T) {
	r, m, l := edgePage(t, 40, "")
	opened := ""
	r.Define("row").Verb("open", func(c Ctx) Result { opened = c.Instance.ID; return Done(nil) }, "Open").Mode("normal").Bind("l", Do("row.open"))
	motionKeys(t, r, m, "l")
	if opened != "t1" || l.ColumnOffset() != 0 || r.Focus != "t1" {
		t.Fatalf("l: opened %q offset %d focus %s", opened, l.ColumnOffset(), r.Focus)
	}
	// h is the row noun's to leave: it still scrolls.
	motionKeys(t, r, m, "$ h")
	if l.ColumnOffset() != 3 {
		t.Fatalf("h: offset %d, want 3", l.ColumnOffset())
	}
}

// R5.12: the markers count the scrolling columns hidden on each side,
// never a pinned one, and a table that fits draws none.
func TestEdgeMarkersCountScrollingColumns(t *testing.T) {
	_, m, l := edgePage(t, 80, "")
	if got := tableHeader(m); strings.ContainsAny(got, "‹›") || got != strings.Join(edgeTitles, " ") {
		t.Fatalf("fits: %q", got)
	}
	m.Width = 40
	_ = m.View()
	if got := tableHeader(m); got != "name alpha bravo 3›" {
		t.Fatalf("narrow: %q", got)
	}
	l.SetPinnedColumns([]int{0, 4})
	_ = m.View()
	if got := tableHeader(m); got != "name delta alpha 3›" {
		t.Fatalf("pinned delta: %q (the right count covers alpha's neighbours, not delta)", got)
	}
}

// R5.13: pinned columns draw first in their order, persist per table id
// (a row-select table's too), and reset-columns puts the default back.
func TestPinsDrawFirstPersistAndReset(t *testing.T) {
	dir := t.TempDir()
	r, m, l := edgePage(t, 40, dir)
	if got := l.PinnedColumns(); !slices.Equal(got, []int{0}) {
		t.Fatalf("default pins %v", got)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.list", Verb: "toggle-pin-column", Args: Arguments{"column": "Charlie"}}); err != nil {
		t.Fatal(err)
	}
	_ = m.View()
	if got := tableHeader(m); got != "name charlie alpha 3›" {
		t.Fatalf("pinned charlie: %q", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, TablesFile))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]TableState
	if err := json.Unmarshal(data, &saved); err != nil || saved["t"].Pins == nil || !slices.Equal(*saved["t"].Pins, []int{0, 3}) {
		t.Fatalf("tables.json %s (%v)", data, err)
	}
	r2, m2, l2 := edgePage(t, 40, dir)
	if got := l2.PinnedColumns(); !slices.Equal(got, []int{0, 3}) {
		t.Fatalf("restored pins %v", got)
	}
	for _, step := range []struct {
		verb, column string
		want         []int
	}{
		{"unpin-column", "1", []int{3}},
		{"pin-column", "echo", []int{3, 5}},
		{"unpin-columns", "", nil},
		{"reset-columns", "", []int{0}},
	} {
		args := Arguments{}
		if step.column != "" {
			args["column"] = step.column
		}
		if _, err := r2.Execute(Request{Noun: "ochazuke.list", Verb: step.verb, Args: args}); err != nil {
			t.Fatalf("%s: %v", step.verb, err)
		}
		if got := l2.PinnedColumns(); !slices.Equal(got, step.want) {
			t.Fatalf("%s %s: pins %v, want %v", step.verb, step.column, got, step.want)
		}
	}
	_ = m2.View()
	data, _ = os.ReadFile(filepath.Join(dir, TablesFile))
	if strings.Contains(string(data), `"t"`) {
		t.Fatalf("the default pins are remembered as a choice: %s", data)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.list", Verb: "pin-column", Args: Arguments{"column": "nope"}}); err == nil {
		t.Fatal("an unknown column pinned")
	}
	// z p toggles the current column of a cell-mode table.
	r3, m3, l3 := edgePage(t, 40, "", CellSelect())
	motionKeys(t, r3, m3, "l l z p")
	if got := l3.PinnedColumns(); !slices.Equal(got, []int{0, 2}) {
		t.Fatalf("z p: pins %v", got)
	}
	motionKeys(t, r3, m3, "z =")
	if got := l3.PinnedColumns(); !slices.Equal(got, []int{0}) {
		t.Fatalf("z =: pins %v", got)
	}
	// Declared pins are the default.
	r4 := NewRuntime()
	l4, err := r4.AddList("d", nil, Columns(Column{Title: "a"}, Column{Title: "b", Pinned: true}, Column{Title: "c", Pinned: true}))
	if err != nil {
		t.Fatal(err)
	}
	if got := l4.PinnedColumns(); !slices.Equal(got, []int{1, 2}) || !slices.Equal(l4.order(), []int{1, 2, 0}) {
		t.Fatalf("declared pins %v order %v", got, l4.order())
	}
}

// R5.13: pins that leave no room for a scrolling column are dropped from
// the right in the view only; the saved pins stay.
func TestPinsDroppedInTheViewOnly(t *testing.T) {
	_, m, l := edgePage(t, 30, "")
	l.SetPinnedColumns([]int{0, 3})
	_ = m.View()
	if got := tableHeader(m); got != "name alpha 3›" {
		t.Fatalf("narrow: %q", got)
	}
	if got := l.PinnedColumns(); !slices.Equal(got, []int{0, 3}) {
		t.Fatalf("pins changed: %v", got)
	}
	m.Width = 80
	_ = m.View()
	if got := tableHeader(m); got != "name charlie alpha bravo delta echo" {
		t.Fatalf("wide: %q", got)
	}
}

// A row whose own guard is false does not shadow an outer row's hint on
// the same key: l on a plain list leaves the page's l its hint.
func TestGuardedOffRowDoesNotShadowAnOuterHint(t *testing.T) {
	r, m, _, _ := motionPage(t)
	r.Define("page-noun").Verb("open", func(Ctx) Result { return Done(nil) }, "Open")
	if err := r.Bind(Binding{Instance: "p", Mode: "normal", Keys: "l", Request: Request{Noun: "page-noun", Verb: "open"}}); err != nil {
		t.Fatal(err)
	}
	_ = m
	if hints := BindingHints(r, r.Focus); !strings.Contains(hints, "l page-noun.open") {
		t.Fatalf("hints %q", hints)
	}
}

// R11.1 over R5.11: a tree with columns keeps its own h/l. l on an open
// node steps onto its first child and on a leaf stays put although the
// tree fits and a next stop exists; with hidden columns l expands rather
// than scrolls, and z l still scrolls.
func TestTreeHLWinOverTableScroll(t *testing.T) {
	for _, width := range []int{80, 30} {
		r := NewRuntime()
		m := NewModel(r)
		r.Define("row")
		cols := []Column{{Title: "name", Width: 10}, {Title: "alpha", Width: 8}, {Title: "bravo", Width: 8}}
		var tr *Tree
		var b *List
		if _, err := r.AddPage("p", "P", func(_ *Runtime, w, _ int) Block {
			return Text(tr.View(w, 4) + "\n" + b.View(w, 2))
		}); err != nil {
			t.Fatal(err)
		}
		var err error
		if tr, err = r.AddTree("tr", []TreeNode{
			{ID: "s1", Label: "main", Cells: []string{"main", "a-1", "b-1"}},
			{ID: "p1", Parent: "s1", Label: "editor", Cells: []string{"editor", "a-2", "b-2"}},
		}, Parent("p"), Columns(cols...)); err != nil {
			t.Fatal(err)
		}
		if b, err = r.AddList("b", []ListItem{{ID: "b1", Label: "b1", Noun: "row"}}, Parent("p")); err != nil {
			t.Fatal(err)
		}
		_ = r.OpenPage("p")
		_ = tr.Focus()
		m.Width, m.Height = width, 20
		_ = m.View()
		l := tr.List()
		motionKeys(t, r, m, "l")
		if r.Focus != "p1" || l.ColumnOffset() != 0 {
			t.Fatalf("width %d: l on the open node: focus %s offset %d, want p1 0", width, r.Focus, l.ColumnOffset())
		}
		motionKeys(t, r, m, "right")
		if r.Focus != "p1" {
			t.Fatalf("width %d: right on a leaf: focus %s, want p1", width, r.Focus)
		}
		motionKeys(t, r, m, "h")
		if r.Focus != "s1" {
			t.Fatalf("width %d: h on a leaf: focus %s, want s1", width, r.Focus)
		}
		if width < 80 {
			motionKeys(t, r, m, "z l")
			if l.ColumnOffset() == 0 {
				t.Fatalf("z l did not scroll the tree's columns")
			}
		}
	}
}
