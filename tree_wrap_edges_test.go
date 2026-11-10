package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTreeWrapSuffixBadgeStaysOnFirstLine(t *testing.T) {
	_, _, tree := wrapTree(t, []TreeNode{{ID: "a", Label: "testing wrap start here", Cells: []string{"- testing wrap start here !"}}}, Columns(Column{Width: 18}))
	for _, editing := range []bool{false, true} {
		if editing {
			if err := tree.Edit("a", false); err != nil {
				t.Fatal(err)
			}
		}
		rows := bodyLines(ansi.Strip(tree.Block(18, 5).Text))
		if rows[0] != "  - testing wrap !" || rows[1] != "    start here" || strings.Contains(strings.Join(rows[1:], "\n"), "!") {
			t.Fatalf("editing %v badge %q", editing, rows)
		}
	}
}

func TestTreeWrapHeightAndLiveOptOut(t *testing.T) {
	r, _, tree := wrapTree(t, []TreeNode{{ID: "a", Label: "testing wrap start here"}, {ID: "b", Label: "after", SpaceAbove: true}})
	if got := tree.HeightAt(14); got != 4 {
		t.Fatalf("wrapped height %d want 4", got)
	}
	if err := tree.Edit("a", false); err != nil {
		t.Fatal(err)
	}
	block := tree.Block(14, 5)
	if block.Cursor == nil || block.Cursor.Y != 1 {
		t.Fatalf("wrapped cursor %+v", block.Cursor)
	}
	if err := r.Settings().Set("tree.wrap", false); err != nil {
		t.Fatal(err)
	}
	block = tree.Block(14, 5)
	if block.Cursor == nil || block.Cursor.Y != 0 || block.Cursor.X >= 14 || tree.HeightAt(14) != 3 {
		t.Fatalf("opt out cursor %+v height %d", block.Cursor, tree.HeightAt(14))
	}
	if err := r.Settings().Set("tree.wrap", true); err != nil {
		t.Fatal(err)
	}
	block = tree.Block(14, 5)
	if block.Cursor == nil || block.Cursor.Y != 1 {
		t.Fatalf("re-enabled cursor %+v", block.Cursor)
	}
	if b := tree.Block(0, 0); b.Cursor != nil {
		t.Fatalf("zero-sized cursor %+v", b.Cursor)
	}
}

func TestTreeWrapFilterAndBreadcrumbOffsetCursor(t *testing.T) {
	_, _, tree := wrapTree(t, []TreeNode{{ID: "root", Label: "Root"}, {ID: "a", Parent: "root", Label: "testing wrap start here"}})
	if err := tree.Zoom("root"); err != nil {
		t.Fatal(err)
	}
	tree.List().SetFilter("testing")
	if err := tree.Edit("a", false); err != nil {
		t.Fatal(err)
	}
	block := tree.Block(14, 7)
	rows := bodyLines(ansi.Strip(block.Text))
	if rows[0] != "Root" || !strings.Contains(rows[1], "/testing") || rows[2] != "  testing wrap" || rows[3] != "  start here" || block.Cursor == nil || *block.Cursor != (Pos{X: 12, Y: 3}) {
		t.Fatalf("cursor %+v rows %q", block.Cursor, rows)
	}
}

func TestTreeWrapExplicitTrailingColumnLines(t *testing.T) {
	_, _, tree := wrapTree(t, []TreeNode{{ID: "a", Label: "one two three", Cells: []string{"one two three", "a\nb\nc\nd"}}}, Columns(Column{Width: 9}, Column{Width: 2, Lines: true}))
	rows := bodyLines(ansi.Strip(tree.View(13, 6)))
	if rows[0] != "  one two  a" || rows[1] != "  three    b" || rows[2] != "           c" || rows[3] != "           d" {
		t.Fatalf("explicit lines %q", rows)
	}
}
