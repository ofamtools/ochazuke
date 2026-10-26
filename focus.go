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
	if top := r.TopModal(); top != "" && !slices.Contains(ancestry, top) {
		return errors.New("focus is outside the open modal")
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
	id = r.clampFocus(id)
	from := r.Focus
	if from == id {
		return
	}
	r.CancelPendingBinding()
	stop, path := r.stopOf(from), r.Ancestry(from)
	r.Focus = id
	r.selectFocusedRow(id)
	r.focusMoved(stop, path, id)
}

// selectFocusedRow makes a focused list row its list's selection (by its
// stable id), so the highlight, List.Selected and the keys that answer
// agree however the focus got there: focus.set, RetargetFocus, a jump, a
// FocusOnClose landing. Moving within the list already selects first.
func (r *Runtime) selectFocusedRow(id string) {
	inst := r.Instances[id]
	if inst == nil {
		return
	}
	if l := r.List(inst.Parent); l != nil {
		l.selectID(id)
	}
}

// errModalAbove refuses a modal opened from beneath the top modal.
var errModalAbove = errors.New("a modal is open above this level")

// actingFor records the instance the running request (its target, else the
// focus) or job completion acts for until restore.
func (r *Runtime) actingFor(id string) (restore func()) {
	before := r.origin
	r.origin = id
	return func() { r.origin = before }
}

// openFromTopLevel refuses opening a modal while another is open unless the
// opener (the running request's target or focus, a completion's owner, else
// the focus) is inside the top modal: nesting from the top is allowed, a
// lower level cannot stack a modal over the one above it (first wins). A
// modal body reopening itself (the palette restoring after a failed run)
// is its own level and allowed.
func (r *Runtime) openFromTopLevel(id string) error {
	top := r.TopModal()
	if top == "" {
		return nil
	}
	origin := r.origin
	if origin == "" {
		origin = r.Focus
	}
	if ancestry := r.Ancestry(origin); !slices.Contains(ancestry, top) && (id == "" || !slices.Contains(ancestry, id)) {
		return errModalAbove
	}
	return nil
}

// focusInTopLevel reports the focus invariant: the focus is inside the top
// level, the top modal's subtree while a modal is open (else the current
// page, else app).
func (r *Runtime) focusInTopLevel() bool {
	return slices.Contains(r.Ancestry(r.Focus), r.focusRoot())
}

// clampFocus keeps an internal focus move inside the top modal: a target
// outside it becomes the modal instance. Explicit focus.set is refused by
// validateFocus instead.
func (r *Runtime) clampFocus(id string) string {
	if top := r.TopModal(); top != "" && !slices.Contains(r.Ancestry(id), top) {
		return top
	}
	return id
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
	if err := r.openFromTopLevel(id); err != nil {
		return err
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
		inst = &Instance{ID: id, Noun: "modal", Parent: "app", seq: r.seq}
		r.Instances[id] = inst
		r.modalInstances[id] = true
	}
	r.CancelPendingBinding()
	inst.Payload.Return = r.Focus
	inst.returnMode = ModeState{Name: r.BaseMode()}
	r.modals = append(r.modals, id)
	// The focus is never outside the top modal: a focus that is not in the
	// body's subtree is adopted under it while it is open (its own parent
	// comes back on Pop); one the body sits beneath (app) becomes the body.
	if ancestry := r.Ancestry(focus); !slices.Contains(ancestry, id) {
		if slices.Contains(r.Ancestry(id), focus) {
			focus = id
		} else {
			target := r.Instances[focus]
			inst.borrowed, inst.lentParent = focus, target.Parent
			target.Parent = id
		}
	}
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
	if err := r.openFromTopLevel(""); err != nil {
		return nil, err
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
	if lent := r.Instances[inst.borrowed]; lent != nil && lent.Parent == id {
		lent.Parent = inst.lentParent
		if r.Instances[lent.Parent] == nil {
			lent.Parent = "app"
		}
	}
	inst.borrowed, inst.lentParent = "", ""
	if r.modalInstances[id] {
		if to := r.closeFocusOf(from, r.subtreeSet(id)); to != "" {
			back, mode = to, ModeState{}
		}
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
	back = r.clampFocus(back)
	r.Focus = back
	r.selectFocusedRow(back)
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
	return r.subtreeFrom(id, r.childIndex())
}

// subtreeFrom is subtree over a childIndex taken earlier: a removal of many
// instances (SetItems dropping rows) takes the index once instead of
// scanning every instance per removal. A child the index lists is walked
// only while it is still registered under that parent, so an index taken
// before earlier removals and reparentings in the same batch stays exact.
func (r *Runtime) subtreeFrom(id string, kids map[string][]*Instance) []string {
	var out []string
	var walk func(string)
	walk = func(id string) {
		for _, child := range kids[id] {
			if r.Instances[child.ID] == child && child.Parent == id {
				walk(child.ID)
			}
		}
		out = append(out, id)
	}
	walk(id)
	return out
}

// subtreeSet is subtree as a set.
func (r *Runtime) subtreeSet(id string) map[string]bool {
	out := map[string]bool{}
	for _, one := range r.subtree(id) {
		out[one] = true
	}
	return out
}

// closeFocusOf is the FocusOnClose target for a focus at from when the
// instances in gone are removed: the innermost removed instance on from's
// path that names a target which survives. Empty means the default.
func (r *Runtime) closeFocusOf(from string, gone map[string]bool) string {
	for _, id := range r.Ancestry(from) {
		if !gone[id] {
			continue
		}
		to := r.Instances[id].closeFocus
		if to != "" && r.Instances[to] != nil && !gone[to] {
			return to
		}
	}
	return ""
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
// to the nearest surviving ancestor (the Focus itself to a removed
// instance's FocusOnClose target when that survives); an open modal inside the subtree is
// kept and reparented there. app cannot be removed.
func (r *Runtime) RemoveInstance(id string) error {
	return r.removeInstance(id, r.childIndex(), r.PageList())
}

// removeInstances is RemoveInstance of each id in order, with one
// childIndex and one PageList for the whole batch: the instances removed
// must not include a page (rows of a list, never app's children), so the
// page list stays the same throughout. Errors are ignored, as SetItems
// ignores them.
func (r *Runtime) removeInstances(ids []string) {
	if len(ids) == 0 {
		return
	}
	kids, pages := r.childIndex(), r.PageList()
	for _, id := range ids {
		_ = r.removeInstance(id, kids, pages)
	}
}

func (r *Runtime) removeInstance(id string, kids map[string][]*Instance, pages []*Page) error {
	inst := r.Instances[id]
	if inst == nil {
		return fmt.Errorf("unknown instance %q", id)
	}
	if id == "app" || inst.Parent == "" {
		return fmt.Errorf("instance %q has no parent to fall back to", id)
	}
	parent := inst.Parent
	gone := map[string]bool{}
	for _, one := range r.subtreeFrom(id, kids) {
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
	back := parent
	if gone[from] {
		if to := r.closeFocusOf(from, gone); to != "" {
			back = to
		}
	}
	for _, page := range pages {
		if gone[page.Remembered] {
			page.Remembered, page.mode = parent, ModeState{}
		}
	}
	for _, modal := range r.modals {
		if m := r.Instances[modal]; m != nil && gone[m.Payload.Return] {
			m.Payload.Return, m.returnMode = parent, ModeState{}
		}
	}
	for _, one := range r.subtreeFrom(id, kids) {
		if !gone[one] {
			continue
		}
		r.closeInstance(one)
		delete(r.Instances, one)
		delete(r.rows, one)
	}
	if gone[from] {
		r.CancelPendingBinding()
		back = r.clampFocus(back)
		r.Focus = back
		r.selectFocusedRow(back)
		r.focusMoved(stop, path, back)
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

// units lists the instances under root that unit accepts, without
// descending into one, in drawing order: the order the page holding root
// drew them in its last frame (Placed), a unit ranked by the first drawn
// instance in its subtree. Before the page's first frame (or in the CLI,
// where none is drawn) every unit counts, in tree order (children in added
// order, depth-first); once a frame drew any of them, a unit the last frame
// did not draw (hidden, or drawn by no Placed call) is skipped, except in
// a modal, whose stops the page frame never records. order ranks every instance
// under root the same way. No map is iterated: the walk is the tree's and
// the frame's slices.
func (r *Runtime) units(root string, unit func(string) bool) (out []string, order map[string]int) {
	var tree []string
	kids := r.childIndex()
	var walk func(id string, inside bool)
	walk = func(id string, inside bool) {
		for _, child := range kids[id] {
			tree = append(tree, child.ID)
			if !inside && unit(child.ID) {
				out = append(out, child.ID)
				walk(child.ID, true)
				continue
			}
			walk(child.ID, inside)
		}
	}
	walk(root, false)
	drawn := r.drawnUnder(root)
	order = make(map[string]int, len(tree))
	for i, id := range tree {
		order[id] = len(drawn) + i
	}
	for i, placed := range drawn {
		for _, at := range r.Ancestry(placed.ID) {
			if at == root {
				break
			}
			if rank, ok := order[at]; ok && rank > i {
				order[at] = i
			}
		}
	}
	if len(drawn) > 0 && !r.insideModal(root) {
		// Once the page has drawn a frame, a unit it did not draw is not
		// on screen (a hidden panel, a collapsed pane): jumps and spills
		// skip it rather than land the focus somewhere invisible.
		out = slices.DeleteFunc(out, func(id string) bool { return order[id] >= len(drawn) })
	}
	slices.SortStableFunc(out, func(a, b string) int { return order[a] - order[b] })
	return out, order
}

// insideModal reports whether id is an open modal or beneath one. A modal's
// stops are not recorded in its page's frame, so they are never skipped as
// undrawn.
func (r *Runtime) insideModal(id string) bool {
	for _, at := range r.Ancestry(id) {
		if slices.Contains(r.modals, at) {
			return true
		}
	}
	return false
}

// drawnUnder is the last frame's drawing order of the page holding root
// (root itself or an ancestor), nil when no page holds it or none was drawn.
func (r *Runtime) drawnUnder(root string) []Placement {
	for _, id := range r.Ancestry(root) {
		if page := r.PageOf(id); page != nil {
			return page.drawn
		}
	}
	return nil
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
// root in drawing order (units), wrapping. The current unit is the nearest stop (or
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
		// from the units around it in drawing order.
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
	r.Register(Operation{Verb: "next", Noun: "focus", Description: "Move the focus to the next focus stop (or region) in drawing order, wrapping, landing on its remembered child", Parameters: jump, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.JumpFocus(max(1, IntArg(a, "steps", 1)), StringArg(a, "scope", "stop"))
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "previous", Noun: "focus", Description: "Move the focus to the previous focus stop (or region) in drawing order, wrapping, landing on its remembered child", Parameters: jump, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.JumpFocus(-max(1, IntArg(a, "steps", 1)), StringArg(a, "scope", "stop"))
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "set", Noun: "focus", Description: "Focus an instance; the one way handlers move focus", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.SetFocus(StringArg(a, "id", ""))
		return r.Context(), err
	}})
	// Owner defaults: } { move stop to stop, ) ( region to region, tab and
	// shift+tab stop to stop; all take a count (the app's count_pending
	// extends normal). An inner level (a multiline editor's insert table, a
	// palette field) binds its own.
	steps := func(op string, args ...Arguments) Binding {
		return Do(op, args...).FromPayload("steps", SourceCount)
	}
	region := Arguments{"scope": "region"}
	r.Define("app").Mode("normal").
		Bind("}", steps("focus.next")).
		Bind("{", steps("focus.previous")).
		Bind(")", steps("focus.next", region)).
		Bind("(", steps("focus.previous", region)).
		Bind("tab", steps("focus.next")).
		Bind("shift+tab", steps("focus.previous"))
}
