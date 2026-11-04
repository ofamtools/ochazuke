package ochazuke

import "slices"

// LeaderFallThroughSetting controls unbound keys of modes with Outside.
const LeaderFallThroughSetting = "leader.fall-through"

// dispatchOutside offers a pending mode's otherwise unbound key to each
// associated thing's outside walk, then the original focus's walk. The real
// focus and mode are never swapped: guards and handlers use the event context,
// while a modal remembers the live surface it opened over.
func (r *Runtime) dispatchOutside(focus string, mode ModeState, key MacroKey) (bool, error) {
	outside := r.modeOutside[mode.Name]
	policy := r.Settings().String(LeaderFallThroughSetting)
	if outside == "" || policy == "off" {
		return false, nil
	}
	run := func(at string) (found, allowed bool, err error) {
		ev := KeyEvent{Key: key, Mode: ModeState{Name: outside}, Focus: at}
		for _, lv := range r.walkOf(r.stack(at), r.passthroughModes[outside]) {
			row, _ := r.lookup(lv, outside, false, "", key.Key, key, ev)
			if row == nil {
				continue // outside multi-key prefixes are deliberately not entered
			}
			op, _ := r.Operation(row.Request.Key())
			if policy != "all" && (op.Motion || op.FocusBound) {
				return true, false, nil
			}
			if lv.inst != nil {
				ev.Level = lv.inst.ID
			}
			r.leavePrefix()
			top := r.TopModal()
			err := r.runRow(lv, *row, ev, outside)
			if inst := r.Instances[at]; at != focus && inst != nil && op.Noun == inst.Noun {
				r.notifyAssociation(op.Noun, at, top, err)
			}
			return true, true, err
		}
		return false, false, nil
	}
	if r.Settings().Bool(SubjectFromAssociationSetting) {
		for _, a := range r.Associations(focus, "") {
			ancestry := r.Ancestry(a.id)
			if !slices.Contains(ancestry, r.Page) || r.TopModal() != "" && !slices.Contains(ancestry, r.TopModal()) {
				continue // outside means this page and this modal boundary
			}
			if found, allowed, err := run(a.id); found {
				return allowed, err
			}
		}
	}
	_, allowed, err := run(focus)
	return allowed, err
}
