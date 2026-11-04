package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

// deleteFixture is the outline
//
//	a
//	  b
//	    c
//	    d
//	  e
//	f
//
// editable, focused, its writes logged, tree.delete-children set to mode.
func deleteFixture(t *testing.T, mode string) (*Runtime, *Model, *Tree, *[]string) {
	t.Helper()
	r, m, tree, writes := treeFixture(t)
	if mode != "" {
		if err := r.Settings().Set(TreeDeleteChildrenSetting, mode); err != nil {
			t.Fatal(err)
		}
	}
	nodes := []TreeNode{
		{ID: "a", Label: "a"}, {ID: "b", Parent: "a", Label: "b"}, {ID: "c", Parent: "b", Label: "c"},
		{ID: "d", Parent: "b", Label: "d"}, {ID: "e", Parent: "a", Label: "e"}, {ID: "f", Label: "f"},
	}
	if err := tree.SetNodes(nodes); err != nil {
		t.Fatal(err)
	}
	return r, m, tree, writes
}

// shape is the nodes as "id<parent" in order, a structure to compare.
func shape(tree *Tree) string {
	var parts []string
	for _, n := range tree.Nodes() {
		p := n.Parent
		if p == "" {
			p = "-"
		}
		parts = append(parts, n.ID+"<"+p)
	}
	return strings.Join(parts, " ")
}

const deleteStart = "a<- b<a c<b d<b e<a f<-"

// movesBeforeDeletes checks the writes a store applies one by one: no
// delete is followed by a move, so a child is never moved out of a row that
// has already gone.
func movesBeforeDeletes(t *testing.T, writes []string) {
	t.Helper()
	deleted := false
	for _, w := range writes {
		switch {
		case strings.HasPrefix(w, "delete"):
			deleted = true
		case strings.HasPrefix(w, "move") && deleted:
			t.Fatalf("a move after a delete: %v", writes)
		}
	}
}

func TestTreeDeletePromoteExpandedPassesChildrenUp(t *testing.T) {
	t.Parallel()
	_, m, tree, writes := deleteFixture(t, "")
	pressKeys(m, "j", "d", "d")
	if got, want := shape(tree), "a<- c<a d<a e<a f<-"; got != want {
		t.Fatalf("shape %q, want %q", got, want)
	}
	if want := []string{"move c under a at 0", "move d under a at 1", "delete b"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	movesBeforeDeletes(t, *writes)
	if n, _ := tree.Selected(); n.ID != "c" {
		t.Fatalf("cursor on %q, want the first promoted child c", n.ID)
	}
	pressKeys(m, "u")
	if got := shape(tree); got != deleteStart {
		t.Fatalf("after u %q, want %q", got, deleteStart)
	}
	movesBeforeDeletes(t, (*writes)[3:]) // the undo's own order: add before moves, as a store needs
	pressKeys(m, "ctrl+r")
	if got, want := shape(tree), "a<- c<a d<a e<a f<-"; got != want {
		t.Fatalf("after redo %q, want %q", got, want)
	}
}

func TestTreeDeleteCollapsedTakesTheSubtree(t *testing.T) {
	t.Parallel()
	_, m, tree, writes := deleteFixture(t, "")
	pressKeys(m, "j", "h", "d", "d")
	if got, want := shape(tree), "a<- e<a f<-"; got != want {
		t.Fatalf("shape %q, want %q", got, want)
	}
	if want := []string{"delete d", "delete c", "delete b"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	pressKeys(m, "u")
	if got := shape(tree); got != deleteStart {
		t.Fatalf("after u %q, want %q", got, deleteStart)
	}
}

func TestTreeDeleteSubtreeSettingAlwaysTakesTheSubtree(t *testing.T) {
	t.Parallel()
	_, m, tree, writes := deleteFixture(t, TreeDeleteSubtree)
	pressKeys(m, "j", "d", "d")
	if got, want := shape(tree), "a<- e<a f<-"; got != want {
		t.Fatalf("shape %q, want %q", got, want)
	}
	if want := []string{"delete d", "delete c", "delete b"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
}

func TestTreeDeleteOrphanMovesChildrenToTheTopLevel(t *testing.T) {
	t.Parallel()
	_, m, tree, writes := deleteFixture(t, TreeDeleteOrphan)
	pressKeys(m, "j", "d", "d")
	if got, want := shape(tree), "a<- e<a c<- d<- f<-"; got != want && got != "a<- e<a c<- d<- f<-" {
		t.Fatalf("shape %q, want %q", got, want)
	}
	if want := []string{"move c under root at 1", "move d under root at 2", "delete b"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	movesBeforeDeletes(t, *writes)
	pressKeys(m, "u")
	if got := shape(tree); got != deleteStart {
		t.Fatalf("after u %q, want %q", got, deleteStart)
	}
	// A top-level node's children take its place at the top.
	*writes = nil
	pressKeys(m, "ctrl+r", "u")
	pressKeys(m, "g", "g", "d", "d")
	if got, want := shape(tree), "b<- c<b d<b e<- f<-"; got != want {
		t.Fatalf("shape %q, want %q", got, want)
	}
	pressKeys(m, "u")
	if got := shape(tree); got != deleteStart {
		t.Fatalf("after u %q, want %q", got, deleteStart)
	}
}

// A fixed row still refuses, and so does an ancestor of a fixed-delete row
// (Fixed: deleting it or any ancestor), however its children would be kept.
func TestTreeDeleteFixedRowsStillRefuse(t *testing.T) {
	t.Parallel()
	_, m, tree, writes := deleteFixture(t, "")
	nodes := tree.Nodes()
	nodes[0].Fixed = []string{TreeDelete}
	_ = tree.SetNodes(nodes)
	pressKeys(m, "d", "d")
	if got := shape(tree); got != deleteStart || len(*writes) != 0 {
		t.Fatalf("a fixed row was deleted: %q %v", got, *writes)
	}
	nodes[0].Fixed = nil
	nodes[2].Fixed = []string{TreeDelete} // c refuses deletion, and so do b and a
	_ = tree.SetNodes(nodes)
	pressKeys(m, "g", "g", "j", "d", "d")
	if got := shape(tree); got != deleteStart || len(*writes) != 0 {
		t.Fatalf("an ancestor of a fixed row was deleted: %q %v", got, *writes)
	}
}

// A visual selection deletes every selected node as one undo step; a
// parent and its selected child are neither an error nor deleted twice.
func TestTreeDeleteVisualSelectionIsOneStep(t *testing.T) {
	t.Parallel()
	r, m, tree, writes := deleteFixture(t, "")
	pressKeys(m, "V", "j", "d") // a and b
	if got, want := shape(tree), "c<- d<- e<- f<-"; got != want {
		t.Fatalf("shape %q, want %q (writes %v)", got, want, *writes)
	}
	if r.Mode.Name != "normal" {
		t.Fatalf("mode %q after the action", r.Mode.Name)
	}
	if n := strings.Count(strings.Join(*writes, ";"), "delete"); n != 2 {
		t.Fatalf("%d deletes in %v, want 2 (each node once)", n, *writes)
	}
	pressKeys(m, "u")
	if got := shape(tree); got != deleteStart {
		t.Fatalf("one u gave %q, want %q", got, deleteStart)
	}
	// A collapsed node selected takes its subtree; its expanded parent
	// passes its children up.
	pressKeys(m, "g", "g", "j", "h", "V", "k", "d")
	if got, want := shape(tree), "e<- f<-"; got != want {
		t.Fatalf("shape %q, want %q", got, want)
	}
	pressKeys(m, "u")
	if got := shape(tree); got != deleteStart {
		t.Fatalf("one u gave %q, want %q", got, deleteStart)
	}
}

// Deleting the last row leaves the focus on the tree's list.
func TestTreeDeleteLastRowKeepsTheFocus(t *testing.T) {
	t.Parallel()
	r, m, tree, _ := treeFixture(t)
	_ = tree.SetNodes([]TreeNode{{ID: "a", Label: "alpha"}})
	pressKeys(m, "d", "d")
	if r.Focus != tree.FocusTarget() || tree.List().Len() != 0 {
		t.Fatalf("focus %q rows %d", r.Focus, tree.List().Len())
	}
}
