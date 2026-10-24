package ochazuke

import (
	"errors"
	"fmt"
	"slices"
)

// Focus, the modal chain and the mode rule a focus change applies.
//
// Runtime.Focus is the one focused instance. An open modal is an instance
// on Runtime's modal chain whose Payload.Return names the focus it was
// opened over (and the mode it was in); Pop restores both. A page
// remembers its focus and mode in Page.Remembered while another page is
// open. RemoveInstance points every one of those references into a removed
// subtree at the nearest surviving ancestor.

// Context derives the path view of the focus.
func (r *Runtime) Context() Context { return r.contextOf(r.Focus) }

// BaseFocus is the focus beneath every open modal: the return of the
// outermost modal, else Focus. Runtime.Page is always an ancestor of it.
func (r *Runtime) BaseFocus() string {
	if len(r.modals) > 0 {
		if inst := r.Instances[r.modals[0]]; inst != nil {
			return inst.Payload.Return
		}
	}
	return r.Focus
}

// Modals lists the open modal instances, outermost first.
func (r *Runtime) Modals() []*Instance {
	out := make([]*Instance, 0, len(r.modals))
	for _, id := range r.modals {
		out = append(out, r.Instances[id])
	}
	return out
}

// ModalFocus is the instance an open modal points at: Focus for the top
// modal, else the return focus stored in the modal opened over it.
func (r *Runtime) ModalFocus(id string) string {
	i := slices.Index(r.modals, id)
	switch {
	case i < 0:
		return ""
	case i == len(r.modals)-1:
		return r.Focus
	}
	return r.Instances[r.modals[i+1]].Payload.Return
}

// TopModal is the id of the innermost open modal, "" when none is open.
func (r *Runtime) TopModal() string {
	if len(r.modals) == 0 {
		return ""
	}
	return r.modals[len(r.modals)-1]
}

func (r *Runtime) modalIDs() []string { return append([]string{}, r.modals...) }

// stopOf is the focus stop an instance belongs to: the nearest focusable
// instance on its ancestry (itself included), else the instance itself.
func (r *Runtime) stopOf(id string) string {
	for _, step := range r.Ancestry(id) {
		if r.focusable(step) {
			return step
		}
	}
	return id
}

// focusable reports whether an instance is a focus stop: its noun is
// declared Focusable, or it fronts an input, and it has not opted out.
func (r *Runtime) focusable(id string) bool {
	inst := r.Instances[id]
	if inst == nil || inst.Unfocusable {
		return false
	}
	if _, ok := inst.Component.(*Input); ok {
		return true
	}
	noun := r.Nouns[inst.Noun]
	return noun != nil && noun.Focusable
}

// focusMoved applies the mode rule to a focus change. Moving within one
// focus stop (a grid's cell to the next cell, a list's row to the next row)
// keeps the mode; moving to another stop resets it to normal with an empty
// payload, or to the pinned mode of the newly focused noun (prompt pins
// insert, terminal pins terminal). A focused input then follows the mode.
func (r *Runtime) focusMoved(fromStop string, fromPath []string, to string) {
	r.rememberLeaving(fromPath, to)
	pin := ""
	if inst := r.Instances[to]; inst != nil {
		if noun := r.Nouns[inst.Noun]; noun != nil {
			pin = noun.Pin
		}
	}
	in := r.Input(to)
	if in != nil && in.pin != "" {
		pin = in.pin
	}
	switch {
	case pin != "":
		if r.BaseMode() != pin || !r.Mode.Payload.empty() {
			r.setMode(ModeState{Name: pin}, fromPath...)
		}
	case fromStop != r.stopOf(to):
		r.setMode(ModeState{Name: "normal"}, fromPath...)
	}
	if in != nil && in.Mode != r.BaseMode() {
		in.transition(r.BaseMode(), false)
	}
}

// restoreMode returns to a remembered mode (a modal's or a page's); the
// focused input follows it.
func (r *Runtime) restoreMode(m ModeState, leaving []string) {
	if m.Name == "" {
		m.Name = "normal"
	}
	r.setMode(m, leaving...)
	if in := r.Input(r.Focus); in != nil && in.Mode != r.BaseMode() {
		in.transition(r.BaseMode(), false)
	}
}

// SetFocus points the focus at one instance: an input, a list, a row, a
// page or a modal body. Without a modal, the focus must sit inside the
// current page; a hidden page's instances are addressed with an explicit
// target.
func (r *Runtime) SetFocus(id string) error {
	if err := r.validateFocus(id); err != nil {
		return err
	}
	r.setFocus(id)
	return nil
}

func (r *Runtime) validateFocus(id string) error {
	ancestry := r.Ancestry(id)
	if len(ancestry) == 0 || ancestry[len(ancestry)-1] != "app" {
		return fmt.Errorf("unknown instance %q", id)
	}
	if r.PageOf(r.Page) == nil {
		return nil
	}
	if len(r.modals) == 0 && (len(ancestry) < 2 || ancestry[len(ancestry)-2] != r.Page) {
		return errors.New("select a context inside the current page; open a modal for a modal input")
	}
	for _, step := range ancestry[:len(ancestry)-1] {
		if r.PageOf(step) != nil && step != r.Page {
			return fmt.Errorf("open page %q before selecting its context", step)
		}
	}
	return nil
}

// setFocus moves the focus, drops any key-sequence prefix and applies the
// mode rule.
func (r *Runtime) setFocus(id string) {
	from := r.Focus
	if from == id {
		return
	}
	r.CancelPendingBinding()
	stop, path := r.stopOf(from), r.Ancestry(from)
	r.Focus = id
	r.focusMoved(stop, path, id)
}

// setModalFocus moves the focus an open modal points at: the live focus for
// the top modal, else the return stored in the modal above it.
func (r *Runtime) setModalFocus(modal, id string) {
	i := slices.Index(r.modals, modal)
	switch {
	case i < 0:
		return
	case i == len(r.modals)-1:
		r.setFocus(id)
	default:
		r.Instances[r.modals[i+1]].Payload.Return = id
	}
}

// RetargetFocus points every focus reference that match accepts (the
// focus, a page's remembered focus, an open modal's return) at to. A
// component that recreates its focused child (the grid's cell, the list's
// row) calls it after the move; staying inside one focus stop, it keeps the
// mode.
func (r *Runtime) RetargetFocus(match func(id string) bool, to string) {
	for _, page := range r.PageList() {
		if page.Remembered != to && match(page.Remembered) {
			page.Remembered = to
		}
	}
	for _, id := range r.modals {
		if inst := r.Instances[id]; inst != nil && inst.Payload.Return != to && match(inst.Payload.Return) {
			inst.Payload.Return = to
		}
	}
	if r.Focus != to && match(r.Focus) {
		r.setFocus(to)
	}
}

// Push opens a modal with id focused on focus (app when empty). An id that
// is not already an instance becomes one of the modal noun for as long as
// it is open. The modal remembers the focus and mode it opened over.
func (r *Runtime) Push(id, focus string) error {
	if id == "" {
		return errors.New("modal ID is required")
	}
	if slices.Contains(r.modals, id) {
		return errors.New("modal is already open")
	}
	if focus == "" {
		focus = "app"
	}
	if len(r.Ancestry(focus)) == 0 {
		return fmt.Errorf("unknown instance %q", focus)
	}
	inst := r.Instances[id]
	if inst == nil {
		r.seq++
		inst = &Instance{ID: id, Noun: "modal", seq: r.seq}
		r.Instances[id] = inst
		r.modalInstances[id] = true
	}
	r.CancelPendingBinding()
	inst.Payload.Return = r.Focus
	inst.returnMode = ModeState{Name: r.BaseMode()}
	r.modals = append(r.modals, id)
	r.setFocus(focus)
	return nil
}

// ModalOption configures OpenModal.
type ModalOption func(*modalSpec)

type modalSpec struct {
	input *Input
	opts  []InstanceOption
}

// WithInput places an input inside the modal body and focuses it, so a
// one-field prompt is one call. The input is registered under the body (a
// Kind option names its noun; Parent is ignored) and is removed with the
// body on Pop; the *Input keeps its text and undo history and may be
// passed again to the next OpenModal.
func WithInput(in *Input, opts ...InstanceOption) ModalOption {
	return func(s *modalSpec) { s.input, s.opts = in, opts }
}

// OpenModal opens a modal whose body is a new instance of noun kind (its id
// is the kind), parented at the current focus and carrying payload, and
// focuses it (or the input WithInput places in it). The instance lives
// until Pop closes the modal. The kind's rows answer first, then the focus
// it opened over, then the modal noun.
func (r *Runtime) OpenModal(kind string, payload Payload, opts ...ModalOption) (*Instance, error) {
	if kind == "" {
		return nil, errors.New("modal kind is required")
	}
	if r.Instances[kind] != nil {
		return nil, fmt.Errorf("modal %q is already open", kind)
	}
	spec := modalSpec{}
	for _, opt := range opts {
		opt(&spec)
	}
	if spec.input != nil && r.Instances[spec.input.ID] != nil {
		return nil, fmt.Errorf("modal input %q is already registered", spec.input.ID)
	}
	r.Define(kind)
	r.seq++
	inst := &Instance{ID: kind, Noun: kind, Parent: r.Focus, Payload: payload, seq: r.seq}
	r.Instances[kind] = inst
	r.modalInstances[kind] = true
	focus := kind
	if spec.input != nil {
		if err := r.AddInput(spec.input, append(spec.opts, Parent(kind))...); err != nil {
			delete(r.Instances, kind)
			delete(r.modalInstances, kind)
			return nil, err
		}
		focus = spec.input.ID
	}
	if err := r.Push(kind, focus); err != nil {
		r.dropSubtree(kind)
		delete(r.modalInstances, kind)
		return nil, err
	}
	return inst, nil
}

// Pop closes the active modal and restores the focus and mode it opened
// over. A body the modal created is closed through the delivery contract
// and removed with what it holds.
func (r *Runtime) Pop() error {
	if len(r.modals) == 0 {
		return errors.New("no modal to close")
	}
	id := r.modals[len(r.modals)-1]
	inst := r.Instances[id]
	r.CancelPendingBinding()
	from := r.Focus
	stop, path := r.stopOf(from), r.Ancestry(from)
	back, mode := inst.Payload.Return, inst.returnMode
	r.modals = r.modals[:len(r.modals)-1]
	if r.modalInstances[id] {
		delete(r.modalInstances, id)
		r.dropSubtree(id)
	} else {
		inst.Payload.Return, inst.returnMode = "", ModeState{}
	}
	if r.Instances[back] == nil {
		back, mode = r.Page, ModeState{}
		if r.Instances[back] == nil {
			back = "app"
		}
	}
	r.Focus = back
	r.rememberLeaving(path, back)
	if mode.Name == "" {
		r.focusMoved(stop, path, back)
	} else {
		r.restoreMode(mode, path)
	}
	return nil
}

// subtree lists id and its descendants, descendants first.
func (r *Runtime) subtree(id string) []string {
	var out []string
	for _, child := range r.Children(id) {
		out = append(out, r.subtree(child.ID)...)
	}
	return append(out, id)
}

// dropSubtree closes and drops an instance and its descendants without
// touching focus references.
func (r *Runtime) dropSubtree(id string) {
	for _, one := range r.subtree(id) {
		r.closeInstance(one)
		delete(r.Instances, one)
		delete(r.rows, one)
	}
}

// RemoveInstance closes an instance and its descendants (components closed,
// jobs cancelled) and drops them. Every focus reference into the removed
// subtree (Focus, a page's remembered focus, an open modal's return) moves
// to the nearest surviving ancestor; an open modal inside the subtree is
// kept and reparented there. app cannot be removed.
func (r *Runtime) RemoveInstance(id string) error {
	inst := r.Instances[id]
	if inst == nil {
		return fmt.Errorf("unknown instance %q", id)
	}
	if id == "app" || inst.Parent == "" {
		return fmt.Errorf("instance %q has no parent to fall back to", id)
	}
	parent := inst.Parent
	gone := map[string]bool{}
	for _, one := range r.subtree(id) {
		if slices.Contains(r.modals, one) {
			continue
		}
		gone[one] = true
	}
	for _, modal := range r.modals {
		if m := r.Instances[modal]; m != nil && gone[m.Parent] {
			m.Parent = parent
		}
	}
	from := r.Focus
	stop, path := r.stopOf(from), r.Ancestry(from)
	for _, page := range r.PageList() {
		if gone[page.Remembered] {
			page.Remembered, page.mode = parent, ModeState{}
		}
	}
	for _, modal := range r.modals {
		if m := r.Instances[modal]; m != nil && gone[m.Payload.Return] {
			m.Payload.Return, m.returnMode = parent, ModeState{}
		}
	}
	for _, one := range r.subtree(id) {
		if !gone[one] {
			continue
		}
		r.closeInstance(one)
		delete(r.Instances, one)
		delete(r.rows, one)
	}
	if gone[from] {
		r.CancelPendingBinding()
		r.Focus = parent
		r.focusMoved(stop, path, parent)
	}
	return nil
}

// PageFocus is the focus a page shows: the live base focus while it is the
// current page, else the focus it remembers.
func (r *Runtime) PageFocus(id string) string {
	page := r.PageOf(id)
	switch {
	case page == nil:
		return ""
	case id == r.Page:
		return r.BaseFocus()
	}
	return page.Remembered
}

// rememberLeaving records, on every instance the focus is leaving (on the
// old path, not on the new one), where the focus was inside it, so a jump
// back to that stop or region lands there.
func (r *Runtime) rememberLeaving(fromPath []string, to string) {
	if len(fromPath) == 0 {
		return
	}
	staying := map[string]bool{}
	for _, id := range r.Ancestry(to) {
		staying[id] = true
	}
	for _, id := range fromPath[1:] {
		if inst := r.Instances[id]; inst != nil && !staying[id] {
			inst.lastFocus = fromPath[0]
		}
	}
}

// FocusTargeter is a component that knows where focus lands inside it: a
// list on its selected row, a grid on its cursor cell.
type FocusTargeter interface {
	FocusTarget() string
}

// focusRoot is the subtree focus jumps cycle through: the top modal, else
// the current page, else app.
func (r *Runtime) focusRoot() string {
	if top := r.TopModal(); top != "" {
		return top
	}
	if r.PageOf(r.Page) != nil {
		return r.Page
	}
	return "app"
}

// region reports whether an instance is a focus region.
func (r *Runtime) region(id string) bool {
	inst := r.Instances[id]
	if inst == nil {
		return false
	}
	noun := r.Nouns[inst.Noun]
	return noun != nil && noun.Region
}

// units lists, in tree order (children in added order, depth-first), the
// instances under root that unit accepts, without descending into one.
// order numbers every instance under root in the same walk.
func (r *Runtime) units(root string, unit func(string) bool) (out []string, order map[string]int) {
	order = map[string]int{}
	var walk func(id string, inside bool)
	walk = func(id string, inside bool) {
		for _, child := range r.Children(id) {
			order[child.ID] = len(order)
			if !inside && unit(child.ID) {
				out = append(out, child.ID)
				walk(child.ID, true)
				continue
			}
			walk(child.ID, inside)
		}
	}
	walk(root, false)
	return out, order
}

// landing is where a jump to a stop or region puts the focus: the
// component's own target, else the focus it last held inside, else (for a
// region) its first stop, else the instance itself.
func (r *Runtime) landing(id string) string {
	inst := r.Instances[id]
	if t, ok := inst.Component.(FocusTargeter); ok {
		if target := t.FocusTarget(); r.Instances[target] != nil {
			return target
		}
	}
	if last := inst.lastFocus; last != "" && last != id && slices.Contains(r.Ancestry(last), id) {
		return last
	}
	if r.region(id) {
		if stops, _ := r.units(id, r.focusable); len(stops) > 0 {
			return r.landing(stops[0])
		}
	}
	return id
}

// JumpFocus moves the focus steps stops (scope "stop") or regions (scope
// "region") forward, or backward for negative steps, through the focus
// root in tree order, wrapping. The current unit is the nearest stop (or
// region) on the focus path; the next one is the first after that unit's
// whole subtree. A stop is never descended into (a grid is one stop, its
// cells are not); the focus lands on the unit's remembered child.
func (r *Runtime) JumpFocus(steps int, scope string) error {
	unit := r.focusable
	switch scope {
	case "", "stop":
	case "region":
		unit = r.region
	default:
		return fmt.Errorf("unknown focus scope %q", scope)
	}
	root := r.focusRoot()
	ring, order := r.units(root, unit)
	if len(ring) == 0 || steps == 0 {
		return nil
	}
	current := ""
	for _, id := range r.Ancestry(r.Focus) {
		if id == root {
			break
		}
		if unit(id) {
			current = id
		}
	}
	index := slices.Index(ring, current)
	if index < 0 {
		// The focus sits outside every unit (on the page or a panel): start
		// from the units around it in tree order.
		at, known := order[r.Focus]
		index = len(ring)
		for i, id := range ring {
			if known && order[id] > at {
				index = i
				break
			}
		}
		if steps > 0 {
			index--
		}
	}
	n := len(ring)
	next := ((index+steps)%n + n) % n
	r.setFocus(r.landing(ring[next]))
	return nil
}

func registerFocus(r *Runtime) {
	jump := map[string]Parameter{"steps": {Type: "integer", Description: "How many stops or regions to move (the count; default 1)"}, "scope": {Type: "string", Description: "stop (default): component to component; region: panel to panel"}}
	r.Register(Operation{Verb: "next", Noun: "focus", Description: "Move the focus to the next focus stop (or region) in tree order, wrapping, landing on its remembered child", Parameters: jump, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.JumpFocus(max(1, IntArg(a, "steps", 1)), StringArg(a, "scope", "stop"))
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "previous", Noun: "focus", Description: "Move the focus to the previous focus stop (or region) in tree order, wrapping, landing on its remembered child", Parameters: jump, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.JumpFocus(-max(1, IntArg(a, "steps", 1)), StringArg(a, "scope", "stop"))
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "set", Noun: "focus", Description: "Focus an instance; the one way handlers move focus", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.SetFocus(StringArg(a, "id", ""))
		return r.Context(), err
	}})
	// Owner defaults: } { move stop to stop, ) ( region to region, tab and
	// shift+tab stop to stop; all take a count. An inner level (a
	// multiline editor's insert table, a palette field) binds its own.
	steps := func(op string, args ...Arguments) Binding {
		return Do(op, args...).FromPayload("steps", SourceCount)
	}
	region := Arguments{"scope": "region"}
	for _, mode := range []string{"normal", "count_pending"} {
		r.Define("app").Mode(mode).
			Bind("}", steps("focus.next")).
			Bind("{", steps("focus.previous")).
			Bind(")", steps("focus.next", region)).
			Bind("(", steps("focus.previous", region)).
			Bind("tab", steps("focus.next")).
			Bind("shift+tab", steps("focus.previous"))
	}
}
