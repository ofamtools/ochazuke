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
	// Within is the hypothetical interaction a query judges the event in
	// (bindings.stops, the typing check, help at the place it opened
	// over): the focus, mode and modal chain that stand for the live ones
	// while the runtime itself stays as it is. Nil when a key or a call is
	// judged live. A guard reads it through Runtime.EventFocus and
	// Runtime.EventModals; Mode and Focus above already carry it.
	Within *resolutionContext
}

// level is one step of the stack walk: an instance (nil for the modal level)
// and the noun whose tables it reads. A capture level reads only capture
// rows; a bubbling level reads only the others.
type level struct {
	inst    *Instance
	noun    *Noun
	capture bool
	// fallback marks the walk's fallback table (Noun.Fallback): read after
	// app, and last in the capture pass wherever the bubbling pass reads it.
	fallback bool
	// owns marks the fallback level of a focused component that owns its
	// keys (KeyOwner: a read-only input): it answers right after the
	// focused instance, and a key one of its rows binds in the mode is the
	// component's even where no row answers (an edit the input refuses, a
	// row guarded off), consumed rather than passed to the page.
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
// stack outer-first (app first, the fallback table last) reading only
// capture rows, then the bubbling pass, the stack inner-first reading the
// rest. The first row that answers in either pass wins, so an app-level
// capture row (ctrl+c → app.quit) answers before an inner any row (a
// terminal's passthrough) can swallow the key. While the base mode is a
// passthrough mode (DefinePassthrough: the terminal) there is no capture
// pass, so the mode's rows see every key, ctrl+c included.
func (r *Runtime) walk(focus string) []level {
	return r.walkIn(r.liveResolution(focus))
}

// walkIn is walk in a resolution context: the stack bounded by its modal
// chain, the capture pass skipped while its base mode is a passthrough
// mode. A query reads it for its own focus, mode and modals.
func (r *Runtime) walkIn(ctx resolutionContext) []level {
	return r.walkOf(r.stackIn(ctx.Focus, ctx.Modals), r.modes[r.baseModeOf(ctx.Mode)].Passthrough)
}

// walkOf is walk over a given stack: the capture pass (skipped in a
// passthrough mode) outer-first with the fallback table last, then the
// stack itself. The live walk and the hypothetical one (nounWalk) share it.
func (r *Runtime) walkOf(inner []level, passthrough bool) []level {
	if passthrough {
		return inner
	}
	var out, fallback []level
	for i := len(inner) - 1; i >= 0; i-- {
		lv := inner[i]
		lv.capture = true
		if lv.fallback {
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
// outward to app, with the modal noun inserted before app while a modal is
// open, and the focus's fallback table last (fallbackOf: Noun.Fallback, else
// the component's, FallbackKeys; every input names the editor noun). A
// level whose noun is the fallback does not read it at its own place. A
// list row, a page or a terminal focus declares none and ends at app:
// editor rows neither answer nor appear in bindings.list for it; counts,
// registers and macros are app rows and still do.
//
// A focused component that owns its keys (KeyOwner: a read-only input, a
// viewer whose keys are the editor's) reads its fallback table right after
// its own level (before the panel, the page, app), so its motions and
// selections win over the page's rows in every mode it is in (visual-line's
// inherited normal rows included), and a key the table binds in the mode
// that does not run there (an edit, refused while read-only) is consumed
// at that level (level.owns) instead of reaching a page row on the same
// key. Keys the table does not bind still reach the page.
//
// The top modal is a resolution boundary (a z-index): while one is open
// and the walked focus is inside it (the live focus always is), the walk is the instances from the focus up to
// and including the modal instance, then the modal noun, then app. The
// levels beneath the modal (its Parent chain, kept for Pop and rendering)
// are never consulted.
func (r *Runtime) stack(focus string) []level { return r.stackIn(focus, r.modals) }

// stackIn is stack bounded by a given modal chain (outermost first): the
// live one, or a query's (help reads its origin under the modals beneath
// it).
func (r *Runtime) stackIn(focus string, modals []string) []level {
	ancestry := r.Ancestry(focus)
	// The modal that holds the focus bounds the walk: the top one for the
	// live focus, a covered one for the focus it keeps (its hint line while
	// a confirm is over it). A focus in none keeps the top modal's level.
	top := ""
	if len(modals) > 0 {
		top = modals[len(modals)-1]
	}
	for i := len(modals) - 1; i >= 0; i-- {
		if slices.Contains(ancestry, modals[i]) {
			top = modals[i]
			break
		}
	}
	if top != "" {
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
	for _, id := range ancestry {
		onStack[id] = true
	}
	var fallback *Noun
	owns := false
	if len(ancestry) > 0 {
		fallback, owns = r.fallbackOf(r.Instances[ancestry[0]])
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
		if i == len(ancestry)-1 && len(modals) > 0 {
			frame := r.Instances[top]
			if frame != nil && onStack[frame.ID] {
				frame = nil
			}
			out = append(out, level{inst: frame, noun: r.Nouns["ochazuke.modal"]})
		}
		noun := r.Nouns[inst.Noun]
		if noun != nil && noun == fallback {
			noun = nil // the fallback table is read at its own place
		}
		out = append(out, level{inst: inst, noun: noun})
		if i == 0 && owns {
			out = append(out, level{noun: fallback, fallback: true, owns: true})
		}
	}
	if fallback != nil && !owns {
		out = append(out, level{noun: fallback, fallback: true})
	}
	return out
}

// fallbackOf is the table a focus on inst reads as the walk's fallback,
// nil when it has none: its noun's Fallback, else its component's
// (FallbackKeys). owns says the component owns that table's keys now
// (KeyOwner), so it is read right after the focus.
func (r *Runtime) fallbackOf(inst *Instance) (fallback *Noun, owns bool) {
	if inst == nil {
		return nil, false
	}
	name := ""
	if n := r.Nouns[inst.Noun]; n != nil {
		name = n.Fallback
	}
	if declares, ok := inst.Component.(FallbackKeys); ok && name == "" {
		name = declares.FallbackNoun()
	}
	if name == "" {
		return nil, false
	}
	fallback = r.Nouns[r.CanonicalNoun(name)]
	if owner, ok := inst.Component.(KeyOwner); ok && fallback != nil {
		owns = owner.OwnsKeys()
	}
	return fallback, owns
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
	exclusive := r.modes[mode.Name].Pending || mode.Payload.Shadow != nil
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
		return outcome, r.runRow(lv, *hit, ev, "")
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
		if handled, err := r.dispatchOutside(focus, mode, key); handled {
			return KeyHandled, err
		}
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
			if !r.availableWithin(row.Request, r.rowInstance(lv, row.Request, ev.Focus), ev.Mode, ev.Within) {
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

// rowInstance is the instance a binding row's request acts at when the
// row runs from focus at level lv, the one Execute judges its
// availability at: the instance its target names (a target that names
// none leaves the focus, and Execute reports it), else the level's
// instance when the operation is that level's own (d d → row.delete on
// the row noun deletes that row and its confirm names it), else the
// focus. Shared operations a level binds (mode.switch, input.*, focus.next)
// keep the focus. The resolver and the live listings ask Available here,
// so a row its target refuses (list.move at a trapped edge of the list a
// child's level reaches) stands aside rather than winning the key and
// being refused when it runs.
func (r *Runtime) rowInstance(lv level, q Request, focus string) string {
	if q.Target != "" && q.Target != "focused-input" {
		if id, err := r.TargetInstance(q.Target); err == nil {
			return id
		}
		return focus
	}
	if lv.inst != nil && lv.inst.ID != focus && lv.noun != nil && lv.noun.Name == q.Noun {
		return lv.inst.ID
	}
	if op, ok := r.Operation(q.Key()); ok && op.byAssociation() && r.nounInstance(op.Noun, focus) == "" {
		if id, _ := r.nearestOf(op.Noun, focus); id != "" {
			return id
		}
	}
	return focus
}

// runRow executes a row. A pending mode is one-shot: the key that answers it
// consumes it, so the mode returns to the shadowed mode before the request
// runs (the request may then enter another pending mode) unless the request
// itself manages modes.
func (r *Runtime) runRow(lv level, b Binding, ev KeyEvent, guardMode string) error {
	keys := b.Keys
	if keys == "" {
		keys = ev.Key.Key
	}
	defer r.Sourced("key:" + keys)()
	before, generation := r.Mode.Name, r.modeGeneration
	if r.modes[before].Pending && !r.Operations[b.Request.Key()].Staying {
		r.leaveMode()
	}
	q := b.Request
	if q.Target == "" {
		// A row addresses the instance of the level it matched at when the
		// operation is that level's own (rowInstance).
		if at := r.rowInstance(lv, q, ev.Focus); at != ev.Focus && lv.inst != nil && at == lv.inst.ID {
			q.Target = at
		}
	}
	if len(b.From) > 0 {
		args, err := r.fillFrom(q.Args, b.From, ev, true)
		if err != nil {
			return err
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
	if r.modes[r.BaseMode()].Passthrough {
		r.UI().passthroughInput++
	}
	execute := func(q Request) (any, error) {
		present := r.keyResultPresenter(q)
		if b.Present != nil && !*b.Present {
			present = nil
		}
		r.rowRequest = true
		c := r.contextOf(ev.Focus)
		c.guardMode = guardMode
		value, err := r.ExecuteAt(c, q)
		if err == nil && present != nil {
			err = present(value)
		}
		return value, err
	}
	// A request lacking a required argument opens the args form for it
	// (MissingOpensFormSetting); the form runs it as the key's.
	formed, err := r.runOrAsk(ev.Focus, q, execute, false, nil)
	if r.modes[before].Pending && r.modeGeneration == generation && r.Mode.Name == before {
		r.leaveMode()
	}
	if err == nil && !formed && b.Replay && !ev.Key.Paste {
		_, err = r.Dispatch(ev.Key)
	}
	return err
}

// argSource reads one From source word for a key event: p is the payload of
// the pending mode (its shadowed mode's when it shadows one), field the part
// after "row:" (empty for a named source).
type argSource func(r *Runtime, ev KeyEvent, p Payload, field string) any

// argSourceRows are the named From sources: one row per source kind.
var argSourceRows = []struct {
	name string
	read argSource
}{
	{SourceKey, func(r *Runtime, ev KeyEvent, p Payload, _ string) any { return ev.Key.Key }},
	{SourceDigit, func(r *Runtime, ev KeyEvent, p Payload, _ string) any {
		n, _ := strconv.Atoi(ev.Key.Key)
		return n
	}},
	{SourceText, func(r *Runtime, ev KeyEvent, p Payload, _ string) any {
		if !ev.Key.Paste {
			return ev.Key.Key
		}
		return strings.ReplaceAll(strings.ReplaceAll(pasteText(ev.Key.Key), "\r\n", "\n"), "\r", "\n")
	}},
	{SourceCount, func(r *Runtime, ev KeyEvent, p Payload, _ string) any { return max(1, p.Count) }},
	{SourceLine, func(r *Runtime, ev KeyEvent, p Payload, _ string) any {
		if p.Count > 0 {
			return p.Count
		}
		return p.OpCount
	}},
	{SourceSteps, func(r *Runtime, ev KeyEvent, p Payload, _ string) any { return max(1, p.Count) * max(1, p.OpCount) }},
	{SourceOp, func(r *Runtime, ev KeyEvent, p Payload, _ string) any { return p.Op }},
	{SourceOpCount, func(r *Runtime, ev KeyEvent, p Payload, _ string) any { return p.OpCount }},
	{SourcePrefix, func(r *Runtime, ev KeyEvent, p Payload, _ string) any { return p.Prefix }},
	{SourceLetter, func(r *Runtime, ev KeyEvent, p Payload, _ string) any { return keyLetter(ev.Key.Key) }},
	{SourceRow, func(r *Runtime, ev KeyEvent, p Payload, _ string) any { return jsonShaped(r.rowData(ev)) }},
}

// argSources indexes argSourceRows by source word.
var argSources = func() map[string]argSource {
	m := make(map[string]argSource, len(argSourceRows))
	for _, row := range argSourceRows {
		m[row.name] = row.read
	}
	return m
}()

// rowFieldSource is the source "row:<field>"; the word's field is passed.
func rowFieldSource(r *Runtime, ev KeyEvent, p Payload, field string) any {
	return r.rowField(ev, field)
}

// parseSource is the reader of one source word and its field: a named
// source from argSources, or "row:<field>" (field checked by validSource).
func parseSource(word string) (argSource, string, bool) {
	if read, ok := argSources[word]; ok {
		return read, "", true
	}
	if field, ok := rowSourceField(word); ok {
		return rowFieldSource, field, true
	}
	return nil, "", false
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
	read, field, ok := parseSource(source)
	if !ok {
		return nil
	}
	p := ev.Mode.Payload
	if p.Shadow != nil {
		p = p.Shadow.Payload
	}
	return read(r, ev, p, field)
}

// enterMode puts the runtime into a pending mode. The base mode it shadows
// is kept in the payload and restored by leave. A payload with a Prefix is a
// key sequence in progress, so entering the mode arms the sequence timer.
func (r *Runtime) enterMode(name string, payload Payload) {
	payload.Return = r.BaseMode()
	r.changeMode(ModeState{Name: name, Payload: payload}, modePendingEnter)
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
	r.changeMode(ModeState{Name: r.BaseMode()}, modePendingLeave)
}

// pending reports whether the mode is a pending mode or a sequence prefix.
func (r *Runtime) pending() bool {
	return r.modes[r.Mode.Name].Pending || r.Mode.Payload.Prefix != ""
}

// ListBindings prints the rows the focus answers to in its current mode,
// one per key or pattern, sorted by key with pattern rows last.
func (r *Runtime) ListBindings(focus string) []Binding {
	return sortListedBindings(r.withAliases(r.bindingRows(focus)))
}

// sortListedBindings orders rows as ListBindings lists them: by key, the
// pattern rows last.
func sortListedBindings(out []Binding) []Binding {
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

func (r *Runtime) allBindings() []Binding { return r.AllBindings() }

// AllBindings is every stored binding row, each with the noun or instance
// whose table holds it and its mode: what bindings list all answers and
// config export writes.
func (r *Runtime) AllBindings() []Binding {
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
	return r.scanRows(r.walk(focus), r.Mode, focus)
}

// scanRows reads the rows a walk answers with in a mode, the first per
// key or pattern, in walk order (see bindingRows). With focus set it is
// the live reading: a capture row whose guard is false there and a row
// whose verb guard is false are left out. Hypothetical (nounRows, no
// instance to judge a guard at) keeps a guarded row, naming its guard,
// and lets it claim its key only once an unguarded row on the same key
// follows, so the rows that answer when the guard is false show after it.
func (r *Runtime) scanRows(levels []level, mode ModeState, focus string) []levelRow {
	return r.renderCache.scan(r, levels, mode, focus)
}

// scanRowsWithin is scanRows for the live interaction (within nil, the
// cache read) or a query's (uncached, its guards judging within).
func (r *Runtime) scanRowsWithin(levels []level, mode ModeState, focus string, within *resolutionContext) []levelRow {
	if within == nil {
		return r.scanRows(levels, mode, focus)
	}
	return r.scanRowsUncached(levels, mode, focus, within)
}

// scanRowsUncached is scanRows reading and filling no cache. within is a
// query's interaction its guards and availability judge (KeyEvent.Within),
// nil for the live one.
func (r *Runtime) scanRowsUncached(levels []level, mode ModeState, focus string, within *resolutionContext) []levelRow {
	live := focus != ""
	base, prefix := baseAndPrefix(mode)
	exclusive := r.modes[mode.Name].Pending || mode.Payload.Shadow != nil
	seen := map[string]bool{}
	var out, owned []levelRow
	var capture, inherited, owns bool
	var name string
	var cur level
	take := func(rows []Binding) {
		for _, b := range rows {
			if b.Unbind || b.Capture != capture {
				continue
			}
			guard := ""
			if live && owns && (!r.availableWithin(b.Request, r.rowInstance(cur, b.Request, focus), mode, within) || !r.rowGuardHolds(b.When, KeyEvent{Mode: mode, Focus: focus, Within: within})) {
				// An owning component's key that does not run there is
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
					if check := r.guard(b.When); check == nil || !check(r, KeyEvent{Mode: mode, Focus: focus, Within: within}) {
						continue
					}
				}
				if !r.availableWithin(b.Request, r.rowInstance(cur, b.Request, focus), mode, within) {
					continue // guarded off where it acts: hints and bindings list match the palette
				}
			} else {
				guard = b.When
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
			if guard == "" && (!live || r.rowGuardHolds(b.When, KeyEvent{Mode: mode, Focus: focus, Within: within})) {
				seen[id] = true
			}
			row := levelRow{Binding: b, level: name, guard: guard, inherited: inherited}
			if owns {
				// The owning level claims its keys first but lists
				// them where the fallback would: after the page's and
				// app's, so a modal's own keys (q) still lead the hints.
				owned = append(owned, row)
				continue
			}
			out = append(out, row)
		}
	}
	for _, lv := range levels {
		cur = lv
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
// then the place's fallback table (placeFallback), with the capture pass
// as walk has it (none in a passthrough mode; a pending mode is
// passthrough when the noun pins one, as leader_pending over a terminal
// is). A hypothetical level holds only noun tables: the rows a live
// instance carries of its own are not part of it. NounPlace{Noun: noun} is
// no place: the noun, app, its fallback.
func (r *Runtime) nounWalk(place NounPlace, mode ModeState) []level {
	place = r.canonicalPlace(place)
	chain := place.Chain()
	root := level{inst: r.Instances["app"]}
	if root.inst != nil {
		root.noun = r.Nouns[root.inst.Noun]
	}
	fallback := r.placeFallback(place)
	var inner []level
	for i := len(chain) - 1; i >= 0; i-- {
		n := r.Nouns[chain[i]]
		if n == nil || n == root.noun || n == fallback {
			continue // the root and the fallback table are read at their own places below
		}
		inner = append(inner, level{noun: n})
	}
	if place.Modal != "" {
		if n := r.Nouns["ochazuke.modal"]; n != nil {
			inner = append(inner, level{noun: n})
		}
	}
	inner = append(inner, root)
	if fallback != nil {
		inner = append(inner, level{noun: fallback, fallback: true})
	}
	base, _ := baseAndPrefix(mode)
	passthrough := r.modes[base].Passthrough
	if r.modes[base].Pending {
		if n := r.Nouns[place.Noun]; n != nil {
			passthrough = r.modes[n.Pin].Passthrough
		}
	}
	return r.walkOf(inner, passthrough)
}

// placeFallback is the fallback table a focus on place's noun would read
// (stack's fallbackOf, with no instance at hand): the noun's Fallback,
// else what a live instance of it declares (an input of an application's
// Kind), else an input's for a noun read as an input (inputNoun: one that
// pins insert, a prompt); nil when it has none.
func (r *Runtime) placeFallback(place NounPlace) *Noun {
	n := r.Nouns[place.Noun]
	if n == nil {
		return nil
	}
	if n.Fallback != "" {
		return r.Nouns[r.CanonicalNoun(n.Fallback)]
	}
	for _, inst := range r.instancesInOrder() {
		if inst.Noun != n.Name {
			continue
		}
		if fallback, _ := r.fallbackOf(inst); fallback != nil {
			return fallback
		}
	}
	if r.inputNoun(n.Name) {
		return r.Nouns[r.CanonicalNoun(new(Input).FallbackNoun())]
	}
	return nil
}

// nounRows is levelRows for a hypothetical focus on place's noun in mode
// (nounWalk): what the keys would run there, guarded rows kept and
// marked with their guard.
func (r *Runtime) nounRows(place NounPlace, mode ModeState) []levelRow {
	place = r.canonicalPlace(place)
	return r.scanRows(r.nounWalk(place, mode), mode, "")
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
	r.Define("ochazuke.mode").Unpersisted()
	r.Define("ochazuke.count").Unpersisted()
	r.Define("ochazuke.key").Unpersisted()
	r.Register(Operation{Verb: "enter", Noun: "ochazuke.mode", FocusBound: true, Staying: true, HistoryKind: HistoryAction, Description: "Enter a pending mode with a typed payload (op, count, opcount; prefix arms the sequence timer, request is what it runs)", Parameters: map[string]Parameter{"mode": {Type: "string", Required: true, Noun: "ochazuke.modes"}, "op": {Type: "string"}, "count": {Type: "integer"}, "opcount": {Type: "integer"}, "prefix": {Type: "string", Description: "Key sequence the mode holds; the timer runs request when no key follows"}, "request": {Type: "object", Description: "Request run when the sequence times out"}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		mode := StringArg(a, "mode", "")
		if !r.modes[mode].Pending {
			return nil, fmt.Errorf("unknown pending mode %q", mode)
		}
		request, err := requestArg(a, "request")
		if err != nil {
			return nil, err
		}
		r.enterMode(mode, Payload{Op: StringArg(a, "op", ""), Count: IntArg(a, "count", 0), OpCount: IntArg(a, "opcount", 0), Prefix: StringArg(a, "prefix", ""), Request: request})
		return r.Mode, nil
	}})
	r.Register(Operation{Verb: "dispatch", Noun: "ochazuke.key", Staying: true, HistoryKind: HistoryAction, Description: "Dispatch one key through the stack walk; mode \"shadow\" resolves it in the runtime's own mode beneath a macro recording, so the key is recorded once, and another mode name resolves it in that mode", Parameters: map[string]Parameter{"key": {Type: "string", Required: true, Description: "Key name as bindings spell it; for a paste, the bracketed text"}, "paste": {Type: "boolean", Description: "The key is pasted text"}, "mode": {Type: "string", Description: "Empty for the live walk, \"shadow\" or a mode name"}}, Result: "boolean", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
			if !r.modeDefined(mode) {
				return nil, fmt.Errorf("unknown mode %q", mode)
			}
			outcome, err := r.dispatchIn(focus, ModeState{Name: mode, Payload: r.Mode.Payload}, key)
			return outcome != KeyUnhandled, err
		}
	}})
	r.Register(Operation{Verb: "leave", Noun: "ochazuke.mode", Staying: true, HistoryKind: HistoryAction, Description: "Return from a pending mode to the mode it shadowed", Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if r.pending() {
			r.leaveMode()
		}
		return r.Mode, nil
	}})
	r.Register(Operation{Verb: "append", Noun: "ochazuke.count", Staying: true, HistoryKind: HistoryAction, Description: "Append a digit to the pending count", Parameters: map[string]Parameter{"digit": {Type: "integer", Required: true}}, Result: "count", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		digit := IntArg(a, "digit", 0)
		if digit < 0 || digit > 9 {
			return nil, errors.New("digit must be 0 to 9")
		}
		if r.Mode.Payload.Count > 100000 {
			return nil, errors.New("count exceeds 1000000")
		}
		next := r.Mode
		next.Payload.Count = next.Payload.Count*10 + digit
		r.changeMode(next, modePayloadUpdate) // the prefix and its deadline stay armed
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
