package ochazuke

import (
	"fmt"
	"maps"
)

// VerbsDisabledSetting says what a verb whose feature setting is off
// (Operation.EnabledSetting) does when it is asked for by name: run runs
// it (the setting changes only what is offered by default), hide leaves it
// out of the palette and refuses it, so nothing offered fails.
const VerbsDisabledSetting = "verbs.disabled"

// disabledHidden reports a verb whose feature setting is off while
// verbs.disabled is not its default, run (that is, hide).
func (r *Runtime) disabledHidden(op Operation) bool {
	s := r.Settings()
	if op.EnabledSetting == "" || s.Bool(op.EnabledSetting) || !s.Has(VerbsDisabledSetting) {
		return false
	}
	def, _ := s.find(VerbsDisabledSetting).Default.(string)
	return s.String(VerbsDisabledSetting) != def
}

// explicitRule applies refuseGuarded to an explicit request: done with the
// refusal (a missing target asks) or with the Fallback's answer; not done
// when the verb itself runs (what it Needs is provided just before its
// handler, after its checks: invoke). The Fallback runs only for a request
// the original verb's agent rights allow; an agent they refuse or ask
// about goes on as the verb itself, whose checks refuse or ask (the
// confirmed request comes back here and falls back). A check (checkOnly:
// CheckRequest, a dry run) never falls back: it judges the verb as asked.
func (r *Runtime) explicitRule(c Context, op Operation, q Request, focus string, mode ModeState, checkOnly bool) (any, error, bool) {
	instead, err := r.refuseGuarded(op, q, focus, mode)
	switch {
	case err != nil:
		return nil, err, true
	case instead == nil || checkOnly || r.agentVerdict(c, q, op) != Allow():
		return nil, nil, false
	}
	value, err := r.runFallback(c, q, instead)
	return value, err, true
}

// asksFirst reports whether a verb with Confirm asks before it runs over
// subjects things: not for an agent the rights let pass (never past a verb
// that Loses: agentPassesPrompt), and not when undo fully restores the
// run.
func (r *Runtime) asksFirst(op Operation, subjects int) bool {
	return !r.agentPassesPrompt(op) && !r.undoRestores(op, subjects)
}

// confirmedFor reports whether q's Confirmed answers op's prompt: always,
// but for an agent's request to a verb that Loses, which only the owner's
// answer to the prompt confirms (E2: Confirmed is on the wire).
func (r *Runtime) confirmedFor(op Operation, q Request) bool {
	if !q.Confirmed {
		return false
	}
	return op.Loses == "" || !r.Actor().IsAgent() || q.agentApproved || r.agentApproved
}

// Requirement is one thing a verb needs before it runs: Kind says what
// (setup, daemon, session, program), Name which one, Met the guard that
// holds when it is there, and By the verb (noun.verb) that provides it,
// run first with Args. A requirement nothing can provide (By empty)
// answers a ChoiceAnswer naming Setup, the command that would.
type Requirement struct {
	Kind  string    `json:"kind"`
	Name  string    `json:"name"`
	Met   string    `json:"met,omitempty"`
	By    string    `json:"by,omitempty"`
	Args  Arguments `json:"args,omitempty"`
	Setup string    `json:"setup,omitempty"`
}

// fallbackRequest is the request a Fallback runs: its preset arguments,
// then the request's own where the fallback verb declares them; the
// request's target carries over.
func fallbackRequest(op Operation, q Request) *Request {
	f := *op.Fallback
	f.Args = maps.Clone(f.Args)
	if f.Args == nil {
		f.Args = Arguments{}
	}
	for name, v := range q.Args {
		if _, set := f.Args[name]; !set {
			f.Args[name] = v
		}
	}
	if f.Target == "" {
		f.Target = q.Target
	}
	// The fallback's own prompt is not answered by the original's
	// Confirmed: it asks for what it does.
	f.Confirmed = false
	return &f
}

// runFallback runs a verb's Fallback in place of it, with a notice. A
// chain of fallbacks that comes back to a verb it passed is a declaration
// error, answered before anything runs (fallbackLoops).
func (r *Runtime) runFallback(c Context, q Request, instead *Request) (any, error) {
	if r.fallbackLoops(q.Key()) {
		return nil, fmt.Errorf("%s: its fallback %s loops", q.Key(), instead.Key())
	}
	r.notice("%s: ran %s instead", r.DisplayKey(Key(q.Key()).Short()), r.DisplayKey(Key(instead.Key()).Short()))
	return r.ExecuteAt(c, *instead)
}

// fallbackLoops reports whether following key's Fallback chain comes back
// to a verb already passed.
func (r *Runtime) fallbackLoops(key string) bool {
	seen := map[string]bool{}
	for op, ok := r.Operation(key); ok && op.Fallback != nil; op, ok = r.Operation(op.Fallback.Key()) {
		if seen[op.Key.Short()] {
			return true
		}
		seen[op.Key.Short()] = true
	}
	return false
}

// provide runs, for each requirement whose Met guard is false at focus,
// the verb that provides it; one nothing provides answers a ChoiceAnswer
// naming its setup command. nil when every need is met or provided. It
// runs for every request about to run its handler, after its checks
// (agent rights first), whether or not the verb's guard held: a need is
// not a guard.
func (r *Runtime) provide(op Operation, q Request, focus string, mode ModeState) error {
	for _, need := range op.Needs {
		if guard := r.guard(need.Met); need.Met != "" && guard != nil && guard(r, KeyEvent{Mode: mode, Focus: focus, Level: focus, Args: q.Args}) {
			continue
		}
		if need.By == "" {
			answer := &ChoiceAnswer{Needs: need.Kind, Reason: fmt.Sprintf("%s needs the %s %s, which nothing here can provide now", r.DisplayKey(op.Key.Short()), need.Kind, need.Name)}
			if need.Setup != "" {
				answer.Choices = []ChoiceRow{{ID: need.Name, Label: "provide it, then run again", Command: need.Setup}}
			}
			return answer
		}
		noun, verb, _ := CutKey(need.By)
		if _, err := r.Execute(Request{Noun: noun, Verb: verb, Args: maps.Clone(need.Args)}); err != nil {
			return err
		}
		r.notice("%s: ran %s first (%s %s)", r.DisplayKey(op.Key.Short()), need.By, need.Kind, need.Name)
	}
	return nil
}

// undoRestores reports a verb whose run over subjects things undo fully
// restores and that declares no loss: a standard verb whose undo recreates
// what it removed (its row's Undo shape) on a store that records undo and
// can create under the old key (the undo's own condition, undoable); in a
// session whose history undo can reach (a one-shot CLI process exits with
// its history, and entries read back from the file are never undone). Such
// a verb does not ask first (no-refusals D7: confirm only on loss; owner
// 2026-11-13: drop the prompt on a delete undo fully restores). A store
// that says what a run costs (ConfirmFor: cascades, orphans) and a run
// over a selection of several things still ask, as a verb with Loses does.
func (r *Runtime) undoRestores(op Operation, subjects int) bool {
	if op.Loses != "" || op.Standard == "" || subjects > 1 || !r.Settings().HasSession() {
		return false
	}
	row, n := standardRow(op.Standard), r.Nouns[op.Noun()]
	if row == nil || row.Undo != undoByRecreate || n == nil || n.Store == nil {
		return false
	}
	s := n.Store
	return s.Undo && s.Create != nil && s.AcceptsKey && s.ConfirmFor == nil
}
