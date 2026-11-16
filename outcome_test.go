package ochazuke

import (
	"errors"
	"fmt"
	"testing"
)

// TestOutcomeOf covers every row of the outcome table once, the done
// answer and the fallback for an untyped error.
func TestOutcomeOf(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		kind  OutcomeKind
		cause OutcomeCause
	}{
		{"done", nil, OutcomeDone, ""},
		{"declined", &DeclinedError{Prompt: "Delete?"}, OutcomeNothing, CauseCancelled},
		{"missing value", &MissingParameterError{Param: "name"}, OutcomeNeeds, CauseValue},
		{"missing target", &ArgumentError{&MissingParameterError{Param: "id", Noun: "task"}}, OutcomeNeeds, CauseTarget},
		{"choice", &ChoiceAnswer{Needs: "verb", Reason: "pick one", Choices: []ChoiceRow{{ID: "a"}}}, OutcomeNeeds, CauseChoice},
		{"setup", &ChoiceAnswer{Needs: "program", Cause: CauseSetup, Choices: []ChoiceRow{{ID: "git", Command: "brew install git"}}}, OutcomeNeeds, CauseSetup},
		{"not found", &NotFoundError{Noun: "task", ID: "x", Near: []string{"xa"}}, OutcomeNeeds, CauseChoice},
		{"unknown name", &UnknownNameError{Kind: "setting", Name: "colr"}, OutcomeNeeds, CauseChoice},
		{"ambiguous noun", &AmbiguousNounError{Word: "doc", Nouns: []string{"json.doc", "md.doc"}}, OutcomeNeeds, CauseChoice},
		{"read-only", &ReadOnlyError{Noun: "task", Prop: "id"}, OutcomeNothing, CauseKept},
		{"kept", &KeptError{Thing: "a", Change: TreeDelete, Reason: `"a" refuses deletion`}, OutcomeNothing, CauseKept},
		{"store refused", fmt.Errorf("no: %w", ErrTreeRefused), OutcomeNothing, CauseKept},
		{"nothing as an error", NothingErr(CauseNotHere, "save controls are disabled"), OutcomeNothing, CauseNotHere},
		{"not applicable", &NotApplicableError{Key: "task.done"}, OutcomeNothing, CauseNotHere},
		{"rights never", &RefusedError{Verb: "task.delete", Rule: "r", Reason: "never"}, OutcomeFailed, CauseRights},
		{"rights confirm", &RefusedError{Verb: "task.delete", Rule: "r", Reason: "ask", Approvable: true, Command: "app task delete"}, OutcomeNeeds, CauseConfirm},
		{"wrapped", fmt.Errorf("saving: %w", &NotFoundError{Noun: "task", ID: "x"}), OutcomeNeeds, CauseChoice},
		{"untyped", errors.New("disk full"), OutcomeFailed, CauseFailure},
		{"over the wire", &ArgumentError{&OutcomeError{Outcome: Outcome{Kind: OutcomeNeeds, Cause: CauseTarget}, Text: "id is required"}}, OutcomeNeeds, CauseTarget},
	}
	rows := map[string]bool{}
	for _, c := range cases {
		o := OutcomeOf("v", c.err)
		if o.Kind != c.kind || o.Cause != c.cause {
			t.Errorf("%s: %s/%s, want %s/%s", c.name, o.Kind, o.Cause, c.kind, c.cause)
		}
		for _, row := range outcomeTable {
			if c.err != nil && row.Find(c.err) != nil {
				rows[row.Type] = true
				break
			}
		}
	}
	for _, row := range outcomeTable {
		if !rows[row.Type] {
			t.Errorf("table row %s has no case", row.Type)
		}
	}
	if o := OutcomeOf(nil, &RefusedError{Approvable: true, Command: "app task delete"}); o.Command != "app task delete" {
		t.Errorf("confirm command %q", o.Command)
	}
}

// TestNothingLeavesNoRowAndTellsInfo: a handler answering Nothing records
// no history row, posts its wording as information, and ExecuteOutcome
// answers nothing with the cause.
func TestNothingLeavesNoRowAndTellsInfo(t *testing.T) {
	r := NewRuntime()
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	r.Define("task").Verb("retitle", func(c Ctx) Result { return Nothing(CauseKept, "the root has no name") }, "Retitle").History(HistoryChange)
	rows := len(r.HistoryOf(0))
	o := r.ExecuteOutcome(Request{Noun: "task", Verb: "retitle"})
	if o.Kind != OutcomeNothing || o.Cause != CauseKept {
		t.Fatalf("outcome %s/%s", o.Kind, o.Cause)
	}
	if n := len(r.HistoryOf(0)); n != rows {
		t.Fatalf("%d history rows, want %d", n, rows)
	}
	msgs := sink.Messages()
	if len(msgs) == 0 || msgs[len(msgs)-1].Kind != UIInfo || msgs[len(msgs)-1].Text != "task.retitle: kept: the root has no name" {
		t.Fatalf("messages %+v", msgs)
	}
}

// TestHistoryRecordsOutcome: the entry of a done, a nothing (an error the
// table answers with nothing), a needs (Ctx.Need on a parameter naming a
// noun: a target) and a failed run carries its outcome.
func TestHistoryRecordsOutcome(t *testing.T) {
	r := NewRuntime()
	r.Define("task").
		Verb("touch", func(c Ctx) Result { return Done(nil) }, "Touch").History(HistoryChange).
		Verb("lock", func(c Ctx) Result { return Failed(&ReadOnlyError{Noun: "task"}) }, "Lock").History(HistoryChange).
		Verb("save", func(c Ctx) Result { return Failed(errors.New("disk full")) }, "Save").History(HistoryChange).
		Verb("link", func(c Ctx) Result { return Failed(c.Need("other", "pick one")) }, "Link").History(HistoryChange).
		Params(map[string]Parameter{"other": {Type: "string", Noun: "task"}})
	for _, c := range []struct {
		verb  string
		kind  OutcomeKind
		cause OutcomeCause
	}{{"touch", OutcomeDone, ""}, {"lock", OutcomeNothing, CauseKept}, {"save", OutcomeFailed, CauseFailure}, {"link", OutcomeNeeds, CauseTarget}} {
		_, _ = r.Execute(Request{Noun: "task", Verb: c.verb})
		h := r.HistoryOf(1, HistoryChange)
		if len(h) != 1 || h[0].Result.Outcome != c.kind || h[0].Result.Cause != c.cause {
			t.Errorf("%s: %+v", c.verb, h)
		}
	}
}

// TestNearestLookupsAnswerNeeds: a resolver that finds no thing of its
// noun on the focus path (a table's list, a terminal) answers needs /
// target and a handler that lacks a value answers needs / value; none
// changes anything.
func TestNearestLookupsAnswerNeeds(t *testing.T) {
	r := NewRuntime()
	here := Context{Path: []string{"app"}}
	_, noList := r.tableList(here, nil)
	_, noTerminal := r.terminalOf(here, nil)
	for _, c := range []struct {
		name  string
		err   error
		kind  OutcomeKind
		cause OutcomeCause
	}{
		{"no focused list", noList, OutcomeNeeds, CauseTarget},
		{"no focused terminal", noTerminal, OutcomeNeeds, CauseTarget},
		{"missing keys", needValue("keys", ""), OutcomeNeeds, CauseValue},
	} {
		if o := OutcomeOf(nil, c.err); o.Kind != c.kind || o.Cause != c.cause {
			t.Errorf("%s: %s/%s (%v), want %s/%s", c.name, o.Kind, o.Cause, c.err, c.kind, c.cause)
		}
	}
}
