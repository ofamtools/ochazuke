package ochazuke

// focusCause says how a focus move lands: what the one focus writer does
// after its assignment. It describes the kind of landing, never a page's,
// a mode's or a noun's name.
type focusCause int

const (
	// focusAssign assigns the focus and does nothing else: the
	// constructor's first focus, and test fixtures that need a focus no
	// public move reaches (forceFocus).
	focusAssign focusCause = iota
	// focusLanding applies the focus mode rule (focusMoved): within one
	// stop the mode stays, on another stop the new focus's declared pin or
	// normal applies. setFocus (SetFocus, RetargetFocus, Push, focus
	// jumps), a Pop or page open with no saved mode, and the repair after
	// a subtree removal land this way.
	focusLanding
	// focusRestore lands on a saved focus with its saved mode (a modal's
	// return, a page's remembered mode): the saved mode is committed once,
	// with no default landing before it.
	focusRestore
	// focusSettle lands without a mode rule: the focus stays in the place
	// the caller already landed on (a page reopened at the focus it holds,
	// a page's OnOpen moving from the page body to its list's selected
	// row). The focused row is still selected.
	focusSettle
)

// focusMove is how one focus move lands: its cause, where the focus is
// leaving (its stop and ancestry, captured before any repair removes or
// reparents instances) and, for focusRestore, the saved mode.
type focusMove struct {
	cause focusCause
	stop  string
	path  []string
	mode  ModeState
}

// focusLeaving is a move of cause away from the current focus: its stop
// and ancestry as they stand now.
func (r *Runtime) focusLeaving(cause focusCause) focusMove {
	return focusMove{cause: cause, stop: r.stopOf(r.Focus), path: r.Ancestry(r.Focus)}
}

// changeFocus is the one writer of Runtime.Focus: every focus move, the
// page restore, a modal's push and pop, the repair after a subtree
// removal and the constructor's first focus go through it, and the guard
// test TestStateWriteOwnership fails on an assignment anywhere else (the
// field stays exported and readable). It assigns to once, then by the
// move's cause selects the focused list row, remembers on the instances
// being left where the focus was inside them, and commits one mode
// transition: the focus rule (focusMoved) or the saved mode
// (restoreMode), never both. Callers validate (validateFocus) or clamp
// (clampFocus) the destination and cancel a pending key sequence before
// calling; it records no history and emits no event.
func (r *Runtime) changeFocus(to string, move focusMove) {
	r.Focus = to
	if move.cause == focusAssign {
		return
	}
	r.selectFocusedRow(to)
	switch move.cause {
	case focusLanding:
		r.focusMoved(move.stop, move.path, to)
	case focusRestore:
		r.rememberLeaving(move.path, to)
		r.restoreMode(move.mode, move.path)
	}
}
