package ochazuke

import (
	"errors"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke/keytest"
)

// fixedTree is treeFixture with the root a fixed (rename, move, delete)
// and a second root c whose child d refuses a rename only.
func fixedTree(t *testing.T) (*Runtime, *Model, *Tree, *[]string) {
	t.Helper()
	r, m, tree, writes := treeFixture(t)
	if err := tree.SetNodes([]TreeNode{
		{ID: "a", Label: "alpha", Fixed: []string{TreeRename, TreeMove, TreeDelete}},
		{ID: "b", Parent: "a", Label: "beta"},
		{ID: "c", Label: "gamma"},
		{ID: "d", Parent: "c", Label: "delta", Fixed: []string{TreeRename}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tree.Select("a"); err != nil {
		t.Fatal(err)
	}
	_ = r.SetFocus("a")
	m.View()
	return r, m, tree, writes
}

// A fixed node refuses delete, move, indent, outdent and rename from every
// path: its keys do nothing, the palette's and the API's requests are
// refused, and nothing is written.
func TestFixedNodeRefusesFromEveryPath(t *testing.T) {
	r, m, tree, writes := fixedTree(t)
	for _, k := range []string{"d", "d", "J", "i"} {
		keytest.Press(m, k)
	}
	if len(*writes) != 0 || len(tree.Nodes()) != 4 || tree.Editing() != "" {
		t.Fatalf("keys changed a fixed node: %v editing=%q", *writes, tree.Editing())
	}
	for _, verb := range []string{"delete", "edit", "indent", "outdent"} {
		if _, err := r.Execute(Request{Noun: "ochazuke.outline", Verb: verb, Target: "a"}); err == nil || !strings.Contains(err.Error(), "not applicable") {
			t.Fatalf("outline.%s on the fixed root: %v", verb, err)
		}
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.outline", Verb: "move", Target: "a", Args: Arguments{"delta": 1}}); err == nil {
		t.Fatal("outline.move on the fixed root must be refused")
	}
	if err := tree.Edit("a", false); err == nil || !strings.Contains(err.Error(), "refuses a rename") {
		t.Fatalf("Tree.Edit: %v", err)
	}
	if len(*writes) != 0 {
		t.Fatalf("writes %v", *writes)
	}
	// A child of the root is free: delete b.
	if _, err := r.Execute(Request{Noun: "ochazuke.outline", Verb: "delete", Target: "b"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*writes, ";") != "delete b" {
		t.Fatalf("writes %v", *writes)
	}
}

// Rename-only: d refuses a rename but moves and deletes; deleting its
// parent c is refused only when a descendant refuses deletion (it does not
// here), and a fixed-delete descendant blocks its ancestor's delete.
func TestFixedNodeGranularity(t *testing.T) {
	r, _, tree, writes := fixedTree(t)
	if _, err := r.Execute(Request{Noun: "ochazuke.outline", Verb: "edit", Target: "d"}); err == nil {
		t.Fatal("rename of d must be refused")
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.outline", Verb: "outdent", Target: "d"}); err != nil {
		t.Fatalf("d moves: %v", err)
	}
	if !tree.allows(TreeDelete, "d") || tree.allows(TreeRename, "d") {
		t.Fatal("availability of d")
	}
	// A fixed-delete node under c: deleting c is refused, naming it.
	if err := tree.SetNodes([]TreeNode{{ID: "c", Label: "gamma"}, {ID: "e", Parent: "c", Label: "keep", Fixed: []string{TreeDelete}}}); err != nil {
		t.Fatal(err)
	}
	*writes = nil
	if err := tree.commit([]TreeChange{{Op: TreeDelete, ID: "e"}, {Op: TreeDelete, ID: "c"}}, ""); err == nil || !strings.Contains(err.Error(), `"keep" refuses deletion`) {
		t.Fatalf("delete of c: %v", err)
	}
	if tree.allows(TreeDelete, "c") {
		t.Fatal("c holds a fixed-delete node: outline-deletable must be false")
	}
	if len(*writes) != 0 || len(tree.Nodes()) != 2 {
		t.Fatalf("nothing is applied: %v", *writes)
	}
}

// Tree.Refuse is the application's check: its error refuses the change
// with that reason from every path; undo is not checked.
func TestTreeRefuseCheck(t *testing.T) {
	r, _, tree, writes := treeFixture(t)
	tree.Refuse = func(n TreeNode, c TreeChange) error {
		if c.Op == TreeMove && c.Parent == "a" {
			return errors.New("alpha takes no children by a move")
		}
		return nil
	}
	if err := tree.SetNodes([]TreeNode{{ID: "a", Label: "alpha"}, {ID: "x", Label: "x"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.outline", Verb: "indent", Target: "x"}); err == nil || !strings.Contains(err.Error(), "no children by a move") {
		t.Fatalf("indent under a: %v", err)
	}
	if len(*writes) != 0 {
		t.Fatalf("writes %v", *writes)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.outline", Verb: "move", Target: "x", Args: Arguments{"delta": -1}}); err != nil {
		t.Fatalf("a move among roots is allowed: %v", err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.outline", Verb: "undo", Target: "x"}); err != nil {
		t.Fatal(err)
	}
}
