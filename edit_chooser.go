package ochazuke

import (
	"maps"
)

// editByChooser reports whether edit {field} opens the field's chooser in
// place of the one-field form, and the field it opens on: only when the
// field takes a choice (editKindOf: choose, the form's own test, so a
// free text field keeps its form), and then as the request's chooser
// argument says, else when edit.field-only and edit.field-picker are both
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

// openEditChooser opens the framework chooser over one choice field's
// choices for edit {field} (the one a form's choice field opens,
// openChoiceChooser), highlighting the value the thing holds. A pick is
// the write (write: set on the subject and the selection, one history
// group, so history-entry.undo takes it back in one step) and closes the
// chooser, the focus back where edit ran; a subject already holding the
// pick is not written, the rest of the selection still is. esc
// (modal.close, the chooser's cancel) closes it with nothing written. An
// optional field offers "(none)", which clears it.
//
// values may be nil while the thing's get is pending: the chooser opens
// at once (its rows load meanwhile, so keys typed reach its query and
// never the page), and fill gives it the thing's values and title when
// the get lands, or closes it when the get failed. Until then the value
// it holds is not known, so an enter with no choice made (nothing typed,
// the highlight not moved) is held (ChooserSpec.Hold) and taken when the
// get lands, on the value it holds then: no write unless the selection
// has rows that differ. A typed query or a moved highlight writes at once.
func (r *Runtime) openEditChooser(c Ctx, n *Noun, field RecordField, values Record, title string, write editWrite) (ch *Chooser, fill func(Record, string, error), err error) {
	p := n.prop(field.Name)
	cur := values
	held := func(text string) bool { return cur != nil && text == cur[field.Name] }
	var fixed []ChooserRow
	if !field.Required {
		fixed = append(fixed, ChooserRow{ID: "", Label: "(none)", Fixed: true})
	}
	ch, err = r.openChoiceChooser(ChooserSpec{
		Kind:  EditChooserKind,
		Title: title,
		Empty: "loading…",
		Start: func(row ChooserRow) bool { return held(row.ID) },
		Hold:  func() bool { return cur == nil },
		Choose: func(row ChooserRow) Result {
			value, err := editPropValue(*p, row.ID)
			if err != nil {
				return Failed(err)
			}
			res, _ := write(map[string]any{field.Name: value}, held(row.ID), 0)
			return res
		},
	}, choiceLoad{
		ctx:      c.Context,
		param:    field.Parameter,
		label:    field.choiceLabel,
		siblings: func() Arguments { return r.recordArguments(n, cur) },
		fixed:    fixed,
	})
	if err != nil {
		return nil, nil, err
	}
	return ch, func(loaded Record, title string, err error) {
		if r.Chooser(EditChooserKind) != ch {
			return // closed or replaced before the get landed
		}
		if err != nil {
			if r.TopModal() == EditChooserKind {
				_ = ch.close()
			}
			return
		}
		cur, ch.spec.Title = loaded, title
		if len(field.SourceArgs) > 0 {
			// A dependent source reads the siblings just loaded.
			ch.SetRows(nil)
			ch.load()
		} else {
			ch.SetRows(ch.spec.Rows)
		}
		ch.Release()
	}, nil
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
