package ochazuke

import (
	"errors"
	"fmt"
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
	Actions   map[string]Handler   `json:"-"`
	// Columns are the table columns a list of the noun's rows draws
	// (NounBuilder.Columns, the catalog's columns): declared data a host
	// passes to Columns when it builds the list, never applied on its own.
	Columns []Column `json:"columns,omitempty"`
}

// tables is what one mode reads on the noun: its own rows, then the rows of
// the mode it extends, and so on up the chain (table inheritance: a row
// bound on the child wins, a row bound later on the parent is inherited).
func (n *Noun) tables(mode string) [][]Binding {
	var out [][]Binding
	seen := map[string]bool{}
	for mode != "" && !seen[mode] {
		seen[mode] = true
		out = append(out, n.Bindings[mode])
		mode = n.Extends[mode]
	}
	return out
}

// Binding is one row of a mode table, identical in code and config:
// {noun | instance, mode, keys | pattern, request | null, from, when}.
// Keys is an exact key or a space-separated sequence ("g g"); Pattern is a
// key class: digit, alpha, printable, paste, any, a class minus keys
// ("alpha-q"), or a set ("one-of:h j k l"). From fills request arguments from
// the key or the mode payload (see Sources). When names a registered guard.
// Unbind removes the default row with the same mode and keys or pattern.
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
	Noun     string            `json:"noun,omitempty"`
	Instance string            `json:"instance,omitempty"`
	Mode     string            `json:"mode,omitempty"`
	Keys     string            `json:"keys,omitempty"`
	Pattern  string            `json:"pattern,omitempty"`
	Request  Request           `json:"request"`
	From     map[string]string `json:"from,omitempty"`
	When     string            `json:"when,omitempty"`
	Unbind   bool              `json:"unbind,omitempty"`
	Replay   bool              `json:"replay,omitempty"`
	Capture  bool              `json:"capture,omitempty"`
	// Hint replaces "noun.verb: description" in the footer and the help
	// for a row whose operation says nothing useful on its own (the
	// terminal leader's mode.enter); bindings list carries it too.
	Hint string `json:"hint,omitempty"`
}

// Sources a From entry may name: "key" (the key name), "digit" (the key as an
// integer), "text" (pasted or typed text), "count" (payload count, at least 1),
// "line" (payload count, 0 when absent, else the operator count), "steps"
// (count times operator count), "op" (payload operator), "opcount", "prefix"
// (the key sequence the pending mode holds). A From value may name several
// sources separated by spaces ("prefix key"): their texts are joined with a
// space, the way terminal.send reads a key list.
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
	Paste     = "paste"
	Any       = "any"
)

// same reports whether two rows address the same key or pattern.
func (b Binding) same(o Binding) bool { return b.Keys == o.Keys && b.Pattern == o.Pattern }

// Do builds a row that runs "noun.verb" with optional arguments.
func Do(operation string, args ...Arguments) Binding {
	noun, verb, _ := strings.Cut(operation, ".")
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
	return Do("mode.enter", args)
}

// Leave builds a row that returns from a pending mode to the mode it shadowed.
func Leave() Binding { return Do("mode.leave") }

// FromKey fills an argument from the pressed key name.
func (b Binding) FromKey(arg string) Binding { return b.from(arg, SourceKey) }

// FromPayload fills an argument from the mode payload (a Source name).
func (b Binding) FromPayload(arg, source string) Binding { return b.from(arg, source) }

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

func (b Binding) from(arg, source string) Binding {
	from := map[string]string{}
	for k, v := range b.From {
		from[k] = v
	}
	from[arg] = source
	b.From = from
	return b
}

// Define declares a noun, or returns the builder of an existing one.
func (r *Runtime) Define(name string) *NounBuilder {
	if name == "" {
		panic("noun name must be nonempty")
	}
	n := r.Nouns[name]
	if n == nil {
		n = &Noun{Name: name, Bindings: map[string][]Binding{}, Actions: map[string]Handler{}}
		r.Nouns[name] = n
	}
	return &NounBuilder{r: r, n: n}
}

// Noun returns the registered noun of that name, or nil.
func (r *Runtime) Noun(name string) *Noun { return r.Nouns[name] }

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
	if _, ok := b.r.Operations[key]; !ok {
		b.r.Register(Operation{Verb: verb, Noun: b.n.Name, Description: strings.Join(description, " "), Result: "JSON value", Run: run, App: app})
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
	_, verb, _ := strings.Cut(b.last, ".")
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
	noun, verb, _ := strings.Cut(key, ".")
	if n := r.Nouns[noun]; n != nil {
		return n.Guards[verb]
	}
	return ""
}

// VerbShown reports whether noun.verb is offered at a focus in a mode: true
// for an unguarded verb or one whose guard holds there. A guard name that
// is not registered hides nothing.
func (r *Runtime) VerbShown(key, focus string, mode ModeState) bool {
	name := r.VerbGuard(key)
	if name == "" {
		return true
	}
	guard := r.guard(name)
	return guard == nil || guard(r, KeyEvent{Mode: mode, Focus: focus, Level: focus})
}

// Action overrides any operation ("noun.verb") while this noun is on the stack.
func (b *NounBuilder) Action(key string, h Handler) *NounBuilder { b.n.Actions[key] = h; return b }

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
	m.r.CancelPendingBinding()
	return m
}

// ModeBuilder binds rows into one mode table of a noun.
type ModeBuilder struct {
	*NounBuilder
	mode string
	last int
}

// Bind adds an exact-key (or key-sequence) row, replacing an unguarded row
// with the same keys.
func (m *ModeBuilder) Bind(keys string, row Binding) *ModeBuilder {
	row.Keys, row.Pattern = strings.Join(strings.Fields(keys), " "), ""
	return m.add(row)
}

// Pattern adds a key-class row.
func (m *ModeBuilder) Pattern(pattern string, row Binding) *ModeBuilder {
	row.Keys, row.Pattern = "", pattern
	return m.add(row)
}

// Unbind removes the rows with these keys or this pattern.
func (m *ModeBuilder) Unbind(keysOrPattern string) *ModeBuilder {
	probe := Binding{Keys: strings.Join(strings.Fields(keysOrPattern), " ")}
	if isPattern(keysOrPattern) {
		probe = Binding{Pattern: keysOrPattern}
	}
	m.n.Bindings[m.mode] = withPrefixCancels(removeRows(m.n.Bindings[m.mode], probe))
	return m
}

// When guards the last bound row with a registered guard name.
func (m *ModeBuilder) When(guard string) *ModeBuilder {
	rows := m.n.Bindings[m.mode]
	if m.last < len(rows) {
		rows[m.last].When = guard
	}
	return m
}

func (m *ModeBuilder) add(row Binding) *ModeBuilder {
	row.Noun, row.Instance, row.Mode = m.n.Name, "", m.mode
	row.Request = m.r.modernRequest(row.Request)
	rows := m.n.Bindings[m.mode]
	if row.When == "" {
		for i, existing := range rows {
			if existing.same(row) && existing.When == "" {
				rows[i] = row
				m.n.Bindings[m.mode] = rows
				m.last = i
				return m
			}
		}
	}
	m.n.Bindings[m.mode] = withPrefixCancels(append(rows, row))
	m.last = len(rows)
	m.r.CancelPendingBinding()
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
		bound[prefixKey{b.Mode, b.Keys}] = true
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
	return strings.HasSuffix(b.Keys, " "+CancelKey) && b.Request.Key() == "mode.leave" && len(b.From) == 0 && b.When == ""
}

// isPattern reports whether a config "keys" value names a key class.
func isPattern(s string) bool {
	class, _, _ := strings.Cut(s, "-")
	switch class {
	case Digit, Alpha, Printable, Paste, Any:
		return true
	}
	return strings.HasPrefix(s, "one-of:")
}

// Bind installs rows from config or code onto their noun or instance tables:
// the same mode and keys or pattern replaces, a new one adds, Unbind removes.
func (r *Runtime) Bind(rows ...Binding) error {
	for _, row := range rows {
		row, err := r.validateRow(row)
		if err != nil {
			return err
		}
		switch {
		case row.Instance != "":
			inst := r.Instances[row.Instance]
			inst.Bindings = removeRows(inst.Bindings, row)
			if !row.Unbind {
				inst.Bindings = append(inst.Bindings, row)
			}
			inst.Bindings = withPrefixCancels(inst.Bindings)
		default:
			noun := r.Nouns[row.Noun]
			noun.Bindings[row.Mode] = removeRows(noun.Bindings[row.Mode], row)
			if !row.Unbind {
				noun.Bindings[row.Mode] = append(noun.Bindings[row.Mode], row)
			}
			noun.Bindings[row.Mode] = withPrefixCancels(noun.Bindings[row.Mode])
		}
		r.CancelPendingBinding()
	}
	return nil
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

// modernRequest accepts the pre-noun.verb spelling for one release: a row
// built as Do("undo.input") parses as noun "undo", verb "input". When that
// key is unknown but its reversal is registered, the request is rewritten
// and the move recorded once per key; ValidateBindings returns the list.
func (r *Runtime) modernRequest(q Request) Request {
	if _, ok := r.Operations[q.Key()]; ok {
		return q
	}
	reversed := Request{Verb: q.Noun, Noun: q.Verb, Target: q.Target, Args: q.Args}
	if _, ok := r.Operations[reversed.Key()]; !ok {
		return q
	}
	warning := fmt.Sprintf("operation %q is spelled noun.verb: use %q", q.Key(), reversed.Key())
	if !slices.Contains(r.deprecations, warning) {
		r.deprecations = append(r.deprecations, warning)
	}
	return reversed
}

// validateRow checks a row against the registry and normalizes it.
func (r *Runtime) validateRow(row Binding) (Binding, error) {
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
	if row.Request.Verb == "" {
		row.Unbind = true
	}
	if !row.Unbind {
		row.Request = r.modernRequest(row.Request)
		op, ok := r.Operations[row.Request.Key()]
		if !ok {
			return row, fmt.Errorf("unknown operation %q", row.Request.Key())
		}
		for name, param := range op.Parameters {
			if _, ok := row.Request.Args[name]; param.Required && !ok && row.From[name] == "" {
				return row, fmt.Errorf("%s is required", name)
			}
		}
		optional := map[string]Parameter{}
		for name, param := range op.Parameters {
			param.Required = false
			optional[name] = param
		}
		if err := validateArguments(Operation{Parameters: optional}, row.Request.Args); err != nil {
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
				case SourceKey, SourceDigit, SourceText, SourceCount, SourceLine, SourceSteps, SourceOp, SourceOpCount, SourcePrefix:
				default:
					return row, fmt.Errorf("unknown argument source %q", one)
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
	} else if r.Nouns[row.Noun] == nil {
		return row, fmt.Errorf("unknown noun %q", row.Noun)
	}
	return row, nil
}

// DefineMode registers a mode name. A pending mode is exclusive: only rows in
// its own table answer, and a row that leaves the mode unchanged returns the
// instance to the mode it shadowed.
func (r *Runtime) DefineMode(name string, pending bool) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("mode name cannot be empty")
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

// Guard is a named condition a row requires. The built-in guards are
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
		case unicode.IsLetter(runes[0]):
			classes = append(classes, Alpha)
		}
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
			case Digit, Alpha:
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
