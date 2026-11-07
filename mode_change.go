package ochazuke

import "fmt"

// modeCause says why the global mode changes. It describes the kind of
// transition, never a mode's or a noun's name. The writer treats every
// cause alike today; the cause documents each call and gives later
// transition policy (focus landing versus explicit escape) one place to
// read it.
type modeCause int

const (
	// modeInit is the constructor's first mode.
	modeInit modeCause = iota
	// modeNamed is an explicit request by name: Runtime.SetMode and
	// mode.switch. It advances the generation even when the name and
	// payload stay the same.
	modeNamed
	// modeInputEdit is a focused input's editing mode mirrored into the
	// global mode (Input.SetMode, storeMode, resetKeys, input.cancel).
	modeInputEdit
	// modeFocusLanding is the mode a focus move lands in: the declared pin
	// of the new stop, or normal when the stop changed.
	modeFocusLanding
	// modeRestore returns to a remembered mode: a modal's or a page's.
	modeRestore
	// modePendingEnter enters a pending mode (count, operator, a sequence
	// prefix, a leader) with its typed payload.
	modePendingEnter
	// modePendingLeave returns from a pending mode to the base it shadowed.
	modePendingLeave
	// modePayloadUpdate changes the payload of the current mode in place of
	// a new mode (count.append, a re-pin's new return base). The prefix
	// and its deadline stay armed.
	modePayloadUpdate
	// modeSelectionEnd leaves a list's filter or selection mode for normal.
	modeSelectionEnd
)

// changeMode is the one writer of Runtime.Mode: every real or payload
// transition, the focused input's mirror and the constructor's first mode
// go through it, and the guard test TestStateWriteOwnership fails on an
// assignment anywhere else (the field stays exported and readable). It
// assigns next once, advances modeGeneration once (so a handler that asked
// for a mode, even the same one, is seen to have changed it: runRow), then
// tells the ModeObservers on the new focus path, then on leaving (the path
// a focus move is leaving), each instance once, when the base mode changed.
// It records no history, emits no event and arms or cancels no prefix
// timer: enterMode, leaveMode and leavePrefix own those.
func (r *Runtime) changeMode(next ModeState, cause modeCause, leaving ...string) {
	before := r.BaseMode()
	r.Mode = next
	r.modeGeneration++
	after := r.BaseMode()
	if before == after {
		return
	}
	seen := map[string]bool{}
	for _, id := range append(r.Ancestry(r.Focus), leaving...) {
		if seen[id] {
			continue
		}
		seen[id] = true
		if inst := r.Instances[id]; inst != nil {
			if obs, ok := inst.Component.(ModeObserver); ok {
				obs.ModeChanged(before, after)
			}
		}
	}
}

// switchMode is the named-mode request mode.switch and Runtime.SetMode
// share: ModeDeclared resolves to the focus's declared mode, the name must
// be a registered base mode, and the payload is cleared. When in is set (the
// request's input), the input runs its own transition (insert sessions, the
// visual anchor, a pin, a read-only viewer) and mirrors the global mode
// while it is focused; a background input changes only itself. Either way
// the request advances the mode generation once more, so a same-name
// request still counts as a mode change.
func (r *Runtime) switchMode(mode string, in *Input) (string, error) {
	mode = r.resolveDeclared(mode)
	if !r.IsBaseMode(mode) {
		return "", fmt.Errorf("unknown mode %q", mode)
	}
	if in != nil {
		in.SetMode(mode)
		r.changeMode(r.Mode, modeNamed)
		return in.Mode, nil
	}
	r.changeMode(ModeState{Name: mode}, modeNamed)
	return mode, nil
}
