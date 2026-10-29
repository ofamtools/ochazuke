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

// AnyParameterType is the Parameter.Type of a value of any type: a word
// from the CLI or the palette arrives as text, an API request's value as
// it was sent, and the handler types it (a standard verb's prop value,
// propValue).
const AnyParameterType = "any"

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
	// A path parameter (Type "path") names a file, or a folder when Path
	// is "dir"; its form field opens the path chooser (path_chooser.go),
	// whose browse starts at Base (~ and relative to the working
	// directory) when the field is empty. New allows a path that does not
	// exist yet (a file to write).
	Path string `json:"path,omitempty"`
	Base string `json:"base,omitempty"`
	New  bool   `json:"new,omitempty"`
	// Rest marks a string parameter that takes the rest of the line,
	// verbatim: the palette hands it the text from its first word to the
	// end, quotes and spaces as typed, and the CLI joins the remaining
	// words with spaces. A verb has at most one, the last its words fill
	// (settings.apply's line: :set scrolloff=5 nowrap). Its Source is asked
	// with the text as the query and, when it declares
	// ChoicesCursorParameter, the cursor in it; each choice's id is the
	// text up to the cursor with the completion in place.
	Rest bool `json:"rest,omitempty"`
	// Focus declares that an omitted value is filled from the focus: the
	// instance the request acts for once ExecuteAt has applied its target
	// and the focus path (Ctx.Instance, or the nearest instance of Noun
	// above it). A standard verb's subject parameter (standard_verbs.go)
	// declares it, and so does a create parameter of a FromFocus prop;
	// values never come from the focus unless their parameter says so,
	// and a fill is always shown (the palette slot, the args form). The
	// catalog's focus.
	Focus bool `json:"focus,omitempty"`
	// Edits names the prop of the subject this parameter writes (rename's
	// name writes the label prop): the args form opens holding the prop's
	// current value, selected as a rename is (rename.select), or, when it
	// is empty, empty with the prop's Fallback value as its placeholder.
	Edits string `json:"edits,omitempty"`
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
	// Via is the alias the request was spelled by (an old verb name, a
	// user's actions.json alias): the request is rewritten to the action it
	// names (Noun, Verb) and Via keeps the spelling, so the handler is told
	// which name ran it (Ctx.Invoked) and history shows it. "" for a
	// request that named its action directly.
	Via string `json:"via,omitempty"`
	// Subjects are the instances a request acts on when it acts on
	// several (the rows of a visual selection), in display order; Target
	// is one of them, the one a confirm or a chooser names. Empty means
	// Target alone. A verb that takes many subjects (Operation.Many) runs
	// once with all of them in Ctx.Subjects; any other runs once per
	// subject (Runtime.eachSubject).
	Subjects []string `json:"subjects,omitempty"`
	// Prop, Value and Toggle make the request prop-shaped (props.go):
	// {noun, prop} reads the prop of the thing the request acts for,
	// {noun, prop, value} writes it and {noun, prop, toggle} flips a
	// boolean prop. A prop-shaped request names no verb; it runs as the
	// noun's standard get or set (propVerbRequest), and history keeps it
	// as it was sent. Args then holds at most the subject's key.
	Prop   string `json:"prop,omitempty"`
	Value  any    `json:"value,omitempty"`
	Toggle bool   `json:"toggle,omitempty"`
}

// Key returns the request's operation as noun.verb, or for a prop-shaped
// request its prop as noun.prop (what hints, history and errors print).
func (q Request) Key() string {
	if q.Prop != "" {
		return q.Noun + "." + q.Prop
	}
	return q.Noun + "." + q.Verb
}

// IsProp reports whether the request is prop-shaped: it reads or writes a
// prop instead of naming a verb.
func (q Request) IsProp() bool { return q.Prop != "" || q.Value != nil || q.Toggle }

// Writes reports whether a prop-shaped request writes its prop (a Value
// or a Toggle) rather than reading it.
func (q Request) Writes() bool { return q.IsProp() && (q.Value != nil || q.Toggle) }

// Empty reports whether the request names nothing: no verb and no prop (a
// binding row's null request, an unbind).
func (q Request) Empty() bool { return q.Verb == "" && !q.IsProp() }

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
	Positional []string `json:"positional,omitempty"`
	// Many marks a verb whose handler takes every subject of a request at
	// once (Ctx.Subjects: a visual selection's rows), so it asks one
	// question, makes one change and posts one notice for all of them; set
	// by NounBuilder.Many. A verb without it that is asked to act on
	// several subjects runs once per subject. The catalog's many.
	Many bool `json:"many,omitempty"`
	// Aliases are the action's other names (ActionAlias), display
	// spelling, sorted. Catalog and operations.list fill it; an entry of
	// Runtime.Operations does not hold it (the alias table does).
	Aliases []string `json:"aliases,omitempty"`
	// Standard is the standard verb (standard_verbs.go: get, list) the
	// runtime generated this operation from for a noun with a store, ""
	// for every verb written by hand. An application verb of the same key
	// replaces a standard one, registered before or after. The catalog's
	// standard.
	Standard string `json:"standard,omitempty"`
	// Checks run before the verb from every path (AddVerbCheck,
	// BeforeQuit, verb_checks.go): each allows, asks first or refuses.
	Checks []VerbCheck `json:"-"`
	Run    Handler     `json:"-"`
	App    AppHandler  `json:"-"`
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

// RestParameter is the name of the operation's rest-of-line parameter
// (Parameter.Rest), "" when it has none.
func (o Operation) RestParameter() string {
	for name, p := range o.Parameters {
		if p.Rest {
			return name
		}
	}
	return ""
}

// checkRestParameter refuses more than one rest parameter, one that is not
// a string, and one that words do not fill last (another parameter after
// it in ParameterOrder that only a word could fill: a required one).
func checkRestParameter(o Operation) error {
	rest := ""
	for name, p := range o.Parameters {
		if !p.Rest {
			continue
		}
		if rest != "" {
			return fmt.Errorf("%s: parameters %s and %s both take the rest of the line", o.Key(), min(rest, name), max(rest, name))
		}
		if p.Type != "" && p.Type != "string" {
			return fmt.Errorf("%s: rest parameter %s must be a string, not %s", o.Key(), name, p.Type)
		}
		rest = name
	}
	if rest == "" {
		return nil
	}
	order := o.ParameterOrder()
	after := order[slices.Index(order, rest)+1:]
	for _, name := range after {
		if o.Parameters[name].Required {
			return fmt.Errorf("%s: required parameter %s comes after the rest parameter %s", o.Key(), name, rest)
		}
	}
	return nil
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
	Nouns      map[string]*Noun
	Instances  map[string]*Instance
	Focus      string    // the focused instance; Context() is derived from it
	Mode       ModeState // the one interaction mode; mode.set writes it, a focus change to another stop resets it
	Page       string    // the current page instance; always an ancestor of BaseFocus()
	Modes      map[string]bool
	Operations map[string]Operation
	Quit       bool
	Pending    string
	// BindingTimeout, when positive, is how long every pending key
	// sequence waits on this runtime in place of the sequence-timeout
	// setting (a host or test that fixes it in code); 0, the default,
	// reads the setting.
	BindingTimeout time.Duration
	// ResolveTarget, when set, maps a request target that is not an
	// instance (a foreign id: an extension's row id, a database key) to
	// the instance that stands for it; ExecuteAt consults it (TargetInstance)
	// before refusing an unknown target, so keys, the palette, the CLI and
	// the API all accept the foreign id. An error refuses the request with
	// it; "" and no error leaves the target unknown.
	ResolveTarget    func(target string) (string, error)
	pendingModes     map[string]bool
	passthroughModes map[string]bool   // base modes in which capture rows stand aside (DefinePassthrough)
	selectionModes   map[string]string // base modes that hold a selection, and its shape (DefineSelection)
	modeExtends      map[string]string // the mode a mode falls back to for a noun with no table of its own (ModeExtends)
	guards           map[string]Guard
	predicates       map[string]PredicateFunc // named query predicates (query.go)
	history          actionHistory            // the executed-request ring and the current source (history.go)
	origin           string                   // the instance the running request or completion acts for; a modal opens only from the top level
	askedAt          string                   // the focus the running request was made at, before it moved to its noun's instance (createFill reads a reference's thing there)
	rowRequest       bool                     // the next execute is a binding row's own request (runRow's level rule targets it)
	subjects         []string                 // the subjects execute hands the application handler it is about to run (Ctx.Subjects)
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
	watches          *watchHub         // the watches of nouns' things (standard_watch.go), made on first use
	jobSeq           uint64            // last issued job id
	defaults         defaultSources    // where each default row and extends came from (bindings defaults)
	deprecations     []string          // rows that named an operation by its old verb.noun spelling, rewritten once
	aliases          map[string]string // registry: a short noun name -> the noun it names (settings -> ochazuke.settings; namespace.go)
	builtinDepth     int               // >0 while Builtin runs: framework registration, whose nouns are ochazuke.<noun>
	actionNames      actionNames       // the action alias table and the running request's name (action_names.go)
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
		selectionModes:   map[string]string{},
		modeExtends:      map[string]string{},
		defaults:         defaultSources{rows: map[string]string{}, extends: map[string]string{}},
		guards:           builtinGuards(),
		predicates:       builtinPredicates(),
		modalInstances:   map[string]bool{},
		aliases:          map[string]string{},
	}
}

// registerFramework registers the framework nouns and operations.
func (r *Runtime) registerFramework() {
	r.Instance("app")
	r.Define("ochazuke.modal")
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
	registerTimelines(r)
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
	registerFocusDisplay(r)
	registerDetail(r)
	registerViewScroll(r)
	registerPanes(r)
	registerQuery(r)
	registerHistory(r)
	registerPlacements(r)
	registerSegments(r)
	registerDocs(r)
	// The default rows, once every verb and guard they name is registered:
	// the framework's files in this order (rows two files put in one table,
	// the app noun's normal table, the list's, keep it). A modal answers esc
	// in normal mode (bindings/modal.json); inputs, pending modes and the
	// palette settle first because their rows sit inside this level.
	for _, name := range frameworkBindingFiles {
		r.loadFrameworkBindings(name)
	}
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
	r.builtinDepth++
	defer func() { r.builtinDepth-- }()
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
	"ochazuke.list.move": true, "ochazuke.focus.next": true, "ochazuke.focus.previous": true,
	"ochazuke.surface.scroll": true, "ochazuke.page.next": true, "ochazuke.page.previous": true,
	"ochazuke.input.move": true, "ochazuke.picker.move": true, "ochazuke.palette.move": true,
	"ochazuke.yank-history.move": true, "ochazuke.grid.move": true, "ochazuke.grid.goto": true,
	"ochazuke.detail.scroll": true, "ochazuke.list.goto": true, "ochazuke.list.scroll": true, "ochazuke.list.scroll-columns": true,
	"ochazuke.list.column": true, "ochazuke.list.choose-column": true,
}

// frameworkHistoryKinds are the framework's operations whose history kind
// is not the derived one (Operation.HistoryKindOf): the reads and reloads
// are fetches, the interaction verbs that change a setting, a register, a
// record or a pane are changes, and the application verbs that only close
// or fold are actions.
var frameworkHistoryKinds = map[string]string{
	"ochazuke.actions.aliases": HistoryFetch, "ochazuke.actions.check": HistoryFetch, "ochazuke.actions.list": HistoryFetch, "ochazuke.actions.path": HistoryFetch, "ochazuke.actions.reload": HistoryFetch,
	"ochazuke.appearance.get": HistoryFetch, "ochazuke.bindings.check": HistoryFetch, "ochazuke.bindings.list": HistoryFetch,
	"ochazuke.bindings.path": HistoryFetch, "ochazuke.bindings.reload": HistoryFetch, "ochazuke.bindings.stops": HistoryFetch,
	"ochazuke.config.path": HistoryFetch, "ochazuke.config.reload": HistoryFetch, "ochazuke.history.list": HistoryFetch, "ochazuke.history.path": HistoryFetch,
	"ochazuke.instances.query": HistoryFetch, "ochazuke.jobs.list": HistoryFetch, "ochazuke.modes.list": HistoryFetch,
	"ochazuke.notice.list": HistoryFetch, "ochazuke.operations.list": HistoryFetch, "ochazuke.pages.list": HistoryFetch,
	"ochazuke.register.get": HistoryFetch, "ochazuke.settings.get": HistoryFetch, "ochazuke.settings.list": HistoryFetch,
	"ochazuke.settings.names": HistoryFetch, "ochazuke.settings.path": HistoryFetch, "ochazuke.settings.reload": HistoryFetch,
	"ochazuke.state.get": HistoryFetch, "ochazuke.themes.list": HistoryFetch, "ochazuke.themes.path": HistoryFetch,
	"ochazuke.themes.reload": HistoryFetch,

	"ochazuke.list.yank": HistoryChange, "ochazuke.list.resize-column": HistoryChange, "ochazuke.list.reset-columns": HistoryChange,

	"ochazuke.appearance.set": HistoryChange, "ochazuke.bindings.load": HistoryChange, "ochazuke.confirm.set": HistoryChange,
	"ochazuke.config.export": HistoryChange, "ochazuke.form-option.pick": HistoryChange, "ochazuke.form.redo": HistoryChange,
	"ochazuke.form.suggest": HistoryChange, "ochazuke.form.undo": HistoryChange, "ochazuke.input.redo": HistoryChange,
	"ochazuke.input.undo": HistoryChange, "ochazuke.input.yank": HistoryChange, "ochazuke.mode.register": HistoryChange,
	"ochazuke.register.set": HistoryChange, "ochazuke.setting-value.commit": HistoryChange, "ochazuke.setting.cycle": HistoryChange,
	"ochazuke.setting.edit": HistoryChange, "ochazuke.setting.reset": HistoryChange, "ochazuke.settings.reset": HistoryChange,
	"ochazuke.settings.set": HistoryChange, "ochazuke.terminal.paste": HistoryChange, "ochazuke.terminal.send": HistoryChange,
	"ochazuke.yank-history.select": HistoryChange,

	"ochazuke.chooser.cancel": HistoryAction, "ochazuke.outline.fold": HistoryAction,
}

// Register adds an operation. An operation with App set is of kind
// application and its Run is the framework's dispatcher for it. Its noun
// may be dotted (ai.skills) and so may its verb (base.add): the key is a
// name in a flat table, looked up whole. A noun named by a
// framework alias (settings) is the noun it names (ochazuke.settings), as
// Define reads it; inside Builtin a plain noun is ochazuke.<noun>. A verb y on
// noun x beside a noun x.y is refused (checkOverlap).
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
	if r.builtinDepth > 0 {
		o.Noun = frameworkName(o.Noun)
	} else {
		o.Noun = r.CanonicalNoun(o.Noun) // an alias extends the framework's noun (Define)
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
	if existing, exists := r.Operations[o.Key()]; exists {
		if existing.Standard == "" || o.Standard != "" {
			panic("duplicate operation: " + o.Key())
		}
		// The application wins: its verb named like a standard verb
		// replaces the generated one. Aliases name the key, so they stay.
		delete(r.Operations, o.Key())
	}
	if n := r.Nouns[o.Noun]; n != nil && n.prop(o.Verb) != nil {
		panic(fmt.Sprintf("verb %s of %s clashes with its prop %s: a word after a noun is a verb or a prop, never both", o.Verb, o.Noun, o.Verb))
	}
	if err := r.checkOverlap(o.Key()); err != nil {
		panic(err.Error())
	}
	r.claimName(o.Key())
	r.ensureNoun(o.Noun)
	if o.Parameters == nil {
		o.Parameters = map[string]Parameter{}
	}
	if err := checkRestParameter(o); err != nil {
		panic(err.Error())
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
	spec := instanceSpec{parent: "app", noun: "ochazuke.editor"}
	for _, opt := range opts {
		opt(&spec)
	}
	if r.Instances[spec.parent] == nil {
		return fmt.Errorf("unknown parent %q", spec.parent)
	}
	spec.noun = r.CanonicalNoun(spec.noun)
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
	q = r.canonicalRequest(q)
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
	r.watchAfterRequest(q, depth)
	return value, err
}

func (r *Runtime) execute(c Context, q Request) (any, error) {
	fromRow := r.rowRequest
	r.rowRequest = false
	if q.IsProp() {
		// A prop-shaped request runs as its noun's standard get or set.
		pq, err := r.propVerbRequest(q)
		if err != nil {
			return nil, err
		}
		q = pq
	}
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
	if !r.Available(q, focus, r.Mode) {
		return nil, fmt.Errorf("%s is not applicable now", r.DisplayKey(q.Key()))
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
	// A row's own verb on a row in a visual selection (V) acts on every
	// selected row, from a key, the palette or the API (subjects.go).
	if vq, ok := r.visualApply(q, op, focus); ok {
		return r.ExecuteAt(c, vq)
	}
	subjects, err := r.subjectsOf(q, focus)
	if err != nil {
		return nil, err
	}
	defer r.actingFor(focus)()
	defer r.asking(actions)()
	switch v := r.runChecks(c, q, op); {
	case v.Refuse != "":
		return nil, errors.New(v.Refuse)
	case v.Ask != "":
		if len(subjects) > 1 {
			q.Subjects = subjects
		}
		return r.openConfirm(c, q, Operation{Noun: op.Noun, Verb: op.Verb, Confirm: v.Ask})
	}
	if op.Confirm != "" && !q.Confirmed {
		if len(subjects) > 1 {
			q.Subjects = subjects
		}
		return r.openConfirm(c, q, op)
	}
	if len(subjects) > 1 && !op.Many {
		return r.eachSubject(q, subjects)
	}
	defer r.invoking(r.invocationOf(q))()
	if fn := r.ActionFor(actions, q.Key()); fn != nil {
		return fn(r, c, q.Args)
	}
	r.subjects = subjects
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

// validatePartialArguments checks the arguments args gives (known names,
// types, choices) and not the required ones it leaves out: a preset that
// a later request completes (a bindings.json row's args, an alias's preset
// args). The completed request is checked whole when it runs, so a missing
// required argument opens the args form or fails naming it.
func validatePartialArguments(op Operation, args Arguments) error {
	optional := make(map[string]Parameter, len(op.Parameters))
	for name, param := range op.Parameters {
		param.Required = false
		optional[name] = param
	}
	return validateArguments(Operation{Parameters: optional}, args)
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
		case "string", PathParameterType:
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
		case AnyParameterType:
			valid = true
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
		op.Aliases = r.aliasNames(op.Key())
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
	r.Register(Operation{Verb: "get", Noun: "ochazuke.state", Description: "Inspect public state using a JSON Pointer", Parameters: map[string]Parameter{"path": {Type: "string"}}, Result: "JSON value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return JSONPointer(r.Snapshot(), StringArg(a, "path", ""))
	}})
	r.Register(Operation{Verb: "list", Noun: "ochazuke.operations", Description: "List registered verbs, nouns, kinds (interaction or application), confirm prompts and argument schemas", Result: "operation[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) { return r.Catalog(), nil }})
	r.Register(Operation{Verb: "list", Noun: "ochazuke.bindings", Description: "List the rows the current stack and mode answer to; all includes every stored noun and instance mode table", Parameters: map[string]Parameter{"all": {Type: "boolean", Description: "Return every stored binding row, including inactive modes and contexts, in configuration JSON format"}}, Result: "binding[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if all, _ := a["all"].(bool); all {
			return r.allBindings(), nil
		}
		return r.ListBindings(focusOf(c)), nil
	}})
	r.Register(Operation{Verb: "defaults", Noun: "ochazuke.bindings", Description: "Print the default bindings (the framework's and the application's, without the user's bindings.json) as a bindings file, each group tagged with its source: the embedded file, go or generated", Result: "bindings file", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		data, err := FormatBindings(r.defaultBindingsDoc())
		return Verbatim(data), err
	}})
	r.Register(Operation{Verb: "stops", Noun: "ochazuke.bindings", Description: "List the rows every focus stop answers to, app-wide: each page's stops in drawing order, the open modals', then app; each with the focus and mode it is read at", Result: "stop bindings[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.StopBindings(), nil
	}})
	r.Register(Operation{Verb: "focus", Noun: "ochazuke.input", Description: "Focus a registered input", Parameters: map[string]Parameter{"id": {Type: "string", Required: true, Noun: "ochazuke.editor", Free: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		id := StringArg(a, "id", "")
		if r.Input(id) == nil {
			return nil, fmt.Errorf("unknown input %q", id)
		}
		err := r.SetFocus(id)
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "set", Noun: "ochazuke.mode", Description: "Set the global interaction mode (a registered mode) and clear any pending payload", Parameters: map[string]Parameter{"mode": {Type: "string", Required: true, Noun: "ochazuke.modes"}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
	r.Register(Operation{Verb: "push", Noun: "ochazuke.modal", Description: "Open a modal focused on a registered input", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}, "input": {Type: "string", Required: true, Noun: "ochazuke.editor", Free: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		id := StringArg(a, "input", "")
		if r.Input(id) == nil {
			return nil, errors.New("unknown modal input")
		}
		err := r.Push(StringArg(a, "id", ""), id)
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "pop", Noun: "ochazuke.modal", Description: "Close the active modal and restore its parent", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) { err := r.Pop(); return r.Context(), err }})
	r.Register(Operation{Verb: "quit", Noun: "app", Description: "Exit the application", Parameters: map[string]Parameter{"force": {Type: "boolean", Description: "quit without running the checks an application hangs on app.quit (BeforeQuit: unsaved edits are lost)"}}, Result: "boolean", Run: func(r *Runtime, c Context, a Arguments) (any, error) { r.Quit = true; return true, nil }})
	r.Register(Operation{Verb: "ignore", Noun: "ochazuke.key", Description: "The noop: always available, does nothing; a row bound to it consumes its key (DispatchOutcome reports KeyConsumed), how a container clamps a key its children leave unhandled", Result: "null", Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
}
