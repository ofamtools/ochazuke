package ochazuke

import (
	"maps"
)

// editByChooser reports whether edit {field} opens the field's chooser in
// place of the one-field form, and the field it opens on: only when the
// field takes a choice (editKindOf: choose, the form's own test, so a
// free text field keeps its form), and then as the request's chooser
// argument says, else when edit.field-only and edit.field-chooser are both
// on. Nothing here names a noun or a prop: the prop's kind decides.
func (r *Runtime) editByChooser(args Arguments, only *PropSpec, fieldOnly bool, fields []RecordField) (RecordField, bool) {
	if only == nil {
		return RecordField{}, false
	}
	var field RecordField
	found := false
	for _, f := range fields {
		if f.Name == only.Name {
			field, found = f, true
			break
		}
	}
	if !found || !r.editKindOf(field).choose {
		return RecordField{}, false
	}
	if v, ok := args[editChooserParam].(bool); ok {
		return field, v
	}
	return field, fieldOnly && r.Settings().Bool(EditFieldChooserSetting)
}

// openEditChooser opens the chooser of one choice field for edit {field}:
// the framework chooser over the field's choices (the one a form's choice
// field opens, openChoiceChooser), or the path chooser for a path field.
// A pick is the write (write: set on the subject and the selection, one
// history group, so history.undo takes it back in one step) and closes the
// chooser, the focus back where edit ran; picking the value the thing
// already holds writes nothing. esc (modal.close, the chooser's cancel)
// closes it with nothing written. An optional field offers "(none)", which
// clears it.
func (r *Runtime) openEditChooser(c Ctx, n *Noun, field RecordField, values Record, title string, write func(map[string]any) Result) error {
	p := n.prop(field.Name)
	pick := func(text string) Result {
		if text == values[field.Name] {
			return Done(nil)
		}
		value, err := editPropValue(*p, text)
		if err != nil {
			return Failed(err)
		}
		return write(map[string]any{field.Name: value})
	}
	if r.editKindOf(field).path {
		_, err := r.OpenPathChooser(PathChooserSpec{
			Kind:   EditChooserKind,
			Title:  title,
			Pick:   PathPick{Name: field.Name, Parameter: field.Parameter, Value: values[field.Name]},
			Choose: pick,
		})
		return err
	}
	var fixed []ChooserRow
	if !field.Required {
		fixed = append(fixed, ChooserRow{ID: "", Label: "(none)", Fixed: true})
	}
	_, err := r.openChoiceChooser(ChooserSpec{
		Kind:   EditChooserKind,
		Title:  title,
		Empty:  "loading…",
		Choose: func(row ChooserRow) Result { return pick(row.ID) },
	}, choiceLoad{
		ctx:      c.Context,
		param:    field.Parameter,
		label:    field.choiceLabel,
		siblings: func() Arguments { return r.recordArguments(n, values) },
		fixed:    fixed,
	})
	return err
}

// recordArguments are a thing's values typed as arguments, the siblings a
// dependent choice source reads (as a form's arguments are); a value its
// prop does not take is left out.
func (r *Runtime) recordArguments(n *Noun, values Record) Arguments {
	args := Arguments{}
	for _, p := range writableProps(n) {
		text := values[p.Name]
		if text == "" {
			continue
		}
		typed, err := argumentsFrom(Operation{Parameters: map[string]Parameter{p.Name: p.Parameter}}, nil, Record{p.Name: text})
		if err == nil {
			maps.Copy(args, typed)
		}
	}
	return args
}
