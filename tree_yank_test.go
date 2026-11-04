package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

func registerText(r *Runtime, name string) string {
	y, _ := r.readYank(name)
	return y.Text
}

// y y copies the row with its subtree as indented lines; p puts them after
// the cursor row's subtree as its next sibling, P before it, each row with
// a fresh id, and one u takes the whole paste back.
func TestTreeYankAndPut(t *testing.T) {
	t.Parallel()
	r, m, tree, writes := deleteFixture(t, "")
	pressKeys(m, "j", "y", "y") // b with c and d
	if got, want := registerText(r, ""), "b\n\tc\n\td\n"; got != want {
		t.Fatalf("register %q, want %q", got, want)
	}
	if y, _ := r.readYank(""); y.Shape != "line" {
		t.Fatalf("shape %q, want line", y.Shape)
	}
	if got := shape(tree); got != deleteStart || len(*writes) != 0 {
		t.Fatalf("a yank changed the tree: %q %v", got, *writes)
	}
	pressKeys(m, "G", "p") // after f
	if got, want := len(tree.Nodes()), 9; got != want {
		t.Fatalf("%d nodes, want %d: %s", got, want, shape(tree))
	}
	ids := map[string]bool{}
	for _, n := range tree.Nodes() {
		ids[n.ID] = true
	}
	if len(ids) != 9 {
		t.Fatal("pasted rows share ids")
	}
	var labels []string
	for _, n := range tree.Nodes() {
		labels = append(labels, strings.Repeat("-", tree.depth(n.ID))+n.Label)
	}
	if got, want := strings.Join(labels, " "), "a -b --c --d -e f b -c -d"; got != want {
		t.Fatalf("rows %q, want %q", got, want)
	}
	if n, _ := tree.Selected(); n.Label != "b" || tree.depth(n.ID) != 0 {
		t.Fatalf("cursor on %+v, want the first pasted row", n)
	}
	pressKeys(m, "u")
	if got := shape(tree); got != deleteStart {
		t.Fatalf("one u gave %q", got)
	}
	// P puts before the cursor row.
	pressKeys(m, "g", "g", "P")
	if tree.Nodes()[0].Label != "b" || tree.Nodes()[3].Label != "a" {
		t.Fatalf("P: %s", shape(tree))
	}
}

// d d fills the register as a yank does, so d d then p moves a row.
func TestTreeDeleteThenPutMovesARow(t *testing.T) {
	t.Parallel()
	r, m, tree, _ := deleteFixture(t, "")
	pressKeys(m, "G", "d", "d") // f, a leaf
	if got := registerText(r, ""); got != "f\n" {
		t.Fatalf("register %q", got)
	}
	if got := registerText(r, "1"); got != "f\n" {
		t.Fatalf("register 1 %q: a line delete shifts the numbered registers", got)
	}
	pressKeys(m, "g", "g", "P")
	if tree.Nodes()[0].Label != "f" || len(tree.Nodes()) != 6 {
		t.Fatalf("rows %s", shape(tree))
	}
	// An expanded parent deletes alone: only its row is in the register.
	pressKeys(m, "j", "j", "d", "d")
	if got := registerText(r, ""); got != "b\n" {
		t.Fatalf("register after deleting an expanded row: %q", got)
	}
}

// Over a visual selection y yanks the selection in order and p puts it.
func TestTreeVisualYankAndPut(t *testing.T) {
	t.Parallel()
	r, m, tree, _ := deleteFixture(t, "")
	pressKeys(m, "j", "V", "j", "j", "y") // b, c, d
	if got, want := registerText(r, ""), "b\n\tc\n\td\n"; got != want {
		t.Fatalf("register %q, want %q", got, want)
	}
	if r.Mode.Name != "normal" {
		t.Fatalf("mode %q", r.Mode.Name)
	}
	// A selected folded row takes its hidden subtree along; an expanded one
	// only the selected rows under it.
	pressKeys(m, "g", "g", "j", "h", "V", "j", "y") // b folded, e
	if got, want := registerText(r, ""), "b\n\tc\n\td\ne\n"; got != want {
		t.Fatalf("register %q, want %q", got, want)
	}
	pressKeys(m, "g", "g", "G", "p")
	var labels []string
	for _, n := range tree.Nodes() {
		labels = append(labels, strings.Repeat("-", tree.depth(n.ID))+n.Label)
	}
	if got, want := strings.Join(labels, " "), "a -b --c --d -e f b -c -d e"; got != want {
		t.Fatalf("rows %q, want %q", got, want)
	}
}

// Putting into an empty tree makes the top level; an empty register says so.
func TestTreePutOnAnEmptyTree(t *testing.T) {
	t.Parallel()
	r, m, tree, _ := treeFixture(t)
	if err := tree.SetNodes(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.outline", Verb: "put", Target: "t"}); err == nil {
		t.Fatal("put from an empty register must say so")
	}
	if err := r.storeYank(Yank{Text: "one\n\ttwo\nthree\n", Shape: "line"}, "", false); err != nil {
		t.Fatal(err)
	}
	if err := tree.Focus(); err != nil {
		t.Fatal(err)
	}
	pressKeys(m, "p")
	if got := shape(tree); !strings.HasPrefix(got, "t-n") || len(tree.Nodes()) != 3 {
		t.Fatalf("rows %s", got)
	}
	if tree.Nodes()[1].Parent != tree.Nodes()[0].ID || tree.Nodes()[2].Parent != "" {
		t.Fatalf("nesting %s", shape(tree))
	}
}

// A paste into a filtered view keeps its rows: they are in the tree, the
// filter and the cursor stay.
func TestTreePutIntoFilteredViewLosesNothing(t *testing.T) {
	t.Parallel()
	r, m, tree, writes := deleteFixture(t, "")
	pressKeys(m, "y", "y") // a with its subtree
	tree.List().SetFilter("f")
	before := len(tree.Nodes())
	pressKeys(m, "p")
	if got := len(tree.Nodes()); got != before+5 {
		t.Fatalf("%d nodes, want %d", got, before+5)
	}
	if tree.List().Filter() != "f" {
		t.Fatalf("filter %q cleared by the paste", tree.List().Filter())
	}
	if len(*writes) != 5 {
		t.Fatalf("writes %v", *writes)
	}
	for _, w := range *writes {
		if !strings.HasPrefix(w, "add ") {
			t.Fatalf("write %q", w)
		}
	}
	_ = r
	pressKeys(m, "u")
	if len(tree.Nodes()) != before {
		t.Fatalf("one u left %d nodes", len(tree.Nodes()))
	}
}

// y y does not exist on a read-only tree (its verbs need an editable one),
// and the numbered register yanks keep the clipboard text: a yank sets the
// clipboard to send when the clipboard setting says so.
func TestTreeYankReachesTheClipboard(t *testing.T) {
	t.Parallel()
	r, m, tree, _ := deleteFixture(t, "")
	pressKeys(m, "y", "y")
	cmd := r.Registers().PendingCommand()
	if cmd == nil {
		t.Fatal("a yank left no clipboard command")
	}
	tree.OnChange = nil
	if slices.ContainsFunc(r.ListBindings(r.Focus), func(b Binding) bool { return b.Request.Verb == "yank" && b.Request.Noun == "ochazuke.outline" }) {
		t.Fatal("a read-only tree offers yank")
	}
	_ = m
}
