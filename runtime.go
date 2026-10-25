package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type Arguments map[string]any

type Parameter struct {
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
	// Enum lists the static choices of a string parameter; Source names an
	// operation (noun.verb) whose result lists more. Free also accepts a
	// value outside them.
	Enum   []string `json:"enum,omitempty"`
	Source string   `json:"source,omitempty"`
	Free   bool     `json:"free,omitempty"`
}

// Request names an operation and, optionally, the instance it addresses.
// Confirmed skips the operation's Confirm prompt: the confirm modal's y sets
// it on the stored request, and a script may set it to run unattended.
type Request struct {
	Verb      string    `json:"verb"`
	Noun      string    `json:"noun"`
	Target    string    `json:"target,omitempty"`
	Args      Arguments `json:"args,omitempty"`
	Confirmed bool      `json:"confirmed,omitempty"`
}

func (q Request) Key() string { return q.Noun + "." + q.Verb }

// Context is the derived view of a focus: the instance ids from app down to
// the focused instance (Path), and the focused input when there is one.
type Context struct {
	Path    []string `json:"path"`
	InputID string   `json:"inputId,omitempty"`
}

func (c Context) Clone() Context {
	return Context{Path: append([]string(nil), c.Path...), InputID: c.InputID}
}

type Handler func(*Runtime, Context, Arguments) (any, error)

// Operation is one registered noun.verb. Kind is "interaction" (the
// default: Run executes synchronously on the loop) or "application" (App is
// an AppHandler whose pending work runs off the loop and reports through the
// delivery contract). Confirm, when set, is a prompt template ({{id}},
// {{noun}}, {{label}}, {{<arg>}}) shown in a confirm modal before the operation runs.
// Repeatable marks a change (not a motion or navigation) that dot-repeat
// (history.repeat, bound to . on app in normal mode) may run again; set it
// on the Operation passed to Register.
type Operation struct {
	Verb        string               `json:"verb"`
	Noun        string               `json:"noun"`
	Description string               `json:"description"`
	Parameters  map[string]Parameter `json:"parameters"`
	Result      string               `json:"result"`
	Kind        string               `json:"kind"`
	Confirm     string               `json:"confirm,omitempty"`
	Repeatable  bool                 `json:"repeatable,omitempty"`
	// Rows names the noun whose rows the verb's result lists (a list
	// verb's JSON array of rows of that noun); "" when the result is not
	// rows. It is data for hosts (the catalog's rows); the runtime does
	// not read it.
	Rows string `json:"rows,omitempty"`
	// State marks the verb whose answer is the rows of every one of a
	// host's lists in one call (NounBuilder.State), as JSON Lines of
	// cli.Row tagged by list noun ({"noun":"agent","id":..,"label":..}).
	// Data for hosts (the catalog's state); the runtime does not read it.
	State bool `json:"state,omitempty"`
	// Quiet marks bookkeeping (a list reload) the jobs ticker does not
	// show; set by NounBuilder.Quiet. The catalog's quiet.
	Quiet bool `json:"quiet,omitempty"`
	// Motion marks movement (list.move, focus.next, surface.scroll,
	// page.next): the footer's hint line leaves its rows out, since motions
	// are standard and the footer is for the verbs the focus offers; ?
	// help and bindings list keep them. Set by NounBuilder.Motion; the
	// framework's own motions are marked as they register (motionOps).
	Motion bool `json:"motion,omitempty"`
	// Positional names the parameters bare words fill first, in this
	// order (NounBuilder.Positional), ahead of ParameterOrder's
	// required-then-by-name rest. The CLI gives a boolean a bare word only
	// when it is named here. The catalog's positional.
	Positional []string   `json:"positional,omitempty"`
	Run        Handler    `json:"-"`
	App        AppHandler `json:"-"`
}

func (o Operation) Key() string { return o.Noun + "." + o.Verb }

// ParameterOrder lists the operation's parameters in positional order:
// the Positional names first, in their order, then the rest required
// first, then by name. Parameters is a map, so this is the declared order
// the palette's words and the CLI's bare arguments fill.
func (o Operation) ParameterOrder() []string {
	names := make([]string, 0, len(o.Parameters))
	var first []string
	for _, name := range o.Positional {
		if _, ok := o.Parameters[name]; ok && !slices.Contains(first, name) {
			first = append(first, name)
		}
	}
	for name := range o.Parameters {
		if !slices.Contains(first, name) {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		ri, rj := o.Parameters[names[i]].Required, o.Parameters[names[j]].Required
		if ri != rj {
			return ri
		}
		return names[i] < names[j]
	})
	return append(first, names...)
}

// Runtime is owned by one event loop. Transport callers submit messages to
// that loop. It holds the operation registry (Operations, Modes and their
// guards), the instance registry (Nouns, Instances and their parent links,
// including list rows), the three plain interaction fields Focus, Mode and
// Page with the chain of open modals, the pending key-sequence state (Pending, prefix, BindingTimeout) and the hook
// that hands off-loop work to the adapter (commands). Components such as
// inputs, lists, the picker and the registers are instances, never fields;
// TestRuntimeHoldsNoComponentFields pins this list.
type Runtime struct {
	Nouns            map[string]*Noun
	Instances        map[string]*Instance
	Focus            string    // the focused instance; Context() is derived from it
	Mode             ModeState // the one interaction mode; mode.set writes it, a focus change to another stop resets it
	Page             string    // the current page instance; always an ancestor of BaseFocus()
	Modes            map[string]bool
	Operations       map[string]Operation
	Quit             bool
	Pending          string
	BindingTimeout   time.Duration
	pendingModes     map[string]bool
	passthroughModes map[string]bool // base modes in which capture rows stand aside (DefinePassthrough)
	guards           map[string]Guard
	predicates       map[string]PredicateFunc // named query predicates (query.go)
	history          actionHistory            // the executed-request ring and the current source (history.go)
	origin           string                   // the instance the running request or completion acts for; a modal opens only from the top level
	rowRequest       bool                     // the next execute is a binding row's own request (runRow's level rule targets it)
	prefix           pendingPrefix
	bindingVersion   uint64
	modeGeneration   uint64
	rows             map[string]string // list row instance id -> owning list id
	modals           []string          // the open modal instances, outermost first; each holds its return focus in Payload.Return
	modalInstances   map[string]bool   // instances created by Push or OpenModal, removed by Pop
	seq              uint64            // instance registration order
	commands         []tea.Cmd         // off-loop work scheduled since the adapter last drained it
	jobs             map[uint64]*job   // the jobs list: running jobs and the finished ones retained for jobs.list
	jobSeq           uint64            // last issued job id
	deprecations     []string          // rows that named an operation by its old verb.noun spelling, rewritten once
}

func NewRuntime() *Runtime {
	r := &Runtime{
		Nouns: map[string]*Noun{}, Instances: map[string]*Instance{}, rows: map[string]string{},
		Focus: "app", Mode: ModeState{Name: "normal"},
		Modes: map[string]bool{"normal": true, "insert": true}, Operations: map[string]Operation{},
		pendingModes:     map[string]bool{},
		passthroughModes: map[string]bool{},
		guards:           builtinGuards(),
		predicates:       builtinPredicates(),
		modalInstances:   map[string]bool{},
		BindingTimeout:   DefaultBindingTimeout,
	}
	r.Instance("app")
	r.Define("modal")
	// "no-modal" holds while no modal is open: the app's ctrl+c capture row
	// quits past any inner row, but a modal (the palette) still answers it.
	r.DefineGuard("no-modal", func(r *Runtime, _ KeyEvent) bool { return len(r.modals) == 0 })
	registerCore(r)
	registerJobs(r)
	registerConfiguration(r)
	registerPages(r)
	registerLists(r)
	RegisterInputOperations(r)
	registerRegisters(r)
	registerYankHistory(r)
	registerBindingHelp(r)
	registerMacros(r)
	registerModeTables(r)
	registerEditorNoun(r)
	registerConfirm(r)
	registerTerminal(r)
	registerFocus(r)
	registerQuery(r)
	registerHistory(r)
	// A modal answers esc in normal mode; inputs, pending modes and the
	// palette settle first because their rows sit inside this level.
	r.Define("modal").Mode("normal").Bind("esc", Do("modal.pop"))
	return r
}

// ModeState is a mode name with the payload the mode carries.
type ModeState struct {
	Name    string  `json:"name"`
	Payload Payload `json:"payload"`
}

// BaseMode is the mode Runtime.Mode stands in: its name, or for a pending
// mode or a key-sequence prefix the base mode it returns to.
func (r *Runtime) BaseMode() string {
	if (r.pendingModes[r.Mode.Name] || r.Mode.Payload.Prefix != "") && r.Mode.Payload.Return != "" {
		return r.Mode.Payload.Return
	}
	return r.Mode.Name
}

// ModeObserver is implemented by a component that follows the base mode
// while the focus sits on it or beneath it: the grid starts and ends its
// selection when a visual shape is entered or left.
type ModeObserver interface {
	ModeChanged(from, to string)
}

// setMode replaces the global mode and tells the observers on the focus path
// (and on extra, the path focus is leaving) when the base mode changed.
func (r *Runtime) setMode(m ModeState, extra ...string) {
	before := r.BaseMode()
	r.Mode = m
	r.modeGeneration++
	after := r.BaseMode()
	if before == after {
		return
	}
	seen := map[string]bool{}
	for _, id := range append(r.Ancestry(r.Focus), extra...) {
		if seen[id] {
			continue
		}
		seen[id] = true
		if inst := r.Instances[id]; inst != nil {
			if obs, ok := inst.Component.(ModeObserver); ok {
				obs.ModeChanged(before, after)
			}
		}
	}
}

// SetMode sets the global mode by name and clears its payload, as mode.set
// does: a focused input runs its own transition (insert sessions, visual
// anchor), and a pinned input stays in its pinned mode.
func (r *Runtime) SetMode(mode string) error {
	if !r.Modes[mode] {
		return fmt.Errorf("unknown mode %q", mode)
	}
	if in := r.Input(r.Focus); in != nil {
		in.SetMode(mode)
		return nil
	}
	r.setMode(ModeState{Name: mode})
	return nil
}

// motionOps are the framework's own motions (Operation.Motion): moving a
// selection, a cursor, the focus between stops, the page or the viewport.
var motionOps = map[string]bool{
	"list.move": true, "focus.next": true, "focus.previous": true,
	"surface.scroll": true, "page.next": true, "page.previous": true,
	"input.move": true, "picker.move": true, "palette.move": true,
	"yank-history.move": true, "grid.move": true, "grid.goto": true,
}

// Register adds an operation. An operation with App set is of kind
// application and its Run is the framework's dispatcher for it.
func (r *Runtime) Register(o Operation) {
	if o.App != nil {
		if o.Run != nil {
			panic("operation takes Run or App, not both")
		}
		o.Kind = KindApplication
		o.Run = func(r *Runtime, c Context, a Arguments) (any, error) {
			return r.runApplication(r.Operations[o.Key()], c, a)
		}
	}
	if o.Verb == "" || o.Noun == "" || o.Run == nil {
		panic("invalid operation registration")
	}
	if motionOps[o.Key()] {
		o.Motion = true
	}
	if _, exists := r.Operations[o.Key()]; exists {
		panic("duplicate operation: " + o.Key())
	}
	if o.Parameters == nil {
		o.Parameters = map[string]Parameter{}
	}
	switch o.Kind {
	case "":
		o.Kind = KindInteraction
	case KindInteraction, KindApplication:
	default:
		panic("unknown operation kind " + o.Kind)
	}
	r.Operations[o.Key()] = o
}

// AddInput registers an editor instance. Options: Parent(id) (default app)
// and Kind(noun) (default "editor"; "prompt" pins insert mode).
func (r *Runtime) AddInput(in *Input, opts ...InstanceOption) error {
	if in.ID == "" || r.Instances[in.ID] != nil {
		return errors.New("input ID must be unique and nonempty")
	}
	spec := instanceSpec{parent: "app", noun: "editor"}
	for _, opt := range opts {
		opt(&spec)
	}
	if r.Instances[spec.parent] == nil {
		return fmt.Errorf("unknown parent %q", spec.parent)
	}
	noun := r.Nouns[spec.noun]
	if noun == nil {
		return fmt.Errorf("unknown noun %q", spec.noun)
	}
	switch in.Kind {
	case "", "text", "path":
	case "paths":
		in.Multiline = true
	default:
		return errors.New("input kind must be text, path, or paths")
	}
	switch in.PathFilter {
	case "", "any", "file", "dir":
	default:
		return errors.New("path filter must be any, file, or dir")
	}
	in.pin = noun.Pin
	if in.pin != "" {
		in.SetMode(in.pin)
	}
	r.seq++
	inst := &Instance{ID: in.ID, Noun: spec.noun, Parent: spec.parent, Component: in, seq: r.seq, closeFocus: spec.closeFocus}
	r.Instances[in.ID] = inst
	in.r = r
	return nil
}

// Input returns the input an instance fronts, or nil.
func (r *Runtime) Input(id string) *Input {
	if inst := r.Instances[id]; inst != nil {
		if in, ok := inst.Component.(*Input); ok {
			return in
		}
	}
	return nil
}

// List returns the list an instance fronts, or nil.
func (r *Runtime) List(id string) *List {
	if inst := r.Instances[id]; inst != nil {
		if list, ok := inst.Component.(*List); ok {
			return list
		}
	}
	return nil
}

// Inputs lists the registered inputs in registration order.
func (r *Runtime) Inputs() []*Input {
	var out []*Input
	for _, inst := range r.instancesInOrder() {
		if in, ok := inst.Component.(*Input); ok {
			out = append(out, in)
		}
	}
	return out
}

// Lists lists the registered lists in registration order.
func (r *Runtime) Lists() []*List {
	var out []*List
	for _, inst := range r.instancesInOrder() {
		if list, ok := inst.Component.(*List); ok {
			out = append(out, list)
		}
	}
	return out
}

func (r *Runtime) instancesInOrder() []*Instance {
	out := make([]*Instance, 0, len(r.Instances))
	for _, inst := range r.Instances {
		out = append(out, inst)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

func (r *Runtime) Execute(q Request) (any, error) { return r.ExecuteAt(r.Context(), q) }

// ExecuteAt runs an operation for a context. The innermost noun on the
// focus stack that overrides the operation handles it; otherwise its Run does.
// Every request but history.list and state.get is appended to the action
// history with the context it ran in and its result.
func (r *Runtime) ExecuteAt(c Context, q Request) (any, error) {
	if unrecorded[q.Key()] {
		return r.execute(c, q)
	}
	source := r.history.source
	if source == "" {
		source = "api"
	}
	at := time.Now()
	ctx := HistoryContext{Focus: focusOf(c), Page: r.Page, Mode: r.Mode.Name, Source: source}
	depth := r.history.depth
	r.history.depth++
	value, err := r.execute(c, q)
	r.history.depth--
	r.record(at, q, ctx, depth, value, err)
	return value, err
}

func (r *Runtime) execute(c Context, q Request) (any, error) {
	fromRow := r.rowRequest
	r.rowRequest = false
	op, ok := r.Operations[q.Key()]
	if !ok {
		return nil, fmt.Errorf("unknown operation %q", q.Key())
	}
	if err := r.checkArguments(op, q.Args); err != nil {
		return nil, err
	}
	if q.Target != "" && q.Target != "focused-input" {
		if len(r.Ancestry(q.Target)) == 0 {
			return nil, fmt.Errorf("unknown target %q", q.Target)
		}
		c = r.contextOf(q.Target)
	}
	if len(c.Path) == 0 && c.InputID == "" {
		return nil, errors.New("interaction path must start at app")
	}
	focus := focusOf(c)
	if r.Instances[focus] == nil {
		return nil, fmt.Errorf("unknown instance %q", focus)
	}
	if c.InputID != "" && r.Input(c.InputID) == nil {
		return nil, fmt.Errorf("unknown input %q", c.InputID)
	}
	// Guards are applicability: a verb guarded off where the request acts
	// (its target, else the caller's focus) is refused, from keys, the
	// palette, the CLI and the API alike.
	if !r.VerbShown(q.Key(), focus, r.Mode) {
		return nil, fmt.Errorf("%s is not applicable now", q.Key())
	}
	// A request without a target addresses the nearest instance of its
	// operation's noun on the focus path (row.delete chosen in the palette
	// while a cell is focused acts for the row; its confirm names the row).
	// A binding row's own request keeps runRow's rule instead: it targets
	// the level the row matched at, and a shared operation keeps the focus.
	// Noun actions still resolve from the caller's focus, so an override on
	// a level between it and the owner (the notes editor's save) applies.
	actions := focus
	if q.Target == "" && !fromRow {
		if owner := r.nounInstance(q.Noun, focus); owner != "" && owner != focus {
			c, focus = r.contextOf(owner), owner
		}
	}
	defer r.actingFor(focus)()
	if op.Confirm != "" && !q.Confirmed {
		return r.openConfirm(c, q, op)
	}
	if fn := r.ActionFor(actions, q.Key()); fn != nil {
		return fn(r, c, q.Args)
	}
	return op.Run(r, c, q.Args)
}

// nounInstance is the nearest instance of noun on the focus path, "" when
// none is. app is never chosen: an app verb acts for the focus.
func (r *Runtime) nounInstance(noun, focus string) string {
	for _, id := range r.Ancestry(focus) {
		if id != "app" && r.Instances[id].Noun == noun {
			return id
		}
	}
	return ""
}

func validateArguments(op Operation, args Arguments) error {
	for name, p := range op.Parameters {
		if _, ok := args[name]; p.Required && !ok {
			return fmt.Errorf("%s is required", name)
		}
	}
	for name, v := range args {
		p, ok := op.Parameters[name]
		if !ok {
			return fmt.Errorf("unknown argument %q", name)
		}
		valid := false
		switch p.Type {
		case "string":
			_, valid = v.(string)
		case "integer":
			switch n := v.(type) {
			case int:
				valid = true
			case float64:
				valid = n == float64(int(n))
			}
		case "boolean":
			_, valid = v.(bool)
		case "array":
			switch v.(type) {
			case []any, []Predicate:
				valid = true
			}
		case "object":
			switch v.(type) {
			case map[string]any, Arguments, Request, *Request:
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("%s must be %s", name, p.Type)
		}
		if s, ok := v.(string); ok && len(p.Enum) > 0 && p.Source == "" && !p.Free && !slices.Contains(p.Enum, s) {
			return fmt.Errorf("%s must be one of %s", name, strings.Join(p.Enum, ", "))
		}
	}
	return nil
}
func StringArg(a Arguments, name, fallback string) string {
	if v, ok := a[name].(string); ok {
		return v
	}
	return fallback
}
func IntArg(a Arguments, name string, fallback int) int {
	switch v := a[name].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return fallback
}
func (r *Runtime) Catalog() []Operation {
	out := make([]Operation, 0, len(r.Operations))
	for _, op := range r.Operations {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

func (r *Runtime) Snapshot() map[string]any {
	inputs := map[string]any{}
	for _, in := range r.Inputs() {
		inputs[in.ID] = in.Snapshot()
	}
	lists := map[string]any{}
	for _, list := range r.Lists() {
		lists[list.id] = list.Snapshot()
	}
	modes := make([]string, 0, len(r.Modes)+len(r.pendingModes))
	for mode := range r.Modes {
		modes = append(modes, mode)
	}
	for mode := range r.pendingModes {
		modes = append(modes, mode)
	}
	sort.Strings(modes)
	nouns := map[string]any{}
	for name, noun := range r.Nouns {
		actions := make([]string, 0, len(noun.Actions))
		for key := range noun.Actions {
			actions = append(actions, key)
		}
		sort.Strings(actions)
		nouns[name] = map[string]any{"name": name, "verbs": noun.Verbs, "bindings": noun.Bindings, "actions": actions, "pin": noun.Pin}
	}
	// Registers and macro state belong to the registers component, never individual fields.
	pages := map[string]any{}
	for _, page := range r.PageList() {
		pages[page.ID] = page
	}
	regs := r.Registers()
	return map[string]any{"modals": r.modalIDs(), "pages": pages, "page": r.Page, "inputs": inputs, "context": r.Context(), "focus": r.Focus, "mode": r.Mode.Name, "modeState": r.Mode, "modes": modes, "bindings": r.ListBindings(r.Focus), "pending": r.Pending, "quit": r.Quit,
		"lists": lists, "registers": regs.Named, "yankHistory": regs.History, "activeRegister": regs.Active, "macro": regs.Macro,
		"nouns": nouns, "instances": r.Instances, "components": r.componentSnapshot(), "jobs": r.runningJobs(), "history": r.History(50)}
}
func JSONPointer(value any, path string) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var node any
	if err = json.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	if path == "" {
		return node, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, errors.New("state path must be a JSON Pointer starting with /")
	}
	for _, raw := range strings.Split(path[1:], "/") {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch v := node.(type) {
		case map[string]any:
			var ok bool
			node, ok = v[key]
			if !ok {
				return nil, fmt.Errorf("unknown state path %q", path)
			}
		case []any:
			n, e := strconv.Atoi(key)
			if e != nil || n < 0 || n >= len(v) {
				return nil, fmt.Errorf("invalid array index %q", key)
			}
			node = v[n]
		default:
			return nil, fmt.Errorf("cannot descend into %q", key)
		}
	}
	return node, nil
}

func registerCore(r *Runtime) {
	r.Register(Operation{Verb: "get", Noun: "state", Description: "Inspect public state using a JSON Pointer", Parameters: map[string]Parameter{"path": {Type: "string"}}, Result: "JSON value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return JSONPointer(r.Snapshot(), StringArg(a, "path", ""))
	}})
	r.Register(Operation{Verb: "list", Noun: "operations", Description: "List registered verbs, nouns, kinds (interaction or application), confirm prompts and argument schemas", Result: "operation[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) { return r.Catalog(), nil }})
	r.Register(Operation{Verb: "list", Noun: "bindings", Description: "List the rows the current stack and mode answer to, pending modes included", Result: "binding[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) { return r.ListBindings(focusOf(c)), nil }})
	r.Register(Operation{Verb: "focus", Noun: "input", Description: "Focus a registered input", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		id := StringArg(a, "id", "")
		if r.Input(id) == nil {
			return nil, fmt.Errorf("unknown input %q", id)
		}
		err := r.SetFocus(id)
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "set", Noun: "mode", Description: "Set the global interaction mode (a registered mode) and clear any pending payload", Parameters: map[string]Parameter{"mode": {Type: "string", Required: true}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		mode := StringArg(a, "mode", "")
		if !r.Modes[mode] {
			return nil, fmt.Errorf("unknown mode %q", mode)
		}
		r.modeGeneration++
		if in := r.Input(c.InputID); in != nil {
			in.SetMode(mode)
			return in.Mode, nil
		}
		r.setMode(ModeState{Name: mode})
		return mode, nil
	}})
	r.Register(Operation{Verb: "push", Noun: "modal", Description: "Open a modal focused on a registered input", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}, "input": {Type: "string", Required: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		id := StringArg(a, "input", "")
		if r.Input(id) == nil {
			return nil, errors.New("unknown modal input")
		}
		err := r.Push(StringArg(a, "id", ""), id)
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "pop", Noun: "modal", Description: "Close the active modal and restore its parent", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) { err := r.Pop(); return r.Context(), err }})
	r.Register(Operation{Verb: "quit", Noun: "app", Description: "Exit the application", Result: "boolean", Run: func(r *Runtime, c Context, a Arguments) (any, error) { r.Quit = true; return true, nil }})
	r.Register(Operation{Verb: "ignore", Noun: "key", Description: "Consume a key without effect; the row a noun binds to swallow keys", Result: "null", Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
}
