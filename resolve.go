package ochazuke

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	// Args are the arguments of the request whose availability is judged
	// (Available): a row's own, or a call's; nil for a row's own guard.
	Args Arguments
}

// level is one step of the stack walk: an instance (nil for the modal level)
// and the noun whose tables it reads. A capture level reads only capture
// rows; a bubbling level reads only the others.
type level struct {
	inst    *Instance
	noun    *Noun
	capture bool
	// owns marks the editor level of a focused read-only input: it answers
	// right after the input, and a key one of its rows binds in the mode
	// is the input's even where no row answers (an edit the input refuses,
	// a row guarded off), consumed rather than passed to the page.
	owns bool
}

// name is the noun a level answers as: its noun table's, else its
// instance's.
func (lv level) name() string {
	if lv.noun != nil {
		return lv.noun.Name
	}
	if lv.inst != nil {
		return lv.inst.Noun
	}
	return ""
}

// walk is the full resolution order for a focus: the capture pass, the
// stack outer-first (app first, the editor fallback last) reading only
// capture rows, then the bubbling pass, the stack inner-first reading the
// rest. The first row that answers in either pass wins, so an app-level
// capture row (ctrl+c → app.quit) answers before an inner any row (a
// terminal's passthrough) can swallow the key. While the base mode is a
// passthrough mode (DefinePassthrough: the terminal) there is no capture
// pass, so the mode's rows see every key, ctrl+c included.
func (r *Runtime) walk(focus string) []level {
	return r.walkOf(r.stack(focus), r.passthroughModes[r.BaseMode()])
}

// walkOf is walk over a given stack: the capture pass (skipped in a
// passthrough mode) outer-first with the editor fallback last, then the
// stack itself. The live walk and the hypothetical one (nounWalk) share it.
func (r *Runtime) walkOf(inner []level, passthrough bool) []level {
	if passthrough {
		return inner
	}
	var out, fallback []level
	for i := len(inner) - 1; i >= 0; i-- {
		lv := inner[i]
		lv.capture = true
		if lv.inst == nil && lv.noun != nil && lv.noun.Name == "ochazuke.editor" {
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

// stack is the resolution order for a focus: the focused instance's inner
// level when it has one (an input's open suggestions), the focused instance
// outward to app, with the modal noun inserted before app while a modal is open, and
// the editor noun last as the framework default when an input (editor,
// prompt or palette field) is on the path. A list row, a page or a terminal
// focus ends at app: editor rows neither answer nor appear in bindings.list
// for it; counts, registers and macros are app rows and still do.
//
// A focused read-only input (Input.ReadOnly) is a viewer whose keys are
// the editor's: the editor level answers right after the input's own
// (before the panel, the page, app), so its motions and selections win
// over the page's rows in every mode the input is in (visual-line's
// inherited normal rows included), and a key the editor binds in the mode
// that does not run there (an edit, refused while read-only) is consumed
// at that level (level.owns) instead of reaching a page row on the same
// key. Keys the editor does not bind still reach the page.
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
		if i == 0 && inst.inner != "" {
			// An inner level holds no focus: the focused input's open
			// suggestions answer before the input, typing falls through.
			if lay := r.Instances[inst.inner]; lay != nil {
				out = append(out, level{inst: lay, noun: r.Nouns[lay.Noun]})
			}
		}
		if i == len(ancestry)-1 && len(r.modals) > 0 {
			frame := r.Instances[r.TopModal()]
			if frame != nil && onStack[frame.ID] {
				frame = nil
			}
			out = append(out, level{inst: frame, noun: r.Nouns["ochazuke.modal"]})
		}
		noun := r.Nouns[inst.Noun]
		if noun != nil && noun.Name == "ochazuke.editor" {
			noun = nil // the editor table is the shared fallback below
		}
		out = append(out, level{inst: inst, noun: noun})
		if i == 0 && editing {
			if in := r.Input(id); in != nil && in.ReadOnly {
				if editor := r.Nouns["ochazuke.editor"]; editor != nil {
					out = append(out, level{noun: editor, owns: true})
					editing = false // placed: no fallback below
				}
			}
		}
	}
	if editor := r.Nouns["ochazuke.editor"]; editor != nil && editing {
		out = append(out, level{noun: editor})
	}
	return out
}

// KeyOutcome is what resolving one key came to (DispatchOutcome).
type KeyOutcome int

const (
	// KeyUnhandled: no level had a row on the key whose action is
	// available, so it went past app.
	KeyUnhandled KeyOutcome = iota
	// KeyHandled: a row ran, or the key started or continued a sequence.
	KeyHandled
	// KeyConsumed: the row that answered runs the noop (NoopAction), the
	// always-available action a clamping container binds a key to so it
	// goes no further out: handled, and nothing happened.
	KeyConsumed
)

// NoopAction is the always-available action that does nothing: a row bound
// to it consumes its key (KeyConsumed), which is how a container clamps a
// key its children leave unhandled at an edge.
const NoopAction = "ochazuke.key.ignore"

// Dispatch resolves one key through the stack walk and runs the first row
// that answers. It reports whether any row claimed the key. Live keys and
// macro playback both arrive here.
func (r *Runtime) Dispatch(key MacroKey) (bool, error) {
	outcome, err := r.DispatchOutcome(key)
	return outcome != KeyUnhandled, err
}

// DispatchOutcome is Dispatch reporting which of the three outcomes the
// key had: handled, unhandled (no level had an available row on it), or
// consumed by a noop row.
func (r *Runtime) DispatchOutcome(key MacroKey) (KeyOutcome, error) {
	if err := r.expirePrefix(); err != nil {
		return KeyHandled, err
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
func (r *Runtime) dispatchIn(focus string, mode ModeState, key MacroKey) (KeyOutcome, error) {
	ev := KeyEvent{Key: key, Mode: mode, Focus: focus}
	base, prefix := baseAndPrefix(mode)
	sequence := key.Key
	if prefix != "" {
		sequence = prefix + " " + key.Key
	}
	// held is the sequence the pending mode holds, whatever made it one: a
	// prefix mode ("normal g") or a named pending mode entered with a
	// prefix (leader_pending); its request is the shorter match.
	held := mode.Payload.Prefix
	var fallback *Request
	if held != "" {
		fallback = mode.Payload.Request
	}
	exclusive := r.pendingModes[mode.Name] || mode.Payload.Shadow != nil
	wait := func(exact *Binding) (KeyOutcome, error) {
		if exact != nil {
			fallback = &exact.Request
		} else if prefix == "" {
			fallback = nil
		}
		r.enterPrefix(base, sequence, fallback)
		return KeyHandled, nil
	}
	run := func(lv level, hit *Binding) (KeyOutcome, error) {
		if prefix != "" {
			r.leavePrefix()
		}
		if lv.inst != nil {
			ev.Level = lv.inst.ID
		}
		outcome := KeyHandled
		if hit.Request.Key() == NoopAction {
			outcome = KeyConsumed
		}
		return outcome, r.runRow(lv, *hit, ev)
	}
	drop := r.dropsInheritedSequences()
	levels := r.walk(focus)
	bound := false // a row on the sequence typed exists but does not answer here
	// The capture pass: outer-first, the first level that answers wins.
	for len(levels) > 0 && levels[0].capture {
		lv := levels[0]
		levels = levels[1:]
		hit, look := r.lookup(lv, base, exclusive, prefix, sequence, key, ev)
		bound = bound || look.bound
		if look.waits(look.ownExact, drop) {
			return wait(look.exact)
		}
		if hit != nil {
			return run(lv, hit)
		}
	}
	// The bubbling pass scans the whole stack per key (Vim typeahead): a
	// sequence extending the keys at any level, inner or outer, waits, with
	// the innermost exact match as the fallback on timeout or mismatch;
	// otherwise the innermost hit runs. A level answering with a key class
	// (a terminal's any) ends the scan: outer sequences never steal its keys.
	var (
		hitLv      level
		hit, exact *Binding
		looks      []lookResult
		ownExact   bool
	)
	for _, lv := range levels {
		h, look := r.lookup(lv, base, exclusive, prefix, sequence, key, ev)
		looks = append(looks, look)
		bound = bound || look.bound
		ownExact = ownExact || look.ownExact
		if exact == nil && look.exact != nil {
			exact = look.exact
		}
		if hit == nil && h != nil {
			hit, hitLv = h, lv
			if h.Keys == "" {
				break
			}
		}
	}
	// A sequence a level inherits from the mode its mode extends (a row
	// noun's normal d d read in visual-line) does not wait over a row the
	// mode binds on its own tables for the keys typed (the list's
	// visual-line d), at any level: the child mode's key answers at once
	// (sequence-inherited drop, the default; keep lets it wait).
	for _, look := range looks {
		if look.waits(ownExact, drop) {
			return wait(exact)
		}
	}
	if hit != nil {
		return run(hitLv, hit)
	}
	if held != "" && bound {
		// The key completes a bound sequence, but no row on it answers
		// here (its action unavailable, its guard false): the sequence
		// and the key are consumed together, never handed on as the key
		// typed alone (g v where values does not apply never runs v).
		r.leavePrefix()
		return KeyConsumed, nil
	}
	if held != "" {
		// A key no row continues the sequence with: sequence-unknown
		// decides. pass hands the keys on: the completed match runs, then
		// the key is offered fresh to the whole stack, as if typed alone.
		// swallow drops both with a notice. (The cancel key is a row,
		// "<prefix> esc" → mode.leave, kept by withPrefixCancels in every
		// table that binds a sequence.)
		r.leavePrefix()
		if r.swallowsUnknown() {
			r.UI().Info(held + " " + key.Key + " is not bound")
			return KeyConsumed, nil
		}
		if fallback != nil {
			restore := r.Sourced("key:" + held)
			_, err := r.Execute(*fallback)
			restore()
			if err != nil {
				return KeyHandled, err
			}
		}
		return r.DispatchOutcome(key)
	}
	return KeyUnhandled, nil
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

// lookResult is what lookup reports beside the row a level answers with:
// whether longer sequences extend the keys typed in the mode's own tables
// (longer) or only in a table the mode inherits (inherited: the tables of
// the mode it extends, Noun.Extends or ModeExtends), the completed match
// (exact), and whether a row of an own table completes the keys typed with
// its guard holding (ownExact).
type lookResult struct {
	longer, inherited, ownExact bool
	exact                       *Binding
	// bound: a row on exactly the sequence typed (two keys or more) that
	// does not answer here, its action unavailable or its guard false.
	bound bool
}

// waits reports whether the walk waits on this level's sequences: an own
// longer sequence always does; an inherited one does unless drop is set
// and a row of the mode's own tables (ownExact, at any level) completes
// the keys typed.
func (l lookResult) waits(ownExact, drop bool) bool {
	return l.longer || l.inherited && !(drop && ownExact)
}

// lookup finds the row a level answers with. For a key sequence it reports
// whether longer sequences exist (so the walk waits) and the completed match.
func (r *Runtime) lookup(lv level, mode string, exclusive bool, prefix, sequence string, key MacroKey, ev KeyEvent) (hit *Binding, look lookResult) {
	var tables [][]Binding
	var inherited []bool
	if lv.inst != nil {
		tables = append(tables, rowsForMode(lv.inst.Bindings, mode, exclusive))
		inherited = append(inherited, false)
	}
	if lv.noun != nil {
		for i, rows := range r.nounTables(lv.noun, mode) {
			tables = append(tables, rows)
			inherited = append(inherited, i > 0)
		}
		if !exclusive {
			tables = append(tables, lv.noun.Bindings[""])
			inherited = append(inherited, false)
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
	// owned is a row of an owning level (level.owns) on the key typed,
	// whether or not it answers: the key is consumed when none does.
	var owned *Binding
	matches := func(row *Binding) bool {
		if row.Keys != "" {
			return !key.Paste && row.Keys == sequence
		}
		ok, _ := matchesPattern(row.Pattern, key)
		return row.Pattern != "" && prefix == "" && ok
	}
	for t, rows := range tables {
		for i := range rows {
			row := &rows[i]
			if row.Unbind || row.Capture != lv.capture {
				continue
			}
			if lv.owns && owned == nil && matches(row) {
				owned = row
			}
			if !r.Available(row.Request, ev.Focus, ev.Mode) {
				if prefix != "" && row.Keys == sequence && !key.Paste {
					look.bound = true
				}
				continue // not available here: the walk goes on, as the listings hide it
			}
			if prefix != "" && row.Keys == sequence && !key.Paste && !r.rowGuardHolds(row.When, ev) {
				look.bound = true
			}
			if row.Keys != "" && !key.Paste {
				if row.Keys == sequence {
					if look.exact == nil {
						look.exact = row
					}
					if !inherited[t] && r.rowGuardHolds(row.When, ev) {
						look.ownExact = true
					}
					candidates = append(candidates, candidate{row, -1})
				} else if strings.HasPrefix(row.Keys, sequence+" ") && !isPrefixCancel(*row) && r.rowGuardHolds(row.When, ev) {
					// A prefix's generated cancel alone never makes it
					// wait: with every sequence on the prefix guarded off
					// (by its verb's guard or its own), the key answers as
					// if none were bound.
					if inherited[t] {
						look.inherited = true
					} else {
						look.longer = true
					}
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
	if hit == nil && owned != nil {
		// The owning level's key, which nothing here runs: consumed. It
		// keeps the keys typed, so a longer sequence further out still
		// waits over it.
		hit = &Binding{Keys: cmp.Or(owned.Keys, key.Key), Request: Do(NoopAction).Request}
	}
	if hit != nil && hit.Keys == "" {
		look.exact = nil
	}
	return hit, look
}

// rowGuardHolds reports whether a row's own guard (When) lets it answer:
// no guard, or a registered guard that holds.
func (r *Runtime) rowGuardHolds(when string, ev KeyEvent) bool {
	if when == "" {
		return true
	}
	check := r.guard(when)
	return check != nil && check(r, ev)
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
var stayingRequests = map[string]bool{"ochazuke.mode.enter": true, "ochazuke.count.append": true, "ochazuke.mode.leave": true, "ochazuke.input.cancel": true, "ochazuke.macro.record": true, "ochazuke.key.dispatch": true}

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
			// A row source with no value (no Data, no such field) leaves
			// the argument out rather than passing null.
			if v := r.argument(source, ev); v != nil {
				args[arg] = v
			}
		}
		q.Args = args
		if q.IsProp() {
			// A prop row's From fills its value (validatePropRow).
			if v, ok := args["value"]; ok {
				q.Value = v
			}
			delete(args, "value")
			if len(args) == 0 {
				q.Args = nil
			}
		}
	}
	execute := func(q Request) error {
		r.rowRequest = true
		_, err := r.Execute(q)
		return err
	}
	// A request lacking a required argument opens the args form for it
	// (MissingOpensFormSetting); the form runs it as the key's.
	source := r.history.source
	formed := r.formForMissing(q, func(q Request) error {
		defer r.forceSource(source)()
		return execute(q)
	})
	var err error
	if !formed {
		err = execute(q)
	}
	if r.pendingModes[before] && r.modeGeneration == generation && r.Mode.Name == before {
		r.leaveMode()
	}
	if err == nil && !formed && b.Replay && !ev.Key.Paste {
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
			text := pasteText(ev.Key.Key)
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
	case SourceLetter:
		return keyLetter(ev.Key.Key)
	case SourceRow:
		return jsonShaped(r.rowData(ev))
	}
	if field, ok := rowSourceField(source); ok {
		return dataField(r.rowData(ev), field)
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
	out := r.withAliases(r.bindingRows(focus))
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

func (r *Runtime) allBindings() []Binding {
	out := []Binding{}
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		noun := r.Nouns[name]
		for _, mode := range slices.Sorted(maps.Keys(noun.Bindings)) {
			for _, row := range noun.Bindings[mode] {
				row.Noun, row.Instance, row.Mode = name, "", mode
				out = append(out, row)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(r.Instances)) {
		for _, row := range r.Instances[id].Bindings {
			row.Noun, row.Instance = "", id
			out = append(out, row)
		}
	}
	SortBindings(out)
	return r.withAliases(out)
}

// withAliases fills each row's Aliases with its operation's other names
// (bindings list): the row names the canonical operation, its aliases
// listed once beside it.
func (r *Runtime) withAliases(rows []Binding) []Binding {
	for i, row := range rows {
		if row.Request.Verb != "" && !row.Request.IsProp() {
			rows[i].Aliases = r.aliasNames(row.Request.Key())
		}
	}
	return rows
}

// SortBindings orders rows the way the full export (bindings.list --all,
// bindings.reference.json) prints them: by mode (the any-mode rows, mode
// "", first), then by noun, a noun's rows before an instance's of the same
// name, then by the request's verb (a row with no verb, an unbind, after
// every verb), then by keys, a pattern row after the key rows; stable, so
// rows equal on all of these keep their stored order.
func SortBindings(rows []Binding) {
	owner := func(b Binding) (string, bool) {
		if b.Noun != "" {
			return b.Noun, false
		}
		return b.Instance, true
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Mode != b.Mode {
			return a.Mode < b.Mode
		}
		an, ai := owner(a)
		bn, bi := owner(b)
		if an != bn {
			return an < bn
		}
		if ai != bi {
			return !ai
		}
		if a.Request.Empty() != b.Request.Empty() {
			return !a.Request.Empty()
		}
		if a.Request.Verb != b.Request.Verb {
			return a.Request.Verb < b.Request.Verb
		}
		if (a.Keys == "") != (b.Keys == "") {
			return a.Keys != ""
		}
		if a.Keys != b.Keys {
			return a.Keys < b.Keys
		}
		return a.Pattern < b.Pattern
	})
}

// bindingRows collects the rows the focus answers to in its current mode in
// resolution order: capture rows outer-first, then innermost level first, each level's instance rows before
// its noun's mode table before its any-mode table, the first row per key or
// pattern. A level whose unguarded any row answers every key ends the
// walk, as it does for Dispatch. A row whose operation's verb guard is
// false at the focus is left out.
func (r *Runtime) bindingRows(focus string) []Binding {
	rows := r.levelRows(focus)
	out := make([]Binding, len(rows))
	for i, row := range rows {
		out[i] = row.Binding
	}
	return out
}

// levelRow is one row of bindingRows with the noun of the level that
// answers with it (the instance's noun, "modal", "editor"). Guard names
// the condition a hypothetical row answers under (nounRows: its own when,
// else its verb guard), which a walk without an instance cannot judge;
// the live walk judges guards and leaves it empty.
type levelRow struct {
	Binding
	level     string
	guard     string
	inherited bool // read from a table the mode inherits (nounTables past the first)
}

// levelRows is bindingRows with each row's level.
func (r *Runtime) levelRows(focus string) []levelRow {
	return r.scanRows(r.walk(focus), r.Mode, focus, false)
}

// scanRows reads the rows a walk answers with in a mode, the first per
// key or pattern, in walk order (see bindingRows). With focus set it is
// the live reading: a capture row whose guard is false there and a row
// whose verb guard is false are left out. Hypothetical (nounRows, no
// instance to judge a guard at) keeps a guarded row, naming its guard,
// and lets it claim its key only once an unguarded row on the same key
// follows, so the rows that answer when the guard is false show after it;
// editing says the editor's implicit input guard holds (an input noun).
func (r *Runtime) scanRows(levels []level, mode ModeState, focus string, editing bool) []levelRow {
	return r.renderCache.scan(r, levels, mode, focus, editing)
}

func (r *Runtime) scanRowsUncached(levels []level, mode ModeState, focus string, editing bool) []levelRow {
	live := focus != ""
	base, prefix := baseAndPrefix(mode)
	exclusive := r.pendingModes[mode.Name] || mode.Payload.Shadow != nil
	seen := map[string]bool{}
	var out, owned []levelRow
	var capture, inherited, owns bool
	var name string
	take := func(rows []Binding) {
		for _, b := range rows {
			if b.Unbind || b.Capture != capture {
				continue
			}
			guard := ""
			if live && owns && (!r.Available(b.Request, focus, mode) || !r.rowGuardHolds(b.When, KeyEvent{Mode: mode, Focus: focus})) {
				// A read-only input's editor key that does not run there is
				// consumed (level.owns): it claims the key, so the page's
				// row on it is not offered, and shows nothing itself.
				if prefix == "" || strings.HasPrefix(b.Keys, prefix+" ") {
					seen[b.Keys+"\x00"+b.Pattern] = true
				}
				continue
			}
			if live {
				if capture && b.When != "" {
					// A capture row standing aside (ctrl+c's no-modal inside the
					// palette) must not shadow the inner row it lets through.
					if check := r.guard(b.When); check == nil || !check(r, KeyEvent{Mode: mode, Focus: focus}) {
						continue
					}
				}
				if !r.Available(b.Request, focus, mode) {
					continue // guarded off here: hints and bindings list match the palette
				}
			} else {
				guard = b.When
				if guard == ImplicitGuard && editing {
					guard = ""
				}
				if guard == "" {
					guard = strings.Join(r.VerbAvailability(requestOperation(b.Request)), " ")
				}
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
			// A row whose own guard is false at a live focus does not
			// answer, so it does not shadow an outer row on its keys (l on
			// a list that is no table leaves the timeline's l its hint).
			if guard == "" && (!live || r.rowGuardHolds(b.When, KeyEvent{Mode: mode, Focus: focus})) {
				seen[id] = true
			}
			row := levelRow{Binding: b, level: name, guard: guard, inherited: inherited}
			if owns {
				// The owning editor level claims its keys first but lists
				// them where the fallback would: after the page's and
				// app's, so a modal's own keys (q) still lead the hints.
				owned = append(owned, row)
				continue
			}
			out = append(out, row)
		}
	}
	for _, lv := range levels {
		capture = lv.capture
		owns = lv.owns && live
		name = lv.name()
		if owns {
			before := len(owned)
			for i, rows := range r.nounTables(lv.noun, base) {
				inherited = i > 0
				take(rows)
			}
			inherited = false
			if !exclusive {
				take(lv.noun.Bindings[""])
			}
			if answersEveryKey(owned[before:]) {
				break
			}
			continue
		}
		before := len(out)
		if lv.inst != nil {
			take(rowsForMode(lv.inst.Bindings, base, exclusive))
		}
		if lv.noun != nil {
			for i, rows := range r.nounTables(lv.noun, base) {
				inherited = i > 0
				take(rows)
			}
			inherited = false
			if !exclusive {
				take(lv.noun.Bindings[""])
			}
		}
		if answersEveryKey(out[before:]) {
			break
		}
	}
	out = append(out, owned...)
	if r.dropsInheritedSequences() {
		out = dropShadowedInherited(out, prefix)
	}
	return out
}

// dropShadowedInherited leaves out the inherited sequences the walk does
// not wait on (sequence-inherited drop): an inherited row whose keys
// extend, past the prefix typed, the keys of an unguarded row of an own
// table on the walk (visual-line d over a row noun's normal d d).
func dropShadowedInherited(rows []levelRow, prefix string) []levelRow {
	own := map[string]bool{}
	for _, row := range rows {
		if !row.inherited && row.Keys != "" && row.guard == "" && !isPrefixCancel(row.Binding) {
			own[row.Keys] = true
		}
	}
	if len(own) == 0 {
		return rows
	}
	shadowed := func(keys string) bool {
		parts := strings.Fields(keys)
		start := 1
		if prefix != "" {
			start = len(strings.Fields(prefix)) + 1
		}
		for n := start; n < len(parts); n++ {
			if own[strings.Join(parts[:n], " ")] {
				return true
			}
		}
		return false
	}
	out := rows[:0:0]
	for _, row := range rows {
		if row.inherited && shadowed(row.Keys) {
			continue
		}
		out = append(out, row)
	}
	return out
}

// nounWalk is the walk a focus on place's noun would have in mode, with
// no instance: built from the place's chain innermost first as stack
// orders a live focus's levels: the noun's tables, then each ancestor
// noun's (the list, the panel, the page), then the modal noun when the
// place is inside a modal, then app (its instance's rows and its noun's),
// then the editor fallback when a noun on the chain is an input noun
// (inputNoun), with the capture pass as walk has it (none in a passthrough
// mode; a pending mode is passthrough when the noun pins one, as
// leader_pending over a terminal is). A hypothetical level holds only
// noun tables: the rows a live instance carries of its own are not part
// of it. NounPlace{Noun: noun} is no place: the noun, app, the editor.
func (r *Runtime) nounWalk(place NounPlace, mode ModeState) []level {
	place = r.canonicalPlace(place)
	chain := place.Chain()
	var inner []level
	for i := len(chain) - 1; i >= 0; i-- {
		name := chain[i]
		if name == "ochazuke.editor" || name == "app" {
			continue // the editor table is the shared fallback below
		}
		if n := r.Nouns[name]; n != nil {
			inner = append(inner, level{noun: n})
		}
	}
	if place.Modal != "" {
		if n := r.Nouns["ochazuke.modal"]; n != nil {
			inner = append(inner, level{noun: n})
		}
	}
	inner = append(inner, level{inst: r.Instances["app"], noun: r.Nouns["app"]})
	if editor := r.Nouns["ochazuke.editor"]; editor != nil && r.placeEditing(place) {
		inner = append(inner, level{noun: editor})
	}
	base, _ := baseAndPrefix(mode)
	passthrough := r.passthroughModes[base]
	if r.pendingModes[base] {
		if n := r.Nouns[place.Noun]; n != nil {
			passthrough = r.passthroughModes[n.Pin]
		}
	}
	return r.walkOf(inner, passthrough)
}

// placeEditing reports whether an input is on the place's chain, so the
// editor noun is the walk's fallback (stack's editing).
func (r *Runtime) placeEditing(place NounPlace) bool {
	return slices.ContainsFunc(place.Chain(), r.inputNoun)
}

// nounRows is levelRows for a hypothetical focus on place's noun in mode
// (nounWalk): what the keys would run there, guarded rows kept and
// marked with their guard.
func (r *Runtime) nounRows(place NounPlace, mode ModeState) []levelRow {
	place = r.canonicalPlace(place)
	return r.scanRows(r.nounWalk(place, mode), mode, "", r.placeEditing(place))
}

// answersEveryKey reports whether a level's rows include an unguarded any
// row, which answers every key before an outer level is consulted.
func answersEveryKey(rows []levelRow) bool {
	for _, b := range rows {
		if b.Pattern == Any && b.When == "" {
			return true
		}
	}
	return false
}

// Key-sequence prefixes.

type pendingPrefix struct {
	focus    string
	mode     string
	deadline time.Time
	// forever is a prefix with no deadline (sequence-timeout 0, the
	// default): it waits for the next key, so no timer is scheduled and
	// nothing expires it; a focus or mode change still cancels it.
	forever   bool
	scheduled bool
}

// enterPrefix waits for the rest of a key sequence as a pending mode named
// "<base> <keys so far>".
func (r *Runtime) enterPrefix(base, sequence string, fallback *Request) {
	r.enterMode(base+" "+sequence, Payload{Prefix: sequence, Request: fallback, Count: r.Mode.Payload.Count, OpCount: r.Mode.Payload.OpCount})
}

// armPrefix starts the sequence timer for a pending mode the focus is in,
// whatever makes it one (a multi-key row's prefix, or a row entering a
// pending mode with a prefix, such as the terminal leader): when
// sequence-timeout passes before the next key, finishPrefix leaves the
// mode and runs the request stored in its payload. 0, the default, arms
// no timer, so the sequence waits for the next key.
func (r *Runtime) armPrefix(focus, mode, sequence string) {
	timeout := r.sequenceTimeout()
	r.Pending = sequence
	r.bindingVersion++
	r.prefix = pendingPrefix{focus: focus, mode: mode, forever: timeout == 0}
	if timeout > 0 {
		r.prefix.deadline = time.Now().Add(timeout)
	}
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
	if r.prefix.forever || time.Now().Before(r.prefix.deadline) {
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
	r.Register(Operation{Verb: "enter", Noun: "ochazuke.mode", Description: "Enter a pending mode with a typed payload (op, count, opcount; prefix arms the sequence timer, request is what it runs)", Parameters: map[string]Parameter{"mode": {Type: "string", Required: true, Noun: "ochazuke.modes"}, "op": {Type: "string"}, "count": {Type: "integer"}, "opcount": {Type: "integer"}, "prefix": {Type: "string", Description: "Key sequence the mode holds; the timer runs request when no key follows"}, "request": {Type: "object", Description: "Request run when the sequence times out"}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
	r.Register(Operation{Verb: "dispatch", Noun: "ochazuke.key", Description: "Dispatch one key through the stack walk; mode \"shadow\" resolves it in the runtime's own mode beneath a macro recording, so the key is recorded once, and another mode name resolves it in that mode", Parameters: map[string]Parameter{"key": {Type: "string", Required: true, Description: "Key name as bindings spell it; for a paste, the bracketed text"}, "paste": {Type: "boolean", Description: "The key is pasted text"}, "mode": {Type: "string", Description: "Empty for the live walk, \"shadow\" or a mode name"}}, Result: "boolean", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		paste, _ := a["paste"].(bool)
		key := MacroKey{Key: StringArg(a, "key", ""), Paste: paste}
		focus := focusOf(c)
		switch mode := StringArg(a, "mode", ""); mode {
		case "":
			return r.Dispatch(key)
		case "shadow":
			outcome, err := r.dispatchIn(focus, r.Mode, key)
			return outcome != KeyUnhandled, err
		default:
			if !r.Modes[mode] && !r.pendingModes[mode] {
				return nil, fmt.Errorf("unknown mode %q", mode)
			}
			outcome, err := r.dispatchIn(focus, ModeState{Name: mode, Payload: r.Mode.Payload}, key)
			return outcome != KeyUnhandled, err
		}
	}})
	r.Register(Operation{Verb: "leave", Noun: "ochazuke.mode", Description: "Return from a pending mode to the mode it shadowed", Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if r.pending() {
			r.leaveMode()
		}
		return r.Mode, nil
	}})
	r.Register(Operation{Verb: "append", Noun: "ochazuke.count", Description: "Append a digit to the pending count", Parameters: map[string]Parameter{"digit": {Type: "integer", Required: true}}, Result: "count", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
	r.Register(Operation{Verb: "run", Noun: "ochazuke.request", Description: "Execute the request stored in the mode payload", Result: "JSON value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
		if q.Empty() || q.Noun == "" {
			return nil, fmt.Errorf("%s needs a noun and a verb or prop", name)
		}
		return &q, nil
	}
	return nil, fmt.Errorf("%s must be a request", name)
}
