package ochazuke

import "sort"

// EffectiveBindingsFor is the stack walk phase 5 dispatches through: from the
// focused instance outward, the first instance whose own rows or noun bind a
// key sequence wins; within one level the instance's rows beat its noun's and
// a row for the current mode beats an any-mode row. That is the precedence
// EffectiveBindings produces by overlaying outer to inner.
func (r *Runtime) EffectiveBindingsFor(focus, mode string) []Binding {
	r.syncNouns()
	byKey := map[string]Binding{}
	take := func(rows []Binding) {
		for i := len(rows) - 1; i >= 0; i-- {
			if _, ok := byKey[rows[i].Keys]; !ok {
				byKey[rows[i].Keys] = rows[i]
			}
		}
	}
	for _, id := range r.Ancestry(focus) {
		own := groupBindings(r.Instances[id].Bindings)
		if mode != "" {
			take(own[mode])
		}
		take(own[""])
		noun := r.Nouns[r.Instances[id].Noun]
		if noun == nil {
			continue
		}
		if mode != "" {
			take(noun.Bindings[mode])
		}
		take(noun.Bindings[""])
	}
	out := make([]Binding, 0, len(byKey))
	for _, b := range byKey {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Keys < out[j].Keys })
	return out
}

// ActionFor returns the innermost noun handler for an operation key on the
// focus stack, or nil when the operation's own Run applies. It is the loop
// ExecuteAt runs over Context.Path, read through instances.
func (r *Runtime) ActionFor(focus, key string) Handler {
	r.syncNouns()
	for _, id := range r.Ancestry(focus) {
		if noun := r.Nouns[r.Instances[id].Noun]; noun != nil {
			if fn := noun.Actions[key]; fn != nil {
				return fn
			}
		}
	}
	return nil
}
