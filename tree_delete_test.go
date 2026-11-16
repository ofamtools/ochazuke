package ochazuke

import (
	"errors"
	"fmt"
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
	r, m, tree, writes := deleteFixture(t, "")
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
	// Asked by name (the palette, the CLI), the delete is kept: no error,
	// and the focus stays on b instead of moving to the next row.
	if _, err := r.Execute(Request{Noun: "ochazuke.outline", Verb: "delete"}); err != nil || shape(tree) != deleteStart || r.Focus != "b" {
		t.Fatalf("explicit delete of b: %v shape %q focus %q", err, shape(tree), r.Focus)
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

// A move in a delete plan that fails (or is refused) stops the plan: the
// delete is not written and not queued, the rows go back to what the store
// has, and u undoes only what was written.
func TestTreeDeletePlanStopsAtAFailedMove(t *testing.T) {
	t.Parallel()
	for name, fail := range map[string]error{"failed": errors.New("disk full"), "refused": fmt.Errorf("no: %w", ErrTreeRefused)} {
		_, m, tree, writes := deleteFixture(t, "")
		write := tree.OnChange
		tree.OnChange = func(c TreeChange) error {
			if c.String() == "move d under a at 1" {
				return fail
			}
			return write(c)
		}
		pressKeys(m, "j", "d", "d") // b: move c, move d (fails), delete b
		if got, want := shape(tree), "a<- c<a b<a d<b e<a f<-"; got != want {
			t.Fatalf("%s: shape %q, want %q", name, got, want)
		}
		if focus := m.Runtime.Focus; focus != "b" {
			t.Fatalf("%s: b stays, and so does the focus: %q", name, focus)
		}
		if want := []string{"move c under a at 0"}; !slices.Equal(*writes, want) {
			t.Fatalf("%s: writes %v, want %v", name, *writes, want)
		}
		pressKeys(m, "u")
		if got := shape(tree); got != deleteStart {
			t.Fatalf("%s: after u %q, want %q", name, got, deleteStart)
		}
		// Nothing was queued: the next write goes through alone.
		before := len(*writes)
		pressKeys(m, "G", "d", "d")
		if got := (*writes)[before:]; len(got) != 1 || got[0] != "delete f" {
			t.Fatalf("%s: later writes %v", name, got)
		}
	}
}

// OrphanParent names the level orphans go to.
func TestTreeDeleteOrphanParent(t *testing.T) {
	t.Parallel()
	_, m, tree, writes := deleteFixture(t, TreeDeleteOrphan)
	tree.OrphanParent = func(n TreeNode) string { return "a" }
	pressKeys(m, "j", "d", "d") // b under a: c and d go to a, where b was
	if got, want := shape(tree), "a<- c<a d<a e<a f<-"; got != want {
		t.Fatalf("shape %q, want %q", got, want)
	}
	if want := []string{"move c under a at 0", "move d under a at 1", "delete b"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
}

// Undoing a delete puts back the nodes as they were: every field (noun,
// data, props, cells, search words, ...), the whole subtree, in the same
// place, with the same collapsed states. The plan's leaf-first deletes and
// one delete of a node with children (a change an application commits)
// both come back whole.
func TestTreeDeleteUndoRestoresTheNodes(t *testing.T) {
	t.Parallel()
	rich := func(t *testing.T) (*Model, *Tree, []TreeNode) {
		t.Helper()
		r, m, tree, _ := deleteFixture(t, "")
		r.Define("leaf")
		nodes := tree.Nodes()
		for i := range nodes {
			n := &nodes[i]
			n.Data = map[string]any{"id": n.ID, "n": i}
			n.Props = Thing{"status": "open-" + n.ID}
			n.Cells = []string{n.ID + "-1", n.ID + "-2"}
			n.Search = []string{"word-" + n.ID}
			n.Section = "s"
			n.PutInto = i%2 == 0
			if n.ID == "c" {
				n.Noun = "leaf"
			}
		}
		if err := tree.SetNodes(nodes); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"b", "c"} {
			if err := tree.SetCollapsed(id, true); err != nil {
				t.Fatal(err)
			}
		}
		return m, tree, tree.Nodes()
	}
	same := func(t *testing.T, tree *Tree, want []TreeNode) {
		t.Helper()
		if got := tree.Nodes(); fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
			t.Fatalf("after undo\n got %#v\nwant %#v", got, want)
		}
		for _, id := range []string{"b", "c"} {
			if !tree.Collapsed(id) {
				t.Fatalf("%s is no longer collapsed after undo", id)
			}
		}
	}
	t.Run("plan", func(t *testing.T) {
		m, tree, want := rich(t)
		pressKeys(m, "j", "d", "d") // b is collapsed: its subtree goes, leaf first
		if got := shape(tree); got != "a<- e<a f<-" {
			t.Fatalf("shape %q", got)
		}
		pressKeys(m, "u")
		same(t, tree, want)
		pressKeys(m, "ctrl+r", "u")
		same(t, tree, want)
	})
	t.Run("one change", func(t *testing.T) {
		_, tree, want := rich(t)
		if err := tree.commit([]TreeChange{{Op: TreeDelete, ID: "b"}}, ""); err != nil {
			t.Fatal(err)
		}
		if got := shape(tree); got != "a<- e<a f<-" {
			t.Fatalf("shape %q", got)
		}
		if err := tree.history(false); err != nil {
			t.Fatal(err)
		}
		same(t, tree, want)
	})
}
