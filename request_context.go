package ochazuke

import "slices"

// RequestContext is what one running request carries besides its
// arguments: the instance it acts for, the focus it was asked at, the
// subjects execute resolved for it, whether a binding row made it, and the
// mode it started in. execute builds one per request and hands it down on
// Context.Request, so a handler's inputs travel with the request (Ctx
// reads them) rather than in runtime fields set around the call and put
// back after (G13). A nested request builds its own; the outer one's is
// never overwritten.
type RequestContext struct {
	// ActsFor is the instance the request acts for: its target, else the
	// focus, after it moved to its noun's instance. A modal opens from it
	// only at the top level (openFromTopLevel).
	ActsFor string `json:"actsFor,omitempty"`
	// AskedAt is the focus the request was made at, before it moved to its
	// noun's instance: create fills a reference's thing from there
	// (createFill).
	AskedAt string `json:"askedAt,omitempty"`
	// FromRow marks a binding row's own request: runRow sets it on the
	// Context it executes in, and execute's level rule targets the level
	// the row matched at rather than its noun's nearest instance.
	FromRow bool `json:"fromRow,omitempty"`
	// Subjects are the ids execute resolved for the handler about to run
	// (Ctx.Subjects).
	Subjects []string `json:"subjects,omitempty"`
	// Mode is the mode the request started in. A replayed verb (a remote
	// store's standard rows run again over the tape) and the standard rows
	// read it through Ctx, never by assigning Runtime.Mode.
	Mode ModeState `json:"mode"`
}

// rowMade reports whether rc is a binding row's own request (FromRow);
// false for no request context.
func (rc *RequestContext) rowMade() bool { return rc != nil && rc.FromRow }

// requestOf is c's request context, nil outside a request.
func requestOf(c Context) *RequestContext { return c.Request }

// mode is the mode ctx's request started in, else the live mode (a Ctx
// built outside a request: a completion, a refresh).
func (ctx Ctx) mode() ModeState {
	if rc := requestOf(ctx.Context); rc != nil && rc.Mode.Name != "" {
		return rc.Mode
	}
	return ctx.Runtime.Mode
}

// withRequestMode is ctx with its request context's mode set to mode, on a
// copy of the request context (the request's own is not changed).
func (ctx Ctx) withRequestMode(mode ModeState) Ctx {
	rc := RequestContext{}
	if at := requestOf(ctx.Context); at != nil {
		rc = *at
	}
	rc.Mode = mode
	ctx.Context.Request = &rc
	return ctx
}

// narrowedTo is the Context of instance id that keeps c's request context:
// a run narrowed to one subject is still the same request.
func (r *Runtime) narrowedTo(c Context, id string) Context {
	out := r.contextOf(id)
	out.Request = c.Request
	return out
}

// actingFor opens a scope that acts for id until end: a job completion for
// its owner, a chooser's callback for the restored focus. It pushes a
// request context of its own over the running ones; no running request's
// context is changed.
func (r *Runtime) actingFor(id string) (end func()) {
	return r.enterRequest(&RequestContext{ActsFor: id})
}

// enterRequest pushes rc as the innermost running request context until
// end pops it.
func (r *Runtime) enterRequest(rc *RequestContext) (end func()) {
	r.history.running = append(r.history.running, rc)
	n := len(r.history.running)
	return func() {
		r.history.running = r.history.running[:n-1]
	}
}

// actsFor is the instance the innermost running request or scope acts
// for, "" when none runs.
func (r *Runtime) actsFor() string {
	if n := len(r.history.running); n > 0 {
		return r.history.running[n-1].ActsFor
	}
	return ""
}

// RunningRequests are copies of the running requests' contexts and the
// acting scopes inside them, outermost first: for inspection.
func (r *Runtime) RunningRequests() []RequestContext {
	out := make([]RequestContext, 0, len(r.history.running))
	for _, rc := range r.history.running {
		c := *rc
		c.Subjects = slices.Clone(rc.Subjects)
		out = append(out, c)
	}
	return out
}
