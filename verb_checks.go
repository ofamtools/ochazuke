package ochazuke

import (
	"errors"
	"fmt"
	"slices"
)

// Verb checks: an application hangs a check on any verb (Operation.Checks)
// that runs before the verb from every path (keys, the palette, the CLI,
// the API), after its guard and before its Confirm prompt. A check allows
// the request, asks first (the confirm modal, whose y runs the request
// marked Confirmed) or refuses it with a reason. The quit hook is one use:
// BeforeQuit hangs a check on app.quit, so an app with unsaved edits asks
// before quitting without wrapping the framework's verb, and
// app.quit {force: true} quits without asking.
//
// A request skips every check when it is Confirmed (the yes of a check's
// own question, or of the verb's Confirm) or carries force: true (a verb
// that should honour force declares the parameter; app.quit does).

// VerbCheck is one check on a verb. Name identifies it (a second check of
// the same name replaces the first); Check reads the request's context and
// arguments and returns a Verdict. Checks run in the order they were
// added; the first that does not allow decides.
type VerbCheck struct {
	Name  string
	Check func(r *Runtime, c Context, args Arguments) Verdict
}

// Verdict is a check's answer: the zero Verdict allows; Ask holds the
// question the confirm modal asks before the verb runs; Refuse holds the
// reason the request fails with.
type Verdict struct {
	Ask    string
	Refuse string
}

// Allow is the Verdict that lets the verb run.
func Allow() Verdict { return Verdict{} }

// AskFirst is the Verdict that asks prompt in the confirm modal first;
// y runs the verb (its checks skipped), n and esc leave it unrun.
func AskFirst(prompt string) Verdict { return Verdict{Ask: prompt} }

// Refuse is the Verdict that fails the request with reason.
func Refuse(reason string) Verdict { return Verdict{Refuse: reason} }

// AddVerbCheck hangs a check on the verb key names (aliases resolve). A
// check of the same name on that verb is replaced in place.
func (r *Runtime) AddVerbCheck(key string, check VerbCheck) error {
	if check.Name == "" || check.Check == nil {
		return errors.New("a verb check needs a name and a check")
	}
	op, ok := r.Operation(key)
	if !ok {
		return fmt.Errorf("unknown operation %q", key)
	}
	if i := slices.IndexFunc(op.Checks, func(v VerbCheck) bool { return v.Name == check.Name }); i >= 0 {
		op.Checks = slices.Clone(op.Checks)
		op.Checks[i] = check
	} else {
		op.Checks = append(slices.Clone(op.Checks), check)
	}
	r.Operations[op.Key()] = op
	return nil
}

// RemoveVerbCheck takes the check named name off the verb; false when it
// had none of that name.
func (r *Runtime) RemoveVerbCheck(key, name string) bool {
	op, ok := r.Operation(key)
	if !ok {
		return false
	}
	i := slices.IndexFunc(op.Checks, func(v VerbCheck) bool { return v.Name == name })
	if i < 0 {
		return false
	}
	op.Checks = slices.Delete(slices.Clone(op.Checks), i, i+1)
	r.Operations[op.Key()] = op
	return true
}

// VerbChecks names the checks on a verb, in the order they run.
func (r *Runtime) VerbChecks(key string) []string {
	op, ok := r.Operation(key)
	if !ok {
		return nil
	}
	out := make([]string, len(op.Checks))
	for i, v := range op.Checks {
		out[i] = v.Name
	}
	return out
}

// BeforeQuit hangs a check named name on app.quit: return AskFirst(prompt)
// while there is something to lose (unsaved edits), Refuse(reason) when
// quitting must not happen now, else Allow(). app.quit {force: true} skips
// it.
func (r *Runtime) BeforeQuit(name string, check func(r *Runtime, c Context) Verdict) error {
	return r.AddVerbCheck(AppNoun+".quit", VerbCheck{Name: name, Check: func(r *Runtime, c Context, _ Arguments) Verdict { return check(r, c) }})
}

// runChecks is the first verdict of op's checks that does not allow, the
// zero Verdict when every check allows or the request skips them.
func (r *Runtime) runChecks(c Context, q Request, op Operation) Verdict {
	if len(op.Checks) == 0 || q.Confirmed || q.Args["force"] == true {
		return Verdict{}
	}
	for _, v := range op.Checks {
		if got := v.Check(r, c, q.Args); got != (Verdict{}) {
			return got
		}
	}
	return Verdict{}
}
