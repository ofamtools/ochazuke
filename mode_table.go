package ochazuke

import (
	"maps"
	"slices"
)

// modeTable is every defined mode by name with its traits (ModeSpec): one
// entry per mode, base or pending. An entry exists exactly when the mode is
// defined (DefineMode, a bindings file's "modes", the built-in modes);
// DefinePassthrough, DefineSelection, ModeExtends and ModeOutside write
// its traits. Code asks a mode's traits, never its name: whether it is
// pending, typing, keeps the cursor on a character, and so on.
type modeTable map[string]ModeSpec

// builtinModes are the framework's two base modes and their traits, as
// data: normal keeps the cursor on a character, insert types. Every other
// mode (the pending modes, the visual modes, the terminal mode) is declared
// by the file or code that owns it.
func builtinModes() map[string]ModeSpec {
	return map[string]ModeSpec{
		"normal": {CursorOnChar: true},
		"insert": {Typing: true},
	}
}

// putMode writes name's entry and keeps the exported Modes view (the base
// modes) in step. It is the table's one writer.
func (r *Runtime) putMode(name string, spec ModeSpec) {
	r.modes[name] = spec
	if spec.Pending {
		delete(r.Modes, name)
	} else {
		r.Modes[name] = true
	}
}

// ensureBuiltinModes defines the built-in modes as base modes with their
// traits, keeping any trait the runtime already gave them.
func (r *Runtime) ensureBuiltinModes() {
	for _, name := range slices.Sorted(maps.Keys(builtinModes())) {
		builtin := builtinModes()[name]
		spec := r.modes[name]
		spec.Pending = false
		spec.Typing = spec.Typing || builtin.Typing
		spec.CursorOnChar = spec.CursorOnChar || builtin.CursorOnChar
		r.putMode(name, spec)
	}
}

// modeDefined reports whether name is a defined mode, base or pending.
func (r *Runtime) modeDefined(name string) bool {
	_, ok := r.modes[name]
	return ok
}

// IsBaseMode reports whether name is a defined base (not pending) mode.
func (r *Runtime) IsBaseMode(name string) bool {
	spec, ok := r.modes[name]
	return ok && !spec.Pending
}

// ModeTraits is name's entry in the mode table, and whether name is a
// defined mode. Ask a mode's traits (Typing, CursorOnChar, Pending,
// Selection, ...) instead of comparing its name.
func (r *Runtime) ModeTraits(name string) (ModeSpec, bool) {
	spec, ok := r.modes[name]
	return spec, ok
}

// modeSpecOf is name's traits on r; with no runtime (an input not yet
// added) the built-in modes' traits.
func modeSpecOf(r *Runtime, name string) ModeSpec {
	if r == nil {
		return builtinModes()[name]
	}
	return r.modes[name]
}

// modeInherits reports whether mode, or a mode it extends (ModeExtends, up
// the chain), has the trait: a mode that extends insert types where the
// framework asks, as ModeIs(mode, "insert") answered before the trait.
func modeInherits(r *Runtime, mode string, trait func(ModeSpec) bool) bool {
	seen := map[string]bool{}
	for m := mode; m != "" && !seen[m]; m = modeSpecOf(r, m).Extends {
		if trait(modeSpecOf(r, m)) {
			return true
		}
		seen[m] = true
	}
	return false
}

// typingMode reports whether mode types text (Typing), itself or through
// the mode it extends.
func typingMode(r *Runtime, mode string) bool {
	return modeInherits(r, mode, func(s ModeSpec) bool { return s.Typing })
}

// charCursorMode reports whether mode keeps the cursor on a character
// (CursorOnChar), itself or through the mode it extends.
func charCursorMode(r *Runtime, mode string) bool {
	return modeInherits(r, mode, func(s ModeSpec) bool { return s.CursorOnChar })
}

// modeNames lists the defined modes whose entry passes keep, sorted.
func (r *Runtime) modeNames(keep func(ModeSpec) bool) []string {
	var out []string
	for name, spec := range r.modes {
		if keep(spec) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// baseModeNames are the defined base modes, sorted.
func (r *Runtime) baseModeNames() []string {
	return r.modeNames(func(s ModeSpec) bool { return !s.Pending })
}

// BaseModeNames lists the defined base modes, sorted. Prefer it to reading
// Runtime.Modes: it is the same set without depending on a field that is
// only a view.
func (r *Runtime) BaseModeNames() []string { return r.baseModeNames() }

// PendingModeNames lists the defined pending modes (count_pending,
// operator_pending, ...), sorted.
func (r *Runtime) PendingModeNames() []string {
	return r.modeNames(func(s ModeSpec) bool { return s.Pending })
}

// allModeNames are every defined mode, base and pending, sorted.
func (r *Runtime) allModeNames() []string {
	return r.modeNames(func(ModeSpec) bool { return true })
}

// modeCurrent reports whether name is the mode the runtime is in: the
// live mode (a pending one included) or the base mode beneath it.
func (r *Runtime) modeCurrent(name string) bool {
	return name == r.Mode.Name || name == r.BaseMode()
}

// NewModeExtends is the mode a mode nobody registered falls back to when
// first used (ensureMode): its keys with no row of their own run the rows
// of this mode, so a new mode is never a trap.
const NewModeExtends = "normal"

// ensureMode makes name a mode when it is not one yet: a layered base mode
// (ModeSpec.Layered) that extends NewModeExtends, listed by mode list, with its own binding
// context (NOUN/name) for rows to target. Any name is accepted (owner
// 2026-11-13: "If it's in the list, I should be able to use it"); only an
// empty name or the reserved declared cannot be one.
func (r *Runtime) ensureMode(name string) error {
	if r.modeDefined(name) {
		return nil
	}
	if err := r.DefineMode(name, false, ModeExtends(NewModeExtends)); err != nil {
		return err
	}
	spec := r.modes[name]
	spec.Layered = true
	r.putMode(name, spec)
	return nil
}
