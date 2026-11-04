package ochazuke

import (
	"errors"
	"slices"
)

// TreeDeleteChildrenSetting says what deleting a node that has children
// does with them.
const TreeDeleteChildrenSetting = "tree.delete-children"

// Values of tree.delete-children.
const (
	// TreeDeletePromote moves an expanded node's children up one level into
	// its place, in order; a collapsed node deletes its subtree.
	TreeDeletePromote = "promote"
	// TreeDeleteOrphan moves an expanded node's children to the top level,
	// in order, at the node's top-level ancestor; a collapsed node deletes
	// its subtree.
	TreeDeleteOrphan = "orphan"
	// TreeDeleteSubtree always deletes the node with its whole subtree.
	TreeDeleteSubtree = "subtree"
)

// TreeDeleteChildren is the tree.delete-children setting: promote
// (default), orphan or subtree.
func (r *Runtime) TreeDeleteChildren() string {
	return r.treeSetting(TreeDeleteChildrenSetting, TreeDeletePromote, TreeDeletePromote, TreeDeleteOrphan, TreeDeleteSubtree)
}

// keepsChildren reports whether deleting id leaves its children: it has
// some, shows them (it is not folded; a filter's folds count while props
// reveal context, as folded reads) and the setting does not delete the
// subtree. A folded node's hidden children go with it.
func (t *Tree) keepsChildren(id string) bool {
	return t.hasChildren(id) && !t.folded(id) && t.r.TreeDeleteChildren() != TreeDeleteSubtree
}

// deletePlan is the changes that delete id: for a node that keeps its
// children, a move of each out to where they go (in order, before the
// delete, so a store writing each change never deletes or orphans them)
// and the delete of the node alone; otherwise the deletes of the subtree,
// deepest first so each write is a leaf (undo adds them back root first).
func (t *Tree) deletePlan(id string) []TreeChange {
	i := t.index(id)
	if i < 0 {
		return nil
	}
	if !t.keepsChildren(id) {
		sub := t.nodes[i:t.subtreeEnd(i)]
		changes := make([]TreeChange, 0, len(sub))
		for j := len(sub) - 1; j >= 0; j-- {
			changes = append(changes, TreeChange{Op: TreeDelete, ID: sub[j].ID})
		}
		return changes
	}
	parent, at := t.parentOf(id), 0
	switch t.r.TreeDeleteChildren() {
	case TreeDeleteOrphan:
		top := id
		for p := t.parentOf(top); p != ""; p = t.parentOf(top) {
			top = p
		}
		parent, at = "", slices.Index(t.children(""), top)
		if top != id {
			at++ // after the ancestor, which stays
		}
	default:
		at = slices.Index(t.children(parent), id)
	}
	kids := t.children(id)
	changes := make([]TreeChange, 0, len(kids)+1)
	for k, kid := range kids {
		changes = append(changes, TreeChange{Op: TreeMove, ID: kid, Parent: parent, Index: at + k})
	}
	return append(changes, TreeChange{Op: TreeDelete, ID: id})
}

// remove deletes the node under the cursor, or every node of a visual
// selection as one undo step (deletePlan for each: a node already gone
// with an earlier one's subtree is skipped, so a selected parent and child
// are neither an error nor deleted twice). The node's children follow
// tree.delete-children.
func (t *Tree) remove(c Context) error {
	if err := t.closeLineAs(false, false); err != nil {
		return err
	}
	ids := []string{t.target(c)}
	l := t.list
	selection := l.visual
	if selection {
		if marked := l.markedShown(); len(marked) > 0 {
			ids = ids[:0]
			for _, it := range marked {
				ids = append(ids, it.ID)
			}
		}
		defer t.r.afterVisualAction(l)
		defer t.group()()
	}
	if t.index(ids[0]) < 0 {
		return errors.New("tree has no selected node")
	}
	var failure error
	for _, id := range ids {
		if t.index(id) < 0 {
			continue
		}
		if err := t.removeOne(id); err != nil {
			if !selection {
				return err
			}
			failure = errors.Join(failure, err)
		}
	}
	return failure
}

// removeOne deletes one node by its plan and puts the cursor on the row
// that follows it, else the one before, else the tree's list.
func (t *Tree) removeOne(id string) error {
	changes := t.deletePlan(id)
	gone := map[string]bool{}
	for _, ch := range changes {
		if ch.Op == TreeDelete {
			gone[ch.ID] = true
		}
	}
	next := ""
	items := t.list.Items()
	if k := slices.IndexFunc(items, func(it ListItem) bool { return it.ID == id }); k >= 0 {
		for _, it := range items[k+1:] {
			if !gone[it.ID] {
				next = it.ID
				break
			}
		}
		if next == "" && k > 0 {
			next = items[k-1].ID
		}
	}
	err := t.commit(changes, next)
	switch {
	case next != "":
		_ = t.r.SetFocus(next)
	case t.index(id) < 0:
		_ = t.list.Focus() // nothing left: the tree's list holds the focus
	}
	return err
}
