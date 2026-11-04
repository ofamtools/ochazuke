package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// spaceTree is a tree of numbered top-level rows r0..r(n-1), each with
// SpaceAbove from the second (every row asks, so the tests show the first
// row refusing it), drawn height lines high; columns draws it as a table.
func spaceTree(t *testing.T, n, height int, columns bool, opts ...InstanceOption) (*Runtime, *Model, *Tree) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 40, height+4
	var tree *Tree
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return tree.Block(w, h) }); err != nil {
		t.Fatal(err)
	}
	nodes := make([]TreeNode, n)
	for i := range nodes {
		nodes[i] = TreeNode{ID: fmt.Sprintf("r%d", i), Label: fmt.Sprintf("row%d", i), SpaceAbove: true}
		if columns {
			nodes[i].Cells = []string{nodes[i].Label}
		}
	}
	if columns {
		opts = append(opts, Columns(Column{Title: "name"}))
	}
	var err error
	if tree, err = r.AddTree("t", nodes, append(opts, Parent("main"))...); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	if err := tree.Focus(); err != nil {
		t.Fatal(err)
	}
	return r, m, tree
}

// lines is a frame's body split at newlines, each right-trimmed.
func bodyLines(text string) []string {
	out := strings.Split(text, "\n")
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	return out
}

func indexOfLine(lines []string, want string) int {
	for i, l := range lines {
		if strings.Contains(l, want) {
			return i
		}
	}
	return -1
}

// Every row but the first has a blank line above it, in a plain tree and
// in a table; the first row has none.
func TestTreeSpaceAboveDrawsBlankLines(t *testing.T) {
	t.Parallel()
	for _, columns := range []bool{false, true} {
		_, _, tree := spaceTree(t, 3, 12, columns)
		lines := bodyLines(tree.View(30, 12))
		first := indexOfLine(lines, "row0")
		if first != 0 && !(columns && first == 1) {
			t.Fatalf("columns %v: row0 at line %d, want the first line (or under the header):\n%s", columns, first, strings.Join(lines, "\n"))
		}
		for _, name := range []string{"row1", "row2"} {
			at := indexOfLine(lines, name)
			if at < 2 || strings.TrimSpace(lines[at-1]) != "" {
				t.Fatalf("columns %v: %s at line %d has no blank line above:\n%s", columns, name, at, strings.Join(lines, "\n"))
			}
		}
	}
}

// The blank line is not a row: j and k step row by row, the selection and
// the row count never see it.
func TestTreeSpaceAboveIsNotARow(t *testing.T) {
	t.Parallel()
	r, m, tree := spaceTree(t, 4, 12, false)
	pressKeys(m, "j", "j")
	if r.Focus != "r2" {
		t.Fatalf("focus %q after j j, want r2", r.Focus)
	}
	pressKeys(m, "k")
	if r.Focus != "r1" {
		t.Fatalf("focus %q after k, want r1", r.Focus)
	}
	if tree.List().Len() != 4 {
		t.Fatalf("list holds %d rows, want 4", tree.List().Len())
	}
	pressKeys(m, "V", "j", "j")
	if got := len(tree.List().markedShown()); got != 3 {
		t.Fatalf("visual selection holds %d rows, want 3", got)
	}
}

// A filter that makes a spaced row the first shown draws no blank line
// above it.
func TestTreeSpaceAboveNeverAboveFirstShownRow(t *testing.T) {
	t.Parallel()
	_, _, tree := spaceTree(t, 4, 12, false)
	tree.List().SetFilter("row2")
	lines := bodyLines(tree.View(30, 12))
	if at := indexOfLine(lines, "row2"); at > 1 { // line 0 may hold the filter strip
		t.Fatalf("row2 at line %d, blank line above the first shown row:\n%s", at, strings.Join(lines, "\n"))
	}
	if strings.Count(strings.Join(lines, "\n"), "\n\n") > 0 && indexOfLine(lines, "row2") > 0 && strings.TrimSpace(lines[indexOfLine(lines, "row2")-1]) == "" {
		t.Fatal("a blank line above the only shown row")
	}
}

// tree.space-above off draws the rows without gaps.
func TestTreeSpaceAboveSettingOff(t *testing.T) {
	t.Parallel()
	r, _, tree := spaceTree(t, 3, 12, false)
	if err := r.Settings().Set(TreeSpaceAboveSetting, false); err != nil {
		t.Fatal(err)
	}
	lines := bodyLines(tree.View(30, 12))
	if indexOfLine(lines, "row1") != 1 || indexOfLine(lines, "row2") != 2 {
		t.Fatalf("rows have gaps with the setting off:\n%s", strings.Join(lines, "\n"))
	}
}

// At the bottom edge the last row stays whole on screen however the window
// got there: G, and a scroll with ctrl+e.
func TestTreeSpaceAboveScrollsToTheBottom(t *testing.T) {
	t.Parallel()
	for _, columns := range []bool{false, true} {
		r, m, tree := spaceTree(t, 12, 8, columns)
		_ = tree.View(30, 8)
		pressKeys(m, "G")
		lines := bodyLines(tree.View(30, 8))
		if r.Focus != "r11" || indexOfLine(lines, "row11") < 0 {
			t.Fatalf("columns %v: after G focus %q:\n%s", columns, r.Focus, strings.Join(lines, "\n"))
		}
		if len(lines) > 8 {
			t.Fatalf("columns %v: %d lines in 8:\n%s", columns, len(lines), strings.Join(lines, "\n"))
		}
		// Back at the top, scroll the window down line by line to the end.
		pressKeys(m, "g", "g")
		_ = tree.View(30, 8)
		for range 20 {
			tree.List().ScrollLines(1)
			_ = tree.View(30, 8)
		}
		lines = bodyLines(tree.View(30, 8))
		if indexOfLine(lines, "row11") < 0 {
			t.Fatalf("columns %v: the last row is not reachable by scrolling:\n%s", columns, strings.Join(lines, "\n"))
		}
		if sel, _ := tree.List().Selected(); indexOfLine(lines, sel.Label) < 0 {
			t.Fatalf("columns %v: the selected row %q is off screen:\n%s", columns, sel.Label, strings.Join(lines, "\n"))
		}
	}
}

// The line the page keeps on screen is the selected row's own, past the
// blank lines above it.
func TestTreeSpaceAboveOpenLineSitsOnItsRow(t *testing.T) {
	t.Parallel()
	for _, columns := range []bool{false, true} {
		_, m, tree := spaceTree(t, 5, 14, columns)
		tree.OnChange = func(TreeChange) error { return nil }
		pressKeys(m, "j", "j", "i")
		block := tree.Block(30, 14)
		lines := bodyLines(ansi.Strip(block.Text))
		at := indexOfLine(lines, "row2")
		if at < 0 || block.Cursor == nil || block.Cursor.Y != at {
			t.Fatalf("columns %v: cursor %+v, row2 on line %d:\n%s", columns, block.Cursor, at, strings.Join(lines, "\n"))
		}
	}
}
