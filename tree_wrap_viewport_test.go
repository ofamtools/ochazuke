package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTreeWrapMixedHeightScrolling(t *testing.T) {
	_, _, tree := wrapTree(t, []TreeNode{
		{ID: "a", Label: "alpha beta gamma"},
		{ID: "b", Label: "short"},
		{ID: "c", Label: "one two three four five"},
		{ID: "d", Label: "last row"},
	})
	draw := func() []string { return bodyLines(ansi.Strip(tree.View(12, 5))) }
	rows := draw()
	if rows[0] != "  alpha beta" || rows[1] != "  gamma" || rows[2] != "  short" || rows[3] != "  one two" || rows[4] != "1–3 / 4" {
		t.Fatalf("initial %q", rows)
	}
	tree.List().ScrollLines(1)
	rows = draw()
	if rows[0] != "  gamma" || rows[1] != "  short" || rows[2] != "  one two" || rows[3] != "  three four" {
		t.Fatalf("line scroll %q", rows)
	}
	tree.List().MovePage(4)
	rows = draw()
	selected, _ := tree.Selected()
	if selected.ID != "c" || rows[0] != "  one two" || rows[1] != "  three four" || rows[2] != "  five" || rows[3] != "  last row" {
		t.Fatalf("page selected %s rows %q", selected.ID, rows)
	}
	tree.List().MovePage(-4)
	rows = draw()
	selected, _ = tree.Selected()
	if selected.ID != "a" || rows[0] != "  alpha beta" {
		t.Fatalf("back page %s %q", selected.ID, rows)
	}
}

func TestTreeWrapTallRowMotionReadsBeforeLeaving(t *testing.T) {
	_, m, tree := wrapTree(t, []TreeNode{{ID: "a", Label: "aaa bbb ccc ddd eee fff ggg hhh iii jjj kkk lll"}, {ID: "b", Label: "last"}})
	draw := func() { tree.View(6, 5) } // four text lines and the range line
	draw()
	for _, want := range []int{2, 4, 6, 8} {
		m.handleKey(MacroKey{Key: "j"})
		draw()
		selected, _ := tree.Selected()
		if selected.ID != "a" || tree.wrap.top != want {
			t.Fatalf("j: selected %s top %d want a/%d", selected.ID, tree.wrap.top, want)
		}
	}
	m.handleKey(MacroKey{Key: "j"})
	draw()
	selected, _ := tree.Selected()
	if selected.ID != "b" {
		t.Fatalf("end of tall row never left: %s", selected.ID)
	}
	m.handleKey(MacroKey{Key: "k"})
	draw()
	if selected, _ = tree.Selected(); selected.ID != "a" || tree.wrap.top < 6 {
		t.Fatalf("k did not enter tall row at its bottom: %s top %d", selected.ID, tree.wrap.top)
	}
}

func TestTreeWrapTallRowPageAndLast(t *testing.T) {
	_, m, tree := wrapTree(t, []TreeNode{{ID: "a", Label: "aaa bbb ccc ddd eee fff ggg hhh iii jjj kkk lll"}, {ID: "b", Label: "last"}})
	tree.View(6, 5)
	for range 4 {
		tree.List().MovePage(4)
		tree.View(6, 5)
	}
	if selected, _ := tree.Selected(); selected.ID != "b" {
		t.Fatalf("paging trapped on %s", selected.ID)
	}
	m.handleKey(MacroKey{Key: "g"})
	m.handleKey(MacroKey{Key: "g"})
	tree.View(6, 5)
	if tree.wrap.top != 0 {
		t.Fatalf("gg top %d", tree.wrap.top)
	}
	// A single tall last row: G must reveal its end even when already selected.
	tree.SetNodes([]TreeNode{{ID: "a", Label: "aaa bbb ccc ddd eee fff ggg hhh iii jjj kkk lll"}})
	tree.Focus()
	tree.View(6, 5)
	m.handleKey(MacroKey{Key: "G"})
	rows := bodyLines(ansi.Strip(tree.View(6, 5)))
	if indexOfLine(rows, "lll") < 0 {
		t.Fatalf("G hid the last wrapped line: %q", rows)
	}
}

func TestTreeWrapEditingTracksTallCursorAndSelection(t *testing.T) {
	_, _, tree := wrapTree(t, []TreeNode{{ID: "a", Label: "aaa bbb ccc ddd eee fff ggg hhh iii jjj"}, {ID: "b", Label: "after"}})
	if err := tree.Edit("a", true); err != nil {
		t.Fatal(err)
	}
	tree.input.Cursor = len(tree.input.Text)
	block := tree.Block(10, 5)
	rows := bodyLines(ansi.Strip(block.Text))
	if block.Cursor == nil || block.Cursor.X < 2 || block.Cursor.X >= 10 || block.Cursor.Y >= 4 || indexOfLine(rows, "iii jjj") < 0 {
		t.Fatalf("cursor %+v rows %q", block.Cursor, rows)
	}
	for _, line := range strings.Split(block.Text, "\n")[:3] {
		if !strings.Contains(line, "\x1b[7m") {
			t.Fatalf("selection lost on continuation %q", line)
		}
	}
	tree.input.Cursor = 0
	block = tree.Block(10, 5)
	if block.Cursor == nil || *block.Cursor != (Pos{X: 2, Y: 0}) {
		t.Fatalf("cursor at start %+v", block.Cursor)
	}
	tree.input.Replace(0, len(tree.input.Text), "changed words that wrap again")
	tree.input.Cursor = len(tree.input.Text)
	block = tree.Block(10, 5)
	if block.Cursor == nil || block.Cursor.Y >= 4 {
		t.Fatalf("typing cursor %+v", block.Cursor)
	}
	if err := tree.Flush(); err != nil {
		t.Fatal(err)
	}
	if selected, _ := tree.Selected(); selected.Label != "changed words that wrap again" {
		t.Fatalf("write %+v", selected)
	}
}

func TestTreeWrapLargeOutlineFilterAndResize(t *testing.T) {
	nodes := make([]TreeNode, 5000)
	for i := range nodes {
		nodes[i] = TreeNode{ID: fmt.Sprintf("r%d", i), Label: fmt.Sprintf("row%04d alpha beta gamma delta", i)}
	}
	_, m, tree := wrapTree(t, nodes)
	tree.View(18, 6)
	m.handleKey(MacroKey{Key: "G"})
	rows := bodyLines(ansi.Strip(tree.View(18, 6)))
	if indexOfLine(rows, "row4999") < 0 || indexOfLine(rows, "gamma delta") < 0 {
		t.Fatalf("large last row %q", rows)
	}
	tree.List().SetFilter("row4321")
	rows = bodyLines(ansi.Strip(tree.View(18, 6)))
	if indexOfLine(rows, "row4321") < 0 || indexOfLine(rows, "gamma delta") < 0 {
		t.Fatalf("filtered %q", rows)
	}
	tree.List().SetFilter("")
	tree.Select("r4321")
	tree.Focus()
	for width := 1; width < 40; width++ {
		block := tree.Block(width, 6)
		for _, line := range strings.Split(block.Text, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflow %q", width, line)
			}
		}
	}
}
