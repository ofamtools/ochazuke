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
// {{noun}}, {{<arg>}}) shown in a confirm modal before the operation runs.
type Operation struct {
	Verb        string               `json:"verb"`
	Noun        string               `json:"noun"`
	Description string               `json:"description"`
	Parameters  map[string]Parameter `json:"parameters"`
	Result      string               `json:"result"`
	Kind        string               `json:"kind"`
	Confirm     string               `json:"confirm,omitempty"`
	Run         Handler              `json:"-"`
	App         AppHandler           `json:"-"`
}

func (o Operation) Key() string { return o.Noun + "." + o.Verb }

// Frame is one entry of the modal stack: the base page frame or an open
// modal. Focus names the instance that receives keys while the frame is on
// top; Context() derives the old path view from it.
type Frame struct {
	ID    string    `json:"id"`
	Focus string    `json:"focus"`
	mode  ModeState // the mode the frame beneath was in when this modal opened
}

// Runtime is owned by one event loop. Transport callers submit messages to
// that loop. It holds the operation registry (Operations, Modes and their
// guards), the instance registry (Nouns, Instances and their parent links,
// including list rows), focus and the modal chain (Frames, Page), the
// pending key-sequence state (Pending, prefix, BindingTimeout) and the hook
// that hands off-loop work to the adapter (commands). Components such as
// inputs, lists, the picker and the registers are instances, never fields;
// TestRuntimeHoldsNoComponentFields pins this list.
type Runtime struct {
	Nouns          map[string]*Noun
	Instances      map[string]*Instance
	Frames         []*Frame
	Page           string    // the current page instance; always on the base frame's focus path
	Mode           ModeState // the one interaction mode; mode.set writes it, a focus change to another stop resets it
	Modes          map[string]bool
	Operations     map[string]Operation
	Quit           bool
	Pending        string
	BindingTimeout time.Duration
	pendingModes   map[string]bool
	guards         map[string]Guard
	prefix         pendingPrefix
	bindingVersion uint64
	modeGeneration uint64
	rows           map[string]string // list row instance id -> owning list id
	modalInstances map[string]bool   // instances created by Push or OpenModal, removed by Pop
	seq            uint64            // instance registration order
	commands       []tea.Cmd         // off-loop work scheduled since the adapter last drained it
	jobs           map[uint64]*job   // the jobs list: running jobs and the finished ones retained for jobs.list
	jobSeq         uint64            // last issued job id
	deprecations   []string          // rows that named an operation by its old verb.noun spelling, rewritten once
}

func NewRuntime() *Runtime {
	r := &Runtime{
		Nouns: map[string]*Noun{}, Instances: map[string]*Instance{}, rows: map[string]string{},
		Frames: []*Frame{{ID: "main", Focus: "app"}}, Mode: ModeState{Name: "normal"},
		Modes: map[string]bool{"normal": true, "insert": true}, Operations: map[string]Operation{},
		pendingModes:   map[string]bool{},
		guards:         builtinGuards(),
		modalInstances: map[string]bool{},
		BindingTimeout: DefaultBindingTimeout,
	}
	r.Instance("app")
	r.Define("modal")
	registerCore(r)
	registerJobs(r)
	registerConfiguration(r)
	registerPages(r)
	registerLists(r)
	RegisterInputOperations(r)
	registerRegisters(r)
	registerYankHistory(r)
	registerMacros(r)
	registerModeTables(r)
	registerEditorNoun(r)
	registerConfirm(r)
	registerTerminal(r)
	// A modal answers esc in normal mode; inputs, pending modes and the
	// palette settle first because their rows sit inside this level.
	r.Define("modal").Mode("normal").Bind("esc", Do("modal.pop"))
	return r
}

func (r *Runtime) top() *Frame { return r.Frames[len(r.Frames)-1] }

// Context derives the active frame's path view from its focus.
func (r *Runtime) Context() Context { return r.contextOf(r.top().Focus) }

// FocusID is the instance the active frame points at.
func (r *Runtime) FocusID() string { return r.top().Focus }

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
	for _, id := range append(r.Ancestry(r.FocusID()), extra...) {
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
	if in := r.Input(r.FocusID()); in != nil {
		in.SetMode(mode)
		return nil
	}
	r.setMode(ModeState{Name: mode})
	return nil
}

// stopOf is the focus stop an instance belongs to: the nearest focusable
// instance on its ancestry (itself included), else the instance itself.
func (r *Runtime) stopOf(id string) string {
	for _, step := range r.Ancestry(id) {
		if r.focusable(step) {
			return step
		}
	}
	return id
}

// focusable reports whether an instance is a focus stop: its noun is
// declared Focusable, or it fronts an input, and it has not opted out.
func (r *Runtime) focusable(id string) bool {
	inst := r.Instances[id]
	if inst == nil || inst.Unfocusable {
		return false
	}
	if _, ok := inst.Component.(*Input); ok {
		return true
	}
	noun := r.Nouns[inst.Noun]
	return noun != nil && noun.Focusable
}

// focusMoved applies the mode rule to a focus change. Moving within one
// focus stop (a grid's cell to the next cell, a list's row to the next row)
// keeps the mode; moving to another stop resets it to normal with an empty
// payload, or to the pinned mode of the newly focused noun (prompt pins
// insert, terminal pins terminal). A focused input then follows the mode.
func (r *Runtime) focusMoved(fromStop string, fromPath []string, to string) {
	pin := ""
	if inst := r.Instances[to]; inst != nil {
		if noun := r.Nouns[inst.Noun]; noun != nil {
			pin = noun.Pin
		}
	}
	in := r.Input(to)
	if in != nil && in.pin != "" {
		pin = in.pin
	}
	switch {
	case pin != "":
		if r.BaseMode() != pin || !r.Mode.Payload.empty() {
			r.setMode(ModeState{Name: pin}, fromPath...)
		}
	case fromStop != r.stopOf(to):
		r.setMode(ModeState{Name: "normal"}, fromPath...)
	}
	if in != nil && in.Mode != r.BaseMode() {
		in.transition(r.BaseMode(), false)
	}
}

// moveFocus points a frame at id; when the frame is the active one the mode
// rule of focusMoved applies.
func (r *Runtime) moveFocus(frame *Frame, id string) {
	if frame != r.top() {
		frame.Focus = id
		return
	}
	from := frame.Focus
	stop, path := r.stopOf(from), r.Ancestry(from)
	frame.Focus = id
	if from != id {
		r.focusMoved(stop, path, id)
	}
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
	inst := &Instance{ID: in.ID, Noun: spec.noun, Parent: spec.parent, Component: in, seq: r.seq}
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

// Focus points the active frame at one instance: an input, a list, a row, a
// page or a modal body. Without a modal, the focus must sit inside the active
// page; a hidden page's instances are addressed with an explicit target.
func (r *Runtime) Focus(id string) error {
	if err := r.validateFocus(id); err != nil {
		return err
	}
	r.setFocus(r.top(), id)
	return nil
}

func (r *Runtime) validateFocus(id string) error {
	ancestry := r.Ancestry(id)
	if len(ancestry) == 0 || ancestry[len(ancestry)-1] != "app" {
		return fmt.Errorf("unknown instance %q", id)
	}
	if r.PageOf(r.Page) == nil {
		return nil
	}
	if len(r.Frames) == 1 && (len(ancestry) < 2 || ancestry[len(ancestry)-2] != r.Page) {
		return errors.New("select a context inside the current page; open a modal for a modal input")
	}
	for _, step := range ancestry[:len(ancestry)-1] {
		if r.PageOf(step) != nil && step != r.Page {
			return fmt.Errorf("open page %q before selecting its context", step)
		}
	}
	return nil
}

// setFocus moves a frame's focus and drops any key-sequence prefix.
func (r *Runtime) setFocus(frame *Frame, id string) {
	if frame.Focus != id {
		r.CancelPendingBinding()
	}
	r.moveFocus(frame, id)
}

// Push opens a modal focused on frame.Focus. A frame id that is not already
// an instance becomes one of the modal noun for as long as it is open.
func (r *Runtime) Push(frame *Frame) error {
	if frame == nil || frame.ID == "" {
		return errors.New("modal ID is required")
	}
	for _, f := range r.Frames {
		if f.ID == frame.ID {
			return errors.New("modal is already open")
		}
	}
	if frame.Focus == "" {
		frame.Focus = "app"
	}
	if len(r.Ancestry(frame.Focus)) == 0 {
		return fmt.Errorf("unknown instance %q", frame.Focus)
	}
	if r.Instances[frame.ID] == nil {
		r.Instances[frame.ID] = &Instance{ID: frame.ID, Noun: "modal"}
		r.modalInstances[frame.ID] = true
	}
	from := r.FocusID()
	stop, path := r.stopOf(from), r.Ancestry(from)
	r.CancelPendingBinding()
	frame.mode = r.Mode
	r.Frames = append(r.Frames, frame)
	if frame.Focus != from {
		r.focusMoved(stop, path, frame.Focus)
	}
	return nil
}

// OpenModal opens a modal whose body is a new instance of noun kind (its id
// is the kind), parented at the current focus and carrying payload, and
// focuses it. The instance lives until Pop closes the modal. The kind's
// rows answer first, then the focus it opened over, then the modal noun.
func (r *Runtime) OpenModal(kind string, payload Payload) (*Instance, error) {
	if kind == "" {
		return nil, errors.New("modal kind is required")
	}
	if r.Instances[kind] != nil {
		return nil, fmt.Errorf("modal %q is already open", kind)
	}
	r.Define(kind)
	r.seq++
	inst := &Instance{ID: kind, Noun: kind, Parent: r.FocusID(), Payload: payload, seq: r.seq}
	r.Instances[kind] = inst
	r.modalInstances[kind] = true
	if err := r.Push(&Frame{ID: kind, Focus: kind}); err != nil {
		delete(r.Instances, kind)
		delete(r.modalInstances, kind)
		return nil, err
	}
	return inst, nil
}

// Pop closes the active modal. An instance the modal created is closed
// through the delivery contract and removed.
func (r *Runtime) Pop() error {
	if len(r.Frames) == 1 {
		return errors.New("no modal to close")
	}
	frame := r.top()
	r.CancelPendingBinding()
	path := r.Ancestry(frame.Focus)
	r.Frames = r.Frames[:len(r.Frames)-1]
	if r.modalInstances[frame.ID] {
		delete(r.modalInstances, frame.ID)
		r.closeInstance(frame.ID)
		delete(r.Instances, frame.ID)
	}
	r.restoreMode(frame.mode, path)
	return nil
}

// restoreMode returns to the mode a modal was opened over; the focused
// input follows it.
func (r *Runtime) restoreMode(m ModeState, leaving []string) {
	if m.Name == "" {
		m.Name = "normal"
	}
	r.setMode(m, leaving...)
	if in := r.Input(r.FocusID()); in != nil && in.Mode != r.BaseMode() {
		in.transition(r.BaseMode(), false)
	}
}
func (r *Runtime) Execute(q Request) (any, error) { return r.ExecuteAt(r.Context(), q) }

// ExecuteAt runs an operation for a context. The innermost noun on the
// focus stack that overrides the operation handles it; otherwise its Run does.
func (r *Runtime) ExecuteAt(c Context, q Request) (any, error) {
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
	if op.Confirm != "" && !q.Confirmed {
		return r.openConfirm(c, q, op)
	}
	if fn := r.ActionFor(focus, q.Key()); fn != nil {
		return fn(r, c, q.Args)
	}
	return op.Run(r, c, q.Args)
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
	return map[string]any{"frames": r.Frames, "pages": pages, "page": r.Page, "inputs": inputs, "context": r.Context(), "focus": r.FocusID(), "mode": r.Mode.Name, "modeState": r.Mode, "modes": modes, "bindings": r.ListBindings(r.FocusID()), "pending": r.Pending, "quit": r.Quit,
		"lists": lists, "registers": regs.Named, "yankHistory": regs.History, "activeRegister": regs.Active, "macro": regs.Macro,
		"nouns": nouns, "instances": r.Instances, "components": r.componentSnapshot(), "jobs": r.runningJobs()}
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
		err := r.Focus(id)
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
		err := r.Push(&Frame{ID: StringArg(a, "id", ""), Focus: id})
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "pop", Noun: "modal", Description: "Close the active modal and restore its parent", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) { err := r.Pop(); return r.Context(), err }})
	r.Register(Operation{Verb: "quit", Noun: "app", Description: "Exit the application", Result: "boolean", Run: func(r *Runtime, c Context, a Arguments) (any, error) { r.Quit = true; return true, nil }})
	r.Register(Operation{Verb: "ignore", Noun: "key", Description: "Consume a key without effect; the row a noun binds to swallow keys", Result: "null", Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
}
