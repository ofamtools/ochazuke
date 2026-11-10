package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// List fields of a record editor (Parameter.Items).
//
// A field whose parameter is a list with Items is drawn as its label, then
// one row per item under it, indented, then an "add" row; the field is one
// focus stop of the noun ochazuke.form-item and the rows are the positions
// of its cursor (0 to the number of items, the last being the add row). The
// value is a JSON array of the items' text ("[]" once emptied), so the
// field is written like any other: each add, edit, delete or move writes
// the whole list at once and is one undo step (u). There is no save key and
// no draft.
//
// The form's item verbs (item-edit and the rest, beside the chips' chip-*;
// a command noun may not take a core verb's name) act on the item under the
// cursor: item-edit (a path opens the path chooser, an item with choices its
// picker, any other item a line of text in a picker whose create row takes
// what is typed, so free text is never refused), item-add (after this one;
// at the end from the add row), item-delete, item-move-up, item-move-down,
// item-yank (into a register), and the edges item-first and item-last.
// item-next and item-previous move the cursor and spill to the field below
// or above at the ends, as chips do. A list is one column of rows, so it has
// no column edges (a component with no horizontal motion takes them as a
// no-op, SpillEdges).

// FormItemNoun is a list field's instance noun: the context its keys are
// bound in (bindings/form.json); its verbs are the form's item-* verbs.
const FormItemNoun = "ochazuke.form-item"

// FormItemKind is the kind of the picker a list item is edited in.
const FormItemKind = "form-item-edit"

// FormListGuard holds on a list field of an open form.
const FormListGuard = "form-list"

// ParseItems reads a list field's value: a JSON array of the items' text,
// "" for none.
func ParseItems(v string) ([]string, error) { return ParseChips(v) }

// ItemsValue is a list field's value for items: a JSON array, "[]" for none.
func ItemsValue(items []string) string {
	if items == nil {
		items = []string{}
	}
	raw, _ := json.Marshal(items)
	return string(raw)
}

// isList reports whether a field is edited as a list of rows.
func isList(field RecordField) bool { return field.Items != nil && !field.Chips }

func (f *recordField) listItems() []string {
	items, _ := ParseItems(f.value)
	return items
}

// itemCursor is the cursor kept inside the rows: 0 to len(items), the last
// being the add row.
func (f *recordField) itemCursor() int {
	return min(max(0, f.item), len(f.listItems()))
}

func (e *RecordEditor) listProblem(f *recordField, v string) string {
	items, err := ParseItems(v)
	if err != nil {
		return "a list of items"
	}
	if len(items) == 0 && f.Required {
		return "required"
	}
	if it := f.Items; it != nil && len(it.Enum) > 0 && !it.Free {
		for _, item := range items {
			if !slices.Contains(it.Enum, item) {
				return fmt.Sprintf("%q is not one of %s", item, strings.Join(it.Enum, ", "))
			}
		}
	}
	if f.Validate != nil {
		if err := f.Validate(v); err != nil {
			return err.Error()
		}
	}
	return ""
}

// listBlock draws the rows of a list field, indent cells in: the items, the
// cursor's row in the chip's focused tone while the field has the focus,
// then the muted add row.
func (e *RecordEditor) listBlock(f *recordField, width, indent int) []string {
	s := e.Styles
	focused := e.r.DrawnFocus(e.id) == f.id
	items := f.listItems()
	at := f.itemCursor()
	pad := strings.Repeat(" ", indent)
	rows := make([]string, 0, len(items)+1)
	for i, item := range items {
		text := ansi.Truncate(strings.Join(strings.Split(item, "\n"), " "), max(1, width-indent), "…")
		st := s.Tone(ToneChip)
		if focused && i == at {
			st = s.Tone(ToneChipFocused)
			if !s.hasTone(ToneChipFocused) {
				st = st.Inherit(s.Selection(true))
			}
		}
		rows = append(rows, pad+e.fieldStyle(f, st).Render(" "+text+" "))
	}
	add := s.Muted
	if focused && at == len(items) {
		add = s.Tone(ToneChipFocused)
		if !s.hasTone(ToneChipFocused) {
			add = add.Inherit(s.Selection(true))
		}
	}
	rows = append(rows, pad+e.fieldStyle(f, add).Render(" + add "+itemWord(f)+" "))
	return rows
}

// itemWord names one item for the add row and the pickers' titles.
func itemWord(f *recordField) string {
	if f.Items != nil && f.Items.Description != "" {
		return f.Items.Description
	}
	if f.Items != nil && f.Items.IsPath() {
		return "path"
	}
	return "item"
}

// landItems puts the cursor of a list field the focus moved into on the end
// it was entered from: the first row coming down, the add row coming up.
func (e *RecordEditor) landItems(f *recordField, delta int) {
	if !f.list {
		return
	}
	if delta < 0 {
		f.item = len(f.listItems())
		return
	}
	f.item = 0
}

// setItems writes the list value and the cursor, as one undo step.
func (e *RecordEditor) setItems(f *recordField, items []string, cursor int) error {
	f.value = ItemsValue(items)
	f.item = min(max(0, cursor), len(items))
	e.seen = e.snapshot()
	return e.seal(f)
}

// itemStep moves the cursor delta rows, spilling to the field below or
// above past the first row or the add row.
func (e *RecordEditor) itemStep(f *recordField, delta int) error {
	to := f.itemCursor() + delta
	if to < 0 || to > len(f.listItems()) {
		return e.move(delta, false)
	}
	f.item = to
	return nil
}

// itemFirst and itemLast put the cursor on the first or last item.
func (f *recordField) itemFirst() error {
	if len(f.listItems()) == 0 {
		return errors.New("no items")
	}
	f.item = 0
	return nil
}

func (f *recordField) itemLast() error {
	n := len(f.listItems())
	if n == 0 {
		return errors.New("no items")
	}
	f.item = n - 1
	return nil
}

// itemEdit edits the item under the cursor, or adds one at the end from the
// add row.
func (e *RecordEditor) itemEdit(f *recordField) error {
	items := f.listItems()
	if at := f.itemCursor(); at < len(items) {
		return e.itemPrompt(f, at, false, items[at])
	}
	return e.itemPrompt(f, len(items), true, "")
}

// itemAdd adds an item after the one under the cursor (at the end from the
// add row).
func (e *RecordEditor) itemAdd(f *recordField) error {
	items := f.listItems()
	at := min(f.itemCursor()+1, len(items))
	return e.itemPrompt(f, at, true, "")
}

// itemDelete drops the item under the cursor; the cursor stays on the next
// item, else the previous.
func (e *RecordEditor) itemDelete(f *recordField) error {
	items := f.listItems()
	at := f.itemCursor()
	if at >= len(items) {
		return errors.New("no item under the cursor")
	}
	items = slices.Delete(items, at, at+1)
	return e.setItems(f, items, min(at, max(0, len(items)-1)))
}

// itemMove swaps the item under the cursor with its neighbour, the cursor
// going with it.
func (e *RecordEditor) itemMove(f *recordField, delta int) error {
	items := f.listItems()
	at := f.itemCursor()
	to := at + delta
	switch {
	case at >= len(items):
		return errors.New("no item under the cursor")
	case to < 0:
		return errors.New("already the first item")
	case to >= len(items):
		return errors.New("already the last item")
	}
	items[at], items[to] = items[to], items[at]
	return e.setItems(f, items, to)
}

// itemYank copies the item under the cursor into a register.
func (e *RecordEditor) itemYank(f *recordField, register string) error {
	items := f.listItems()
	at := f.itemCursor()
	if at >= len(items) {
		return errors.New("no item under the cursor")
	}
	if err := e.r.storeYank(Yank{Text: items[at], Shape: "char"}, register, false); err != nil {
		return err
	}
	e.r.UI().Info("copied " + itemWord(f))
	return nil
}

// itemPrompt opens the editor of one item by its kind: a path item the path
// chooser, an item with choices its picker, any other item a line of text.
// The pick replaces the item at (or inserts a new one there) and writes the
// list at once; esc writes nothing.
func (e *RecordEditor) itemPrompt(f *recordField, at int, insert bool, current string) error {
	take := func(text string) Result {
		items := f.listItems()
		switch {
		case insert:
			at = min(at, len(items))
			items = slices.Insert(items, at, text)
		case at < len(items):
			items[at] = text
		default:
			return Failed(errors.New("the item is gone"))
		}
		if err := e.setItems(f, items, at); err != nil {
			return Failed(err)
		}
		return Done(text)
	}
	label := itemWord(f)
	verb := "Edit "
	if insert {
		verb = "Add "
	}
	title := verb + label + " (" + f.label() + ")"
	it := *f.Items
	if it.IsPath() {
		_, err := e.r.OpenPathChooser(PathChooserSpec{
			Title:  title,
			Pick:   PathPick{Name: f.Name, Parameter: it, Value: current},
			Choose: take,
		})
		return err
	}
	spec := ChooserSpec{
		Kind:   FormItemKind,
		Title:  title,
		Empty:  "loading…",
		Choose: func(row ChooserRow) Result { return take(row.ID) },
		Start:  func(row ChooserRow) bool { return heldChoice(current, row.ID) },
	}
	free := it.Free || !paramHasChoices(it)
	if free {
		spec.Create, spec.CreateRow = take, true
		spec.CreateLabel = func(query string) string { return "use " + strconv.Quote(query) }
	}
	if !paramHasChoices(it) {
		c, err := e.r.OpenPicker(spec)
		if err != nil {
			return err
		}
		c.SetQuery(current)
		return nil
	}
	_, err := e.r.openChoiceChooser(spec, choiceLoad{
		ctx:      e.r.contextOf(f.id),
		param:    it,
		label:    f.choiceLabel,
		row:      f.choiceRow,
		siblings: e.arguments,
	})
	return err
}

func registerFormItems(r *Runtime) {
	list := func(run func(*RecordEditor, *recordField, Arguments) error) Handler {
		return func(r *Runtime, c Context, a Arguments) (any, error) {
			e := r.recordEditorOf(focusOf(c))
			if e == nil {
				return nil, errors.New("no focused record editor")
			}
			f := e.fieldOn(focusOf(c))
			if f == nil || !f.list {
				return nil, errors.New("the focused field is not a list")
			}
			return nil, run(e, f, a)
		}
	}
	r.Define(FormItemNoun).Focusable()
	r.Define(FormNoun).
		Verb("item-edit", list(func(e *RecordEditor, f *recordField, _ Arguments) error { return e.itemEdit(f) }), "Edit the list item under the cursor (a path in the path chooser, a choice in its picker, text in a line); on the add row, add one").When(FormListGuard).
		Verb("item-add", list(func(e *RecordEditor, f *recordField, _ Arguments) error { return e.itemAdd(f) }), "Add a list item after the one under the cursor (at the end from the add row)").When(FormListGuard).
		Verb("item-delete", list(func(e *RecordEditor, f *recordField, _ Arguments) error { return e.itemDelete(f) }), "Delete the list item under the cursor; written at once, u undoes it").When(FormListGuard).
		Verb("item-move-up", list(func(e *RecordEditor, f *recordField, _ Arguments) error { return e.itemMove(f, -1) }), "Move the list item under the cursor up one place; written at once").When(FormListGuard).
		Verb("item-move-down", list(func(e *RecordEditor, f *recordField, _ Arguments) error { return e.itemMove(f, 1) }), "Move the list item under the cursor down one place; written at once").When(FormListGuard).
		Verb("item-yank", list(func(e *RecordEditor, f *recordField, a Arguments) error {
			return e.itemYank(f, StringArg(a, "register", ""))
		}), "Copy the list item under the cursor into a register (the active one, else the unnamed one)").When(FormListGuard).
		Verb("item-next", list(func(e *RecordEditor, f *recordField, _ Arguments) error { return e.itemStep(f, 1) }), "Move the cursor to the next row; past the add row the focus goes to the field below").Motion().When(FormListGuard).
		Verb("item-previous", list(func(e *RecordEditor, f *recordField, _ Arguments) error { return e.itemStep(f, -1) }), "Move the cursor to the previous row; past the first row the focus goes to the field above").Motion().When(FormListGuard).
		Verb("item-first", list(func(_ *RecordEditor, f *recordField, _ Arguments) error { return f.itemFirst() }), "Put the cursor on the first list item").Motion().When(FormListGuard).
		Verb("item-last", list(func(_ *RecordEditor, f *recordField, _ Arguments) error { return f.itemLast() }), "Put the cursor on the last list item").Motion().When(FormListGuard)
	op := r.Operations.At(Key(FormNoun + ".item-yank"))
	op.Parameters = map[string]Parameter{"register": {Type: "string", Description: "The register to copy into (default the active one)"}}
	r.Operations.Set(op)
	r.DefineGuard(FormListGuard, func(r *Runtime, ev KeyEvent) bool {
		e := r.recordEditorOf(ev.Focus)
		if e == nil {
			return false
		}
		f := e.fieldOn(ev.Focus)
		return f != nil && f.list
	})
}
