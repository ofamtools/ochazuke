package ochazuke

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Noun is the behaviour shared by every instance of one kind: the verbs it
// answers to, its key bindings per mode, the action handlers that override an
// operation's default while the noun is on the focus stack, and an optional
// pinned mode (the prompt noun pins insert).
//
// Bindings is keyed by mode name. "" holds rows that apply in every base
// mode; a pending mode (operator_pending, a key-sequence prefix, ...) is
// exclusive and reads only its own table.
type Noun struct {
	Name     string               `json:"name"`
	Verbs    []string             `json:"verbs,omitempty"`
	Pin      string               `json:"pin,omitempty"`
	Bindings map[string][]Binding `json:"bindings"`
	Actions  map[string]Handler   `json:"-"`
}

// Binding is one row of a mode table, identical in code and config:
// {noun | instance, mode, keys | pattern, request | null, from, when}.
// Keys is an exact key or a space-separated sequence ("g g"); Pattern is a
// key class: digit, alpha, printable, paste, any, a class minus keys
// ("alpha-q"), or a set ("one-of:h j k l"). From fills request arguments from
// the key or the mode payload (see Sources). When names a registered guard.
// Unbind removes the default row with the same mode and keys or pattern.
// Replay dispatches the key again, fresh, after the row ran (a pending mode
// that hands an unknown key back to the stack). Func rows exist only where a row must re-dispatch (macro recording, leader).
type Binding struct {
	Noun     string                         `json:"noun,omitempty"`
	Instance string                         `json:"instance,omitempty"`
	Mode     string                         `json:"mode,omitempty"`
	Keys     string                         `json:"keys,omitempty"`
	Pattern  string                         `json:"pattern,omitempty"`
	Request  Request                        `json:"request"`
	From     map[string]string              `json:"from,omitempty"`
	When     string                         `json:"when,omitempty"`
	Unbind   bool                           `json:"unbind,omitempty"`
	Replay   bool                           `json:"replay,omitempty"`
	Func     func(*Runtime, KeyEvent) error `json:"-"`
}

// Sources a From entry may name: "key" (the key name), "digit" (the key as an
// integer), "text" (pasted or typed text), "count" (payload count, at least 1),
// "line" (payload count, 0 when absent, else the operator count), "steps"
// (count times operator count), "op" (payload operator), "opcount".
const (
	SourceKey     = "key"
	SourceDigit   = "digit"
	SourceText    = "text"
	SourceCount   = "count"
	SourceLine    = "line"
	SourceSteps   = "steps"
	SourceOp      = "op"
	SourceOpCount = "opcount"
)

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

// Do builds a row that runs "verb.noun" with optional arguments.
func Do(operation string, args ...Arguments) Binding {
	verb, noun, _ := strings.Cut(operation, ".")
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

// Enter builds a row that enters a pending mode with a typed payload.
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
	}
	return Do("enter.mode", args)
}

// Leave builds a row that returns from a pending mode to the mode it shadowed.
func Leave() Binding { return Do("leave.mode") }

// Func builds a row that runs Go code with the key event; use it only where a
// row must re-dispatch the key.
func Func(fn func(*Runtime, KeyEvent) error) Binding { return Binding{Func: fn} }

// FromKey fills an argument from the pressed key name.
func (b Binding) FromKey(arg string) Binding { return b.from(arg, SourceKey) }

// FromPayload fills an argument from the mode payload (a Source name).
func (b Binding) FromPayload(arg, source string) Binding { return b.from(arg, source) }

// Guard names the registered guard the row requires.
func (b Binding) Guard(name string) Binding { b.When = name; return b }

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
	r *Runtime
	n *Noun
}

func (b *NounBuilder) Noun() *Noun { return b.n }

// Pin keeps every instance of the noun in one base mode.
func (b *NounBuilder) Pin(mode string) *NounBuilder { b.n.Pin = mode; return b }

// Verbs records the verbs the noun answers to, for help and the palette.
func (b *NounBuilder) Verbs(verbs ...string) *NounBuilder {
	b.n.Verbs = append(b.n.Verbs, verbs...)
	return b
}

// Verb registers verb.noun as an operation when it is new, else installs the
// handler as this noun's override of the existing operation.
func (b *NounBuilder) Verb(verb string, h Handler, description ...string) *NounBuilder {
	key := verb + "." + b.n.Name
	if _, ok := b.r.Operations[key]; !ok {
		b.r.Register(Operation{Verb: verb, Noun: b.n.Name, Description: strings.Join(description, " "), Result: "JSON value", Run: h})
	} else {
		b.n.Actions[key] = h
	}
	b.Verbs(verb)
	return b
}

// Action overrides any operation ("verb.noun") while this noun is on the stack.
func (b *NounBuilder) Action(key string, h Handler) *NounBuilder { b.n.Actions[key] = h; return b }

// Mode selects the table rows are bound into; "" is the any-mode table.
func (b *NounBuilder) Mode(mode string) *ModeBuilder { return &ModeBuilder{NounBuilder: b, mode: mode} }

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
	m.n.Bindings[m.mode] = removeRows(m.n.Bindings[m.mode], probe)
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
	m.n.Bindings[m.mode] = append(rows, row)
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
		default:
			noun := r.Nouns[row.Noun]
			noun.Bindings[row.Mode] = removeRows(noun.Bindings[row.Mode], row)
			if !row.Unbind {
				noun.Bindings[row.Mode] = append(noun.Bindings[row.Mode], row)
			}
		}
		r.CancelPendingBinding()
	}
	return nil
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
	if row.Func == nil && row.Request.Verb == "" {
		row.Unbind = true
	}
	if !row.Unbind && row.Func == nil {
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
			switch source {
			case SourceKey, SourceDigit, SourceText, SourceCount, SourceLine, SourceSteps, SourceOp, SourceOpCount:
			default:
				return row, fmt.Errorf("unknown argument source %q", source)
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

// Guard is a named condition a row requires. The built-in guards are
// "input" (the focus is an input), "multiline", "count" and "no-count" (the
// pending count is present or absent), "self" (the focus is the instance
// whose level the row sits on, not a descendant) and "op:<key>" (the pending
// operator). Applications add their own with DefineGuard.
type Guard func(*Runtime, KeyEvent) bool

func builtinGuards() map[string]Guard {
	return map[string]Guard{
		"input":     func(r *Runtime, ev KeyEvent) bool { return r.Inputs[ev.Focus] != nil },
		"multiline": func(r *Runtime, ev KeyEvent) bool { in := r.Inputs[ev.Focus]; return in != nil && in.Multiline },
		"count":     func(r *Runtime, ev KeyEvent) bool { return ev.Mode.Payload.Count > 0 },
		"no-count":  func(r *Runtime, ev KeyEvent) bool { return ev.Mode.Payload.Count == 0 },
		"self":      func(r *Runtime, ev KeyEvent) bool { return ev.Focus == ev.Level },
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
