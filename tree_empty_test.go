package ochazuke

import (
	"slices"
	"testing"
)

func nodeIDs(tree *Tree) []string {
	var ids []string
	for _, n := range tree.Nodes() {
		ids = append(ids, n.ID)
	}
	return ids
}

// o then esc on the new blank row removes it, lands back on the row o ran
// on, and leaves nothing to undo: u does not bring it back.
func TestTreeBlankNewRowIsRemoved(t *testing.T) {
	t.Parallel()
	r, m, tree, writes := treeFixture(t)
	pressKeys(m, "o")
	if tree.Editing() == "" {
		t.Fatal("o opened no line")
	}
	pressKeys(m, "esc")
	if want := []string{"add t-n1 under root at 1: ", "delete t-n1"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	if got := nodeIDs(tree); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("nodes %v", got)
	}
	if r.Focus != "a" || r.Mode.Name != "normal" {
		t.Fatalf("focus %q mode %q, want a in normal", r.Focus, r.Mode.Name)
	}
	pressKeys(m, "u")
	if len(*writes) != 2 {
		t.Fatalf("u wrote %v: the blank row left no undo step", *writes)
	}
}

// A row edited down to nothing is removed in one undo step; u brings it
// back with the text it had, and the cursor landed on the row above.
func TestTreeRowEditedToNothingIsRemovedInOneStep(t *testing.T) {
	t.Parallel()
	r, m, tree, writes := treeFixture(t)
	pressKeys(m, "j", "i", "backspace", "esc")
	if want := []string{"delete b"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v: the blank text is never written", *writes, want)
	}
	if got := nodeIDs(tree); !slices.Equal(got, []string{"a"}) || r.Focus != "a" {
		t.Fatalf("nodes %v focus %q", got, r.Focus)
	}
	pressKeys(m, "u")
	if want := []string{"delete b", "add b under a at 0: beta"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	if n, _ := tree.Selected(); n.ID != "b" || n.Label != "beta" {
		t.Fatalf("selected %+v", n)
	}
}

// A row with children is not removed, nor a fixed row; an empty text is
// written as before.
func TestTreeEmptyRowWithChildrenOrFixedStays(t *testing.T) {
	t.Parallel()
	_, m, tree, writes := treeFixture(t)
	pressKeys(m, "i", "backspace", "esc")
	if want := []string{"rename a: "}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	if got := nodeIDs(tree); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("nodes %v", got)
	}
	_ = tree.SetNodes([]TreeNode{{ID: "a", Label: "alpha", Fixed: []string{TreeDelete}}})
	*writes = nil
	pressKeys(m, "i", "backspace", "esc")
	if got := nodeIDs(tree); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("a fixed row was removed: %v", got)
	}
}

// A row that was blank before anyone touched it is left alone when its
// line opens and closes unchanged.
func TestTreeUntouchedBlankRowStays(t *testing.T) {
	t.Parallel()
	_, m, tree, writes := treeFixture(t)
	_ = tree.SetNodes([]TreeNode{{ID: "a", Label: "alpha"}, {ID: "e", Label: ""}})
	pressKeys(m, "j", "i", "esc")
	if got := nodeIDs(tree); !slices.Equal(got, []string{"a", "e"}) || len(*writes) != 0 {
		t.Fatalf("nodes %v writes %v", got, *writes)
	}
}

// With tree.remove-empty-rows off a blank row stays.
func TestTreeRemoveEmptyRowsOff(t *testing.T) {
	t.Parallel()
	r, m, tree, _ := treeFixture(t)
	if err := r.Settings().Set(TreeRemoveEmptyRowsSetting, false); err != nil {
		t.Fatal(err)
	}
	pressKeys(m, "o", "esc")
	if got := nodeIDs(tree); len(got) != 3 {
		t.Fatalf("nodes %v", got)
	}
}

// enter on a blank line removes it and starts the next line in its place,
// so a run of enters never piles blank rows up; the last esc removes that.
func TestTreeEnterOnBlankLineReplacesIt(t *testing.T) {
	t.Parallel()
	_, m, tree, _ := treeFixture(t)
	pressKeys(m, "o", "enter", "enter", "esc")
	if got := nodeIDs(tree); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("nodes %v", got)
	}
}

// A blank line that was the tree's last row leaves the tree empty with the
// focus on its list, never on nothing.
func TestTreeEmptiedTreeKeepsFocus(t *testing.T) {
	t.Parallel()
	r, m, tree, _ := treeFixture(t)
	_ = tree.SetNodes([]TreeNode{{ID: "a", Label: "alpha"}})
	pressKeys(m, "i", "backspace", "esc")
	if got := nodeIDs(tree); len(got) != 0 || r.Focus != tree.FocusTarget() {
		t.Fatalf("nodes %v focus %q want %q", got, r.Focus, tree.FocusTarget())
	}
}

// A row declaring TreeRemoveEmpty that held text stays when its line is
// left blank: the old text comes back, nothing is written (not even by a
// flush while the line was blank) and no undo step is left.
func TestTreeRemoveEmptyDeclaredKeepsRow(t *testing.T) {
	t.Parallel()
	_, m, tree, writes := treeFixture(t)
	_ = tree.SetNodes([]TreeNode{{ID: "a", Label: "alpha"}, {ID: "b", Label: "beta", Fixed: []string{TreeRemoveEmpty}}})
	pressKeys(m, "j", "i", "backspace")
	if err := tree.Flush(); err != nil {
		t.Fatal(err)
	}
	pressKeys(m, "esc")
	if len(*writes) != 0 {
		t.Fatalf("writes %v, want none", *writes)
	}
	if n, ok := tree.Selected(); !ok || n.ID != "b" || n.Label != "beta" {
		t.Fatalf("selected %+v", n)
	}
	if got := nodeIDs(tree); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("nodes %v", got)
	}
	pressKeys(m, "u")
	if len(*writes) != 0 {
		t.Fatalf("u wrote %v: the kept row left no undo step", *writes)
	}
	// Other ops are not touched by the declaration: rename still works.
	pressKeys(m, "i", "x", "esc")
	if want := []string{"rename b: x"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
}

// A row just added and never filled in is removed even when it declares
// TreeRemoveEmpty: nothing existed yet.
func TestTreeRemoveEmptyDeclaredFreshRowIsRemoved(t *testing.T) {
	t.Parallel()
	_, m, tree, writes := treeFixture(t)
	pressKeys(m, "o")
	for i := range tree.nodes {
		if tree.nodes[i].ID == tree.Editing() {
			tree.nodes[i].Fixed = []string{TreeRemoveEmpty}
		}
	}
	pressKeys(m, "esc")
	if got := nodeIDs(tree); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("nodes %v writes %v", got, *writes)
	}
}

// A new row abandoned blank after tab or shift+tab leaves no undo step: u
// goes straight to the change before it.
func TestTreeBlankNewRowAfterIndentLeavesNoUndo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		keys []string
	}{
		{"tab", []string{"tab"}},
		{"shift+tab", []string{"shift+tab"}},
		{"tab then shift+tab", []string{"tab", "shift+tab"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, m, tree, writes := treeFixture(t)
			_ = tree.SetNodes([]TreeNode{
				{ID: "a", Label: "alpha"},
				{ID: "b", Parent: "a", Label: "beta"},
				{ID: "c", Parent: "a", Label: "gamma"},
			})
			pressKeys(m, "j", "j", "d", "d", "k", "o")
			pressKeys(m, tc.keys...)
			pressKeys(m, "esc")
			if got := nodeIDs(tree); !slices.Equal(got, []string{"a", "b"}) {
				t.Fatalf("nodes %v after esc", got)
			}
			pressKeys(m, "u")
			if got := nodeIDs(tree); !slices.Equal(got, []string{"a", "b", "c"}) {
				t.Fatalf("nodes %v after u, want the deleted c back", got)
			}
			if last := (*writes)[len(*writes)-1]; last != "add c under a at 1: gamma" {
				t.Fatalf("last write %q, writes %v", last, *writes)
			}
			pressKeys(m, "ctrl+r")
			if got := nodeIDs(tree); !slices.Equal(got, []string{"a", "b"}) {
				t.Fatalf("nodes %v after redo", got)
			}
		})
	}
}
