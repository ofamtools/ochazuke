// Package tree is a pure, deterministic tree widget model: hierarchical nodes,
// folding, visual-row spacer viewport math, inline rename/add editing, and
// rendering to a ui.KindTree ViewNode.
//
// It performs no I/O and imports only the standard library, core, ui, and
// widgets/input.
package tree

import (
	"fmt"
	"hash/fnv"
	"slices"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
	"ofam.dev/ochazuke/ui"
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

// Command IDs handled by Model.HandleCommand besides standard motions.
const (
	CmdFoldToggle   = "tree.fold.toggle"
	CmdFoldOpen     = "tree.fold.open"
	CmdFoldClose    = "tree.fold.close"
	CmdFoldOpenAll  = "tree.fold.open.all"
	CmdFoldCloseAll = "tree.fold.close.all"
)

// Node is one domain tree item. Domain records never appear here; Ref and
// Facts carry identity and guard data.
type Node struct {
	ID       string
	Label    string
	Ref      *core.Ref
	Scope    string
	Tone     string
	Icon     string
	IconTone string
	Badge    string
	Badges   []ui.Badge
	Meta     []ui.TreeMeta
	Unread   bool
	Facts    map[string]any
	Children []Node
}

// VisibleRow is one flattened visible row in display order.
type VisibleRow struct {
	Node             Node
	ID               string
	ParentID         string
	Depth            int
	Index            int // 0-based logical index among visible rows
	VisualRow        int // 0-based visual row index accounting for GapTopLevel spacers
	HasChildren      bool
	Folded           bool
	HiddenDescendant int
	IsRoot           bool
	Draft            bool // true for a synthetic inline-add draft row
}

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

// Model is the tree widget state. Use pointer methods to mutate.
type Model struct {
	Nodes         []Node
	SelectedID    string
	Offset        int // visual row index (accounting for GapTopLevel spacers)
	PageSize      int
	ScrollOff     int
	Guides        bool
	GapTopLevel   bool
	ShowFoldCount bool
	Folded        map[string]bool

	edit *EditState
}

// New returns a Model over nodes with defaults (Guides, GapTopLevel, and
// ShowFoldCount enabled; PageSize 10).
func New(nodes []Node) *Model {
	m := &Model{
		Nodes:         nodes,
		PageSize:      10,
		Guides:        true,
		GapTopLevel:   true,
		ShowFoldCount: true,
	}
	if len(nodes) > 0 {
		m.SelectedID = nodes[0].ID
	}
	m.Clamp()
	return m
}

// Copy returns an independent copy (Folded map and active edit cloned; Nodes shared read-only).
func (m Model) Copy() Model {
	if m.Folded != nil {
		f := make(map[string]bool, len(m.Folded))
		for k, v := range m.Folded {
			f[k] = v
		}
		m.Folded = f
	}
	if m.edit != nil {
		cp := *m.edit
		cp.Line.Text = slices.Clone(m.edit.Line.Text)
		m.edit = &cp
	}
	return m
}

// SetNodes replaces the tree's nodes, drops fold entries for vanished nodes,
// and re-clamps selection and viewport offset.
func (m *Model) SetNodes(nodes []Node) {
	m.Nodes = nodes
	if len(m.Folded) > 0 {
		m.Folded = traits.FoldPrune(traits.FoldState{Collapsed: m.Folded}, collectNodeIDs(nodes, false)).Collapsed
	}
	m.Clamp()
}

func collectNodeIDs(ns []Node, onlyParents bool) []string {
	var ids []string
	var walk func(ns []Node)
	walk = func(ns []Node) {
		for _, nd := range ns {
			if !onlyParents || len(nd.Children) > 0 {
				ids = append(ids, nd.ID)
			}
			walk(nd.Children)
		}
	}
	walk(ns)
	return ids
}

func countDescendants(ns []Node) int {
	n := len(ns)
	for _, nd := range ns {
		n += countDescendants(nd.Children)
	}
	return n
}

func (m *Model) findNodeWithParent(id string) (node *Node, parentID, prevSiblingID string, depth int, ok bool) {
	var walk func(ns []Node, pID string, d int) bool
	walk = func(ns []Node, pID string, d int) bool {
		for i := range ns {
			if ns[i].ID == id {
				node = &ns[i]
				parentID = pID
				if i > 0 {
					prevSiblingID = ns[i-1].ID
				}
				depth = d
				return true
			}
			if walk(ns[i].Children, ns[i].ID, d+1) {
				return true
			}
		}
		return false
	}
	ok = walk(m.Nodes, "", 0)
	return
}

func (m *Model) pathTo(id string) []string {
	var path []string
	var walk func(ns []Node) bool
	walk = func(ns []Node) bool {
		for _, nd := range ns {
			if nd.ID == id {
				path = append(path, nd.ID)
				return true
			}
			if walk(nd.Children) {
				path = append([]string{nd.ID}, path...)
				return true
			}
		}
		return false
	}
	if walk(m.Nodes) {
		return path
	}
	return nil
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

// Flatten returns all currently visible rows in display order, including any
// active inline-add draft row and visual row indices accounting for GapTopLevel.
func (m *Model) Flatten() []VisibleRow {
	var out []VisibleRow
	visRow := 0
	emitDraft := func(parentID string, depth int, topIdx int) {
		if m.GapTopLevel && depth == 0 && topIdx > 0 {
			visRow++
		}
		out = append(out, VisibleRow{
			ParentID:  parentID,
			Depth:     depth,
			Index:     len(out),
			VisualRow: visRow,
			IsRoot:    depth == 0,
			Draft:     true,
		})
		visRow++
	}

	var walk func(ns []Node, parentID string, depth int)
	walk = func(ns []Node, parentID string, depth int) {
		draftHere := m.hasAddDraft() && m.edit.ParentID == parentID
		draftPos := -1
		if draftHere {
			draftPos = draftInsertIndex(ns, m.edit)
		}
		topIdx := 0
		for i, nd := range ns {
			if draftHere && i == draftPos {
				emitDraft(parentID, depth, topIdx)
				topIdx++
			}
			if m.GapTopLevel && depth == 0 && topIdx > 0 {
				visRow++
			}
			hasKids := len(nd.Children) > 0
			draftUnder := m.hasAddDraft() && m.edit.ParentID == nd.ID
			folded := hasKids && m.Folded[nd.ID] && !draftUnder
			hidden := 0
			if folded {
				hidden = countDescendants(nd.Children)
			}
			out = append(out, VisibleRow{
				Node:             nd,
				ID:               nd.ID,
				ParentID:         parentID,
				Depth:            depth,
				Index:            len(out),
				VisualRow:        visRow,
				HasChildren:      hasKids,
				Folded:           folded,
				HiddenDescendant: hidden,
				IsRoot:           depth == 0,
			})
			visRow++
			topIdx++
			if (hasKids && !folded) || draftUnder {
				walk(nd.Children, nd.ID, depth+1)
			}
		}
		if draftHere && draftPos >= len(ns) {
			emitDraft(parentID, depth, topIdx)
		}
	}
	walk(m.Nodes, "", 0)
	return out
}

func totalVisualRows(rows []VisibleRow) int {
	if len(rows) == 0 {
		return 0
	}
	return rows[len(rows)-1].VisualRow + 1
}

func (m *Model) viewportConfig() traits.ViewportConfig {
	return traits.ViewportConfig{ScrollOff: m.ScrollOff}
}

func (m *Model) visualViewport(cursor, total int) traits.ViewportState {
	return traits.ViewportState{Cursor: cursor, Offset: m.Offset, Total: total, Height: max(m.PageSize, 1)}
}

// Clamp ensures SelectedID refers to a visible row (when non-empty) and clamps
// Offset to keep the active row inside the viewport with ScrollOff padding.
func (m *Model) Clamp() {
	rows := m.Flatten()
	if len(rows) == 0 {
		m.SelectedID, m.Offset = "", 0
		return
	}
	selIdx := -1
	if m.hasAddDraft() {
		for i, r := range rows {
			if r.Draft {
				selIdx = i
				break
			}
		}
	}
	if selIdx < 0 {
		for i, r := range rows {
			if !r.Draft && r.ID == m.SelectedID {
				selIdx = i
				break
			}
		}
	}
	if selIdx < 0 {
		for i, r := range rows {
			if !r.Draft {
				selIdx, m.SelectedID = i, r.ID
				break
			}
		}
	}
	if selIdx < 0 {
		selIdx = 0
	}
	m.Offset = traits.ViewportClamp(m.visualViewport(rows[selIdx].VisualRow, totalVisualRows(rows)), m.viewportConfig()).Offset
}

// Focused returns the currently focused visible row (or false when empty).
func (m *Model) Focused() (VisibleRow, bool) {
	m.Clamp()
	rows := m.Flatten()
	if len(rows) == 0 {
		return VisibleRow{}, false
	}
	if m.hasAddDraft() {
		for _, r := range rows {
			if r.Draft {
				return r, true
			}
		}
	}
	for _, r := range rows {
		if !r.Draft && r.ID == m.SelectedID {
			return r, true
		}
	}
	return rows[0], true
}

// Select focuses the visible node with the given ID, returning false if not visible.
func (m *Model) Select(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range m.Flatten() {
		if !r.Draft && r.ID == id {
			m.SelectedID = id
			m.Clamp()
			return true
		}
	}
	return false
}

// Reveal unfolds any folded ancestors of id, selects id, and clamps the viewport.
// It returns false if id does not exist in Nodes.
func (m *Model) Reveal(id string) bool {
	path := m.pathTo(id)
	if len(path) == 0 {
		return false
	}
	for _, anc := range path[:len(path)-1] {
		delete(m.Folded, anc)
	}
	m.SelectedID = id
	m.Clamp()
	return true
}

func (m *Model) applyFold(cmdID string) bool {
	var target traits.FoldTarget
	if row, ok := m.Focused(); ok && !row.Draft {
		target = traits.FoldTarget{ID: row.ID, ParentID: row.ParentID, HasChildren: row.HasChildren}
	}
	var all []string
	if cmdID == traits.FoldCmdCloseAll {
		all = collectNodeIDs(m.Nodes, true)
	}
	next, focusID, changed, _ := traits.FoldHandle(
		traits.FoldState{Collapsed: m.Folded},
		traits.FoldConfig{LeafFoldsParent: true},
		target, all, core.Command{ID: cmdID},
	)
	m.Folded = next.Collapsed
	if focusID != "" {
		m.SelectedID = focusID
	} else if cmdID == traits.FoldCmdCloseAll {
		if path := m.pathTo(m.SelectedID); len(path) > 0 {
			m.SelectedID = path[0]
		}
	}
	if changed || cmdID == traits.FoldCmdCloseAll || cmdID == traits.FoldCmdOpenAll {
		m.Clamp()
	}
	return changed
}

// ToggleFold flips fold state of the focused node. On a leaf node with a parent,
// it folds the parent and moves selection to the parent.
func (m *Model) ToggleFold() bool { return m.applyFold(traits.FoldCmdToggle) }

// Fold collapses the focused node. On a leaf node with a parent, it collapses
// the parent and moves selection to the parent.
func (m *Model) Fold() bool { return m.applyFold(traits.FoldCmdClose) }

// Unfold expands the focused node if it is folded.
func (m *Model) Unfold() bool { return m.applyFold(traits.FoldCmdOpen) }

// FoldAll collapses every node with children, snapping selection to the
// nearest visible ancestor if needed.
func (m *Model) FoldAll() { m.applyFold(traits.FoldCmdCloseAll) }

// UnfoldAll expands every node in the tree.
func (m *Model) UnfoldAll() { m.applyFold(traits.FoldCmdOpenAll) }

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
		_, parentID, prevSib, depth, _ := m.findNodeWithParent(row.ID)
		m.edit = &EditState{
			Kind:     EditAddAbove,
			TargetID: row.ID,
			ParentID: parentID,
			AfterID:  prevSib,
			BeforeID: row.ID,
			Depth:    depth,
			Prefix:   prefix,
			Line:     input.NewLine(""),
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
func (m *Model) EditOutdent() bool {
	if !m.hasAddDraft() || m.edit.Depth <= 0 || m.edit.ParentID == "" {
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

// HandleCommand applies a standard motion or tree fold command, reporting
// whether it was handled.
func (m *Model) HandleCommand(cmd core.Command) bool {
	n := cmd.Times()
	m.Clamp()
	rows := m.Flatten()
	ps := max(m.PageSize, 1)

	curIdx := 0
	for i, r := range rows {
		if !r.Draft && r.ID == m.SelectedID {
			curIdx = i
			break
		}
	}
	last := len(rows) - 1
	moveToIdx := func(to int) {
		if len(rows) == 0 {
			return
		}
		to = max(0, min(to, last))
		m.SelectedID = rows[to].ID
		m.Clamp()
	}

	if cmd.ID == "motion.scroll.down" || cmd.ID == "motion.scroll.up" {
		m.scrollVisual(rows, curIdx, ps, cmd)
		return true
	}
	if next, _, handled := traits.ViewportStep(traits.ViewportState{Cursor: curIdx, Total: len(rows), Height: ps}, m.viewportConfig(), cmd); handled {
		moveToIdx(next.Cursor)
		return true
	}

	switch cmd.ID {
	case "motion.block.next":
		idx := curIdx
		for ; n > 0; n-- {
			for i := idx + 1; i <= last; i++ {
				if rows[i].IsRoot {
					idx = i
					break
				}
			}
		}
		moveToIdx(idx)
	case "motion.block.prev":
		idx := curIdx
		for ; n > 0; n-- {
			from := idx
			if from >= 0 && from <= last && rows[from].IsRoot {
				from--
			}
			for i := from; i >= 0; i-- {
				if rows[i].IsRoot {
					idx = i
					break
				}
			}
		}
		moveToIdx(idx)
	case CmdFoldToggle, traits.FoldCmdToggle:
		m.ToggleFold()
	case CmdFoldOpen, traits.FoldCmdOpen:
		m.Unfold()
	case CmdFoldClose, traits.FoldCmdClose:
		m.Fold()
	case CmdFoldOpenAll, traits.FoldCmdOpenAll:
		m.UnfoldAll()
	case CmdFoldCloseAll, traits.FoldCmdCloseAll:
		m.FoldAll()
	default:
		return false
	}
	return true
}

func (m *Model) scrollVisual(rows []VisibleRow, curIdx, ps int, cmd core.Command) {
	if len(rows) == 0 {
		return
	}
	totalVis := totalVisualRows(rows)
	next, _, _ := traits.ViewportStep(traits.ViewportState{Cursor: rows[curIdx].VisualRow, Offset: m.Offset, Total: totalVis, Height: ps}, m.viewportConfig(), cmd)
	m.Offset = next.Offset
	targetVis := next.Cursor

	// Resolve targetVis to a visible row; if targetVis lands on a GapTopLevel
	// spacer, break the tie in the scroll direction.
	picked := -1
	for i, r := range rows {
		if r.VisualRow == targetVis {
			picked = i
			break
		}
	}
	if picked < 0 {
		if cmd.ID == "motion.scroll.down" {
			picked = len(rows) - 1
			for i, r := range rows {
				if r.VisualRow > targetVis {
					picked = i
					break
				}
			}
		} else {
			picked = 0
			for i := len(rows) - 1; i >= 0; i-- {
				if rows[i].VisualRow < targetVis {
					picked = i
					break
				}
			}
		}
	}
	m.SelectedID = rows[picked].ID
	if rv := rows[picked].VisualRow; rv < m.Offset || rv >= m.Offset+ps {
		m.Offset = traits.ViewportClamp(traits.ViewportState{Cursor: rv, Offset: m.Offset, Total: totalVis, Height: ps}, m.viewportConfig()).Offset
	}
}

func (m *Model) facts(rows []VisibleRow, row VisibleRow, ok bool) map[string]any {
	rowCount, selVis := 0, 0
	for _, r := range rows {
		if !r.Draft {
			rowCount++
		}
	}
	if ok {
		selVis = row.VisualRow
	}
	f := traits.MergeFacts(traits.ViewportFacts(m.visualViewport(selVis, totalVisualRows(rows)), m.viewportConfig()), map[string]any{
		"isTree":    true,
		"rowCount":  rowCount,
		"rootCount": len(m.Nodes),
		"editing":   m.Editing(),
	})
	if ok && !row.Draft {
		f = traits.MergeFacts(f, traits.FoldFacts(traits.FoldState{Collapsed: m.Folded}, traits.FoldTarget{
			ID: row.ID, ParentID: row.ParentID, HasChildren: row.HasChildren,
		}))
		f["depth"] = row.Depth
		f["folded"] = row.Folded
		f["isRoot"] = row.IsRoot
		for k, v := range row.Node.Facts {
			if _, dup := f[k]; !dup {
				f[k] = v
			}
		}
	}
	return f
}

// ContextFrame describes the focused tree node for the modal context chain.
func (m *Model) ContextFrame(scope, nodeID string) core.ContextFrame {
	row, ok := m.Focused()
	rows := m.Flatten()
	cf := core.ContextFrame{
		Scope:  scope,
		NodeID: nodeID,
		Facts:  m.facts(rows, row, ok),
	}
	if ok && !row.Draft {
		if row.Node.Scope != "" && scope == "" {
			cf.Scope = row.Node.Scope
		}
		if row.Node.Ref != nil {
			r := *row.Node.Ref
			cf.Subject = &r
		}
	}
	return cf
}

func (m *Model) buildUINodes(ns []Node, parentID string) []ui.TreeNode {
	draftHere := m.hasAddDraft() && m.edit.ParentID == parentID
	draftPos := -1
	if draftHere {
		draftPos = draftInsertIndex(ns, m.edit)
	}
	capN := len(ns)
	if draftHere {
		capN++
	}
	out := make([]ui.TreeNode, 0, capN)
	makeDraft := func() ui.TreeNode {
		return ui.TreeNode{
			Editing:    true,
			EditPrefix: m.edit.Prefix,
			EditValue:  m.edit.Line.Value(),
			EditCursor: m.edit.Line.Cursor,
		}
	}

	for i, nd := range ns {
		if draftHere && i == draftPos {
			out = append(out, makeDraft())
		}
		draftUnder := m.hasAddDraft() && m.edit.ParentID == nd.ID
		folded := len(nd.Children) > 0 && m.Folded[nd.ID] && !draftUnder
		var expPtr *bool
		if len(nd.Children) > 0 || draftUnder {
			exp := !folded
			expPtr = &exp
		}
		meta := slices.Clone(nd.Meta)
		if m.ShowFoldCount && folded {
			meta = append(meta, ui.TreeMeta{
				Text: fmt.Sprintf("(+%d)", countDescendants(nd.Children)),
				Tone: "muted",
			})
		}
		tn := ui.TreeNode{
			Label:    nd.Label,
			Expanded: expPtr,
			Tone:     nd.Tone,
			Icon:     nd.Icon,
			IconTone: nd.IconTone,
			Badge:    nd.Badge,
			Badges:   slices.Clone(nd.Badges),
			Meta:     meta,
			Unread:   nd.Unread,
		}
		if m.edit != nil && m.edit.Kind == EditRename && m.edit.TargetID == nd.ID {
			tn.Editing = true
			tn.EditPrefix = m.edit.Prefix
			tn.EditValue = m.edit.Line.Value()
			tn.EditCursor = m.edit.Line.Cursor
		}
		if len(nd.Children) > 0 || draftUnder {
			tn.Children = m.buildUINodes(nd.Children, nd.ID)
		}
		out = append(out, tn)
	}
	if draftHere && draftPos >= len(ns) {
		out = append(out, makeDraft())
	}
	return out
}

func hashUINodes(h interface{ Write([]byte) (int, error) }, ns []ui.TreeNode) {
	fmt.Fprintf(h, "[%d:", len(ns))
	for _, nd := range ns {
		exp := true
		if nd.Expanded != nil {
			exp = *nd.Expanded
		}
		fmt.Fprintf(h, "{%q|%t|%q|%q|%q|%q|%t|%t|%q|%q|%d|",
			nd.Label, exp, nd.Tone, nd.Icon, nd.IconTone, nd.Badge,
			nd.Unread, nd.Editing, nd.EditPrefix, nd.EditValue, nd.EditCursor)
		for _, b := range nd.Badges {
			fmt.Fprintf(h, "B(%q,%q)", b.Text, b.Tone)
		}
		for _, mt := range nd.Meta {
			fmt.Fprintf(h, "M(%q,%q)", mt.Text, mt.Tone)
		}
		if len(nd.Children) > 0 {
			hashUINodes(h, nd.Children)
		}
		fmt.Fprint(h, "}")
	}
	fmt.Fprint(h, "]")
}

// View renders the model as a ui.KindTree ViewNode.
func (m *Model) View(id string) ui.ViewNode {
	m.Clamp()
	rows := m.Flatten()
	selIdx := -1
	if m.hasAddDraft() {
		for _, r := range rows {
			if r.Draft {
				selIdx = r.Index
				break
			}
		}
	} else {
		for _, r := range rows {
			if !r.Draft && r.ID == m.SelectedID {
				selIdx = r.Index
				break
			}
		}
	}
	uiNodes := m.buildUINodes(m.Nodes, "")

	props := map[string]any{
		"nodes":       uiNodes,
		"guides":      m.Guides,
		"gapTopLevel": m.GapTopLevel,
		"selected":    selIdx,
		"offset":      m.Offset,
	}
	n := ui.ViewNode{
		ID:        id,
		Kind:      ui.KindTree,
		Focusable: true,
		Props:     props,
	}
	cf := m.ContextFrame("", id)
	n.Scope, n.Ref, n.Facts = cf.Scope, cf.Subject, cf.Facts
	if row, ok := m.Focused(); ok && !row.Draft {
		n.Unread, n.Badges = row.Node.Unread, row.Node.Badges
	}
	h := fnv.New64a()
	fmt.Fprintf(h, "%t|%t|%t|%d|%d|", m.Guides, m.GapTopLevel, m.ShowFoldCount, selIdx, m.Offset)
	hashUINodes(h, uiNodes)
	n.CacheKey = h.Sum64() | 1
	return n
}
