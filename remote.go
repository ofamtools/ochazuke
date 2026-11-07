package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Remote stores. A store whose calls leave the process (Store.Remote: a
// mounted extension's CLI) must not block the loop, so a standard verb over
// it splits into plan, call and record:
//
//   - plan, on the loop: the verb's row runs as it does over any store
//     (resolve the target, build the change, validate), but each store call
//     goes through Ctx.Await, which on the first run past the calls already
//     answered stops the run and hands back the call;
//   - call, off the loop: the verb is one job (cancellable, queued behind
//     earlier writes of the same thing, never blocking quit) whose work runs
//     the calls one at a time;
//   - record, on the loop: when a call answers, the row runs again from the
//     start with every answer so far replayed, until it needs another call
//     or finishes; the change and its undo entry are noted by that last
//     run, and attached to the history entry the request recorded.
//
// The row runs several times, so what it does before a store call must be
// repeatable: the standard rows' reads and checks are. Each run sees the
// same inputs (the subject key is resolved once and kept, the mode is the
// one the request started in) and every answer on the tape records the call
// it answered; a run that makes other calls than the tape holds fails the
// job with an error. Notices a run posts (Ctx.Notify, Info, Error) wait
// until the run that finishes. Store code must not recover the panic that
// stops a run (it is not an error), and everything it reads on the loop
// outside a plan comes from Ctx.Await's last-known answers, never a call.
//
// A store that is not Remote keeps the synchronous path.

// StoreCall names a store call: the noun's store, what is asked of it, of which
// thing (key), and whether it writes. It is the identity an answer on the
// tape records and the key of the last-known answers.
type StoreCall struct {
	Store string
	Op    string
	Key   string
	Write bool
}

func (c StoreCall) id() string { return c.Store + "\x00" + c.Op + "\x00" + c.Key }

// ErrNotLoaded is what a read of a remote store answers on the loop outside
// a plan when no answer to it is known yet: the loop never waits for the
// source.
var ErrNotLoaded = errors.New("not loaded yet")

// tape is the answers a verb's run replays: the store calls made so far,
// in order, and where the run is in them. notes are the notices the run
// posted, kept until the run finishes.
type tape struct {
	done  []tapeAnswer
	next  int
	notes []func()
	// missing and filled keep a copy store's runs alike (copy_store.go):
	// the things its copy lacked when the plan first read them, and those
	// the current run has filled from the source, so every run makes the
	// same source calls whatever earlier runs wrote to the copy.
	missing map[string]bool
	filled  map[string]bool
}

type tapeAnswer struct {
	call  StoreCall
	value any
	err   error
}

// needCall stops a planning run at a store call that has no answer yet.
type needCall struct {
	call  StoreCall
	res   Result
	limit time.Duration
}

// replayMismatch stops a planning run that made a different call than the
// tape recorded.
type replayMismatch struct{ err error }

// work is the call as work to run off the loop, under its limit.
func (n *needCall) work() func(context.Context) (any, error) {
	return func(ctx context.Context) (any, error) {
		if n.limit > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, n.limit)
			defer cancel()
		}
		return n.res.work(ctx)
	}
}

// Await is how a remote store makes a call. start makes the call's Result
// on the loop (a mounted verb's handler result: pending work, or an
// answer), limit bounds the wait (0 for none).
//
// In a verb's plan it replays the answer already on the tape, else stops
// the run so the call can happen off the loop. Outside a plan (watches,
// confirmations and forms read the store on the loop) it never calls: a
// read answers what the last call answered (or ErrNotLoaded), a write
// fails. start's Result must not be stepped (Result.Continue): its steps run
// on the loop, which a call off the loop cannot reach.
func (c Ctx) Await(call StoreCall, start func() Result, limit time.Duration) (any, error) {
	t := c.tape
	if t == nil {
		if call.Write {
			return nil, fmt.Errorf("%s: a write to a remote store runs as a job", call.Store)
		}
		if v, ok := c.Runtime.lastKnown(call); ok {
			return v, nil
		}
		return nil, fmt.Errorf("%s %s %s: %w", call.Store, call.Op, call.Key, ErrNotLoaded)
	}
	if t.next < len(t.done) {
		a := t.done[t.next]
		t.next++
		if a.call != call {
			panic(&replayMismatch{fmt.Errorf("remote call changed between runs: the tape holds %s %s, the run made %s %s; the verb stops", a.call.Op, a.call.Key, call.Op, call.Key)})
		}
		return a.value, a.err
	}
	res := start()
	switch {
	case res.cont != nil:
		return nil, errors.New("a stepped result cannot be awaited inside a plan")
	case res.fg != nil:
		return nil, errors.New("a call that needs the terminal cannot be made by a store")
	case res.work == nil:
		t.done = append(t.done, tapeAnswer{call, res.value, res.err}) // answered at once
		t.next++
		return res.value, res.err
	}
	panic(&needCall{call: call, res: res, limit: limit})
}

// lastKnown is the answer to a read the last call to it got, on the loop.
func (r *Runtime) lastKnown(call StoreCall) (any, bool) {
	v, ok := r.remoteSeen[call.Store][call.id()]
	return v, ok
}

// remember keeps an answer as the last known one; a write forgets what is
// known of the store, which it may have changed.
func (r *Runtime) remember(a tapeAnswer) {
	if a.err != nil {
		return
	}
	if r.remoteSeen == nil {
		r.remoteSeen = map[string]map[string]any{}
	}
	if a.call.Write {
		delete(r.remoteSeen, a.call.Store)
		return
	}
	seen := r.remoteSeen[a.call.Store]
	if seen == nil || len(seen) >= remoteSeenLimit {
		seen = map[string]any{}
		r.remoteSeen[a.call.Store] = seen
	}
	seen[a.call.id()] = a.value
}

const remoteSeenLimit = 512

// remoteRun is one remote-store verb in flight: a standard row, or another
// body that reads and writes a remote store (undo).
type remoteRun struct {
	r     *Runtime
	base  Ctx
	noun  string // the store's noun; "" for a body that touches several
	label string // the verb's key, for notices
	body  func(Ctx) (any, error)
	// finish, when set, runs once on the loop with the body's last answer
	// and gives the verb's.
	finish func(value any, err error) (any, error)
	// pin, when set, runs first and answers the subject key every run uses.
	pin    func(Ctx) (string, error)
	pinKey string
	pinned bool
	// queue serialises the verb's writes to one thing; owned makes the job
	// the app's, so closing the row it ran on does not cancel a write.
	queue    string
	waiting  bool
	owned    bool
	subjects []string // a run over several: the instance of each
	many     bool
	unit     int
	tape     *tape
	asked    StoreCall
	job      uint64
	changes  []ThingChange
	applied  []string
	skipped  []string
	first    error
	ending   bool
}

// runRemote is the handler of a standard verb over a remote store (see the
// file comment).
func (r *Runtime) runRemote(row standardVerb, n *Noun, c Ctx) Result {
	name := n.Name
	s := &remoteRun{r: r, base: c.withRequestMode(r.Mode), noun: name, label: name + "." + row.Verb, tape: &tape{}}
	s.body = func(c Ctx) (any, error) {
		noun := r.Nouns[name]
		if noun == nil || noun.Store == nil {
			return nil, fmt.Errorf("%s has no store", name)
		}
		c.standard = s.label
		return r.runRecording(row, noun, c)
	}
	writes := row.Undo != undoNone
	s.owned = writes
	if row.Many && len(c.Subjects) > 1 {
		s.many = true
		for _, in := range c.Subjects {
			s.subjects = append(s.subjects, in.ID)
		}
		if key := n.keyProp(); key != nil {
			s.base.Args = cloneArgs(c.Args)
			delete(s.base.Args, key.Name)
		}
	} else if key := n.keyProp(); key != nil && row.TakesSubject {
		s.pin = func(c Ctx) (string, error) { return r.subjectKey(n, c) }
		if writes {
			s.queue = name + "\x00" + r.subjectText(n, c)
		}
	}
	return s.start()
}

// subjectText is the text naming a request's subject without asking the
// store: the key argument, else the focused row's key.
func (r *Runtime) subjectText(n *Noun, c Ctx) string {
	key := n.keyProp()
	if v, ok := c.Args[key.Name]; ok && !emptyProp(v) {
		return fmt.Sprint(v)
	}
	if row := r.subjectRow(n, c); row != nil {
		if k, err := r.rowKey(n, row); err == nil {
			return k
		}
	}
	return ""
}

// start queues the verb behind earlier writes of its thing, then runs it.
func (s *remoteRun) start() Result {
	r := s.r
	if s.queue != "" {
		r.remoteQueue = append(r.remoteQueue, s)
		if !s.head() {
			s.waiting = true
			return s.result(s.poll(), nil, nil)
		}
	}
	return s.result(s.advance())
}

// head reports whether no earlier write of the thing is still running.
func (s *remoteRun) head() bool {
	for _, o := range s.r.remoteQueue {
		if o == s {
			return true
		}
		if o.queue == s.queue && o.alive() {
			return false
		}
	}
	return true
}

// alive says the verb's job is running (or not launched yet).
func (s *remoteRun) alive() bool {
	if s.job == 0 {
		return true
	}
	j := s.r.jobs[s.job]
	return j != nil && j.State == JobRunning
}

// leave takes the verb out of its queue.
func (s *remoteRun) leave() {
	if s.queue == "" {
		return
	}
	q := s.r.remoteQueue
	for i, o := range q {
		if o == s {
			s.r.remoteQueue = append(q[:i:i], q[i+1:]...)
			return
		}
	}
}

// poll is the work of a queued verb: a short wait, then the loop looks
// again whether the earlier writes are done.
func (s *remoteRun) poll() func(context.Context) (any, error) {
	return func(ctx context.Context) (any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return nil, nil
		}
	}
}

// result turns a step into the handler's answer.
func (s *remoteRun) result(more func(context.Context) (any, error), value any, err error) Result {
	if more == nil {
		s.leave()
		if err != nil {
			return Failed(err)
		}
		return Done(value)
	}
	res := Pending(more).Continue(s.next).Launched(func(t Ticket) { s.job = t.Job })
	if s.owned {
		res = res.OwnedBy("app")
	}
	return res
}

// next is the job's continuation: replay the verb with the call's answer.
func (s *remoteRun) next(value any, err error) (func(context.Context) (any, error), any, error) {
	switch {
	case s.ending:
		s.leave()
		return nil, value, err // the end's own completion: the answer as it is
	case s.waiting:
		if !s.head() {
			return s.poll(), nil, nil
		}
		s.waiting = false
		return s.advance()
	}
	a := tapeAnswer{s.asked, value, err}
	s.r.remember(a)
	s.tape.done = append(s.tape.done, a)
	return s.advance()
}

// advance runs the verb until it needs a call, answered as the work to
// run, or is done, answered as its value and error.
func (s *remoteRun) advance() (func(context.Context) (any, error), any, error) {
	more, value, err := s.step()
	if more != nil || s.job == 0 {
		return more, value, err
	}
	// The end is its own step, so the job's completion carries the answer.
	s.ending = true
	return func(context.Context) (any, error) { return value, err }, nil, nil
}

func (s *remoteRun) step() (func(context.Context) (any, error), any, error) {
	r := s.r
	if s.pin != nil && !s.pinned {
		key, err, need := s.passPin()
		if need != nil {
			return need, nil, nil
		}
		s.tape = &tape{}
		if err != nil {
			return nil, nil, err
		}
		s.pinned, s.pinKey = true, key
		if k := r.Nouns[s.noun].keyProp(); k != nil && key != "" {
			s.base.Args = cloneArgs(s.base.Args)
			if s.base.Args == nil {
				s.base.Args = Arguments{}
			}
			s.base.Args[k.Name] = key
		}
	}
	for {
		c, ok := s.unitCtx()
		if !ok {
			return s.conclude()
		}
		value, err, need := s.pass(c, s.body)
		if need != nil {
			return need, nil, nil
		}
		s.tape = &tape{}
		if !s.many {
			s.settle()
			r.attachChanges(s.job, s.changes)
			return s.done(value, err)
		}
		id := s.subjects[s.unit]
		s.unit++
		s.settle()
		if err != nil {
			s.skipped = append(s.skipped, id+": "+err.Error())
			if s.first == nil {
				s.first = err
			}
			continue
		}
		s.applied = append(s.applied, id)
	}
}

// done is the verb's answer once its body has run.
func (s *remoteRun) done(value any, err error) (func(context.Context) (any, error), any, error) {
	if s.finish != nil {
		value, err = s.finish(value, err)
	}
	return nil, value, err
}

// unitCtx is the Ctx of the unit to run next: the request's, or one
// subject's; false when every subject has run.
func (s *remoteRun) unitCtx() (Ctx, bool) {
	c := s.base
	c.tape = s.tape
	if !s.many {
		return c, s.unit == 0
	}
	for s.unit < len(s.subjects) {
		id := s.subjects[s.unit]
		in := s.r.Instances[id]
		if in == nil {
			// An earlier subject's change removed it.
			s.skipped = append(s.skipped, id+": no longer there")
			if s.first == nil {
				s.first = fmt.Errorf("%s is no longer there", id)
			}
			s.unit++
			continue
		}
		c.Instance, c.Context, c.Subjects = in, s.r.narrowedTo(c.Context, in.ID), []*Instance{in}
		return c, true
	}
	return c, false
}

// passPin runs the subject resolution over the tape.
func (s *remoteRun) passPin() (key string, err error, need func(context.Context) (any, error)) {
	var out string
	_, err, need = s.pass(s.pinCtx(), func(c Ctx) (any, error) {
		k, e := s.pin(c)
		out = k
		return k, e
	})
	return out, err, need
}

func (s *remoteRun) pinCtx() Ctx {
	c := s.base
	c.tape = s.tape
	return c
}

// pass runs a body once over the tape; need is the call it stopped at.
func (s *remoteRun) pass(c Ctx, body func(Ctx) (any, error)) (value any, err error, need func(context.Context) (any, error)) {
	r := s.r
	s.tape.next = 0
	s.tape.notes = nil
	s.tape.filled = nil
	r.targets = nil // a target probe is a store call: every run makes the same ones
	if s.pinned && s.pinKey != "" {
		r.targets = map[string]string{s.noun + "\x00" + s.pinKey: s.pinKey}
	}
	stashed := r.takeChanges(0)
	defer func() {
		if v := recover(); v != nil {
			switch p := v.(type) {
			case *needCall:
				r.takeChanges(0)
				s.asked = p.call
				need = p.work()
			case *replayMismatch:
				r.takeChanges(0)
				value, err = nil, p.err
			default:
				panic(v)
			}
		} else {
			for _, post := range s.tape.notes {
				post()
			}
		}
		s.tape.notes = nil
		if len(stashed) > 0 {
			if r.history.changes == nil {
				r.history.changes = map[int][]ThingChange{}
			}
			r.history.changes[0] = append(stashed, r.history.changes[0]...)
		}
	}()
	value, err = body(c)
	return value, err, nil
}

// settle takes the changes a finished run noted, unless the verb has not
// launched yet: then they stay for the request's own history entry.
func (s *remoteRun) settle() {
	if s.job != 0 {
		s.changes = append(s.changes, s.r.takeChanges(0)...)
	}
}

// conclude ends a run over several subjects and records their changes on
// the request's history entry; their answer is Each's.
func (s *remoteRun) conclude() (func(context.Context) (any, error), any, error) {
	r := s.r
	r.attachChanges(s.job, s.changes)
	name := r.DisplayKey(s.base.Invoked.Noun + "." + s.base.Invoked.Verb)
	if err := r.subjectsNotice(name, s.applied, s.skipped, s.first); err != nil {
		return nil, nil, err
	}
	return nil, map[string]any{"verb": s.base.Invoked.Verb, "applied": s.applied, "skipped": s.skipped}, nil
}

// attachChanges adds changes to the history entry job started, which was
// recorded when the request launched it.
func (r *Runtime) attachChanges(job uint64, changes []ThingChange) {
	if job == 0 || len(changes) == 0 {
		return
	}
	h := &r.history
	for i := len(h.entries) - 1; i >= 0; i-- {
		if h.entries[i].Job == job {
			h.entries[i].Changes = append(h.entries[i].Changes, changes...)
			return
		}
	}
}

// runSteppedBody runs body as a remote verb of its own (undo): on the loop
// until it needs a call, then as a job. finish gives the verb's answer.
func (r *Runtime) runSteppedBody(c Ctx, label string, body func(Ctx) (any, error), finish func(any, error) (any, error)) Result {
	s := &remoteRun{r: r, base: c.withRequestMode(r.Mode), label: label, tape: &tape{}, body: body, finish: finish, owned: true}
	return s.start()
}

// resolvingHandler is a row's own handler (edit) over a store: for a remote
// store, a subject named by a key argument that may be a human key is
// resolved first, as a job, and the handler runs on the loop with the key
// it found; any other request runs the handler as it is.
func (r *Runtime) resolvingHandler(row standardVerb, noun string, h AppHandler) AppHandler {
	return func(c Ctx) Result {
		n := r.Nouns[noun]
		key := n.keyProp()
		if n == nil || n.Store == nil || !n.Store.Remote || !row.TakesSubject || key == nil || !n.resolvable() {
			return h(c)
		}
		if v, ok := c.Args[key.Name]; !ok || emptyProp(v) {
			return h(c)
		}
		label := noun + "." + row.Verb
		body := func(c Ctx) (any, error) { return r.subjectKey(n, c) }
		finish := func(value any, err error) (any, error) {
			if err != nil {
				return nil, err
			}
			c.Args = cloneArgs(c.Args)
			c.Args[key.Name] = value
			res := h(c)
			switch {
			case res.err != nil:
				return nil, res.err
			case res.work == nil && res.fg == nil:
				return res.value, nil
			}
			owner := "app"
			if c.Instance != nil && r.Instances[c.Instance.ID] != nil {
				owner = c.Instance.ID
			}
			return r.Launch(owner, label, res, c)
		}
		s := &remoteRun{r: r, base: c.withRequestMode(r.Mode), noun: noun, label: label, tape: &tape{}, body: body, finish: finish}
		return s.start()
	}
}
