package traits

import "ofam.dev/ochazuke/core"

// Standard selection and visual-range verb IDs handled by SelectHandle.
const (
	SelectCmdMarkToggle   = "select.mark.toggle"
	SelectCmdMarkClear    = "select.mark.clear"
	SelectCmdVisualToggle = "select.visual.toggle"
	SelectCmdVisualLine   = "select.visual.line"
	SelectCmdVisualMark   = "select.visual.mark"
	SelectCmdVisualClear  = "select.visual.clear"
	SelectCmdAll          = "select.all"
	SelectCmdInvert       = "select.invert"
)

// SelectCommandSpecs returns the CommandSpecs for the canonical selection and
// visual-range commands handled by SelectHandle.
func SelectCommandSpecs() []core.CommandSpec {
	tags := []string{"select"}
	return []core.CommandSpec{
		{ID: SelectCmdMarkToggle, Description: "Toggle mark on focused item, or mark active visual range", Tags: tags},
		{ID: SelectCmdMarkClear, Description: "Clear all marks", Tags: tags},
		{ID: SelectCmdVisualToggle, Description: "Toggle visual range selection anchored at cursor", Tags: tags},
		{ID: SelectCmdVisualLine, Description: "Toggle line-wise visual range selection anchored at cursor", Tags: tags},
		{ID: SelectCmdVisualMark, Description: "Mark all items in the active visual range and exit visual mode", Tags: tags},
		{ID: SelectCmdVisualClear, Description: "Exit visual range selection", Tags: tags},
		{ID: SelectCmdAll, Description: "Mark all visible items", Tags: tags},
		{ID: SelectCmdInvert, Description: "Invert marks across all visible items", Tags: tags},
	}
}

// SelectState stores pure multi-select and visual-range selection state.
type SelectState struct {
	Marked       map[string]bool
	VisualActive bool
	VisualAnchor int
}

func cloneMarked(in map[string]bool) map[string]bool {
	var out map[string]bool
	for k, v := range in {
		if k != "" && v {
			if out == nil {
				out = make(map[string]bool, len(in))
			}
			out[k] = true
		}
	}
	return out
}

func countMarked(m map[string]bool) int {
	n := 0
	for k, v := range m {
		if k != "" && v {
			n++
		}
	}
	return n
}

// SelectBounds returns the inclusive visual selection range [from, to] clamped
// to [0, total-1]. It returns ok=false when visual mode is inactive or total<=0.
func SelectBounds(st SelectState, total, cursor int) (from, to int, ok bool) {
	if !st.VisualActive || total <= 0 {
		return 0, 0, false
	}
	a := max(0, min(st.VisualAnchor, total-1))
	c := max(0, min(cursor, total-1))
	return min(a, c), max(a, c), true
}

// SelectRangeIDs returns all non-empty IDs from visibleIDs within the active
// visual selection range. Returns nil when visual mode is inactive or empty.
func SelectRangeIDs(st SelectState, visibleIDs []string, cursor int) []string {
	from, to, ok := SelectBounds(st, len(visibleIDs), cursor)
	if !ok {
		return nil
	}
	var out []string
	for i := from; i <= to; i++ {
		if id := visibleIDs[i]; id != "" {
			out = append(out, id)
		}
	}
	return out
}

// SelectPrune returns a SelectState keeping only marked IDs present in
// presentIDs, without mutating st.Marked in place.
func SelectPrune(st SelectState, presentIDs []string) SelectState {
	next := SelectState{
		VisualActive: st.VisualActive,
		VisualAnchor: st.VisualAnchor,
	}
	if len(st.Marked) == 0 || len(presentIDs) == 0 {
		return next
	}
	present := make(map[string]bool, len(presentIDs))
	for _, id := range presentIDs {
		if id != "" {
			present[id] = true
		}
	}
	var out map[string]bool
	for id, v := range st.Marked {
		if v && id != "" && present[id] {
			if out == nil {
				out = make(map[string]bool)
			}
			out[id] = true
		}
	}
	next.Marked = out
	return next
}

// SelectHandle applies one of the standard selection commands to st without
// mutating st.Marked in place.
func SelectHandle(st SelectState, visibleIDs []string, cursor int, cmd core.Command) (next SelectState, changed, handled bool) {
	switch cmd.ID {
	case SelectCmdMarkToggle:
		if st.VisualActive {
			return markVisualRange(st, visibleIDs, cursor), true, true
		}
		if cursor < 0 || cursor >= len(visibleIDs) || visibleIDs[cursor] == "" {
			return st, false, true
		}
		id := visibleIDs[cursor]
		m := cloneMarked(st.Marked)
		if m[id] {
			delete(m, id)
			if len(m) == 0 {
				m = nil
			}
		} else {
			if m == nil {
				m = make(map[string]bool, 1)
			}
			m[id] = true
		}
		return SelectState{Marked: m, VisualActive: st.VisualActive, VisualAnchor: st.VisualAnchor}, true, true

	case SelectCmdMarkClear:
		if countMarked(st.Marked) == 0 {
			return SelectState{VisualActive: st.VisualActive, VisualAnchor: st.VisualAnchor}, false, true
		}
		return SelectState{VisualActive: st.VisualActive, VisualAnchor: st.VisualAnchor}, true, true

	case SelectCmdVisualToggle, SelectCmdVisualLine:
		if st.VisualActive {
			return SelectState{Marked: st.Marked, VisualActive: false, VisualAnchor: st.VisualAnchor}, true, true
		}
		if len(visibleIDs) == 0 {
			return st, false, true
		}
		anchor := max(0, min(cursor, len(visibleIDs)-1))
		return SelectState{Marked: st.Marked, VisualActive: true, VisualAnchor: anchor}, true, true

	case SelectCmdVisualMark:
		if !st.VisualActive {
			return st, false, true
		}
		return markVisualRange(st, visibleIDs, cursor), true, true

	case SelectCmdVisualClear:
		if !st.VisualActive {
			return st, false, true
		}
		return SelectState{Marked: st.Marked, VisualActive: false, VisualAnchor: st.VisualAnchor}, true, true

	case SelectCmdAll:
		m := cloneMarked(st.Marked)
		diff := false
		for _, id := range visibleIDs {
			if id == "" || m[id] {
				continue
			}
			if m == nil {
				m = make(map[string]bool, len(visibleIDs))
			}
			m[id] = true
			diff = true
		}
		if !diff {
			return st, false, true
		}
		return SelectState{Marked: m, VisualActive: st.VisualActive, VisualAnchor: st.VisualAnchor}, true, true

	case SelectCmdInvert:
		m := cloneMarked(st.Marked)
		seen := make(map[string]bool, len(visibleIDs))
		diff := false
		for _, id := range visibleIDs {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			diff = true
			if m[id] {
				delete(m, id)
			} else {
				if m == nil {
					m = make(map[string]bool, len(visibleIDs))
				}
				m[id] = true
			}
		}
		if !diff {
			return st, false, true
		}
		if len(m) == 0 {
			m = nil
		}
		return SelectState{Marked: m, VisualActive: st.VisualActive, VisualAnchor: st.VisualAnchor}, true, true

	default:
		return st, false, false
	}
}

func markVisualRange(st SelectState, visibleIDs []string, cursor int) SelectState {
	ids := SelectRangeIDs(st, visibleIDs, cursor)
	m := cloneMarked(st.Marked)
	for _, id := range ids {
		if m == nil {
			m = make(map[string]bool, len(ids))
		}
		m[id] = true
	}
	return SelectState{Marked: m, VisualActive: false, VisualAnchor: st.VisualAnchor}
}

// SelectFacts derives truthful read-only selection facts for cursor over
// visibleIDs under st.
func SelectFacts(st SelectState, visibleIDs []string, cursor int) map[string]any {
	focusedMarked := cursor >= 0 && cursor < len(visibleIDs) && visibleIDs[cursor] != "" && st.Marked[visibleIDs[cursor]]
	mc := countMarked(st.Marked)
	vc := len(SelectRangeIDs(st, visibleIDs, cursor))
	return map[string]any{
		"marked":       focusedMarked,
		"markedCount":  mc,
		"visualActive": st.VisualActive,
		"visualCount":  vc,
		"hasSelection": st.VisualActive || mc > 0,
	}
}
