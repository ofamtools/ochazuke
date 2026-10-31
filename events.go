package ochazuke

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"
)

// Events and hooks: one general mechanism. Something happens (a modal, page
// or form opens or closes, the application quits, an application's own
// domain event) and the runtime is told with Emit(name, fields). The
// application's hooks.json rows (hooks.go) attach verbs to events: each row
// whose "on" names the event and whose "where" matches its fields runs its
// "do" verb, in row order. Nothing in the framework names a surface, a noun
// or a verb: "left the tasks modal after a task changed" is one row's data.
//
// The framework emits:
//
//   - surface.opened when a modal, form or page opens (kind, id, noun);
//   - surface.left when it closes (kind, id, noun, changes, changed,
//     opened_at), changes and changed counting the change verbs that ran
//     while it was open and the nouns they changed;
//   - app.quitting when the application quits (changes and changed since
//     the last surface.left).
//
// Every event also carries its own name as the field "event".

// Built-in event names.
const (
	EventSurfaceOpened = "surface.opened"
	EventSurfaceLeft   = "surface.left"
	EventAppQuitting   = "app.quitting"
)

// Surface kinds, the kind field of the surface events.
const (
	SurfaceModal = "modal"
	SurfaceForm  = "form"
	SurfacePage  = "page"
)

// HookChainLimit bounds the events one burst of hooks may cause (a hook
// whose verb closes a modal emits surface.left, which may run another
// hook): past it the rest are dropped with a notice, so two rows that feed
// each other never spin the loop.
const HookChainLimit = 32

// EventField is one field an event carries: its name, its JSON type
// (string, integer, boolean, list, time) and what it holds.
type EventField struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

// EventSpec declares an event: its name (dotted, noun.happening), what it
// means, the fields it carries and who declared it (ochazuke for the
// framework's, else the application or extension that called
// DeclareEvent). hooks check warns on a row naming an event nobody
// declared, and on a where key that is none of its fields.
type EventSpec struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Fields      []EventField `json:"fields,omitempty"`
	Source      string       `json:"source,omitempty"`
}

// Field reports whether the event declares the field (or it is "event",
// which every event carries).
func (e EventSpec) Field(name string) bool {
	return name == "event" || slices.ContainsFunc(e.Fields, func(f EventField) bool { return f.Name == name })
}

// eventHub is the runtime's events state, made on first use: declared
// events, the open surfaces' change counters, the firings waiting for the
// hooks to run, and the hook rows (hooks.go).
type eventHub struct {
	specs []EventSpec
	// open counts changes for each open surface, by instance id.
	open map[string]*surfaceCount
	// sinceLeft counts changes since the last surface.left (app.quitting's).
	sinceLeft surfaceCount
	queue     []firing
	draining  bool
	// hooking is >0 while a hook's verb runs: what a hook changes is not
	// counted, so a hook never feeds the events that run it.
	hooking int

	hooks hookState

	// quitting: app.quitting was emitted (Model's quit) and the jobs its
	// hooks started, which the Model waits on until QuitHookWait.
	quitEmitted  bool
	quitJobs     []uint64
	quitDeadline time.Time
}

type surfaceCount struct {
	kind, noun string
	opened     time.Time
	changes    int
	changed    map[string]bool
}

func (c *surfaceCount) add(nouns []string) {
	c.changes++
	for _, n := range nouns {
		if c.changed == nil {
			c.changed = map[string]bool{}
		}
		c.changed[n] = true
	}
}

func (c *surfaceCount) nouns() []string {
	out := slices.Collect(maps.Keys(c.changed))
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

// firing is one emitted event waiting for its hooks.
type firing struct {
	event  string
	fields map[string]any
}

func (r *Runtime) eventHub() *eventHub {
	if r.events == nil {
		r.events = &eventHub{open: map[string]*surfaceCount{}, specs: builtinEvents()}
	}
	return r.events
}

func builtinEvents() []EventSpec {
	surface := []EventField{
		{Name: "kind", Type: "string", Description: "modal, form or page"},
		{Name: "id", Type: "string", Description: "the surface's instance id"},
		{Name: "noun", Type: "string", Description: "the surface's noun"},
	}
	counts := []EventField{
		{Name: "changes", Type: "integer", Description: "change verbs that ran (and Runtime.Changed calls)"},
		{Name: "changed", Type: "list", Description: "the nouns they changed, sorted; a where value matches when the list holds it"},
	}
	left := append(slices.Clone(surface), counts...)
	left = append(left, EventField{Name: "opened_at", Type: "time", Description: "when the surface opened (RFC 3339)"})
	return []EventSpec{
		{Name: EventSurfaceOpened, Description: "A modal, form or page opened", Fields: surface, Source: FrameworkNamespace},
		{Name: EventSurfaceLeft, Description: "A modal, form or page closed (a page: another page opened), with what changed while it was open", Fields: left, Source: FrameworkNamespace},
		{Name: EventAppQuitting, Description: "The application is quitting, with what changed since the last surface.left", Fields: counts, Source: FrameworkNamespace},
	}
}

// DeclareEvent declares an application's (or extension's) event, so hooks
// check knows its name and fields; a later declaration of the same name
// replaces the earlier. Source defaults to "app". Emitting an event nobody
// declared still runs its rows.
func (r *Runtime) DeclareEvent(spec EventSpec) error {
	if spec.Name == "" || !strings.Contains(spec.Name, ".") {
		return fmt.Errorf("event name %q is not noun.happening", spec.Name)
	}
	if spec.Source == "" {
		spec.Source = AppNoun
	}
	h := r.eventHub()
	if i := slices.IndexFunc(h.specs, func(e EventSpec) bool { return e.Name == spec.Name }); i >= 0 {
		if h.specs[i].Source == FrameworkNamespace {
			return fmt.Errorf("event %q is the framework's", spec.Name)
		}
		h.specs[i] = spec
		return nil
	}
	h.specs = append(h.specs, spec)
	return nil
}

// Events are the declared events, the framework's first, then in
// declaration order.
func (r *Runtime) Events() []EventSpec {
	out := slices.Clone(r.eventHub().specs)
	for i := range out {
		out[i].Fields = slices.Clone(out[i].Fields)
	}
	return out
}

// Event is the declared event of that name.
func (r *Runtime) Event(name string) (EventSpec, bool) {
	for _, e := range r.eventHub().specs {
		if e.Name == name {
			return e, true
		}
	}
	return EventSpec{}, false
}

// Emit tells the runtime an event happened: every hook row on it whose
// where matches fields runs its verb, in row order, once the request
// running now (if any) has finished, so a verb that closes a modal never
// has another verb run in the middle of it. Fields are passed to the row's
// verb through the row's from. Emit with hooks.enabled off, or with no row
// on the event, does nothing. Call it on the loop.
func (r *Runtime) Emit(event string, fields map[string]any) {
	h := r.eventHub()
	if !r.hasHookOn(event) || !r.hooksEnabled() {
		return
	}
	f := firing{event: event, fields: maps.Clone(fields)}
	if f.fields == nil {
		f.fields = map[string]any{}
	}
	f.fields["event"] = event
	h.queue = append(h.queue, f)
	if r.history.depth == 0 {
		r.runHooks()
	}
}

// countChange adds one change of nouns to every open surface and to the
// count since the last surface.left. The framework's own nouns
// (ochazuke.*: typing into an input, moving a list) are interaction, not
// the application's data, and are not counted; nor is what a hook changes.
func (r *Runtime) countChange(nouns []string) {
	h := r.eventHub()
	if h.hooking > 0 {
		return
	}
	var kept []string
	for _, n := range nouns {
		if n != "" && !strings.HasPrefix(n, FrameworkNamespace+".") {
			kept = append(kept, n)
		}
	}
	if len(nouns) > 0 && len(kept) == 0 {
		return
	}
	for _, id := range r.openSurfaces() {
		c := h.open[id]
		if c == nil {
			// The starting page has no surface.opened: it is counted
			// from its first change.
			c = &surfaceCount{kind: SurfacePage, noun: r.instanceNounOf(id), opened: time.Now()}
			h.open[id] = c
		}
		c.add(kept)
	}
	h.sinceLeft.add(kept)
}

// openSurfaces are the surfaces open now: the current page and every open
// modal, outermost first.
func (r *Runtime) openSurfaces() []string {
	var out []string
	if r.Page != "" && r.Instances[r.Page] != nil {
		out = append(out, r.Page)
	}
	return append(out, r.modals...)
}

func (r *Runtime) instanceNounOf(id string) string {
	if inst := r.Instances[id]; inst != nil {
		return inst.Noun
	}
	return ""
}

// surfaceOpened starts counting for a surface and emits surface.opened.
func (r *Runtime) surfaceOpened(id, kind string) {
	h := r.eventHub()
	c := &surfaceCount{kind: kind, noun: r.instanceNounOf(id), opened: time.Now()}
	h.open[id] = c
	r.Emit(EventSurfaceOpened, map[string]any{"kind": kind, "id": id, "noun": c.noun})
}

// surfaceLeft stops counting for a surface and emits surface.left with
// its counts. noun is the surface's noun read before it was removed.
func (r *Runtime) surfaceLeft(id, kind, noun string) {
	h := r.eventHub()
	c := h.open[id]
	delete(h.open, id)
	if c == nil {
		c = &surfaceCount{kind: kind, noun: noun}
	}
	if noun == "" {
		noun = c.noun
	}
	h.sinceLeft = surfaceCount{}
	fields := map[string]any{"kind": kind, "id": id, "noun": noun, "changes": c.changes, "changed": c.nouns()}
	if !c.opened.IsZero() {
		fields["opened_at"] = c.opened.UTC().Format(time.RFC3339)
	}
	r.Emit(EventSurfaceLeft, fields)
}

// modalKind is the surface kind of a modal instance: a form for a record
// editor, else a modal.
func (r *Runtime) modalKind(id string) string {
	if inst := r.Instances[id]; inst != nil {
		if _, ok := inst.Component.(*RecordEditor); ok {
			return SurfaceForm
		}
	}
	return SurfaceModal
}

// emitQuitting emits app.quitting with what changed since the last
// surface.left; the Model calls it once, when the application quits.
func (r *Runtime) emitQuitting() {
	h := r.eventHub()
	c := h.sinceLeft
	h.sinceLeft = surfaceCount{}
	r.Emit(EventAppQuitting, map[string]any{"changes": c.changes, "changed": c.nouns()})
}

// runHooks runs the waiting firings' hooks, in emit order; the events the
// hooks cause join the queue, up to HookChainLimit.
func (r *Runtime) runHooks() {
	h := r.events
	if h == nil || h.draining || len(h.queue) == 0 {
		return
	}
	h.draining = true
	defer func() { h.draining = false }()
	for n := 0; len(h.queue) > 0; n++ {
		if n >= HookChainLimit {
			r.UI().Notify(fmt.Sprintf("hooks: more than %d events in one burst; the rest were dropped (do two rows feed each other?)", HookChainLimit))
			h.queue = nil
			return
		}
		f := h.queue[0]
		h.queue = h.queue[1:]
		for _, row := range r.HookRows() {
			if row.Off || row.On != f.event || !row.Matches(f.fields) {
				continue
			}
			r.runHook(row, f)
		}
	}
}

// runHook runs one row's verb at the focus. The row is the user's (or the
// application's) standing request, so it runs confirmed. An application
// verb's work runs off the loop as a job; a failure, now or when the job
// ends, is a notice and never stops the other rows.
func (r *Runtime) runHook(row HookRow, f firing) {
	h := r.events
	q, err := row.request()
	if err != nil {
		r.UI().Notify(hookFailure(row, err))
		return
	}
	q.Confirmed = true
	if q.Args == nil {
		q.Args = Arguments{}
	}
	for _, arg := range slices.Sorted(maps.Keys(row.From)) {
		field := row.From[arg]
		if field == HookAllFields {
			q.Args[arg] = maps.Clone(f.fields)
			continue
		}
		if v, ok := f.fields[field]; ok {
			q.Args[arg] = v
		}
	}
	restore := r.forceSource("hook:" + f.event)
	h.hooking++
	value, err := r.Execute(q)
	h.hooking--
	restore()
	if err != nil {
		r.UI().Notify(hookFailure(row, err))
		return
	}
	if t, ok := value.(Ticket); ok {
		if j := r.jobs[t.Job]; j != nil {
			then := j.then
			j.then = func(c Ctx, v any, err error) error {
				if err != nil {
					r.UI().Notify(hookFailure(row, err))
				}
				if then != nil {
					return then(c, v, err)
				}
				return nil
			}
			if f.event == EventAppQuitting {
				h.quitJobs = append(h.quitJobs, t.Job)
			}
		}
	}
}

func hookFailure(row HookRow, err error) string {
	return fmt.Sprintf("hook %s → %s failed: %v", row.On, row.Do, err)
}

// quitHooksRunning reports whether a job app.quitting's hooks started is
// still running.
func (r *Runtime) quitHooksRunning() bool {
	h := r.events
	if h == nil {
		return false
	}
	for _, id := range h.quitJobs {
		if j := r.jobs[id]; j != nil && j.State == JobRunning {
			return true
		}
	}
	return false
}
