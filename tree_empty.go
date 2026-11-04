package ochazuke

import (
	"slices"
	"strings"
)

// TreeRemoveEmptyRowsSetting removes a row whose line is left empty: a new
// row left blank, or a row edited down to nothing.
const TreeRemoveEmptyRowsSetting = "tree.remove-empty-rows"

// RemoveEmptyRows is the tree.remove-empty-rows setting (default on).
func (r *Runtime) RemoveEmptyRows() bool {
	v, _ := r.settingValue(TreeRemoveEmptyRowsSetting)
	on, _ := v.(bool)
	return on
}

// markFresh notes that add made row id from the row from: it counts as
// blank until it holds text, and esc on it lands back on from.
func (t *Tree) markFresh(id, from string) {
	if t.fresh == nil {
		t.fresh, t.origin = map[string]bool{}, map[string]string{}
	}
	t.fresh[id] = true
	if from != "" {
		t.origin[id] = from
	}
}

// emptyRemovable reports whether leaving id's line removes the row: the
// setting is on, the line's text is blank, and the user made it so (a row add
// made and never filled in, or a row that held text when the line opened).
// A row with children stays, as does one that refuses a delete (Fixed or
// Refuse), so nothing is lost and nothing refused is forced. A row that
// was blank before anyone touched it is left alone.
func (t *Tree) emptyRemovable(id string, blank bool) bool {
	if t.index(id) < 0 || !blank || !t.r.RemoveEmptyRows() || t.keepsText(id, "") {
		return false
	}
	if !t.fresh[id] && strings.TrimSpace(t.opened) == "" {
		return false
	}
	if t.hasChildren(id) {
		return false
	}
	return t.refuses(TreeChange{Op: TreeDelete, ID: id}) == nil
}

// landing is the row the cursor goes to when id's row is removed: the row
// id was added from, else the next shown row, else the previous one, "" in
// an emptied tree.
func (t *Tree) landing(id string) string {
	if from := t.origin[id]; from != "" && from != id && t.index(from) >= 0 && t.list.shows(from) {
		return from
	}
	items := t.list.Items()
	k := slices.IndexFunc(items, func(it ListItem) bool { return it.ID == id })
	if k < 0 {
		return ""
	}
	for _, it := range items[k+1:] {
		if t.list.shows(it.ID) {
			return it.ID
		}
	}
	for j := k - 1; j >= 0; j-- {
		if t.list.shows(items[j].ID) {
			return items[j].ID
		}
	}
	return ""
}

// dropEmpty removes id's blank row as its line closes. Its blank text is
// never written: the removal is the one change, and its undo brings the
// row back as the line opened, so emptying and removing are one step. A
// row that was added and never filled in leaves no step at all (the add is
// taken back, as if the row had not been made). The cursor goes
// to landing; with nothing left the tree's list takes the focus.
func (t *Tree) dropEmpty(id string, refocus bool) error {
	next := t.landing(id)
	change := TreeChange{Op: TreeDelete, ID: id}
	inv, err := t.apply(change)
	if err != nil {
		return err
	}
	delete(t.fresh, id)
	step := treeEdit{undo: []TreeChange{inv}}
	switch n := len(t.undo); {
	case n > 0 && len(t.undo[n-1].undo) == 1 && t.undo[n-1].undo[0].Op == TreeDelete && t.undo[n-1].undo[0].ID == id:
		t.undo = t.undo[:n-1] // the add of this row: both vanish
	default:
		t.undo = append(t.undo, step)
	}
	t.redo = nil
	t.dropped = id
	werr := t.write([]TreeChange{change})
	if next != "" {
		_ = t.Select(next)
	}
	if refocus {
		if next != "" {
			_ = t.r.SetFocus(next)
		} else {
			_ = t.list.Focus()
		}
		if t.r.Mode.Name != "normal" {
			_ = t.r.SetMode("normal")
		}
	}
	return werr
}

// keepsText reports whether the open line of id, holding text, must not be
// written: the row declares TreeRemoveEmpty, the text is blank, and the row
// held text when the line opened and was not just added. The row keeps its
// old text, nothing is written and no undo step is left.
func (t *Tree) keepsText(id, text string) bool {
	i := t.index(id)
	if i < 0 || strings.TrimSpace(text) != "" || !t.nodes[i].Refuses(TreeRemoveEmpty) {
		return false
	}
	return !t.fresh[id] && strings.TrimSpace(t.opened) != ""
}
