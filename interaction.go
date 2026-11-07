package ochazuke

import "slices"

// The interaction accessors read the live focus and mode, and the
// interaction a page or modal is drawn in, as detached values: nothing a
// caller does to a result reaches the runtime. They are the reads that
// replace the exported Runtime.Mode and Runtime.Focus fields (which still
// compile; hiding them is a later step of the mode-ownership plan).
// Dispatch and domain handlers read the live accessors; drawing code asks
// PresentationAt for the surface it draws.

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
// outermost modal's return; another page reads its remembered state.
func (r *Runtime) PresentationAt(id string) Presentation {
	if r.Instances[id] == nil {
		return Presentation{}
	}
	if i := r.surfaceModal(id); i >= 0 {
		view := Presentation{Surface: r.modals[i], Modals: slices.Clone(r.modals[:i+1])}
		if i == len(r.modals)-1 {
			return r.liveView(view)
		}
		above := r.ModalReturns[r.modals[i+1]]
		return r.savedView(view, above.Focus, above.Mode)
	}
	page := r.surfacePage(id)
	switch {
	case page != nil && page.ID != r.Page:
		focus := page.Remembered
		if r.Instances[focus] == nil {
			focus = page.ID
		}
		return r.savedView(Presentation{Surface: page.ID}, focus, page.mode)
	case page != nil:
		id = page.ID
	default:
		id = "app"
		if r.Page != "" {
			id = r.Page
		}
	}
	view := Presentation{Surface: id}
	if len(r.modals) == 0 {
		return r.liveView(view)
	}
	outer := r.ModalReturns[r.modals[0]]
	return r.savedView(view, outer.Focus, outer.Mode)
}

// surfaceModal is the index in the open modals of the innermost one on
// id's ancestry (id itself included), -1 for none.
func (r *Runtime) surfaceModal(id string) int {
	for _, step := range r.Ancestry(id) {
		if i := slices.Index(r.modals, step); i >= 0 {
			return i
		}
	}
	return -1
}

// surfacePage is the nearest page on id's ancestry (id itself included).
func (r *Runtime) surfacePage(id string) *Page {
	for _, step := range r.Ancestry(id) {
		if page := r.PageOf(step); page != nil {
			return page
		}
	}
	return nil
}

func (r *Runtime) liveView(view Presentation) Presentation {
	view.Focus, view.Mode, view.Live = r.Focus, cloneModeState(r.Mode), true
	view.Context = r.contextOf(r.Focus)
	return view
}

func (r *Runtime) savedView(view Presentation, focus string, mode ModeState) Presentation {
	view.Focus = focus
	view.Mode = cloneModeState(mode)
	if view.Mode.Name == "" {
		view.Mode = ModeState{Name: r.DeclaredMode(focus)}
	}
	view.Context = r.contextOf(focus)
	return view
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
