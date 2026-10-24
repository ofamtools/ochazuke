package ochazuke

import (
	"context"
	"errors"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Component is the delivery contract every instance that works off the loop
// implements through Instance.Component. HandleMessage applies a result on
// the loop and reports whether the message was for this component;
// PendingCommand hands the Bubble Tea adapter the work scheduled since the
// last drain; Close cancels outstanding work and releases anything held.
// The picker and the registers component are the framework's own; an
// application adds its own by setting Instance.Component.
type Component interface {
	HandleMessage(tea.Msg) bool
	PendingCommand() tea.Cmd
	Close()
}

// Ticket names one job: the instance that owns it, its runtime-wide id and
// the slot it was started in. Slot is "" for a job that accumulates (every
// completion is delivered, in finish order) and a name for a job that
// replaces: a newer job in the same slot on the same owner drops the older.
// A completion is stale, and dropped, only when its job was cancelled by a
// newer job in its slot or by its owner closing.
type Ticket struct {
	Owner string `json:"owner"`
	Job   uint64 `json:"job"`
	Slot  string `json:"slot,omitempty"`
}

// Completion is the message off-loop work sends back. It is delivered to the
// owning instance only while its ticket is current.
type Completion struct {
	Ticket
	Value any
	Err   error
}

// Ctx is what an application handler receives: the runtime, the context the
// request runs in, its arguments and the instance it addresses (id, noun,
// payload).
type Ctx struct {
	Runtime  *Runtime
	Context  Context
	Args     Arguments
	Instance *Instance
}

// AppHandler is the signature of an application verb: it reads the Ctx and
// returns Done, Failed or Pending work. Pending work runs off the loop; its
// completion is delivered to the owning instance through the contract, so a
// handler never blocks key handling.
type AppHandler func(Ctx) Result

// Result is what an application handler returns.
type Result struct {
	value any
	err   error
	work  func(context.Context) (any, error)
	then  func(Ctx, any, error)
	slot  string
}

// Done reports a synchronous result.
func Done(value any) Result { return Result{value: value} }

// Failed reports a synchronous failure.
func Failed(err error) Result {
	if err == nil {
		err = errors.New("application verb failed")
	}
	return Result{err: err}
}

// Pending schedules work off the loop as a job that accumulates: it runs
// beside the owner's other jobs and its completion is delivered whenever it
// finishes. The context is cancelled when the owning instance closes.
func Pending(work func(context.Context) (any, error)) Result { return Result{work: work} }

// Replacing puts the pending work in a slot on its owner: starting it
// cancels the owner's running job in the same slot, and a newer job in the
// slot cancels this one. Use it for replaceable work such as a preview or a
// search, never for independent work.
func (res Result) Replacing(slot string) Result { res.slot = slot; return res }

// Then runs on the loop when the pending work's completion is delivered and
// still current; it never runs for a stale or cancelled ticket.
func (res Result) Then(fn func(Ctx, any, error)) Result { res.then = fn; return res }

// Job states as jobs.list and the snapshot report them.
const (
	JobRunning   = "running"
	JobDone      = "done"
	JobFailed    = "failed"
	JobCancelled = "cancelled"
)

// jobHistory bounds the finished jobs the list retains between reads.
const jobHistory = 32

// JobInfo is one row of the jobs list: what jobs.list returns and what
// the snapshot's /jobs holds for running jobs.
type JobInfo struct {
	Job     uint64    `json:"job"`
	Owner   string    `json:"owner"`
	Verb    string    `json:"verb"`
	Slot    string    `json:"slot,omitempty"`
	Started time.Time `json:"started"`
	State   string    `json:"state"`
	Error   string    `json:"error,omitempty"`
}

// job is one entry of the runtime's jobs list.
type job struct {
	JobInfo
	cancel context.CancelFunc
	then   func(Ctx, any, error)
	ctx    Ctx
}

func (j *job) ticket() Ticket { return Ticket{Owner: j.Owner, Job: j.Job, Slot: j.Slot} }

// Issue admits a job for owner and returns its ticket and the context its
// work runs under; the caller runs the work and reports back with a message
// carrying the ticket, checked with Accepts and closed with Settle. A slot
// name cancels the owner's running job in that slot; "" accumulates. Launch
// and Start build on it for work the runtime runs as a tea.Cmd.
func (r *Runtime) Issue(owner, verb, slot string, c Ctx) (Ticket, context.Context, error) {
	if r.Instances[owner] == nil {
		return Ticket{}, nil, errors.New("unknown instance " + owner)
	}
	if slot != "" {
		if old := r.slotJob(owner, slot); old != nil {
			r.finish(old, JobCancelled, nil)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.jobSeq++
	j := &job{JobInfo: JobInfo{Job: r.jobSeq, Owner: owner, Verb: verb, Slot: slot, Started: time.Now(), State: JobRunning}, cancel: cancel, ctx: c}
	if r.jobs == nil {
		r.jobs = map[uint64]*job{}
	}
	r.jobs[j.Job] = j
	return j.ticket(), ctx, nil
}

// Launch starts a pending result as a job of owner under verb and queues
// its work for the adapter. The completion reaches the owner's
// Component.HandleMessage and the result's Then callback when the job is
// still current.
func (r *Runtime) Launch(owner, verb string, res Result, c Ctx) (Ticket, error) {
	if res.work == nil {
		return Ticket{}, errors.New("Launch needs pending work")
	}
	ticket, ctx, err := r.Issue(owner, verb, res.slot, c)
	if err != nil {
		return Ticket{}, err
	}
	r.jobs[ticket.Job].then = res.then
	work := res.work
	r.commands = append(r.commands, func() tea.Msg {
		value, err := work(ctx)
		return Completion{Ticket: ticket, Value: value, Err: err}
	})
	return ticket, nil
}

// Start schedules accumulating work owned by an instance and returns its
// ticket; it is Launch with Pending(work).Then(then) under the verb "work".
func (r *Runtime) Start(owner string, work func(context.Context) (any, error), then func(Ctx, any, error), c Ctx) (Ticket, error) {
	return r.Launch(owner, "work", Pending(work).Then(then), c)
}

// Accepts reports whether a ticket names a running job.
func (r *Runtime) Accepts(t Ticket) bool {
	j := r.jobs[t.Job]
	return j != nil && j.State == JobRunning && j.Owner == t.Owner
}

// Settle records the outcome of a running job and reports whether the ticket
// was current; a stale ticket leaves the list unchanged. Deliver settles the
// completions it routes; a component that runs its own commands settles the
// tickets it issued.
func (r *Runtime) Settle(t Ticket, err error) bool {
	if !r.Accepts(t) {
		return false
	}
	state := JobDone
	if err != nil {
		state = JobFailed
	}
	r.finish(r.jobs[t.Job], state, err)
	return true
}

// finish moves a job out of the running state and trims the retained
// history to jobHistory finished entries, oldest first.
func (r *Runtime) finish(j *job, state string, err error) {
	j.cancel()
	j.State = state
	if err != nil {
		j.Error = err.Error()
	}
	finished := r.jobList(func(j *job) bool { return j.State != JobRunning })
	for len(finished) > jobHistory {
		delete(r.jobs, finished[0].Job)
		finished = finished[1:]
	}
}

// slotJob is the owner's running job in a slot, or nil.
func (r *Runtime) slotJob(owner, slot string) *job {
	for _, j := range r.jobs {
		if j.Owner == owner && j.Slot == slot && j.State == JobRunning {
			return j
		}
	}
	return nil
}

// cancelJobs cancels every running job of an owner; their completions
// become stale.
func (r *Runtime) cancelJobs(owner string) {
	for _, j := range r.jobs {
		if j.Owner == owner && j.State == JobRunning {
			r.finish(j, JobCancelled, nil)
		}
	}
}

// jobList lists the jobs matching keep in start order.
func (r *Runtime) jobList(keep func(*job) bool) []*job {
	var out []*job
	for _, j := range r.jobs {
		if keep(j) {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Job < out[k].Job })
	return out
}

// Jobs lists every job the runtime knows in start order: the running ones
// and the finished ones retained since the last jobs.list read.
func (r *Runtime) Jobs() []JobInfo {
	out := []JobInfo{}
	for _, j := range r.jobList(func(*job) bool { return true }) {
		out = append(out, j.JobInfo)
	}
	return out
}

// runningJobs is the snapshot's /jobs: running jobs only, in start order.
func (r *Runtime) runningJobs() []JobInfo {
	out := []JobInfo{}
	for _, j := range r.jobList(func(j *job) bool { return j.State == JobRunning }) {
		out = append(out, j.JobInfo)
	}
	return out
}

// registerJobs adds list.jobs. Finished entries (done, failed, cancelled)
// stay in the list until a jobs.list read returns them, bounded to
// jobHistory entries meanwhile; the snapshot's /jobs shows running jobs only.
func registerJobs(r *Runtime) {
	r.Register(Operation{Verb: "list", Noun: "jobs", Description: "List off-loop jobs: running ones and those finished since the last read (done, failed or cancelled, with the error)", Result: "job[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		jobs := r.Jobs()
		for _, j := range jobs {
			if j.State != JobRunning {
				delete(r.jobs, j.Job)
			}
		}
		return jobs, nil
	}})
}

// Deliver routes a message on the loop: a Completion goes to its owner when
// its job is running and is dropped otherwise; any other message is offered
// to every component until one claims it. A Then callback that removes its
// own owner is safe: the job is already settled and the completion is not
// offered to the gone component. It reports whether a component consumed
// the message and the delivered completion's error, if any.
func (r *Runtime) Deliver(msg tea.Msg) (bool, error) {
	if done, ok := msg.(Completion); ok {
		if !r.Settle(done.Ticket, done.Err) {
			return true, nil
		}
		j := r.jobs[done.Job]
		defer r.actingFor(done.Owner)()
		if j.then != nil {
			j.then(j.ctx, done.Value, done.Err)
		}
		// The callback may have removed the owner (a kill verb dropping its
		// own row): the job is settled, so the message is dropped.
		if inst := r.Instances[done.Owner]; inst != nil {
			if comp, ok := inst.Component.(Component); ok {
				comp.HandleMessage(done)
			}
		}
		return true, done.Err
	}
	for _, inst := range r.components() {
		if inst.Component.(Component).HandleMessage(msg) {
			return true, nil
		}
	}
	return false, nil
}

// PendingCommands drains the work scheduled since the last call: the
// runtime's own queue and every component's PendingCommand.
func (r *Runtime) PendingCommands() tea.Cmd {
	commands := r.commands
	r.commands = nil
	for _, inst := range r.components() {
		if cmd := inst.Component.(Component).PendingCommand(); cmd != nil {
			commands = append(commands, cmd)
		}
	}
	if len(commands) == 0 {
		return nil
	}
	return tea.Batch(commands...)
}

// components lists the instances whose Component implements the contract,
// in registration order.
func (r *Runtime) components() []*Instance {
	var out []*Instance
	for _, inst := range r.Instances {
		if _, ok := inst.Component.(Component); ok {
			out = append(out, inst)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

// closeInstance cancels an instance's jobs and closes its component. Pop,
// SetItems and removeInstance call it.
func (r *Runtime) closeInstance(id string) {
	inst := r.Instances[id]
	if inst == nil {
		return
	}
	r.cancelJobs(id)
	if comp, ok := inst.Component.(Component); ok {
		comp.Close()
	}
}

// componentSnapshot describes the registered components for get.state.
func (r *Runtime) componentSnapshot() map[string]any {
	running := map[string]int{}
	for _, j := range r.jobs {
		if j.State == JobRunning {
			running[j.Owner]++
		}
	}
	out := map[string]any{}
	for _, inst := range r.Instances {
		comp := inst.Component
		if comp == nil {
			continue
		}
		kind := ""
		switch comp.(type) {
		case *Input:
			kind = "input"
		case *List:
			kind = "list"
		case *Page:
			kind = "page"
		case *Picker:
			kind = "picker"
		case *Registers:
			kind = "registers"
		case *Terminal:
			kind = "terminal"
		case *Palette:
			kind = "palette"
		default:
			kind = "component"
		}
		_, contract := comp.(Component)
		out[inst.ID] = map[string]any{"id": inst.ID, "noun": inst.Noun, "component": kind, "contract": contract, "jobs": running[inst.ID], "pending": running[inst.ID] > 0}
	}
	return out
}

// Operation kinds.
const (
	KindInteraction = "interaction" // runs synchronously on the loop (default)
	KindApplication = "application" // an AppHandler; pending work runs off the loop
)

// runApplication is the Run of an application operation: it calls the
// AppHandler and turns a pending result into a job under the verb's key.
func (r *Runtime) runApplication(op Operation, c Context, args Arguments) (any, error) {
	inst := r.Instances[focusOf(c)]
	ctx := Ctx{Runtime: r, Context: c, Args: args, Instance: inst}
	res := op.App(ctx)
	switch {
	case res.err != nil:
		return nil, res.err
	case res.work == nil:
		return res.value, nil
	}
	return r.Launch(inst.ID, op.Key(), res, ctx)
}

// appHandlerOf accepts the handler shapes Verb and Register take.
func appHandlerOf(h any) (Handler, AppHandler) {
	switch fn := h.(type) {
	case Handler:
		return fn, nil
	case func(*Runtime, Context, Arguments) (any, error):
		return fn, nil
	case AppHandler:
		return nil, fn
	case func(Ctx) Result:
		return nil, fn
	}
	panic("handler must be a Handler or an AppHandler")
}
