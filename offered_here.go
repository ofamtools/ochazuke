package ochazuke

import "slices"

// A component says, as data, which verbs are not usual on it (owner
// 2026-11-13: "certain components were supposed to be able to say that
// certain actions aren't available for them ... there needs to be a way to
// override it, like the command palette or the CLI"). The declaration does
// what a false guard does and nothing more: a key's row for the verb is
// skipped (the key falls through to the next row), hints and the footer
// leave it out, the palette lists it after the usual verbs marked "not
// usual here", and an explicit request (the palette, the CLI, MCP, the
// help modal's enter) runs it (no-refusals D3).

// Unoffers declares, on the instance a constructor makes, the verbs that
// are not usual on it: a full key (noun.verb) or a bare verb (that verb
// of every noun).
func Unoffers(verbs ...string) InstanceOption {
	return func(s *instanceSpec) { s.unoffered = append(s.unoffered, verbs...) }
}

// Unoffer adds verbs to a live instance's Unoffered (see Unoffers), for
// components their constructor does not take options for.
func (r *Runtime) Unoffer(id string, verbs ...string) {
	if inst := r.Instances[id]; inst != nil {
		inst.Unoffered = append(inst.Unoffered, verbs...)
		r.InvalidateBindings()
	}
}

// unofferedAt reports whether the focus, or a component it sits in,
// declares the verb of key not usual there, or the thing at the focus
// keeps itself from the change the verb makes (fixed.go).
func (r *Runtime) unofferedAt(key, focus string) bool {
	if focus == "" {
		return false
	}
	op, ok := r.Operation(key)
	if !ok {
		return false
	}
	if !r.offersChange(op, focus) {
		return true // the thing at the focus keeps itself from the verb's change (fixed.go)
	}
	names := []string{key, op.Key.Short(), op.Name(), r.DisplayKey(op.Key.Short())}
	for _, id := range r.Ancestry(focus) {
		inst := r.Instances[id]
		if inst == nil {
			continue
		}
		if slices.ContainsFunc(inst.Unoffered, func(v string) bool { return slices.Contains(names, v) }) {
			return true
		}
		if u, ok := inst.Component.(unofferer); ok && u.unoffers(op) {
			return true
		}
	}
	return false
}

// unofferer is a component that declares, from what it is, verbs that are
// not usual on it, as Unoffered does for a list of names: a Tree keeps the
// standard writes of its noun off its keys (Tree.unoffers).
type unofferer interface {
	unoffers(op Operation) bool
}

// NotUsualHere is the mark the palette puts on a verb that is not usual
// where it opened (a guard false there, a component's Unoffered): it is
// listed after the usual verbs and runs when chosen.
const NotUsualHere = "not usual here"
