package ochazuke

import (
	"errors"
	"fmt"
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
// and the noun whose tables it reads.
type level struct {
	inst *Instance
	noun *Noun
}

// stack is the resolution order for a focus: the focused instance outward to
// app, with the modal noun inserted before app while a modal is open, and
// the editor noun last as the framework default when an input (editor,
// prompt or palette field) is on the path. A list row, a page or a terminal
// focus ends at app: editor rows neither answer nor appear in list.bindings
// for it; counts, registers and macros are app rows and still do.
func (r *Runtime) stack(focus string) []level {
	ancestry := r.Ancestry(focus)
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
		if i == len(ancestry)-1 && len(r.Frames) > 1 {
			frame := r.Instances[r.top().ID]
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
	focus := r.FocusID()
	mode := r.modeState(focus)
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
	for _, lv := range r.stack(focus) {
		hit, longer, exact := r.lookup(lv, base, exclusive, prefix, sequence, key, ev)
		if longer {
			if exact != nil {
				fallback = &exact.Request
			} else if prefix == "" {
				fallback = nil
			}
			r.enterPrefix(focus, base, sequence, fallback)
			return true, nil
		}
		if hit == nil {
			continue
		}
		if prefix != "" {
			r.leavePrefix(focus)
		}
		return true, r.runRow(lv, *hit, ev)
	}
	if prefix != "" {
		// An unmatched sequence hands its keys on: the completed match runs,
		// then the mismatched key is offered fresh to the whole stack. Escape
		// cancels the sequence without running anything.
		r.leavePrefix(focus)
		if key.Key == "esc" {
			return true, nil
		}
		if fallback != nil {
			if _, err := r.Execute(*fallback); err != nil {
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
		tables = append(tables, lv.noun.Bindings[mode])
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
			if row.Unbind {
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

// stayingRequests are the operations that manage the pending mode themselves.
var stayingRequests = map[string]bool{"enter.mode": true, "append.count": true, "leave.mode": true, "cancel.input": true}

// runRow executes a row. A pending mode is one-shot: the key that answers it
// consumes it, so the instance returns to the shadowed mode before the
// request runs (the request may then enter another pending mode) unless the
// request itself manages modes. Func rows manage modes themselves.
func (r *Runtime) runRow(lv level, b Binding, ev KeyEvent) error {
	inst := r.Instances[ev.Focus]
	before, generation := inst.State, r.modeGeneration
	var err error
	if b.Func != nil {
		err = b.Func(r, ev)
	} else {
		if r.pendingModes[before] && !stayingRequests[b.Request.Key()] {
			r.leaveMode(inst)
		}
		q := b.Request
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
		_, err = r.Execute(q)
	}
	if b.Func == nil && r.pendingModes[before] && r.modeGeneration == generation && r.Instances[ev.Focus] == inst && inst.State == before {
		r.leaveMode(inst)
	}
	if err == nil && b.Replay && !ev.Key.Paste {
		_, err = r.Dispatch(ev.Key)
	}
	return err
}

// argument resolves a From source for a key event.
func (r *Runtime) argument(source string, ev KeyEvent) any {
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
	}
	return nil
}

// enterMode puts the focused instance into a pending mode. The raw State it
// had (possibly "", inherited) is kept in the payload and restored by leave.
func (r *Runtime) enterMode(inst *Instance, name string, payload Payload) {
	if r.pendingModes[inst.State] || inst.Payload.Prefix != "" {
		payload.Return = inst.Payload.Return
	} else {
		payload.Return = inst.State
	}
	inst.State, inst.Payload = name, payload
	r.modeGeneration++
}

// leaveMode restores the mode a pending mode shadowed.
func (r *Runtime) leaveMode(inst *Instance) {
	inst.State, inst.Payload = inst.Payload.Return, Payload{}
	r.modeGeneration++
}

// ListBindings prints the rows the focus answers to in its current mode,
// one per key or pattern, innermost level first.
func (r *Runtime) ListBindings(focus string) []Binding {
	mode := r.modeState(focus)
	base, prefix := baseAndPrefix(mode)
	exclusive := r.pendingModes[mode.Name] || mode.Payload.Shadow != nil
	seen := map[string]bool{}
	var out []Binding
	take := func(rows []Binding) {
		for _, b := range rows {
			if b.Unbind {
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
	for _, lv := range r.stack(focus) {
		if lv.inst != nil {
			take(rowsForMode(lv.inst.Bindings, base, exclusive))
		}
		if lv.noun != nil {
			take(lv.noun.Bindings[base])
			if !exclusive {
				take(lv.noun.Bindings[""])
			}
		}
	}
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

// Key-sequence prefixes.

type pendingPrefix struct {
	focus     string
	mode      string
	deadline  time.Time
	scheduled bool
}

// enterPrefix waits for the rest of a key sequence as a pending mode named
// "<base> <keys so far>" on the focused instance.
func (r *Runtime) enterPrefix(focus, base, sequence string, fallback *Request) {
	inst := r.Instances[focus]
	name := base + " " + sequence
	r.enterMode(inst, name, Payload{Prefix: sequence, Request: fallback})
	timeout := r.BindingTimeout
	if timeout <= 0 {
		timeout = DefaultBindingTimeout
	}
	r.Pending = sequence
	r.bindingVersion++
	r.prefix = pendingPrefix{focus: focus, mode: name, deadline: time.Now().Add(timeout)}
}

func (r *Runtime) leavePrefix(focus string) {
	if inst := r.Instances[focus]; inst != nil && inst.Payload.Prefix != "" {
		r.leaveMode(inst)
	}
	r.Pending = ""
	r.bindingVersion++
	r.prefix = pendingPrefix{}
}

// prefixStillPending reports whether the recorded prefix instance is still
// in its prefix mode and focused.
func (r *Runtime) prefixStillPending() bool {
	if r.Pending == "" || r.prefix.focus == "" {
		return false
	}
	inst := r.Instances[r.prefix.focus]
	return inst != nil && inst.State == r.prefix.mode && r.FocusID() == r.prefix.focus
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
	inst := r.Instances[r.prefix.focus]
	fallback := inst.Payload.Request
	r.leavePrefix(r.prefix.focus)
	if fallback == nil {
		return false, nil
	}
	_, err := r.Execute(*fallback)
	return true, err
}

// Pending-mode operations shared by every noun.

func registerModeTables(r *Runtime) {
	r.Register(Operation{Verb: "enter", Noun: "mode", Description: "Enter a pending mode on the focused instance with a typed payload (op, count, opcount)", Parameters: map[string]Parameter{"mode": {Type: "string", Required: true}, "op": {Type: "string"}, "count": {Type: "integer"}, "opcount": {Type: "integer"}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		mode := StringArg(a, "mode", "")
		if !r.pendingModes[mode] {
			return nil, fmt.Errorf("unknown pending mode %q", mode)
		}
		inst := r.Instances[focusOf(c)]
		r.enterMode(inst, mode, Payload{Op: StringArg(a, "op", ""), Count: IntArg(a, "count", 0), OpCount: IntArg(a, "opcount", 0)})
		return r.modeState(inst.ID), nil
	}})
	r.Register(Operation{Verb: "leave", Noun: "mode", Description: "Return from a pending mode to the mode it shadowed", Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		inst := r.Instances[focusOf(c)]
		if r.pendingModes[inst.State] || inst.Payload.Prefix != "" {
			r.leaveMode(inst)
		}
		return r.modeState(inst.ID), nil
	}})
	r.Register(Operation{Verb: "append", Noun: "count", Description: "Append a digit to the pending count", Parameters: map[string]Parameter{"digit": {Type: "integer", Required: true}}, Result: "count", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		inst := r.Instances[focusOf(c)]
		digit := IntArg(a, "digit", 0)
		if digit < 0 || digit > 9 {
			return nil, errors.New("digit must be 0 to 9")
		}
		if inst.Payload.Count > 100000 {
			return nil, errors.New("count exceeds 1000000")
		}
		inst.Payload.Count = inst.Payload.Count*10 + digit
		r.modeGeneration++
		return inst.Payload.Count, nil
	}})
	r.Register(Operation{Verb: "run", Noun: "request", Description: "Execute the request stored in the focused instance's payload", Result: "JSON value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		inst := r.Instances[focusOf(c)]
		if inst.Payload.Request == nil {
			return nil, errors.New("no stored request")
		}
		q := *inst.Payload.Request
		r.leaveMode(inst)
		return r.ExecuteAt(c, q)
	}})
}
