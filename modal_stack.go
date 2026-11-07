package ochazuke

import "slices"

// The modal stack and its saved returns have one owner: this file.
//
// The open modal IDs (Runtime.modals, outermost first), the instances the
// stack created and removes again (Runtime.modalInstances), each open
// modal's saved return (Runtime.ModalReturns: the focus and mode closing it
// restores, one value) and the adoption bookkeeping of a borrowed focus
// (Instance.borrowed, lentParent) are written only by the methods below.
// Push, Pop, OpenModal (ReplaceOpen included), RetargetFocus, subtree
// removal, PinInstance and the end of a visual selection call them; none
// writes a return itself. Everything stays readable: ModalReturns is
// exported for inspection and ModalFrames gives a detached copy. The guard
// test TestStateWriteOwnership refuses a write anywhere else.
//
// An open modal's Payload.Return is a compatibility snapshot of its saved
// return focus, kept in step by setModalReturn so code and JSON that read
// it see what they always saw. It is not the authority: writing it through
// an *Instance (Modals() hands those out) does not change what Pop
// restores. A standalone pending payload's Return (Runtime.Mode's) is
// unrelated and stays the mode's own.

// ModalReturnState is what closing an open modal restores: the focus and
// the mode it was opened over. They are saved, repaired and restored
// together as one value.
type ModalReturnState struct {
	Focus string    `json:"focus"`
	Mode  ModeState `json:"mode"`
}

// ModalFrame is one open modal as the stack holds it: its id, its saved
// return and whether the stack created its instance (and so removes it on
// close). ModalFrames returns detached copies.
type ModalFrame struct {
	ID      string           `json:"id"`
	Return  ModalReturnState `json:"return"`
	Created bool             `json:"created,omitempty"`
}

// ModalFrames lists the open modals' frames, outermost first, detached:
// changing the result changes nothing in the runtime.
func (r *Runtime) ModalFrames() []ModalFrame {
	out := make([]ModalFrame, 0, len(r.modals))
	for _, id := range r.modals {
		ret := r.ModalReturns[id]
		ret.Mode = cloneModeState(ret.Mode)
		out = append(out, ModalFrame{ID: id, Return: ret, Created: r.modalInstances[id]})
	}
	return out
}

// initModalStack is the constructor's empty stack.
func (r *Runtime) initModalStack() {
	r.modals = nil
	r.modalInstances = map[string]bool{}
	r.ModalReturns = map[string]ModalReturnState{}
}

// markModalInstance records that the stack created id's instance (Push of
// an unknown id, OpenModal's body), so closing it removes the instance.
func (r *Runtime) markModalInstance(id string) { r.modalInstances[id] = true }

// releaseModalInstance forgets that the stack created id: the modal is
// closing and its instance is being removed, or an open rolled back.
func (r *Runtime) releaseModalInstance(id string) { delete(r.modalInstances, id) }

// pushModalFrame puts id on top of the stack with the return it saves.
func (r *Runtime) pushModalFrame(id string, ret ModalReturnState) {
	r.modals = append(r.modals, id)
	r.setModalReturn(id, ret)
}

// adoptModalFocus parents focus under the open modal while it is open (a
// Push focused outside the modal's subtree), remembering its own parent
// for popModalFrame to give back.
func (r *Runtime) adoptModalFocus(modal, focus string) {
	inst, target := r.Instances[modal], r.Instances[focus]
	if inst == nil || target == nil {
		return
	}
	inst.borrowed, inst.lentParent = focus, target.Parent
	target.Parent = modal
}

// setModalReturn is the one write of an open modal's saved return: the
// authoritative frame value and the Payload.Return snapshot together.
func (r *Runtime) setModalReturn(id string, ret ModalReturnState) {
	if !slices.Contains(r.modals, id) {
		return
	}
	ret.Mode = cloneModeState(ret.Mode)
	r.ModalReturns[id] = ret
	if inst := r.Instances[id]; inst != nil {
		inst.Payload.Return = ret.Focus
	}
}

// repairModalReturns gives each open modal's saved return, outermost
// first, to fix and saves what it returns when it reports a change: a
// retarget, a removed subtree, a re-pin or the end of a selection repairs
// the returns through it with its own matching rule, never the live
// focus of the top modal.
func (r *Runtime) repairModalReturns(fix func(id string, ret ModalReturnState) (ModalReturnState, bool)) {
	for _, id := range slices.Clone(r.modals) {
		if next, ok := fix(id, r.ModalReturns[id]); ok {
			r.setModalReturn(id, next)
		}
	}
}

// popModalFrame takes the top modal off the stack and gives back its id
// and the return it saved. A borrowed focus gets its own parent back (app
// when that parent is gone). A modal whose instance the stack did not
// create keeps neither a return snapshot nor a close request: the next
// Push sets its own.
func (r *Runtime) popModalFrame() (id string, ret ModalReturnState) {
	id = r.modals[len(r.modals)-1]
	ret = r.ModalReturns[id]
	r.modals = r.modals[:len(r.modals)-1]
	delete(r.ModalReturns, id)
	inst := r.Instances[id]
	if inst == nil {
		return id, ret
	}
	if lent := r.Instances[inst.borrowed]; lent != nil && lent.Parent == id {
		lent.Parent = inst.lentParent
		if r.Instances[lent.Parent] == nil {
			lent.Parent = "app"
		}
	}
	inst.borrowed, inst.lentParent = "", ""
	if !r.modalInstances[id] {
		inst.Payload.Return, inst.close = "", nil
	}
	return id, ret
}
