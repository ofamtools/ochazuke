package ochazuke

import (
	"errors"
	"maps"
)

// MissingParameterError says a request lacks a value of the parameter
// Param: the runtime found it before the verb ran (validateArguments), or
// the verb's handler found it late (Ctx.Need). It is the one error that
// asks: wherever a person runs a verb in the TUI (a key, the palette, the
// footer, the help's enter) it opens the args form on that field with the
// values already given, and the confirmed form runs the same request again
// (askNeed). The CLI and the MCP gateway have no form: they answer with the
// error as it reads, Error().
type MissingParameterError struct {
	Param  string // the parameter the request needs
	Reason string // why, in the person's words ("" for a plain missing argument)
}

func (e *MissingParameterError) Error() string {
	if e.Reason == "" {
		return e.Param + " is required"
	}
	return e.Param + " is required: " + e.Reason
}

// Need is the error a handler returns (Failed(c.Need("project", "...")))
// when it finds late that it lacks a value of its parameter param: the
// person is asked for it, not told no.
func (c Ctx) Need(param, reason string) error {
	return &MissingParameterError{Param: param, Reason: reason}
}

// needed is the parameter err says a request needs, nil when it is not
// that error.
func needed(err error) *MissingParameterError {
	var mp *MissingParameterError
	if errors.As(err, &mp) {
		return mp
	}
	return nil
}

// askNeed opens the args form for q on the parameter err says it needs and
// reports whether it did: the form holds the request's values, marks that
// field required, shows the reason, and runs the request again through run
// when it is confirmed (esc leaves it with no effect). It does not when err
// is another error, the setting is off, a form is open, the verb has no
// such field a form can hold, or the request already gave that parameter a
// value (asking again would loop on a value the verb does not accept).
func (r *Runtime) askNeed(q Request, err error, run func(Request) error) bool {
	need := needed(err)
	if need == nil || !r.missingOpensForm() || r.Instances[ArgsFormID] != nil {
		return false
	}
	op, ok := r.Operations.Find(Key(q.Key()))
	if !ok {
		return false
	}
	p, ok := op.Parameters[need.Param]
	if !ok || !formable(p) || !emptyProp(r.formSources(q).Args[need.Param]) {
		return false
	}
	// The form replaces the one this request came from (one at a time): it
	// opens at that form's level, not as a modal from beneath the top one.
	defer func(before string) { r.reopening = before }(r.reopening)
	r.reopening = ArgsFormID
	e, openErr := r.openArgsForm(q, run, []string{need.Param})
	if openErr != nil {
		return false
	}
	if e != nil && need.Reason != "" {
		e.out.err = need.Reason
	}
	return true
}

// withValue is q with one more argument, q's own arguments untouched.
func withValue(q Request, name string, value any) Request {
	args := maps.Clone(q.Args)
	if args == nil {
		args = Arguments{}
	}
	args[name] = value
	return withArgs(q, args)
}

// refuseGuarded is the refusal of a request whose verb is guarded off where it
// acts (refusal), nil when it is available or runs away (runsAway).
func (r *Runtime) refuseGuarded(op Operation, q Request, focus string, mode ModeState) error {
	if r.Available(q, focus, mode) || r.runsAway(op, q, focus) {
		return nil
	}
	return r.refusal(op, q, focus, mode)
}

// refusal is why a request is not run where it is guarded off. A refusal
// that is really "this needs a thing" asks for it: the request acts on its
// noun's thing (a target parameter, targetParams), nothing of that noun is
// at focus and the request names none, so the answer is a
// MissingParameterError for that parameter (the form asks with the noun's
// picker; the CLI reads "x is required: no t is focused"). Any other
// refusal says which guards hold it back.
func (r *Runtime) refusal(op Operation, q Request, focus string, mode ModeState) error {
	if err := r.missingRole(q); err != nil {
		return err
	}
	if r.Settings().Bool(PaletteReachSetting) {
		if missing := r.missingTargets(op, q.Args, focus); len(missing) > 0 {
			noun := r.DisplayNoun(op.Parameters[missing[0]].Noun)
			return &ArgumentError{&MissingParameterError{Param: missing[0], Reason: "no " + noun + " is focused"}}
		}
	}
	var held []string
	for _, name := range r.VerbAvailability(q.Key()) {
		if guard := r.guard(name); guard != nil && !guard(r, KeyEvent{Mode: mode, Focus: focus, Level: focus, Args: q.Args}) {
			held = append(held, name)
		}
	}
	return &NotApplicableError{Key: r.DisplayKey(q.Key()), Held: held}
}

// NotApplicableError is a request refused because its verb is guarded off
// where it acts; Held names the guards that are false there, so a caller
// can say what would make the verb available. Error() is the plain
// refusal the CLI and the API always gave.
type NotApplicableError struct {
	Key  string
	Held []string
}

func (e *NotApplicableError) Error() string { return e.Key + " is not applicable now" }

// runsAway reports whether a request guarded off at focus runs anyway,
// headless: it is a person's, the verb is a standard one of a noun with a
// store (it needs no UI state, as the CLI runs it), the request names its
// thing by key, and no thing of that noun is on the focus path for the
// guard to judge. The palette reaches any action this way (PaletteReach).
func (r *Runtime) runsAway(op Operation, q Request, focus string) bool {
	if !r.Settings().Bool(PaletteReachSetting) || !personSource(r.history.source) || op.Standard == "" {
		return false
	}
	n := r.Nouns[op.Noun()]
	if n == nil || n.Store == nil || n.keyProp() == nil || emptyProp(q.Args[n.keyProp().Name]) {
		return false
	}
	if op.Disabled || r.unavailableFor(op, q.Args) != "" || r.missingRole(q) != nil {
		return false
	}
	return r.nounInstance(op.Noun(), focus) == ""
}
