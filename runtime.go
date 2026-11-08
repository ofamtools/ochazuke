package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	// SourceArgs maps a source parameter to a sibling parameter or record
	// field. Only set sibling values are forwarded; query is still supplied
	// automatically unless explicitly mapped here. Register the source first.
	SourceArgs map[string]string `json:"sourceArgs,omitempty"`
	// SourceFixed gives a source parameter a constant value on every call
	// (the standard restore asks its noun's list for archived rows only).
	// A sibling mapped by SourceArgs wins over it.
	SourceFixed map[string]any `json:"sourceFixed,omitempty"`
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
	// Start names the path chooser source it opens on: a source of the
	// chooser (browse, recent, typed) or one the application registered
	// (AddWalkPathSource's walk, AddPathSource's names). An unknown name,
	// or a source the pick leaves out, opens as if Start were empty.
	Start string `json:"start,omitempty"`
	// Rest marks a string parameter that takes the rest of the line,
	// verbatim: the palette hands it the text from its first word to the
	// end, quotes and spaces as typed, and the CLI joins the remaining
	// words with spaces. A verb has at most one, the last its words fill
	// (setting.apply's line: :set scrolloff=5 nowrap). Its Source is asked
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
	// From declares where an omitted value comes from when no key says:
	// "row" (the Data of the row the request acts for) or "row:<field>"
	// (one field of it, dots walking nested ones), the sources of a
	// binding row's From (Binding.FromRow) that need no key. Every route
	// reads it the same way (Runtime.parameterSources, in execute): an
	// argument given wins, then the binding row's From, then this, then
	// Focus, then the default, and the args form asks for what is still
	// missing. The row is the innermost instance with Data from the
	// request's target (else the focus, for a request a person makes: a
	// key, the palette, the help; a program names its target) outward. A
	// row without Data or the field fills nothing. A fill is always shown
	// (the palette slot, the args form). The catalog's from.
	From string `json:"from,omitempty"`
	// Plumbing marks a parameter that is the verb's machinery, not
	// something a person gives: a list's id, the row an action acts on, the
	// flag that says the confirm was answered. The args form leaves it out
	// (a confirmed field would skip the verb's confirm), the help does not
	// offer an args form for a verb that has only these, and a request or
	// the runtime sets them. The catalog's plumbing.
	Plumbing bool `json:"plumbing,omitempty"`
	// Forwards marks a parameter whose argument names a verb of the noun
	// of the rows the operation acts on (list.apply's verb): the operation
	// runs that verb for them, so a row requesting the operation with the
	// argument counts as binding that verb (the help's unbound list). The
	// catalog's forwards.
	Forwards bool `json:"forwards,omitempty"`
	// Edits names the prop of the subject this parameter writes (rename's
	// name writes the label prop): the args form opens holding the prop's
	// current value, selected as a rename is (rename.select), or, when it
	// is empty, empty with the prop's Fallback value as its placeholder.
	Edits string `json:"edits,omitempty"`
	// ValueOf names the parameter whose argument is a prop of the
	// operation's noun: this parameter holds a value of that prop, so its
	// choices (and the palette's check of a typed word) are the prop's own
	// (its Enum, Source or Noun; true and false for a boolean). A standard
	// set's value declares ValueOf "prop". The catalog's valueOf.
	ValueOf string `json:"valueOf,omitempty"`
	// Pairs marks a parameter that takes NAME=VALUE words of the noun's
	// props (where, set's several-prop form): a word holding = completes
	// the values of prop NAME; one without completes the Enum when there
	// is one, else NAME= for each prop. The catalog's pairs.
	Pairs bool `json:"pairs,omitempty"`
	// Excludes names the parameters a request cannot give with this one
	// (set's toggle and value). A preset (an action alias's args) that a
	// caller's argument excludes, or that excludes it, yields to the
	// caller's (Operation.WithPresets). The catalog's excludes.
	Excludes []string `json:"excludes,omitempty"`
	// Covers marks a parameter that, given, stands in for every required
	// parameter of its verb: create's from, whose thing holds their
	// values. A request giving it lacks none (Operation.lacks). The
	// catalog's covers.
	Covers bool `json:"covers,omitempty"`
	// Named marks a parameter no bare word fills: it is given only as
	// NAME=VALUE or --NAME (edit's props, so a word after the field is
	// refused, never a prop's value). ParameterOrder still lists it (the
	// args form, help); the palette and the CLI pass over it when they
	// fill words in order. The catalog's named.
	Named bool `json:"named,omitempty"`
}

// lacks reports whether args lack the required parameter name: it is
// required, not given, and no given parameter Covers it.
func (o Operation) lacks(name string, args Arguments) bool {
	if _, given := args[name]; given || !o.Parameters[name].Required {
		return false
	}
	for other, v := range args {
		if o.Parameters[other].Covers && !emptyProp(v) {
			return false
		}
	}
	return true
}

// WithPresets is args with presets (an action alias's arguments) filling
// what args leaves out: a caller's argument wins over a preset of the
// same name, and over a preset it excludes or that excludes it
// (Parameter.Excludes: star, task.set {toggle: true}, given value: false
// writes false). args is not changed.
func (op Operation) WithPresets(args, presets Arguments) Arguments {
	if len(presets) == 0 {
		return args
	}
	out := maps.Clone(args)
	if out == nil {
		out = Arguments{}
	}
	excluded := func(preset string) bool {
		for name := range args {
			if slices.Contains(op.Parameters[name].Excludes, preset) || slices.Contains(op.Parameters[preset].Excludes, name) {
				return true
			}
		}
		return false
	}
	for k, v := range presets {
		if _, set := out[k]; !set && !excluded(k) {
			out[k] = v
		}
	}
	return out
}

// Request names an operation and, optionally, the instance it addresses.
// Confirmed skips the operation's Confirm prompt: the confirm modal's y sets
// it on the stored request, and a script may set it to run unattended.
type Request struct {
	agentApproved bool
	// Actor is an explicit API identity; nil inherits the enclosing request or owner.
	Actor     *Actor    `json:"actor,omitempty"`
	source    string    // a deferred request preserves its original source
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
	// guardMode is the hypothetical outside mode, without changing live
	// mode or notifying components. Only outside row dispatch supplies it.
	guardMode string
	// Request is the running request's own context (request_context.go):
	// execute fills it for the handler it runs; nil outside a request.
	Request *RequestContext `json:"-"`
}

// Clone returns a copy whose Path does not share the original's backing array.
func (c Context) Clone() Context {
	c.Path = append([]string(nil), c.Path...)
	return c
}

// Handler runs an interaction operation on the UI loop and returns its result.
type Handler func(*Runtime, Context, Arguments) (any, error)

// Operation is one registered noun.verb. Kind is "interaction" (the
// default: Run executes synchronously on the loop) or "application" (App is
// an AppHandler whose pending work runs off the loop and reports through the
// delivery contract). Confirm, when set, is a prompt template ({{id}},
// {{noun}}, {{label}}, {{<arg>}}) shown in a confirm modal before the operation runs.
// Repeatable marks a change (not a motion or navigation) that dot-repeat
// (history-entry.repeat, bound to . on app in normal mode) may run again; set it
// on the Operation passed to Register.
type Operation struct {
	Destructive bool      `json:"destructive,omitempty"`
	Agent       string    `json:"agent,omitempty"`
	Examples    []Example `json:"examples,omitempty"`
	// CatalogGuards preserves a proxy's remote guards; the remote runtime checks them.
	// CatalogProxy marks a verb forwarded to an extension runtime.
	CatalogProxy  bool     `json:"-"`
	CatalogGuards []string `json:"-"`
	// Key is the operation's name, namespace.noun.name (Key); JSON
	// writes it with its parts as noun, verb, name and namespace
	// (MarshalJSON).
	Key Key `json:"-"`
	// App is the program that runs the handler; Handler addresses it
	// there (HandlerID). Operations.Push fills both.
	App         App                  `json:"-"`
	Handler     HandlerID            `json:"-"`
	Description string               `json:"description"`
	Parameters  map[string]Parameter `json:"parameters"`
	Result      string               `json:"result"`
	Kind        string               `json:"kind"`
	Confirm     string               `json:"confirm,omitempty"`
	Repeatable  bool                 `json:"repeatable,omitempty"`
	// Foreground tells catalog hosts to hand the terminal to this verb.
	Foreground bool `json:"foreground,omitempty"`
	// OpensPage is the id of a page the verb opens in the tool's own
	// runtime. A catalog host that mounts that page opens it itself (under
	// its own page id) instead of running the verb in the tool; the tool
	// run on its own still runs the verb. Data for hosts.
	OpensPage string `json:"opensPage,omitempty"`
	// Slot admits only one pending invocation per subject row and slot.
	// A repeat while it runs reports that fact instead of running user work twice.
	Slot string `json:"slot,omitempty"`
	// Timeout is a catalog host's captured execution limit; zero uses its default.
	Timeout time.Duration `json:"timeout,omitempty"`
	// EnabledSetting gates a verb on a producing-runtime boolean setting.
	// Catalog hosts receive its Disabled snapshot, never the setting name.
	EnabledSetting string `json:"enabledSetting,omitempty"`
	Disabled       bool   `json:"disabled,omitempty"`
	// Unavailable, when not "", is why the verb cannot run on its noun:
	// a core verb whose store lacks what it needs (core_verbs.go: "its
	// store creates no things (no Store.Create)"). The verb is listed (a
	// thing noun has every core verb) but never available, and running it
	// fails with the reason. The catalog's unavailable.
	Unavailable string `json:"unavailable,omitempty"`
	// Summary names the field of the verb's answer (an object) that holds
	// the one line a person reads for it ("undid task.set on Groceries").
	// Shown to a person (the palette, the help's run), the answer is that
	// line in the status line, never its JSON; programs (the CLI, MCP, an
	// app's Execute) still get the whole answer. "" when the answer has no
	// such line.
	Summary string `json:"summary,omitempty"`
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
	// show and the inspector does not retain as the last user request;
	// set by NounBuilder.Quiet. The catalog's quiet.
	Quiet bool `json:"quiet,omitempty"`
	// Builtin marks an operation the framework registered itself: every
	// one NewRuntime and NewModel register, and those a framework
	// constructor registers later (NewPalette, grid.Register,
	// markdown.Register, cli.AddCommands), each inside Runtime.Builtin. An
	// application's verb is never builtin, even on a framework noun
	// (history.capture-hook). Data for hosts (the catalog's builtin): a
	// host mounting another tool's verbs skips these, which every
	// ochazuke tool has; the runtime does not read it.
	Builtin bool `json:"builtin,omitempty"`
	// PersistArgs lets the history file keep this operation's arguments
	// named like text (text, value, keys, args, password, secret, token), which
	// it otherwise leaves out; set by NounBuilder.PersistArgs. Only for an
	// operation whose arguments are never typed secrets (setting.set).
	PersistArgs bool `json:"persistArgs,omitempty"`
	// Motion marks movement (list.move, focus.next, surface.scroll,
	// page.next): the footer's hint line leaves its rows out, since motions
	// are standard and the footer is for the verbs the focus offers; ?
	// help and bindings list keep them. Set by NounBuilder.Motion or at
	// registration.
	Motion bool `json:"motion,omitempty"`
	// Requires names the guard that must hold at the focus for the verb to
	// have what it acts on (the input verbs: "input", an input focused).
	// It is not availability: the verb is still offered and a direct call
	// runs, its handler saying why it cannot. Dot-repeat skips a recorded
	// change whose Requires is false at the focus, so typing into a line
	// that has since closed is never replayed outside an input.
	Requires string `json:"-"`
	// FocusBound marks operations whose editing/selection mode belongs to
	// their own focused component, not a hypothetical outside context.
	FocusBound bool `json:"focusBound,omitempty"`
	// Typing marks an operation that passes the key that ran it on as
	// content rather than acting on it as a command: typing into an input
	// (input.insert), keys forwarded to a pane (terminal.send,
	// terminal.paste). The key echo (keys.echo) collapses a run of them
	// into the text typed, and hides the ones a passthrough mode forwards
	// unless keys.echo-terminal is on. Set by NounBuilder.Typing or at
	// registration.
	Typing bool `json:"typing,omitempty"`
	// Staying marks an operation that manages the pending mode itself, or
	// re-dispatches the key inside it (recording): a row running it leaves
	// the one-shot pending mode in place (NounBuilder.Staying).
	Staying bool `json:"staying,omitempty"`
	// HistoryKind is the kind of entry the operation records
	// (HistoryAction, HistoryChange, HistoryFetch); empty derives it
	// (HistoryKindOf). Set it to HistoryFetch on a verb that only asks for
	// data (NounBuilder.Fetch, the catalog's history), so history-entry.list's
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
	// ByAssociation admits a person's targetless request through a declared
	// association on the focus path. Programs still need an explicit target.
	ByAssociation bool `json:"byAssociation,omitempty"`
	// Aliases are the action's other names (ActionAlias), display
	// spelling, sorted. Catalog and operation.list fill it; an entry of
	// Runtime.Operations does not hold it (the alias table does).
	Aliases []string `json:"aliases,omitempty"`
	// Standard is the standard verb (standard_verbs.go: get, list) the
	// runtime generated this operation from for a noun with a store, ""
	// for every verb written by hand. An application verb of the same key
	// replaces a standard one, registered before or after. The catalog's
	// standard.
	Standard string `json:"standard,omitempty"`
	// ShadowsStandard says a hand-written verb named like a standard verb
	// its noun's store would generate replaces it on purpose (a facts
	// noun whose own list answers in place of the standard one):
	// StandardShadows does not report it. A shadow not declared this way
	// keeps its info line.
	ShadowsStandard bool `json:"-"`
	// Checks run before the verb from every path (AddVerbCheck,
	// BeforeQuit, verb_checks.go): each allows, asks first or refuses.
	Checks []VerbCheck `json:"-"`
	// ConfirmFor, when set, is the prompt template one request asks in
	// place of Confirm (given as prompt), read when the request would ask:
	// a verb whose confirm depends on what it will do to the thing (the
	// standard delete under delete.archive). "" asks nothing.
	ConfirmFor func(r *Runtime, c Context, q Request, prompt string) string `json:"-"`
	Run        Handler                                                      `json:"-"`
	// AppHandler is an application verb's handler (Kind application):
	// its pending work runs off the loop.
	AppHandler AppHandler `json:"-"`
}

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
			return fmt.Errorf("%s: parameters %s and %s both take the rest of the line", o.Key.Short(), min(rest, name), max(rest, name))
		}
		if p.Type != "" && p.Type != "string" {
			return fmt.Errorf("%s: rest parameter %s must be a string, not %s", o.Key.Short(), name, p.Type)
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
			return fmt.Errorf("%s: required parameter %s comes after the rest parameter %s", o.Key.Short(), name, rest)
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
	Nouns     map[string]*Noun
	Instances map[string]*Instance
	Focus     string    // the focused instance; Context() is derived from it
	Mode      ModeState // the one interaction mode; mode.switch writes it, a focus change to another stop resets it
	Page      string    // the current page instance; always an ancestor of BaseFocus()
	// Modes is the defined base modes, a read-only view of the mode
	// table kept for callers that index it: write modes with DefineMode,
	// read them with IsBaseMode, IsPendingMode and ModeTraits.
	Modes      map[string]bool
	Operations Operations
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
	ResolveTarget func(target string) (string, error)
	// ModalReturns is each open modal's saved return, by modal id: the
	// focus and mode closing it restores, one value. Readable for
	// inspection; only the modal stack's owner (modal_stack.go) writes it.
	// An open modal's Payload.Return is a snapshot of its Focus.
	ModalReturns   map[string]ModalReturnState
	modes          modeTable // every defined mode and its traits (mode_table.go); Modes mirrors its base modes
	guards         map[string]Guard
	predicates     map[string]PredicateFunc // named query predicates (query.go)
	agentApproved  bool                     // the owner approved this request and its nested work
	agentTransport *agentTransport          // scoped adapter identity and execution environment, shared by nested requests
	history        actionHistory            // the executed-request ring and the current source (history.go)
	reopening      string                   // the record editor reopening itself over the level it opened over (runFailed)
	prefix         pendingPrefix
	bindingVersion uint64
	renderCache    runtimeRenderCache
	modeGeneration uint64
	rows           map[string]string         // list row instance id -> owning list id
	holders        []string                  // every instance that is not a list row, in registration order: the ones that may hold a Component (components())
	modals         []string                  // the open modal instances, outermost first; their saved returns are ModalReturns (modal_stack.go)
	modalInstances map[string]bool           // instances created by Push or OpenModal, removed by Pop (modal_stack.go)
	remoteSeen     map[string]map[string]any // the last answer of each read of a remote store (Ctx.Await)
	remoteQueue    []*remoteRun              // remote writes in flight, in order, per thing
	copyMarks      map[string]*copyMarks     // by noun, what each copy noun's last refresh read (copy_refresh.go)
	diffRenderer   DiffRenderer              // draws a plain diff (SetDiffRenderer); nil draws - and + lines
	targets        map[string]string         // target texts resolved to keys during the running request (resolveTarget)
	seq            uint64                    // instance registration order
	commands       []tea.Cmd                 // off-loop work scheduled since the adapter last drained it
	jobs           map[uint64]*job           // the jobs list: running jobs and the finished ones retained for job.list
	watches        *watchHub                 // the watches of nouns' things (standard_watch.go), made on first use
	events         *eventHub                 // declared events, open surfaces' change counts, hook rows (events.go, hooks.go), made on first use
	jobSeq         uint64                    // last issued job id
	defaults       defaultSources            // where each default row and extends came from (bindings defaults)
	deprecations   []string                  // rows that named an operation by its old verb.noun spelling, rewritten once
	aliases        map[string]string         // registry: a short noun name -> the noun it names (settings -> ochazuke.settings; namespace.go)
	builtinDepth   int                       // >0 while Builtin runs: framework registration, whose nouns are ochazuke.<noun>
	actionNames    actionNames               // the action alias table and the running request's name (action_names.go)

	// namedSettings indexes the settings rows name (binding_settings.go).
	namedSettings map[string]bool
}

// NewRuntime returns a runtime with the framework nouns registered and focus on the app instance.
func NewRuntime() *Runtime {
	r := newRuntime()
	r.Builtin(r.registerFramework)
	return r
}

// newRuntime is a runtime with no operations, focus on app.
func newRuntime() *Runtime {
	r := &Runtime{
		Nouns: map[string]*Noun{}, Instances: map[string]*Instance{}, rows: map[string]string{},
		Modes:      map[string]bool{},
		modes:      modeTable{},
		defaults:   defaultSources{rows: map[string]string{}, extends: map[string]string{}},
		guards:     builtinGuards(),
		predicates: builtinPredicates(),
		aliases:    map[string]string{},
	}
	r.Operations = NewOperations(r.Nouns)
	r.initModalStack()
	r.ensureBuiltinModes()
	r.changeFocus("app", focusMove{cause: focusAssign})
	r.changeMode(ModeState{Name: "normal"}, modeInit)
	return r
}

// registerModalAliases keeps modal.pop, the close verb before v0.83.0, as
// an old name of modal.close, which now answers esc and q on every modal
// (Keep actions: the old name stays one release). It runs once settings
// exist: an alias's checks read the noun display setting.
func registerModalAliases(r *Runtime) {
	if err := r.AddActionAlias(ActionAlias{Alias: "ochazuke.modal.pop", Action: "ochazuke.modal.close", Since: "v0.83.0", RemoveAfter: "v0.84.0"}); err != nil {
		panic(err)
	}
}

// registerFramework registers the framework nouns and operations.
func (r *Runtime) registerFramework() {
	r.Instance("app")
	r.Define("ochazuke.modal")
	// "no-modal" holds while no modal is open: the app's ctrl+c capture row
	// quits past any inner row, but a modal (the palette) still answers it.
	r.DefineGuard("no-modal", func(r *Runtime, ev KeyEvent) bool { return r.EventTopModal(ev) == "" })
	registerCore(r)
	registerJobs(r)
	registerConfiguration(r)
	registerSearchMatch(r)
	registerChooser(r)
	registerConfig(r)
	registerActions(r)
	registerSettings(r)
	registerFactNouns(r)
	registerAgentRights(r)
	registerSettingsEditor(r)
	registerAbout(r)
	registerConfigExport(r)
	registerConfigReload(r)
	registerHooks(r)
	registerPages(r)
	registerPickerRegistry(r)
	registerModalAliases(r)
	registerPickerAliases(r)
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
	registerKeyEcho(r)
	registerDetail(r)
	registerViewScroll(r)
	registerPanes(r)
	registerModePin(r)
	registerQuery(r)
	registerHistory(r)
	registerInspector(r)
	registerPlacements(r)
	registerSegments(r)
	registerDocs(r)
	registerTextView(r)
	registerSearchFind(r)
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
// registers operations after them (NewPalette, NewAppearance,
// grid.Register, markdown.Register, cli.AddCommands), so an operation
// registered lazily is marked too. Operations registered before it, or by
// the application at any other time, keep Builtin false. Calls nest. It is
// for framework packages; an application's own verbs are not builtin.
// Builtin also records each new operation's effective history kind after
// the registration batch, including its builder's Rows/State annotations.
func (r *Runtime) Builtin(register func()) {
	before := make(map[string]bool, r.Operations.Len())
	for key := range r.Operations.Entries() {
		before[key] = true
	}
	r.builtinDepth++
	defer func() { r.builtinDepth-- }()
	register()
	for key, op := range r.Operations.Entries() {
		if !before[key] {
			op.Builtin = true
			if op.HistoryKind == "" {
				op.HistoryKind = op.HistoryKindOf()
			}
			r.Operations.Set(op)
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
func (r *Runtime) BaseMode() string { return r.baseModeOf(r.Mode) }

// baseModeOf is BaseMode for any mode state: a query's own mode as well
// as the live one.
func (r *Runtime) baseModeOf(m ModeState) string {
	if (r.modes[m.Name].Pending || m.Payload.Prefix != "") && m.Payload.Return != "" {
		return m.Payload.Return
	}
	return m.Name
}

// ModeObserver is implemented by a component that follows the base mode
// while the focus sits on it or beneath it: the grid starts and ends its
// selection when a visual shape is entered or left.
type ModeObserver interface {
	ModeChanged(from, to string)
}

// SetMode sets the global mode by name and clears its payload: the same
// request as mode.switch (switchMode), at the focus's input. ModeDeclared
// names the focus's declared mode (DeclaredMode); a focused input runs its
// own transition (insert sessions, visual anchor), a pinned input stays in
// its pinned mode, and the mode generation advances even for the same name.
func (r *Runtime) SetMode(mode string) error {
	_, err := r.switchMode(mode, r.Input(r.Focus))
	return err
}

// Register adds an operation. An operation with App set is of kind
// application and its Run is the framework's dispatcher for it. Its noun
// may be dotted (ai.skills) and so may its verb (base.add): the key is a
// name in a flat table, looked up whole. A noun named by a
// framework alias (settings) is the noun it names (ochazuke.settings), as
// Define reads it; inside Builtin a plain noun is ochazuke.<noun>. A verb y on
// noun x beside a noun x.y is refused (checkOverlap).
func (r *Runtime) Register(o Operation) {
	if o.AppHandler != nil {
		if o.Run != nil {
			panic("operation takes Run or App, not both")
		}
		o.Kind = KindApplication
		o.Run = func(r *Runtime, c Context, a Arguments) (any, error) {
			return r.runApplication(r.Operations.At(o.Key), c, a)
		}
	}
	if o.Name() == "" || o.Noun() == "" || o.Run == nil {
		panic("invalid operation registration")
	}
	if r.builtinDepth > 0 {
		o.Key = KeyOf(frameworkName(o.Noun()), o.Name())
	} else {
		o.Key = KeyOf(r.CanonicalNoun(o.Noun()), o.Name()) // an alias extends the framework's noun (Define)
	}
	switch o.HistoryKind {
	case "", HistoryAction, HistoryChange, HistoryFetch:
	default:
		panic("unknown history kind " + o.HistoryKind)
	}
	if existing, exists := r.Operations.Find(o.Key); exists {
		if existing.Standard == "" || o.Standard != "" {
			panic("duplicate operation: " + o.Key.Short())
		}
		// The application wins: its verb named like a standard verb
		// replaces the generated one. Aliases name the key, so they stay.
		r.Operations.Delete(o.Key)
	}
	if n := r.Nouns[o.Noun()]; n != nil && n.prop(o.Name()) != nil {
		panic(fmt.Sprintf("verb %s of %s clashes with its prop %s: a word after a noun is a verb or a prop, never both", o.Name(), o.Noun(), o.Name()))
	}
	if err := r.checkOverlap(o.Key.Short()); err != nil {
		panic(err.Error())
	}
	r.claimName(o.Key.Short())
	r.ensureNoun(o.Noun())
	if o.Parameters == nil {
		o.Parameters = map[string]Parameter{}
	}
	if err := r.CheckSourceArgs(o.Parameters); err != nil {
		panic(o.Key.Short() + ": " + err.Error())
	}
	if err := checkRestParameter(o); err != nil {
		panic(err.Error())
	}
	if err := CheckParameterSources(o); err != nil {
		panic(err.Error())
	}
	switch o.Kind {
	case "":
		o.Kind = KindInteraction
	case KindInteraction, KindApplication:
	default:
		panic("unknown operation kind " + o.Kind)
	}
	o.Checks = append([]VerbCheck{{Name: "agent-rights", requestCheck: (*Runtime).agentVerdict}}, o.Checks...)
	r.Operations.Set(o)
	r.adoptFieldParam(o.Key.Short())
}

// AddInput registers an editor instance. Options: Parent(id) (default app),
// Kind(noun) (default "editor"; "prompt" pins insert mode) and
// Completions(triggers...).
func (r *Runtime) AddInput(in *Input, opts ...InstanceOption) error {
	if in.ID == "" || r.Instances[in.ID] != nil {
		return errors.New("input ID must be unique and nonempty")
	}
	spec := instanceSpec{parent: "app", noun: EditorNoun}
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
	if spec.readOnly {
		in.ReadOnly = true
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
func (r *Runtime) Input(id string) *Input { return componentAt[*Input](r, id) }

// List returns the list an instance fronts, or nil.
func (r *Runtime) List(id string) *Collection { return componentAt[*Collection](r, id) }

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
func (r *Runtime) Lists() []*Collection {
	var out []*Collection
	for _, inst := range r.instancesInOrder() {
		if list, ok := inst.Component.(*Collection); ok {
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
	return r.executeAt(c, q, nil)
}

// operationRunner selects how an admitted operation is run. Choice reads use
// a synchronous runner; regular requests retain the delivery contract.
type operationRunner func(Operation, Context, Arguments) (any, error)

func (r *Runtime) executeAt(c Context, q Request, run operationRunner) (any, error) {
	actor := r.Actor()
	if r.agentTransport != nil {
		actor = r.agentTransport.actor
	} else if q.Actor != nil {
		actor = q.Actor.normalized()
	}
	if err := actor.validate(); err != nil {
		return nil, err
	}
	defer r.WithActor(actor)()
	approved := r.agentApproved
	if q.agentApproved {
		r.agentApproved = true
	}
	defer func() { r.agentApproved = approved }()
	if q.source != "" {
		defer r.forceSource(q.source)()
	}

	if _, ok := r.spelledAlias(q); !ok {
		r.NoteNounAlias(q.Noun) // a request spelled by a deprecated noun alias: its one-time notice (a renamed verb's alias posts its own)
	}
	q, notices := r.UseParamAliases(q) // an alias's old parameter names: their one-time notices
	for _, text := range notices {
		r.UI().Notify(text)
	}
	q = r.canonicalRequest(q)
	source := r.history.source
	if source == "" {
		source = "api"
	}
	at := time.Now()
	ctx := HistoryContext{Focus: focusOf(c), Page: r.Page, Mode: r.Mode.Name, Source: source, Actor: actor}
	depth := r.history.depth
	from := r.place()
	r.history.depth++
	value, err := r.execute(c, &q, false, run)
	r.history.depth--
	r.record(at, q, ctx, depth, from, value, err)
	if depth == 0 {
		r.inspectRequest(q, value, err)
	}
	if err == nil && r.historyKind(q) == HistoryChange {
		// The open surfaces count every change request that ran (events.go).
		r.countChange([]string{q.Noun})
	}
	r.watchAfterRequest(q, depth)
	if depth == 0 {
		// The hooks of what this request emitted run once it is done.
		r.runHooks()
	}
	return value, err
}

// endRequest drops what a top-level request memoized: its resolved targets.
func (r *Runtime) endRequest() {
	if r.history.depth <= 1 {
		clear(r.targets)
	}
}

func (r *Runtime) execute(c Context, request *Request, checkOnly bool, run ...operationRunner) (value any, err error) {
	defer r.endRequest()
	q := *request
	// A binding row's own request (runRow) is this request's only: the
	// handler's own requests start from their own.
	fromRow := c.Request.rowMade()
	c.Request = nil
	if q.IsProp() {
		// A prop-shaped request runs as its noun's standard get or set.
		pq, err := r.propVerbRequest(q)
		if err != nil {
			return nil, err
		}
		q = pq
	}
	op, ok := r.Operations.Find(Key(q.Key()))
	if !ok {
		return nil, r.unknownOperation(q.Noun, q.Verb)
	}
	r.targetAsKey(op, &q, request)
	if q.Args, err = r.standardArgs(op, q); err != nil {
		return nil, err
	}
	// Parameter sources (Parameter.From) fill what the request omits, for
	// every route; what a fan-out over several subjects runs is filled
	// again for each subject's own row.
	filled, err := r.parameterSources(op, q, focusOf(c))
	if err != nil {
		return nil, err
	}
	if err := r.checkArguments(op, filled); err != nil {
		return nil, &ArgumentError{err}
	}
	associated, err := r.associationSubject(op, q, focusOf(c))
	if err != nil {
		return nil, err
	}
	if associated != "" {
		q.Target = associated
		request.Target = associated
		if !checkOnly {
			top := r.TopModal()
			defer func() {
				r.notifyAssociation(op.Noun(), associated, top, err)
			}()
		}
	}
	guardMode := r.Mode
	if c.guardMode != "" {
		guardMode = ModeState{Name: c.guardMode}
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
	if !r.Available(withArgs(q, filled), focus, guardMode) {
		return nil, r.notApplicable(withArgs(q, filled))
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
	// selected row, from a key or the palette; a program's request (API,
	// CLI, MCP, a poll, a hook) acts on its own target (personSource).
	if vq, ok := r.visualApply(q, op, focus); ok && !checkOnly {
		return r.ExecuteAt(c, vq)
	}
	subjects, err := r.subjectsOf(q, focus)
	if err != nil {
		return nil, err
	}
	if op.hasSources() && (len(subjects) <= 1 || op.Many) {
		q.Args, request.Args = filled, filled
	}
	rc := &RequestContext{ActsFor: focus, AskedAt: actions, Mode: r.Mode}
	c.Request = rc
	defer r.enterRequest(rc)()
	switch v := r.runChecks(c, q, op); {
	case v.Refuse != "":
		if v.err != nil {
			return nil, v.err
		}
		return nil, errors.New(v.Refuse)
	case v.Ask != "" && !r.agentPassesPrompt(op):
		if checkOnly {
			return nil, nil
		}
		if len(subjects) > 1 {
			q.Subjects = subjects
		}
		return r.openConfirm(c, q, Operation{Key: KeyOf(op.Noun(), op.Name()), Confirm: v.Ask})
	}
	if checkOnly {
		return nil, nil
	}
	if op.Confirm != "" && !q.Confirmed && !r.agentPassesPrompt(op) {
		if len(subjects) > 1 {
			q.Subjects = subjects
		}
		ask := op
		if op.ConfirmFor != nil {
			ask.Confirm = op.ConfirmFor(r, c, q, op.Confirm)
		}
		if ask.Confirm != "" {
			return r.openConfirm(c, q, ask)
		}
	}
	if len(subjects) > 1 && !op.Many {
		return r.eachSubject(q, subjects)
	}
	defer r.invoking(r.invocationOf(q))()
	if fn := r.ActionFor(actions, q.Key()); fn != nil {
		return fn(r, c, q.Args)
	}
	rc.Subjects = subjects
	if len(run) > 0 && run[0] != nil {
		return run[0](op, c, q.Args)
	}
	return op.Run(r, c, q.Args)
}

// targetThingKey is the key parameter a request's target fills when the
// target names no instance (TargetInstance fails) but the operation takes
// its noun's thing by key (a subject parameter named after the key prop:
// Parameter.Focus with the noun) and the request gives no key: the target
// is then a thing's key or human key, read as that argument, so
// `register set --target a` names register a as `--name a` does, whether
// or not it exists yet (the store answers for a new key). An instance id,
// or a target the application's ResolveTarget maps, keeps its instance.
func (r *Runtime) targetThingKey(op Operation, q Request) (string, bool) {
	if q.Target == "" || q.Target == "focused-input" {
		return "", false
	}
	n := r.Noun(op.Noun())
	if n == nil || !n.resolvable() {
		return "", false
	}
	key := n.keyProp()
	p, ok := op.Parameters[key.Name]
	if !ok || !p.Focus || p.Noun != n.Name || !emptyProp(q.Args[key.Name]) {
		return "", false
	}
	if _, err := r.TargetInstance(q.Target); err == nil {
		return "", false
	}
	return key.Name, true
}

// targetAsKey moves a target that names a thing, not an instance
// (targetThingKey), into the subject's key argument, in the request being
// run and the one history records.
func (r *Runtime) targetAsKey(op Operation, q, request *Request) {
	key, ok := r.targetThingKey(op, *q)
	if !ok {
		return
	}
	args := maps.Clone(q.Args)
	if args == nil {
		args = Arguments{}
	}
	args[key] = q.Target
	q.Args, q.Target = args, ""
	request.Args, request.Target = args, ""
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
	return validateArguments(Operation{Parameters: optional, CatalogProxy: op.CatalogProxy}, args)
}

func validateArguments(op Operation, args Arguments) error {
	for name := range op.Parameters {
		if op.lacks(name, args) {
			return fmt.Errorf("%s is required", name)
		}
	}
	for name, v := range args {
		p, ok := op.Parameters[name]
		// A proxy's undeclared target is transport metadata, not a local
		// instance or a remote verb argument. The host forwards it as --target.
		if !ok && name == "target" && op.CatalogProxy {
			p, ok = Parameter{Type: "string"}, true
		}
		if !ok {
			return fmt.Errorf("unknown argument %q", name)
		}
		if k := kindOf(p.Type); k.accepts == nil || !k.accepts(v) {
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
	out := make([]Operation, 0, r.Operations.Len())
	for _, op := range r.Operations.Entries() {
		op.Aliases = r.aliasNames(op.Key.Short())
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key.Short() < out[j].Key.Short() })
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
	modes := r.allModeNames()
	nouns := map[string]any{}
	for name, noun := range r.Nouns {
		actions := make([]string, 0, len(noun.Actions))
		for key := range noun.Actions {
			actions = append(actions, key)
		}
		sort.Strings(actions)
		nouns[name] = map[string]any{"name": name, "verbs": r.Operations.ActionNames(noun), "bindings": noun.Bindings, "actions": actions, "pin": noun.Pin}
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
	r.Register(Operation{Key: "ochazuke.binding.defaults", HistoryKind: HistoryFetch, Description: "Print the default bindings (the framework's and the application's, without the user's bindings.json) as a bindings file, each group tagged with its source: the embedded file, go or generated", Result: "bindings file", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		data, err := FormatBindings(r.defaultBindingsDoc())
		return Verbatim(data), err
	}})
	r.Register(Operation{Key: "ochazuke.binding.stops", HistoryKind: HistoryFetch, Description: "List the rows every focus stop answers to, app-wide: each page's stops in drawing order, the open modals', then app; each with the focus and mode it is read at", Result: "stop bindings[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.StopBindings(), nil
	}})
	r.Register(Operation{Key: "ochazuke.input.focus", Description: "Focus a registered input", Parameters: map[string]Parameter{"id": {Type: "string", Required: true, Noun: "ochazuke.editor", Free: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		id := StringArg(a, "id", "")
		if r.Input(id) == nil {
			return nil, fmt.Errorf("unknown input %q", id)
		}
		err := r.SetFocus(id)
		return r.Context(), err
	}})
	r.Register(Operation{Key: "ochazuke.mode.switch", FocusBound: true, HistoryKind: HistoryAction, Description: "Switch the global interaction mode (a registered mode, or declared: the mode the focus declares, normal if none) and clear any pending payload", Parameters: map[string]Parameter{"mode": {Type: "string", Required: true, Noun: "ochazuke.mode"}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		mode, err := r.switchMode(StringArg(a, "mode", ""), r.Input(c.InputID))
		if err != nil {
			return nil, err
		}
		return mode, nil
	}})
	r.Register(Operation{Key: "ochazuke.modal.push", Description: "Open a modal focused on a registered input", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}, "input": {Type: "string", Required: true, Noun: "ochazuke.editor", Free: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		id := StringArg(a, "input", "")
		if r.Input(id) == nil {
			return nil, errors.New("unknown modal input")
		}
		err := r.Push(StringArg(a, "id", ""), id)
		return r.Context(), err
	}})
	r.Register(Operation{Key: "ochazuke.modal.close", Description: "Close the active modal: run its close request (CloseWith), else pop it back to the focus and mode it opened over", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) { err := r.CloseModal(c); return r.Context(), err }})
	r.Register(Operation{Key: "app.quit", Description: "Exit the application", Parameters: map[string]Parameter{"force": {Type: "boolean", Description: "quit without checks or waiting for writes; report unfinished values"}}, Result: "boolean", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		h := r.eventHub()
		now := time.Now()
		if a["force"] == true || r.Quit && now.Sub(h.quitRequested) >= r.Settings().Duration("quit.force-after") {
			h.quitForced = true
		}
		if !r.Quit {
			h.quitRequested = now
		}
		r.Quit = true
		return true, nil
	}})
	r.Register(Operation{Key: "ochazuke.key.ignore", Description: "The noop: always available, does nothing; a row bound to it consumes its key (DispatchOutcome reports KeyConsumed), how a container clamps a key its children leave unhandled", Result: "null", Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
}

// notApplicable is why execute refuses a request that is not available:
// the role it addresses that its noun lacks (missingRole), else that it
// does not apply now.
func (r *Runtime) notApplicable(q Request) error {
	if err := r.missingRole(q); err != nil {
		return err
	}
	return fmt.Errorf("%s is not applicable now", r.DisplayKey(q.Key()))
}

// withArgs is q with args as its arguments.
func withArgs(q Request, args Arguments) Request {
	q.Args = args
	return q
}

// standardArgs are a request's arguments as a standard verb reads them,
// before parameter sources fill them: an unavailable verb fails with its
// reason (Operation.Unavailable), and a row that reads its words itself
// (ReadArgs: create's prop aliases and prop=value words) does so on a
// noun whose props have aliases. Any other operation's arguments are q's.
func (r *Runtime) standardArgs(op Operation, q Request) (Arguments, error) {
	if reason := r.unavailableFor(op, q.Args); reason != "" {
		return nil, fmt.Errorf("%s is unavailable: %s", r.DisplayKey(q.Key()), reason)
	}
	row, n := standardRow(op.Standard), r.Noun(op.Noun())
	if row == nil || row.ReadArgs == nil || n == nil || !slices.ContainsFunc(n.Props, func(p PropSpec) bool { return len(p.Aliases) > 0 }) {
		return q.Args, nil
	}
	return row.ReadArgs(n, q.Args)
}
