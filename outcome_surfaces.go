package ochazuke

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// How each surface shows the end of a run (ask-not-refuse O4). Every
// surface reads the same Outcome (OutcomeOf, Runtime.ExecuteOutcome) and
// has one encoder for the four kinds; which encoder runs is chosen by the
// request's source (RequestSource), never by a verb, a noun or a mode:
//
//   - a person at the UI (a key, a macro, the palette, the help's run):
//     done as the verb answers; nothing an info line, quiet or a notice
//     (outcome.nothing); needs the ask, a picker of the candidates or the
//     args form (ask.choices); failed the error line (answerPerson,
//     tellNothing);
//   - the CLI and MCP (package cli: Shell.answer, the MCP run's status);
//   - the socket (HandleCall: Response.Outcome);
//   - a hook (hookAnswer): never a picker, a notice for needs and failed;
//   - background work: nothing is shown.

// The settings of what a person sees at the end of a run.
const (
	// OutcomeNothingSetting is what a person is told when a request had
	// nothing to change: info (an info line until the next key, the
	// default), quiet (nothing) or notice (a notice that stays).
	OutcomeNothingSetting = "outcome.nothing"
	// AskChoicesSetting is how the TUI asks for what a request needs:
	// picker (the default: a picker of the candidates opens at once; the
	// args form when there are none) or form (the args form opens on the
	// needed field, as before).
	AskChoicesSetting = "ask.choices"
	// NeedsStatusSetting is how the CLI and MCP answer a request that
	// needs a value or a target: choose (the default: CLI exit status 3
	// with one command per candidate, MCP status choose) or error (exit
	// status 1 and the error text, MCP status failed, as before).
	NeedsStatusSetting = "cli.needs-status"
)

// NeedPickerKind is the kind of the picker a needs outcome opens.
const NeedPickerKind = "needs"

// nothingTellers are outcome.nothing's values: how a person is told that
// a request changed nothing. The first is the default.
type nothingTeller struct {
	value string
	tell  func(r *Runtime, text string)
}

func (t nothingTeller) settingValue() string { return t.value }

var nothingTellers = []nothingTeller{
	{"info", func(r *Runtime, text string) { r.UI().Info(text) }},
	{"quiet", func(*Runtime, string) {}},
	{"notice", func(r *Runtime, text string) { r.UI().Notify(text) }},
}

// tellNothing tells o, a request that changed nothing, as the surface of
// the running request's source tells it: a person by outcome.nothing, a
// hook or background work not at all, a program (the CLI's status line,
// MCP's messages, the API) as information.
func (r *Runtime) tellNothing(o Outcome) {
	source := RequestSource(r.history.source)
	switch {
	case source.Person():
		settingRow(r, OutcomeNothingSetting, nothingTellers).tell(r, o.Text())
	case source.Class() == RequestFromHook, source.Background():
	default:
		r.UI().Info(o.Text())
	}
}

// settingRow is the row of rows whose value the setting name holds, the
// first row when the setting is unset or holds no row's value.
func settingRow[T interface{ settingValue() string }](r *Runtime, name string, rows []T) T {
	value := ""
	if s := r.Settings(); s != nil && s.Has(name) {
		value = s.String(name)
	}
	for _, row := range rows {
		if row.settingValue() == value {
			return row
		}
	}
	return rows[0]
}

// requestOutcome is err, the answer to q, as an Outcome with the verb as
// displayed.
func (r *Runtime) requestOutcome(q Request, err error) Outcome {
	o := OutcomeOf(nil, err)
	o.Verb = r.DisplayKey(r.canonicalRequest(q).Key())
	return o
}

// answerPerson is the TUI's encoder of a person's request that answered
// err: nothing is told (tellNothing) and answers nil; needs is asked for
// (askOutcome) and answers nil when the ask opened; anything else (failed,
// a need nothing here can ask for) is err, for the caller's error line.
// run runs the request again with what the ask adds; held says a thing
// holds the request (runOrAsk).
func (r *Runtime) answerPerson(q Request, err error, run func(Request) error, held bool) error {
	o := r.requestOutcome(q, err)
	switch o.Kind {
	case OutcomeNothing:
		r.tellNothing(o)
		return nil
	case OutcomeNeeds:
		if r.askOutcome(q, o, run, held) {
			return nil
		}
	}
	return err
}

// askWay is one way the TUI asks for what an outcome needs; it reports
// whether it opened.
type askWay func(r *Runtime, q Request, o Outcome, run func(Request) error, held bool) bool

// askWays are ask.choices's values: the ways tried in order. The first is
// the default.
type askWays struct {
	value string
	ways  []askWay
}

func (a askWays) settingValue() string { return a.value }

var askChoices = []askWays{
	{"picker", []askWay{(*Runtime).pickNeed, (*Runtime).formNeed}},
	{"form", []askWay{(*Runtime).formNeed}},
}

// askTargets are ask.target's values: the ways a need of a thing of a noun
// (Need.Target) is asked for, tried in order. The first is the default:
// the args form with the field's picker on top (its ranked candidates
// first, then every thing of the noun), else a picker of the candidates.
var askTargets = []askWays{
	{"picker", []askWay{(*Runtime).pickTarget, (*Runtime).pickNeed}},
	{"form", []askWay{(*Runtime).formNeed, (*Runtime).pickNeed}},
}

// targetPicker is one value of ask.target as the args form reads it when
// it opens for a request's missing arguments (formForMissing): whether the
// start field's picker opens on top at once.
type targetPicker struct {
	value string
	opens bool
}

func (t targetPicker) settingValue() string { return t.value }

// targetPickers are ask.target's values for a form that opens before the
// request runs; the first is the default.
var targetPickers = []targetPicker{{"picker", true}, {"form", false}}

// picksTarget reports whether ask.target opens a needed thing's picker
// over the args form at once; never with subject.ask off.
func (r *Runtime) picksTarget() bool {
	return r.subjectAsks() && settingRow(r, AskTargetSetting, targetPickers).opens
}

// askOutcome asks a person for what o needs, and reports whether an ask
// opened: a thing of a noun by ask.target, anything else by ask.choices.
func (r *Runtime) askOutcome(q Request, o Outcome, run func(Request) error, held bool) bool {
	if o.Needs == nil {
		return false
	}
	setting, table := AskChoicesSetting, askChoices
	if o.Needs.Target != nil {
		setting, table = AskTargetSetting, askTargets
	}
	for _, way := range settingRow(r, setting, table).ways {
		if way(r, q, o, run, held) {
			return true
		}
	}
	return false
}

// needParam is the parameter of q's verb that what need lacks goes into:
// the parameter it names, else the argument that held the value it says
// named nothing (Need.Given); "" when the verb has neither.
func (r *Runtime) needParam(q Request, need *Need) string {
	op, ok := r.Operations.Find(Key(q.Key()))
	if !ok {
		return ""
	}
	if _, ok := op.Parameters[need.Param]; ok {
		return need.Param
	}
	if need.Given == "" {
		return ""
	}
	for _, name := range slices.Sorted(maps.Keys(q.Args)) {
		if _, ok := op.Parameters[name]; ok && fmt.Sprint(q.Args[name]) == need.Given {
			return name
		}
	}
	return ""
}

// formNeed opens the args form for q on the parameter o needs and reports
// whether it did: the form holds the request's values, marks that field
// required, shows the reason, and runs the request again through run when
// it is confirmed (esc leaves it with no effect). It does not when the
// verb has no such field a form can hold, the setting
// palette.missing-opens-form is off, a form is open, or the request
// already gave that parameter a value (asking again would loop on a value
// the verb does not accept).
func (r *Runtime) formNeed(q Request, o Outcome, run func(Request) error, held bool) bool {
	param := r.needParam(q, o.Needs)
	if param == "" || !r.missingOpensForm() || r.Instances[ArgsFormID] != nil {
		return false
	}
	op, _ := r.Operations.Find(Key(q.Key()))
	if !formable(op.Parameters[param]) || !emptyProp(r.formSources(q).Args[param]) {
		return false
	}
	// The form replaces the one this request came from (one at a time): it
	// opens at that form's level, not as a modal from beneath the top one.
	defer func(before string) { r.reopening = before }(r.reopening)
	r.reopening = ArgsFormID
	e, openErr := r.openArgsForm(q, run, []string{param}, held)
	if openErr != nil {
		return false
	}
	if e != nil && o.Reason != "" {
		e.out.err = o.Reason
	}
	return true
}

// needRows are the picker rows of what o needs: the candidates the
// outcome carries, in its order, each answering to why it was found in the
// filter. A need of a thing of a noun asks with the field's picker first
// (pickTarget), which follows them with every thing of the noun.
func needRows(o Outcome) []PickerRow {
	rows := make([]PickerRow, 0, len(o.Needs.Choices))
	for _, c := range o.Needs.Choices {
		choice := Choice{ID: c.ID, Label: c.Label}
		if c.Why != "" {
			choice.Keywords = []string{string(c.Why)}
		}
		rows = append(rows, choice.Row())
	}
	return rows
}

// pickTarget opens the args form on the parameter o needs, as formNeed
// does, with that field's picker on top at once (ask.target picker): the
// subject picker, its ranked candidates as the here source, then every
// thing of the noun, flat and filterable, then the walk. It reports
// whether the form opened. With subject.ask off it is formNeed alone, as
// before the ask.
func (r *Runtime) pickTarget(q Request, o Outcome, run func(Request) error, held bool) bool {
	if !r.formNeed(q, o, run, held) {
		return false
	}
	if e := r.RecordEditor(ArgsFormID); e != nil && r.picksTarget() {
		_ = e.suggestStart()
	}
	return true
}

// pickNeed opens a picker of the candidates o carries for the parameter
// it needs and reports whether it did; the pick runs the request again
// through run with that parameter set to the picked id. It does not when
// the outcome carries no candidate or names no parameter of the verb.
func (r *Runtime) pickNeed(q Request, o Outcome, run func(Request) error, _ bool) bool {
	param := r.needParam(q, o.Needs)
	rows := needRows(o)
	if param == "" || len(rows) == 0 {
		return false
	}
	_, err := r.OpenPicker(PickerSpec{
		Kind: NeedPickerKind, Title: o.Text(), Plural: "choices", Noun: o.Needs.Noun,
		NoPropFilters: true, NoSort: true, Rows: rows,
		// The request's own answer is the pick's (a failure it reports
		// is shown as the request's error line).
		Choose: func(row PickerRow) Result { return Result{err: run(withValue(q, param, row.ID))} },
	})
	return err == nil
}

// hookAnswer tells what a hook row's request answered with err: nothing
// is not told; needs is a notice naming what it needs and the commands
// that give it (a hook never opens a picker); failed is the failure
// notice.
func (r *Runtime) hookAnswer(row HookRow, q Request, err error) {
	o := r.requestOutcome(q, err)
	switch o.Kind {
	case OutcomeNothing:
	case OutcomeNeeds:
		o = r.ChoicesShown(o, Request{}) // at most cli.choices-max commands
		text := o.Text()
		if !strings.HasPrefix(text, o.Verb) {
			text = o.Verb + ": " + text
		}
		text = fmt.Sprintf("hook %s → %s", row.On, text)
		var commands []string
		for _, c := range o.Needs.Choices {
			if c.Command != "" {
				commands = append(commands, c.Command)
			}
		}
		if o.Command != "" {
			commands = append(commands, o.Command)
		}
		if len(commands) > 0 {
			text += "; run: " + strings.Join(commands, ", ")
		}
		r.UI().Notify(text)
	default:
		r.UI().Notify(hookFailure(row, err))
	}
}
