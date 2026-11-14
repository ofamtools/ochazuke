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
// when the verb itself runs.
func (r *Runtime) explicitRule(c Context, op Operation, q Request, focus string, mode ModeState, checkOnly bool) (any, error, bool) {
	instead, err := r.refuseGuarded(op, q, focus, mode)
	switch {
	case err != nil:
		return nil, err, true
	case instead != nil && !checkOnly:
		v, err := r.runFallback(c, q, instead)
		return v, err, true
	}
	return nil, nil, false
}

// asksFirst reports whether a verb with Confirm asks before it runs: not
// for an agent the rights let pass, and not when undo fully restores it.
func (r *Runtime) asksFirst(op Operation) bool {
	return !r.agentPassesPrompt(op) && !r.undoRestores(op)
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
	f.Confirmed = q.Confirmed
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
// naming its setup command. nil when every need is met or provided.
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

// undoRestores reports a verb whose run undo fully restores and that
// declares no loss: a standard verb whose undo recreates what it removed
// (its row's Undo shape) on a store that records undo. Such a verb does not
// ask first (no-refusals D7: confirm only on loss; owner 2026-11-13: drop
// the prompt on a delete undo fully restores). A verb with Loses always
// asks.
func (r *Runtime) undoRestores(op Operation) bool {
	if op.Loses != "" || op.Standard == "" {
		return false
	}
	row, n := standardRow(op.Standard), r.Nouns[op.Noun()]
	return row != nil && row.Undo == undoByRecreate && n != nil && n.Store != nil && n.Store.Undo
}
