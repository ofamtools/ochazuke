package traits

import "ofam.dev/ochazuke/core"

// Standard fold verb IDs handled by FoldHandle.
const (
	FoldCmdToggle   = "fold.toggle"
	FoldCmdOpen     = "fold.open"
	FoldCmdClose    = "fold.close"
	FoldCmdOpenAll  = "fold.open.all"
	FoldCmdCloseAll = "fold.close.all"
)

// FoldConfig configures fold interaction policy.
type FoldConfig struct {
	LeafFoldsParent bool `json:"leafFoldsParent,omitempty"`
	DefaultFolded   bool `json:"defaultFolded,omitempty"`
}

// FoldTarget describes the currently focused item for a fold command.
type FoldTarget struct {
	ID          string
	ParentID    string
	HasChildren bool
}

// FoldState stores the set of collapsed item IDs.
type FoldState struct {
	Collapsed map[string]bool
}

// FoldInitial returns the initial FoldState for collapsibleIDs under cfg.
// When cfg.DefaultFolded is false, it returns an empty FoldState; when true,
// every non-empty ID in collapsibleIDs is marked collapsed.
func FoldInitial(cfg FoldConfig, collapsibleIDs []string) FoldState {
	if !cfg.DefaultFolded || len(collapsibleIDs) == 0 {
		return FoldState{}
	}
	m := make(map[string]bool, len(collapsibleIDs))
	for _, id := range collapsibleIDs {
		if id != "" {
			m[id] = true
		}
	}
	if len(m) == 0 {
		return FoldState{}
	}
	return FoldState{Collapsed: m}
}

func cloneCollapsed(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		if v {
			out[k] = true
		}
	}
	return out
}

func collapseCount(m map[string]bool) int {
	n := 0
	for _, v := range m {
		if v {
			n++
		}
	}
	return n
}

// FoldHandle applies one of the standard fold commands (fold.toggle,
// fold.open, fold.close, fold.open.all, fold.close.all) to st without mutating
// st.Collapsed in place. When LeafFoldsParent collapses a leaf's parent,
// focusID is set to target.ParentID so the caller can snap selection to it.
func FoldHandle(st FoldState, cfg FoldConfig, target FoldTarget, allCollapsible []string, cmd core.Command) (next FoldState, focusID string, changed, handled bool) {
	switch cmd.ID {
	case FoldCmdToggle:
		if target.HasChildren && target.ID != "" {
			m := cloneCollapsed(st.Collapsed)
			if m[target.ID] {
				delete(m, target.ID)
			} else {
				m[target.ID] = true
			}
			if len(m) == 0 {
				m = nil
			}
			return FoldState{Collapsed: m}, "", true, true
		}
		if cfg.LeafFoldsParent && target.ParentID != "" {
			if st.Collapsed[target.ParentID] {
				return st, target.ParentID, false, true
			}
			m := cloneCollapsed(st.Collapsed)
			m[target.ParentID] = true
			return FoldState{Collapsed: m}, target.ParentID, true, true
		}
		return st, "", false, true

	case FoldCmdOpen:
		if !target.HasChildren || target.ID == "" || !st.Collapsed[target.ID] {
			return st, "", false, true
		}
		m := cloneCollapsed(st.Collapsed)
		delete(m, target.ID)
		if len(m) == 0 {
			m = nil
		}
		return FoldState{Collapsed: m}, "", true, true

	case FoldCmdClose:
		if target.HasChildren && target.ID != "" {
			if st.Collapsed[target.ID] {
				return st, "", false, true
			}
			m := cloneCollapsed(st.Collapsed)
			m[target.ID] = true
			return FoldState{Collapsed: m}, "", true, true
		}
		if cfg.LeafFoldsParent && target.ParentID != "" {
			if st.Collapsed[target.ParentID] {
				return st, target.ParentID, false, true
			}
			m := cloneCollapsed(st.Collapsed)
			m[target.ParentID] = true
			return FoldState{Collapsed: m}, target.ParentID, true, true
		}
		return st, "", false, true

	case FoldCmdOpenAll:
		if collapseCount(st.Collapsed) == 0 {
			return FoldState{}, "", false, true
		}
		return FoldState{}, "", true, true

	case FoldCmdCloseAll:
		m := make(map[string]bool, len(allCollapsible))
		for _, id := range allCollapsible {
			if id != "" {
				m[id] = true
			}
		}
		if len(m) == 0 {
			return FoldState{}, "", collapseCount(st.Collapsed) > 0, true
		}
		diff := len(m) != collapseCount(st.Collapsed)
		if !diff {
			for id := range m {
				if !st.Collapsed[id] {
					diff = true
					break
				}
			}
		}
		return FoldState{Collapsed: m}, "", diff, true

	default:
		return st, "", false, false
	}
}

// FoldPrune returns a FoldState keeping only collapsed entries whose IDs exist
// in presentIDs, without mutating st.Collapsed in place.
func FoldPrune(st FoldState, presentIDs []string) FoldState {
	if len(st.Collapsed) == 0 {
		return FoldState{}
	}
	present := make(map[string]bool, len(presentIDs))
	for _, id := range presentIDs {
		present[id] = true
	}
	var out map[string]bool
	for id, v := range st.Collapsed {
		if v && present[id] {
			if out == nil {
				out = make(map[string]bool)
			}
			out[id] = true
		}
	}
	return FoldState{Collapsed: out}
}

// FoldFacts derives truthful read-only fold facts for target under st.
func FoldFacts(st FoldState, target FoldTarget) map[string]any {
	return map[string]any{
		"folded":      target.HasChildren && st.Collapsed[target.ID],
		"hasChildren": target.HasChildren,
		"foldCount":   collapseCount(st.Collapsed),
	}
}
