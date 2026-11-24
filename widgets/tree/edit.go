package tree

import (
	"slices"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/widgets/input"
)

// EditKind identifies an inline edit operation on a tree model.
type EditKind string

// Inline edit kinds.
const (
	EditRename   EditKind = "rename"
	EditAddBelow EditKind = "add_below"
	EditAddAbove EditKind = "add_above"
	EditAddChild EditKind = "add_child"
)

// EditState holds the active inline edit session (rename or add).
type EditState struct {
	Kind     EditKind
	TargetID string // node being renamed, or anchor node for Add* ("" on empty tree)
	ParentID string // resolved parent ID for Add* (changes with EditIndent/EditOutdent)
	AfterID  string // sibling ID this new node is placed after ("" = first child/root)
	BeforeID string // sibling ID this new node is placed before (EditAddAbove)
	Depth    int    // visual depth of the edited/draft row
	Prefix   string // optional display prefix shown before the edit field
	Line     input.Line
}

// Value returns the current inline edit buffer text.
func (e EditState) Value() string { return e.Line.Value() }

// Cursor returns the current rune cursor offset in the edit buffer.
func (e EditState) Cursor() int { return e.Line.Cursor }

// EditResult describes the confirmed inline edit returned by CommitEdit.
type EditResult struct {
	Kind     EditKind
	TargetID string
	ParentID string
	AfterID  string
	BeforeID string
	Depth    int
	Value    string
}

// draftInsertIndex determines where the inline-add draft row sits within siblings.
func draftInsertIndex(siblings []Node, e *EditState) int {
	if e.BeforeID != "" {
		for i, nd := range siblings {
			if nd.ID == e.BeforeID {
				return i
			}
		}
	}
	if e.AfterID != "" {
		for i, nd := range siblings {
			if nd.ID == e.AfterID {
				return i + 1
			}
		}
	}
	return len(siblings)
}

func (m *Model) hasAddDraft() bool {
	return m.edit != nil && m.edit.Kind != EditRename
}

// Editing reports whether an inline rename or add session is active.
func (m *Model) Editing() bool { return m.edit != nil }

// Edit returns a copy of the active EditState, or nil when not editing.
func (m *Model) Edit() *EditState {
	if m.edit == nil {
		return nil
	}
	cp := *m.edit
	cp.Line.Text = slices.Clone(m.edit.Line.Text)
	return &cp
}

// StartRename starts an inline rename on the focused node with an optional display prefix.
func (m *Model) StartRename(prefix string) bool {
	row, ok := m.Focused()
	if !ok || row.Draft {
		return false
	}
	m.edit = &EditState{
		Kind:     EditRename,
		TargetID: row.ID,
		ParentID: row.ParentID,
		Depth:    row.Depth,
		Prefix:   prefix,
		Line:     input.NewLine(row.Node.Label),
	}
	m.Clamp()
	return true
}

// StartAdd starts an inline add (EditAddBelow, EditAddAbove, or EditAddChild)
// relative to the focused node, or at root level when the tree is empty.
//
// When hoisted and focused on the active HoistID root, EditAddBelow promotes to
// EditAddChild under the hoisted root so new items are created inside the
// hoisted view rather than as hidden siblings outside it.
func (m *Model) StartAdd(kind EditKind, prefix string) bool {
	switch kind {
	case EditAddBelow, EditAddAbove, EditAddChild:
	default:
		return false
	}
	row, ok := m.Focused()
	if !ok || row.Draft {
		m.edit = &EditState{
			Kind:   kind,
			Depth:  0,
			Prefix: prefix,
			Line:   input.NewLine(""),
		}
		m.Clamp()
		return true
	}
	onHoistRoot := m.Hoisted() && row.ID == m.HoistID()
	if onHoistRoot && kind == EditAddBelow {
		kind = EditAddChild
	}
	switch kind {
	case EditAddBelow:
		m.edit = &EditState{
			Kind:     EditAddBelow,
			TargetID: row.ID,
			ParentID: row.ParentID,
			AfterID:  row.ID,
			Depth:    row.Depth,
			Prefix:   prefix,
			Line:     input.NewLine(""),
		}
	case EditAddAbove:
		if onHoistRoot {
			delete(m.Folded, row.ID)
			beforeID := ""
			if len(row.Node.Children) > 0 {
				beforeID = row.Node.Children[0].ID
			}
			m.edit = &EditState{
				Kind:     EditAddChild,
				TargetID: row.ID,
				ParentID: row.ID,
				BeforeID: beforeID,
				Depth:    row.Depth + 1,
				Prefix:   prefix,
				Line:     input.NewLine(""),
			}
		} else {
			_, parentID, prevSib, _, _ := m.findNodeWithParent(row.ID)
			m.edit = &EditState{
				Kind:     EditAddAbove,
				TargetID: row.ID,
				ParentID: parentID,
				AfterID:  prevSib,
				BeforeID: row.ID,
				Depth:    row.Depth,
				Prefix:   prefix,
				Line:     input.NewLine(""),
			}
		}
	case EditAddChild:
		delete(m.Folded, row.ID)
		afterID := ""
		if len(row.Node.Children) > 0 {
			afterID = row.Node.Children[len(row.Node.Children)-1].ID
		}
		m.edit = &EditState{
			Kind:     EditAddChild,
			TargetID: row.ID,
			ParentID: row.ID,
			AfterID:  afterID,
			Depth:    row.Depth + 1,
			Prefix:   prefix,
			Line:     input.NewLine(""),
		}
	}
	m.Clamp()
	return true
}

// HandleEditCommand forwards an input/Readline command to the active inline editor.
func (m *Model) HandleEditCommand(cmd core.Command) bool {
	if m.edit == nil {
		return false
	}
	_, handled := m.edit.Line.HandleCommand(cmd)
	return handled
}

// EditInsertNewline inserts a literal newline at the cursor of the active inline edit.
func (m *Model) EditInsertNewline() bool {
	if m.edit == nil {
		return false
	}
	return m.edit.Line.Insert("\n")
}

// EditIndent demotes an inline-add draft under its preceding sibling.
func (m *Model) EditIndent() bool {
	if !m.hasAddDraft() || m.edit.AfterID == "" {
		return false
	}
	prev, _, _, _, ok := m.findNodeWithParent(m.edit.AfterID)
	if !ok {
		return false
	}
	delete(m.Folded, prev.ID)
	m.edit.ParentID = prev.ID
	m.edit.Depth++
	m.edit.BeforeID = ""
	if len(prev.Children) > 0 {
		m.edit.AfterID = prev.Children[len(prev.Children)-1].ID
	} else {
		m.edit.AfterID = ""
	}
	m.Clamp()
	return true
}

// EditOutdent promotes an inline-add draft up one level to follow its current parent.
// While hoisted, it clamps at depth 1 so a draft cannot outdent above HoistID.
func (m *Model) EditOutdent() bool {
	if !m.hasAddDraft() || m.edit.Depth <= 0 || m.edit.ParentID == "" {
		return false
	}
	if m.Hoisted() && (m.edit.ParentID == m.HoistID() || m.edit.Depth <= 1) {
		return false
	}
	parentID := m.edit.ParentID
	_, grandParentID, _, _, ok := m.findNodeWithParent(parentID)
	if !ok {
		return false
	}
	m.edit.ParentID = grandParentID
	m.edit.AfterID = parentID
	m.edit.BeforeID = ""
	m.edit.Depth--
	m.Clamp()
	return true
}

// CommitEdit ends the active inline edit and returns the resulting EditResult.
func (m *Model) CommitEdit() (EditResult, bool) {
	if m.edit == nil {
		return EditResult{}, false
	}
	e := m.edit
	res := EditResult{
		Kind:     e.Kind,
		TargetID: e.TargetID,
		ParentID: e.ParentID,
		AfterID:  e.AfterID,
		BeforeID: e.BeforeID,
		Depth:    e.Depth,
		Value:    e.Line.Value(),
	}
	m.edit = nil
	m.Clamp()
	return res, true
}

// CancelEdit aborts the active inline edit without producing a result.
func (m *Model) CancelEdit() bool {
	if m.edit == nil {
		return false
	}
	m.edit = nil
	m.Clamp()
	return true
}
