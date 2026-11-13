package ochazuke

import "slices"

// LeaderFallThroughSetting controls unbound keys of modes with Outside.
const LeaderFallThroughSetting = "leader.fall-through"

// reachedOutcome is what a reached thing does with an outside key.
type reachedOutcome int

const (
	reachRun      reachedOutcome = iota // the thing is the nearest of its noun: it takes the key
	reachYield                          // another thing is nearer: its own turn comes first
	reachNotified                       // no ask path: the candidates were named and nothing ran
	reachAsk                            // several candidates: the key runs with no subject and asks
)

// reachedAction decides what the reached thing at does with the key's
// operation op, the original focus being focus. Several candidates of the
// noun (or none that can be acted on directly) pick nothing: the key runs
// with no subject, as the outside key does, and asks.
func (r *Runtime) reachedAction(op Operation, at, focus string) reachedOutcome {
	id, things := r.nearestOf(r.Instances[at].Noun, focus, op.HistoryKindOf() == HistoryFetch)
	switch {
	case id == at:
		return reachRun
	case id != "":
		return reachYield
	case !r.asksSubject(op):
		// No ask path: say which things the key could mean and run
		// nothing, never an error or a guess.
		r.leavePrefix()
		r.UI().Notify(r.DisplayNoun(op.Noun()) + ": " + candidateNames(things))
		return reachNotified
	}
	return reachAsk
}

// dispatchOutside offers a pending mode's otherwise unbound key to each
// related thing's outside walk, then the original focus's walk. The real
// focus and mode are never swapped: guards and handlers use the event context,
// while a modal remembers the live surface it opened over.
func (r *Runtime) dispatchOutside(focus string, mode ModeState, key MacroKey) (bool, error) {
	outside := r.modes[mode.Name].Outside
	policy := r.Settings().String(LeaderFallThroughSetting)
	if outside == "" || policy == "off" {
		return false, nil
	}
	run := func(at string) (found, allowed bool, err error) {
		ev := KeyEvent{Key: key, Mode: ModeState{Name: outside}, Focus: at}
		for _, lv := range r.walkOf(r.stack(at), r.modes[outside].Passthrough) {
			row, _ := r.lookup(lv, outside, false, "", key.Key, key, ev)
			if row == nil {
				continue // outside multi-key prefixes are deliberately not entered
			}
			op, _ := r.Operation(row.Request.Key())
			if policy != "all" && (op.Motion || op.FocusBound) {
				return true, false, nil
			}
			if at != focus {
				switch r.reachedAction(op, at, focus) {
				case reachYield:
					return false, false, nil // another thing is nearer: its own turn comes first
				case reachNotified:
					return true, true, nil
				case reachAsk:
					ev.Focus, lv.inst = focus, nil
				}
			}
			if lv.inst != nil {
				ev.Level = lv.inst.ID
			}
			r.leavePrefix()
			top := r.TopModal()
			err := r.runRow(lv, *row, ev, outside)
			if inst := r.Instances[at]; at != focus && lv.inst != nil && inst != nil && op.Noun() == inst.Noun {
				r.notifyAssociation(op.Noun(), at, top, err)
			}
			return true, true, err
		}
		return false, false, nil
	}
	// Things the focus path reaches through relations (graph_reach.go) offer
	// the key to their own outside walk, level by level, nearest first.
	for _, at := range r.outsideReached(focus) {
		if found, allowed, err := run(at); found {
			return allowed, err
		}
	}
	_, allowed, err := run(focus)
	return allowed, err
}

// outsideReached are the instances the focus path reaches through
// relations that may offer an outside key, nearest first: each once, never
// one already on the focus path, and only within this page and this modal
// boundary.
func (r *Runtime) outsideReached(focus string) []string {
	path := r.Ancestry(focus)
	tried := map[string]bool{}
	var out []string
	for _, level := range path {
		for _, a := range r.reachFrom(level) {
			if a.inst == "" || tried[a.inst] || slices.Contains(path, a.inst) {
				continue
			}
			tried[a.inst] = true
			ancestry := r.Ancestry(a.inst)
			if !slices.Contains(ancestry, r.Page) || r.TopModal() != "" && !slices.Contains(ancestry, r.TopModal()) {
				continue // outside means this page and this modal boundary
			}
			out = append(out, a.inst)
		}
	}
	return out
}
