package ochazuke

import (
	"context"
	"sync"
	"sync/atomic"
)

// lifecycle is what the runtime started that outlives one request: the
// context every job's context is made under, and the stop of every store
// watch running now. Shutdown ends both; its methods are safe from any
// goroutine (a signal handler's), unlike the rest of the runtime.
type lifecycle struct {
	ctx  context.Context
	end  context.CancelFunc
	shut atomic.Bool

	mu    sync.Mutex
	stops map[*Watch]func()
}

func newLifecycle() *lifecycle {
	ctx, end := context.WithCancel(context.Background())
	return &lifecycle{ctx: ctx, end: end, stops: map[*Watch]func(){}}
}

// Shutdown stops everything the runtime started, for a program that is
// closing: every running job's context is cancelled (their work sees
// ctx.Done, a process it runs is stopped as a cancelled job's is), every
// store watch's stop runs (Runtime.Watch, the watch verb, the watches lists
// and related sections keep), and no watch reads again. Jobs launched
// after it start with their context done. It may be called more than once
// and from any goroutine (a signal handler's): it touches nothing the
// update loop owns. Quitting the Model does not call it; the program calls
// it once the Model has returned (or on a signal), after Config.Flush.
func (r *Runtime) Shutdown() {
	l := r.life
	if l == nil || !l.shut.CompareAndSwap(false, true) {
		return
	}
	l.end()
	l.mu.Lock()
	stops := make([]func(), 0, len(l.stops))
	for w, stop := range l.stops {
		stops = append(stops, stop)
		delete(l.stops, w)
	}
	l.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}

// ShuttingDown reports whether Shutdown has been called.
func (r *Runtime) ShuttingDown() bool { return r.life != nil && r.life.shut.Load() }

// Lifetime is the runtime's own context: done once Shutdown is called.
// Every job's context is made under it; an application derives the
// contexts of the work it starts outside jobs from it to end with the
// runtime.
func (r *Runtime) Lifetime() context.Context {
	if r.life == nil {
		return context.Background()
	}
	return r.life.ctx
}

// keepStop records a store watch's stop so Shutdown can run it, and
// answers the stop to call instead: it runs once, whoever calls it first.
func (l *lifecycle) keepStop(w *Watch, stop func()) func() {
	once := sync.OnceFunc(stop)
	if l == nil {
		return once
	}
	l.mu.Lock()
	if l.shut.Load() {
		l.mu.Unlock()
		once() // shut down already: the watch never runs
		return once
	}
	l.stops[w] = once
	l.mu.Unlock()
	return func() {
		l.mu.Lock()
		delete(l.stops, w)
		l.mu.Unlock()
		once()
	}
}
