package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// Noun is the behaviour shared by every instance of one kind: the verbs it
// answers to, its key bindings per mode, the action handlers that override an
// operation's default while the noun is on the focus stack, and an optional
// pinned mode (the prompt noun pins insert). Focusable marks every instance
// of the noun as a focus stop (focus.next and the mode rule read it); Region
// marks a grouping that focus.next {scope: "region"} jumps between.
//
// Bindings is keyed by mode name. "" holds rows that apply in every base
// mode; a pending mode (operator_pending, a key-sequence prefix, ...) is
// exclusive and reads only its own table.
type Noun struct {
	Name      string               `json:"name"`
	Verbs     []string             `json:"verbs,omitempty"`
	Pin       string               `json:"pin,omitempty"`
	Focusable bool                 `json:"focusable,omitempty"`
	Region    bool                 `json:"region,omitempty"`
	Bindings  map[string][]Binding `json:"bindings"`
	Extends   map[string]string    `json:"extends,omitempty"`
	Guards    map[string]string    `json:"guards,omitempty"`
	// Availability are the guards added to a verb beside its own
	// (Runtime.AddVerbAvailability), by verb: every one must hold too.
	Availability map[string][]string `json:"availability,omitempty"`
	Actions      map[string]Handler  `json:"-"`
	// Columns are the table columns a list of the noun's rows draws
	// (NounBuilder.Columns, the catalog's columns): declared data a host
	// passes to Columns when it builds the list, never applied on its own.
	Columns []Column `json:"columns,omitempty"`
	// Completions are the completion triggers of the noun's inputs that
	// declare none of their own (NounBuilder.Completions).
	Completions []CompletionTrigger `json:"-"`
	// Props are the fields of the noun's things (NounBuilder.Props,
	// props.go), in declaration order; Store is its store row
	// (NounBuilder.Store), nil when it declares none.
	Props []PropSpec `json:"props,omitempty"`
	Store *Store     `json:"-"`
	// Filters are default saved prop filters for lists of this noun's rows.
	Filters []SavedFilter `json:"filters,omitempty"`
}

// Binding is one row of a mode table, identical in code and config:
// {noun | instance, mode, keys | pattern, request | null, from, when}.
// Keys is an exact key or a space-separated sequence ("g g"); Pattern is a
// key class: digit, alpha, printable, paste, any, a class minus keys
// ("alpha-q"), or a set ("one-of:h j k l"); upper is one uppercase
// letter and ctrl+alpha one ctrl+letter (ctrl+a to ctrl+z). From fills request arguments from
// the key or the mode payload (see Sources). When names a registered guard.
// A row's identity is its table (noun or instance), mode, keys or pattern
// and guard: a row replaces only the row with the same identity, so a row
// with When and the unguarded row on the same keys coexist, and Unbind
// removes only the row with the same identity (an unbind row without When
// removes the unguarded row, never a guarded one). The editor noun's
// implicit "input" guard is not part of the identity (see
// withImplicitGuard).
// Replay dispatches the key again, fresh, after the row ran (a pending mode
// that hands an unknown key back to the stack). Capture rows are consulted
// outer-first (app first) before the inner-first walk (see resolve.go).
//
// A row is atomic: a Request (one registered operation, its arguments from
// the row, the key or the payload), a mode transition (Enter, Leave) or an
// unbind. There is no row that runs Go code with the key. Composite
// behaviour is an operation whose handler calls other operations through
// Execute (macro.record, terminal.paste under the leader); a condition is a
// named guard. TestBindingRowsAreAtomic pins this for every table.
type Binding struct {
	// The field order is the bindings file's (BindingFieldOrder): mode,
	// the noun or instance, the keys or pattern, the request, then the
	// rest. FormatBindingRows writes a file in that order (and the
	// request noun before verb); plain json.Marshal follows the
	// declarations below.
	Mode     string            `json:"mode,omitempty"`
	Noun     string            `json:"noun,omitempty"`
	Instance string            `json:"instance,omitempty"`
	Keys     string            `json:"keys,omitempty"`
	Pattern  string            `json:"pattern,omitempty"`
	Request  Request           `json:"request"`
	When     string            `json:"when,omitempty"`
	Unbind   bool              `json:"unbind,omitempty"`
	From     map[string]string `json:"from,omitempty"`
	Replay   bool              `json:"replay,omitempty"`
	Capture  bool              `json:"capture,omitempty"`
	// Hint replaces "noun.verb: description" in the footer and the help
	// for a row whose operation says nothing useful on its own (the
	// terminal leader's mode.enter); bindings list carries it too.
	Hint string `json:"hint,omitempty"`
	// Hidden keeps the row off the footer's hint line (counts, registers,
	// macros, ctrl+c); ? help and bindings list still show it.
	Hidden bool `json:"hidden,omitempty"`
	// Help replaces the operation's description in ? help and bindings
	// list for a row whose operation is generic (mode.enter into a
	// register, macro or count mode).
	Help string `json:"help,omitempty"`
	// Aliases are the other names of the row's operation, which bindings
	// list adds so a row names its canonical operation and its aliases
	// once; a stored row never holds them (a file's value is dropped).
	Aliases []string `json:"aliases,omitempty"`
	// named holds the row's text as written when its keys, hint, help or
	// a string argument name a setting ("{terminal-leader}"): the row
	// holds the resolved text and is resolved again from this when the
	// setting changes (binding_settings.go). Nil for every other row.
	named *namedRow
}

// Sources a From entry may name: "key" (the key name), "digit" (the key as an
// integer), "text" (pasted or typed text), "count" (payload count, at least 1),
// "line" (payload count, 0 when absent, else the operator count), "steps"
// (count times operator count), "op" (payload operator), "opcount", "prefix"
// (the key sequence the pending mode holds), "letter" (the key's letter,
// lowercased, without ctrl+ or shift+: f, F, shift+f and ctrl+f all read
// f; "" for a key that is not one letter), "row" (the row's Data, see
// FromRow) and "row:<field>" (one field of it). A From value may name
// several sources separated by spaces ("prefix key"): their texts are
// joined with a space, the way terminal.send reads a key list.
const (
	SourceKey     = "key"
	SourceDigit   = "digit"
	SourceText    = "text"
	SourceCount   = "count"
	SourceLine    = "line"
	SourceSteps   = "steps"
	SourceOp      = "op"
	SourceOpCount = "opcount"
	SourcePrefix  = "prefix"
	SourceLetter  = "letter"
	SourceRow     = "row"
)

// CancelKey is the key every key-sequence prefix answers with mode.leave: a
// default row "<prefix> esc" is kept in each table that binds a sequence, so
// Escape cancels a pending prefix without running anything and the resolver
// never names a key.
const CancelKey = "esc"

// Pattern names.
const (
	Digit     = "digit"
	Alpha     = "alpha"
	Printable = "printable"
	// Upper is one uppercase letter (A to Z, any script's capital): it
	// answers before Alpha, which an uppercase letter also belongs to.
	Upper = "upper"
	// CtrlAlpha is one ctrl+letter, ctrl+a to ctrl+z: one row for every
	// control letter, the letter read with the letter source. A legacy
	// terminal sends ctrl+h, ctrl+i, ctrl+j and ctrl+m as backspace, tab
	// and enter, and ctrl+[ as esc, so those never reach the row there;
	// labels a row reads this way should avoid h, i, j and m.
	CtrlAlpha = "ctrl+alpha"
	Paste     = "paste"
	Any       = "any"
)

// same reports whether two rows have the same identity: mode, keys or
// pattern, and guard (identityGuard). The table (noun or instance) is the
// caller's.
func (b Binding) same(o Binding) bool {
	return b.Mode == o.Mode && b.Keys == o.Keys && b.Pattern == o.Pattern && b.identityGuard() == o.identityGuard()
}

// sameKeys reports whether two rows answer the same mode and keys or
// pattern, whatever their guards.
func (b Binding) sameKeys(o Binding) bool {
	return b.Mode == o.Mode && b.Keys == o.Keys && b.Pattern == o.Pattern
}

// ImplicitGuard is the guard the framework puts on every editor noun row
// that runs an input operation (input.cancel aside): such a row answers
// only while the focus is an input itself, not a non-input instance an
// application parented under one. It is the framework's, not the user's:
// it is not part of a row's identity, so a row without when replaces or
// unbinds the default it shadows, and a user row that runs an input
// operation on the editor noun gets it too, as the default it replaces had.
const ImplicitGuard = "input"

// withImplicitGuard gives an editor noun row that runs an input operation
// the implicit guard when it names none.
func withImplicitGuard(row Binding) Binding {
	if row.Noun == "ochazuke.editor" && row.Instance == "" && row.When == "" && implicitlyGuarded(row) {
		row.When = ImplicitGuard
	}
	return row
}

// implicitlyGuarded reports whether a row's request is one the editor
// noun's rows guard implicitly.
func implicitlyGuarded(row Binding) bool {
	return row.Request.Noun == "ochazuke.input" && row.Request.Verb != "cancel"
}

// identityGuard is the guard that is part of the row's identity: When,
// except the implicit guard on the editor noun, which counts as none
// whether the framework added it or the row spells it out (the editor
// noun is consulted only with an input on the focus path, where the guard
// is the framework's detail, not a second row).
func (b Binding) identityGuard() string {
	if b.When == ImplicitGuard && b.Noun == "ochazuke.editor" && b.Instance == "" {
		return ""
	}
	return b.When
}

// placeRow puts row into a table by identity: in place of the row with the
// same identity (any further row with it is dropped), else, for a guarded
// row, before the first unguarded row on the same keys in the same pass
// (capture or not), which would otherwise answer first and leave it dead,
// else at the end. It returns the table and the row it displaced, if any.
func placeRow(rows []Binding, row Binding) ([]Binding, *Binding) {
	out, displaced := placeRowIn(rows, row, nil)
	if len(displaced) == 0 {
		return out, nil
	}
	return out, &displaced[0]
}

// rowBatch is the row identities (sourceKey) one batch of rows has placed
// so far: a file (a default file, the user's bindings.json) or one Bind
// call. A key may repeat in a batch: its first row with an identity
// replaces the table's rows with that identity, and its later ones are
// alternatives placed after it in order, so the resolver runs the first
// whose action is available (Available). A nil batch is one row.
type rowBatch map[string]bool

// placeRowIn is placeRow inside a batch, returning every row it displaced:
// a row whose identity the batch placed already goes after the last row
// with it, and displaces nothing.
func placeRowIn(rows []Binding, row Binding, batch rowBatch) ([]Binding, []Binding) {
	id := sourceKey(row)
	if batch[id] {
		for i := len(rows) - 1; i >= 0; i-- {
			if rows[i].same(row) {
				return insertRow(rows, i+1, row), nil
			}
		}
	}
	if batch != nil {
		batch[id] = true
	}
	if i := slices.IndexFunc(rows, row.same); i >= 0 {
		var displaced []Binding
		out := make([]Binding, 0, len(rows))
		for j, b := range rows {
			switch {
			case j == i:
				out = append(out, row)
				displaced = append(displaced, b)
			case b.same(row):
				displaced = append(displaced, b)
			default:
				out = append(out, b)
			}
		}
		return out, displaced
	}
	at := len(rows)
	if row.identityGuard() != "" {
		if i := slices.IndexFunc(rows, func(b Binding) bool {
			return b.sameKeys(row) && b.identityGuard() == "" && b.Capture == row.Capture
		}); i >= 0 {
			at = i
		}
	}
	return insertRow(rows, at, row), nil
}

// insertRow inserts row at index at (clamped to the table).
func insertRow(rows []Binding, at int, row Binding) []Binding {
	at = max(0, min(at, len(rows)))
	out := make([]Binding, 0, len(rows)+1)
	out = append(out, rows[:at]...)
	out = append(out, row)
	return append(out, rows[at:]...)
}

// Do builds a row that runs "noun.verb" with optional arguments.
func Do(operation string, args ...Arguments) Binding {
	noun, verb := SplitKey(operation)
	q := Request{Verb: verb, Noun: noun}
	for _, a := range args {
		if q.Args == nil {
			q.Args = Arguments{}
		}
		for k, v := range a {
			q.Args[k] = v
		}
	}
	return Binding{Request: q}
}

// Enter builds a row that enters a pending mode with a typed payload: the
// operator fields (Op, Count, OpCount) and, for a leader, Prefix (the key
// sequence the mode holds, which arms the sequence timer) and Request (what
// the timer runs when no second key arrives).
func Enter(mode string, payload ...Payload) Binding {
	args := Arguments{"mode": mode}
	for _, p := range payload {
		if p.Op != "" {
			args["op"] = p.Op
		}
		if p.Count != 0 {
			args["count"] = p.Count
		}
		if p.OpCount != 0 {
			args["opcount"] = p.OpCount
		}
		if p.Prefix != "" {
			args["prefix"] = p.Prefix
		}
		if p.Request != nil {
			args["request"] = *p.Request
		}
	}
	return Do("ochazuke.mode.enter", args)
}

// Leave builds a row that returns from a pending mode to the mode it shadowed.
func Leave() Binding { return Do("ochazuke.mode.leave") }

// FromKey fills an argument from the pressed key name.
func (b Binding) FromKey(arg string) Binding { return b.from(arg, SourceKey) }

// FromPayload fills an argument from the mode payload (a Source name).
func (b Binding) FromPayload(arg, source string) Binding { return b.from(arg, source) }

// FromRow fills an argument from the Data of the row the key acted on:
// the innermost instance carrying Data from the focus out to the level the
// row answered at (a list row's ListItem.Data, also when the row is bound
// on the list). field names a key of that value (a map's key, a struct's
// JSON field name; dots walk nested values, "repo.path"); "" passes the
// whole value. A struct is read through its JSON encoding, so the value
// is what the CLI would send (numbers float64, the whole row an object). A row without Data, or without the field, leaves the
// argument out. The source is "row:<field>" (or "row") in a binding file.
func (b Binding) FromRow(arg, field string) Binding {
	if field == "" {
		return b.from(arg, SourceRow)
	}
	return b.from(arg, SourceRow+":"+field)
}

// Join fills an argument from several sources joined with a space: the
// leader's any row sends Join("keys", SourcePrefix, SourceKey).
func (b Binding) Join(arg string, sources ...string) Binding {
	return b.from(arg, strings.Join(sources, " "))
}

// Guard names the registered guard the row requires.
func (b Binding) Guard(name string) Binding { b.When = name; return b }

// Hinted sets the row's hint (config field "hint"): the text shown after
// its key in place of the operation and its description.
func (b Binding) Hinted(text string) Binding { b.Hint = text; return b }

// Hide keeps the row off the footer (config field "hidden"); ? help and
// bindings list still show it.
func (b Binding) Hide() Binding { b.Hidden = true; return b }

func (b Binding) from(arg, source string) Binding {
	from := map[string]string{}
	for k, v := range b.From {
		from[k] = v
	}
	from[arg] = source
	b.From = from
	return b
}

// Define declares a noun, or returns the builder of an existing one. A
// name the framework's nouns answer to (settings for ochazuke.settings:
// CanonicalNoun) returns that noun's builder, so an application's verbs
// and rows extend it; DefineOwn claims the short name for a noun of the
// application's own instead. Inside Builtin a plain name is the framework
// noun ochazuke.<name>.
func (r *Runtime) Define(name string) *NounBuilder {
	if name == "" {
		panic("noun name must be nonempty")
	}
	if r.builtinDepth > 0 {
		name = frameworkName(name)
	} else {
		name = r.CanonicalNoun(name)
	}
	return &NounBuilder{r: r, n: r.ensureNoun(name)}
}

// DefineOwn declares a noun of the application's own under exactly name,
// or returns its builder when it exists. When name is the alias of a
// framework noun (settings for ochazuke.settings) the application's noun owns
// the short name from then on: keys, binding files, actions.json, the
// palette and the CLI reach it by that name, and the framework's noun
// stays reachable by its canonical name (ochazuke.settings), its verbs and
// rows intact. An application with a settings, config or page noun of its
// own declares it with DefineOwn instead of removing the framework's.
func (r *Runtime) DefineOwn(name string) *NounBuilder {
	if name == "" {
		panic("noun name must be nonempty")
	}
	return &NounBuilder{r: r, n: r.ensureNoun(name)}
}

// ensureNoun is the noun of that exact name, created when it does not
// exist yet (refused when the name is an operation key: checkNounOverlap).
// Inside Builtin a framework noun ochazuke.<noun> records its alias.
func (r *Runtime) ensureNoun(name string) *Noun {
	n := r.Nouns[name]
	if n == nil {
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.Contains(name, "..") {
			panic(fmt.Sprintf("noun %q has an empty part", name))
		}
		if err := r.checkNounOverlap(name); err != nil {
			panic(err.Error())
		}
		n = &Noun{Name: name, Bindings: map[string][]Binding{}, Actions: map[string]Handler{}}
		r.Nouns[name] = n
		r.claimBareName(name)
	}
	r.noteFrameworkNoun(name)
	return n
}

// Noun returns the registered noun a name stands for (an alias resolved:
// CanonicalNoun), or nil.
func (r *Runtime) Noun(name string) *Noun { return r.Nouns[r.CanonicalNoun(name)] }

// NounBuilder is the declaration surface of one noun.
type NounBuilder struct {
	r    *Runtime
	n    *Noun
	last string // the operation key the last Verb call declared
}

func (b *NounBuilder) Noun() *Noun { return b.n }

// Pin keeps every instance of the noun in one base mode.
func (b *NounBuilder) Pin(mode string) *NounBuilder { b.n.Pin = mode; return b }

// Focusable makes every instance of the noun a focus stop.
func (b *NounBuilder) Focusable() *NounBuilder { b.n.Focusable = true; return b }

// Columns declares the table columns a list of the noun's rows draws, in
// order, each row's ListItem.Cells the same order (Noun.Columns). The
// catalog carries them, so a host mounting the noun's rows draws a table;
// an application passes r.Noun(noun).Columns to the Columns list option.
func (b *NounBuilder) Columns(cols ...Column) *NounBuilder {
	b.n.Columns = slices.Clone(cols)
	return b
}

// Region makes every instance of the noun a focus region: a grouping (a
// panel) that focus.next {scope: "region"} jumps between, never a stop.
func (b *NounBuilder) Region() *NounBuilder { b.n.Region = true; return b }

// Verbs records the verbs the noun answers to, for help and the palette.
func (b *NounBuilder) Verbs(verbs ...string) *NounBuilder {
	b.n.Verbs = append(b.n.Verbs, verbs...)
	return b
}

// Verb registers noun.verb as an operation when it is new, else installs the
// handler as this noun's override of the existing operation. The handler is
// a Handler (an interaction verb, run on the loop) or an AppHandler
// (func(Ctx) Result: an application verb whose pending work runs off the
// loop). An override of an existing operation must be a Handler.
func (b *NounBuilder) Verb(verb string, h any, description ...string) *NounBuilder {
	key := b.n.Name + "." + verb
	run, app := appHandlerOf(h)
	if existing, ok := b.r.Operations[key]; !ok || existing.Standard != "" {
		// A verb named like a generated standard verb replaces it.
		b.r.Register(Operation{Verb: verb, Noun: b.n.Name, Description: strings.Join(description, " "), Result: "JSON value", Run: run, App: app})
		if ok {
			b.last = key
			return b
		}
	} else if run != nil {
		b.n.Actions[key] = run
	} else {
		panic("an application verb cannot override " + key)
	}
	b.last = key
	b.Verbs(verb)
	return b
}

// Confirm asks before the last declared verb runs: the prompt template
// ({{id}}, {{noun}}, {{<arg>}}) opens a confirm modal whose y runs the
// stored request and n or esc pops it. An empty prompt removes the confirm.
func (b *NounBuilder) Confirm(prompt string) *NounBuilder {
	if b.last == "" {
		panic("Confirm must follow Verb")
	}
	if err := b.r.SetConfirm(b.last, prompt); err != nil {
		panic(err)
	}
	return b
}

// Rows declares that the last declared verb's result lists rows of noun
// (Operation.Rows): a host reading the catalog knows which noun a list
// verb's rows answer to. An empty noun removes the declaration.
func (b *NounBuilder) Rows(noun string) *NounBuilder {
	if b.last == "" {
		panic("Rows must follow Verb")
	}
	op := b.r.Operations[b.last]
	if noun != "" {
		noun = b.r.CanonicalNoun(noun)
	}
	op.Rows = noun
	b.r.Operations[b.last] = op
	return b
}

// State marks the last declared verb as the runtime's state source
// (Operation.State): one call answers every list's rows as JSON Lines, each
// line tagged with the list noun (see cli.CatalogDoc). A host reading the
// catalog's state runs it once instead of one row source per list. The
// framework names no state verb.
func (b *NounBuilder) State() *NounBuilder {
	if b.last == "" {
		panic("State must follow Verb")
	}
	op := b.r.Operations[b.last]
	op.State = true
	b.r.Operations[b.last] = op
	return b
}

// Quiet marks the last declared verb as bookkeeping (Operation.Quiet): its
// jobs stay off the jobs ticker. The framework names no quiet verb.
func (b *NounBuilder) Quiet() *NounBuilder {
	if b.last == "" {
		panic("Quiet must follow Verb")
	}
	op := b.r.Operations[b.last]
	op.Quiet = true
	b.r.Operations[b.last] = op
	return b
}

// PersistArgs marks the last declared verb's text-like arguments as safe
// to keep in the history file (Operation.PersistArgs).
func (b *NounBuilder) PersistArgs() *NounBuilder {
	if b.last == "" {
		panic("PersistArgs must follow Verb")
	}
	op := b.r.Operations[b.last]
	op.PersistArgs = true
	b.r.Operations[b.last] = op
	return b
}

// Fetch marks the last declared verb as a data request (Operation.HistoryKind
// HistoryFetch): a state, list or show verb. Its calls are history entries
// of kind fetch, which history.list leaves out by default and the ring
// bounds apart from the user's actions and changes.
func (b *NounBuilder) Fetch() *NounBuilder {
	if b.last == "" {
		panic("Fetch must follow Verb")
	}
	op := b.r.Operations[b.last]
	op.HistoryKind = HistoryFetch
	b.r.Operations[b.last] = op
	return b
}

// Motion marks the last declared verb as a motion (Operation.Motion): the
// footer leaves its rows to ? help, as it does for list.move.
func (b *NounBuilder) Motion() *NounBuilder {
	if b.last == "" {
		panic("Motion must follow Verb")
	}
	op := b.r.Operations[b.last]
	op.Motion = true
	b.r.Operations[b.last] = op
	return b
}

// Many marks the last declared verb as taking many subjects at once
// (Operation.Many): run over a visual selection, its handler is called
// once with every selected row in Ctx.Subjects instead of once per row.
// Use it for a verb that asks something (a chooser, a confirm naming what
// it affects) or that should make one change and one notice for all.
func (b *NounBuilder) Many() *NounBuilder {
	if b.last == "" {
		panic("Many must follow Verb")
	}
	op := b.r.Operations[b.last]
	op.Many = true
	b.r.Operations[b.last] = op
	return b
}

// Positional names the parameters of the last declared verb that bare
// words fill first, in order (Operation.Positional): Positional("name")
// makes BIN project link foo set name even though the boolean all sorts
// first. The CLI gives a boolean a bare word only when it is named here.
func (b *NounBuilder) Positional(names ...string) *NounBuilder {
	if b.last == "" {
		panic("Positional must follow Verb")
	}
	op := b.r.Operations[b.last]
	op.Positional = slices.Clone(names)
	b.r.Operations[b.last] = op
	return b
}

// When guards the last declared verb with a registered guard name: while
// the guard is false at the focus and mode the verb would run in, the
// palette does not offer it. The guard reads a KeyEvent whose Focus and
// Level are that focus and whose Mode is that mode. A palette noun's
// default verb is never guarded (RegisterNoun refuses one). An empty name
// removes the guard.
func (b *NounBuilder) When(guard string) *NounBuilder {
	if b.last == "" {
		panic("When must follow Verb")
	}
	_, verb := b.r.keyParts(b.last)
	if b.n.Guards == nil {
		b.n.Guards = map[string]string{}
	}
	if guard == "" {
		delete(b.n.Guards, verb)
	} else {
		b.n.Guards[verb] = guard
	}
	return b
}

// VerbGuard names the guard on noun.verb, "" when the verb is unguarded.
func (r *Runtime) VerbGuard(key string) string {
	noun, verb := r.keyParts(key)
	if n := r.Noun(noun); n != nil {
		return n.Guards[verb]
	}
	return ""
}

// VerbShown reports whether noun.verb is offered at a focus in a mode: true
// for an unguarded verb or one whose guard holds there. A guard name that
// is not registered hides nothing. It is Available for a request with no
// arguments.
func (r *Runtime) VerbShown(key, focus string, mode ModeState) bool {
	noun, verb := r.keyParts(key)
	return r.Available(Request{Noun: noun, Verb: verb}, focus, mode)
}

// Available reports whether an action can run now: its availability (the
// verb guard, NounBuilder.When or SetAvailability) holds at the focus in
// the mode for the request's arguments, or it has none. The guard reads a
// KeyEvent whose Focus and Level are focus, Mode is mode and Args are the
// request's (a binding row's own arguments, or a CLI call's), so an action
// that acts on an argument (timeline.open {card}) judges that thing. The
// resolver runs, at each level, the first row on a key whose action is
// available (an unavailable one stands aside and the key goes on, out to
// the next level when none at this one is); the palette, the hints, the
// help and Execute ask the same question. A guard name that is not
// registered hides nothing. A standard verb also reads its props' guards
// (PropSpec.When, propsAvailable), and a prop-shaped request is asked as
// the get or set it runs as.
func (r *Runtime) Available(q Request, focus string, mode ModeState) bool {
	if q.IsProp() {
		pq, err := r.propVerbRequest(q)
		if err != nil {
			return false
		}
		q = pq
	}
	for _, name := range r.VerbAvailability(q.Key()) {
		if guard := r.guard(name); guard != nil && !guard(r, KeyEvent{Mode: mode, Focus: focus, Level: focus, Args: q.Args}) {
			return false
		}
	}
	return r.propsAvailable(q, focus, mode)
}

// AddVerbAvailability adds a guard to a registered verb (aliases resolve)
// beside the availability it already has (its own guard, NounBuilder.When
// or SetAvailability, and guards added before): the verb is available
// only while every one holds, so the resolver, the palette, the hints, the
// help and Execute all leave it out where the added guard is false. It is
// how an application narrows a framework verb it uses
// (ochazuke.list.choose-column where its own pane draws no columns) without
// replacing the framework's guard or guarding each binding row. The guard
// need not be registered yet; one that is not registered when asked hides
// nothing, as a verb guard does. Adding a guard the verb already has is a
// no-op.
func (r *Runtime) AddVerbAvailability(key, guard string) error {
	if guard == "" {
		return errors.New("AddVerbAvailability: empty guard name")
	}
	op, ok := r.Operation(key)
	if !ok {
		return fmt.Errorf("unknown operation %q", key)
	}
	n := r.Noun(op.Noun)
	if n == nil {
		return fmt.Errorf("unknown noun %q", op.Noun)
	}
	if n.Guards[op.Verb] == guard || slices.Contains(n.Availability[op.Verb], guard) {
		return nil
	}
	if n.Availability == nil {
		n.Availability = map[string][]string{}
	}
	n.Availability[op.Verb] = append(slices.Clone(n.Availability[op.Verb]), guard)
	return nil
}

// RemoveVerbAvailability takes a guard AddVerbAvailability added off a
// verb; false when the verb had no such added guard (its own guard stays:
// SetAvailability changes that one).
func (r *Runtime) RemoveVerbAvailability(key, guard string) bool {
	op, ok := r.Operation(key)
	if !ok {
		return false
	}
	n := r.Noun(op.Noun)
	if n == nil || !slices.Contains(n.Availability[op.Verb], guard) {
		return false
	}
	n.Availability[op.Verb] = slices.DeleteFunc(slices.Clone(n.Availability[op.Verb]), func(g string) bool { return g == guard })
	if len(n.Availability[op.Verb]) == 0 {
		delete(n.Availability, op.Verb)
	}
	return true
}

// VerbAvailability names every guard noun.verb is available under: its own
// (VerbGuard) first, then those AddVerbAvailability added, in order; nil
// when it has none.
func (r *Runtime) VerbAvailability(key string) []string {
	noun, verb := r.keyParts(key)
	n := r.Noun(noun)
	if n == nil {
		return nil
	}
	var out []string
	if g := n.Guards[verb]; g != "" {
		out = append(out, g)
	}
	return append(out, n.Availability[verb]...)
}

// SetAvailability makes a registered action available only while the
// named guard holds (Available): what NounBuilder.When does for the verb
// it follows, for an operation registered with Register (the framework's
// components). An empty guard makes the action always available.
func (r *Runtime) SetAvailability(key, guard string) {
	noun, verb := r.keyParts(key)
	n := r.Noun(noun)
	if n == nil {
		panic(fmt.Sprintf("SetAvailability: unknown noun %q", noun))
	}
	if guard == "" {
		delete(n.Guards, verb)
		return
	}
	if n.Guards == nil {
		n.Guards = map[string]string{}
	}
	n.Guards[verb] = guard
}

// Action overrides any operation ("noun.verb") while this noun is on the stack.
func (b *NounBuilder) Action(key string, h Handler) *NounBuilder {
	b.n.Actions[b.r.CanonicalKey(key)] = h
	return b
}

// Mode selects the table rows are bound into; "" is the any-mode table.
func (b *NounBuilder) Mode(mode string) *ModeBuilder { return &ModeBuilder{NounBuilder: b, mode: mode} }

// Extends makes the selected mode's table read parent's rows after its own,
// as the editor spec's "extends" does: three motion tables become one
// table and two that extend it. A mode extends one parent; Extends("")
// removes the link.
func (m *ModeBuilder) Extends(parent string) *ModeBuilder {
	if m.n.Extends == nil {
		m.n.Extends = map[string]string{}
	}
	if parent == "" {
		delete(m.n.Extends, m.mode)
	} else {
		m.n.Extends[m.mode] = parent
	}
	delete(m.r.defaults.extends, m.n.Name+"/"+m.mode)
	m.r.CancelPendingBinding()
	return m
}

// ModeBuilder binds rows into one mode table of a noun.
type ModeBuilder struct {
	*NounBuilder
	mode string
	last int
	// displaced is the row the last add replaced, put back if When then
	// gives the new row another identity.
	displaced *Binding
}

// Bind adds an exact-key (or key-sequence) row, replacing the row with the
// same keys and guard (Binding.Guard, or When after Bind).
func (m *ModeBuilder) Bind(keys string, row Binding) *ModeBuilder {
	row.Keys, row.Pattern = strings.Join(strings.Fields(keys), " "), ""
	return m.add(row)
}

// Pattern adds a key-class row.
func (m *ModeBuilder) Pattern(pattern string, row Binding) *ModeBuilder {
	row.Keys, row.Pattern = "", pattern
	return m.add(row)
}

// Unbind removes the unguarded row with these keys or this pattern; a
// guarded row on them stays (UnbindGuarded removes that one).
func (m *ModeBuilder) Unbind(keysOrPattern string) *ModeBuilder {
	return m.UnbindGuarded(keysOrPattern, "")
}

// UnbindGuarded removes the row with these keys or this pattern and this
// guard ("" is the unguarded row).
func (m *ModeBuilder) UnbindGuarded(keysOrPattern, guard string) *ModeBuilder {
	probe := Binding{Noun: m.n.Name, Mode: m.mode, Keys: strings.Join(strings.Fields(keysOrPattern), " "), When: guard}
	if isPattern(keysOrPattern) {
		probe.Keys, probe.Pattern = "", keysOrPattern
	}
	m.r.forgetDefaultSource(probe)
	m.n.Bindings[m.mode] = withPrefixCancels(removeRows(m.n.Bindings[m.mode], probe))
	m.last, m.displaced = len(m.n.Bindings[m.mode]), nil
	return m
}

// When guards the last bound row with a registered guard name. The guard is
// part of the row's identity, so the row is placed again under it: a row it
// replaced as an unguarded row comes back, and it replaces the row with the
// same keys and this guard instead.
func (m *ModeBuilder) When(guard string) *ModeBuilder {
	rows := m.n.Bindings[m.mode]
	if m.last < 0 || m.last >= len(rows) || rows[m.last].When == guard {
		return m
	}
	row := rows[m.last]
	if m.displaced != nil {
		rows[m.last] = *m.displaced
	} else {
		rows = append(rows[:m.last:m.last], rows[m.last+1:]...)
	}
	m.n.Bindings[m.mode] = rows
	row.When = guard
	return m.add(row)
}

func (m *ModeBuilder) add(row Binding) *ModeBuilder {
	row.Noun, row.Instance, row.Mode = m.n.Name, "", m.mode
	row.Request = m.r.modernRequest(row.Request)
	// A row naming a setting ({terminal-leader}) is resolved here as Bind
	// resolves it; keys naming an unknown setting stay as written, and
	// ValidateBindings reports them.
	if resolved, err := m.r.resolveNamedRow(row); err == nil {
		row = resolved
	}
	row = withImplicitGuard(row)
	m.r.forgetDefaultSource(row)
	rows, displaced := placeRow(m.n.Bindings[m.mode], row)
	rows = withPrefixCancels(rows)
	m.n.Bindings[m.mode], m.displaced = rows, displaced
	m.last = slices.IndexFunc(rows, row.same)
	// As Runtime.Bind: a key sequence in progress is kept unless the row
	// is on it.
	if m.r.rowTouchesPending(row) {
		m.r.CancelPendingBinding()
	}
	return m
}

func removeRows(rows []Binding, probe Binding) []Binding {
	out := rows[:0]
	for _, b := range rows {
		if !b.same(probe) {
			out = append(out, b)
		}
	}
	return out
}

// withPrefixCancels keeps one "<prefix> esc" → mode.leave row per proper
// prefix of every key sequence in a table, and drops the rows it generated
// for prefixes no sequence continues any more. This is where a prefix mode
// is created (a sequence row makes its prefixes wait), so this is where its
// default cancel row lives; a row an application binds to those keys wins.
func withPrefixCancels(rows []Binding) []Binding {
	// A table is one mode's rows on a noun, or an instance's rows across
	// modes, so prefixes are tracked per mode.
	type prefixKey struct{ mode, prefix string }
	live := map[prefixKey]Binding{} // the cancel row each live prefix wants
	bound := map[prefixKey]bool{}
	var order []prefixKey
	for _, b := range rows {
		if b.Keys == "" || b.Unbind {
			continue
		}
		parts := strings.Fields(b.Keys)
		if b.identityGuard() == "" {
			// A guarded row on "<prefix> esc" may stand aside; the
			// unguarded cancel stays behind it.
			bound[prefixKey{b.Mode, b.Keys}] = true
		}
		if isPrefixCancel(b) {
			continue
		}
		for i := 1; i < len(parts); i++ {
			k := prefixKey{b.Mode, strings.Join(parts[:i], " ")}
			if _, seen := live[k]; !seen {
				row := Leave()
				row.Noun, row.Instance, row.Mode, row.Keys = b.Noun, b.Instance, b.Mode, k.prefix+" "+CancelKey
				live[k] = row
				order = append(order, k)
			}
		}
	}
	out := rows[:0]
	for _, b := range rows {
		if isPrefixCancel(b) {
			if _, ok := live[prefixKey{b.Mode, strings.TrimSuffix(b.Keys, " "+CancelKey)}]; !ok {
				continue
			}
		}
		out = append(out, b)
	}
	for _, k := range order {
		if !bound[prefixKey{k.mode, k.prefix + " " + CancelKey}] {
			out = append(out, live[k])
		}
	}
	return out
}

// isPrefixCancel reports whether a row is the default cancel of a sequence
// prefix: "<prefix> esc" running leave.mode.
func isPrefixCancel(b Binding) bool {
	return strings.HasSuffix(b.Keys, " "+CancelKey) && b.Request.Key() == "ochazuke.mode.leave" && len(b.From) == 0 && b.When == ""
}

// isPattern reports whether a config "keys" value names a key class.
func isPattern(s string) bool {
	class, _, _ := strings.Cut(s, "-")
	switch class {
	case Digit, Alpha, Upper, CtrlAlpha, Printable, Paste, Any:
		return true
	}
	return strings.HasPrefix(s, "one-of:")
}

// Bind installs rows from config or code onto their noun or instance tables
// by identity (mode, keys or pattern, and When): the row with the same
// identity is replaced in place, a new row is added (a guarded one before
// the unguarded row on its keys, so it answers first while its guard
// holds), and an unbind row removes only the row with its identity.
// A key sequence in progress (d of d d) is kept unless a row added or
// removed is on it: in its mode, with the keys typed so far, a sequence
// extending them or one they extend. Such a row cancels the sequence, so
// the next key starts fresh against the new tables.
//
// The rows of one call are one batch (rowBatch): a key repeated with the
// same identity is not a replacement but an alternative after the first,
// so Bind(Do("timeline.open"), Do("timeline.enter")) on l keeps both, and
// the first whose action is available answers.
func (r *Runtime) Bind(rows ...Binding) error { return r.bindRows(rows, true, rowBatch{}) }

// bindRows is Bind; code is false for the user's file rows, which leave
// the source of the default they replace alone (bindings defaults shows
// that default again). batch spans the file or the call.
func (r *Runtime) bindRows(rows []Binding, code bool, batch rowBatch) error {
	for _, row := range rows {
		row, err := r.validateRow(row)
		if err != nil {
			return err
		}
		row = withImplicitGuard(row)
		if code {
			r.forgetDefaultSource(row)
		}
		switch {
		case row.Instance != "":
			inst := r.Instances[row.Instance]
			inst.Bindings = withPrefixCancels(bindRow(inst.Bindings, row, batch))
		default:
			noun := r.Nouns[row.Noun]
			noun.Bindings[row.Mode] = withPrefixCancels(bindRow(noun.Bindings[row.Mode], row, batch))
		}
		if r.rowTouchesPending(row) {
			r.CancelPendingBinding()
		}
	}
	return nil
}

// bindRow applies one validated row to a table: an unbind removes the row
// with its identity, any other row is placed by identity (placeRow).
func bindRow(rows []Binding, row Binding, batch rowBatch) []Binding {
	if row.Unbind {
		return removeRows(rows, row)
	}
	rows, _ = placeRowIn(rows, row, batch)
	return rows
}

// rowTouchesPending reports whether binding row changes what the key
// sequence in progress resolves to. A sequence prefix ("normal d") is
// touched by a row in its base mode (or any mode) whose keys equal the
// keys typed so far, extend them or are extended by them; a named
// pending mode holding a sequence (the terminal leader) by any row in
// that mode. Pattern rows never answer inside a sequence. A stale
// sequence (its focus or mode gone) counts as touched, so it is cleared.
func (r *Runtime) rowTouchesPending(row Binding) bool {
	if r.Pending == "" {
		return false
	}
	if !r.prefixStillPending() {
		return true
	}
	if row.Mode == r.Mode.Name {
		return true
	}
	base, sequence := baseAndPrefix(r.Mode)
	if base == r.Mode.Name || row.Keys == "" || row.Mode != "" && row.Mode != base {
		return false
	}
	return row.Keys == sequence || strings.HasPrefix(row.Keys, sequence+" ") || strings.HasPrefix(sequence, row.Keys+" ")
}

// ValidateBindings checks every row of every noun and instance table the
// way Bind checks a row it installs: keys or a pattern, a registered mode
// and guard, and a Request to a registered operation with known arguments
// and sources, or an unbind. Every row is therefore atomic. It is the check
// behind TestBindingRowsAreAtomic and is for an application to run after it
// loads its own rows.
func (r *Runtime) ValidateBindings() ([]string, error) {
	nouns := make([]string, 0, len(r.Nouns))
	for name := range r.Nouns {
		nouns = append(nouns, name)
	}
	sort.Strings(nouns)
	for _, name := range nouns {
		modes := make([]string, 0, len(r.Nouns[name].Bindings))
		for mode := range r.Nouns[name].Bindings {
			modes = append(modes, mode)
		}
		sort.Strings(modes)
		for _, mode := range modes {
			rows := r.Nouns[name].Bindings[mode]
			for i, row := range rows {
				normalized, err := r.validateRow(row)
				if err != nil {
					return nil, fmt.Errorf("noun %s mode %q row %s%s: %w", name, mode, row.Keys, row.Pattern, err)
				}
				rows[i].Request = normalized.Request
			}
		}
	}
	for _, inst := range r.instancesInOrder() {
		for i, row := range inst.Bindings {
			normalized, err := r.validateRow(row)
			if err != nil {
				return nil, fmt.Errorf("instance %s mode %q row %s%s: %w", inst.ID, row.Mode, row.Keys, row.Pattern, err)
			}
			inst.Bindings[i].Request = normalized.Request
		}
	}
	return append([]string(nil), r.deprecations...), nil
}

// modernRequest resolves a request's noun (an alias such as list names
// ochazuke.list: CanonicalNoun) and accepts the pre-noun.verb spelling for one
// release: a row built as Do("undo.input") parses as noun "undo", verb
// "input". When that two-word key is unknown but its reversal is
// registered, the request is rewritten and the move recorded once per key;
// ValidateBindings returns the list. A key of three or more words (a dotted
// noun: ochazuke.settings.set) is never swapped.
func (r *Runtime) modernRequest(q Request) Request {
	q = r.canonicalRequest(q)
	if _, ok := r.Operations[q.Key()]; ok || q.Verb == "" || strings.Contains(q.Noun, ".") {
		return q
	}
	reversed := r.canonicalRequest(Request{Verb: q.Noun, Noun: q.Verb, Target: q.Target, Args: q.Args})
	if _, ok := r.Operations[reversed.Key()]; !ok {
		return q
	}
	warning := fmt.Sprintf("operation %q is spelled noun.verb: use %q", q.Key(), r.DisplayKey(reversed.Key()))
	if !slices.Contains(r.deprecations, warning) {
		r.deprecations = append(r.deprecations, warning)
	}
	return reversed
}

// validateRow checks a row against the registry and normalizes it.
func (r *Runtime) validateRow(row Binding) (Binding, error) {
	row, err := r.resolveNamedRow(row)
	if err != nil {
		return row, err
	}
	if row.Keys == "" && row.Pattern == "" {
		return row, errors.New("binding needs keys or a pattern")
	}
	if row.Keys != "" && row.Pattern != "" {
		return row, errors.New("binding takes keys or a pattern, not both")
	}
	if row.Pattern != "" && !isPattern(row.Pattern) {
		return row, fmt.Errorf("unknown key pattern %q", row.Pattern)
	}
	if row.Mode != "" && !r.Modes[row.Mode] && !r.pendingModes[row.Mode] {
		return row, fmt.Errorf("unknown mode %q", row.Mode)
	}
	if row.When != "" && r.guard(row.When) == nil {
		return row, fmt.Errorf("unknown guard %q", row.When)
	}
	row.Aliases = nil
	if row.Request.Empty() {
		row.Unbind = true
	}
	if !row.Unbind && row.Request.IsProp() {
		// A prop row ({"request": {"noun", "prop", "value"|"toggle"}}):
		// its prop is checked, and it runs as the noun's get or set.
		row.Request.Noun = r.CanonicalNoun(row.Request.Noun)
		if err := r.validatePropRow(row); err != nil {
			return row, err
		}
	} else if !row.Unbind {
		row.Request = r.modernRequest(row.Request)
		op, ok := r.Operations[row.Request.Key()]
		if !ok {
			return row, fmt.Errorf("unknown operation %q", row.Request.Key())
		}
		// A row may leave a required argument out: the key opens the args
		// form for it (MissingOpensFormSetting), or, with the form off,
		// fails naming it as the API does. What a row gives is checked.
		if err := validatePartialArguments(op, row.Request.Args); err != nil {
			return row, err
		}
		for arg, source := range row.From {
			if _, ok := op.Parameters[arg]; !ok {
				return row, fmt.Errorf("%s has no argument %q", op.Key(), arg)
			}
			if len(strings.Fields(source)) == 0 {
				return row, fmt.Errorf("%s has an empty source", arg)
			}
			for _, one := range strings.Fields(source) {
				switch one {
				case SourceKey, SourceDigit, SourceText, SourceCount, SourceLine, SourceSteps, SourceOp, SourceOpCount, SourcePrefix, SourceLetter, SourceRow:
				default:
					if field, ok := rowSourceField(one); !ok || !validRowField(field) {
						return row, fmt.Errorf("unknown argument source %q", one)
					}
				}
			}
		}
	}
	row.Keys = strings.Join(strings.Fields(row.Keys), " ")
	if row.Instance != "" {
		if r.Instances[row.Instance] == nil {
			return row, fmt.Errorf("unknown instance %q", row.Instance)
		}
		row.Noun = ""
	} else if row.Noun = r.CanonicalNoun(row.Noun); r.Nouns[row.Noun] == nil {
		return row, fmt.Errorf("unknown noun %q", row.Noun)
	}
	return row, nil
}

// DefineMode registers a mode name. A pending mode is exclusive: only rows in
// its own table answer, and a row that leaves the mode unchanged returns the
// instance to the mode it shadowed.
func (r *Runtime) DefineMode(name string, pending bool, opts ...ModeOption) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("mode name cannot be empty")
	}
	if name == ModeDeclared {
		return fmt.Errorf("mode name %q is reserved (mode.set reads it as the focus's declared mode)", name)
	}
	var spec ModeSpec
	for _, opt := range opts {
		opt(&spec)
	}
	if spec.Extends != "" {
		if spec.Extends == name || !r.Modes[spec.Extends] && !r.pendingModes[spec.Extends] {
			return fmt.Errorf("mode %q extends unknown mode %q", name, spec.Extends)
		}
		defer func() { r.modeExtends[name] = spec.Extends }()
	}
	if pending {
		delete(r.Modes, name)
		r.pendingModes[name] = true
	} else {
		delete(r.pendingModes, name)
		r.Modes[name] = true
	}
	return nil
}

// DefinePassthrough marks a defined mode as a passthrough mode: while it is
// the base mode (a pending mode over it included) capture rows stand aside,
// so the focused level's rows answer every key. The terminal mode is one:
// ctrl+c reaches the pane instead of the app's captured quit.
func (r *Runtime) DefinePassthrough(name string) error {
	if !r.Modes[name] {
		return fmt.Errorf("unknown base mode %q", name)
	}
	r.passthroughModes[name] = true
	return nil
}

// ModeOption is a property DefineMode gives a mode, as a bindings file's
// "modes" entry does.
type ModeOption func(*ModeSpec)

// ModeExtends makes a mode fall back to parent's rows for every noun with
// no table of its own in the mode (no rows and no extends there): a mode
// that is "normal plus a few keys" (o-json's query, visual-line) binds
// only its own keys, and the nouns it does not mention keep their normal
// rows. A noun's own table (even an empty one, or one with its own
// extends) replaces the fallback for that noun.
func ModeExtends(parent string) ModeOption {
	return func(s *ModeSpec) { s.Extends = parent }
}

// ModeParent is the mode name falls back to for a noun with no table of
// its own (ModeExtends), "" for none.
func (r *Runtime) ModeParent(name string) string { return r.modeExtends[name] }

// ModeIs reports whether mode is base or extends it (ModeExtends, followed
// up the chain): a mode that extends insert behaves as insert where the
// framework asks (completion opens in it, an input groups its typing into
// one undo change and draws the bar cursor), so an application's own
// typing mode (a query line's JQ) keeps insert's behaviour under its own
// name and badge.
func (r *Runtime) ModeIs(mode, base string) bool {
	seen := map[string]bool{}
	for m := mode; m != "" && !seen[m]; m = r.modeExtends[m] {
		if m == base {
			return true
		}
		seen[m] = true
	}
	return false
}

// modeIs is ModeIs that works without a runtime (an input not yet added):
// then only mode itself is base.
func modeIs(r *Runtime, mode, base string) bool {
	if r == nil {
		return mode == base
	}
	return r.ModeIs(mode, base)
}

// nounTables is what mode reads on n: its own table, then the mode it
// extends on n (Noun.Extends), and so on; where n has no table of its own
// for a mode in the chain, the mode's fallback (ModeExtends) continues it.
func (r *Runtime) nounTables(n *Noun, mode string) [][]Binding {
	var out [][]Binding
	seen := map[string]bool{}
	for mode != "" && !seen[mode] {
		seen[mode] = true
		rows, own := n.Bindings[mode]
		out = append(out, rows)
		next := n.Extends[mode]
		if next == "" && !own {
			next = r.modeExtends[mode]
		}
		mode = next
	}
	return out
}

// Selection shapes: a mode that holds a selection says which shape it
// selects (DefineSelection, a bindings file's "modes" entry
// {"selection": "line"}). Code asks the shape, never a mode's name: the
// editor draws and edits by it, a list starts its row selection when a
// line-shaped mode is entered on it (list_visual.go).
const (
	SelectionChar  = "char"  // characters from the anchor to the cursor (visual)
	SelectionLine  = "line"  // whole lines or list rows (visual-line, V)
	SelectionBlock = "block" // a rectangle (visual-block)
)

// DefineSelection marks a defined base mode as a selection mode of shape
// (char, line, block, or a component's own shape such as the grid's row
// and column); "" makes it an ordinary mode again.
func (r *Runtime) DefineSelection(name, shape string) error {
	if !r.Modes[name] {
		return fmt.Errorf("unknown base mode %q", name)
	}
	if shape == "" {
		delete(r.selectionModes, name)
		return nil
	}
	r.selectionModes[name] = shape
	return nil
}

// SelectionShape is the selection shape of mode, "" when the mode holds
// no selection.
func (r *Runtime) SelectionShape(mode string) string {
	if r == nil {
		return builtinSelections()[mode]
	}
	return r.selectionModes[mode]
}

// SelectionMode is the mode that selects shape ("" when none does); of
// several, the first by name.
func (r *Runtime) SelectionMode(shape string) string {
	modes := r.selectionModes
	if r == nil {
		modes = builtinSelections()
	}
	for _, name := range slices.Sorted(maps.Keys(modes)) {
		if modes[name] == shape {
			return name
		}
	}
	return ""
}

// Guard is a named condition: an action's availability (Available, the
// verb guard) or, the older use kept while areas move, a condition a row
// requires (Binding.When; bindings check lists such rows as debts). The built-in guards are
// "input" and "no-input" (the focus is or is not an input), "multiline", "count" and "no-count" (the
// pending count is present or absent), "self" (the focus is the instance
// whose level the row sits on, not a descendant), "shadow-idle" (the mode a
// shadow mode such as macro_recording re-dispatches in is normal with an
// empty payload) and "op:<key>" (the pending operator). Applications add
// their own with DefineGuard.
type Guard func(*Runtime, KeyEvent) bool

func builtinGuards() map[string]Guard {
	return map[string]Guard{
		"input":     func(r *Runtime, ev KeyEvent) bool { return r.Input(ev.Focus) != nil },
		"no-input":  func(r *Runtime, ev KeyEvent) bool { return r.Input(ev.Focus) == nil },
		"multiline": func(r *Runtime, ev KeyEvent) bool { in := r.Input(ev.Focus); return in != nil && in.Multiline },
		"count":     func(r *Runtime, ev KeyEvent) bool { return ev.Mode.Payload.Count > 0 },
		"no-count":  func(r *Runtime, ev KeyEvent) bool { return ev.Mode.Payload.Count == 0 },
		"self":      func(r *Runtime, ev KeyEvent) bool { return ev.Focus == ev.Level },
		"shadow-idle": func(r *Runtime, ev KeyEvent) bool {
			shadow := ev.Mode.Payload.Shadow
			return shadow != nil && shadow.Name == "normal" && shadow.Payload.empty()
		},
	}
}

// DefineGuard registers a named row guard.
func (r *Runtime) DefineGuard(name string, fn Guard) { r.guards[name] = fn }

// guard resolves a guard name.
func (r *Runtime) guard(name string) Guard {
	if op, ok := strings.CutPrefix(name, "op:"); ok {
		return func(_ *Runtime, ev KeyEvent) bool { return ev.Mode.Payload.Op == op }
	}
	return r.guards[name]
}

// classOf reports the key classes a key belongs to, most specific first.
func classOf(key MacroKey) []string {
	if key.Paste {
		return []string{Paste}
	}
	runes := []rune(key.Key)
	var classes []string
	if len(runes) == 1 {
		switch {
		case unicode.IsDigit(runes[0]):
			classes = append(classes, Digit)
		case unicode.IsUpper(runes[0]):
			classes = append(classes, Upper, Alpha)
		case unicode.IsLetter(runes[0]):
			classes = append(classes, Alpha)
		}
	}
	if rest, ok := strings.CutPrefix(key.Key, "ctrl+"); ok && len(rest) == 1 && rest[0] >= 'a' && rest[0] <= 'z' {
		classes = append(classes, CtrlAlpha)
	}
	if printableKey(key.Key) {
		classes = append(classes, Printable)
	}
	return append(classes, Any)
}

// matchesPattern reports whether a key satisfies a pattern and how specific
// the pattern is (lower is tighter).
func matchesPattern(pattern string, key MacroKey) (bool, int) {
	if set, ok := strings.CutPrefix(pattern, "one-of:"); ok {
		return !key.Paste && slicesContains(strings.Fields(set), key.Key), 0
	}
	class, minus, _ := strings.Cut(pattern, "-")
	if minus != "" && !key.Paste && slicesContains(strings.Split(minus, "-"), key.Key) {
		return false, 0
	}
	for _, have := range classOf(key) {
		if have == class {
			switch class {
			case Upper:
				return true, 0
			case Digit, Alpha, CtrlAlpha:
				return true, 1
			case Printable, Paste:
				return true, 2
			default:
				return true, 3
			}
		}
	}
	return false, 0
}

func slicesContains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// keyTexts is the one key-name-to-text table: the named keys that stand for
// a character when a macro register is read as text. Every other key name
// is its own text. textKeys is the reverse for the control characters, read
// when a text register is replayed as keys.
var keyTexts = map[string]string{"esc": "\x1b", "enter": "\n", "tab": "\t", "space": " "}

// pasteKey is the key a paste reaches the walk as: its text in brackets
// (MacroKey.Paste set); pasteText reads the text back. The framing lives
// here with keyTexts, so no other file spells a bracket as a key.
func pasteKey(text string) string { return "[" + text + "]" }

func pasteText(key string) string {
	return strings.TrimSuffix(strings.TrimPrefix(key, "["), "]")
}

// keyText is the text a key name stands for in a register.
func keyText(name string) string {
	if text, ok := keyTexts[name]; ok {
		return text
	}
	return name
}

// textKey is the key name a register character replays as: control
// characters take their key name, printable text is its own key.
func textKey(ch rune) string {
	if unicode.IsControl(ch) {
		for name, text := range keyTexts {
			if text == string(ch) {
				return name
			}
		}
	}
	return string(ch)
}

// isBlackHole reports whether a register name is the black-hole register,
// which stores nothing and reads empty.
func isBlackHole(name string) bool { return name == "_" }

// leaderKey reports whether a key name can be a terminal leader: one
// ctrl+ or alt+ combination, so no text the pane needs is taken.
func leaderKey(key string) bool {
	if strings.ContainsAny(key, " \t") {
		return false
	}
	for _, mod := range []string{"ctrl+", "alt+"} {
		if rest, ok := strings.CutPrefix(key, mod); ok && rest != "" {
			return true
		}
	}
	return false
}

// keyLetter is the letter a key stands for (the letter source): one
// letter, lowercased, with a ctrl+ or shift+ modifier dropped; "" for any
// other key.
func keyLetter(key string) string {
	for _, mod := range []string{"ctrl+", "shift+"} {
		if rest, ok := strings.CutPrefix(key, mod); ok {
			key = rest
			break
		}
	}
	runes := []rune(key)
	if len(runes) != 1 || !unicode.IsLetter(runes[0]) {
		return ""
	}
	return string(unicode.ToLower(runes[0]))
}

// printableKey reports whether a key name is text the editor inserts as-is.
func printableKey(key string) bool {
	if key == "" {
		return false
	}
	if strings.HasPrefix(key, "ctrl+") || strings.HasPrefix(key, "alt+") || strings.HasPrefix(key, "shift+") {
		return false
	}
	switch key {
	case "esc", "enter", "tab", "shift+tab", "backspace", "delete", "insert", "home", "end", "pgup", "pgdown", "up", "down", "left", "right", "space":
		return false
	}
	if len(key) > 1 && key[0] == 'f' && strings.Trim(key[1:], "0123456789") == "" {
		return false
	}
	for _, ch := range key {
		if unicode.IsControl(ch) && ch != '\n' && ch != '\r' && ch != '\t' {
			return false
		}
	}
	return true
}
