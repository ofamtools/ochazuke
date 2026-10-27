package ochazuke

import (
	"maps"
	"slices"
	"strings"
)

// Read-only views of runtime state for tools that check an application
// from outside (the explore package): each reports what the runtime
// already decides internally and changes nothing.

// FocusInTopLevel reports the focus invariant: the focus is inside the top
// level, the top modal's subtree while a modal is open (else the current
// page, else app).
func (r *Runtime) FocusInTopLevel() bool { return r.focusInTopLevel() }

// GuardNames lists every registered row guard (the built-ins and those of
// DefineGuard), sorted. The "op:<key>" family is not listed: it is a
// pattern, not a name.
func (r *Runtime) GuardNames() []string { return slices.Sorted(maps.Keys(r.guards)) }

// GuardHolds evaluates the named guard at focus in the current mode, as the
// footer's hints and ? help judge a row's guard (KeyEvent{Mode, Focus});
// known is false for a name no guard answers to.
func (r *Runtime) GuardHolds(name, focus string) (holds, known bool) {
	check := r.guard(name)
	if check == nil {
		return false, false
	}
	return check(r, KeyEvent{Mode: r.Mode, Focus: focus}), true
}

// PendingModes lists the registered pending modes (DefineMode with
// pending), sorted.
func (r *Runtime) PendingModes() []string { return slices.Sorted(maps.Keys(r.pendingModes)) }

// IsPendingMode reports whether name is a registered pending mode.
func (r *Runtime) IsPendingMode(name string) bool { return r.pendingModes[name] }

// IsPassthrough reports whether name is a passthrough base mode
// (DefinePassthrough).
func (r *Runtime) IsPassthrough(name string) bool { return r.passthroughModes[name] }

// ModeKnown reports whether the current mode is one the runtime defined: a
// base mode, a pending mode, or a key-sequence prefix over a base mode.
func (r *Runtime) ModeKnown() bool {
	name := r.Mode.Name
	if r.Modes[name] || r.pendingModes[name] {
		return true
	}
	if prefix := r.Mode.Payload.Prefix; prefix != "" {
		base, ok := strings.CutSuffix(name, " "+prefix)
		return ok && (r.Modes[base] || r.pendingModes[base])
	}
	return false
}

// ModalReturn is what closing an open modal restores: the focus and mode
// it opened over. Unknown or closed modals report "" and the zero mode.
func (r *Runtime) ModalReturn(id string) (focus string, mode ModeState) {
	if !slices.Contains(r.modals, id) {
		return "", ModeState{}
	}
	inst := r.Instances[id]
	return inst.Payload.Return, inst.returnMode
}
