package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Record is one record's values by field name, as an editor loads and
// writes them.
type Record map[string]string

// RecordField is one editable field of a record schema. Its Parameter is
// the same description a verb's parameter carries (Type, Required, Enum,
// Source, Free, Description), so a form can come from a verb's catalog
// parameters (RecordFields). A field with Enum or Source (and not Free), or
// of Type boolean, is edited through a chooser, never as free text. A Free
// field with Enum or Source is a text field with suggestions (form-suggest):
// typing stays free text, enter in normal mode or ctrl+space opens the
// framework chooser over the suggestions (the Enum, then the Source's
// result, loaded off the loop when the Source verb is Pending), a pick
// fills the field and is written at once, and a value outside the
// suggestions is written as typed; it is one line high (Multiline is
// ignored). The rest are text inputs, Multiline ones several lines high.
type RecordField struct {
	Name  string `json:"name"`
	Label string `json:"label,omitempty"` // shown above the value (default Name)
	Parameter
	Multiline bool `json:"multiline,omitempty"`
	// Validate checks a value the type, Required and the choices allow;
	// its error is why the value is not written.
	Validate func(value string) error `json:"-"`
}

// RecordWrite is one write an editor hands Save: the record as it is to be
// stored (every field's last valid value) and the fields this write
// changes, in schema order.
type RecordWrite struct {
	Values  Record
	Changed []string
}

// String reads a write in one line: "write title=milk status=doing", the
// changed fields in order (a value with spaces or empty is quoted).
func (w RecordWrite) String() string {
	parts := []string{"write"}
	for _, name := range w.Changed {
		v := w.Values[name]
		if v == "" || strings.ContainsAny(v, " \t\n\"") {
			v = strconv.Quote(v)
		}
		parts = append(parts, name+"="+v)
	}
	return strings.Join(parts, " ")
}

// RecordSpec is what an application gives a record editor: the schema, how
// to load the record and how to write it.
type RecordSpec struct {
	Title  string // the modal's title; a placed editor's heading is the application's
	Fields []RecordField
	// Load reads the record when the editor opens (nil: every field empty).
	Load func() (Record, error)
	// Save writes one change: Done, Failed, or Pending work that runs off
	// the loop. At most one write is in flight; changes made meanwhile go
	// in the next write. A failed write keeps the values on screen, says
	// so, and is retried with the next change.
	Save func(RecordWrite) Result
	// Debounce is the pause in typing before a text field is written
	// (DefaultRecordDebounce when zero).
	Debounce time.Duration
	// Insert opens the editor with the first field in insert mode when it
	// is a text field.
	Insert bool
	// OnLeave runs after the editor closed (form.leave): every change is
	// written or queued by then; a placed editor stops being drawn.
	OnLeave func()
}

// DefaultRecordDebounce is how long typing in a field pauses before the
// record is written.
const DefaultRecordDebounce = DefaultTreeDebounce

// RecordDebounceMsg is the timer a pause in typing sets off: when Seq is
// still the editor's latest change, the record is written.
type RecordDebounceMsg struct {
	Editor string
	Seq    uint64
}

// Form nouns: the editor, its text fields (single- and multi-line), its
// choice fields, and the chooser's rows. ("form", not "record", so the
// palette's words do not collide with the macro verb record.)
const (
	FormNoun       = "app.form"
	FormFieldNoun  = "app.form-field"
	FormTextNoun   = "app.form-text"
	FormChoiceNoun = "app.form-choice"
	FormOptionNoun = "app.form-option"
	// FormSuggestNoun is a free text field with suggestions: an input
	// whose enter (normal) and ctrl+space open the suggestions chooser.
	FormSuggestNoun = "app.form-suggest"
	formChooseID    = "form-choose"
	formChooseList  = "__form-choose/list"
	// FormSuggestionsKind is the suggestions chooser's modal kind
	// (Runtime.Chooser(FormSuggestionsKind) while it is open).
	FormSuggestionsKind = "form-suggestions"
)

// formWriteVerb names a pending write's job.
const formWriteVerb = "app.form.write"

// recordStep is one undo step: a field's written value before and after.
type recordStep struct {
	field    string
	from, to string
}

type recordField struct {
	RecordField
	id      string
	input   *Input // nil for a choice field
	value   string // a choice field's value
	before  string // the written value when the current edit began
	choices []Choice
	choose  bool
	suggest bool // a free text field with suggestions
}

func (f *recordField) text() string {
	if f.input != nil {
		return string(f.input.Text)
	}
	return f.value
}

func (f *recordField) label() string {
	if f.Label != "" {
		return f.Label
	}
	return f.Name
}

// RecordEditor is a form over one record: a field per schema entry, each a
// focus stop, and every change written as it is made. Typing in a text
// field is written after a pause (Debounce) and at once when the field is
// left or insert mode ends; a choice, an edit in normal mode, an undo or a
// redo is written at once. A value that is not valid (Required, the type,
// the choices, Validate) is not written, and its label says why. u and
// ctrl+r undo and redo the record's written changes, each written like any
// other; q and esc leave, and nothing is ever discarded. There is no save
// key.
//
// The editor is an instance of the form noun (id) with a child per field
// (id/name): text fields are inputs of form-field (form-text when
// multiline), choice fields instances of form-choice, whose enter opens the
// chooser (a modal list of form-option rows).
type RecordEditor struct {
	r      *Runtime
	id     string
	spec   RecordSpec
	fields []*recordField
	// Styles colour the labels (the zero value draws them plain); a modal
	// editor uses the Model's.
	Styles Styles

	written  Record   // the record as written, or queued to be
	dirty    []string // fields written in memory, not yet in the store
	inflight bool
	writeErr string
	undo     []recordStep
	redo     []recordStep
	seq      uint64
	seen     string
	focus    string // the focus at the last PendingCommand
	typing   bool   // whether a text field was being typed in then
	modal    bool
	closed   bool
	failure  error
}

// AddRecordEditor places a record editor under Parent (default app); the
// application draws it with Block in its page. FocusOnClose names where
// the focus goes when it leaves. Call Focus to start editing.
func (r *Runtime) AddRecordEditor(id string, spec RecordSpec, opts ...InstanceOption) (*RecordEditor, error) {
	if id == "" || r.Instances[id] != nil {
		return nil, fmt.Errorf("record editor ID %q must be nonempty and unused", id)
	}
	spec2 := instanceSpec{parent: "app"}
	for _, opt := range opts {
		opt(&spec2)
	}
	if r.Instances[spec2.parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", spec2.parent)
	}
	return r.newRecordEditor(id, spec, spec2.parent, spec2.closeFocus)
}

// OpenRecordEditor opens a record editor as a modal over the focus, its
// first field focused; leaving closes the modal and restores the focus and
// mode it opened over.
func (r *Runtime) OpenRecordEditor(id string, spec RecordSpec) (*RecordEditor, error) {
	if id == "" || r.Instances[id] != nil {
		return nil, fmt.Errorf("record editor ID %q must be nonempty and unused", id)
	}
	if err := r.openFromTopLevel(id); err != nil {
		return nil, err
	}
	e, err := r.newRecordEditor(id, spec, "app", "")
	if err != nil {
		return nil, err
	}
	e.modal = true
	if err := r.Push(id, e.fields[0].id); err != nil {
		_ = r.RemoveInstance(id)
		return nil, err
	}
	if err := e.startInsert(); err != nil {
		return e, err
	}
	e.focus = r.Focus
	return e, nil
}

func (r *Runtime) newRecordEditor(id string, spec RecordSpec, parent, closeFocus string) (*RecordEditor, error) {
	if len(spec.Fields) == 0 {
		return nil, errors.New("record editor needs at least one field")
	}
	if spec.Debounce <= 0 {
		spec.Debounce = DefaultRecordDebounce
	}
	values := Record{}
	if spec.Load != nil {
		loaded, err := spec.Load()
		if err != nil {
			return nil, fmt.Errorf("load record: %w", err)
		}
		maps.Copy(values, loaded)
	}
	names := map[string]bool{}
	for _, f := range spec.Fields {
		if f.Name == "" || names[f.Name] || strings.Contains(f.Name, "/") {
			return nil, fmt.Errorf("record field name %q must be nonempty, unique and without /", f.Name)
		}
		names[f.Name] = true
	}
	e := &RecordEditor{r: r, id: id, spec: spec, written: Record{}}
	r.Instance(id, Parent(parent), Kind(FormNoun), FocusOnClose(closeFocus)).Component = e
	for _, field := range spec.Fields {
		f := &recordField{RecordField: field, id: id + "/" + field.Name}
		f.choose = field.Type == "boolean" || ((len(field.Enum) > 0 || field.Source != "") && !field.Free)
		f.suggest = !f.choose && (len(field.Enum) > 0 || field.Source != "")
		v := values[field.Name]
		e.written[field.Name], f.before = v, v
		if f.choose {
			f.value = v
			r.Instance(f.id, Parent(id), Kind(FormChoiceNoun))
		} else {
			noun := FormFieldNoun
			multiline := field.Multiline && !f.suggest
			switch {
			case f.suggest:
				noun = FormSuggestNoun
			case multiline:
				noun = FormTextNoun
			}
			f.Multiline = multiline
			f.input = NewInput(f.id, v, multiline)
			f.input.Cursor = len(f.input.Text)
			if err := r.AddInput(f.input, Parent(id), Kind(noun)); err != nil {
				_ = r.RemoveInstance(id)
				return nil, err
			}
		}
		e.fields = append(e.fields, f)
		if f.choose {
			e.loadChoices(f)
		}
	}
	e.seen = e.snapshot()
	e.focus = r.Focus
	return e, nil
}

// RecordFields is a form's schema from a verb's parameters, in the order
// bare CLI words fill them (Operation.ParameterOrder): a mounted tool's
// edit verb gets a form from its catalog alone.
func RecordFields(op Operation) []RecordField {
	var out []RecordField
	for _, name := range op.ParameterOrder() {
		out = append(out, RecordField{Name: name, Parameter: op.Parameters[name]})
	}
	return out
}

// RecordEditor is the record editor with this id, or nil.
func (r *Runtime) RecordEditor(id string) *RecordEditor {
	if inst := r.Instances[id]; inst != nil {
		if e, ok := inst.Component.(*RecordEditor); ok {
			return e
		}
	}
	return nil
}

// recordEditorOf is the editor on a focus's ancestry, or nil.
func (r *Runtime) recordEditorOf(focus string) *RecordEditor {
	for _, id := range r.Ancestry(focus) {
		if e := r.RecordEditor(id); e != nil {
			return e
		}
	}
	return nil
}

// ID is the editor's instance id.
func (e *RecordEditor) ID() string { return e.id }

// Closed reports whether the editor was left (form.leave) or removed.
func (e *RecordEditor) Closed() bool { return e.closed }

// Values is every field's value as shown, valid or not.
func (e *RecordEditor) Values() Record {
	out := Record{}
	for _, f := range e.fields {
		out[f.Name] = f.text()
	}
	return out
}

// Written is the record as written (or queued to be): the last valid value
// of every field.
func (e *RecordEditor) Written() Record { return maps.Clone(e.written) }

// Problem says why a field's value is not written, "" when it is.
func (e *RecordEditor) Problem(name string) string {
	if f := e.field(name); f != nil {
		return e.problem(f)
	}
	return ""
}

// WriteError is the last write's failure, "" once a write succeeds.
func (e *RecordEditor) WriteError() string { return e.writeErr }

// FocusTarget is where a jump to the editor lands: its first field.
func (e *RecordEditor) FocusTarget() string { return e.fields[0].id }

// Focus focuses the first field (in insert mode when the spec says so).
func (e *RecordEditor) Focus() error {
	if err := e.r.SetFocus(e.fields[0].id); err != nil {
		return err
	}
	return e.startInsert()
}

func (e *RecordEditor) startInsert() error {
	if f := e.fields[0]; e.spec.Insert && f.input != nil {
		return e.r.SetMode("insert")
	}
	return nil
}

func (e *RecordEditor) field(name string) *recordField {
	for _, f := range e.fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func (e *RecordEditor) fieldAt(id string) *recordField {
	for _, f := range e.fields {
		if f.id == id {
			return f
		}
	}
	return nil
}

// fieldOn is the field on a focus's ancestry (a chooser row's field too).
func (e *RecordEditor) fieldOn(focus string) *recordField {
	for _, id := range e.r.Ancestry(focus) {
		if f := e.fieldAt(id); f != nil {
			return f
		}
	}
	return nil
}

func (e *RecordEditor) snapshot() string {
	parts := make([]string, len(e.fields))
	for i, f := range e.fields {
		parts[i] = f.text()
	}
	return strings.Join(parts, "\x00")
}

// loadChoices reads a choice field's values: true and false for a
// boolean, else the Enum then the Source's result.
func (e *RecordEditor) loadChoices(f *recordField) {
	if f.Type == "boolean" && len(f.Enum) == 0 && f.Source == "" {
		f.choices = []Choice{{ID: "true", Label: "true"}, {ID: "false", Label: "false"}}
		return
	}
	if len(f.Enum) == 0 && f.Source == "" {
		return
	}
	choices, err := e.r.Choices(e.r.contextOf(e.id), f.Parameter)
	if err != nil && len(choices) == 0 {
		return
	}
	f.choices = choices
}

// problem says why a value is not written: required and empty, not of the
// type, not one of the choices, or Validate's error.
func (e *RecordEditor) problem(f *recordField) string {
	v := f.text()
	if strings.TrimSpace(v) == "" {
		if f.Required {
			return "required"
		}
		return ""
	}
	switch f.Type {
	case "integer":
		if _, err := strconv.Atoi(strings.TrimSpace(v)); err != nil {
			return "a whole number"
		}
	case "number":
		if _, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err != nil {
			return "a number"
		}
	case "boolean":
		if v != "true" && v != "false" {
			return "true or false"
		}
	}
	if !f.Free && (len(f.Enum) > 0 || f.Source != "") && len(f.choices) > 0 && !hasChoice(f.choices, v) {
		ids := make([]string, len(f.choices))
		for i, c := range f.choices {
			ids[i] = c.ID
		}
		return "one of " + strings.Join(ids, ", ")
	}
	if f.Validate != nil {
		if err := f.Validate(v); err != nil {
			return err.Error()
		}
	}
	return ""
}

// flush takes every changed valid value into the record and writes it.
func (e *RecordEditor) flush() error {
	for _, f := range e.fields {
		v := f.text()
		if v == e.written[f.Name] || e.problem(f) != "" {
			continue
		}
		e.written[f.Name] = v
		if !slices.Contains(e.dirty, f.Name) {
			e.dirty = append(e.dirty, f.Name)
		}
	}
	return e.send()
}

// send hands Save the record when fields are dirty and no write is in
// flight; a pending write's completion sends what changed meanwhile.
func (e *RecordEditor) send() error {
	if e.inflight || len(e.dirty) == 0 {
		return nil
	}
	var changed []string
	for _, f := range e.fields {
		if slices.Contains(e.dirty, f.Name) {
			changed = append(changed, f.Name)
		}
	}
	e.dirty = nil
	if e.spec.Save == nil {
		return nil
	}
	res := e.spec.Save(RecordWrite{Values: maps.Clone(e.written), Changed: slices.Clone(changed)})
	switch {
	case res.err != nil:
		e.requeue(changed)
		e.writeErr = res.err.Error()
		return fmt.Errorf("write %s: %w", strings.Join(changed, ", "), res.err)
	case res.work == nil:
		e.writeErr = ""
		return nil
	}
	// The job belongs to the editor's parent, so leaving the editor never
	// cancels a write in flight.
	owner := e.r.Instances[e.id].Parent
	if e.r.Instances[owner] == nil {
		owner = "app"
	}
	inner := res.then
	res.then = func(c Ctx, v any, err error) {
		if inner != nil {
			inner(c, v, err)
		}
		e.inflight = false
		if err != nil {
			e.requeue(changed)
			e.writeErr = err.Error()
			return
		}
		e.writeErr = ""
		if serr := e.send(); serr != nil {
			e.fail(serr)
		}
	}
	e.inflight = true
	c := Ctx{Runtime: e.r, Context: e.r.contextOf(owner), Instance: e.r.Instances[owner]}
	if _, err := e.r.Launch(owner, formWriteVerb, res, c); err != nil {
		e.inflight = false
		e.requeue(changed)
		e.writeErr = err.Error()
		return err
	}
	return nil
}

func (e *RecordEditor) requeue(changed []string) {
	for _, name := range changed {
		if !slices.Contains(e.dirty, name) {
			e.dirty = append(e.dirty, name)
		}
	}
}

// seal writes the record and makes a field's change since its edit began
// one undo step.
func (e *RecordEditor) seal(f *recordField) error {
	err := e.flush()
	if f != nil && e.written[f.Name] != f.before {
		e.undo = append(e.undo, recordStep{field: f.Name, from: f.before, to: e.written[f.Name]})
		e.redo = nil
		f.before = e.written[f.Name]
	}
	return err
}

// set replaces a field's value as shown.
func (e *RecordEditor) set(f *recordField, v string) error {
	if f.input == nil {
		f.value = v
		return nil
	}
	if err := f.input.Replace(0, len(f.input.Text), v); err != nil {
		return err
	}
	f.input.Cursor = len(f.input.Text)
	return nil
}

// history undoes (or redoes) one written change: the value goes back, is
// written at once, and its field takes the focus.
func (e *RecordEditor) history(redo bool) error {
	err := e.seal(e.fieldOn(e.r.Focus))
	from, to := &e.undo, &e.redo
	if redo {
		from, to = &e.redo, &e.undo
	}
	if len(*from) == 0 {
		return err
	}
	step := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	f := e.field(step.field)
	if f == nil {
		return err
	}
	if serr := e.set(f, step.from); serr != nil {
		return serr
	}
	werr := e.flush()
	f.before = e.written[f.Name]
	*to = append(*to, recordStep{field: step.field, from: step.to, to: step.from})
	e.seen = e.snapshot()
	if e.r.Focus != f.id {
		if ferr := e.r.SetFocus(f.id); ferr != nil && werr == nil {
			werr = ferr
		}
	}
	e.focus = e.r.Focus
	return errors.Join(err, werr)
}

// move focuses the field delta places from the focused one, clamped at the
// first and last; insert keeps typing when the field is a text field.
func (e *RecordEditor) move(delta int, insert bool) error {
	i := slices.Index(e.fields, e.fieldOn(e.r.Focus))
	if i < 0 {
		i = 0
	}
	j := min(max(0, i+delta), len(e.fields)-1)
	if j != i {
		if err := e.r.SetFocus(e.fields[j].id); err != nil {
			return err
		}
	}
	if insert && e.fields[j].input != nil {
		return e.r.SetMode("insert")
	}
	if e.r.Mode.Name == "insert" && e.fields[j].input == nil {
		return e.r.SetMode("normal")
	}
	return nil
}

// recordChooser is the chooser modal's body: it draws the option list.
type recordChooser struct {
	editor *RecordEditor
	field  *recordField
}

func (c *recordChooser) ModalTitle() string { return "Choose " + c.field.label() }

func (c *recordChooser) ModalBlock(s Styles, width, height int) Block {
	list := c.editor.r.List(formChooseList)
	if list == nil {
		return Text("")
	}
	return Text(list.View(width, min(height, max(1, list.Height()))))
}

// choose opens the chooser for a choice field: its choices as a list, the
// current value selected (an optional field offers "(none)" first).
func (e *RecordEditor) choose(f *recordField) error {
	if f == nil || !f.choose {
		return errors.New("the focused field is not a choice field")
	}
	e.loadChoices(f)
	if len(f.choices) == 0 {
		return fmt.Errorf("%s has no choices", f.label())
	}
	var items []ListItem
	if !f.Required {
		items = append(items, ListItem{ID: formChooseList + "/0", Label: "(none)", Noun: FormOptionNoun, Data: ""})
	}
	for _, c := range f.choices {
		items = append(items, ListItem{ID: fmt.Sprintf("%s/%d", formChooseList, len(items)), Label: choiceLabel(c), Noun: FormOptionNoun, Data: c.ID})
	}
	inst, err := e.r.OpenModal(formChooseID, Payload{})
	if err != nil {
		return err
	}
	inst.Component = &recordChooser{editor: e, field: f}
	list, err := e.r.AddList(formChooseList, items, Parent(formChooseID), NoSpill())
	if err != nil {
		_ = e.r.Pop()
		return err
	}
	for _, it := range items {
		if it.Data == f.value {
			_ = list.Select(it.ID)
		}
	}
	return list.Focus()
}

// suggest opens the framework chooser over a free field's suggestions:
// the Enum at once, the Source's result when it lands (the load is a job
// of the chooser, so closing it cancels the load). A pick fills the field
// and writes it at once; with a query nothing matches, enter (or the
// create row) takes the query as typed.
func (e *RecordEditor) suggest(f *recordField) error {
	if f == nil || !f.suggest {
		return errors.New("the focused field has no suggestions")
	}
	fill := func(v string) Result {
		if err := e.fill(f, v); err != nil {
			return Failed(err)
		}
		return Done(v)
	}
	c, err := e.r.OpenChooser(ChooserSpec{
		Kind:        FormSuggestionsKind,
		Title:       "Suggest " + f.label(),
		Empty:       "loading…",
		Choose:      func(row ChooserRow) Result { return fill(row.ID) },
		Create:      fill,
		CreateRow:   true,
		CreateLabel: func(query string) string { return "use " + strconv.Quote(query) },
	})
	if err != nil {
		return err
	}
	e.r.loadChoices(e.r.contextOf(f.id), f.Parameter, FormSuggestionsKind, func(choices []Choice, err error) {
		if e.r.Chooser(FormSuggestionsKind) != c {
			return
		}
		f.choices = choices
		rows := make([]ChooserRow, len(choices))
		for i, ch := range choices {
			rows[i] = ChooserRow{ID: ch.ID, Label: choiceLabel(ch)}
		}
		switch {
		case err != nil && len(rows) == 0:
			c.spec.Empty = "no suggestions: " + err.Error()
		case len(rows) == 0:
			c.spec.Empty = "no suggestions"
		}
		c.SetRows(rows)
	})
	return nil
}

// fill sets a text field to a suggestion (the chooser has closed and the
// focus is back on the field) and writes it at once.
func (e *RecordEditor) fill(f *recordField, v string) error {
	if err := e.set(f, v); err != nil {
		return err
	}
	e.seen = e.snapshot()
	return e.seal(f)
}

// pick sets the chooser's field to a choice, writes it at once and closes
// the chooser.
func (e *RecordEditor) pick(f *recordField, v string) error {
	if err := e.r.Pop(); err != nil {
		return err
	}
	f.value = v
	e.seen = e.snapshot()
	return e.seal(f)
}

// leave writes what is pending and closes the editor.
func (e *RecordEditor) leave() error {
	err := e.seal(e.fieldOn(e.r.Focus))
	e.closed = true
	if e.modal && e.r.TopModal() == e.id {
		if perr := e.r.Pop(); perr != nil {
			return errors.Join(err, perr)
		}
	}
	if e.r.Instances[e.id] != nil {
		if rerr := e.r.RemoveInstance(e.id); rerr != nil {
			return errors.Join(err, rerr)
		}
	}
	if e.spec.OnLeave != nil {
		e.spec.OnLeave()
	}
	return err
}

// Block draws the form: per field its label (with why a value is not
// written) and its value, a text field as its input, a choice field as its
// value with the one highlight while focused; a failed write is the last
// line. Each field is placed in drawing order, so motions follow it.
func (e *RecordEditor) Block(width, height int) Block {
	width, height = max(1, width), max(1, height)
	s := e.Styles
	var lines []string
	var cursor *Pos
	for _, f := range e.fields {
		focused := e.fieldOn(e.r.Focus) == f
		label := f.label()
		style := s.Muted
		if focused {
			style = s.Accent
		}
		if problem := e.problem(f); problem != "" {
			label += " · not written: " + problem
			style = s.Error
		}
		lines = append(lines, style.Render(ansi.Truncate(label, width, "…")))
		inner := max(1, width-2)
		if f.input != nil {
			h := 1
			if f.Multiline {
				h = min(max(3, strings.Count(string(f.input.Text), "\n")+1), max(3, height/3))
			}
			block := InputBlock(f.input, inner, h)
			if block.Cursor != nil && e.r.Focus == f.id {
				cursor = &Pos{X: block.Cursor.X + 2, Y: len(lines) + block.Cursor.Y}
			}
			for _, row := range strings.Split(block.Text, "\n") {
				lines = append(lines, "  "+row)
			}
			continue
		}
		value := "(none)"
		for _, c := range f.choices {
			if c.ID == f.value {
				value = choiceLabel(c)
			}
		}
		if f.value != "" && value == "(none)" {
			value = f.value
		}
		value = ansi.Truncate(value+" ▾", inner, "…")
		if focused && e.r.Focus == f.id {
			value = reverseVideo(value)
		}
		lines = append(lines, e.r.PlacedAt(f.id, "  "+value, 0, 1))
	}
	if e.writeErr != "" {
		lines = append(lines, s.Error.Render(ansi.Truncate("! not saved: "+e.writeErr, width, "…")))
	}
	return Block{Text: strings.Join(lines, "\n"), Cursor: cursor}
}

// reverseVideo draws text in reverse video: the one highlight.
func reverseVideo(text string) string { return "\x1b[7m" + text + "\x1b[27m" }

// ModalTitle and ModalBlock draw a modal editor (ModalBody).
func (e *RecordEditor) ModalTitle() string {
	if e.spec.Title != "" {
		return e.spec.Title
	}
	return "Edit"
}

func (e *RecordEditor) ModalBlock(s Styles, width, height int) Block {
	e.Styles = s
	return e.Block(width, height)
}

// HandleMessage writes the record when its pause timer is the latest.
func (e *RecordEditor) HandleMessage(msg tea.Msg) bool {
	tick, ok := msg.(RecordDebounceMsg)
	if !ok || tick.Editor != e.id {
		return false
	}
	if e.DebounceCurrent(tick) {
		e.fail(e.flush())
	}
	return true
}

// DebounceCurrent reports whether a pause timer is for the latest change.
func (e *RecordEditor) DebounceCurrent(tick RecordDebounceMsg) bool {
	return tick.Editor == e.id && tick.Seq == e.seq && !e.closed
}

// PendingCommand notices changes: typing in a text field sets the pause
// timer; leaving a field, ending insert mode or any other change writes at
// once and closes the field's undo step.
func (e *RecordEditor) PendingCommand() tea.Cmd {
	if e.closed {
		return nil
	}
	focus := e.r.Focus
	f := e.fieldAt(focus)
	typing := e.r.Mode.Name == "insert" && f != nil && f.input != nil
	var cmd tea.Cmd
	changed := false
	if snap := e.snapshot(); snap != e.seen {
		e.seen, changed = snap, true
		e.seq++
		if typing {
			seq, id := e.seq, e.id
			cmd = tea.Tick(e.spec.Debounce, func(time.Time) tea.Msg { return RecordDebounceMsg{Editor: id, Seq: seq} })
		}
	}
	// Only a change, a move off the field or the end of insert writes, so a
	// failed write is retried at the next of them, not on every message.
	left := focus != e.focus || (e.typing && !typing)
	if prev := e.fieldAt(e.focus); prev != nil && (left || (changed && !typing)) {
		e.fail(e.seal(prev))
	} else if changed && !typing {
		e.fail(e.flush())
	}
	e.focus, e.typing = focus, typing
	return cmd
}

// Close writes what is pending (the editor is being removed).
func (e *RecordEditor) Close() {
	if !e.closed {
		e.closed = true
		_ = e.flush()
	}
}

// Flush writes what is pending now (on quit).
func (e *RecordEditor) Flush() error { return e.flush() }

func (e *RecordEditor) fail(err error) {
	if err != nil {
		e.failure = err
	}
}

// Failure is the last background failure, cleared when read.
func (e *RecordEditor) Failure() error {
	err := e.failure
	e.failure = nil
	return err
}

// registerForms defines the form nouns and their rows.
func registerForms(r *Runtime) {
	on := func(run func(*RecordEditor, Context, Arguments) error) Handler {
		return func(r *Runtime, c Context, a Arguments) (any, error) {
			e := r.recordEditorOf(focusOf(c))
			if e == nil {
				return nil, errors.New("no focused record editor")
			}
			return nil, run(e, c, a)
		}
	}
	insert := map[string]Parameter{"insert": {Type: "boolean", Description: "keep typing when the field is a text field"}}
	r.Define(FormNoun).
		Verb("leave", on(func(e *RecordEditor, _ Context, _ Arguments) error { return e.leave() }), "Leave the editor; every change is already written").
		Verb("undo", on(func(e *RecordEditor, _ Context, _ Arguments) error { return e.history(false) }), "Undo the record's last change; the undo is written like any change").
		Verb("redo", on(func(e *RecordEditor, _ Context, _ Arguments) error { return e.history(true) }), "Redo the record's last undone change").
		Verb("next", on(func(e *RecordEditor, _ Context, a Arguments) error { return e.move(1, boolArg(a, "insert")) }), "Focus the next field; the field left is written").Motion().
		Verb("previous", on(func(e *RecordEditor, _ Context, a Arguments) error { return e.move(-1, boolArg(a, "insert")) }), "Focus the previous field; the field left is written").Motion().
		Verb("choose", on(func(e *RecordEditor, c Context, _ Arguments) error { return e.choose(e.fieldOn(focusOf(c))) }), "Choose the field's value from its choices").
		Verb("suggest", on(func(e *RecordEditor, c Context, _ Arguments) error { return e.suggest(e.fieldOn(focusOf(c))) }), "Fill the field from its suggestions; typing stays free").
		Mode("normal").
		Bind("q", Do("app.form.leave")).
		Bind("esc", Do("app.form.leave")).
		Bind("u", Do("app.form.undo")).
		Bind("ctrl+r", Do("app.form.redo"))
	for _, verb := range []string{"next", "previous"} {
		op := r.Operations[FormNoun+"."+verb]
		op.Parameters = insert
		r.Operations[FormNoun+"."+verb] = op
	}
	r.Define(FormFieldNoun).
		Mode("normal").
		Bind("j", Do("app.form.next")).
		Bind("down", Do("app.form.next")).
		Bind("k", Do("app.form.previous")).
		Bind("up", Do("app.form.previous")).
		Mode("insert").
		Bind("enter", Do("app.form.next", Arguments{"insert": true})).
		Bind("tab", Do("app.form.next", Arguments{"insert": true})).
		Bind("shift+tab", Do("app.form.previous", Arguments{"insert": true}))
	r.Define(FormSuggestNoun).
		Mode("normal").
		Bind("j", Do("app.form.next")).
		Bind("down", Do("app.form.next")).
		Bind("k", Do("app.form.previous")).
		Bind("up", Do("app.form.previous")).
		Bind("enter", Do("app.form.suggest")).
		Bind("ctrl+space", Do("app.form.suggest")).
		Mode("insert").
		Bind("enter", Do("app.form.next", Arguments{"insert": true})).
		Bind("tab", Do("app.form.next", Arguments{"insert": true})).
		Bind("shift+tab", Do("app.form.previous", Arguments{"insert": true})).
		Bind("ctrl+space", Do("app.form.suggest"))
	r.Define(FormTextNoun).
		Mode("insert").
		Bind("tab", Do("app.form.next", Arguments{"insert": true})).
		Bind("shift+tab", Do("app.form.previous", Arguments{"insert": true}))
	r.Define(FormChoiceNoun).Focusable().
		Mode("normal").
		Bind("j", Do("app.form.next")).
		Bind("down", Do("app.form.next")).
		Bind("k", Do("app.form.previous")).
		Bind("up", Do("app.form.previous")).
		Bind("enter", Do("app.form.choose")).
		Bind("space", Do("app.form.choose")).
		Bind("i", Do("app.form.choose"))
	r.Define(FormOptionNoun).
		Verb("pick", func(r *Runtime, c Context, _ Arguments) (any, error) {
			inst := r.Instances[focusOf(c)]
			chooser, _ := r.Instances[formChooseID].Component.(*recordChooser)
			if inst == nil || chooser == nil {
				return nil, errors.New("no open chooser")
			}
			v, _ := inst.Data.(string)
			return v, chooser.editor.pick(chooser.field, v)
		}, "Set the field to this choice; written at once").
		Mode("normal").
		Bind("enter", Do("app.form-option.pick")).
		Bind("space", Do("app.form-option.pick")).
		Bind("q", Do("app.modal.pop"))
}

func boolArg(a Arguments, name string) bool {
	v, _ := a[name].(bool)
	return v
}
