package grid

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ochazuke"
)

func sheet(t *testing.T) (*ochazuke.Runtime, *Grid) {
	t.Helper()
	r := ochazuke.NewRuntime()
	r.Instance("page")
	g, err := New(r, "grid", "page", []string{"a", "b", "c"}, [][]string{{"1", "2", "3"}, {"4", "5", "6"}, {"7", "8", "9"}, {"x", "y", "z"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("page", "Page", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Text("") }, ochazuke.Focus(g.focusTarget())); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	return r, g
}

func TestFocusInstancesFollowTheCursor(t *testing.T) {
	r, g := sheet(t)
	if r.Focus != "r0:c0" {
		t.Fatalf("focus = %s", r.Focus)
	}
	if got := r.InstancePath("r0:c0"); !reflect.DeepEqual(got, []string{"app", "page", "grid", "c0", "r0", "r0:c0"}) {
		t.Fatalf("path = %v", got)
	}
	for id, noun := range map[string]string{"r0:c0": "cell", "r0": "row", "c0": "column", "grid": "grid"} {
		if inst := r.Instances[id]; inst == nil || inst.Noun != noun {
			t.Fatalf("%s noun = %v", id, inst)
		}
	}
	if _, err := r.Execute(ochazuke.Request{Verb: "move", Noun: "grid", Args: ochazuke.Arguments{"motion": "down", "steps": 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(ochazuke.Request{Verb: "move", Noun: "grid", Args: ochazuke.Arguments{"motion": "right"}}); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "r2:c1" || r.Instances["r0:c0"] != nil || r.Instances["r0"] != nil || r.Instances["c0"] != nil {
		t.Fatalf("focus=%s stale instances kept", r.Focus)
	}
	if got := len(r.Children("grid")); got != 1 {
		t.Fatalf("grid children = %d, want the one column", got)
	}
	if _, err := r.Execute(ochazuke.Request{Verb: "move", Noun: "grid", Args: ochazuke.Arguments{"motion": "bottom", "line": 2}}); err != nil {
		t.Fatal(err)
	}
	if g.Cursor() != (Ref{Row: 1, Col: 1}) {
		t.Fatalf("2G cursor = %v", g.Cursor())
	}
	if err := g.Move("sideways", 1, 0); err == nil {
		t.Fatal("unknown motion accepted")
	}
}

func TestSelectionShapesAndMarks(t *testing.T) {
	r, g := sheet(t)
	if err := g.Select(ShapeBlock); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != ShapeBlock {
		t.Fatalf("mode = %s", r.Mode.Name)
	}
	g.Move("down", 1, 0)
	g.Move("right", 1, 0)
	if got := g.Selected(); !reflect.DeepEqual(got, []Ref{{0, 0}, {0, 1}, {1, 0}, {1, 1}}) {
		t.Fatalf("block = %v", got)
	}
	g.Select(ShapeRow)
	if got := g.Selected(); len(got) != 6 || got[5] != (Ref{1, 2}) {
		t.Fatalf("rows = %v", got)
	}
	g.Select(ShapeColumn)
	if got := g.Selected(); len(got) != 8 || got[7] != (Ref{3, 1}) {
		t.Fatalf("columns = %v", got)
	}
	g.Select(ShapeBlock)
	g.Move("down", 1, 0)
	g.ToggleMark()
	if err := g.SwapAnchor(); err != nil {
		t.Fatal(err)
	}
	if g.Cursor() != (Ref{0, 0}) || g.Anchor() != (Ref{2, 1}) {
		t.Fatalf("swap: cursor=%v anchor=%v", g.Cursor(), g.Anchor())
	}
	g.ToggleMark()
	g.ToggleMark()
	if got := g.Marks(); !reflect.DeepEqual(got, []Ref{{2, 1}}) {
		t.Fatalf("marks = %v", got)
	}
	g.Select(ShapeRow)
	g.ToggleMark()
	if got := g.Marks(); len(got) != 4 {
		t.Fatalf("row mark = %v", got)
	}
	g.Cancel()
	if g.Shape() != "" || g.Selected() != nil || len(g.Marks()) != 0 {
		t.Fatal("cancel kept selection state")
	}
	g.SelectAll()
	if g.Shape() != ShapeBlock || len(g.Selected()) != 12 {
		t.Fatalf("all: shape=%s n=%d", g.Shape(), len(g.Selected()))
	}
}

func TestFilterKeepsIdsAndClampsCursor(t *testing.T) {
	r, g := sheet(t)
	g.Move("bottom", 1, 0)
	g.SetFilter(func(row []string) bool { return row[0] != "x" })
	if g.VisibleCount() != 3 || r.Focus != "r2:c0" {
		t.Fatalf("filtered: visible=%d focus=%s", g.VisibleCount(), r.Focus)
	}
	if err := g.Goto(Ref{Row: 3, Col: 0}); err == nil {
		t.Fatal("goto a hidden row succeeded")
	}
	g.Select(ShapeColumn)
	if got := g.Selected(); len(got) != 3 {
		t.Fatalf("column selection spans hidden rows: %v", got)
	}
	g.Cancel()
	g.SetFilter(nil)
	rows := g.Rows()[:2]
	g.SetData(g.Header(), rows)
	if r.Focus != "r1:c0" || g.Cursor() != (Ref{1, 0}) {
		t.Fatalf("shrunk: focus=%s cursor=%v", r.Focus, g.Cursor())
	}
	g.SetData(nil, nil)
	if r.Focus != "grid" || len(r.Children("grid")) != 0 {
		t.Fatalf("empty: focus=%s children=%d", r.Focus, len(r.Children("grid")))
	}
}

func TestViewScrollsBothAxes(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Instance("page")
	header := make([]string, 8)
	rows := make([][]string, 30)
	for c := range header {
		header[c] = strings.Repeat("h", 6) + string(rune('a'+c))
	}
	for i := range rows {
		rows[i] = make([]string, 8)
		for c := range rows[i] {
			rows[i][c] = strings.Repeat("v", 6) + string(rune('a'+c))
		}
	}
	g, err := New(r, "grid", "page", header, rows)
	if err != nil {
		t.Fatal(err)
	}
	view := ansi.Strip(g.View(30, 6))
	lines := strings.Split(view, "\n")
	if len(lines) != 6 || !strings.Contains(lines[0], "hhhhhha") || strings.Contains(lines[0], "hhhhhhh") || !strings.HasPrefix(lines[1], "  1 vvvvvva") {
		t.Fatalf("view:\n%s", view)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) != 30 {
			t.Fatalf("line width %d: %q", ansi.StringWidth(line), line)
		}
	}
	g.Move("bottom", 1, 0)
	g.Move("row-end", 1, 0)
	view = ansi.Strip(g.View(30, 6))
	if !strings.Contains(view, "30 ") || !strings.Contains(view, "hhhhhhh") || strings.Contains(view, "hhhhhha") {
		t.Fatalf("scrolled view:\n%s", view)
	}
	if ref, kind, ok := ParseID("r3:c2"); !ok || kind != "cell" || ref != (Ref{3, 2}) {
		t.Fatalf("ParseID r3:c2 = %v %s %v", ref, kind, ok)
	}
	if ref, kind, ok := ParseID("c7"); !ok || kind != "column" || ref.Col != 7 {
		t.Fatalf("ParseID c7 = %v %s %v", ref, kind, ok)
	}
	if _, _, ok := ParseID("row"); ok {
		t.Fatal("ParseID accepted junk")
	}
}
