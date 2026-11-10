package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func wrapTree(t *testing.T, nodes []TreeNode, opts ...InstanceOption) (*Runtime, *Model, *Tree) {
	t.Helper()
	r, m, tree, _ := treeFixture(t)
	tree.List().SetColumns()
	for _, opt := range opts {
		spec := instanceSpec{}
		opt(&spec)
		if len(spec.columns) > 0 {
			tree.List().SetColumns(spec.columns...)
		}
	}
	if err := tree.SetNodes(nodes); err != nil {
		t.Fatal(err)
	}
	if err := tree.Focus(); err != nil {
		t.Fatal(err)
	}
	return r, m, tree
}

func TestTreeWrapHangingPrefix(t *testing.T) {
	for _, tc := range []struct{ name, prefix string }{
		{"bullet", "- "}, {"unchecked", "- [ ] "}, {"checked", "- [x] "}, {"number", "12. "}, {"wide glyph", "界 "},
	} {
		for depth := range 3 {
			t.Run(fmt.Sprintf("%s/depth%d", tc.name, depth), func(t *testing.T) {
				nodes := []TreeNode{}
				parent := ""
				for d := range depth {
					id := fmt.Sprintf("parent%d", d)
					nodes = append(nodes, TreeNode{ID: id, Parent: parent, Label: "parent"})
					parent = id
				}
				nodes = append(nodes, TreeNode{ID: "row", Parent: parent, Label: "testing wrap start here", Cells: []string{tc.prefix + "testing wrap start here"}})
				_, _, tree := wrapTree(t, nodes, Columns(Column{Width: 2*depth + 2 + ansi.StringWidth(tc.prefix) + 12}))
				tree.Select("row")
				lines := bodyLines(ansi.Strip(tree.View(2*depth+2+ansi.StringWidth(tc.prefix)+12, 20)))
				at := indexOfLine(lines, "testing wrap")
				indent := strings.Repeat(" ", 2*depth+2+ansi.StringWidth(tc.prefix))
				if at < 0 || lines[at] != strings.Repeat(" ", 2*depth+2)+tc.prefix+"testing wrap" || lines[at+1] != indent+"start here" {
					t.Fatalf("hanging prefix:\n%s", strings.Join(lines, "\n"))
				}
			})
		}
	}
}

func TestTreeWrapWideAndLongWord(t *testing.T) {
	for _, tc := range []struct {
		label string
		want  []string
	}{
		{"界界 界界 界界", []string{"  界界 界界", "  界界"}},
		{"abcdefghijklmnopqrstuv", []string{"  abcdefghi", "  jklmnopqr", "  stuv"}},
	} {
		_, _, tree := wrapTree(t, []TreeNode{{ID: "row", Label: tc.label}})
		got := bodyLines(ansi.Strip(tree.View(11, 10)))[:len(tc.want)]
		if !slices.Equal(got, tc.want) {
			t.Fatalf("%q: got %q want %q", tc.label, got, tc.want)
		}
	}
}

func TestTreeWrapOff(t *testing.T) {
	r, _, tree := wrapTree(t, []TreeNode{{ID: "row", Label: "testing wrap start here"}})
	if err := r.Settings().Set("tree.wrap", false); err != nil {
		t.Fatal(err)
	}
	got := bodyLines(ansi.Strip(tree.View(14, 4)))
	if got[0] != "  testing wra…" || got[1] != "" {
		t.Fatalf("clipped row %q", got)
	}
}

func TestTreeWrapEditingKeepsPrefixAndBadges(t *testing.T) {
	_, m, tree := wrapTree(t, []TreeNode{{ID: "row", Label: "testing wrap start here", Cells: []string{"- [ ] testing wrap start here", "todo"}}, {ID: "next", Label: "next", Cells: []string{"next", "done"}}}, Columns(Column{Width: 20}, Column{Width: 4}))
	pressKeys(m, "i")
	tree.input.Cursor = len(tree.input.Text)
	block := tree.Block(26, 8)
	rows := bodyLines(ansi.Strip(block.Text))
	if rows[0] != "  - [ ] testing wrap  todo" || rows[1] != "        start here" || rows[2] != "  next                done" {
		t.Fatalf("editing rows %q", rows)
	}
	if block.Cursor == nil || *block.Cursor != (Pos{X: 18, Y: 1}) {
		t.Fatalf("cursor %+v", block.Cursor)
	}
	pressKeys(m, "ctrl+a")
	block = tree.Block(26, 8)
	if block.Cursor == nil || *block.Cursor != (Pos{X: 8, Y: 0}) {
		t.Fatalf("start cursor %+v", block.Cursor)
	}
}

func TestTreeWrapHighlightAndVisualSelection(t *testing.T) {
	_, m, tree := wrapTree(t, []TreeNode{{ID: "a", Label: "testing wrap start here"}, {ID: "b", Label: "another row"}})
	rows := strings.Split(tree.View(14, 8), "\n")
	for _, line := range rows[:2] {
		if !strings.Contains(line, "\x1b[7m") {
			t.Fatalf("not highlighted %q", line)
		}
	}
	pressKeys(m, "V", "j")
	if got := tree.List().Marked(); len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("marks %+v", got)
	}
}
