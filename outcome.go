package ochazuke

import (
	"errors"
	"strings"
)

// OutcomeKind is how a run ended: done, nothing, needs or failed. The
// test that separates them is who can change it: nobody needs to (done,
// nothing), the caller by giving a value or a target (needs), the
// environment, the developer or the owner's rights rules (failed). A run is
// never failed because of the state the person left the UI in.
type OutcomeKind string

// The four outcome kinds.
const (
	OutcomeDone    OutcomeKind = "done"    // it ran; the answer is its value
	OutcomeNothing OutcomeKind = "nothing" // it ran and there was nothing to change
	OutcomeNeeds   OutcomeKind = "needs"   // it changed nothing: it lacks a value or a target the caller can give
	OutcomeFailed  OutcomeKind = "failed"  // it tried and the world or the program said no
)

// OutcomeCause says why a run ended as it did, under its kind; each cause
// has one wording (Outcome.Text).
type OutcomeCause string

// The causes, by kind. A done outcome has none.
const (
	CauseKept      OutcomeCause = "kept"      // nothing: the thing keeps itself (read-only, fixed)
	CauseUnchanged OutcomeCause = "unchanged" // nothing: already so, the value is the same
	CauseEmpty     OutcomeCause = "empty"     // nothing: there is nothing to act on
	CauseNotHere   OutcomeCause = "not-here"  // nothing: the verb does nothing here (disabled, hidden)
	CauseCancelled OutcomeCause = "cancelled" // nothing: the person declined a confirm

	CauseTarget  OutcomeCause = "target"  // needs: a thing of a noun
	CauseValue   OutcomeCause = "value"   // needs: a parameter's value
	CauseChoice  OutcomeCause = "choice"  // needs: one of the listed candidates
	CauseSetup   OutcomeCause = "setup"   // needs: something set up first (a program, a daemon)
	CauseConfirm OutcomeCause = "confirm" // needs: the owner's confirmation

	CauseFailure     OutcomeCause = "failure"     // failed: disk, network, a process, any untyped error
	CauseDeclaration OutcomeCause = "declaration" // failed: a developer's declaration error
	CauseRights      OutcomeCause = "rights"      // failed: agent rights say never
)

// Outcome is the answer every run gives, from any route: its kind, the
// verb as displayed, the value (done, and nothing when the verb answers its
// state), the cause, one reason sentence, what it needs (needs only) and a
// command the owner or the caller runs (rights to confirm, setup). Err is
// the original error, for errors.As. OutcomeOf builds one from a run's
// (value, error); Runtime.ExecuteOutcome runs a request and answers one.
type Outcome struct {
	Kind    OutcomeKind  `json:"outcome"`
	Verb    string       `json:"verb,omitempty"`
	Value   any          `json:"value,omitempty"`
	Cause   OutcomeCause `json:"cause,omitempty"`
	Reason  string       `json:"reason,omitempty"`
	Needs   *Need        `json:"needs,omitempty"`
	Command string       `json:"command,omitempty"`
	Err     error        `json:"-"`
}

// Need is what a needs outcome lacks: the parameter ("target" for the
// subject), the noun its value names, what it takes, the value the caller
// gave that named nothing (Given: a not-found id, an unknown name; a
// surface reruns the request with a candidate in the argument that held
// it), the Target when it lacks a thing of a noun (its ranked candidates
// and the command that lists every thing), the candidates as choices, each
// with its exact command when one is known, and how many candidates a
// program was not listed (More, cli.choices-max).
type Need struct {
	Param   string      `json:"param,omitempty"`
	Noun    string      `json:"noun,omitempty"`
	Accepts string      `json:"accepts,omitempty"`
	Given   string      `json:"given,omitempty"`
	Target  *Target     `json:"target,omitempty"`
	Choices []ChoiceRow `json:"choices"`
	More    int         `json:"more,omitempty"`
}

// outcomeRow is one row of the table OutcomeOf reads: the typed error it
// matches (errors.As through Find), the kind and cause it answers, and an
// optional fill that copies the error's own data into the outcome (its
// needs, its command, a cause the error carries). Type names the error for
// the table's readers and tests.
type outcomeRow struct {
	Type  string
	Find  func(error) error
	Kind  OutcomeKind
	Cause OutcomeCause
	Fill  func(error, *Outcome)
}

// as is the Find of a row for the error type E.
func as[E error](err error) error {
	var target E
	if errors.As(err, &target) {
		return target
	}
	return nil
}

// is is the Find of a row for the sentinel error target.
func is(target error) func(error) error {
	return func(err error) error {
		if errors.Is(err, target) {
			return target
		}
		return nil
	}
}

// outcomeTable maps today's typed errors to an outcome, first match wins.
// Any error no row matches is failed / failure. Order matters only where
// one error wraps another: the narrower type first.
var outcomeTable = []outcomeRow{
	{Type: "*OutcomeError", Find: as[*OutcomeError], Fill: fillOutcome},
	{Type: "*DeclinedError", Find: as[*DeclinedError], Kind: OutcomeNothing, Cause: CauseCancelled},
	{Type: "*MissingParameterError", Find: as[*MissingParameterError], Kind: OutcomeNeeds, Cause: CauseValue, Fill: fillMissing},
	{Type: "*ChoiceAnswer", Find: as[*ChoiceAnswer], Kind: OutcomeNeeds, Cause: CauseChoice, Fill: fillChoice},
	{Type: "*NotFoundError", Find: as[*NotFoundError], Kind: OutcomeNeeds, Cause: CauseChoice, Fill: fillNotFound},
	{Type: "*UnknownNameError", Find: as[*UnknownNameError], Kind: OutcomeNeeds, Cause: CauseChoice, Fill: fillUnknownName},
	{Type: "*AmbiguousNounError", Find: as[*AmbiguousNounError], Kind: OutcomeNeeds, Cause: CauseChoice, Fill: fillAmbiguous},
	{Type: "*ReadOnlyError", Find: as[*ReadOnlyError], Kind: OutcomeNothing, Cause: CauseKept, Fill: fillReadOnly},
	{Type: "*KeptError", Find: as[*KeptError], Kind: OutcomeNothing, Cause: CauseKept},
	{Type: "ErrTreeRefused", Find: is(ErrTreeRefused), Kind: OutcomeNothing, Cause: CauseKept},
	{Type: "*NotApplicableError", Find: as[*NotApplicableError], Kind: OutcomeNothing, Cause: CauseNotHere},
	{Type: "*RefusedError", Find: as[*RefusedError], Kind: OutcomeFailed, Cause: CauseRights, Fill: fillRefused},
}

// OutcomeOf classifies a run's answer: no error is done with the value;
// an error is looked up in the table of typed errors (outcomeTable), and
// one no row matches is failed / failure. Reason is the error's own text
// (or a reason the error carries); Verb is left for the caller to set
// (Runtime.ExecuteOutcome sets it).
func OutcomeOf(value any, err error) Outcome {
	if err == nil {
		return Outcome{Kind: OutcomeDone, Value: value}
	}
	for _, row := range outcomeTable {
		found := row.Find(err)
		if found == nil {
			continue
		}
		o := Outcome{Kind: row.Kind, Cause: row.Cause, Reason: err.Error(), Err: err}
		if row.Fill != nil {
			row.Fill(found, &o)
		}
		return o
	}
	return Outcome{Kind: OutcomeFailed, Cause: CauseFailure, Reason: err.Error(), Err: err}
}

// fillOutcome: an outcome already classified (one that crossed the
// socket, OutcomeError) is itself, with the error it arrived as.
func fillOutcome(err error, o *Outcome) {
	e := err.(*OutcomeError)
	err = o.Err
	*o = e.Outcome
	o.Err = err
}

// fillMissing: a parameter whose value names a thing of a noun needs a
// target, any other a value; the reason is the error's own reason, ""
// for a plain missing argument (the wording says what it needs). A
// target's ranked candidates (MissingParameterError.Target) are its
// choices, each with its command.
func fillMissing(err error, o *Outcome) {
	e := err.(*MissingParameterError)
	o.Needs = &Need{Param: e.Param, Noun: e.Noun, Choices: []ChoiceRow{}}
	if e.Target != nil {
		o.Needs = e.Target.Need()
		o.Needs.Param, o.Needs.Noun = e.Param, e.Noun
	}
	if e.Noun != "" || e.Target != nil {
		o.Cause = CauseTarget
	}
	o.Reason = e.Reason
}

// fillChoice: the answer's candidates, and the cause it declares (setup for
// a requirement nothing here provides).
func fillChoice(err error, o *Outcome) {
	e := err.(*ChoiceAnswer)
	if e.Cause != "" {
		o.Cause = e.Cause
	}
	o.Needs = &Need{Param: e.Needs, Choices: append([]ChoiceRow{}, e.Choices...)}
	if o.Cause == CauseSetup && len(e.Choices) > 0 {
		o.Command = e.Choices[0].Command
	}
}

// fillNotFound: the near ids are the candidates.
func fillNotFound(err error, o *Outcome) {
	e := err.(*NotFoundError)
	o.Needs = &Need{Noun: e.Noun, Given: e.ID, Choices: nameRows(e.Near)}
}

// fillUnknownName: the near names are the candidates; the kind names what
// is needed (a parameter, a setting, a verb).
func fillUnknownName(err error, o *Outcome) {
	e := err.(*UnknownNameError)
	o.Needs = &Need{Param: e.Kind, Given: e.Name, Choices: nameRows(e.Near)}
}

// fillAmbiguous: each noun the word reaches, in full, is a candidate.
func fillAmbiguous(err error, o *Outcome) {
	e := err.(*AmbiguousNounError)
	o.Needs = &Need{Param: "noun", Choices: nameRows(e.Nouns)}
}

// fillReadOnly: the declared reason, when there is one.
func fillReadOnly(err error, o *Outcome) {
	if e := err.(*ReadOnlyError); e.Reason != "" {
		o.Reason = e.Reason
	}
}

// fillRefused: a right the owner can approve (confirm, refused headless)
// needs the owner's confirmation and names the command they run; any other
// stays a rights failure. The reason is the refusal's own.
func fillRefused(err error, o *Outcome) {
	e := err.(*RefusedError)
	o.Reason = e.Reason
	o.Command = e.Command
	if e.Approvable {
		o.Kind, o.Cause = OutcomeNeeds, CauseConfirm
	}
}

// nameRows are candidates of bare names, each its own id.
func nameRows(names []string) []ChoiceRow {
	out := make([]ChoiceRow, 0, len(names))
	for _, n := range names {
		out = append(out, ChoiceRow{ID: n})
	}
	return out
}

// outcomeWording is the one wording of each cause: the text before the
// reason, with VERB standing for the verb as displayed and ACT for its
// verb word. A cause with no row is the reason alone.
var outcomeWording = []struct {
	Cause  OutcomeCause
	Format string
}{
	{CauseKept, "VERB: kept"},
	{CauseUnchanged, "VERB: nothing to change"},
	{CauseEmpty, "VERB: nothing to ACT"},
	{CauseNotHere, "VERB: does nothing here"},
	{CauseCancelled, "VERB: not run"},
	{CauseTarget, "VERB needs a NOUN"},
	{CauseValue, "VERB needs PARAM"},
	{CauseConfirm, "VERB"},
}

// Text is the outcome in its one wording, for the status line, the CLI's
// stderr and MCP's reason: "task.retitle: kept: the root has no key",
// "pane.attach needs a pane: no pane is focused". A done outcome is "";
// a cause with no wording, or an outcome with no verb, is the reason.
func (o Outcome) Text() string {
	if o.Kind == OutcomeDone {
		return ""
	}
	format := ""
	for _, w := range outcomeWording {
		if w.Cause == o.Cause {
			format = w.Format
		}
	}
	if format == "" || o.Verb == "" {
		return o.Reason
	}
	act := o.Verb
	if i := strings.LastIndexAny(act, ". "); i >= 0 {
		act = act[i+1:]
	}
	var param, noun string
	if o.Needs != nil {
		param, noun = o.Needs.Param, o.Needs.Noun
	}
	head := strings.NewReplacer("VERB", o.Verb, "ACT", act, "NOUN", noun, "PARAM", param).Replace(format)
	if o.Cause == CauseCancelled || o.Reason == "" {
		return head
	}
	return head + ": " + o.Reason
}

// OutcomeError is an outcome that crossed a boundary as an error (the
// socket's Response.Outcome, read back by a CLI or MCP client): OutcomeOf
// answers the outcome it carries, so the client encodes it as the runtime
// classified it. Text is the original error's text.
type OutcomeError struct {
	Outcome Outcome
	Text    string
}

func (e *OutcomeError) Error() string { return e.Text }

// DeclinedError is a confirm the person answered no to: the request ran
// nothing. The CLI answers it when a prompt is declined.
type DeclinedError struct {
	Prompt string `json:"prompt"`
}

func (e *DeclinedError) Error() string { return "declined: " + e.Prompt }

// Nothing is the result of a handler that ran and found nothing to change:
// cause says why (CauseKept, CauseUnchanged, CauseEmpty, CauseNotHere), the
// reason in the person's words. Like Ctx.NothingChanged the request leaves
// no history row and no undo record; the reason is told on the status
// line as information (Ctx.Info), never a modal or an error.
func Nothing(cause OutcomeCause, reason string) Result {
	return Result{nothing: &Outcome{Kind: OutcomeNothing, Cause: cause, Reason: reason}}
}

// ExecuteOutcome runs q at the current focus as Execute does and answers
// its Outcome: the classified (value, error), or nothing when the handler
// said it changed nothing (Nothing, Ctx.NothingChanged), with the verb as
// displayed.
func (r *Runtime) ExecuteOutcome(q Request) Outcome {
	value, nothing, err := r.executeAtNoting(r.Context(), q, nil)
	o := OutcomeOf(value, err)
	if nothing != nil {
		o = *nothing
		o.Value = value
	}
	o.Verb = r.DisplayKey(r.canonicalRequest(q).Key())
	return o
}
