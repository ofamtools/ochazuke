package ochazuke

import (
	"errors"
	"slices"
	"strconv"
)

// Quit hooks: the work quitting does before the program leaves, as a list
// of registered hooks rather than a verb per caller. app.quit is the one
// verb that quits (app.restart runs it); while it quits the Model asks
// each hook's Wait what it still waits for (a side pane still opening),
// and keeps the program running, saying the first reason, until every
// hook answers "" or the quit is forced (app.quit {force: true}, or a
// second quit quit.force-after or more later). Close is the application's
// cleanup once the terminal is released when the program does not return
// to its caller: a restart runs every hook's Close, in order, before it
// starts the installed binary (an error stops the restart and is
// printed). A program that quits returns from the Model and runs its own
// cleanup (Runtime.CloseQuitHooks runs the same Closes when it wants
// them).
//
// A hook's Wait runs on the loop on every update while quitting, so it
// reads state, never IO.

// QuitHook is one piece of before-quit work. Name identifies it (a second
// hook of the same name replaces the first; none gets "quit-hook-N");
// Wait and Close are each optional.
type QuitHook struct {
	Name string
	// Wait answers what quitting still waits for, said in the status line
	// ("quitting once the side pane finishes opening"); "" when it waits
	// for nothing.
	Wait func(r *Runtime) string
	// Close is the cleanup before a restart's exec.
	Close func() error
}

// quitHookState is the registered hooks and whether their Closes ran.
type quitHookState struct {
	hooks  []QuitHook
	closed bool
	err    error
}

// AddQuitHook registers a quit hook; one of the same name is replaced in
// place.
func (r *Runtime) AddQuitHook(h QuitHook) {
	q := &r.eventHub().quitHooks
	if h.Name == "" {
		h.Name = "quit-hook-" + strconv.Itoa(len(q.hooks)+1)
	}
	if i := slices.IndexFunc(q.hooks, func(o QuitHook) bool { return o.Name == h.Name }); i >= 0 {
		q.hooks[i] = h
		return
	}
	q.hooks = append(q.hooks, h)
}

// RemoveQuitHook takes the hook named name off; false when there is none.
func (r *Runtime) RemoveQuitHook(name string) bool {
	q := &r.eventHub().quitHooks
	n := len(q.hooks)
	q.hooks = slices.DeleteFunc(q.hooks, func(o QuitHook) bool { return o.Name == name })
	return len(q.hooks) != n
}

// QuitHooks names the registered quit hooks, in the order they run.
func (r *Runtime) QuitHooks() []string {
	var out []string
	for _, h := range r.eventHub().quitHooks.hooks {
		out = append(out, h.Name)
	}
	return out
}

// quitWaitingFor is the first reason a quit hook still waits, "" when none
// does.
func (r *Runtime) quitWaitingFor() string {
	for _, h := range r.eventHub().quitHooks.hooks {
		if h.Wait == nil {
			continue
		}
		if why := h.Wait(r); why != "" {
			return why
		}
	}
	return ""
}

// CloseQuitHooks runs every quit hook's Close once, in order, and answers
// their errors joined; a second call answers the first's.
func (r *Runtime) CloseQuitHooks() error {
	q := &r.eventHub().quitHooks
	if q.closed {
		return q.err
	}
	q.closed = true
	var errs []error
	for _, h := range q.hooks {
		if h.Close != nil {
			errs = append(errs, h.Close())
		}
	}
	q.err = errors.Join(errs...)
	return q.err
}
