package modal

import (
	"slices"

	"ofam.dev/ochazuke/core"
)

// Stack maintains the active modal overlays and keeps core.State.Modals
// synchronized 1:1 using copy-on-write state transitions.
type Stack struct {
	modals []Modal
}

// NewStack returns an empty modal Stack.
func NewStack() *Stack {
	return &Stack{}
}

// Len returns the number of active modals.
func (st *Stack) Len() int {
	if st == nil {
		return 0
	}
	return len(st.modals)
}

// Empty reports whether the stack has no modals.
func (st *Stack) Empty() bool {
	return st.Len() == 0
}

// Top returns the topmost modal, or nil when empty.
func (st *Stack) Top() Modal {
	if st == nil || len(st.modals) == 0 {
		return nil
	}
	return st.modals[len(st.modals)-1]
}

// Get returns the modal with the given ID, or nil when absent.
func (st *Stack) Get(id string) Modal {
	if st == nil || id == "" {
		return nil
	}
	for i := len(st.modals) - 1; i >= 0; i-- {
		if st.modals[i].ID() == id {
			return st.modals[i]
		}
	}
	return nil
}

// Has reports whether a modal with the given ID is on the stack.
func (st *Stack) Has(id string) bool {
	return st.Get(id) != nil
}

// IDs returns the IDs of all active modals in bottom-to-top order.
func (st *Stack) IDs() []string {
	if st == nil || len(st.modals) == 0 {
		return nil
	}
	out := make([]string, len(st.modals))
	for i, m := range st.modals {
		out[i] = m.ID()
	}
	return out
}

// Push adds m on top of the stack and appends its core.Frame to s.Modals.
// If a modal with the same ID already exists, the old modal is removed after
// calling old.OnClose(CloseCancel) before pushing m.
func (st *Stack) Push(s core.State, m Modal) core.State {
	if st == nil || m == nil {
		return s
	}
	id := m.ID()
	if idx := st.indexOf(id); idx >= 0 {
		old := st.modals[idx]
		st.modals = slices.Delete(st.modals, idx, idx+1)
		s = removeModalFrame(s, id)
		old.OnClose(CloseCancel)
	}
	st.modals = append(st.modals, m)
	frame := core.Frame{
		ID:      id,
		Kind:    core.FrameModal,
		FocusID: m.FocusID(),
		Mode:    m.Mode(),
	}
	s.Modals = append(slices.Clone(s.Modals), frame)
	return s
}

// Pop removes the top modal, removes its frame from s.Modals, and calls
// OnClose(reason).
func (st *Stack) Pop(s core.State, reason CloseReason) (core.State, Modal) {
	if st == nil || len(st.modals) == 0 {
		return s, nil
	}
	last := len(st.modals) - 1
	top := st.modals[last]
	st.modals = st.modals[:last]
	s = removeModalFrame(s, top.ID())
	top.OnClose(reason)
	return s, top
}

// Close removes the modal with the given ID, removes its frame from s.Modals,
// and calls OnClose(reason).
func (st *Stack) Close(s core.State, id string, reason CloseReason) (core.State, Modal) {
	if st == nil || id == "" {
		return s, nil
	}
	idx := st.indexOf(id)
	if idx < 0 {
		return s, nil
	}
	removed := st.modals[idx]
	st.modals = slices.Delete(st.modals, idx, idx+1)
	s = removeModalFrame(s, id)
	removed.OnClose(reason)
	return s, removed
}

// DropAll clears every modal from the stack, clears s.Modals, and invokes
// OnClose(CloseDropAll) on each removed modal in LIFO (top-to-bottom) order.
func (st *Stack) DropAll(s core.State) core.State {
	s.Modals = nil
	if st == nil || len(st.modals) == 0 {
		return s
	}
	removed := st.modals
	st.modals = nil
	for i := len(removed) - 1; i >= 0; i-- {
		removed[i].OnClose(CloseDropAll)
	}
	return s
}

// ApplyResult transitions the stack and core.State according to res:
//  1. If res.Close, pops the top modal with res.CloseReason.
//  2. If res.Push != nil, pushes res.Push.
//  3. Otherwise, synchronizes the top frame's Mode and FocusID from
//     res.Mode/res.FocusID (falling back to top.Mode()/top.FocusID()).
func (st *Stack) ApplyResult(s core.State, res Result) core.State {
	if res.Close {
		s, _ = st.Pop(s, res.CloseReason)
	}
	if res.Push != nil {
		return st.Push(s, res.Push)
	}
	if res.Close {
		return s
	}
	return st.syncTopFrame(s, res.Mode, res.FocusID)
}

// SyncFrame updates the top core.Frame in s.Modals to match the top modal's
// current Mode and FocusID.
func (st *Stack) SyncFrame(s core.State) core.State {
	return st.syncTopFrame(s, "", "")
}

func (st *Stack) syncTopFrame(s core.State, modeOverride, focusOverride string) core.State {
	top := st.Top()
	if top == nil || len(s.Modals) == 0 {
		return s
	}
	idx := len(s.Modals) - 1
	if s.Modals[idx].ID != top.ID() {
		return s
	}
	mode := modeOverride
	if mode == "" {
		mode = top.Mode()
	}
	focusID := focusOverride
	if focusID == "" {
		focusID = top.FocusID()
	}
	if s.Modals[idx].Mode == mode && s.Modals[idx].FocusID == focusID {
		return s
	}
	s.Modals = slices.Clone(s.Modals)
	s.Modals[idx].Mode = mode
	s.Modals[idx].FocusID = focusID
	return s
}

func (st *Stack) indexOf(id string) int {
	for i := len(st.modals) - 1; i >= 0; i-- {
		if st.modals[i].ID() == id {
			return i
		}
	}
	return -1
}

func removeModalFrame(s core.State, id string) core.State {
	for i := len(s.Modals) - 1; i >= 0; i-- {
		if s.Modals[i].ID == id {
			next := slices.Delete(slices.Clone(s.Modals), i, i+1)
			if len(next) == 0 {
				s.Modals = nil
			} else {
				s.Modals = next
			}
			return s
		}
	}
	return s
}
