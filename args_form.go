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

// SinglePathSetting says what a request whose only missing required
// argument is a path opens (R7.8): SinglePathChooser (the default) the
// path chooser at once, its pick running the request, optional
// parameters left out; SinglePathAlone the same only when the path is
// the form's one field (with optional parameters, the form); SinglePathForm
// always the args form. A form that opens on a missing path field of the
// picker kind opens its chooser over it, so typing reaches the path.
const SinglePathSetting = "forms.single-path"

// The values of SinglePathSetting.
const (
	SinglePathChooser = "chooser"
	SinglePathAlone   = "alone"
	SinglePathForm    = "form"
)

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
	case "", "string", "integer", "number", "boolean", PathParameterType, AnyParameterType:
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
// read from the focus or a guess (R12.5), except what a parameter declares
// and the form shows: a Focus parameter starts holding the focused thing
// (focusValue), and an Edits one the subject's current value of its prop,
// selected as a rename is, or empty with the prop's Fallback as its
// placeholder (argsFormStart). enter runs the form once every
// required field is valid (form.run) and otherwise goes to the field that
// still needs a value; esc in normal mode cancels and runs nothing.
// run executes the completed request after the form closed, at the focus
// it opened over; its error reopens the form with the values. When the
// only missing required parameter is a path and SinglePathSetting allows,
// no form opens: the path chooser does (the editor returned is nil), and
// its pick runs the request.
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
	missing := missingRequired(op, q.Args)
	start := ""
	if len(missing) > 0 {
		start = missing[0]
	}
	rename := r.argsFormStart(q, op, fields, values, start)
	title := "Run " + q.Key()
	if q.Via != "" {
		title = "Run " + q.Via
	}
	if f, ok := r.singlePath(missing, fields); ok {
		return nil, r.choosePathArg(q, f, title, run)
	}
	spec := RecordSpec{
		Title:  title,
		Fields: fields,
		Load:   func() (Record, error) { return values, nil },
		Insert: true,
		Rename: rename,
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
	e, err := r.OpenRecordEditor(ArgsFormID, spec)
	if err != nil {
		return e, err
	}
	// A form opening on a missing path stop opens its chooser: that is
	// where the path is typed.
	if f := e.fields[e.start()]; start != "" && f.path && f.choose {
		return e, e.choosePath(f)
	}
	return e, nil
}

// argsFormStart fills the args form's declared starting values into
// values (and placeholders into fields) for the fields q leaves out: a
// Focus parameter's focused thing (focusValue) and an Edits parameter's
// current value of the subject's prop, else that prop's Fallback value as
// the placeholder. It reports whether the start field opens on a current
// value, which the form then selects as a rename (rename.select).
func (r *Runtime) argsFormStart(q Request, op Operation, fields []RecordField, values Record, start string) bool {
	focus := r.BaseFocus()
	if q.Target != "" {
		if id, err := r.TargetInstance(q.Target); err == nil {
			focus = id
		}
	}
	selected := false
	for i, f := range fields {
		if _, set := q.Args[f.Name]; set {
			continue
		}
		if t, ok := r.focusValue(op, f.Name, focus); ok {
			values[f.Name] = t.Key
		}
		if f.Edits == "" {
			continue
		}
		current, fallback := r.subjectProp(op, q, focus, f.Edits)
		switch {
		case current != "":
			values[f.Name] = current
			selected = selected || f.Name == start
		case fallback != "":
			fields[i].Placeholder = fallback
		}
	}
	return selected
}

// subjectProp reads the prop of the thing op's request acts for at focus,
// and its Fallback prop's value, from the noun's store ("" for both when
// it cannot be read).
func (r *Runtime) subjectProp(op Operation, q Request, focus, prop string) (current, fallback string) {
	n := r.Nouns[op.Noun]
	p := n.prop(prop)
	inst := r.Instances[focus]
	if n == nil || n.Store == nil || n.Store.Get == nil || p == nil || inst == nil {
		return "", ""
	}
	ctx := Ctx{actor: r.Actor(), Runtime: r, Context: r.contextOf(focus), Args: q.Args, Instance: inst}
	key, err := r.subjectKey(n, ctx)
	if err != nil {
		return "", ""
	}
	t, err := n.Store.Get(ctx, key)
	if err != nil || t == nil {
		return "", ""
	}
	if v := t[prop]; !emptyProp(v) {
		current = fmt.Sprint(v)
	}
	if p.Fallback != "" && !emptyProp(t[p.Fallback]) {
		fallback = fmt.Sprint(t[p.Fallback])
	}
	return current, fallback
}

// singlePath is the field of the lone missing required path when
// SinglePathSetting says a chooser answers for it without the form.
func (r *Runtime) singlePath(missing []string, fields []RecordField) (RecordField, bool) {
	if len(missing) != 1 {
		return RecordField{}, false
	}
	mode, _ := r.settingValue(SinglePathSetting)
	if mode == SinglePathForm || (mode == SinglePathAlone && len(fields) != 1) {
		return RecordField{}, false
	}
	for _, f := range fields {
		if f.Name == missing[0] && f.Type == PathParameterType {
			return f, true
		}
	}
	return RecordField{}, false
}

// choosePathArg opens the path chooser for a request's one missing path:
// the pick runs the request with it at the focus the chooser opened
// over; esc runs nothing.
func (r *Runtime) choosePathArg(q Request, f RecordField, title string, run func(Request) error) error {
	_, err := r.OpenPathChooser(PathChooserSpec{
		Title: title + " · choose " + f.Name,
		Pick:  PathPick{Name: f.Name, Parameter: f.Parameter},
		Choose: func(path string) Result {
			done := q
			done.Args = maps.Clone(q.Args)
			if done.Args == nil {
				done.Args = Arguments{}
			}
			done.Args[f.Name] = path
			if err := run(done); err != nil {
				return Failed(err)
			}
			return Done(path)
		},
	})
	return err
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
