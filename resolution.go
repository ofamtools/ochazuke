package ochazuke

import "slices"

// resolutionContext is the interaction a binding walk resolves in: the
// focus it starts at, the mode its rows answer in and the modal chain that
// bounds it. Live dispatch and the live listings read the runtime's own
// (liveResolution). A query that asks "what would answer here" for a focus
// or mode that is not the live one (bindings.stops, the typing check, help
// at the place it opened over) builds its own and passes it down, so it
// never assigns Runtime.Mode, Runtime.Focus or the modal chain, reads no
// binding cache and fills none. Its guards see it on KeyEvent.Within
// (owner question 5 of the mode-ownership plan: a hypothetical guard reads
// the event, the runtime's accessors stay live).
type resolutionContext struct {
	// Focus is the instance the walk starts at.
	Focus string
	// Mode is the mode the rows answer in, with its payload.
	Mode ModeState
	// Modals is the open modal chain the walk is bounded by, outermost
	// first.
	Modals []string
	// Hypothetical marks a query's own interaction: guards read it on
	// KeyEvent.Within, and the walk neither reads nor fills the binding
	// and help caches. False for the live interaction.
	Hypothetical bool
}

// liveResolution is the live interaction read at focus: the live mode and
// modal chain. Not hypothetical: guards read the runtime itself.
func (r *Runtime) liveResolution(focus string) resolutionContext {
	return resolutionContext{Focus: focus, Mode: r.Mode, Modals: r.modals}
}

// queryResolution is a hypothetical interaction for a query: focus in
// mode under the given modal chain, detached from the runtime (the mode
// and the chain are copies).
func queryResolution(focus string, mode ModeState, modals []string) resolutionContext {
	return resolutionContext{Focus: focus, Mode: cloneModeState(mode), Modals: slices.Clone(modals), Hypothetical: true}
}

// within is the context guards read on KeyEvent.Within: the context itself
// for a query, nil for the live interaction.
func (c *resolutionContext) within() *resolutionContext {
	if !c.Hypothetical {
		return nil
	}
	return c
}

// rowsIn is levelRows in a context: the rows its walk answers with, the
// first per key or pattern. The live context reads the binding cache; a
// hypothetical one reads none.
func (r *Runtime) rowsIn(ctx resolutionContext) []levelRow {
	return r.rowsInMode(ctx, ctx.Mode)
}

// rowsInMode is rowsIn with the rows answering in mode over ctx's walk
// (help's pending-mode expansion: the walk stays the context's, the rows
// are the entered mode's).
func (r *Runtime) rowsInMode(ctx resolutionContext, mode ModeState) []levelRow {
	return r.scanRowsWithin(r.walkIn(ctx), mode, ctx.Focus, ctx.within())
}

// listBindingsIn is ListBindings in a context.
func (r *Runtime) listBindingsIn(ctx resolutionContext) []Binding {
	rows := r.rowsIn(ctx)
	out := make([]Binding, len(rows))
	for i, row := range rows {
		out[i] = row.Binding
	}
	return sortListedBindings(r.withAliases(out))
}

// EventFocus is the focus an event is judged from: the query's focus when
// a query judges it hypothetically (KeyEvent.Within), else the live focus.
// A guard that compares with the focused instance (not the instance the
// row acts at, KeyEvent.Focus) reads it rather than Runtime.Focus, so
// bindings.stops, the typing check and help read it as that focus would.
func (r *Runtime) EventFocus(ev KeyEvent) string {
	if ev.Within != nil {
		return ev.Within.Focus
	}
	return r.Focus
}

// EventModals is the open modal chain an event is judged under,
// outermost first: the query's (KeyEvent.Within), else the live one. A
// guard on "no modal is open" reads it rather than Runtime.Modals, so help
// at the place it opened over does not count the help itself. The result
// is a copy.
func (r *Runtime) EventModals(ev KeyEvent) []string {
	if ev.Within != nil {
		return slices.Clone(ev.Within.Modals)
	}
	return r.modalIDs()
}

// EventTopModal is the innermost modal of EventModals, "" when none.
func (r *Runtime) EventTopModal(ev KeyEvent) string {
	modals := r.modals
	if ev.Within != nil {
		modals = ev.Within.Modals
	}
	if len(modals) == 0 {
		return ""
	}
	return modals[len(modals)-1]
}
