// Package tree is a pure, deterministic tree widget model: hierarchical nodes,
// folding, visual-row spacer viewport math, inline rename/add editing, and
// rendering to a ui.KindTree ViewNode.
//
// It performs no I/O and imports only the standard library, core, ui, and
// widgets/input.
package tree

import (
	"fmt"
	"slices"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
	"ofam.dev/ochazuke/ui"
)

// Command IDs handled by Model.HandleCommand besides standard motions.
const (
	CmdFoldToggle   = "tree.fold.toggle"
	CmdFoldOpen     = "tree.fold.open"
	CmdFoldClose    = "tree.fold.close"
	CmdFoldOpenAll  = "tree.fold.open.all"
	CmdFoldCloseAll = "tree.fold.close.all"
	CmdFoldOrStepIn = "tree.fold.in"
	CmdFoldOrParent = "tree.fold.out"
)

// Specs returns the CommandSpecs for tree-specific fold and hoist commands.
func Specs() []core.CommandSpec {
	tags := []string{"tree"}
	return []core.CommandSpec{
		{ID: CmdFoldToggle, Description: "Toggle fold on the focused tree node", Tags: tags},
		{ID: CmdFoldOpen, Description: "Expand the focused tree node", Tags: tags},
		{ID: CmdFoldClose, Description: "Collapse the focused tree node", Tags: tags},
		{ID: CmdFoldOpenAll, Description: "Expand all tree nodes", Tags: tags},
		{ID: CmdFoldCloseAll, Description: "Collapse all tree nodes", Tags: tags},
		{ID: CmdFoldOrStepIn, Description: "Unfold collapsed node or step into first child", Tags: tags},
		{ID: CmdFoldOrParent, Description: "Fold expanded node or step to parent", Tags: tags},
		{
			ID:          CmdHoistIn,
			Description: "Zoom into the focused tree node subtree",
			Tags:        tags,
			Params:      []core.ParamSpec{{Name: "id", Type: core.ParamString, Description: "node ID to hoist (default: focused node)"}},
		},
		{ID: CmdHoistOut, Description: "Zoom out one level from the hoisted subtree", Tags: tags},
		{ID: CmdHoistClear, Description: "Clear all hoisting and return to the full tree", Tags: tags},
	}
}

// Node is one domain tree item. Domain records never appear here; Ref and
// Facts carry identity and guard data.
type Node struct {
	ID         string
	Label      string
	LabelSpans []ui.ToneSpan
	LabelRuns  []ui.InlineRun
	Ref        *core.Ref
	Scope      string
	Tone       string
	Gutter     string
	GutterTone string
	Icon       string
	IconTone   string
	Badge      string
	Badges     []ui.Badge
	Meta       []ui.TreeMeta
	Unread     bool
	Facts      map[string]any
	SearchText string // optional preformatted FilterableText (e.g. "key:value\n...")
	Children   []Node
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
	ShowGutter    bool
	Wrap          bool
	Folded        map[string]bool
	Marked        map[string]bool
	VisualActive  bool
	VisualAnchor  int
	HoistStack    []string
	DefaultFolded bool
	Query         string
	MatchMode     traits.MatchMode
	FilterErr     string

	// MotionSelection is the range the last motion.object.select reported, in
	// MotionNodes indices (nil when the focused row has no such object).
	MotionSelection *traits.ObjectRange

	foldInitialized bool
	filter          compiledFilter
	edit            *EditState
	viewCache       *treeViewCache
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

// Copy returns an independent copy (Folded and Marked maps, HoistStack, and active edit cloned; Nodes shared read-only).
func (m Model) Copy() Model {
	if m.Folded != nil {
		f := make(map[string]bool, len(m.Folded))
		for k, v := range m.Folded {
			f[k] = v
		}
		m.Folded = f
	}
	if m.Marked != nil {
		mk := make(map[string]bool, len(m.Marked))
		for k, v := range m.Marked {
			mk[k] = v
		}
		m.Marked = mk
	}
	m.HoistStack = slices.Clone(m.HoistStack)
	if m.edit != nil {
		cp := *m.edit
		cp.Line.Text = slices.Clone(m.edit.Line.Text)
		m.edit = &cp
	}
	m.viewCache = nil
	return m
}

// SetNodes replaces the tree's nodes, initializes DefaultFolded on first
// non-empty load (or prunes fold entries for vanished nodes thereafter), prunes
// vanished marks and hoist targets, keeps the active hoisted root unfolded, and
// re-clamps selection and viewport offset.
func (m *Model) SetNodes(nodes []Node) {
	m.Nodes = nodes
	allIDs := collectNodeIDs(nodes, false)
	if m.DefaultFolded && !m.foldInitialized && len(nodes) > 0 {
		m.Folded = traits.FoldInitial(
			traits.FoldConfig{DefaultFolded: true},
			collectNodeIDs(nodes, true),
		).Collapsed
		m.foldInitialized = true
	} else if len(m.Folded) > 0 {
		m.Folded = traits.FoldPrune(traits.FoldState{Collapsed: m.Folded}, allIDs).Collapsed
	}
	if len(m.Marked) > 0 {
		m.Marked = traits.SelectPrune(m.selectState(), allIDs).Marked
	}
	m.pruneHoistStack(allIDs)
	if id := m.HoistID(); id != "" {
		delete(m.Folded, id)
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

// Flatten returns all currently visible rows in display order, including any
// active inline-add draft row and visual row indices accounting for GapTopLevel.
// Flatten is a pure projection and never mutates m.
func (m *Model) Flatten() []VisibleRow {
	rows, _ := m.cachedRows(m.filteredRoots(), m.FilterError())
	return rows
}

func (m *Model) flattenRoots(roots []Node) []VisibleRow {
	var out []VisibleRow
	visRow := 0
	hoistID := m.HoistID()
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
			isHoistRoot := hoistID != "" && depth == 0 && nd.ID == hoistID
			folded := hasKids && m.Folded[nd.ID] && !draftUnder && !isHoistRoot && !m.IsFiltered()
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
	walk(roots, "", 0)
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
	return m.visualViewportAt(cursor, m.Offset, total)
}

func (m *Model) visualViewportAt(cursor, offset, total int) traits.ViewportState {
	return traits.ViewportState{Cursor: cursor, Offset: offset, Total: total, Height: max(m.PageSize, 1)}
}

// projectSelection computes the effective focused row index, ID, and clamped
// viewport offset for rows without mutating m.
func (m *Model) projectSelection(rows []VisibleRow) (int, string, int) {
	if len(rows) == 0 {
		return -1, "", 0
	}
	selIdx := -1
	selID := m.SelectedID
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
			if !r.Draft && r.ID == selID {
				selIdx = i
				break
			}
		}
	}
	if selIdx < 0 {
		for i, r := range rows {
			if !r.Draft {
				selIdx, selID = i, r.ID
				break
			}
		}
	}
	if selIdx < 0 {
		selIdx = 0
	}
	offset := traits.ViewportClamp(m.visualViewport(rows[selIdx].VisualRow, totalVisualRows(rows)), m.viewportConfig()).Offset
	return selIdx, selID, offset
}

// Clamp ensures SelectedID refers to a visible row (when non-empty) and clamps
// Offset to keep the active row inside the viewport with ScrollOff padding.
func (m *Model) Clamp() {
	rows := m.Flatten()
	selIdx, selID, offset := m.projectSelection(rows)
	if selIdx < 0 {
		m.SelectedID, m.Offset = "", 0
		return
	}
	m.SelectedID, m.Offset = selID, offset
}

// Focused returns the currently focused visible row (or false when empty)
// without mutating m.
func (m *Model) Focused() (VisibleRow, bool) {
	rows := m.Flatten()
	selIdx, _, _ := m.projectSelection(rows)
	if selIdx < 0 {
		return VisibleRow{}, false
	}
	return rows[selIdx], true
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
// It returns false if id does not exist in the active view.
func (m *Model) Reveal(id string) bool {
	path := pathIn(m.activeRoots(), id)
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
	hoistID := m.HoistID()
	var target traits.FoldTarget
	if row, ok := m.Focused(); ok && !row.Draft {
		target = traits.FoldTarget{ID: row.ID, ParentID: row.ParentID, HasChildren: row.HasChildren}
	}
	if hoistID != "" && target.ID == hoistID && (cmdID == traits.FoldCmdToggle || cmdID == traits.FoldCmdClose) {
		return false
	}
	var all []string
	if cmdID == traits.FoldCmdCloseAll {
		for _, id := range collectNodeIDs(m.activeRoots(), true) {
			if id != hoistID {
				all = append(all, id)
			}
		}
	}
	next, focusID, changed, _ := traits.FoldHandle(
		traits.FoldState{Collapsed: m.Folded},
		traits.FoldConfig{LeafFoldsParent: hoistID == "" || target.ParentID != hoistID},
		target, all, core.Command{ID: cmdID},
	)
	m.Folded = next.Collapsed
	if hoistID != "" {
		delete(m.Folded, hoistID)
	}
	if focusID != "" {
		m.SelectedID = focusID
	} else if cmdID == traits.FoldCmdCloseAll {
		if path := pathIn(m.activeRoots(), m.SelectedID); len(path) > 0 {
			if hoistID != "" && len(path) > 1 {
				m.SelectedID = path[1]
			} else {
				m.SelectedID = path[0]
			}
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

// HandleCommand applies a standard motion or tree fold command, reporting
// whether it was handled.
func (m *Model) HandleCommand(cmd core.Command) bool {
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
	if next, _, handled := traits.SelectHandle(m.selectState(), rowVisibleIDs(rows), curIdx, cmd); handled {
		m.applySelectState(next)
		return true
	}
	if m.stepMotion(rows, curIdx, moveToIdx, cmd) {
		return true
	}

	switch cmd.ID {
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
	case CmdFoldOrStepIn:
		return m.StepIn()
	case CmdFoldOrParent:
		return m.StepOut()
	case "esc":
		if !m.VisualActive {
			return false
		}
		m.ClearVisual()
	case CmdHoistIn:
		if id, ok := cmd.Args["id"].(string); ok && id != "" {
			m.Hoist(id)
		} else if row, ok := m.Focused(); ok && !row.Draft {
			m.Hoist(row.ID)
		}
	case CmdHoistOut:
		m.Unhoist()
	case CmdHoistClear:
		m.ClearHoist()
	case "table.filter.mode.cycle":
		m.CycleMatchMode()
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

func (m *Model) facts(rows []VisibleRow, row VisibleRow, ok bool, curIdx, offset int) map[string]any {
	rowCount, selVis := 0, 0
	for _, r := range rows {
		if !r.Draft {
			rowCount++
		}
	}
	if ok {
		selVis = row.VisualRow
	}
	f := traits.MergeFacts(traits.ViewportFacts(m.visualViewportAt(selVis, offset, totalVisualRows(rows)), m.viewportConfig()), map[string]any{
		"isTree":     true,
		"rowCount":   rowCount,
		"rootCount":  len(m.Nodes),
		"editing":    m.Editing(),
		"hoisted":    m.Hoisted(),
		"hoistDepth": len(m.HoistStack),
		"hoistLabel": m.hoistLabel(),
	})
	f = traits.MergeFacts(f, traits.FilterFacts(traits.FilterState{
		Query: m.Query,
		Mode:  m.MatchMode,
		Err:   m.FilterError(),
	}, traits.FilterConfig{}))
	f = traits.MergeFacts(f, traits.SelectFacts(m.selectState(), rowVisibleIDs(rows), curIdx))
	if ok && !row.Draft {
		f = traits.MergeFacts(f, traits.FoldFacts(traits.FoldState{Collapsed: m.Folded}, traits.FoldTarget{
			ID: row.ID, ParentID: row.ParentID, HasChildren: row.HasChildren,
		}))
		f["depth"] = row.Depth
		f["folded"] = row.Folded
		f["hasParent"] = row.ParentID != "" && row.ParentID != m.HoistID()
		f["isRoot"] = row.IsRoot
		for k, v := range row.Node.Facts {
			if _, dup := f[k]; !dup {
				f[k] = v
			}
		}
	} else {
		f = traits.MergeFacts(f, traits.FoldFacts(traits.FoldState{Collapsed: m.Folded}, traits.FoldTarget{}))
		f["hasParent"] = false
	}
	return f
}

// ContextFrame describes the focused tree node for the modal context chain
// without mutating m.
func (m *Model) ContextFrame(scope, nodeID string) core.ContextFrame {
	rows := m.Flatten()
	selIdx, _, offset := m.projectSelection(rows)
	return m.contextFrameFor(rows, selIdx, offset, scope, nodeID)
}

func (m *Model) contextFrameFor(rows []VisibleRow, selIdx, offset int, scope, nodeID string) core.ContextFrame {
	var row VisibleRow
	ok := selIdx >= 0 && selIdx < len(rows)
	if ok {
		row = rows[selIdx]
	}
	cf := core.ContextFrame{
		Scope:  scope,
		NodeID: nodeID,
		Facts:  m.facts(rows, row, ok, selIdx, offset),
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
	cf.Subjects = m.markedRefs()
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

	hoistID := m.HoistID()
	for i, nd := range ns {
		if draftHere && i == draftPos {
			out = append(out, makeDraft())
		}
		draftUnder := m.hasAddDraft() && m.edit.ParentID == nd.ID
		isHoistRoot := hoistID != "" && parentID == "" && nd.ID == hoistID
		folded := len(nd.Children) > 0 && m.Folded[nd.ID] && !draftUnder && !isHoistRoot && !m.IsFiltered()
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
			Label:      nd.Label,
			LabelSpans: slices.Clone(nd.LabelSpans),
			LabelRuns:  slices.Clone(nd.LabelRuns),
			Expanded:   expPtr,
			Tone:       nd.Tone,
			Gutter:     nd.Gutter,
			GutterTone: nd.GutterTone,
			Icon:       nd.Icon,
			IconTone:   nd.IconTone,
			Badge:      nd.Badge,
			Badges:     slices.Clone(nd.Badges),
			Meta:       meta,
			Unread:     nd.Unread,
		}
		if m.Marked[nd.ID] {
			const markPrefix = "● "
			shift := len([]rune(markPrefix))
			if len(tn.LabelRuns) > 0 {
				prefixed := make([]ui.InlineRun, 0, len(tn.LabelRuns)+1)
				prefixed = append(prefixed, ui.InlineRun{Text: markPrefix, Tone: "accent"})
				prefixed = append(prefixed, tn.LabelRuns...)
				tn.LabelRuns = prefixed
			} else {
				tn.Label = markPrefix + tn.Label
			}
			if len(tn.LabelSpans) > 0 {
				shifted := make([]ui.ToneSpan, len(tn.LabelSpans))
				for si, sp := range tn.LabelSpans {
					sp.From += shift
					if sp.To > 0 {
						sp.To += shift
					}
					shifted[si] = sp
				}
				tn.LabelSpans = shifted
			}
			if len(tn.LabelRuns) == 0 {
				tn.LabelSpans = append([]ui.ToneSpan{{From: 0, To: shift, Tone: "accent"}}, tn.LabelSpans...)
			}
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

// View renders the model as a ui.KindTree ViewNode purely without mutating m.
func (m *Model) View(id string) ui.ViewNode {
	roots := m.filteredRoots()
	filterErr := m.FilterError()
	rows, uiNodes, contentHash := m.cachedViewData(roots, filterErr)
	selIdx, _, offset := m.projectSelection(rows)

	props := map[string]any{
		"nodes":       uiNodes,
		"guides":      m.Guides,
		"gapTopLevel": m.GapTopLevel,
		"gutter":      m.ShowGutter,
		"wrap":        m.Wrap,
		"selected":    selIdx,
		"offset":      offset,
	}
	vFrom, vTo, vOK := m.visualBounds(rows, selIdx)
	if vOK {
		props["visualFrom"] = vFrom
		props["visualTo"] = vTo
	}
	n := ui.ViewNode{
		ID:        id,
		Kind:      ui.KindTree,
		Traits:    []string{traits.TraitNav, traits.TraitScroll, traits.TraitFilter, traits.TraitSelect, traits.TraitFold},
		Focusable: true,
		Props:     props,
	}
	cf := m.contextFrameFor(rows, selIdx, offset, "", id)
	n.Scope, n.Ref, n.Facts = cf.Scope, cf.Subject, cf.Facts
	if selIdx >= 0 && selIdx < len(rows) && !rows[selIdx].Draft {
		n.Unread, n.Badges = rows[selIdx].Node.Unread, rows[selIdx].Node.Badges
	}
	n.CacheKey = combineViewCacheKey(contentHash, selIdx, offset, vOK, vFrom, vTo, m.VisualActive, m.VisualAnchor)
	return n
}
