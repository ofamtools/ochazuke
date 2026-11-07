package ochazuke

import "slices"

// The interaction accessors read the live focus and mode, and the
// interaction a page or modal is drawn in, as detached values: nothing a
// caller does to a result reaches the runtime. Runtime.Mode and
// Runtime.Focus stay exported and readable (the owner's 2026-11-07
// decision); their writers are guarded by TestStateWriteOwnership.
// Dispatch and domain handlers read the live accessors; drawing code asks
// for the surface it draws (PresentationAt, DrawnFocus, a component's
// DrawnFocused). Nothing assigns the fields for a frame: a page beneath a
// modal is drawn by reading its presentation, never by borrowing the
// modal's saved return into the live state.

// CurrentMode is the live interaction mode as a detached value: its name
// and payload, with the payload's stored request (its arguments, subjects,
// value and actor) and macro shadow copied, so changing the result never
// changes the runtime. Reading it never transitions.
func (r *Runtime) CurrentMode() ModeState { return cloneModeState(r.Mode) }

// FocusID is the live focused instance: the top modal's focus while a
// modal is open, else the page's.
func (r *Runtime) FocusID() string { return r.Focus }

// Presentation is the interaction one surface (a page or an open modal) is
// drawn in, detached from the runtime. For the top surface it is the live
// focus and mode; for a surface beneath an open modal it is what the modal
// directly above it saved when it opened (the focus and base mode Pop
// restores); for a page that is not current it is what the page
// remembers. It is read-only: asking for it never moves the focus, changes
// the mode or installs an override, and changing it changes nothing.
type Presentation struct {
	// Surface is the page or modal instance the view describes, "app" for
	// an instance on no page and under no modal; "" when the id asked
	// for is not an instance.
	Surface string `json:"surface"`
	// Focus is the instance the surface shows as focused.
	Focus string `json:"focus"`
	// Mode is the mode the surface is drawn in: the live mode on the top
	// surface, else the saved base mode (the mode the saved focus declares,
	// Runtime.DeclaredMode, when nothing was saved).
	Mode ModeState `json:"mode"`
	// Context is Focus's context (its path and input).
	Context Context `json:"context"`
	// Modals are the open modals up to and including the surface,
	// outermost first: the modal boundary visible at it. Empty for a page.
	Modals []string `json:"modals,omitempty"`
	// Live reports that the view is the live interaction: the surface is
	// the top modal, or the current page while no modal is open.
	Live bool `json:"live"`
}

// PresentationAt is the detached interaction view of the surface that
// holds id: id itself when it is an open modal or a page, else the
// innermost open modal on its ancestry, else its page. Components pass
// their own registered id; an application renderer passes the page or
// region it draws. The modal directly above the surface supplies its saved
// focus and mode; the current page beneath open modals reads the
// outermost modal's return; another page reads its remembered state. A
// saved return without a mode (a return repaired after its focus was
// removed) reads the mode that focus declares, which is what closing the
// modal lands in. Rendering reads the same rule (DrawnFocus, the
// components' DrawnFocused), so what a frame draws is what PresentationAt
// reports.
func (r *Runtime) PresentationAt(id string) Presentation {
	if r.Instances[id] == nil {
		return Presentation{}
	}
	surface, depth, focus, mode, live := r.drawnAt(id)
	view := Presentation{Surface: surface, Focus: focus, Mode: cloneModeState(mode), Live: live, Context: r.contextOf(focus)}
	if depth > 0 {
		view.Modals = slices.Clone(r.modals[:depth])
	}
	return view
}

// DrawnFocus is the focus the surface holding id is drawn with:
// PresentationAt(id).Focus without building the view. Drawing code (a
// component's highlight, cursor, scroll and focused styles; an
// application's page renderer) compares against it; dispatch and domain
// handlers read the live FocusID. An id that is not an instance reads the
// live focus.
func (r *Runtime) DrawnFocus(id string) string {
	_, _, focus, _, _ := r.drawnAt(id)
	return focus
}

// DrawnBaseMode is the base mode the surface holding id is drawn in:
// PresentationAt(id).Mode as BaseMode reads it (a pending mode or a
// key-sequence prefix stands in the mode it returns to).
func (r *Runtime) DrawnBaseMode(id string) string {
	return r.baseModeOf(r.drawnMode(id))
}

// drawnMode is the mode the surface holding id is drawn in
// (PresentationAt(id).Mode), not copied: for drawing code that only reads
// it.
func (r *Runtime) drawnMode(id string) ModeState {
	_, _, _, mode, _ := r.drawnAt(id)
	return mode
}

// drawnAt is PresentationAt's one rule: the surface holding id, how many
// open modals are visible at it (0 for a page), the focus and mode it is
// drawn in (the mode not copied) and whether that is the live
// interaction. It allocates nothing on the common paths, so drawing code
// may ask once per component per frame. An unknown id reads live.
func (r *Runtime) drawnAt(id string) (surface string, depth int, focus string, mode ModeState, live bool) {
	if r.Instances[id] == nil {
		return "", len(r.modals), r.Focus, r.Mode, true
	}
	modal, page := r.surfaceOf(id)
	if modal >= 0 {
		surface, depth = r.modals[modal], modal+1
		if modal == len(r.modals)-1 {
			return surface, depth, r.Focus, r.Mode, true
		}
		above := r.ModalReturns[r.modals[modal+1]]
		focus, mode = r.savedInteraction(above.Focus, above.Mode)
		return surface, depth, focus, mode, false
	}
	switch {
	case page != nil && page.ID != r.Page:
		focus := page.Remembered
		if r.Instances[focus] == nil {
			focus = page.ID
		}
		focus, mode = r.savedInteraction(focus, page.mode)
		return page.ID, 0, focus, mode, false
	case page != nil:
		surface = page.ID
	case r.Page != "":
		surface = r.Page
	default:
		surface = "app"
	}
	if len(r.modals) == 0 {
		return surface, 0, r.Focus, r.Mode, true
	}
	outer := r.ModalReturns[r.modals[0]]
	focus, mode = r.savedInteraction(outer.Focus, outer.Mode)
	return surface, 0, focus, mode, false
}

// surfaceOf walks id's ancestry (id itself included) once: the index in
// the open modals of the innermost modal on it (-1 for none) and, when no
// modal holds it, the nearest page on it.
func (r *Runtime) surfaceOf(id string) (modal int, page *Page) {
	modal = -1
	for steps := len(r.Instances); id != "" && steps > 0; steps-- {
		inst := r.Instances[id]
		if inst == nil {
			break
		}
		if len(r.modals) > 0 {
			if i := slices.Index(r.modals, id); i >= 0 {
				return i, nil
			}
		}
		if page == nil {
			page = r.PageOf(id)
		}
		id = inst.Parent
	}
	return -1, page
}

// savedInteraction is a saved focus and mode as a surface is drawn in it:
// a saved return without a mode reads the mode its focus declares.
func (r *Runtime) savedInteraction(focus string, mode ModeState) (string, ModeState) {
	if mode.Name == "" {
		mode = ModeState{Name: r.DeclaredMode(focus)}
	}
	return focus, mode
}

// cloneModeState is m with nothing shared: the payload's stored request
// (its arguments and value deep-copied as JSON-shaped data, its subjects
// and actor chain copied) and its shadow mode, recursively.
func cloneModeState(m ModeState) ModeState {
	m.Payload.Request = cloneRequestPtr(m.Payload.Request)
	if m.Payload.Shadow != nil {
		shadow := cloneModeState(*m.Payload.Shadow)
		m.Payload.Shadow = &shadow
	}
	return m
}

func cloneRequestPtr(q *Request) *Request {
	if q == nil {
		return nil
	}
	out := *q
	if q.Args != nil {
		out.Args = cloneRenderValue(q.Args).(Arguments)
	}
	out.Value = cloneRenderValue(q.Value)
	out.Subjects = slices.Clone(q.Subjects)
	out.Actor = cloneActor(q.Actor)
	return &out
}

func cloneActor(a *Actor) *Actor {
	if a == nil {
		return nil
	}
	out := *a
	out.Via = cloneActor(a.Via)
	return &out
}
