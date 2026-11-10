package ochazuke

import (
	"fmt"
	"maps"
	"strings"
)

// Argument sources: where an omitted argument comes from. Two places
// declare one, and both read it through fillFrom:
//
//   - a binding row's From (Binding.FromRow, FromKey, ...), read when the
//     key runs (runRow), with the key's own sources (the key, the count);
//   - a parameter's From (Parameter.From), read by every route when the
//     request executes (execute): keys, the help's unbound list, the
//     palette, the args form, a request that names a target (API, MCP,
//     catalog run). The CLI has no row, so it contributes nothing there.
//
// The order is: an argument given, the row's From, the parameter's From,
// Focus, the default, then the form or chooser for what is still missing.

// fillFrom fills args from sources (argument name -> source) read for ev,
// and answers the arguments (args is not changed). replace lets a source
// overwrite an argument already given (a binding row's From over the row's
// own arguments); otherwise an argument given is kept. A "row" source that
// reads nothing leaves the argument out; a blank target is an error, except
// for an argument optional reports as optional (a parameter that is not
// Required): the thing the target would name is then simply not given, so a
// verb with a collection form (no target) runs when the request's target is
// a list or page standing for the collection, not a row.
func (r *Runtime) fillFrom(args Arguments, from map[string]string, ev KeyEvent, replace bool, optional func(arg string) bool) (Arguments, error) {
	out := maps.Clone(args)
	if out == nil {
		out = Arguments{}
	}
	for arg, source := range from {
		if _, given := out[arg]; given && !replace && !emptyProp(out[arg]) {
			continue
		}
		v := r.argument(source, ev)
		if field, rowField := rowSourceField(source); rowField {
			text, isText := v.(string)
			if v == nil || isText && strings.TrimSpace(text) == "" {
				if arg == "target" && (optional == nil || !optional(arg)) {
					row := ev.Focus
					if inst := r.sourceRow(ev); inst != nil {
						row = inst.ID
					}
					return nil, fmt.Errorf("row %q: target field %q is empty", row, field)
				}
				if replace {
					delete(out, arg)
				}
				continue
			}
		}
		if v != nil {
			out[arg] = v
		}
	}
	return out, nil
}

// CheckParameterSources refuses a parameter whose From is not "row" or
// "row:<field>" (one word, a well-formed field).
func CheckParameterSources(o Operation) error {
	for name, p := range o.Parameters {
		if p.From == "" {
			continue
		}
		if len(strings.Fields(p.From)) != 1 {
			return fmt.Errorf("%s: parameter %s has from %q: name one source", o.Key.Short(), name, p.From)
		}
		if err := validSource(p.From); err != nil || !isRowSource(p.From) {
			return fmt.Errorf("%s: parameter %s has from %q: only %q and %q name a source without a key", o.Key.Short(), name, p.From, SourceRow, SourceRow+":<field>")
		}
	}
	return nil
}

// hasSources reports whether any parameter declares a From.
func (op Operation) hasSources() bool {
	for _, p := range op.Parameters {
		if p.From != "" {
			return true
		}
	}
	return false
}

func isRowSource(source string) bool {
	if source == SourceRow {
		return true
	}
	_, ok := rowSourceField(source)
	return ok
}

// sourceStart is the instance a request's parameter sources read from: its
// target, else, for a person at the UI, the focus. "" for a program that
// names no target: it acts on what it names, never on the owner's focus.
func (r *Runtime) sourceStart(q Request, focus string) string {
	if q.Target != "" && q.Target != "focused-input" {
		if id, err := r.TargetInstance(q.Target); err == nil {
			return id
		}
		return ""
	}
	if personSource(r.history.source) {
		return focus
	}
	return ""
}

// parameterSources is q's arguments with the omitted ones filled from
// their parameters' From (Parameter.From), read for the row at start (see
// sourceStart). An argument given is kept.
func (r *Runtime) parameterSources(op Operation, q Request, focus string) (Arguments, error) {
	return r.sourcesAt(op, q, r.sourceStart(q, focus))
}

// sourcesAt fills q's omitted arguments from op's parameters' From, read
// for the row at start ("" fills nothing). An explicit target of the
// subject's noun already names that subject, ahead of its row source.
func (r *Runtime) sourcesAt(op Operation, q Request, start string) (Arguments, error) {
	from := map[string]string{}
	for name, p := range op.Parameters {
		if q.Target != "" && p.Focus && r.CanonicalNoun(p.Noun) == op.Noun() && r.nounInstance(op.Noun(), start) != "" {
			continue
		}
		if p.From != "" {
			from[name] = p.From
		}
	}
	if len(from) == 0 || start == "" {
		return q.Args, nil
	}
	optional := func(arg string) bool { return !op.Parameters[arg].Required }
	return r.fillFrom(q.Args, from, KeyEvent{Focus: start}, false, optional)
}

// formSources is q with its parameters' sources filled in, for the args
// form: a form is a person's, so it reads the row at q's target, else the
// base focus, whatever route opened it.
func (r *Runtime) formSources(q Request) Request {
	op, ok := r.Operations.Find(Key(q.Key()))
	if !ok {
		return q
	}
	start := r.BaseFocus()
	if q.Target != "" && q.Target != "focused-input" {
		if id, err := r.TargetInstance(q.Target); err == nil {
			start = id
		}
	}
	if args, err := r.sourcesAt(op, q, start); err == nil {
		q.Args = args
	}
	return q
}

// sourceValue is the value op's parameter name would be filled with from
// its From for a person at focus when the request omits it, for the
// palette's slot and its check of what a line still needs.
func (r *Runtime) sourceValue(op Operation, name, focus string) (any, bool) {
	if p, ok := op.Parameters[name]; !ok || p.From == "" {
		return nil, false
	}
	args, err := r.sourcesAt(op, Request{}, focus)
	if err != nil {
		return nil, false
	}
	v, ok := args[name]
	return v, ok
}
