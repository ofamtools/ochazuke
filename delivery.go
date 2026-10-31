package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
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
// replaces: a newer job in the same slot on the same owner (or, with
// ReplacingAt, on the same scope) drops the older.
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
	actor    Actor
	source   string
	Runtime  *Runtime
	Context  Context
	Args     Arguments
	Instance *Instance
	// Subjects are the instances the request acts on: Instance alone,
	// or, for a verb that takes many (Operation.Many) run over a visual
	// selection, every selected row in display order (Instance among them).
	Subjects []*Instance
	// Invoked is the name the request ran the verb by: its own key, or an
	// alias (an old name, a user's actions.json alias, the other noun of
	// an operation reachable under two nouns), with that name's noun as
	// the subject side.
	Invoked Invocation
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
	scope string
	label string
	fg    *exec.Cmd // Foreground: a program given the terminal (foreground.go)
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

// ReplacingAt is Replacing with the slot scoped to scope, the owner or one
// of its ancestors: the newest job in (scope, slot) cancels the older one
// whichever descendant of scope asked, so previews launched from the rows
// of one list replace each other while another list keeps its own. The job
// still belongs to the row that asked (its completion and Then land there
// and closing it cancels the job); Replacing(slot) is ReplacingAt(owner,
// slot). A scope off the owner's ancestry fails the launch.
func (res Result) ReplacingAt(scope, slot string) Result {
	res.scope, res.slot = scope, slot
	return res
}

// Label names this job for people reading the jobs list: the ticker shows
// it after the verb ("agent.launch claude running") and jobs.list carries
// it. It says which of a verb's jobs this is (the program a launch starts,
// the file a build writes); "" leaves the verb alone.
func (res Result) Label(label string) Result { res.label = label; return res }

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
	Label   string    `json:"label,omitempty"`
	Slot    string    `json:"slot,omitempty"`
	Scope   string    `json:"scope,omitempty"`
	Started time.Time `json:"started"`
	State   string    `json:"state"`
	Error   string    `json:"error,omitempty"`
}

// job is one entry of the runtime's jobs list.
type job struct {
	JobInfo
	cancel context.CancelFunc
	then   func(Ctx, any, error) error
	ctx    Ctx
}

func (j *job) ticket() Ticket { return Ticket{Owner: j.Owner, Job: j.Job, Slot: j.Slot} }

// Issue admits a job for owner and returns its ticket and the context its
// work runs under; the caller runs the work and reports back with a message
// carrying the ticket, checked with Accepts and closed with Settle. A slot
// name cancels the owner's running job in that slot; "" accumulates. Launch
// and Start build on it for work the runtime runs as a tea.Cmd.
func (r *Runtime) Issue(owner, verb, slot string, c Ctx) (Ticket, context.Context, error) {
	return r.IssueAt(owner, owner, verb, slot, c)
}

// IssueAt is Issue with the slot scoped to scope, owner or one of its
// ancestors: the running job in (scope, slot) is cancelled whichever
// instance owns it. See Result.ReplacingAt.
func (r *Runtime) IssueAt(owner, scope, verb, slot string, c Ctx) (Ticket, context.Context, error) {
	if r.Instances[owner] == nil {
		return Ticket{}, nil, errors.New("unknown instance " + owner)
	}
	if scope == "" {
		scope = owner
	}
	if !slices.Contains(r.Ancestry(owner), scope) {
		return Ticket{}, nil, fmt.Errorf("slot scope %q is not %s or one of its ancestors", scope, owner)
	}
	if slot == "" {
		scope = ""
	} else if old := r.slotJob(scope, slot); old != nil {
		r.finish(old, JobCancelled, nil)
	}
	if c.actor == (Actor{}) {
		c.actor = r.Actor()
	}
	if c.source == "" {
		c.source = r.history.source
		if c.source == "" {
			c.source = "api"
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.jobSeq++
	j := &job{JobInfo: JobInfo{Job: r.jobSeq, Owner: owner, Verb: verb, Slot: slot, Scope: scope, Started: time.Now(), State: JobRunning}, cancel: cancel, ctx: c}
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
	if res.fg != nil {
		return r.launchForeground(owner, verb, res, c)
	}
	if res.work == nil {
		return Ticket{}, errors.New("Launch needs pending work")
	}
	verb = r.CanonicalKey(verb)
	ticket, ctx, err := r.IssueAt(owner, res.scope, verb, res.slot, c)
	if err != nil {
		return Ticket{}, err
	}
	if res.then != nil {
		r.jobs[ticket.Job].then = func(c Ctx, value any, err error) error {
			res.then(c, value, err)
			return nil
		}
	}
	r.jobs[ticket.Job].Label = res.label
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
	for {
		count, oldest := 0, uint64(0)
		for id, job := range r.jobs {
			if job.State != JobRunning {
				count++
				if oldest == 0 || id < oldest {
					oldest = id
				}
			}
		}
		if count <= jobHistory {
			break
		}
		delete(r.jobs, oldest)
	}
}

// HasRunningJob checks a replacing slot without constructing the sorted
// public jobs list. Scope is the owner or ancestor used by ReplacingAt.
func (r *Runtime) HasRunningJob(scope, slot string) bool { return r.slotJob(scope, slot) != nil }

// slotJob is the running job in a slot of a scope, or nil.
func (r *Runtime) slotJob(scope, slot string) *job {
	for _, j := range r.jobs {
		if j.Scope == scope && j.Slot == slot && j.State == JobRunning {
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
	r.Register(Operation{Verb: "list", Noun: "ochazuke.jobs", Description: "List off-loop jobs: running ones and those finished since the last read (done, failed or cancelled, with the error)", Result: "job[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
	// A foreground program with no Model to suspend: run it in place.
	if fg, ok := msg.(ForegroundMsg); ok {
		if !r.Accepts(fg.Ticket) {
			return true, nil
		}
		return r.Deliver(fg.Run())
	}
	if m, ok := msg.(watchMsg); ok {
		m.deliver(r)
		return true, nil
	}
	if done, ok := msg.(Completion); ok {
		// Settling may evict this job from the bounded history.
		j := r.jobs[done.Job]
		if !r.Settle(done.Ticket, done.Err) {
			return true, nil
		}
		if j != nil {
			defer r.watchAfterJob(j.Verb)
		}
		defer r.actingFor(done.Owner)()
		if j != nil {
			defer r.WithActor(j.ctx.Actor())()
			defer r.forceSource(j.ctx.source)()
		}
		var callbackErr error
		if j.then != nil {
			callbackErr = j.then(j.ctx, done.Value, done.Err)
		}
		// The callback may have removed the owner (a kill verb dropping its
		// own row): the job is settled, so the message is dropped.
		if inst := r.Instances[done.Owner]; inst != nil {
			if comp, ok := inst.Component.(Component); ok {
				comp.HandleMessage(done)
			}
		}
		r.inspectCompletion(done, done.Err)
		if callbackErr != nil {
			r.inspectCompletion(done, callbackErr)
		}
		if done.Err != nil {
			return true, done.Err
		}
		return true, callbackErr
	}
	for _, inst := range r.components() {
		comp := inst.Component.(Component)
		if comp.HandleMessage(msg) {
			// A component that keeps background failures (a tree's timed
			// write) reports the one this message caused.
			if f, ok := comp.(interface{ Failure() error }); ok {
				return true, f.Failure()
			}
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
// in registration order. It walks the holder index (every instance that is
// not a list row), so a page of many rows costs nothing per message: rows
// are instances without components. A list row holds no Component; put one
// on an instance of its own (AddTerminal under the row does). The index is
// rebuilt from the registry when its count disagrees with it, which only
// code writing r.Instances directly causes.
func (r *Runtime) components() []*Instance {
	if len(r.Instances) != len(r.rows)+len(r.holders) {
		r.reindexHolders()
	}
	var out []*Instance
	for _, id := range r.holders {
		if inst := r.Instances[id]; inst != nil {
			if _, ok := inst.Component.(Component); ok {
				out = append(out, inst)
			}
		}
	}
	return out
}

// reindexHolders rebuilds the holder index from the registry: every
// instance that is not a list row, by registration sequence.
func (r *Runtime) reindexHolders() {
	var all []*Instance
	for id, inst := range r.Instances {
		if _, row := r.rows[id]; !row {
			all = append(all, inst)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].seq < all[j].seq })
	r.holders = r.holders[:0]
	for _, inst := range all {
		r.holders = append(r.holders, inst.ID)
	}
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
		if _, order := comp.(*appPages); comp == nil || order {
			continue // app's page order is the runtime's own bookkeeping
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
	ctx := Ctx{actor: r.Actor(), source: r.history.source, Runtime: r, Context: c, Args: args, Instance: inst, Subjects: r.takeSubjects(inst), Invoked: r.Invoked()}
	ancestry := r.Ancestry(inst.ID)
	res := op.App(ctx)
	switch {
	case res.err != nil:
		return nil, res.err
	case res.work == nil && res.fg == nil:
		return res.value, nil
	}
	owner := inst.ID
	if r.Instances[owner] == nil {
		// The handler removed its own instance (a chooser's pick closes
		// its modal, input and all, before Choose launches): the job
		// belongs to the nearest instance that was above it and survives,
		// the focus the modal opened over, so its completion still lands.
		owner = r.survivor(ancestry)
		if res.scope == inst.ID {
			res.scope = owner
		}
	}
	return r.Launch(owner, op.Key(), res, ctx)
}

// survivor is the first instance of an ancestry captured earlier that
// still exists, else app.
func (r *Runtime) survivor(ancestry []string) string {
	for _, id := range ancestry {
		if r.Instances[id] != nil {
			return id
		}
	}
	return "app"
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
