package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// MissingOpensFormSetting says whether a request missing a required
// argument, run from the palette's enter or from a key, opens the args
// form (on, the default) instead of naming the first missing word.
const MissingOpensFormSetting = "palette.missing-opens-form"

// ArgsFormID is the args form's modal instance (one at a time).
const ArgsFormID = "args-form"

// ensureFormSettings declares the args-form setting the application did
// not.
func (r *Runtime) ensureFormSettings() {
	if r.Settings().find(MissingOpensFormSetting) == nil {
		_ = r.DeclareSetting(Setting{Name: MissingOpensFormSetting, Type: SettingBoolean, Default: true, Description: "Enter on a command line, or a key, whose action lacks a required argument opens a form for its arguments; off names the missing word instead"})
	}
}

// missingOpensForm reads MissingOpensFormSetting, on when it is not
// declared.
func (r *Runtime) missingOpensForm() bool { return r.helpFlag(MissingOpensFormSetting, true) }

// missingRequired names the required parameters args lacks, in parameter
// order.
func missingRequired(op Operation, args Arguments) []string {
	var out []string
	for _, name := range op.ParameterOrder() {
		if _, set := args[name]; op.Parameters[name].Required && !set {
			out = append(out, name)
		}
	}
	return out
}

// formable reports whether a form field can hold a parameter's value: a
// string, a number or a boolean (never an array or an object).
func formable(p Parameter) bool {
	switch p.Type {
	case "", "string", "integer", "number", "boolean":
		return true
	}
	return false
}

// ArgsFields is the args form's schema for a verb: its form-held
// parameters (RecordFields), the required ones first, an optional one's
// label marked "(optional)", a description after the name. It fails when
// a required parameter cannot be held by a field.
func ArgsFields(op Operation) ([]RecordField, error) {
	var required, optional []RecordField
	for _, f := range RecordFields(op) {
		if !formable(f.Parameter) {
			if f.Required {
				return nil, fmt.Errorf("%s (%s) cannot be filled in a form", f.Name, f.Type)
			}
			continue
		}
		f.Label = f.Name
		if !f.Required {
			f.Label += " (optional)"
		}
		if f.Description != "" {
			f.Label += " · " + f.Description
		}
		if f.Required {
			required = append(required, f)
		} else {
			optional = append(optional, f)
		}
	}
	return append(required, optional...), nil
}

// OpenArgsForm opens the args form over the focus for a request whose
// verb lacks arguments: one field per parameter (ArgsFields), prefilled
// from q's arguments, the first missing required one focused (typing,
// when it is a text field). Every field is filled by the user: nothing is
// read from the focus or a guess (R12.5). enter runs the form once every
// required field is valid (form.run) and otherwise goes to the field that
// still needs a value; esc in normal mode cancels and runs nothing.
// run executes the completed request after the form closed, at the focus
// it opened over; its error reopens the form with the values.
func (r *Runtime) OpenArgsForm(q Request, run func(Request) error) (*RecordEditor, error) {
	op, ok := r.Operations[q.Key()]
	if !ok {
		return nil, fmt.Errorf("unknown operation %q", q.Key())
	}
	if run == nil {
		return nil, errors.New("args form needs a run")
	}
	if r.Instances[ArgsFormID] != nil {
		return nil, errors.New("an args form is already open")
	}
	fields, err := ArgsFields(op)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%s takes no arguments a form can hold", q.Key())
	}
	values := Record{}
	for _, f := range fields {
		if v, set := q.Args[f.Name]; set && v != nil {
			values[f.Name] = fmt.Sprint(v)
		}
	}
	start := ""
	if missing := missingRequired(op, q.Args); len(missing) > 0 {
		start = missing[0]
	}
	title := "Run " + q.Key()
	if q.Via != "" {
		title = "Run " + q.Via
	}
	spec := RecordSpec{
		Title:  title,
		Fields: fields,
		Load:   func() (Record, error) { return values, nil },
		Insert: true,
		Start:  start,
		Submit: func(rec Record) Result {
			done := q
			args, err := argumentsFrom(op, q.Args, rec)
			if err != nil {
				return Failed(err)
			}
			done.Args = args
			if err := run(done); err != nil {
				return Failed(err)
			}
			return Done(nil)
		},
	}
	return r.OpenRecordEditor(ArgsFormID, spec)
}

// argumentsFrom is a request's arguments from a form's values: base's,
// with each field's value in its parameter's type; an empty optional
// field is left out.
func argumentsFrom(op Operation, base Arguments, rec Record) (Arguments, error) {
	args := maps.Clone(base)
	if args == nil {
		args = Arguments{}
	}
	for _, name := range slices.Sorted(maps.Keys(rec)) {
		p, ok := op.Parameters[name]
		if !ok {
			continue
		}
		text := rec[name]
		if strings.TrimSpace(text) == "" {
			delete(args, name)
			continue
		}
		switch p.Type {
		case "integer":
			n, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil {
				return nil, fmt.Errorf("%s must be integer", name)
			}
			args[name] = n
		case "number":
			n, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
			if err != nil {
				return nil, fmt.Errorf("%s must be a number", name)
			}
			args[name] = n
		case "boolean":
			args[name] = text == "true"
		default:
			args[name] = text
		}
	}
	return args, nil
}

// formForMissing opens the args form for a key's request that lacks a
// required argument, when the setting is on; it reports whether it did. A
// request it cannot hold in a form runs as before and fails naming the
// argument.
func (r *Runtime) formForMissing(q Request, run func(Request) error) bool {
	op, ok := r.Operations[q.Key()]
	if !ok || len(missingRequired(op, q.Args)) == 0 || !r.missingOpensForm() {
		return false
	}
	_, err := r.OpenArgsForm(q, run)
	return err == nil
}
