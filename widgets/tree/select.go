package tree

import (
	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
)

func (m *Model) selectState() traits.SelectState {
	return traits.SelectState{
		Marked:       m.Marked,
		VisualActive: m.VisualActive,
		VisualAnchor: m.VisualAnchor,
	}
}

func (m *Model) applySelectState(st traits.SelectState) {
	m.Marked = st.Marked
	m.VisualActive = st.VisualActive
	m.VisualAnchor = st.VisualAnchor
}

func (m *Model) visibleIndex(rows []VisibleRow) int {
	if len(rows) == 0 {
		return -1
	}
	if m.hasAddDraft() {
		for i, r := range rows {
			if r.Draft {
				return i
			}
		}
	}
	for i, r := range rows {
		if !r.Draft && r.ID == m.SelectedID {
			return i
		}
	}
	return 0
}

func rowVisibleIDs(rows []VisibleRow) []string {
	ids := make([]string, len(rows))
	for i, r := range rows {
		if !r.Draft {
			ids[i] = r.ID
		}
	}
	return ids
}

func (m *Model) visualBounds(rows []VisibleRow, curIdx int) (from, to int, ok bool) {
	if curIdx < 0 {
		return 0, 0, false
	}
	return traits.SelectBounds(m.selectState(), len(rows), curIdx)
}

// ToggleMark flips the mark on the focused node.
func (m *Model) ToggleMark() {
	m.Clamp()
	rows := m.Flatten()
	curIdx := m.visibleIndex(rows)
	if curIdx < 0 {
		return
	}
	st := m.selectState()
	st.VisualActive = false
	next, _, _ := traits.SelectHandle(st, rowVisibleIDs(rows), curIdx, core.Command{ID: traits.SelectCmdMarkToggle})
	m.Marked = next.Marked
}

// ClearMarks clears all marks.
func (m *Model) ClearMarks() { m.Marked = nil }

// MarkedIDs returns marked node IDs in deterministic pre-order tree traversal order.
func (m *Model) MarkedIDs() []string {
	if len(m.Marked) == 0 {
		return nil
	}
	var out []string
	var walk func(ns []Node)
	walk = func(ns []Node) {
		for _, nd := range ns {
			if nd.ID != "" && m.Marked[nd.ID] {
				out = append(out, nd.ID)
			}
			if len(nd.Children) > 0 {
				walk(nd.Children)
			}
		}
	}
	walk(m.Nodes)
	return out
}

func (m *Model) markedRefs() []core.Ref {
	if len(m.Marked) == 0 {
		return nil
	}
	var out []core.Ref
	var walk func(ns []Node)
	walk = func(ns []Node) {
		for _, nd := range ns {
			if nd.ID != "" && m.Marked[nd.ID] && nd.Ref != nil {
				out = append(out, *nd.Ref)
			}
			if len(nd.Children) > 0 {
				walk(nd.Children)
			}
		}
	}
	walk(m.Nodes)
	return out
}

// ToggleVisual enters visual range selection anchored at the focused row, or exits it.
func (m *Model) ToggleVisual() {
	m.Clamp()
	rows := m.Flatten()
	curIdx := m.visibleIndex(rows)
	if curIdx < 0 {
		m.VisualActive = false
		return
	}
	next, _, _ := traits.SelectHandle(m.selectState(), rowVisibleIDs(rows), curIdx, core.Command{ID: traits.SelectCmdVisualToggle})
	m.applySelectState(next)
}

// ClearVisual exits visual range selection.
func (m *Model) ClearVisual() { m.VisualActive = false }

// VisualIDs returns the non-draft node IDs within the active visual range.
func (m *Model) VisualIDs() []string {
	m.Clamp()
	rows := m.Flatten()
	curIdx := m.visibleIndex(rows)
	if curIdx < 0 {
		return nil
	}
	return traits.SelectRangeIDs(m.selectState(), rowVisibleIDs(rows), curIdx)
}

// MarkVisual marks all non-draft nodes in the active visual range and exits visual mode.
func (m *Model) MarkVisual() {
	m.Clamp()
	rows := m.Flatten()
	curIdx := m.visibleIndex(rows)
	if curIdx < 0 {
		m.VisualActive = false
		return
	}
	next, _, _ := traits.SelectHandle(m.selectState(), rowVisibleIDs(rows), curIdx, core.Command{ID: traits.SelectCmdVisualMark})
	m.applySelectState(next)
}

// SelectedOrMarkedIDs returns the active visual range IDs if visual mode is
// active, else the marked IDs if any are marked, else the focused node ID.
func (m *Model) SelectedOrMarkedIDs() []string {
	if m.VisualActive {
		if ids := m.VisualIDs(); len(ids) > 0 {
			return ids
		}
	}
	if ids := m.MarkedIDs(); len(ids) > 0 {
		return ids
	}
	if row, ok := m.Focused(); ok && !row.Draft && row.ID != "" {
		return []string{row.ID}
	}
	return nil
}

// StepIn performs smart right/inward fold navigation:
//   - on a collapsed parent, unfolds it and returns true;
//   - on an expanded parent with a visible child at curIdx+1, moves cursor to
//     that first child and returns true;
//   - on a leaf (no children), returns false so callers may spill focus right.
func (m *Model) StepIn() bool {
	m.Clamp()
	rows := m.Flatten()
	curIdx := m.visibleIndex(rows)
	if curIdx < 0 || curIdx >= len(rows) {
		return false
	}
	row := rows[curIdx]
	if row.Draft || !row.HasChildren {
		return false
	}
	if row.Folded {
		return m.Unfold()
	}
	if curIdx+1 < len(rows) && !rows[curIdx+1].Draft && rows[curIdx+1].ParentID == row.ID {
		m.SelectedID = rows[curIdx+1].ID
		m.Clamp()
		return true
	}
	return false
}

// StepOut performs smart left/outward fold navigation:
//   - on an expanded parent (other than the active hoist root), folds it and returns true;
//   - on a collapsed parent or leaf with a visible parent (where parent is not
//     the active hoist root), moves cursor to that parent and returns true;
//   - on a root node that is already collapsed or a leaf root (or the active
//     hoist root), returns false so callers may spill focus left.
func (m *Model) StepOut() bool {
	m.Clamp()
	rows := m.Flatten()
	curIdx := m.visibleIndex(rows)
	if curIdx < 0 || curIdx >= len(rows) {
		return false
	}
	row := rows[curIdx]
	if row.Draft {
		return false
	}
	hoistID := m.HoistID()
	if row.HasChildren && !row.Folded && (hoistID == "" || row.ID != hoistID) {
		return m.Fold()
	}
	if row.ParentID != "" && row.ParentID != hoistID {
		for _, r := range rows {
			if !r.Draft && r.ID == row.ParentID {
				m.SelectedID = row.ParentID
				m.Clamp()
				return true
			}
		}
	}
	return false
}
