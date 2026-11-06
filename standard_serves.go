package ochazuke

// StoreServes reports whether the noun's store generates the standard verb
// available (a core verb is always registered; a legacy row only where its
// needs are met), so a host that mounts an extension's verbs leaves that
// verb to the store. A verb the store has but cannot run (unavailable with
// a reason, as forms.edit off) is still served: the extension's copy is not
// mounted in its place.
func (r *Runtime) StoreServes(noun, verb string) bool {
	n, row := r.Noun(noun), standardRow(verb)
	if n == nil || n.Store == nil || row == nil {
		return false
	}
	return !row.optional || row.unmet(r, n) == ""
}
