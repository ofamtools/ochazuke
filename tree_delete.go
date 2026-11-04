package ochazuke

import (
	"errors"
	"slices"
	"strings"
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
		if t.OrphanParent != nil {
			parent = t.OrphanParent(t.nodes[i])
		} else {
			parent = ""
		}
		// top is the node's ancestor that sits directly under parent (or
		// the node itself when it does): the children follow it.
		top := id
		for p := t.parentOf(top); p != parent && p != ""; p = t.parentOf(top) {
			top = p
		}
		if t.parentOf(top) == parent {
			at = slices.Index(t.children(parent), top)
			if top != id {
				at++ // after the ancestor, which stays
			}
		} else {
			at = len(t.children(parent)) // parent is not above the node: the end of its children
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
		return errors.New("no row selected")
	}
	// What goes is copied to the register as a yank does (d d then p moves
	// a row), read before anything is deleted: the rows of the selection
	// and, where a row takes its subtree with it, the subtree.
	snapshot := t.snapshotUnder(ids)
	var failure error
	for _, id := range ids {
		if t.index(id) < 0 {
			continue
		}
		if err := t.removeOne(id); err != nil {
			if !selection {
				failure = err
				break
			}
			failure = errors.Join(failure, err)
		}
	}
	if err := t.yankDeleted(snapshot); err != nil {
		failure = errors.Join(failure, err)
	}
	return failure
}

// snap is a row as it was before a delete: what a yank of it needs.
type snap struct {
	id, label string
	depth     int
}

// snapshotUnder is the rows at and under ids, in tree order.
func (t *Tree) snapshotUnder(ids []string) []snap {
	in := make(map[string]bool, len(ids))
	for _, id := range ids {
		in[id] = true
	}
	var out []snap
	for _, n := range t.nodes {
		for p := n.ID; p != ""; p = t.parentOf(p) {
			if in[p] {
				out = append(out, snap{n.ID, n.Label, t.depth(n.ID)})
				break
			}
		}
	}
	return out
}

// yankDeleted puts the snapshot rows that are gone in the registers, as a
// delete does (the numbered registers, the clipboard only for
// clipboard=unnamed), indented from the shallowest.
func (t *Tree) yankDeleted(rows []snap) error {
	t.removed = nil
	low, n := -1, 0
	gone := rows[:0:0]
	for _, row := range rows {
		if t.index(row.id) >= 0 {
			continue
		}
		gone = append(gone, row)
		if low < 0 || row.depth < low {
			low = row.depth
		}
		n++
	}
	if n == 0 {
		return nil
	}
	var b strings.Builder
	for _, row := range gone {
		b.WriteString(strings.Repeat("\t", row.depth-low) + strings.TrimSpace(strings.ReplaceAll(row.label, "\n", " ")) + "\n")
	}
	if err := t.r.storeYank(lineYank(b.String()), "", true); err != nil {
		return err
	}
	// The rows are remembered with their ids: a put of this very text puts
	// them back as they were (tree_yank.go), so d d then p moves a row.
	t.removed = &removedRows{text: b.String()}
	for _, row := range gone {
		t.removed.ids = append(t.removed.ids, row.id)
	}
	return nil
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
	err := t.commitPlan(changes, next)
	switch {
	case next != "":
		_ = t.r.SetFocus(next)
	case t.index(id) < 0:
		_ = t.list.Focus() // nothing left: the tree's list holds the focus
	}
	return err
}
