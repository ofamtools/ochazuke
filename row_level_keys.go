package ochazuke

import "slices"

// RowNouns are the nouns of the list's rows, sorted: the rows it holds,
// or the last rows it held when it has none now (what an empty list's
// verbs and filters read).
func (l *Collection) RowNouns() []string { return slices.Clone(l.rowNouns()) }

// RowLevelKeys are the keys a focus on instance id answers at its own
// level in mode: the rows the walk answers with (the first per key, the
// live guards and availability judged at id, as binding list judges
// them) whose level is the instance itself (its own rows, its noun's
// tables) or a level its key redirect puts on the walk (the related
// row's, key_redirect.go). Keys the list, the page or app answer are
// left out. Each key maps to the request its row runs; a sequence is one
// key ("d d"); an unguarded any row at that level is keyed Any (it
// answers every key); other pattern rows (the redirect's unbound) are
// left out. A pure query: it reads the walk in a hypothetical context
// with no modal open, assigns no Runtime field and reads or fills no
// binding cache. nil for an unknown instance.
//
// The rule checks read it (rulecheck deadkeys: a key one row answers that
// a sibling row of another noun does not).
func (r *Runtime) RowLevelKeys(id string, mode ModeState) map[string]Request {
	if r.Instances[id] == nil {
		return nil
	}
	ctx := queryResolution(id, mode, nil)
	levels := r.walkIn(ctx)
	own := map[string]bool{}
	for _, lv := range levels {
		if lv.related || (lv.inst != nil && lv.inst.ID == id) {
			own[lv.name()] = true
		}
	}
	out := map[string]Request{}
	for _, row := range r.scanRowsUncached(levels, ctx.Mode, id, ctx.within()) {
		if !own[row.level] || row.Request.Empty() {
			continue
		}
		key := row.Keys
		if key == "" {
			if row.Pattern != Any || row.When != "" {
				continue
			}
			key = Any
		}
		if _, seen := out[key]; !seen {
			out[key] = row.Request
		}
	}
	return out
}
