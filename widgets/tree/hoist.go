package tree

// Hoist command IDs handled by Model.HandleCommand.
const (
	CmdHoistIn    = "tree.hoist.in"
	CmdHoistOut   = "tree.hoist.out"
	CmdHoistClear = "tree.hoist.clear"
)

// Hoisted reports whether the tree is currently zoomed into a hoisted subtree.
func (m *Model) Hoisted() bool {
	return len(m.HoistStack) > 0
}

// HoistID returns the active hoisted node ID (top of HoistStack), or "" when not hoisted.
func (m *Model) HoistID() string {
	if len(m.HoistStack) == 0 {
		return ""
	}
	return m.HoistStack[len(m.HoistStack)-1]
}

// Hoist zooms the tree into the subtree rooted at id. It pushes id onto
// HoistStack, unfolds id so its children are immediately visible, preserves a
// valid selection inside the subtree (or selects id), and returns true if the
// hoist state changed.
func (m *Model) Hoist(id string) bool {
	if id == "" || id == m.HoistID() {
		return false
	}
	if _, _, _, _, ok := m.findNodeWithParent(id); !ok {
		return false
	}
	m.HoistStack = append(m.HoistStack, id)
	delete(m.Folded, id)
	m.Clamp()
	return true
}

// Unhoist pops the top node from HoistStack and keeps the previously hoisted
// node selected in the parent view. Returns true if the hoist state changed.
func (m *Model) Unhoist() bool {
	if len(m.HoistStack) == 0 {
		return false
	}
	prev := m.HoistStack[len(m.HoistStack)-1]
	m.HoistStack = m.HoistStack[:len(m.HoistStack)-1]
	if len(m.HoistStack) == 0 {
		m.HoistStack = nil
	}
	if top := m.HoistID(); top != "" {
		delete(m.Folded, top)
	}
	if path := pathIn(m.activeRoots(), prev); len(path) > 0 {
		for _, anc := range path[:len(path)-1] {
			delete(m.Folded, anc)
		}
		m.SelectedID = prev
	}
	m.Clamp()
	return true
}

// ClearHoist clears HoistStack and keeps the previously hoisted node selected
// in the full tree. Returns true if the hoist state changed.
func (m *Model) ClearHoist() bool {
	if len(m.HoistStack) == 0 {
		return false
	}
	prev := m.HoistStack[len(m.HoistStack)-1]
	m.HoistStack = nil
	if path := pathIn(m.Nodes, prev); len(path) > 0 {
		for _, anc := range path[:len(path)-1] {
			delete(m.Folded, anc)
		}
		m.SelectedID = prev
	}
	m.Clamp()
	return true
}

// HoistPath returns the ancestor node path from the top-level root to the
// active hoisted node (inclusive), or nil when not hoisted.
func (m *Model) HoistPath() []Node {
	id := m.HoistID()
	if id == "" {
		return nil
	}
	var path []Node
	var walk func(ns []Node) bool
	walk = func(ns []Node) bool {
		for _, nd := range ns {
			if nd.ID == id {
				path = append(path, nd)
				return true
			}
			if walk(nd.Children) {
				path = append([]Node{nd}, path...)
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

// ResetFoldInit clears the one-shot DefaultFolded initialization guard so the
// next non-empty SetNodes re-applies DefaultFolded.
func (m *Model) ResetFoldInit() {
	m.foldInitialized = false
}

// activeRoots returns the projected root slice: the single hoisted node when
// hoisted, or m.Nodes otherwise.
func (m *Model) activeRoots() []Node {
	if id := m.HoistID(); id != "" {
		if nd, _, _, _, ok := m.findNodeWithParent(id); ok {
			return []Node{*nd}
		}
	}
	return m.Nodes
}

func (m *Model) hoistLabel() string {
	if id := m.HoistID(); id != "" {
		if nd, _, _, _, ok := m.findNodeWithParent(id); ok {
			return nd.Label
		}
	}
	return ""
}

func (m *Model) pruneHoistStack(presentIDs []string) {
	if len(m.HoistStack) == 0 {
		return
	}
	present := make(map[string]bool, len(presentIDs))
	for _, id := range presentIDs {
		present[id] = true
	}
	kept := m.HoistStack[:0]
	for _, id := range m.HoistStack {
		if present[id] {
			kept = append(kept, id)
		}
	}
	if len(kept) == 0 {
		m.HoistStack = nil
	} else {
		m.HoistStack = kept
	}
}

func pathIn(ns []Node, id string) []string {
	if id == "" {
		return nil
	}
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
	if walk(ns) {
		return path
	}
	return nil
}
