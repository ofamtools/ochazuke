package ochazuke

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
)

// EditFormsSetting enables generation of standard edit verbs. Set it before
// declaring stores or mounting a catalog; an application's own edit is unchanged.
const EditFormsSetting = "forms.edit"

// EditApplySetting chooses immediate debounced writes or one confirmed write.
const EditApplySetting = "forms.edit-apply"

const editFormID = "edit-form"
const editOpenSlot = "standard-edit-open"

func init() {
	standardVerbs = append(standardVerbs, standardVerb{
		Verb: "edit", Description: "Edit a thing's writable props in a form; supplied props write directly without opening a form",
		Kinds:   []string{Collection, Singleton, Instances},
		Needs:   func(n *Noun) bool { return n.Store.Get != nil && n.Store.Patch != nil && len(writableProps(n)) > 0 },
		Subject: true, HistoryKind: HistoryAction, Params: editParams,
		Handler: func(r *Runtime, n *Noun) AppHandler { return r.EditHandler(n.Name) },
	})
}

func editParams(n *Noun) map[string]Parameter {
	params := map[string]Parameter{}
	for _, p := range writableProps(n) {
		param := propParameter(p)
		param.Required = false // omitted props keep their values; the form enforces Required
		params[p.Name] = param
	}
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
	return func(c Ctx) Result {
		n := r.Noun(noun)
		subject := Arguments{}
		if key := n.keyProp(); key != nil {
			value, err := r.subjectKey(n, c)
			if err != nil {
				return Failed(err)
			}
			typed, err := propValue(*key, value)
			if err != nil {
				return Failed(err)
			}
			subject[key.Name] = typed
		}
		writeKey := noun
		if key := n.keyProp(); key != nil {
			writeKey += "\x00" + fmt.Sprint(subject[key.Name])
		}
		changes := map[string]any{}
		for name, value := range c.Args {
			if key := n.keyProp(); key != nil && name == key.Name {
				continue
			}
			p := n.prop(name)
			if p == nil || !p.writable() {
				return Failed(fmt.Errorf("%s.%s is not writable", noun, name))
			}
			changes[name] = value
		}
		write := func(changes map[string]any) Result {
			args := maps.Clone(subject)
			args["changes"] = changes
			// The subject key, not the ephemeral row, identifies this write.
			value, err := r.ExecuteAt(r.contextOf("app"), Request{Noun: noun, Verb: "set", Args: args})
			if err != nil {
				return Failed(err)
			}
			return Done(value)
		}
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
			fields := []RecordField{}
			values, err := editRecord(n, thing)
			if err != nil {
				return err
			}
			for _, p := range writableProps(n) {
				if !r.propShown(p, focusOf(c.Context), r.Mode, c.Args) {
					continue
				}
				field := PropField(p)
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
			r.pendingRecord(writeKey, values)
			label := n.displayLabel(func(name string) any { return thing[name] })
			if c.Instance != nil && c.Instance.Label != "" {
				label = c.Instance.Label
			}
			if label == "" && n.keyProp() != nil {
				label = fmt.Sprint(subject[n.keyProp().Name])
			}
			spec := RecordSpec{Title: "Edit " + r.DisplayNoun(noun) + ": " + label, Fields: fields, WriteKey: writeKey, Load: func() (Record, error) { return values, nil }}
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
				work, then := res.work, res.then
				var raw any
				res.work = func(ctx context.Context) (any, error) {
					var err error
					raw, err = work(ctx)
					if err != nil {
						return nil, err
					}
					return editRecord(n, raw)
				}
				if then != nil {
					res.then = func(c Ctx, _ any, err error) { then(c, raw, err) }
				}
				return res
			}
			if r.Settings().String(EditApplySetting) == "confirm" {
				spec.Submit = func(rec Record) Result {
					changed := map[string]any{}
					for _, f := range fields {
						if rec[f.Name] != values[f.Name] {
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
			_, err = r.OpenRecordEditor(editFormID, spec)
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
	if op, ok := r.Operation(noun + ".edit"); ok && op.Standard == "edit" {
		q := Request{Noun: noun, Verb: "edit", Target: focus}
		return q, r.Available(q, focus, r.Mode)
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
}
