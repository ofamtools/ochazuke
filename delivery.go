package ochazuke

import (
	"context"
	"errors"
	"sort"

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

// Ticket names one piece of pending work: the instance that owns it and the
// generation it was started under. A completion whose generation is no longer
// the owner's current one is stale and dropped.
type Ticket struct {
	Owner      string `json:"owner"`
	Generation uint64 `json:"generation"`
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

// Pending schedules work off the loop. The context is cancelled when the
// owning instance closes or newer work on it starts.
func Pending(work func(context.Context) (any, error)) Result { return Result{work: work} }

// Then runs on the loop when the pending work's completion is delivered and
// still current; it never runs for a stale or cancelled ticket.
func (res Result) Then(fn func(Ctx, any, error)) Result { res.then = fn; return res }

// pendingWork is the outstanding work of one instance.
type pendingWork struct {
	ticket Ticket
	cancel context.CancelFunc
	then   func(Ctx, any, error)
	ctx    Ctx
}

// Generation is the instance's current work generation.
func (r *Runtime) Generation(id string) uint64 {
	if inst := r.Instances[id]; inst != nil {
		return inst.generation
	}
	return 0
}

// Start schedules work owned by an instance and returns its ticket. Earlier
// work of the same instance is cancelled: one generation is current at a
// time. The completion reaches the owner's Component.HandleMessage and the
// optional then callback when it is still current.
func (r *Runtime) Start(owner string, work func(context.Context) (any, error), then func(Ctx, any, error), c Ctx) (Ticket, error) {
	inst := r.Instances[owner]
	if inst == nil {
		return Ticket{}, errors.New("unknown instance " + owner)
	}
	r.cancelWork(inst)
	inst.generation++
	ctx, cancel := context.WithCancel(context.Background())
	ticket := Ticket{Owner: owner, Generation: inst.generation}
	inst.work = &pendingWork{ticket: ticket, cancel: cancel, then: then, ctx: c}
	r.commands = append(r.commands, func() tea.Msg {
		value, err := work(ctx)
		return Completion{Ticket: ticket, Value: value, Err: err}
	})
	return ticket, nil
}

// cancelWork drops an instance's outstanding work; its completion becomes stale.
func (r *Runtime) cancelWork(inst *Instance) {
	if inst.work != nil {
		inst.work.cancel()
		inst.work = nil
	}
}

// Accepts reports whether a ticket is still the owner's current generation.
func (r *Runtime) Accepts(t Ticket) bool {
	inst := r.Instances[t.Owner]
	return inst != nil && inst.generation == t.Generation
}

// Deliver routes a message on the loop: a Completion goes to its owner when
// current and is dropped otherwise; any other message is offered to every
// component until one claims it. It reports whether a component consumed
// the message and the delivered completion's error, if any.
func (r *Runtime) Deliver(msg tea.Msg) (bool, error) {
	if done, ok := msg.(Completion); ok {
		if !r.Accepts(done.Ticket) {
			return true, nil
		}
		inst := r.Instances[done.Owner]
		var then func(Ctx, any, error)
		var c Ctx
		if inst.work != nil && inst.work.ticket == done.Ticket {
			then, c = inst.work.then, inst.work.ctx
			inst.work = nil
		}
		if then != nil {
			then(c, done.Value, done.Err)
		}
		if comp, ok := inst.Component.(Component); ok {
			comp.HandleMessage(done)
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

// closeInstance cancels an instance's work, retires its generation and
// closes its component. Pop, SetItems and removeInstance call it.
func (r *Runtime) closeInstance(id string) {
	inst := r.Instances[id]
	if inst == nil {
		return
	}
	r.cancelWork(inst)
	inst.generation++
	if comp, ok := inst.Component.(Component); ok {
		comp.Close()
	}
}

// componentSnapshot describes the registered components for get.state.
func (r *Runtime) componentSnapshot() map[string]any {
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
		out[inst.ID] = map[string]any{"id": inst.ID, "noun": inst.Noun, "component": kind, "contract": contract, "generation": inst.generation, "pending": inst.work != nil}
	}
	return out
}

// Operation kinds.
const (
	KindInteraction = "interaction" // runs synchronously on the loop (default)
	KindApplication = "application" // an AppHandler; pending work runs off the loop
)

// runApplication is the Run of an application operation: it calls the
// AppHandler and turns a pending result into scheduled work.
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
	return r.Start(inst.ID, res.work, res.then, ctx)
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
