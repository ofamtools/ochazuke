package ochazuke

// A through mode (ModeSpec.Through) is a pending mode that lends the keyboard
// to a base mode for one binding: a live terminal's ephemeral passthrough
// resolves its keys in normal and then goes back to the pane. The resolver is
// not told about it. Keys reach dispatchIn with the runtime really standing
// in the base mode, so what a key resolves to, what waits and what a held
// sequence does with the next key are that mode's own rules. This file only
// enters the base mode before a key and decides, once the whole key is done,
// where the runtime goes next.

// throughState is a through mode's key in progress: set from the moment a key
// goes to the base mode until that key, and any sequence it started, has
// ended. It lives here and not in a mode's payload because a held sequence is
// a mode of its own (enterPrefix) with a payload of its own.
type throughState struct {
	mode    string // the through mode; "" when no key is in progress
	returns string // the base mode the through mode was entered over
	focus   string // the surface the key was typed on
	page    string
	modals  int // modals open when the key was typed
	depth   int // dispatches running inside dispatchThrough
}

// throughApplies reports whether a key is the through mode's to take: the
// mode is current, or a sequence begun in it is held. A key dispatched while
// one is being handled (the key after a sequence's shorter match, a replay)
// is the resolver's own.
func (r *Runtime) throughApplies() bool {
	return r.UI().through.depth == 0 && (r.UI().through.mode != "" || r.modes[r.Mode.Name].Through != "")
}

// dispatchThrough handles one key of a through mode. The mode's own rows
// answer first (esc leaves it); any other key resolves in the base mode. A
// key nothing there answers belongs to the surface: it is offered in the mode
// entered over, so what is typed is never lost.
func (r *Runtime) dispatchThrough(key MacroKey) (KeyOutcome, error) {
	if r.UI().through.mode != "" {
		// A sequence is held from an earlier key. Its deadline may have
		// passed, or something other than a key may have ended it.
		if err := r.expirePrefix(); err != nil {
			return KeyHandled, err
		}
		r.settleThrough(false)
		if r.UI().through.mode == "" {
			return r.DispatchOutcome(key)
		}
	} else {
		outcome, err := r.dispatchIn(r.Focus, r.Mode, key)
		if outcome != KeyUnhandled || err != nil {
			return outcome, err
		}
		r.UI().through = throughState{mode: r.Mode.Name, returns: r.BaseMode(), focus: r.Focus, page: r.Page, modals: len(r.modals)}
		r.restoreMode(ModeState{Name: r.modes[r.Mode.Name].Through}, nil)
	}
	cancels := r.cancelsHeld(key)
	r.UI().through.depth++
	outcome, err := r.DispatchOutcome(key)
	r.UI().through.depth--
	if outcome == KeyUnhandled && r.onThroughSurface() {
		return r.offerToSurface(key)
	}
	r.settleThrough(outcome == KeyHandled && !cancels)
	return outcome, err
}

// cancelsHeld reports whether key is the held sequence's cancel row
// ("<prefix> esc"): it ends the sequence and resolves no binding.
func (r *Runtime) cancelsHeld(key MacroKey) bool {
	held := r.Mode.Payload.Prefix
	if held == "" {
		return false
	}
	for _, row := range r.levelRows(r.Focus) {
		if row.Keys == held+" "+key.Key {
			return isPrefixCancel(row.Binding)
		}
	}
	return false
}

// onThroughSurface reports whether the key in progress left the runtime where
// it was typed: same focus and page, no new modal, still in the base mode.
func (r *Runtime) onThroughSurface() bool {
	t := r.UI().through
	return r.Focus == t.focus && r.Page == t.page && len(r.modals) == t.modals && r.BaseMode() == r.modes[t.mode].Through
}

// offerToSurface gives a key the base mode has no row for to the mode's own
// rows, then to the mode the through mode was entered over.
func (r *Runtime) offerToSurface(key MacroKey) (KeyOutcome, error) {
	t := r.UI().through
	r.UI().through = throughState{}
	r.changeMode(ModeState{Name: t.mode, Payload: Payload{Return: t.returns}}, modeRestore)
	outcome, err := r.dispatchIn(r.Focus, r.Mode, key)
	if outcome != KeyUnhandled || err != nil {
		return outcome, err
	}
	r.restoreMode(ModeState{Name: t.returns}, nil)
	return r.DispatchOutcome(key)
}

// settleThrough decides where a finished key leaves the runtime; ran says a
// binding resolved. While a sequence is still held on the surface nothing
// changes. Otherwise the state is cleared, so it can never act later on some
// other surface: a modal the key opened returns to the mode entered over when
// it closes, a key that moved the person elsewhere leaves them there, a
// resolved binding returns to the mode entered over, and a key that resolved
// nothing (a cancelled sequence) waits in the through mode for the next one.
func (r *Runtime) settleThrough(ran bool) {
	t := r.UI().through
	if t.mode == "" || t.depth > 0 {
		return
	}
	surface := r.onThroughSurface()
	if surface && r.pending() {
		return
	}
	r.UI().through = throughState{}
	switch {
	case len(r.modals) > t.modals:
		opened := r.modals[t.modals]
		r.repairModalReturns(func(id string, ret ModalReturnState) (ModalReturnState, bool) {
			if id != opened || ret.Focus != t.focus {
				return ret, false
			}
			ret.Mode = ModeState{Name: t.returns}
			return ret, true
		})
	case !surface:
	case ran:
		r.restoreMode(ModeState{Name: t.returns}, nil)
	default:
		r.changeMode(ModeState{Name: t.mode, Payload: Payload{Return: t.returns}}, modeRestore)
	}
}
