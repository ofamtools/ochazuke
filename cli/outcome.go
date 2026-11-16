package cli

import (
	"encoding/json"
	"fmt"

	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

// cliRun is what a command's run notes for its answer: the verb as
// displayed, and the outcome of a request that changed nothing (its error
// is nil, so OutcomeOf cannot tell it).
type cliRun struct {
	verb    string
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
		for _, c := range o.Needs.Choices {
			line := "  " + c.ID
			if c.Command != "" {
				line = "  " + c.Command
			}
			if c.Label != "" && c.Label != c.ID {
				line += "  # " + c.Label
			}
			fmt.Fprintln(s.stderr(), line)
		}
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

// answer is the CLI's encoder: the run's error, or the nothing ran noted,
// as its Outcome (kept for an MCP caller in s.outcome), printed and turned
// into Run's error by cliAnswers.
func (s *Shell) answer(ran cliRun, err error) error {
	o := ochazuke.OutcomeOf(nil, err)
	if err == nil && ran.nothing != nil {
		o = *ran.nothing
	}
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
