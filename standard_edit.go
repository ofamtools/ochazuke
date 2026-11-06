package ochazuke

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
)

// EditFormsSetting makes the standard edit verbs available: off, every
// thing noun still has edit, unavailable with the reason "forms.edit is
// off". Set it before declaring stores or mounting a catalog; an
// application's own edit is unchanged.
const EditFormsSetting = "forms.edit"

// EditApplySetting chooses immediate debounced writes or one confirmed write.
const EditApplySetting = "forms.edit-apply"

// EditFieldOnlySetting: edit with a field (edit {field: "@label"}) opens a
// form holding only that field, opened as a rename (RecordSpec.Rename: its
// value selected under rename.select). Off, the full form opens with that
// field focused. Default on.
const EditFieldOnlySetting = "edit.field-only"

// editFieldParam is edit's parameter naming the one prop to edit.
const editFieldParam = "field"

const editFormID = "edit-form"
const editOpenSlot = "standard-edit-open"

func editParams(n *Noun) map[string]Parameter {
	params := map[string]Parameter{}
	var fields []string
	for _, p := range writableProps(n) {
		param := propParameter(p)
		param.Required = false // omitted props keep their values; the form enforces Required
		params[p.Name] = param
		fields = append(fields, p.Name)
	}
	for _, role := range n.roleNames() {
		if p := n.prop(role); p != nil && p.writable() {
			fields = append(fields, role)
		}
	}
	// Free: a key row may name a role the noun lacks; the request is then
	// unavailable there (propsAvailable), as the purged verb was, not a
	// rejected row.
	params[editFieldParam] = Parameter{Type: "string", Enum: fields, Free: true, Plumbing: true,
		Description: "The one prop to edit (a name, or @label, @owner, @archive for the prop with that role): with edit.field-only the form holds only it, else the full form opens on it"}
	return params
}

// EditHandler builds the standard edit handler for a declared noun, including a
// catalog-mounted noun with get and set proxies but no local Store. A nil
// handler means forms.edit disabled generation. Supplied props go through set;
// otherwise get loads a RecordEditor in the chosen apply mode. Both retain normal
// availability, checks, confirmations and agent rights.
func (r *Runtime) EditHandler(noun string) AppHandler {
	if !r.Settings().Bool(EditFormsSetting) {
		return nil
	}
	return r.editHandler(noun)
}

// editHandler is the edit verb's handler whatever forms.edit says: the
// setting makes the full form unavailable (needEditForms), and a request
// giving field (the one-field form) waives it. Over several subjects (a
// visual selection, marks: edit is Many) the form opens on the focused
// one and every write goes to each subject. All the writes of one form
// (every subject, every debounced save) are one history group, so
// history.undo takes the form back in one step.
func (r *Runtime) editHandler(noun string) AppHandler {
	return func(c Ctx) Result {
		n := r.Noun(noun)
		subject, others, err := r.editSubjects(n, c)
		if err != nil {
			return Failed(err)
		}
		writeKey := noun
		if key := n.keyProp(); key != nil {
			writeKey += "\x00" + fmt.Sprint(subject[key.Name])
		}
		only, changes, err := editArgs(n, c.Args)
		if err != nil {
			return Failed(err)
		}
		if human := n.humanProp(); human != nil && only != nil && only.Name == human.Name && len(others) > 0 {
			// A human key names one thing: typed once, it is not every
			// selected row's name. The focused row takes it; the rest stay.
			r.UI().Info(fmt.Sprintf("left %d as they were: %s names one row", len(others), human.Name))
			others = nil
		}
		write := r.editWriter(n, subject, others, r.newHistoryGroup("edit "+writeKey))
		if len(changes) > 0 {
			return write(changes)
		}
		if e := r.RecordEditor(editFormID); e != nil {
			if err := e.Leave(); err != nil {
				return Failed(err)
			}
			defer r.actingFor(r.Focus)()
		}
		open := func(value any) error {
			thing, err := ThingOf(value)
			if err != nil {
				return err
			}
			values, err := editRecord(n, thing)
			if err != nil {
				return err
			}
			fieldOnly := only != nil && r.Settings().Bool(EditFieldOnlySetting)
			fields := r.editFields(n, c, thing, only, fieldOnly)
			r.pendingRecord(writeKey, values)
			label := n.displayLabel(func(name string) any { return thing[name] })
			instanceSubject := c
			instanceSubject.Args = nil
			key, _ := r.subjectKey(n, instanceSubject)
			if c.Instance != nil && c.Instance.Noun == n.Name && c.Instance.Label != "" && (n.keyProp() == nil || key == fmt.Sprint(subject[n.keyProp().Name])) {
				label = c.Instance.Label
			}
			if label == "" && n.keyProp() != nil {
				label = fmt.Sprint(subject[n.keyProp().Name])
			}
			spec := RecordSpec{Title: "Edit " + r.DisplayNoun(noun) + ": " + label, Fields: fields, WriteKey: writeKey, Load: func() (Record, error) { return values, nil }}
			editStart(&spec, only, fieldOnly, r.DisplayNoun(noun), label)
			spec.Watch = noun
			spec.WatchArgs = subject
			spec.Reload = func() Result {
				res, _ := r.readEdit(c, noun, subject)
				if res.work == nil {
					if res.err != nil {
						return res
					}
					loaded, err := editRecord(n, res.value)
					if err != nil {
						return Failed(err)
					}
					return Done(loaded)
				}
				then := res.then
				var raw any
				res = res.Map(func(value any, err error) (any, error) {
					if err != nil {
						return nil, err
					}
					raw = value
					return editRecord(n, raw)
				})
				if then != nil {
					res.then = func(c Ctx, _ any, err error) { then(c, raw, err) }
				}
				return res
			}
			var editor *RecordEditor
			if r.Settings().String(EditApplySetting) == "confirm" {
				spec.Submit = func(rec Record) Result {
					changed := map[string]any{}
					for _, f := range fields {
						if rec[f.Name] != editor.baseline[f.Name] {
							p := n.prop(f.Name)
							value, err := editPropValue(*p, rec[f.Name])
							if err != nil {
								return Failed(fmt.Errorf("%s %w", f.Name, err))
							}
							changed[f.Name] = value
						}
					}
					if len(changed) == 0 {
						return Done(nil)
					}
					return write(changed)
				}
			} else {
				spec.FieldWrites = true
				spec.Save = func(w RecordWrite) Result {
					name := w.Changed[0]
					value, err := editPropValue(*n.prop(name), w.Values[name])
					if err != nil {
						return Failed(err)
					}
					return write(map[string]any{name: value})
				}
			}
			editor, err = r.OpenRecordEditor(editFormID, spec)
			return err
		}
		res, ran := r.readEdit(c, noun, subject)
		if res.err != nil {
			return res
		}
		if !ran {
			return Done(nil)
		} // a check or confirmation took over
		if res.work == nil {
			if err := open(res.value); err != nil {
				return Failed(err)
			}
			return Done(nil)
		}
		focus, page, mode, modal := r.Focus, r.Page, r.Mode.Name, r.TopModal()
		then := res.then
		return res.ReplacingAt("app", editOpenSlot).Then(func(ctx Ctx, value any, err error) {
			if then != nil {
				then(ctx, value, err)
			}
			if err == nil && !r.Quit && r.Focus == focus && r.Page == page && r.Mode.Name == mode && r.TopModal() == modal {
				if err := open(value); err != nil {
					ctx.Error(err.Error())
				}
			}
		})
	}
}

// editSubjects are the subject the edit form opens on (its key as an
// argument) and the keys of the request's other subjects (a selection),
// each write going to all of them.
func (r *Runtime) editSubjects(n *Noun, c Ctx) (Arguments, []any, error) {
	subject := Arguments{}
	key := n.keyProp()
	if key == nil {
		return subject, nil, nil
	}
	value, err := r.subjectKey(n, c)
	if err != nil {
		return nil, nil, err
	}
	typed, err := propValue(*key, value)
	if err != nil {
		return nil, nil, err
	}
	subject[key.Name] = typed
	others, err := r.otherSubjectKeys(n, c, *key, value)
	return subject, others, err
}

// editWriter is the form's write: set {props} on the subject and on each
// other subject, every one recorded in group, so history.undo takes the
// whole form back in one step. The noun's human key (humanProp) is
// written to the subject alone: one name typed is never every row's. A
// write over several subjects posts the one notice a run over many does
// (subjectsNotice: done on N rows, or how many failed).
func (r *Runtime) editWriter(n *Noun, subject Arguments, others []any, group string) func(map[string]any) Result {
	return func(changes map[string]any) Result {
		defer r.inHistoryGroup(group)()
		name := r.DisplayKey(n.Name + ".edit")
		var value any
		var applied, skipped []string
		var first error
		for i, target := range append([]any{nil}, others...) {
			args := maps.Clone(subject)
			props := maps.Clone(changes)
			if i > 0 {
				args[n.keyProp().Name] = target
				if human := n.humanProp(); human != nil {
					delete(props, human.Name)
				}
				if len(props) == 0 {
					continue
				}
			}
			args["props"] = props
			// The subject key, not the ephemeral row, identifies this write.
			v, err := r.ExecuteAt(r.contextOf("app"), Request{Noun: n.Name, Verb: "set", Args: args})
			id := ""
			if key := n.keyProp(); key != nil {
				id = fmt.Sprint(args[key.Name])
			}
			switch {
			case err != nil && i == 0:
				return Failed(err)
			case err != nil:
				skipped = append(skipped, id+": "+err.Error())
				if first == nil {
					first = err
				}
				continue
			}
			applied = append(applied, id)
			if i == 0 {
				value = v
			}
		}
		if len(applied)+len(skipped) > 1 {
			if err := r.subjectsNotice(name, applied, skipped, first); err != nil {
				return Failed(err)
			}
		}
		return Done(value)
	}
}

// otherSubjectKeys are the keys of the request's subjects (Ctx.Subjects,
// a selection) other than the one it acts for (primary), typed by the key
// prop, in order.
func (r *Runtime) otherSubjectKeys(n *Noun, c Ctx, key PropSpec, primary string) ([]any, error) {
	if len(c.Subjects) <= 1 {
		return nil, nil
	}
	var out []any
	for _, s := range c.Subjects {
		k, err := r.rowKey(n, s)
		if err != nil {
			return nil, err
		}
		if k == primary {
			continue
		}
		typed, err := propValue(key, k)
		if err != nil {
			return nil, err
		}
		out = append(out, typed)
	}
	return out, nil
}

// editArgs reads an edit request's arguments beside the subject's key:
// the one prop it opens on (field: a name or @role), and the props it
// writes directly instead of opening the form.
func editArgs(n *Noun, args Arguments) (*PropSpec, map[string]any, error) {
	rest := maps.Clone(args)
	delete(rest, editFieldParam)
	if key := n.keyProp(); key != nil {
		delete(rest, key.Name)
	}
	var only *PropSpec
	if field := StringArg(args, editFieldParam, ""); field != "" {
		p, err := writeTarget(n, field)
		if err != nil {
			return nil, nil, err
		}
		only = p
	}
	changes := map[string]any{}
	for name, value := range rest {
		p := n.prop(name)
		if p == nil || !p.writable() {
			return nil, nil, fmt.Errorf("%s.%s is not writable", n.Name, name)
		}
		changes[name] = value
	}
	return only, changes, nil
}

// editFields are the form's fields: every writable prop shown where the
// request acts, or only the one it opens on (fieldOnly). An empty prop
// with a Fallback shows the fallback's value as its placeholder, as the
// rows do.
func (r *Runtime) editFields(n *Noun, c Ctx, thing Thing, only *PropSpec, fieldOnly bool) []RecordField {
	fields := []RecordField{}
	for _, p := range writableProps(n) {
		if !r.propShown(p, focusOf(c.Context), r.Mode, c.Args) || fieldOnly && p.Name != only.Name {
			continue
		}
		field := PropField(p)
		if v := thing[p.Fallback]; p.Fallback != "" && v != nil {
			field.Placeholder = fmt.Sprint(v)
		}
		field.Validate = func(text string) error {
			_, err := editPropValue(p, text)
			return err
		}
		// Structured props remain JSON text, validated before each save.
		if p.Type == SettingList || p.Type == PropObject || p.Type == SettingDuration {
			field.Type = AnyParameterType
		}
		fields = append(fields, field)
	}
	return fields
}

// editStart opens a form on one prop (edit {field}) as a rename opened:
// that field first, its value selected (rename.select) so typing replaces
// it; a one-field form's title names the prop.
func editStart(spec *RecordSpec, only *PropSpec, fieldOnly bool, noun, label string) {
	if only == nil {
		return
	}
	spec.Start, spec.Rename = only.Name, true
	if fieldOnly {
		spec.Title = "Edit " + noun + " " + only.Name + ": " + label
	}
}

func (r *Runtime) readEdit(c Ctx, noun string, subject Arguments) (res Result, ran bool) {
	_, err := r.executeAt(r.contextOf("app"), Request{Noun: noun, Verb: "get", Args: subject}, func(op Operation, ctx Context, args Arguments) (any, error) {
		ran = true
		if op.App == nil {
			value, err := op.Run(r, ctx, args)
			res = Done(value)
			return value, err
		}
		c.Context, c.Args, c.Invoked = ctx, args, r.Invoked()
		c.Instance = r.Instances[focusOf(ctx)]
		res = op.App(c)
		return res.value, res.err
	})
	if err != nil {
		res = Failed(err)
	}
	return res, ran
}

func editRecord(n *Noun, value any) (Record, error) {
	thing, err := ThingOf(value)
	if err != nil {
		return nil, err
	}
	values := Record{}
	for _, p := range writableProps(n) {
		values[p.Name] = ""
		if v := thing[p.Name]; v != nil {
			v = fileForm(v)
			switch {
			case p.Type == SettingList || p.Type == PropObject:
				raw, err := json.Marshal(v)
				if err != nil {
					return nil, err
				}
				values[p.Name] = string(raw)
			default:
				if number, ok := v.(float64); ok {
					values[p.Name] = strconv.FormatFloat(number, 'f', -1, 64)
				} else {
					values[p.Name] = fmt.Sprint(v)
				}
			}
		}
	}
	return values, nil
}

func editPropValue(p PropSpec, text string) (any, error) {
	if text == "" && !p.Required {
		return nil, nil
	}
	return propValue(p, text)
}

func (r *Runtime) focusedEdit(focus string) (Request, bool) {
	if r.TopModal() != "" || r.contextOf(focus).InputID != "" {
		return Request{}, false
	}
	inst := r.Instances[focus]
	if inst == nil {
		return Request{}, false
	}
	noun := inst.Noun
	if n := r.Noun(noun); n == nil || n.NoEditKey {
		return Request{}, false
	}
	// The noun's standard verb that opens its form (a Form row: edit).
	for _, v := range standardTable() {
		if op, ok := r.Operation(noun + "." + v.Verb); v.Form && ok && op.Standard == v.Verb {
			q := Request{Noun: noun, Verb: v.Verb, Target: focus}
			return q, r.Available(q, focus, r.Mode)
		}
	}
	return Request{}, false
}

func registerStandardEdit(r *Runtime) {
	r.DefineGuard("noun-editable", func(r *Runtime, ev KeyEvent) bool {
		_, ok := r.focusedEdit(ev.Focus)
		return ok
	})
	r.Define("ochazuke.nouns").Verb("edit", func(r *Runtime, c Context, _ Arguments) (any, error) {
		q, _ := r.focusedEdit(focusOf(c))
		return r.ExecuteAt(c, q)
	}, "Open the focused thing's standard edit form; existing inner edit keys take precedence").When("noun-editable")
	r.DefineGuard("edit-opening", func(r *Runtime, _ KeyEvent) bool {
		return r.HasRunningJob("app", editOpenSlot)
	})
	r.Define("ochazuke.nouns").Verb("cancel-edit", func(c Ctx) Result {
		if j := r.slotJob("app", editOpenSlot); j != nil {
			r.finish(j, JobCancelled, nil)
		}
		return Done(nil)
	}, "Cancel a pending edit open without writing").When("edit-opening")
	op := r.Operations["ochazuke.nouns.cancel-edit"]
	op.HistoryKind = HistoryAction
	r.Operations[op.Key()] = op
}
