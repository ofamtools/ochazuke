package core

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"ofam.dev/ochazuke/expr"
	"ofam.dev/ochazuke/trie"
)

// Mode is a named input mode.
type Mode struct {
	Name        string `json:"name"`
	Base        string `json:"base,omitempty"`        // parent mode ("visual" -> "normal")
	Passthrough bool   `json:"passthrough,omitempty"` // unmatched keys emit PassToWidget; skips the capture pass
}

// Layer is a transient key map pushed by a binding.
type Layer struct {
	Name        string   `json:"name"`                  // e.g. "leader", "resize"
	Modes       []string `json:"modes"`                 // searched in order, each expanding its Base chain
	ExcludeTags []string `json:"excludeTags,omitempty"` // skip commands carrying any of these tags
	Sticky      bool     `json:"sticky,omitempty"`      // false = pop after a match; true = stay until cancel
}

// Binding maps a key sequence or key class to a command or layer.
type Binding struct {
	Scope   string         `json:"scope"`             // matches ContextFrame.Scope ("" = any scope)
	Mode    string         `json:"mode"`              // matches Mode.Name ("" = any mode)
	Keys    []string       `json:"keys,omitempty"`    // exact sequence: ["g", "g"]
	Class   string         `json:"class,omitempty"`   // key class when Keys is empty
	When    string         `json:"when,omitempty"`    // expr over {mode, layer, scope, facts, frame, subject, modal, count}
	Phase   string         `json:"phase,omitempty"`   // "capture" (outer->inner) or "bubble" (default)
	Command string         `json:"command,omitempty"` // command ID to emit
	Args    map[string]any `json:"args,omitempty"`    // "$count", "$key", "$text", "$register", "$subject.id", "$facts.<k>"
	Layer   *Layer         `json:"layer,omitempty"`   // push a Layer instead of running a command
	Unbind  bool           `json:"unbind,omitempty"`  // suppress later/outer bindings on the same key or class
	Hint    string         `json:"hint,omitempty"`    // footer/which-key label
	Hidden  bool           `json:"hidden,omitempty"`  // omit from footer hints
}

// Binding phases.
const (
	PhaseCapture = "capture"
	PhaseBubble  = "bubble"
)

// Key classes, most specific first.
const (
	ClassDigit0    = "digit-0"   // "0"
	ClassDigit     = "digit"     // "0".."9"
	ClassAlpha     = "alpha"     // one letter
	ClassPrintable = "printable" // one printable rune or "space"
	ClassPaste     = "paste"     // PasteKey
	ClassAny       = "any"       // every key
)

var classOrder = []string{ClassDigit0, ClassDigit, ClassAlpha, ClassPrintable, ClassPaste, ClassAny}

// PasteKey is the key name under which paste events are resolved.
const PasteKey = "paste"

// DefaultCancelKey cancels pending input state.
const DefaultCancelKey = "esc"

// ClassMatches reports whether key belongs to class.
func ClassMatches(class, key string) bool {
	r, size := utf8.DecodeRuneInString(key)
	single := key != "" && size == len(key) && r != utf8.RuneError
	switch class {
	case ClassDigit0:
		return key == "0"
	case ClassDigit:
		return single && r >= '0' && r <= '9'
	case ClassAlpha:
		return single && unicode.IsLetter(r)
	case ClassPrintable:
		return key == "space" || single && unicode.IsPrint(r)
	case ClassPaste:
		return key == PasteKey
	case ClassAny:
		return true
	}
	return false
}

type rule struct {
	Binding
	when expr.Expr
}

// Keymap is an immutable compiled set of modes and bindings.
type Keymap struct {
	CancelKey string   // defaults to DefaultCancelKey
	Commands  Registry // command specs consulted for Layer.ExcludeTags

	modes   map[string]Mode
	byScope map[string][]*rule
}

// NewKeymap compiles modes and bindings. Commands holds DefaultRegistry plus
// specs. Bindings are kept in declaration order, which breaks priority ties.
func NewKeymap(modes []Mode, bindings []Binding, specs ...CommandSpec) (*Keymap, error) {
	km := &Keymap{CancelKey: DefaultCancelKey, Commands: DefaultRegistry(), modes: map[string]Mode{}, byScope: map[string][]*rule{}}
	km.Commands.Register(specs...)
	for _, m := range modes {
		km.modes[m.Name] = m
	}
	var errs []error
	for i, b := range bindings {
		r, err := compileBinding(b)
		if err != nil {
			errs = append(errs, fmt.Errorf("binding %d (%s %q): %w", i, b.Mode, strings.Join(b.Keys, " ")+b.Class, err))
			continue
		}
		km.byScope[b.Scope] = append(km.byScope[b.Scope], r)
	}
	return km, errors.Join(errs...)
}

func compileBinding(b Binding) (*rule, error) {
	switch {
	case len(b.Keys) > 0 && b.Class != "":
		return nil, errors.New("keys and class are exclusive")
	case len(b.Keys) == 0 && b.Class == "":
		return nil, errors.New("needs keys or class")
	case b.Class != "" && !slices.Contains(classOrder, b.Class):
		return nil, fmt.Errorf("unknown class %q", b.Class)
	case slices.Contains(b.Keys, ""):
		return nil, errors.New("empty key in sequence")
	case !b.Unbind && b.Command == "" && b.Layer == nil:
		return nil, errors.New("needs command, layer or unbind")
	case b.Phase != "" && b.Phase != PhaseCapture && b.Phase != PhaseBubble:
		return nil, fmt.Errorf("unknown phase %q", b.Phase)
	}
	w, err := expr.Parse(b.When)
	if err != nil {
		return nil, fmt.Errorf("when: %w", err)
	}
	b.Keys = slices.Clone(b.Keys)
	return &rule{Binding: b, when: w}, nil
}

// Mode returns the named mode.
func (km *Keymap) Mode(name string) (Mode, bool) {
	m, ok := km.modes[name]
	return m, ok
}

// ModeChain expands name through its Base chain: ["visual", "normal"].
func (km *Keymap) ModeChain(name string) []string {
	var out []string
	for name != "" && !slices.Contains(out, name) {
		out = append(out, name)
		name = km.modes[name].Base
	}
	return out
}

func (km *Keymap) cancelKey() string {
	if km.CancelKey == "" {
		return DefaultCancelKey
	}
	return km.CancelKey
}

// Outcome classifies a key resolution.
type Outcome uint8

// Resolution outcomes.
const (
	Unmatched   Outcome = iota // no binding; pending state cleared
	Matched                    // Command is set
	PushedLayer                // Command is a layer.push
	Pending                    // key extended the prefix; NextKeys lists continuations
	Cancelled                  // cancel key cleared pending state
	PassThrough                // passthrough mode: forward the key to the focused widget
)

func (o Outcome) String() string {
	return [...]string{"Unmatched", "Matched", "PushedLayer", "Pending", "Cancelled", "PassThrough"}[o]
}

// KeyQuery is the input to Resolve.
type KeyQuery struct {
	Chain []ContextFrame // innermost first
	Mode  string         // base mode of the active frame
	Input InputState
	Key   string
	Text  string // inserted text for printable keys and paste
	Modal string // ID of the top modal, "" when none
}

// Candidate is one binding considered for a key, recorded in KeyTrace.
type Candidate struct {
	Binding  Binding `json:"binding"`
	Level    string  `json:"level"` // scope of the chain level ("" = any-scope level)
	WhenOK   bool    `json:"whenOk"`
	Excluded bool    `json:"excluded,omitempty"` // command carries a Layer.ExcludeTags tag
}

// KeyTrace records how one key was resolved.
type KeyTrace struct {
	Key         string      `json:"key"`
	Prefix      []string    `json:"prefix,omitempty"`
	Modes       []string    `json:"modes,omitempty"`
	Layer       string      `json:"layer,omitempty"`
	Passthrough bool        `json:"passthrough,omitempty"`
	Candidates  []Candidate `json:"candidates,omitempty"` // bindings on this key, in priority order
	Match       string      `json:"match,omitempty"`      // trie match kind, or "class"
	Outcome     Outcome     `json:"outcome"`
	Winner      *Binding    `json:"winner,omitempty"`
	Note        string      `json:"note,omitempty"`
}

// Resolution is the result of Resolve.
type Resolution struct {
	Outcome  Outcome
	Command  *Command           // set for Matched and PushedLayer
	Replay   bool               // a held fallback fired; dispatch Key again afresh
	NextKeys map[string]Binding // continuations for Pending (zero Binding = deeper prefix)
	Input    InputState         // next input state
	Trace    KeyTrace
}

type cand struct {
	*rule
	frame ContextFrame
	facts map[string]any
	level string
}

// Resolve applies one key to the input state (spec section 3.3). It is pure.
func (km *Keymap) Resolve(q KeyQuery) Resolution {
	in := q.Input
	in.Prefix = slices.Clone(in.Prefix)
	res := Resolution{Trace: KeyTrace{Key: q.Key, Prefix: slices.Clone(in.Prefix)}}
	finish := func(o Outcome) Resolution {
		res.Outcome, res.Trace.Outcome, res.Input = o, o, in
		return res
	}

	if q.Key == km.cancelKey() && !in.Idle() {
		in.Prefix, in.Fallback, in.Layer, in.Count, in.Operator = nil, nil, nil, nil, nil
		return finish(Cancelled)
	}

	modes, exclude := km.activeModes(q.Mode, in.Layer)
	pass := in.Layer == nil && len(modes) > 0 && km.modes[modes[0]].Passthrough
	res.Trace.Modes, res.Trace.Passthrough = modes, pass
	if in.Layer != nil {
		res.Trace.Layer = in.Layer.Name
	}
	cands := km.collect(q, modes, exclude, pass, &res.Trace)

	var entries []trie.Entry[*cand]
	for _, c := range cands {
		if len(c.Keys) > 0 {
			entries = append(entries, trie.Entry[*cand]{Keys: c.Keys, Value: c})
		}
	}
	m := trie.Build(entries).Step(in.Prefix, q.Key)
	kind := m.Kind
	var hit *cand
	if m.Value != nil {
		if hit = *m.Value; hit.Unbind {
			res.Trace.Note, hit = "unbound", nil
			if kind == trie.Exact {
				kind = trie.None
			} else {
				kind = trie.Prefix // continuations stay reachable
			}
		}
	}
	res.Trace.Match = kind.String()
	seq := append(slices.Clone(in.Prefix), q.Key)

	switch kind {
	case trie.Exact:
		return km.fire(q, in, hit, seq, &res)
	case trie.Prefix, trie.Ambiguous:
		in.Fallback = nil
		if hit != nil {
			cmd := km.command(q, in, hit, seq)
			in.Fallback, res.Trace.Winner = &cmd, &hit.Binding
		}
		in.Prefix = seq
		in.Seq++
		res.NextKeys = map[string]Binding{}
		for k, v := range m.NextKeys {
			if v == nil {
				res.NextKeys[k] = Binding{}
			} else if !v.Unbind {
				res.NextKeys[k] = v.Binding
			}
		}
		return finish(Pending)
	}

	if len(in.Prefix) > 0 {
		fb := in.Fallback
		in.Prefix, in.Fallback = nil, nil
		in.popLayer()
		in.Count = nil
		if fb != nil {
			res.Command, res.Replay, res.Trace.Note = fb, true, "fallback"
			return finish(Matched)
		}
		res.Trace.Note = "broken prefix"
		return finish(Unmatched)
	}

	if c := classHit(cands, q.Key); c != nil {
		res.Trace.Match = "class"
		return km.fire(q, in, c, seq, &res)
	}
	in.Count = nil
	if pass {
		return finish(PassThrough)
	}
	in.popLayer()
	return finish(Unmatched)
}

func (km *Keymap) fire(q KeyQuery, in InputState, c *cand, seq []string, res *Resolution) Resolution {
	cmd := km.command(q, in, c, seq)
	in.Prefix, in.Fallback = nil, nil
	in.popLayer()
	out := PushedLayer
	if c.Layer == nil {
		out, in.Count = Matched, nil
	}
	res.Command, res.Trace.Winner = &cmd, &c.Binding
	res.Outcome, res.Trace.Outcome, res.Input = out, out, in
	return *res
}

func classHit(cands []*cand, key string) *cand {
	for _, cls := range classOrder {
		if !ClassMatches(cls, key) {
			continue
		}
		for _, c := range cands {
			if c.Class == cls {
				if c.Unbind {
					break
				}
				return c
			}
		}
	}
	return nil
}

func (km *Keymap) activeModes(base string, l *Layer) (modes, exclude []string) {
	if l == nil || len(l.Modes) == 0 {
		return km.ModeChain(base), nil
	}
	for _, name := range l.Modes {
		for _, m := range km.ModeChain(name) {
			if !slices.Contains(modes, m) {
				modes = append(modes, m)
			}
		}
	}
	return modes, l.ExcludeTags
}

type level struct {
	scope string
	frame ContextFrame
	facts map[string]any
}

// collect returns eligible bindings in priority order: capture pass
// (any-scope level, then outer to inner) unless pass, then bubble pass
// (inner to outer, then the any-scope level).
func (km *Keymap) collect(q KeyQuery, modes, exclude []string, pass bool, tr *KeyTrace) []*cand {
	n := len(q.Chain)
	merged := make([]map[string]any, n+1)
	for i := n - 1; i >= 0; i-- {
		merged[i] = mergeFacts(merged[i+1], q.Chain[i].Facts)
	}
	anyLevel := level{facts: merged[0]}
	if n > 0 {
		anyLevel.frame = q.Chain[0]
	}
	var bubble, capture []level
	for i, f := range q.Chain {
		bubble = append(bubble, level{scope: f.Scope, frame: f, facts: merged[i]})
	}
	capture = append([]level{anyLevel}, bubble...)
	slices.Reverse(capture[1:])
	bubble = append(bubble, anyLevel)

	var out []*cand
	walk := func(levels []level, capturePhase bool) {
		for _, lv := range levels {
			var env map[string]any
			var here []*cand
			for _, r := range km.byScope[lv.scope] {
				if (r.Phase == PhaseCapture) != capturePhase || modeRank(r.Mode, modes) < 0 {
					continue
				}
				if env == nil {
					env = whenEnv(q, lv)
				}
				c := Candidate{Binding: r.Binding, Level: lv.scope, WhenOK: r.when.Eval(env)}
				c.Excluded = r.Command != "" && km.Commands.HasAnyTag(r.Command, exclude)
				if relevant(r, q.Input.Prefix, q.Key) {
					tr.Candidates = append(tr.Candidates, c)
				}
				if c.WhenOK && !c.Excluded {
					here = append(here, &cand{rule: r, frame: lv.frame, facts: lv.facts, level: lv.scope})
				}
			}
			sort.SliceStable(here, func(i, j int) bool {
				ri, rj := modeRank(here[i].Mode, modes), modeRank(here[j].Mode, modes)
				if ri != rj {
					return ri < rj
				}
				return len(here[i].Keys) > 0 && len(here[j].Keys) == 0
			})
			out = append(out, here...)
		}
	}
	if !pass {
		walk(capture, true)
	}
	walk(bubble, false)
	return out
}

func modeRank(mode string, modes []string) int {
	if mode == "" {
		return len(modes)
	}
	return slices.Index(modes, mode)
}

func relevant(r *rule, prefix []string, key string) bool {
	if r.Class != "" {
		return len(prefix) == 0 && ClassMatches(r.Class, key)
	}
	return len(r.Keys) > len(prefix) && slices.Equal(r.Keys[:len(prefix)], prefix) && r.Keys[len(prefix)] == key
}

func mergeFacts(outer, inner map[string]any) map[string]any {
	if len(inner) == 0 {
		return outer
	}
	out := make(map[string]any, len(outer)+len(inner))
	for k, v := range outer {
		out[k] = v
	}
	for k, v := range inner {
		out[k] = v
	}
	return out
}

func whenEnv(q KeyQuery, lv level) map[string]any {
	env := map[string]any{
		"mode":  q.Mode,
		"scope": lv.frame.Scope,
		"facts": lv.facts,
		"frame": map[string]any{"scope": lv.frame.Scope, "nodeId": lv.frame.NodeID},
		"modal": map[string]any{"open": q.Modal != "", "id": q.Modal},
		"count": nil,
		"layer": nil,
	}
	if s := lv.frame.Subject; s != nil {
		env["subject"] = map[string]any{"kind": s.Kind, "id": s.ID}
	}
	if q.Input.Count != nil {
		env["count"] = *q.Input.Count
	}
	if q.Input.Layer != nil {
		env["layer"] = q.Input.Layer.Name
	}
	return env
}

func (km *Keymap) command(q KeyQuery, in InputState, c *cand, seq []string) Command {
	cmd := Command{ID: c.Command, Source: "key:" + strings.Join(seq, " "), Actor: Actor{Kind: "user"}}
	if c.Layer != nil {
		l := *c.Layer
		cmd.ID, cmd.Args = CmdLayerPush, map[string]any{"layer": l}
		return cmd
	}
	if s := c.frame.Subject; s != nil {
		t := *s
		cmd.Target = &t
	}
	cmd.Targets = slices.Clone(c.frame.Subjects)
	if in.Count != nil {
		cmd.Count = *in.Count
	}
	if c.Args != nil {
		cmd.Args = interpolate(c.Args, func(name string) (any, bool) {
			switch name {
			case "$count":
				return cmd.Times(), true
			case "$key":
				return q.Key, true
			case "$text":
				return q.Text, true
			case "$register":
				if in.Register == 0 {
					return "", true
				}
				return string(in.Register), true
			case "$subject.id", "$subject.kind":
				if cmd.Target == nil {
					return "", true
				}
				if name == "$subject.id" {
					return cmd.Target.ID, true
				}
				return cmd.Target.Kind, true
			}
			if k, ok := strings.CutPrefix(name, "$facts."); ok {
				return c.facts[k], true
			}
			return nil, false
		}).(map[string]any)
	}
	return cmd
}

// interpolate deep-copies v, replacing "$var" strings via lookup.
func interpolate(v any, lookup func(string) (any, bool)) any {
	switch t := v.(type) {
	case string:
		if strings.HasPrefix(t, "$") {
			if r, ok := lookup(t); ok {
				return r
			}
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = interpolate(e, lookup)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = interpolate(e, lookup)
		}
		return out
	}
	return v
}

// intArg converts a JSON-ish number or numeric string to int.
func intArg(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	case string:
		n, err := strconv.Atoi(t)
		return n, err == nil
	}
	return 0, false
}
