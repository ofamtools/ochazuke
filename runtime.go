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

// Arguments are a request's named arguments, keyed by parameter name.
type Arguments map[string]any

// Parameter describes one argument of an operation: its type, whether it is required, and its choices.
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
	// Noun says the value names one of that noun's things: a row of its
	// list verb (NOUN.list, when it is a read with no required parameter,
	// stands in for an empty Source), else the id of a live instance of
	// the noun. A Source still wins; Enum values come first.
	Noun string `json:"noun,omitempty"`
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

// Key returns the request's operation as noun.verb.
func (q Request) Key() string { return q.Noun + "." + q.Verb }

// Context is the derived view of a focus: the instance ids from app down to
// the focused instance (Path), and the focused input when there is one.
type Context struct {
	Path    []string `json:"path"`
	InputID string   `json:"inputId,omitempty"`
}

// Clone returns a copy whose Path does not share the original's backing array.
func (c Context) Clone() Context {
	return Context{Path: append([]string(nil), c.Path...), InputID: c.InputID}
}

// Handler runs an interaction operation on the UI loop and returns its result.
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
	// Builtin marks an operation the framework registered itself: every
	// one NewRuntime and NewModel register, and those a framework
	// constructor registers later (NewPicker, NewPalette, grid.Register,
	// markdown.Register, cli.AddCommands), each inside Runtime.Builtin. An
	// application's verb is never builtin, even on a framework noun
	// (history.capture-hook). Data for hosts (the catalog's builtin): a
	// host mounting another tool's verbs skips these, which every
	// ochazuke tool has; the runtime does not read it.
	Builtin bool `json:"builtin,omitempty"`
	// PersistArgs lets the history file keep this operation's arguments
	// named like text (text, value, keys, args, password, secret, token), which
	// it otherwise leaves out; set by NounBuilder.PersistArgs. Only for an
	// operation whose arguments are never typed secrets (settings.set).
	PersistArgs bool `json:"persistArgs,omitempty"`
	// Motion marks movement (list.move, focus.next, surface.scroll,
	// page.next): the footer's hint line leaves its rows out, since motions
	// are standard and the footer is for the verbs the focus offers; ?
	// help and bindings list keep them. Set by NounBuilder.Motion; the
	// framework's own motions are marked as they register (motionOps).
	Motion bool `json:"motion,omitempty"`
	// HistoryKind is the kind of entry the operation records
	// (HistoryAction, HistoryChange, HistoryFetch); empty derives it
	// (HistoryKindOf). Set it to HistoryFetch on a verb that only asks for
	// data (NounBuilder.Fetch, the catalog's history), so history.list's
	// default view and the bounded ring leave its calls to the fetches.
	HistoryKind string `json:"historyKind,omitempty"`
	// Positional names the parameters bare words fill first, in this
	// order (NounBuilder.Positional), ahead of ParameterOrder's
	// required-then-by-name rest. The CLI gives a boolean a bare word only
	// when it is named here. The catalog's positional.
	Positional []string   `json:"positional,omitempty"`
	Run        Handler    `json:"-"`
	App        AppHandler `json:"-"`
}

func (o Operation) Key() string { return o.Noun + "." + o.Verb }

// HistoryKindOf is the kind of history entry the operation records: its
// HistoryKind when set; else a fetch for a verb that answers rows (Rows,
// State), a change for a Repeatable one, an action for a Motion, a change
// for any other application verb (work that may change something outside
// the runtime: the safe reading of an extension verb that declares
// nothing), and an action for any other interaction verb.
func (o Operation) HistoryKindOf() string {
	switch {
	case o.HistoryKind != "":
		return o.HistoryKind
	case o.Rows != "" || o.State:
		return HistoryFetch
	case o.Repeatable:
		return HistoryChange
	case o.Motion:
		return HistoryAction
	case o.Kind == KindApplication:
		return HistoryChange
	}
	return HistoryAction
}

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
	Nouns          map[string]*Noun
	Instances      map[string]*Instance
	Focus          string    // the focused instance; Context() is derived from it
	Mode           ModeState // the one interaction mode; mode.set writes it, a focus change to another stop resets it
	Page           string    // the current page instance; always an ancestor of BaseFocus()
	Modes          map[string]bool
	Operations     map[string]Operation
	Quit           bool
	Pending        string
	BindingTimeout time.Duration
	// ResolveTarget, when set, maps a request target that is not an
	// instance (a foreign id: an extension's row id, a database key) to
	// the instance that stands for it; ExecuteAt consults it (TargetInstance)
	// before refusing an unknown target, so keys, the palette, the CLI and
	// the API all accept the foreign id. An error refuses the request with
	// it; "" and no error leaves the target unknown.
	ResolveTarget    func(target string) (string, error)
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
	holders          []string          // every instance that is not a list row, in registration order: the ones that may hold a Component (components())
	modals           []string          // the open modal instances, outermost first; each holds its return focus in Payload.Return
	modalInstances   map[string]bool   // instances created by Push or OpenModal, removed by Pop
	seq              uint64            // instance registration order
	commands         []tea.Cmd         // off-loop work scheduled since the adapter last drained it
	jobs             map[uint64]*job   // the jobs list: running jobs and the finished ones retained for jobs.list
	jobSeq           uint64            // last issued job id
	deprecations     []string          // rows that named an operation by its old verb.noun spelling, rewritten once
}

// NewRuntime returns a runtime with the framework nouns registered and focus on the app instance.
func NewRuntime() *Runtime {
	r := newRuntime()
	r.Builtin(r.registerFramework)
	return r
}

// newRuntime is a runtime with no operations, focus on app.
func newRuntime() *Runtime {
	return &Runtime{
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
}

// registerFramework registers the framework nouns and operations.
func (r *Runtime) registerFramework() {
	r.Instance("app")
	r.Define("modal")
	// "no-modal" holds while no modal is open: the app's ctrl+c capture row
	// quits past any inner row, but a modal (the palette) still answers it.
	r.DefineGuard("no-modal", func(r *Runtime, _ KeyEvent) bool { return len(r.modals) == 0 })
	registerCore(r)
	registerJobs(r)
	registerConfiguration(r)
	registerChooser(r)
	registerConfig(r)
	registerActions(r)
	registerSettings(r)
	registerSettingsEditor(r)
	registerConfigExport(r)
	registerConfigReload(r)
	registerPages(r)
	registerLists(r)
	registerTrees(r)
	registerForms(r)
	RegisterInputOperations(r)
	registerCompletion(r)
	registerRegisters(r)
	registerUI(r)
	registerYankHistory(r)
	registerBindingHelp(r)
	registerMacros(r)
	registerModeTables(r)
	registerEditorNoun(r)
	registerConfirm(r)
	registerTerminal(r)
	registerFocus(r)
	registerDetail(r)
	registerPanes(r)
	registerQuery(r)
	registerHistory(r)
	// A modal answers esc in normal mode; inputs, pending modes and the
	// palette settle first because their rows sit inside this level.
	r.Define("modal").Mode("normal").Bind("esc", Do("modal.pop"))
}

// Builtin runs register and marks every operation it registered as the
// framework's own (Operation.Builtin). NewRuntime and NewModel wrap their
// registrations in it, and so does every framework constructor that
// registers operations after them (NewPicker, NewPalette, NewAppearance,
// grid.Register, markdown.Register, cli.AddCommands), so an operation
// registered lazily is marked too. Operations registered before it, or by
// the application at any other time, keep Builtin false. Calls nest. It is
// for framework packages; an application's own verbs are not builtin.
func (r *Runtime) Builtin(register func()) {
	before := make(map[string]bool, len(r.Operations))
	for key := range r.Operations {
		before[key] = true
	}
	register()
	for key, op := range r.Operations {
		if !before[key] && !op.Builtin {
			op.Builtin = true
			r.Operations[key] = op
		}
	}
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
	"detail.scroll": true, "list.goto": true, "list.scroll": true, "list.scroll-columns": true,
	"list.column": true, "list.choose-column": true,
}

// frameworkHistoryKinds are the framework's operations whose history kind
// is not the derived one (Operation.HistoryKindOf): the reads and reloads
// are fetches, the interaction verbs that change a setting, a register, a
// record or a pane are changes, and the application verbs that only close
// or fold are actions.
var frameworkHistoryKinds = map[string]string{
	"actions.list": HistoryFetch, "actions.path": HistoryFetch, "actions.reload": HistoryFetch,
	"appearance.get": HistoryFetch, "bindings.check": HistoryFetch, "bindings.list": HistoryFetch,
	"bindings.path": HistoryFetch, "bindings.reload": HistoryFetch, "bindings.stops": HistoryFetch,
	"config.path": HistoryFetch, "config.reload": HistoryFetch, "history.list": HistoryFetch, "history.path": HistoryFetch,
	"instances.query": HistoryFetch, "jobs.list": HistoryFetch, "modes.list": HistoryFetch,
	"notice.list": HistoryFetch, "operations.list": HistoryFetch, "pages.list": HistoryFetch,
	"register.get": HistoryFetch, "settings.get": HistoryFetch, "settings.list": HistoryFetch,
	"settings.names": HistoryFetch, "settings.path": HistoryFetch, "settings.reload": HistoryFetch,
	"state.get": HistoryFetch, "themes.list": HistoryFetch, "themes.path": HistoryFetch,
	"themes.reload": HistoryFetch,

	"list.yank": HistoryChange, "list.resize-column": HistoryChange, "list.reset-columns": HistoryChange,

	"appearance.set": HistoryChange, "bindings.load": HistoryChange, "confirm.set": HistoryChange,
	"config.export": HistoryChange, "form-option.pick": HistoryChange, "form.redo": HistoryChange,
	"form.suggest": HistoryChange, "form.undo": HistoryChange, "input.redo": HistoryChange,
	"input.undo": HistoryChange, "input.yank": HistoryChange, "mode.register": HistoryChange,
	"register.set": HistoryChange, "setting-value.commit": HistoryChange, "setting.cycle": HistoryChange,
	"setting.edit": HistoryChange, "setting.reset": HistoryChange, "settings.reset": HistoryChange,
	"settings.set": HistoryChange, "terminal.paste": HistoryChange, "terminal.send": HistoryChange,
	"yank-history.select": HistoryChange,

	"chooser.cancel": HistoryAction, "outline.fold": HistoryAction,
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
	if kind := frameworkHistoryKinds[o.Key()]; kind != "" && o.HistoryKind == "" {
		o.HistoryKind = kind
	}
	switch o.HistoryKind {
	case "", HistoryAction, HistoryChange, HistoryFetch:
	default:
		panic("unknown history kind " + o.HistoryKind)
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

// AddInput registers an editor instance. Options: Parent(id) (default app),
// Kind(noun) (default "editor"; "prompt" pins insert mode) and
// Completions(triggers...).
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
	if len(spec.completions) > 0 {
		in.Completions = spec.completions
	}
	if err := validTriggers(in.Completions); err != nil {
		return err
	}
	in.pin = noun.Pin
	if in.pin != "" {
		in.SetMode(in.pin)
	}
	r.seq++
	inst := &Instance{ID: in.ID, Noun: spec.noun, Parent: spec.parent, Component: in, seq: r.seq, closeFocus: spec.closeFocus}
	r.register(inst)
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

// Execute runs an operation at the current focus context; see ExecuteAt.
func (r *Runtime) Execute(q Request) (any, error) { return r.ExecuteAt(r.Context(), q) }

// ExecuteAt runs an operation for a context. The innermost noun on the
// focus stack that overrides the operation handles it; otherwise its Run does.
// Every request is appended to the action history with its kind, the
// context it ran in, its outcome and where it moved the interaction.
func (r *Runtime) ExecuteAt(c Context, q Request) (any, error) {
	source := r.history.source
	if source == "" {
		source = "api"
	}
	at := time.Now()
	ctx := HistoryContext{Focus: focusOf(c), Page: r.Page, Mode: r.Mode.Name, Source: source}
	depth := r.history.depth
	from := r.place()
	r.history.depth++
	value, err := r.execute(c, q)
	r.history.depth--
	r.record(at, q, ctx, depth, from, value, err)
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
		return nil, &ArgumentError{err}
	}
	if q.Target != "" && q.Target != "focused-input" {
		id, err := r.TargetInstance(q.Target)
		if err != nil {
			return nil, err
		}
		q.Target = id
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

// TargetInstance is the instance a request target names: the target when
// it is an instance, else what ResolveTarget maps it to. An unknown target
// is an error naming it.
func (r *Runtime) TargetInstance(target string) (string, error) {
	if len(r.Ancestry(target)) > 0 {
		return target, nil
	}
	if r.ResolveTarget != nil {
		id, err := r.ResolveTarget(target)
		if err != nil {
			return "", fmt.Errorf("target %q: %w", target, err)
		}
		if id != "" {
			if len(r.Ancestry(id)) == 0 {
				return "", fmt.Errorf("target %q resolved to %q, which is not an instance", target, id)
			}
			return id, nil
		}
	}
	return "", fmt.Errorf("unknown target %q", target)
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
		"nouns": nouns, "instances": r.Instances, "components": r.componentSnapshot(), "jobs": r.runningJobs(), "history": r.HistoryOf(50, historyUserKinds...)}
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
	r.Register(Operation{Verb: "list", Noun: "bindings", Description: "List the rows the current stack and mode answer to; all includes every stored noun and instance mode table", Parameters: map[string]Parameter{"all": {Type: "boolean", Description: "Return every stored binding row, including inactive modes and contexts, in configuration JSON format"}}, Result: "binding[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if all, _ := a["all"].(bool); all {
			return r.allBindings(), nil
		}
		return r.ListBindings(focusOf(c)), nil
	}})
	r.Register(Operation{Verb: "stops", Noun: "bindings", Description: "List the rows every focus stop answers to, app-wide: each page's stops in drawing order, the open modals', then app; each with the focus and mode it is read at", Result: "stop bindings[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.StopBindings(), nil
	}})
	r.Register(Operation{Verb: "focus", Noun: "input", Description: "Focus a registered input", Parameters: map[string]Parameter{"id": {Type: "string", Required: true, Noun: "editor", Free: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		id := StringArg(a, "id", "")
		if r.Input(id) == nil {
			return nil, fmt.Errorf("unknown input %q", id)
		}
		err := r.SetFocus(id)
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "set", Noun: "mode", Description: "Set the global interaction mode (a registered mode) and clear any pending payload", Parameters: map[string]Parameter{"mode": {Type: "string", Required: true, Noun: "modes"}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
	r.Register(Operation{Verb: "push", Noun: "modal", Description: "Open a modal focused on a registered input", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}, "input": {Type: "string", Required: true, Noun: "editor", Free: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
