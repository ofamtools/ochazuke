package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

// cliRun is what a command's run notes for its answer: the verb as
// displayed, the request it ran (the listed candidates' commands run it
// again), and the outcome of a request that changed nothing (its error is
// nil, so OutcomeOf cannot tell it).
type cliRun struct {
	verb    string
	request ochazuke.Request
	nothing *ochazuke.Outcome
}

// needsAnswered are cli.needs-status's values: whether a needs outcome is
// answered as one (exit status 3, MCP status choose) or as the plain
// error it was (exit status 1, MCP status failed). The first is the
// default.
var needsAnswered = []struct {
	value  string
	answer bool
}{
	{"choose", true},
	{"error", false},
}

// answersNeeds reports whether cli.needs-status answers a needs outcome as
// one.
func answersNeeds(r *ochazuke.Runtime) bool {
	value := ""
	if s := r.Settings(); s != nil && s.Has(ochazuke.NeedsStatusSetting) {
		value = s.String(ochazuke.NeedsStatusSetting)
	}
	for _, row := range needsAnswered {
		if row.value == value {
			return row.answer
		}
	}
	return needsAnswered[0].answer
}

// cliAnswer is the CLI's encoder of one outcome: what it prints and the
// error (the exit status) Run returns. err is the run's error.
type cliAnswer func(s *Shell, o ochazuke.Outcome, err error) error

// asError answers the run's error as it is (exit status 1, or 0 for none).
func asError(_ *Shell, _ ochazuke.Outcome, err error) error { return err }

// cliCauses are the causes the CLI answers otherwise than their kind: a
// confirm the script did not answer yes to stays a failure (exit status
// 1, "declined: PROMPT"), so a script without --yes never passes for one
// that ran.
var cliCauses = map[ochazuke.OutcomeCause]cliAnswer{
	ochazuke.CauseCancelled: asError,
}

// cliAnswers are the CLI's encoder of each outcome kind.
var cliAnswers = map[ochazuke.OutcomeKind]cliAnswer{
	ochazuke.OutcomeDone: asError,
	// Nothing to change is exit status 0 with the wording as a status:
	// line; a handler's Nothing already told it through the runtime (the
	// stderr sink), so only an answer that came as an error prints here.
	ochazuke.OutcomeNothing: func(s *Shell, o ochazuke.Outcome, err error) error {
		if err != nil {
			stderrSink{s.stderr()}.Info(o.Text())
		}
		return nil
	},
	// What it needs is exit status 3: the reason (oden reports the
	// error), one line per candidate on stderr (its command, else its id,
	// and its label), and the outcome as JSON on stdout when no person
	// reads it.
	ochazuke.OutcomeNeeds: func(s *Shell, o ochazuke.Outcome, err error) error {
		if !answersNeeds(s.Runtime) {
			return err
		}
		writeChoices(s.stderr(), o.Needs)
		if o.Command != "" {
			fmt.Fprintln(s.stderr(), "  "+o.Command)
		}
		if !s.terminal() {
			if data, jerr := json.Marshal(o); jerr == nil {
				fmt.Fprintln(s.stdout(), string(data))
			}
		}
		return &oden.ExitError{Code: ChoiceExitCode, Err: err}
	},
	ochazuke.OutcomeFailed: asError,
}

// writeChoices prints need's candidates, one line each (its command, else
// its id, then its label and why it was found), then how many were not
// listed (cli.choices-max) and the command that lists every thing of the
// noun.
func writeChoices(w io.Writer, need *ochazuke.Need) {
	for _, c := range need.Choices {
		line := "  " + c.ID
		if c.Command != "" {
			line = "  " + c.Command
		}
		var notes []string
		if c.Label != "" && c.Label != c.ID {
			notes = append(notes, c.Label)
		}
		if c.Why != "" {
			notes = append(notes, string(c.Why))
		}
		if len(notes) > 0 {
			line += "  # " + strings.Join(notes, " · ")
		}
		fmt.Fprintln(w, line)
	}
	all := ""
	if need.Target != nil {
		all = need.Target.All
	}
	switch {
	case need.More > 0 && all != "":
		fmt.Fprintf(w, "  %s  # %d more\n", all, need.More)
	case need.More > 0:
		fmt.Fprintf(w, "  # %d more\n", need.More)
	case all != "":
		fmt.Fprintf(w, "  %s  # every one\n", all)
	}
}

// targetHint is err, the failure of a command that named no --target, with
// the things of its noun it could act on (Runtime.SubjectTarget): ranked,
// each with the command that runs it with that --target, at most
// cli.choices-max of them, then how many more and the command that lists
// every one. The failure stays a failure (exit status 1): a handler's
// untyped failure is not read as a missing target. Any other error is err.
func (s *Shell) targetHint(q ochazuke.Request, err error) error {
	var bad *ochazuke.ArgumentError
	if q.Target != "" || errors.As(err, &bad) || ochazuke.OutcomeOf(nil, err).Kind != ochazuke.OutcomeFailed {
		return err
	}
	o := s.Runtime.ChoicesShown(ochazuke.Outcome{Kind: ochazuke.OutcomeNeeds, Needs: s.Runtime.SubjectTarget(q).Need()}, ochazuke.Request{})
	if len(o.Needs.Choices) == 0 {
		return err
	}
	var b strings.Builder
	writeChoices(&b, o.Needs)
	return &hintError{err: err, hint: strings.TrimRight(b.String(), "\n")}
}

// hintError is a failure with the --target values to run it again with.
type hintError struct {
	err  error
	hint string
}

func (e *hintError) Error() string { return e.err.Error() + "; pass --target, one of:\n" + e.hint }
func (e *hintError) Unwrap() error { return e.err }

// answer is the CLI's encoder: the run's error, or the nothing ran noted,
// as its Outcome (kept for an MCP caller in s.outcome), printed and turned
// into Run's error by cliAnswers. What it needs is listed as a program is
// answered (Runtime.ChoicesShown: the noun's things after its ranked
// candidates, at most cli.choices-max).
func (s *Shell) answer(ran cliRun, err error) error {
	o := ochazuke.OutcomeOf(nil, err)
	if err == nil && ran.nothing != nil {
		o = *ran.nothing
	}
	o = s.Runtime.ChoicesShown(o, ran.request)
	if o.Verb == "" {
		o.Verb = ran.verb
	}
	if o.Needs == nil && o.Kind == ochazuke.OutcomeNeeds {
		o.Needs = &ochazuke.Need{Choices: []ochazuke.ChoiceRow{}}
	}
	if s.outcome != nil {
		*s.outcome = o
	}
	if answer, ok := cliCauses[o.Cause]; ok {
		return answer(s, o, err)
	}
	return cliAnswers[o.Kind](s, o, err)
}
