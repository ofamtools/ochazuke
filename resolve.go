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
)

// KeyEvent is what a row receives: the key, the mode it was resolved in and
// the focused instance.
type KeyEvent struct {
	Key   MacroKey
	Mode  ModeState
	Focus string
	Level string // the instance whose level is answering, "" for a noun-only level
}

// level is one step of the stack walk: an instance (nil for the modal level)
// and the noun whose tables it reads. A capture level reads only capture
// rows; a bubbling level reads only the others.
type level struct {
	inst    *Instance
	noun    *Noun
	capture bool
}

// walk is the full resolution order for a focus: the capture pass, the
// stack outer-first (app first, the editor fallback last) reading only
// capture rows, then the bubbling pass, the stack inner-first reading the
// rest. The first row that answers in either pass wins, so an app-level
// capture row (ctrl+c → app.quit) answers before an inner any row (a
// terminal's passthrough) can swallow the key.
func (r *Runtime) walk(focus string) []level {
	inner := r.stack(focus)
	var out, fallback []level
	for i := len(inner) - 1; i >= 0; i-- {
		lv := inner[i]
		lv.capture = true
		if lv.inst == nil && lv.noun != nil && lv.noun.Name == "editor" {
			fallback = append(fallback, lv)
			continue
		}
		out = append(out, lv)
	}
	return append(append(out, fallback...), inner...)
}

// Captured marks a row as a capture row: consulted outer-first before the
// inner-first walk (config field "capture").
func (b Binding) Captured() Binding { b.Capture = true; return b }

// stack is the resolution order for a focus: the focused instance outward to
// app, with the modal noun inserted before app while a modal is open, and
// the editor noun last as the framework default when an input (editor,
// prompt or palette field) is on the path. A list row, a page or a terminal
// focus ends at app: editor rows neither answer nor appear in bindings.list
// for it; counts, registers and macros are app rows and still do.
//
// The top modal is a resolution boundary (a z-index): while one is open
// and the walked focus is inside it (the live focus always is), the walk is the instances from the focus up to
// and including the modal instance, then the modal noun, then app. The
// levels beneath the modal (its Parent chain, kept for Pop and rendering)
// are never consulted.
func (r *Runtime) stack(focus string) []level {
	ancestry := r.Ancestry(focus)
	if top := r.TopModal(); top != "" {
		// The live focus is always inside the top modal (focusInTopLevel),
		// so the cut is at the modal instance and app closes the walk. A
		// focus named explicitly outside it (bindings.list for the row a
		// help modal describes) keeps its own full walk.
		if i := slices.Index(ancestry, top); i >= 0 {
			ancestry = append(ancestry[:i+1:i+1], "app")
		}
	}
	var out []level
	onStack := map[string]bool{}
	editing := false
	for _, id := range ancestry {
		onStack[id] = true
		if r.Input(id) != nil {
			editing = true
		}
	}
	for i, id := range ancestry {
		inst := r.Instances[id]
		if i == len(ancestry)-1 && len(r.modals) > 0 {
			frame := r.Instances[r.TopModal()]
			if frame != nil && onStack[frame.ID] {
				frame = nil
			}
			out = append(out, level{inst: frame, noun: r.Nouns["modal"]})
		}
		noun := r.Nouns[inst.Noun]
		if noun != nil && noun.Name == "editor" {
			noun = nil // the editor table is the shared fallback below
		}
		out = append(out, level{inst: inst, noun: noun})
	}
	if editor := r.Nouns["editor"]; editor != nil && editing {
		out = append(out, level{noun: editor})
	}
	return out
}

// Dispatch resolves one key through the stack walk and runs the first row
// that answers. It reports whether any row claimed the key. Live keys and
// macro playback both arrive here.
func (r *Runtime) Dispatch(key MacroKey) (bool, error) {
	if err := r.expirePrefix(); err != nil {
		return true, err
	}
	focus := r.Focus
	mode := r.Mode
	if r.Registers().Macro.Recording != "" && r.Registers().Macro.Depth == 0 {
		shadow := mode
		mode = ModeState{Name: "macro_recording", Payload: Payload{Shadow: &shadow}}
	}
	return r.dispatchIn(focus, mode, key)
}

// HandleBinding dispatches a named key; kept for callers that only know keys.
func (r *Runtime) HandleBinding(key string) (bool, error) { return r.Dispatch(MacroKey{Key: key}) }

// dispatchIn runs the walk for an explicit mode (the shadow of a recording).
func (r *Runtime) dispatchIn(focus string, mode ModeState, key MacroKey) (bool, error) {
	ev := KeyEvent{Key: key, Mode: mode, Focus: focus}
	base, prefix := baseAndPrefix(mode)
	sequence := key.Key
	if prefix != "" {
		sequence = prefix + " " + key.Key
	}
	var fallback *Request
	if prefix != "" {
		fallback = mode.Payload.Request
	}
	exclusive := r.pendingModes[mode.Name] || mode.Payload.Shadow != nil
	for _, lv := range r.walk(focus) {
		hit, longer, exact := r.lookup(lv, base, exclusive, prefix, sequence, key, ev)
		if longer {
			if exact != nil {
				fallback = &exact.Request
			} else if prefix == "" {
				fallback = nil
			}
			r.enterPrefix(base, sequence, fallback)
			return true, nil
		}
		if hit == nil {
			continue
		}
		if prefix != "" {
			r.leavePrefix()
		}
		if lv.inst != nil {
			ev.Level = lv.inst.ID
		}
		return true, r.runRow(lv, *hit, ev)
	}
	if prefix != "" {
		// An unmatched sequence hands its keys on: the completed match runs,
		// then the mismatched key is offered fresh to the whole stack. (The
		// cancel key is a row, "<prefix> esc" → mode.leave, kept by
		// withPrefixCancels in every table that binds a sequence.)
		r.leavePrefix()
		if fallback != nil {
			restore := r.Sourced("key:" + prefix)
			_, err := r.Execute(*fallback)
			restore()
			if err != nil {
				return true, err
			}
		}
		return r.Dispatch(key)
	}
	return false, nil
}

// baseAndPrefix splits a prefix mode name ("normal g g") into the table it
// reads and the sequence typed so far.
func baseAndPrefix(mode ModeState) (string, string) {
	if mode.Payload.Prefix != "" {
		if base, _, ok := strings.Cut(mode.Name, " "); ok {
			return base, mode.Payload.Prefix
		}
	}
	return mode.Name, ""
}

// lookup finds the row a level answers with. For a key sequence it reports
// whether longer sequences exist (so the walk waits) and the completed match.
func (r *Runtime) lookup(lv level, mode string, exclusive bool, prefix, sequence string, key MacroKey, ev KeyEvent) (hit *Binding, longer bool, exact *Binding) {
	var tables [][]Binding
	if lv.inst != nil {
		tables = append(tables, rowsForMode(lv.inst.Bindings, mode, exclusive))
	}
	if lv.noun != nil {
		tables = append(tables, lv.noun.tables(mode)...)
		if !exclusive {
			tables = append(tables, lv.noun.Bindings[""])
		}
	}
	if lv.inst != nil {
		ev.Level = lv.inst.ID
	} else {
		ev.Level = ""
	}
	type candidate struct {
		row  *Binding
		rank int
	}
	var candidates []candidate
	for _, rows := range tables {
		for i := range rows {
			row := &rows[i]
			if row.Unbind || row.Capture != lv.capture {
				continue
			}
			if row.Keys != "" && !key.Paste {
				if row.Keys == sequence {
					if exact == nil {
						exact = row
					}
					candidates = append(candidates, candidate{row, -1})
				} else if strings.HasPrefix(row.Keys, sequence+" ") {
					longer = true
				}
				continue
			}
			if row.Pattern != "" && prefix == "" {
				if ok, rank := matchesPattern(row.Pattern, key); ok {
					candidates = append(candidates, candidate{row, rank})
				}
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].rank < candidates[j].rank })
	for _, c := range candidates {
		if check := r.guard(c.row.When); c.row.When == "" || check != nil && check(r, ev) {
			hit = c.row
			break
		}
	}
	if hit != nil && hit.Keys == "" {
		exact = nil
	}
	return hit, longer, exact
}

// rowsForMode filters instance rows the way a noun table is read.
func rowsForMode(rows []Binding, mode string, exclusive bool) []Binding {
	var out []Binding
	for _, b := range rows {
		if b.Mode == mode || b.Mode == "" && !exclusive {
			out = append(out, b)
		}
	}
	return out
}

// stayingRequests are the operations that manage the pending mode
// themselves, or re-dispatch the key inside it (recording).
var stayingRequests = map[string]bool{"mode.enter": true, "count.append": true, "mode.leave": true, "input.cancel": true, "macro.record": true, "key.dispatch": true}

// runRow executes a row. A pending mode is one-shot: the key that answers it
// consumes it, so the mode returns to the shadowed mode before the request
// runs (the request may then enter another pending mode) unless the request
// itself manages modes.
func (r *Runtime) runRow(lv level, b Binding, ev KeyEvent) error {
	keys := b.Keys
	if keys == "" {
		keys = ev.Key.Key
	}
	defer r.Sourced("key:" + keys)()
	before, generation := r.Mode.Name, r.modeGeneration
	if r.pendingModes[before] && !stayingRequests[b.Request.Key()] {
		r.leaveMode()
	}
	q := b.Request
	if q.Target == "" && ev.Level != "" && ev.Level != ev.Focus && lv.noun != nil && lv.noun.Name == q.Noun {
		// A row addresses the instance of the level it matched at when the
		// operation is that level's own: d d → row.delete on the row noun
		// deletes that row and its confirm names it. Shared operations a
		// level binds (mode.set, input.*, focus.next) keep the focus.
		q.Target = ev.Level
	}
	if len(b.From) > 0 {
		args := Arguments{}
		for k, v := range q.Args {
			args[k] = v
		}
		for arg, source := range b.From {
			args[arg] = r.argument(source, ev)
		}
		q.Args = args
	}
	_, err := r.Execute(q)
	if r.pendingModes[before] && r.modeGeneration == generation && r.Mode.Name == before {
		r.leaveMode()
	}
	if err == nil && b.Replay && !ev.Key.Paste {
		_, err = r.Dispatch(ev.Key)
	}
	return err
}

// argument resolves a From source for a key event. Several sources
// separated by spaces resolve each and join their texts with a space.
func (r *Runtime) argument(source string, ev KeyEvent) any {
	if sources := strings.Fields(source); len(sources) > 1 {
		parts := make([]string, 0, len(sources))
		for _, one := range sources {
			if v := r.argument(one, ev); v != nil {
				parts = append(parts, fmt.Sprint(v))
			}
		}
		return strings.Join(parts, " ")
	}
	p := ev.Mode.Payload
	if p.Shadow != nil {
		p = p.Shadow.Payload
	}
	switch source {
	case SourceKey:
		return ev.Key.Key
	case SourceDigit:
		n, _ := strconv.Atoi(ev.Key.Key)
		return n
	case SourceText:
		if ev.Key.Paste {
			text := strings.TrimSuffix(strings.TrimPrefix(ev.Key.Key, "["), "]")
			return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
		}
		return ev.Key.Key
	case SourceCount:
		return max(1, p.Count)
	case SourceLine:
		if p.Count > 0 {
			return p.Count
		}
		return p.OpCount
	case SourceSteps:
		return max(1, p.Count) * max(1, p.OpCount)
	case SourceOp:
		return p.Op
	case SourceOpCount:
		return p.OpCount
	case SourcePrefix:
		return p.Prefix
	}
	return nil
}

// enterMode puts the runtime into a pending mode. The base mode it shadows
// is kept in the payload and restored by leave. A payload with a Prefix is a
// key sequence in progress, so entering the mode arms the sequence timer.
func (r *Runtime) enterMode(name string, payload Payload) {
	payload.Return = r.BaseMode()
	r.setMode(ModeState{Name: name, Payload: payload})
	if payload.Prefix != "" {
		r.armPrefix(r.Focus, name, payload.Prefix)
	}
}

// leaveMode restores the mode a pending mode shadowed and disarms the
// sequence timer when it belongs to that mode.
func (r *Runtime) leaveMode() {
	if r.prefix.mode == r.Mode.Name {
		r.Pending = ""
		r.bindingVersion++
		r.prefix = pendingPrefix{}
	}
	r.setMode(ModeState{Name: r.BaseMode()})
}

// pending reports whether the mode is a pending mode or a sequence prefix.
func (r *Runtime) pending() bool {
	return r.pendingModes[r.Mode.Name] || r.Mode.Payload.Prefix != ""
}

// ListBindings prints the rows the focus answers to in its current mode,
// one per key or pattern, sorted by key with pattern rows last.
func (r *Runtime) ListBindings(focus string) []Binding {
	out := r.bindingRows(focus)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Keys == "") != (b.Keys == "") {
			return a.Keys != ""
		}
		if a.Keys != b.Keys {
			return a.Keys < b.Keys
		}
		return a.Pattern < b.Pattern
	})
	return out
}

// bindingRows collects the rows the focus answers to in its current mode in
// resolution order: capture rows outer-first, then innermost level first, each level's instance rows before
// its noun's mode table before its any-mode table, the first row per key or
// pattern. A level whose unguarded any row answers every key ends the
// walk, as it does for Dispatch.
func (r *Runtime) bindingRows(focus string) []Binding {
	mode := r.Mode
	base, prefix := baseAndPrefix(mode)
	exclusive := r.pendingModes[mode.Name] || mode.Payload.Shadow != nil
	seen := map[string]bool{}
	var out []Binding
	var capture bool
	take := func(rows []Binding) {
		for _, b := range rows {
			if b.Unbind || b.Capture != capture {
				continue
			}
			id := b.Keys + "\x00" + b.Pattern
			if prefix != "" {
				if !strings.HasPrefix(b.Keys, prefix+" ") {
					continue
				}
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, b)
		}
	}
	for _, lv := range r.walk(focus) {
		capture = lv.capture
		before := len(out)
		if lv.inst != nil {
			take(rowsForMode(lv.inst.Bindings, base, exclusive))
		}
		if lv.noun != nil {
			for _, rows := range lv.noun.tables(base) {
				take(rows)
			}
			if !exclusive {
				take(lv.noun.Bindings[""])
			}
		}
		if answersEveryKey(out[before:]) {
			break
		}
	}
	return out
}

// answersEveryKey reports whether a level's rows include an unguarded any
// row, which answers every key before an outer level is consulted.
func answersEveryKey(rows []Binding) bool {
	for _, b := range rows {
		if b.Pattern == Any && b.When == "" {
			return true
		}
	}
	return false
}

// Key-sequence prefixes.

type pendingPrefix struct {
	focus     string
	mode      string
	deadline  time.Time
	scheduled bool
}

// enterPrefix waits for the rest of a key sequence as a pending mode named
// "<base> <keys so far>".
func (r *Runtime) enterPrefix(base, sequence string, fallback *Request) {
	r.enterMode(base+" "+sequence, Payload{Prefix: sequence, Request: fallback, Count: r.Mode.Payload.Count, OpCount: r.Mode.Payload.OpCount})
}

// armPrefix starts the sequence timer for a pending mode the focus is in:
// when BindingTimeout passes before the next key, finishPrefix leaves the
// mode and runs the request stored in its payload. The terminal leader
// enters leader_pending with the request that forwards the leader alone.
func (r *Runtime) armPrefix(focus, mode, sequence string) {
	timeout := r.BindingTimeout
	if timeout <= 0 {
		timeout = DefaultBindingTimeout
	}
	r.Pending = sequence
	r.bindingVersion++
	r.prefix = pendingPrefix{focus: focus, mode: mode, deadline: time.Now().Add(timeout)}
}

func (r *Runtime) leavePrefix() {
	if r.Mode.Payload.Prefix != "" {
		r.leaveMode()
	}
	r.Pending = ""
	r.bindingVersion++
	r.prefix = pendingPrefix{}
}

// prefixStillPending reports whether the runtime is still in the recorded
// prefix mode with the same focus.
func (r *Runtime) prefixStillPending() bool {
	if r.Pending == "" || r.prefix.focus == "" {
		return false
	}
	return r.Mode.Name == r.prefix.mode && r.Focus == r.prefix.focus
}

// expirePrefix resolves a sequence whose deadline passed before the next key.
func (r *Runtime) expirePrefix() error {
	if r.Pending == "" {
		return nil
	}
	if !r.prefixStillPending() {
		r.CancelPendingBinding()
		return nil
	}
	if time.Now().Before(r.prefix.deadline) {
		return nil
	}
	_, err := r.finishPrefix()
	return err
}

// finishPrefix leaves the prefix mode and runs the completed match, if any.
func (r *Runtime) finishPrefix() (bool, error) {
	fallback := r.Mode.Payload.Request
	defer r.Sourced("key:" + r.Pending)()
	r.leavePrefix()
	if fallback == nil {
		return false, nil
	}
	_, err := r.Execute(*fallback)
	return true, err
}

// Pending-mode and dispatch operations shared by every noun.

func registerModeTables(r *Runtime) {
	r.Register(Operation{Verb: "enter", Noun: "mode", Description: "Enter a pending mode with a typed payload (op, count, opcount; prefix arms the sequence timer, request is what it runs)", Parameters: map[string]Parameter{"mode": {Type: "string", Required: true}, "op": {Type: "string"}, "count": {Type: "integer"}, "opcount": {Type: "integer"}, "prefix": {Type: "string", Description: "Key sequence the mode holds; the timer runs request when no key follows"}, "request": {Type: "object", Description: "Request run when the sequence times out"}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		mode := StringArg(a, "mode", "")
		if !r.pendingModes[mode] {
			return nil, fmt.Errorf("unknown pending mode %q", mode)
		}
		request, err := requestArg(a, "request")
		if err != nil {
			return nil, err
		}
		r.enterMode(mode, Payload{Op: StringArg(a, "op", ""), Count: IntArg(a, "count", 0), OpCount: IntArg(a, "opcount", 0), Prefix: StringArg(a, "prefix", ""), Request: request})
		return r.Mode, nil
	}})
	r.Register(Operation{Verb: "dispatch", Noun: "key", Description: "Dispatch one key through the stack walk; mode \"shadow\" resolves it in the runtime's own mode beneath a macro recording, so the key is recorded once, and another mode name resolves it in that mode", Parameters: map[string]Parameter{"key": {Type: "string", Required: true, Description: "Key name as bindings spell it; for a paste, the bracketed text"}, "paste": {Type: "boolean", Description: "The key is pasted text"}, "mode": {Type: "string", Description: "Empty for the live walk, \"shadow\" or a mode name"}}, Result: "boolean", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		paste, _ := a["paste"].(bool)
		key := MacroKey{Key: StringArg(a, "key", ""), Paste: paste}
		focus := focusOf(c)
		switch mode := StringArg(a, "mode", ""); mode {
		case "":
			return r.Dispatch(key)
		case "shadow":
			return r.dispatchIn(focus, r.Mode, key)
		default:
			if !r.Modes[mode] && !r.pendingModes[mode] {
				return nil, fmt.Errorf("unknown mode %q", mode)
			}
			return r.dispatchIn(focus, ModeState{Name: mode, Payload: r.Mode.Payload}, key)
		}
	}})
	r.Register(Operation{Verb: "leave", Noun: "mode", Description: "Return from a pending mode to the mode it shadowed", Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if r.pending() {
			r.leaveMode()
		}
		return r.Mode, nil
	}})
	r.Register(Operation{Verb: "append", Noun: "count", Description: "Append a digit to the pending count", Parameters: map[string]Parameter{"digit": {Type: "integer", Required: true}}, Result: "count", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		digit := IntArg(a, "digit", 0)
		if digit < 0 || digit > 9 {
			return nil, errors.New("digit must be 0 to 9")
		}
		if r.Mode.Payload.Count > 100000 {
			return nil, errors.New("count exceeds 1000000")
		}
		r.Mode.Payload.Count = r.Mode.Payload.Count*10 + digit
		r.modeGeneration++
		return r.Mode.Payload.Count, nil
	}})
	r.Register(Operation{Verb: "run", Noun: "request", Description: "Execute the request stored in the mode payload", Result: "JSON value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if r.Mode.Payload.Request == nil {
			return nil, errors.New("no stored request")
		}
		q := *r.Mode.Payload.Request
		r.leaveMode()
		return r.ExecuteAt(c, q)
	}})
}

// requestArg reads a Request argument given as a Request (from a Go row) or
// as the JSON object a config row or the CLI sends.
func requestArg(a Arguments, name string) (*Request, error) {
	switch v := a[name].(type) {
	case nil:
		return nil, nil
	case Request:
		return &v, nil
	case *Request:
		return v, nil
	case map[string]any:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var q Request
		if err := json.Unmarshal(data, &q); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if q.Verb == "" || q.Noun == "" {
			return nil, fmt.Errorf("%s needs verb and noun", name)
		}
		return &q, nil
	}
	return nil, fmt.Errorf("%s must be a request", name)
}
