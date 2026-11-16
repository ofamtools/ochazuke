package ochazuke

import (
	"cmp"
	"fmt"
	"slices"
)

// Which thing a verb acts on (ask-not-refuse O6). A Target is a value the
// verb's handler asks for (Ctx.Target), and the surfaces that ask for what a
// request needs read it: a value the request gives wins; otherwise the
// things of the parameter's noun found from the whole context, ranked by why
// they were found (Why, in the order of targetSources), and last every thing
// of the noun, which is read only when a picker shows it or a program lists
// it (Target.All, Runtime.ChoicesShown). No rule reads how many candidates
// there are: one, two or N take the same path, and the verb acts without
// asking only on a candidate whose Why is in its acting set (Parameter.Acts,
// target.linked).

// Why says why a thing is a candidate for a target, in rank order: given
// (the request named it), selected (a row of the visual selection), marked
// (a marked row), focused (the focus, or what the parameter's From reads
// there), on-path (a thing on the focus path), on-page (another live thing
// of the noun on the current page), linked (a thing the focus path reaches
// through relations and associations), recent (a thing recent history
// entries name, newest first) and listed (a thing of the noun read from its
// store for a program's list of candidates).
type Why string

// The reasons a thing is a candidate, in rank order.
const (
	WhyGiven    Why = "given"
	WhySelected Why = "selected"
	WhyMarked   Why = "marked"
	WhyFocused  Why = "focused"
	WhyOnPath   Why = "on-path"
	WhyOnPage   Why = "on-page"
	WhyLinked   Why = "linked"
	WhyRecent   Why = "recent"
	WhyListed   Why = "listed"
)

// DefaultActs is the acting set of a parameter that declares none
// (Parameter.Acts): the visual selection, the focus and the focus path, what
// a request acted on before targets were ranked. Whether linked things join
// it is the setting target.linked.
var DefaultActs = []Why{WhySelected, WhyFocused, WhyOnPath}

// The settings of how a target is chosen and asked for.
const (
	// TargetLinkedSetting is what a verb does with a thing linked to what
	// you are on (through a relation or an association) when nothing of its
	// noun is selected, focused or on the focus path: act (the verb acts on
	// the best-ranked linked thing, nearest first; a verb that changes
	// something only on one a single hop away), ask-on-change (the
	// default: a verb that only reads or opens acts on it; a verb that
	// changes something asks with the picker, linked things first) or
	// always-pick (every verb asks, linked things first).
	TargetLinkedSetting = "target.linked"
	// AskTargetSetting is how the TUI asks for a thing a request needs:
	// picker (the default: the args form opens with the needed field's
	// picker on top, the ranked candidates first, then every thing of the
	// noun) or form (the args form opens on the field, and you open its
	// picker yourself, as before).
	AskTargetSetting = "ask.target"
	// ChoicesMaxSetting is how many candidates the CLI and MCP list for what
	// a command needs before the command that lists every thing (default
	// 20; 0 lists every one).
	ChoicesMaxSetting = "cli.choices-max"
)

// TargetCandidate is one thing a target may name: its id (the thing's key, an
// AnyRef for a parameter of any noun, the value itself for a path), how it
// is shown, its noun, why it was found, the live instance that stands for
// it when there is one, and the exact command that runs the request with it.
type TargetCandidate struct {
	ID       string `json:"id"`
	Label    string `json:"label,omitempty"`
	Noun     string `json:"noun,omitempty"`
	Why      Why    `json:"why"`
	Instance string `json:"instance,omitempty"`
	Command  string `json:"command,omitempty"`
	// dist is a linked thing's distance from the focus path (1: named
	// directly), which target.linked's act reads for a change verb.
	dist int
}

// Target is the value that says which thing a verb's parameter acts on:
// the parameter and its noun, whether the request gave it (Given; Chosen is
// then the given value), the candidate the verb acts on without asking
// (Chosen; nil means ask), the ranked candidates (never every thing of the
// noun), and All, the command that lists every thing of the noun ("" when
// the noun has no list verb).
type Target struct {
	Param      string            `json:"param"`
	Noun       string            `json:"noun,omitempty"`
	Given      bool              `json:"given"`
	Chosen     *TargetCandidate  `json:"chosen,omitempty"`
	Candidates []TargetCandidate `json:"candidates"`
	All        string            `json:"all,omitempty"`
}

// Need is the target as what a needs outcome lacks: its parameter and noun,
// the target itself, and its candidates as choices, each with its command.
func (t Target) Need() *Need {
	need := &Need{Param: t.Param, Noun: t.Noun, Target: &t, Choices: make([]ChoiceRow, 0, len(t.Candidates))}
	for _, c := range t.Candidates {
		need.Choices = append(need.Choices, ChoiceRow{ID: c.ID, Label: c.Label, Command: c.Command, Why: c.Why})
	}
	return need
}

// targetReadMax bounds what one candidate source reads: the candidates are
// the nearest things, and every thing of the noun follows them (the picker's
// list scope, a program's listed rows), so no source reads a list whole.
const targetReadMax = 50

// recentEntries is how many of the newest history entries the recent source
// reads.
const recentEntries = 200

// targetScan is what a candidate source reads: the parameter, its noun
// (canonical; "" for a plain value, AnyNoun for any noun) and the focus.
type targetScan struct {
	p     Parameter
	noun  string
	focus string
}

// anyNoun reports whether the scan takes a thing of any noun.
func (s targetScan) anyNoun() bool { return s.noun == AnyNoun }

// targetSource is one place candidates come from, with the reason each of
// its candidates carries.
type targetSource struct {
	why  Why
	read func(r *Runtime, s targetScan) []TargetCandidate
}

// targetSources are the candidate sources in rank order. Each reads only
// what is live or in memory (the instances, the focus path, the history),
// at most targetReadMax things; linked reads what the focus path's levels
// reach, as key dispatch does (nearestReached). Every thing of the noun is
// not a source: a picker's list scope and a program's listed rows read it,
// one page, when they show it.
var targetSources = []targetSource{
	{WhySelected, func(r *Runtime, s targetScan) []TargetCandidate { return r.markedCandidates(s, true) }},
	{WhyMarked, func(r *Runtime, s targetScan) []TargetCandidate { return r.markedCandidates(s, false) }},
	{WhyFocused, (*Runtime).focusedCandidates},
	{WhyOnPath, (*Runtime).pathCandidates},
	{WhyOnPage, (*Runtime).pageCandidates},
	{WhyLinked, (*Runtime).linkedCandidates},
	{WhyRecent, (*Runtime).recentCandidates},
}

// candidatesFor are the ranked candidates for a value of p at focus: each
// source in rank order, a thing found twice kept where it was found first.
func (r *Runtime) candidatesFor(p Parameter, focus string) []TargetCandidate {
	s := targetScan{p: p, noun: r.CanonicalNoun(p.Noun), focus: focus}
	var out []TargetCandidate
	for _, source := range targetSources {
		for _, c := range source.read(r, s) {
			if c.ID == "" || slices.ContainsFunc(out, func(one TargetCandidate) bool { return one.ID == c.ID }) {
				continue
			}
			c.Why = source.why
			out = append(out, c)
		}
	}
	return out
}

// ofNoun reports whether inst is a thing the scan takes: an instance below
// the root of the scan's noun, or of any noun for AnyNoun.
func (s targetScan) ofNoun(inst *Instance) bool {
	return inst != nil && inst.Parent != "" && (s.anyNoun() || inst.Noun == s.noun)
}

// thingCandidate is inst as a candidate: the thing's key (an AnyRef for a
// parameter of any noun) and label.
func (r *Runtime) thingCandidate(inst *Instance, s targetScan) TargetCandidate {
	t := r.instanceThing(inst)
	id := t.Key
	if s.anyNoun() {
		id = AnyRef(inst.Noun, t.Key)
	}
	return TargetCandidate{ID: id, Label: t.Label, Noun: inst.Noun, Instance: inst.ID}
}

// markedCandidates are the marked rows of the scan's noun in the nearest
// list on the focus path: its visual selection when visual is true and the
// list is in visual mode, its marks when visual is false and it is not.
func (r *Runtime) markedCandidates(s targetScan, visual bool) []TargetCandidate {
	for _, id := range r.Ancestry(s.focus) {
		l, ok := r.Instances[id].Component.(*Collection)
		if !ok {
			continue
		}
		var out []TargetCandidate
		for _, item := range l.Marked() {
			if inst := r.Instances[item.ID]; l.Visual() == visual && s.ofNoun(inst) && len(out) < targetReadMax {
				out = append(out, r.thingCandidate(inst, s))
			}
		}
		return out
	}
	return nil
}

// focusedCandidates are what the parameter's From reads at the focus (the
// verb's own declaration of where to look), then the focus itself when it
// is a thing of the noun; for a parameter of any noun, the nearest thing on
// the focus path.
func (r *Runtime) focusedCandidates(s targetScan) []TargetCandidate {
	var out []TargetCandidate
	if s.p.From != "" {
		if value := r.argument(s.p.From, KeyEvent{Focus: s.focus}); !emptyProp(value) {
			text := fmt.Sprint(value)
			out = append(out, TargetCandidate{ID: text, Label: text, Noun: s.p.Noun})
		}
	}
	for _, id := range r.Ancestry(s.focus) {
		inst := r.Instances[id]
		if s.ofNoun(inst) && (s.anyNoun() || id == s.focus) {
			return append(out, r.thingCandidate(inst, s))
		}
	}
	return out
}

// pathCandidates are the things of the noun on the focus path above the
// focus, nearest first, and for a path parameter the path props of the
// nearest level that has one.
func (r *Runtime) pathCandidates(s targetScan) []TargetCandidate {
	var out []TargetCandidate
	for _, id := range r.Ancestry(s.focus) {
		if inst := r.Instances[id]; !s.anyNoun() && id != s.focus && s.ofNoun(inst) {
			out = append(out, r.thingCandidate(inst, s))
		}
	}
	if !s.p.IsPath() {
		return out
	}
	for _, id := range r.Ancestry(s.focus) {
		props := r.pathProps(r.Instances[id])
		if len(props) > 0 {
			return append(out, props...)
		}
	}
	return out
}

// pathProps are the path props inst's noun declares that inst holds, each
// a candidate labelled "PROP · VALUE".
func (r *Runtime) pathProps(inst *Instance) []TargetCandidate {
	n := r.Nouns[inst.Noun]
	if n == nil {
		return nil
	}
	var out []TargetCandidate
	for _, prop := range n.Props {
		if !prop.IsPath() {
			continue
		}
		if value := r.rowValue(inst, prop.Name); !emptyProp(value) {
			text := fmt.Sprint(value)
			out = append(out, TargetCandidate{ID: text, Label: prop.Name + " · " + text, Noun: inst.Noun})
		}
	}
	return out
}

// pageCandidates are the other live things of the noun on the current page
// (an instance whose ancestry holds the page), in registration and list
// order, at most targetReadMax.
func (r *Runtime) pageCandidates(s targetScan) []TargetCandidate {
	if s.noun == "" || s.anyNoun() || r.Page == "" {
		return nil
	}
	var out []TargetCandidate
	add := func(inst *Instance) {
		if s.ofNoun(inst) && len(out) < targetReadMax && slices.Contains(r.Ancestry(inst.ID), r.Page) {
			out = append(out, r.thingCandidate(inst, s))
		}
	}
	for _, id := range r.holders {
		inst := r.Instances[id]
		if inst == nil || len(out) >= targetReadMax {
			continue
		}
		add(inst)
		if list, ok := inst.Component.(*Collection); ok {
			for _, item := range list.all {
				add(r.Instances[item.ID])
			}
		}
	}
	return out
}

// linkedCandidates are the things of the noun the nearest level of the
// focus path that reaches any reaches through relations and associations,
// at the nearest distance (nearestReached, as key dispatch reads them).
func (r *Runtime) linkedCandidates(s targetScan) []TargetCandidate {
	if s.noun == "" || s.anyNoun() {
		return nil
	}
	for _, level := range r.Ancestry(s.focus) {
		found := r.nearestReached(s.noun, level)
		if len(found) == 0 {
			continue
		}
		out := make([]TargetCandidate, 0, min(len(found), targetReadMax))
		for _, a := range found[:min(len(found), targetReadMax)] {
			out = append(out, TargetCandidate{ID: a.Key, Label: a.Label, Noun: a.Noun, Instance: a.inst, dist: a.Dist})
		}
		return out
	}
	return nil
}

// recentCandidates are the things of the noun the newest history entries
// name (their target, the things they changed or answered), newest first.
func (r *Runtime) recentCandidates(s targetScan) []TargetCandidate {
	if s.noun == "" || s.anyNoun() {
		return nil
	}
	entries := r.HistoryOf(recentEntries)
	var out []TargetCandidate
	for i := len(entries) - 1; i >= 0 && len(out) < targetReadMax; i-- {
		out = append(out, r.entryCandidates(entries[i], s)...)
	}
	return out
}

// entryCandidates are the things of the scan's noun one history entry
// names: its target's instance when it is still live, the things it
// changed, and the things it answered.
func (r *Runtime) entryCandidates(e HistoryEntry, s targetScan) []TargetCandidate {
	var out []TargetCandidate
	if inst := r.Instances[e.Request.Target]; s.ofNoun(inst) {
		out = append(out, r.thingCandidate(inst, s))
	}
	refs := slices.Clone(e.Things)
	for _, change := range e.Changes {
		refs = append(refs, AnyRef(change.Noun, change.Key))
	}
	for _, ref := range refs {
		if noun, key, ok := SplitAnyRef(ref); ok && r.CanonicalNoun(noun) == s.noun {
			out = append(out, TargetCandidate{ID: key, Label: key, Noun: s.noun})
		}
	}
	return out
}

// linkedPolicy is one value of target.linked: whether a verb acts on a
// linked thing without asking (acts), and whether one more than a single
// hop away may be taken (distant), by whether the verb only reads or opens
// (its history kind is not a change).
type linkedPolicy struct {
	value  string
	choose func(reads bool) (acts, distant bool)
}

func (p linkedPolicy) settingValue() string { return p.value }

// linkedPolicies are target.linked's values; the first is the default.
var linkedPolicies = []linkedPolicy{
	{"ask-on-change", func(reads bool) (bool, bool) { return reads, true }},
	{"act", func(reads bool) (bool, bool) { return true, reads }},
	{"always-pick", func(bool) (bool, bool) { return false, false }},
}

// linkedActs reports whether op acts on a linked thing without asking under
// target.linked, and whether a distant one may be taken. Which verbs only
// read or open is the history kind op declares (HistoryKindOf: a fetch or
// an action reads or opens, a change changes), never a list of verb names.
func (r *Runtime) linkedActs(op Operation) (acts, distant bool) {
	return settingRow(r, TargetLinkedSetting, linkedPolicies).choose(op.HistoryKindOf() != HistoryChange)
}

// chooseCandidate is the candidate op acts on without asking: the first
// whose Why is in acts (DefaultActs when empty), or a linked one when
// target.linked lets op act on it; nil when none is, so the verb asks.
func (r *Runtime) chooseCandidate(op Operation, acts []Why, candidates []TargetCandidate) *TargetCandidate {
	if len(acts) == 0 {
		acts = DefaultActs
	}
	linked, distant := r.linkedActs(op)
	for i, c := range candidates {
		takesLinked := linked && c.Why == WhyLinked && c.Instance != "" && (distant || c.dist <= 1)
		if slices.Contains(acts, c.Why) || takesLinked {
			chosen := candidates[i]
			return &chosen
		}
	}
	return nil
}

// targetOf is the Target of op's parameter name at focus for a request
// with args: the given value when args holds one, else the ranked
// candidates and the one op acts on without asking. A name op does not
// declare is the request's own subject: a thing of op's noun, named by its
// instance (the CLI's --target). Each candidate carries the command that
// runs the request with it.
func (r *Runtime) targetOf(op Operation, name, focus string, args Arguments) Target {
	p, declared := op.Parameters[name]
	if !declared {
		p = Parameter{Noun: op.Noun()}
	}
	t := Target{Param: name, Noun: r.CanonicalNoun(p.Noun), Candidates: []TargetCandidate{}, All: r.allCommand(p.Noun)}
	if v, set := args[name]; set && !emptyProp(v) {
		given := TargetCandidate{ID: fmt.Sprint(v), Noun: t.Noun, Why: WhyGiven}
		t.Given, t.Chosen = true, &given
		return t
	}
	t.Candidates = r.candidatesFor(p, focus)
	for i, c := range t.Candidates {
		value := c.ID
		if !declared {
			value = cmp.Or(c.Instance, c.ID)
		}
		if op.Key != "" {
			t.Candidates[i].Command = r.commandWith(op, args, name, value)
		}
	}
	t.Chosen = r.chooseCandidate(op, p.Acts, t.Candidates)
	return t
}

// allCommand is the command that lists every thing of noun, "" when the
// noun has no list verb.
func (r *Runtime) allCommand(noun string) string {
	noun = r.CanonicalNoun(noun)
	if noun == "" || noun == AnyNoun {
		return ""
	}
	if _, ok := r.Operations.Find(Key(noun + ".list")); !ok {
		return ""
	}
	return r.DisplayNoun(noun) + " list"
}

// Target is the Target of the running verb's parameter param: the value
// the request gave, else the things found from the whole context, ranked,
// and the one the verb acts on without asking (Chosen, nil to ask). A
// handler that finds no chosen candidate answers Failed(c.NeedTarget(param,
// reason)), never a refusal.
func (c Ctx) Target(param string) Target {
	op, _ := c.Runtime.Operation(c.Invoked.Name)
	return c.Runtime.targetOf(op, param, focusOf(c.Context), c.Args)
}

// NeedTarget is the error a handler returns (Failed(c.NeedTarget("pane",
// "..."))) when it lacks the thing its parameter param names: a needs /
// target outcome carrying the Target, so the person is asked with its
// candidates (the picker, ask.target) and the CLI and MCP list them, each
// with its command.
func (c Ctx) NeedTarget(param, reason string) error {
	t := c.Target(param)
	return &MissingParameterError{Param: param, Reason: reason, Noun: t.Noun, Target: &t}
}

// SubjectTarget is the Target of q's own subject at the base focus: the
// things of its verb's noun found from the whole context, ranked, each
// with the command that runs q with it as --target. The CLI lists them
// under a command that named no --target and failed; MCP answers them for
// a request to act on the owner's selection or marks.
func (r *Runtime) SubjectTarget(q Request) Target {
	op, _ := r.Operations.Find(Key(r.CanonicalRequest(q).Key()))
	return r.targetOf(op, "target", r.BaseFocus(), q.Args)
}

// withTarget fills err, the answer of op's request q, when it is a needs /
// target (a MissingParameterError for a parameter that names a thing) that
// carries no Target: the Target of that parameter at origin, so every
// surface asks with the same ranked candidates. A dry run (checkOnly) and
// any other error are err as they are.
func (r *Runtime) withTarget(op Operation, q Request, origin string, err error, checkOnly bool) error {
	mp := needed(err)
	if checkOnly || mp == nil || mp.Noun == "" || mp.Target != nil {
		return err
	}
	t := r.targetOf(op, mp.Param, origin, q.Args)
	mp.Target = &t
	return err
}

// choicesMax is cli.choices-max: how many candidates a program is listed,
// 0 for every one.
func (r *Runtime) choicesMax() int {
	if s := r.Settings(); s != nil && s.Has(ChoicesMaxSetting) {
		return max(0, s.Int(ChoicesMaxSetting))
	}
	return 20
}

// ChoicesShown is o as a program (the CLI, MCP) is answered with it: a needs
// outcome for a thing of a noun lists, after its ranked candidates, the
// noun's things one paged read of its store gives (Why listed; only a
// store that pages, Store.Query, is read, and only for the rows still to
// show); then at most cli.choices-max choices and candidates (0: every
// one), Need.More counting the ones left out. q is the request o answers
// (the listed rows' commands run it); an empty q lists nothing more. Any
// outcome that needs nothing is o.
func (r *Runtime) ChoicesShown(o Outcome, q Request) Outcome {
	if o.Needs == nil {
		return o
	}
	need := *o.Needs
	limit := r.choicesMax()
	var more int
	need.Choices, more = r.listedChoices(need, q, limit)
	need.More += more
	if limit > 0 && len(need.Choices) > limit {
		need.More += len(need.Choices) - limit
		need.Choices = need.Choices[:limit]
	}
	if t := need.Target; t != nil && limit > 0 && len(t.Candidates) > limit {
		shown := *t
		shown.Candidates = t.Candidates[:limit]
		need.Target = &shown
	}
	o.Needs = &need
	return o
}

// listedChoices are need's choices and, when it needs a thing of a noun
// whose store pages, the first things of the noun not among them (one page
// of as many as are still to show), each with the command that runs q
// with it, and how many things of the noun the page did not hold.
func (r *Runtime) listedChoices(need Need, q Request, limit int) ([]ChoiceRow, int) {
	choices := slices.Clone(need.Choices)
	op, ok := r.Operations.Find(Key(r.CanonicalRequest(q).Key()))
	n := r.Nouns[r.CanonicalNoun(need.Noun)]
	if need.Target == nil || !ok || n == nil || n.Store == nil || n.Store.Query == nil || n.keyProp() == nil {
		return choices, 0
	}
	want := 0
	if limit > 0 {
		want = limit - len(choices)
		if want <= 0 {
			return choices, 0
		}
	}
	page, err := n.Store.Query(r.reachCtx(n.Name), ListQuery{Page: 1, PerPage: want})
	if err != nil {
		return choices, 0
	}
	for _, thing := range page.Items {
		key := fmt.Sprint(thing[n.keyProp().Name])
		if slices.ContainsFunc(choices, func(c ChoiceRow) bool { return c.ID == key }) {
			continue
		}
		label := n.displayLabel(func(name string) any { return thing[name] })
		choices = append(choices, ChoiceRow{ID: key, Label: label, Command: r.commandWith(op, q.Args, need.Param, key), Why: WhyListed})
	}
	return choices, max(0, page.Total-len(page.Items))
}
