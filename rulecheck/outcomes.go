package rulecheck

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/cli/mcp"
)

const outcomesHeader = `rulecheck outcomes baseline: kind, subject, detail, count, reason (tab separated).
Kinds: "failed" (subject: an offered request, noun.verb with its arguments; detail: the outcome's cause): a dry run
(Runtime.CheckRequestAt) of a verb offered at a place answered failed, which no caller can change; make it ask
(Ctx.Need, MissingParameterError, a ChoiceAnswer), answer nothing (Nothing, KeptError) or run.
"unwalked" (subject: a noun; detail "no place"): a noun that can be focused (Focusable, or with key rows of its own)
stands at no place the walk read (live instances, recorded places), so no check saw it where it is used: record a place
(the runtime's placements, Config.Places) or give a reason.
"unknown-noun" (subject: a noun name; detail "not registered"): a recorded place names a noun no runtime has (renamed or
not built here): rename it in the places file, or give a reason.
A row with a reason is a reviewed exception; a row without one is debt that only shrinks (RULECHECK_UPDATE=1).`

// OutcomesWidth is the number of key fields of an outcomes row.
const OutcomesWidth = 3

// Kinds of an outcomes finding (its first field).
const (
	// OutcomeFailedKind: a dry run of an offered verb answered failed.
	OutcomeFailedKind = "failed"
	// OutcomeUnwalked: a noun that can be focused stands at no walked place.
	OutcomeUnwalked = "unwalked"
	// OutcomeUnknownNoun: a recorded place names a noun no runtime has.
	OutcomeUnknownNoun = "unknown-noun"
)

// OutcomesCoverage is what the outcomes walk saw, led by its coverage:
// a walk over too few places passes while faults exist (2026-11-16).
type OutcomesCoverage struct {
	// Recorded are the places recorded (Config.Places and each runtime's
	// NounPlaces), Walked the distinct places walked (recorded and live
	// instances, not nouns alone), Live the live instances among them,
	// Skipped the recorded places whose noun no runtime has, Unknown those
	// names (anywhere in a place's chain).
	Recorded, Walked, Live, Skipped int
	Unknown                         []string
	// Alone are the nouns walked on their own (every noun's rows and
	// verbs at the runtime's focus).
	Alone int
	// Runs are the dry runs made, Verbs the distinct verbs among them;
	// ByStatus counts them by MCP status (mcp.Answer, the one map from an
	// outcome to a status).
	Runs, Verbs int
	ByStatus    map[string]int
	// NotDryRunnable are verbs offered but not run, with why; Unavailable
	// the verbs declared never available (Operation.Unavailable: a
	// refusecheck K1 site), not run.
	NotDryRunnable []string
	Unavailable    int
	// PerNoun is the number of walked places each noun stands at (as a
	// level of the walk), and Focusable the nouns the floor holds to one.
	PerNoun   map[string]int
	Focusable int
}

func (c OutcomesCoverage) String() string {
	var status []string
	for _, k := range slices.Sorted(maps.Keys(c.ByStatus)) {
		status = append(status, fmt.Sprintf("%s=%d", k, c.ByStatus[k]))
	}
	unknown := "none"
	if len(c.Unknown) > 0 {
		unknown = strings.Join(c.Unknown, ", ")
	}
	nd := "none"
	if len(c.NotDryRunnable) > 0 {
		nd = strings.Join(c.NotDryRunnable, ", ")
	}
	walked := 0
	for _, n := range c.PerNoun {
		if n > 0 {
			walked++
		}
	}
	return fmt.Sprintf("places recorded %d; walked %d (live instances %d); skipped, noun unknown %d (unknown names: %s); nouns walked alone %d; nouns at a walked place %d (floor holds %d); dry runs %d of %d verbs; by status {%s}; declared unavailable, not run %d; not dry-runnable %d (%s)",
		c.Recorded, c.Walked, c.Live, c.Skipped, unknown, c.Alone, walked, c.Focusable, c.Runs, c.Verbs, strings.Join(status, " "), c.Unavailable, len(c.NotDryRunnable), nd)
}

// Outcomes dry-runs every verb offered at every place of the runtimes and
// reports each one that answers failed, each noun that can be focused but
// stands at no walked place, and each recorded place's noun no runtime
// has.
//
// The invariant: if it is offered, it works. Every run ends done,
// nothing, needs or failed (ochazuke.Outcome); only failed is a fault
// (no caller can change it), and agent rights (mcp's refused) are the
// owner's rule (exception E1). The answer is classified as the MCP
// gateway answers it (mcp.Answer over ochazuke.OutcomeOf), so done,
// nothing, choose and refused pass and failed is a finding.
//
// What is offered: at a place, every row of every noun on its walk (the
// noun, its ancestors, the fallback and root levels; every mode's table)
// and of the instance there, with the row's arguments; and every verb of
// those nouns (what the palette and help offer there), with no arguments
// and with each value of each parameter's Enum. Every noun on its own is
// offered at the runtime's focus the same way, which is offercheck's walk
// (every registered verb). A dry run is Runtime.CheckRequestAt: arguments,
// target, subjects, guards and verb checks, never the handler, a modal or
// history; a live instance's offers run from its own focus, a recorded
// place's (which has no instance to stand at) from the runtime's focus,
// each request once per focus. A catalog proxy (its checks run in the
// extension's process) is not dry-runnable and is listed; a verb declared
// Unavailable is counted and not run.
//
// The places are the live instances, each runtime's recorded places
// (NounPlaces) and the extra places given (a recorded noun-places file,
// LoadPlaces); a place is matched by canonical noun (an alias, a
// namespaced or bare name), and one whose noun no runtime has is skipped
// and reported (unknown-noun). The floor: a noun that is Focusable or
// declares key rows of its own must stand at one walked place (a noun
// walked alone does not count), else unwalked.
//
// Findings have fields kind (failed, unwalked, unknown-noun), subject and
// detail; work counts one unit per place walked and per dry run, and the
// walk stops when it returns false.
func Outcomes(runtimes []*ochazuke.Runtime, places []ochazuke.NounPlace, work func(int) bool) ([]Finding, OutcomesCoverage) {
	o := &outcomeRun{
		cov:       OutcomesCoverage{ByStatus: map[string]int{}, PerNoun: map[string]int{}, Recorded: len(places)},
		found:     map[string]Finding{},
		focusable: map[string]bool{},
		unknownIn: map[string]int{},
		unknownAt: map[string]int{},
		verbs:     map[string]bool{},
		notRun:    map[string]bool{},
		work:      work,
	}
	for _, r := range runtimes {
		if !o.runtime(r, places) {
			break
		}
	}
	return o.done(len(runtimes))
}

// outcomeRun is one Outcomes walk over every runtime.
type outcomeRun struct {
	cov       OutcomesCoverage
	found     map[string]Finding
	focusable map[string]bool
	// unknownIn counts the runtimes a recorded name is unknown in;
	// unknownAt the most places naming it in one runtime.
	unknownIn, unknownAt map[string]int
	verbs, notRun        map[string]bool
	work                 func(int) bool
}

// runtime walks one runtime; false once the work cap is passed.
func (o *outcomeRun) runtime(r *ochazuke.Runtime, places []ochazuke.NounPlace) bool {
	w := outcomeWalk{outcomeRun: o, r: r, ran: map[string]bool{}, byNoun: opsByNoun(r)}
	for name, n := range r.Nouns {
		if n != nil && (n.Focusable || declaresRows(n)) {
			o.focusable[name] = true
		}
	}
	// Every noun alone, at the runtime's focus: its rows and verbs (a verb
	// registered without a defined noun too: offercheck's every verb).
	alone := map[string]bool{}
	for name := range r.Nouns {
		alone[name] = true
	}
	for name := range w.byNoun {
		alone[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(alone)) {
		o.cov.Alone++
		if !w.offerNoun(r.Focus, name, "") {
			return false
		}
	}
	// Recorded places: coverage (their rows and verbs ran above, at the
	// same focus).
	own := r.NounPlaces("")
	o.cov.Recorded += len(own)
	unknown := map[string]int{}
	seen := map[string]bool{}
	for _, p := range append(own, places...) {
		chain := p.Chain()
		skip := false
		var named []string
		for _, n := range chain {
			if r.Nouns[r.CanonicalNoun(n)] != nil || slices.Contains(named, n) {
				continue
			}
			named = append(named, n)
			unknown[n]++
			skip = skip || n == p.Noun
		}
		if skip {
			o.cov.Skipped++
			continue
		}
		key := strings.Join(chain, ">") + "\x00" + p.Modal
		if seen[key] {
			continue
		}
		seen[key] = true
		if !o.work(1) {
			return false
		}
		o.cov.Walked++
		for _, l := range r.HelpWalkAtPlace(p, w.resting()).Levels {
			o.cov.PerNoun[r.CanonicalNoun(l.Noun)]++
		}
	}
	for n, count := range unknown {
		o.unknownIn[n]++
		o.unknownAt[n] = max(o.unknownAt[n], count)
	}
	// Live instances: their walk's nouns stand at a place; their own
	// noun's rows and verbs and their own rows run from their own focus
	// (an ancestor's verbs run at the ancestor, which is live too).
	for _, id := range slices.Sorted(maps.Keys(r.Instances)) {
		inst := r.Instances[id]
		if inst == nil || inst.Noun == "" {
			continue
		}
		if !o.work(1) {
			return false
		}
		o.cov.Walked++
		o.cov.Live++
		for _, l := range r.HelpWalkAt(id, w.resting(), nil).Levels {
			o.cov.PerNoun[r.CanonicalNoun(l.Noun)]++
		}
		for _, row := range inst.Bindings {
			if !w.offerRow(id, row, id) {
				return false
			}
		}
		if !w.offerNoun(id, r.CanonicalNoun(inst.Noun), id) {
			return false
		}
	}
	return true
}

// done adds the floor's and the unknown names' findings, sorted.
func (o *outcomeRun) done(runtimes int) ([]Finding, OutcomesCoverage) {
	cov := o.cov
	cov.Verbs = len(o.verbs)
	cov.NotDryRunnable = slices.Sorted(maps.Keys(o.notRun))
	cov.Focusable = len(o.focusable)
	for _, name := range slices.Sorted(maps.Keys(o.focusable)) {
		if cov.PerNoun[name] > 0 {
			continue
		}
		o.add(Finding{
			Fields:  []string{OutcomeUnwalked, name, "no place"},
			Message: fmt.Sprintf("outcomes: noun %s can be focused and stands at no walked place (live instance or recorded place), so no rule check saw it where it is used; record a place (Config.Places) or add a reviewed row with a reason", name),
		})
	}
	for _, name := range slices.Sorted(maps.Keys(o.unknownIn)) {
		if o.unknownIn[name] < runtimes {
			continue // another runtime has it
		}
		cov.Unknown = append(cov.Unknown, name)
		o.add(Finding{
			Fields:  []string{OutcomeUnknownNoun, name, "not registered"},
			Message: fmt.Sprintf("outcomes: %d recorded places name noun %s, which no runtime here has: rename it in the places file (a renamed noun), or add a reviewed row with a reason (a component not built here)", o.unknownAt[name], name),
		})
	}
	return sortedFindings(o.found), cov
}

func (o *outcomeRun) add(f Finding) {
	if _, dup := o.found[f.Key()]; !dup {
		o.found[f.Key()] = f
	}
}

// outcomeWalk is one runtime's dry runs: each (focus, request) once.
type outcomeWalk struct {
	*outcomeRun
	r      *ochazuke.Runtime
	ran    map[string]bool
	byNoun map[string][]*ochazuke.Operation
	rest   *ochazuke.ModeState
}

// resting is the runtime's resting mode (ModeSpec.CursorOnChar: normal),
// the mode a place's levels are read in.
func (w *outcomeWalk) resting() ochazuke.ModeState {
	if w.rest == nil {
		w.rest = &ochazuke.ModeState{}
		for _, m := range w.r.HelpModes(ochazuke.NounPlace{Noun: ochazuke.AppNoun}, false) {
			if s, _ := w.r.ModeTraits(m.Name); s.CursorOnChar {
				w.rest = &ochazuke.ModeState{Name: m.Name}
				break
			}
		}
	}
	return *w.rest
}

// offerNoun dry-runs a noun's rows (every mode) and verbs (each offer)
// from focus; at names the place for a finding's message ("" for the
// runtime's focus). False once the work cap is passed.
func (w *outcomeWalk) offerNoun(focus, name, at string) bool {
	if n := w.r.Nouns[name]; n != nil {
		for _, mode := range slices.Sorted(maps.Keys(n.Bindings)) {
			for _, row := range n.Bindings[mode] {
				if !w.offerRow(focus, row, at) {
					return false
				}
			}
		}
	}
	for _, op := range w.byNoun[name] {
		for _, args := range offers(*op) {
			if !w.run(focus, *op, ochazuke.Request{Noun: op.Noun(), Verb: op.Name(), Args: args}, at) {
				return false
			}
		}
	}
	return true
}

// offerRow dry-runs a key row's request with its arguments.
func (w *outcomeWalk) offerRow(focus string, row ochazuke.Binding, at string) bool {
	if row.Unbind || row.Request.Empty() {
		return true
	}
	q := row.Request
	key := q.Key()
	if !q.IsProp() {
		key = w.r.CanonicalKey(key)
	}
	op, ok := w.r.Operations.Find(ochazuke.Key(key))
	if !ok && !q.IsProp() {
		op = ochazuke.Operation{Key: ochazuke.Key(key)}
	}
	return w.run(focus, op, q, at)
}

// run dry-runs q from focus once, and records a failed answer.
func (w *outcomeWalk) run(focus string, op ochazuke.Operation, q ochazuke.Request, at string) bool {
	text := requestText(q)
	if w.ran[focus+"\x00"+text] {
		return true
	}
	w.ran[focus+"\x00"+text] = true
	switch {
	case op.CatalogProxy:
		w.notRun[op.Key.Short()+" (catalog proxy: its checks run in the extension)"] = true
		return true
	case op.Unavailable != "":
		w.cov.Unavailable++
		return true
	}
	if !w.work(1) {
		return false
	}
	w.cov.Runs++
	w.verbs[q.Key()] = true
	o := ochazuke.OutcomeOf(nil, w.r.CheckRequestAt(focus, q))
	o.Verb = w.r.DisplayKey(q.Key())
	answer := map[string]any{}
	mcp.Answer(answer, o, true)
	status, _ := answer["status"].(string)
	w.cov.ByStatus[status]++
	if status != "failed" {
		return true
	}
	where := "the runtime's focus"
	if at != "" {
		where = "instance " + at
	}
	w.add(Finding{
		Fields:  []string{OutcomeFailedKind, text, string(o.Cause)},
		Message: fmt.Sprintf("outcomes: %s offered at %s answers failed (%s): %s. An offered verb's dry run must not fail: make it ask (Ctx.Need, MissingParameterError, a ChoiceAnswer), answer nothing (Nothing), or run; or add a reviewed row with a reason", text, where, o.Cause, o.Reason),
	})
	return true
}

// declaresRows reports whether a noun binds key rows of its own (not the
// generated noun defaults).
func declaresRows(n *ochazuke.Noun) bool {
	for _, rows := range n.Bindings {
		for _, b := range rows {
			if b.Keys != "" && !b.Generated && !b.Unbind {
				return true
			}
		}
	}
	return false
}

// opsByNoun are the offered verbs of each noun, by canonical noun.
func opsByNoun(r *ochazuke.Runtime) map[string][]*ochazuke.Operation {
	out := map[string][]*ochazuke.Operation{}
	for _, op := range r.Operations.List() {
		n := r.CanonicalNoun(op.Noun())
		out[n] = append(out[n], op)
	}
	return out
}

// offers are the requests an operation is offered with: no arguments,
// then each Enum value of each parameter.
func offers(op ochazuke.Operation) []ochazuke.Arguments {
	out := []ochazuke.Arguments{{}}
	for _, name := range slices.Sorted(maps.Keys(op.Parameters)) {
		for _, v := range op.Parameters[name].Enum {
			out = append(out, ochazuke.Arguments{name: v})
		}
	}
	return out
}

// requestText is a request as a finding's subject: "noun.verb" or
// "noun.verb {json}", a prop-shaped one with its prop and value.
func requestText(q ochazuke.Request) string {
	key := q.Key()
	extra := map[string]any{}
	for k, v := range q.Args {
		extra[k] = v
	}
	if q.Value != nil {
		extra["@value"] = q.Value
	}
	if q.Toggle {
		extra["@toggle"] = true
	}
	if len(extra) == 0 {
		return key
	}
	args, _ := json.Marshal(extra)
	return key + " " + string(args)
}

func runOutcomes(t *testing.T, c *Context) {
	f, cov := Outcomes(c.AllRuntimes(), c.Places, c.Work)
	c.Over(t)
	t.Log("outcomes: " + cov.String())
	CheckFindings(t, "outcomes", c.BaselinePath("outcomes"), OutcomesWidth, outcomesHeader, f)
}
