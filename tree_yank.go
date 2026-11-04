package ochazuke

import (
	"errors"
	"fmt"
	"strings"
)

// Outline yank and put. A row is yanked as text, one line per row, a tab
// per level below the shallowest row yanked, in the shared registers
// (storeYank) as whole lines; the system clipboard gets it as any yank
// does (the clipboard setting). Put reads a register back with pasteLines,
// so the same text pastes from the system clipboard, another outline or an
// editor, and every pasted row gets a fresh id through the add path.

// outlineText is the rows ids (in tree order) as indented lines.
func (t *Tree) outlineText(ids []string) (string, int) {
	if len(ids) == 0 {
		return "", 0
	}
	low := t.depth(ids[0])
	for _, id := range ids {
		low = min(low, t.depth(id))
	}
	var b strings.Builder
	for _, id := range ids {
		i := t.index(id)
		if i < 0 {
			continue
		}
		label := strings.TrimSpace(strings.ReplaceAll(t.nodes[i].Label, "\n", " "))
		b.WriteString(strings.Repeat("\t", t.depth(id)-low) + label + "\n")
	}
	return b.String(), len(ids)
}

// subtreeIDs is node id and its descendants, in tree order.
func (t *Tree) subtreeIDs(id string) []string {
	i := t.index(id)
	if i < 0 {
		return nil
	}
	var ids []string
	for _, n := range t.nodes[i:t.subtreeEnd(i)] {
		ids = append(ids, n.ID)
	}
	return ids
}

// selectionIDs are the rows a visual selection yanks, in tree order: the
// selected rows, and the hidden rows under a selected row that is folded
// (they go with it, as with d d).
func (t *Tree) selectionIDs(selected map[string]bool) []string {
	var ids []string
	for _, n := range t.nodes {
		take := selected[n.ID]
		for p := n.Parent; p != "" && !take; p = t.parentOf(p) {
			take = selected[p] && t.folded(p)
		}
		if take {
			ids = append(ids, n.ID)
		}
	}
	return ids
}

// yankRows is how the tree's list copies a visual selection (List.yankRows).
func (t *Tree) yankRows(items []ListItem) (Yank, int) {
	selected := make(map[string]bool, len(items))
	for _, it := range items {
		selected[it.ID] = true
	}
	text, n := t.outlineText(t.selectionIDs(selected))
	return lineYank(text), n
}

// lineYank is text as a yank of whole lines.
func lineYank(text string) Yank { return Yank{Text: text, Shape: "line"} }

// yank copies the row under the cursor with its subtree into a register.
func (t *Tree) yank(c Context, register string) error {
	if err := t.closeLineAs(false, false); err != nil {
		return err
	}
	id := t.target(c)
	if t.index(id) < 0 {
		return errors.New("tree has no selected node")
	}
	text, n := t.outlineText(t.subtreeIDs(id))
	if err := t.r.storeYank(lineYank(text), register, false); err != nil {
		return err
	}
	t.r.UI().Info(copiedRows(n))
	return nil
}

func copiedRows(n int) string {
	if n == 1 {
		return "copied 1 row"
	}
	return fmt.Sprintf("copied %d rows", n)
}

// removedRows are the rows of the tree's last delete: their ids, in tree
// order, and the register text the delete made of them.
type removedRows struct {
	text string
	ids  []string
}

// removedIDs are the ids of the last delete's rows when text is the very
// text it put in the register and none of them is in the tree: a put of it
// moves the rows (their own ids, so a store puts back what it held), once.
// Otherwise nil: the rows are copies, with fresh ids.
func (t *Tree) removedIDs(text string, lines int) []string {
	rm := t.removed
	if rm == nil || rm.text != text || len(rm.ids) != lines {
		return nil
	}
	for _, id := range rm.ids {
		if t.index(id) >= 0 {
			return nil
		}
	}
	return rm.ids
}

// put pastes a register's rows next to the row under the cursor: after its
// subtree as its next sibling, or before it (before). Rows go in with fresh
// ids through add, as one undo step, nested as the yanked text is; on an
// empty tree they become the top level. The cursor goes to the first row
// pasted, unless a filter hides it (then it stays; the rows are in the
// tree).
func (t *Tree) put(c Context, before bool, register string) error {
	y, err := t.r.readYank(register)
	t.r.Registers().Active = ""
	if err != nil {
		return err
	}
	lines, depths := pasteLines(y.Text)
	if len(lines) == 0 {
		return errors.New("nothing in the register to put")
	}
	if err := t.closeLineAs(false, false); err != nil {
		return err
	}
	parent, index := t.slot(t.target(c), before)
	ids := t.removedIDs(y.Text, len(lines))
	t.removed = nil // a delete's rows move once; the next put copies
	changes := t.blockAddsWith(lines, depths, parent, index, ids)
	first := changes[0].ID
	if err := t.commit(changes, ""); err != nil {
		return err
	}
	if t.list.shows(first) {
		t.focusNode(first)
	} else {
		t.r.UI().Info(fmt.Sprintf("put %d rows, hidden by the filter", len(lines)))
	}
	return nil
}
