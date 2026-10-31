package ochazuke

import (
	"context"
	"encoding/json"
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
// Source, Noun, Free, Description), so a form can come from a verb's
// catalog parameters (RecordFields). A field with choices (Enum, Source or
// Noun: Runtime.Choices) and not Free, or of Type boolean, is edited
// through a chooser, never as free text. A Free field with choices is a
// text field with suggestions (form-suggest):
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
	// Secret makes the field's input secret (Input.Secret): what is typed
	// into it is never echoed. PropField sets it from a Secret prop.
	Secret bool `json:"secret,omitempty"`
	// Placeholder is shown, muted, in the field while its text is empty
	// (a rename's Fallback: what the thing shows with no name).
	Placeholder string `json:"placeholder,omitempty"`
	// Validate checks a value the type, Required and the choices allow;
	// its error is why the value is not written.
	Validate func(value string) error `json:"-"`
	// Chips makes a field with choices take several of them: its value is
	// a JSON array of choice ids ("" for none), drawn as chips wrapped
	// across the lines under its label (record_chips.go).
	Chips bool `json:"chips,omitempty"`
	// Invertible gives the field a normal and an inverted state
	// (form.invert); the record carries it as the field's name plus
	// RecordInvertSuffix, "true" while inverted.
	Invertible bool `json:"invertible,omitempty"`
	// Completions complete a text field's tokens as it is typed
	// (completion.go): ctrl+n and ctrl+p walk the suggestions.
	Completions []CompletionTrigger `json:"-"`
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
	// Rename says the editor renames a thing: it opens in insert like
	// Insert, and with the start field's text selected (Input.SelectAll)
	// while the rename.select setting is on, so typing replaces the name
	// and a motion keeps it.
	Rename bool
	// OnLeave runs after the editor closed (form.leave, or form.run's
	// Submit): every change is written or queued by then; a placed editor
	// stops being drawn.
	OnLeave func()
	// Submit, when set, makes the editor a form that runs once instead of
	// writing each change: Save is never called, changes stay on screen,
	// and form.run (available once every field is valid) closes a modal
	// editor and hands Submit every value as shown. A failed Submit
	// reopens the modal with the values and the error (`! not run:`);
	// leaving (form.leave, esc) runs nothing.
	Submit func(Record) Result
	// Start names the field focused first ("" the first field).
	Start string
	// Clearable offers form.clear: every field emptied and every inverted
	// field back to normal, each written (and undone) like any change.
	Clearable bool
	// Watch names the noun whose store holds the record: when its things
	// change other than through this editor (a verb such as set, another
	// client, Runtime.Changed), the open editor loads the record again
	// (forms.outside-write: reload keeps it open, close leaves it). ""
	// watches nothing; Reload does the same on the application's call.
	Watch string
}

// FormsOutsideWriteSetting says what an open record editor does when its
// record is written from outside it (RecordSpec.Watch, Reload): reload
// (the default) takes the new values and stays open, focus, field and mode
// kept, a field with unsaved typing keeping the typing; close leaves the
// editor (e edits again), as before.
const FormsOutsideWriteSetting = "forms.outside-write"

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
	FormNoun       = "ochazuke.form"
	FormFieldNoun  = "ochazuke.form-field"
	FormTextNoun   = "ochazuke.form-text"
	FormChoiceNoun = "ochazuke.form-choice"
	FormOptionNoun = "ochazuke.form-option"
	// FormSuggestNoun is a free text field with suggestions: an input
	// whose enter (normal) and ctrl+space open the suggestions chooser.
	FormSuggestNoun = "ochazuke.form-suggest"
	formChooseID    = "form-choose"
	formChooseList  = "__form-choose/list"
	// FormSuggestionsKind is the suggestions chooser's modal kind
	// (Runtime.Chooser(FormSuggestionsKind) while it is open).
	FormSuggestionsKind = "form-suggestions"
)

// formWriteVerb names a pending write's job; formRunVerb a pending
// Submit's.
const (
	formWriteVerb = "ochazuke.form.write"
	formRunVerb   = "ochazuke.form.run"
)

// Form availability: form.run is available on a form that runs once
// (Submit) whose every field is valid (form-runnable), form.next-missing
// on one with a field still not valid (form-missing). On such a form one
// of the two always is, so enter's rows (run, next-missing, the field's
// own action) never fall through to the field's own; on an editor that
// writes each change neither is, and enter keeps its old action.
const (
	FormRunnableGuard = "form-runnable"
	FormMissingGuard  = "form-missing"
)

// paramHasChoices reports whether a parameter's values come from a list:
// an Enum, a Source, or a Noun's things (Runtime.Choices).
func paramHasChoices(p Parameter) bool {
	return len(p.Enum) > 0 || p.Source != "" || p.Noun != ""
}

// recordStep is one undo step: a field's written value before and after.
type recordStep struct {
	field    string
	from, to string
}

type recordField struct {
	RecordField
	id            string
	input         *Input // nil for a choice field
	value         string // a choice field's value
	before        string // the written value when the current edit began
	chooser       *Chooser
	choiceKey     string
	choiceSeq     uint64
	choicesReady  bool
	choiceLoading bool
	choices       []Choice
	choose        bool
	suggest       bool // a free text field with suggestions
	path          bool // a path field: its chooser is the path chooser
	chips         bool // a choice field that takes several choices (Chips)
	chip          int  // the chip under the cursor, an index into choices
	// lines are the chips' rows as last drawn (indexes into choices), so
	// j and k move by what is on screen.
	lines [][]int
	not   bool // an Invertible field's state as shown
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
	sending  []string // the fields of the write in flight
	watch    *Watch   // the Watch noun's subscription while open
	writeErr string
	undo     []recordStep
	redo     []recordStep
	seq      uint64
	seen     string
	focus    string // the focus at the last PendingCommand
	typing   bool   // whether a text field was being typed in then
	modal    bool
	closed   bool
	loading  bool // outsideWrite is loading the record
	ran      bool // form.run handed the values to Submit
	failure  error
}

// AddRecordEditor places a record editor under Parent (default app); the
// application draws it with Block in its page. FocusOnClose names where
// the focus goes when it leaves. Call Focus to start editing.
func (r *Runtime) AddRecordEditor(id string, spec RecordSpec, opts ...InstanceOption) (*RecordEditor, error) {
	r.freeClosedEditor(id)
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
	r.freeClosedEditor(id)
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
	if err := r.Push(id, e.fields[e.start()].id); err != nil {
		_ = r.RemoveInstance(id)
		return nil, err
	}
	if err := e.startInsert(); err != nil {
		return e, err
	}
	e.focus = r.Focus
	return e, nil
}

// freeClosedEditor removes a closed record editor still registered under
// id (closed through Close, which leaves its instance), so the id can be
// used again: a closed editor is never in the way of opening one.
func (r *Runtime) freeClosedEditor(id string) {
	inst := r.Instances[id]
	if inst == nil {
		return
	}
	if e, ok := inst.Component.(*RecordEditor); ok && e.closed {
		_ = r.RemoveInstance(id)
	}
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
		if f.Name == "" || names[f.Name] || strings.ContainsAny(f.Name, "/"+RecordInvertSuffix) {
			return nil, fmt.Errorf("record field name %q must be nonempty, unique and without / or %s", f.Name, RecordInvertSuffix)
		}
		names[f.Name] = true
	}
	params := map[string]Parameter{}
	for _, f := range spec.Fields {
		params[f.Name] = f.Parameter
	}
	if err := r.CheckSourceArgs(params); err != nil {
		return nil, err
	}
	e := &RecordEditor{r: r, id: id, spec: spec, written: Record{}}
	r.Instance(id, Parent(parent), Kind(FormNoun), FocusOnClose(closeFocus)).Component = e
	for _, field := range spec.Fields {
		f := &recordField{RecordField: field, id: id + "/" + field.Name}
		f.chips = field.Chips && (paramHasChoices(field.Parameter) || field.Type == "boolean")
		f.choose = !f.chips && (field.Type == "boolean" || (paramHasChoices(field.Parameter) && !field.Free))
		f.suggest = !f.chips && !f.choose && paramHasChoices(field.Parameter)
		if field.Type == PathParameterType {
			// A path field is a stop that opens the path chooser, or a
			// text field whose suggestions are the path chooser
			// (path.field).
			f.path = true
			v, _ := r.settingValue(PathFieldSetting)
			f.choose, f.suggest = v != "text", v == "text"
		}
		v := values[field.Name]
		e.written[field.Name], f.before = v, v
		if field.Invertible {
			inv := values[field.Name+RecordInvertSuffix]
			f.not = inv == "true"
			e.written[field.Name+RecordInvertSuffix] = invertText(f.not)
		}
		switch {
		case f.chips:
			f.value = v
			r.Instance(f.id, Parent(id), Kind(FormChipsNoun))
		case f.choose:
			f.value = v
			r.Instance(f.id, Parent(id), Kind(FormChoiceNoun))
		default:
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
			f.input.Secret = field.Secret
			opts := []InstanceOption{Parent(id), Kind(noun)}
			if len(field.Completions) > 0 {
				opts = append(opts, Completions(field.Completions...))
			}
			if err := r.AddInput(f.input, opts...); err != nil {
				_ = r.RemoveInstance(id)
				return nil, err
			}
		}
		e.fields = append(e.fields, f)
	}
	for _, f := range e.fields {
		if (f.choose || f.chips) && len(f.SourceArgs) == 0 {
			e.loadChoices(f)
		}
	}
	e.refreshChoices()
	e.seen = e.snapshot()
	e.focus = r.Focus
	if spec.Watch != "" {
		w, err := r.Watch(spec.Watch, Arguments{}, func(WatchEvent) { e.fail(e.outsideWrite()) })
		if err != nil {
			_ = r.RemoveInstance(id)
			return nil, fmt.Errorf("record editor watch: %w", err)
		}
		e.watch = w
	}
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

// Ran reports whether form.run handed the values to Submit.
func (e *RecordEditor) Ran() bool { return e.ran }

// FocusTarget is where a jump to the editor lands: its first field (the
// spec's Start).
func (e *RecordEditor) FocusTarget() string { return e.fields[e.start()].id }

// Focus focuses the first field (the spec's Start; in insert mode when the
// spec says so).
func (e *RecordEditor) Focus() error {
	if err := e.r.SetFocus(e.fields[e.start()].id); err != nil {
		return err
	}
	return e.startInsert()
}

// start is the index of the field focused first: Start, else 0.
func (e *RecordEditor) start() int {
	for i, f := range e.fields {
		if f.Name == e.spec.Start {
			return i
		}
	}
	return 0
}

// startInsert enters insert on the first field when the spec says so and
// it is a text field; otherwise the editor starts in normal (a form opened
// from a prompt, the palette's line, does not stay in its insert).
func (e *RecordEditor) startInsert() error {
	if f := e.fields[e.start()]; (e.spec.Insert || e.spec.Rename) && f.input != nil {
		if err := e.r.SetMode("insert"); err != nil {
			return err
		}
		if e.spec.Rename && e.r.RenameSelect() {
			return f.input.SelectAll()
		}
		return nil
	}
	if e.r.Mode.Name == "insert" {
		return e.r.SetMode("normal")
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
		parts[i] = f.text() + "\x01" + invertText(f.not)
	}
	return strings.Join(parts, "\x00")
}

// loadChoices reads a choice field's values: true and false for a
// boolean, else the Enum then the Source's result.
func (e *RecordEditor) loadChoices(f *recordField) {
	if f.path {
		return
	}
	if f.Type == "boolean" && len(f.Enum) == 0 && f.Source == "" {
		f.choices = []Choice{{ID: "true", Label: "true"}, {ID: "false", Label: "false"}}
		return
	}
	if !paramHasChoices(f.Parameter) {
		return
	}
	choices, err := e.r.Choices(e.r.contextOf(e.id), f.Parameter, e.arguments())
	if err != nil && len(choices) == 0 {
		return
	}
	f.choices, f.choicesReady = choices, err == nil
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
	if f.chips {
		return e.chipsProblem(f, v)
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
	if !f.Free && f.choiceLoading {
		return "loading choices"
	}
	if !f.Free && paramHasChoices(f.Parameter) && (len(f.SourceArgs) == 0 && len(f.choices) > 0 || len(f.SourceArgs) > 0 && f.choicesReady) && !hasChoice(f.choices, v) {
		ids := make([]string, len(f.choices))
		for i, c := range f.choices {
			ids[i] = c.ID
		}
		if len(ids) == 0 {
			return "no current choices"
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
	e.refreshChoices()
	for _, f := range e.fields {
		if f.Invertible {
			if inv := invertText(f.not); inv != e.written[f.Name+RecordInvertSuffix] {
				e.written[f.Name+RecordInvertSuffix] = inv
				e.requeue([]string{f.Name})
			}
		}
		v := f.text()
		if v == e.written[f.Name] || e.problem(f) != "" {
			continue
		}
		e.written[f.Name] = v
		e.requeue([]string{f.Name})
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
	if e.spec.Save == nil || e.spec.Submit != nil {
		return nil // a form that runs once keeps its changes on screen
	}
	// While Save runs, its fields are this editor's write: a Changed the
	// application calls from it is not an outside write of them.
	e.sending = changed
	res := e.spec.Save(RecordWrite{Values: maps.Clone(e.written), Changed: slices.Clone(changed)})
	e.sending = nil
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
		e.inflight, e.sending = false, nil
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
	e.inflight, e.sending = true, changed
	c := Ctx{actor: e.r.Actor(), Runtime: e.r, Context: e.r.contextOf(owner), Instance: e.r.Instances[owner]}
	if _, err := e.r.Launch(owner, formWriteVerb, res, c); err != nil {
		e.inflight, e.sending = false, nil
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
	name, inverted := strings.CutSuffix(step.field, RecordInvertSuffix)
	f := e.field(name)
	if f == nil {
		return err
	}
	if inverted {
		f.not = step.from == "true"
	} else if serr := e.set(f, step.from); serr != nil {
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
		e.landChips(e.fields[j], delta)
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
	if f.path {
		return e.choosePath(f)
	}
	if len(f.SourceArgs) > 0 {
		return e.suggest(f)
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
	inst, err := e.r.OpenModal(formChooseID, Payload{Anchor: f.id})
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

// suggest opens the framework chooser (type to filter) over a field's
// choices: the Enum at once, the Source's result or the Noun's things when
// they land (the load is a job of the chooser, so closing it cancels the
// load). A pick fills the field and writes it at once. On a free field,
// with a query nothing matches, enter (or the create row) takes the query
// as typed; a choice field takes only its choices, and an optional one
// offers "(none)" at the bottom.
func (e *RecordEditor) suggest(f *recordField) error {
	if f == nil || (!f.suggest && !f.choose) {
		return errors.New("the focused field has no suggestions")
	}
	if f.path {
		return e.choosePath(f)
	}
	fill := func(v string) Result {
		if err := e.fill(f, v); err != nil {
			return Failed(err)
		}
		return Done(v)
	}
	spec := ChooserSpec{
		Kind:   FormSuggestionsKind,
		Title:  "Suggest " + f.label(),
		Empty:  "loading…",
		Choose: func(row ChooserRow) Result { return fill(row.ID) },
	}
	var fixed []ChooserRow
	if f.suggest {
		spec.Create, spec.CreateRow = fill, true
		spec.CreateLabel = func(query string) string { return "use " + strconv.Quote(query) }
	} else {
		spec.Title = "Choose " + f.label()
		if !f.Required {
			fixed = append(fixed, ChooserRow{ID: "", Label: "(none)", Fixed: true})
		}
	}
	if e.r.ChoicesTakeQuery(f.Parameter) || len(f.SourceArgs) > 0 {
		// Query sources reload while typing; dependent sources also reload
		// when a sibling changes while their chooser is open.
		spec.Sources = []ChooserSource{{
			Name: "suggestions",
			Live: e.r.ChoicesTakeQuery(f.Parameter),
			Load: func(query string) Result {
				res := e.r.choicesLoad(e.r.contextOf(f.id), f.Parameter, query, e.arguments())
				rows := func(value any) []ChooserRow {
					choices, _ := value.([]Choice)
					out := make([]ChooserRow, 0, len(choices)+len(fixed))
					for _, ch := range choices {
						out = append(out, ChooserRow{ID: ch.ID, Label: choiceLabel(ch)})
					}
					return append(out, fixed...)
				}
				if res.work == nil {
					if res.err != nil {
						return res
					}
					if len(f.SourceArgs) == 0 {
						f.choices, _ = res.value.([]Choice)
					}
					return Done(rows(res.value))
				}
				work := res.work
				return Pending(func(ctx context.Context) (any, error) {
					value, err := work(ctx)
					if err != nil {
						return nil, err
					}
					return rows(value), nil
				}).Then(func(_ Ctx, value any, _ error) {
					// value is the rows; the choices they came from are
					// their ids.
					if len(f.SourceArgs) > 0 {
						return
					}
					got, _ := value.([]ChooserRow)
					f.choices = f.choices[:0]
					for _, row := range got {
						if !row.Fixed {
							f.choices = append(f.choices, Choice{ID: row.ID, Label: row.Label})
						}
					}
				})
			},
		}}
		c, err := e.r.OpenChooser(spec)
		f.chooser = c
		return err
	}
	c, err := e.r.OpenChooser(spec)
	if err != nil {
		return err
	}
	done := func(choices []Choice, err error) {
		if e.r.Chooser(FormSuggestionsKind) != c {
			return
		}
		if len(f.SourceArgs) == 0 {
			f.choices = choices
		}
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
		c.SetRows(append(rows, fixed...))
	}
	if f.Type == "boolean" && !paramHasChoices(f.Parameter) {
		e.loadChoices(f)
		done(f.choices, nil)
		return nil
	}
	e.r.loadChoices(e.r.contextOf(f.id), f.Parameter, FormSuggestionsKind, done, e.arguments())
	return nil
}

// choosePath opens the path chooser for a path field; the chosen path
// fills it and is written at once.
func (e *RecordEditor) choosePath(f *recordField) error {
	_, err := e.r.OpenPathChooser(PathChooserSpec{
		Title: "Choose " + f.label(),
		Pick:  PathPick{Name: f.Name, Parameter: f.Parameter, Value: f.text()},
		Choose: func(path string) Result {
			var err error
			if f.input != nil {
				err = e.fill(f, path)
			} else {
				f.value = path
				e.seen = e.snapshot()
				err = e.seal(f)
			}
			if err != nil {
				return Failed(err)
			}
			return Done(path)
		},
	})
	return err
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

// firstProblem is the first field, from index from on and wrapping, whose
// value is not valid; nil when every field is.
func (e *RecordEditor) firstProblem(from int) *recordField {
	n := len(e.fields)
	for k := range n {
		if f := e.fields[(from+k)%n]; e.problem(f) != "" {
			return f
		}
	}
	return nil
}

// Runnable reports whether form.run would run the form now: it has a
// Submit, is open, and every field is valid.
func (e *RecordEditor) Runnable() bool {
	return e.spec.Submit != nil && !e.closed && e.firstProblem(0) == nil
}

// nextMissing focuses the field that still needs a value: the focused one
// when it is not valid, else the next that is not (wrapping). A field with
// choices opens its chooser; a text field keeps typing when insert is set.
// With every field valid it does nothing.
func (e *RecordEditor) nextMissing(insert bool) error {
	i := max(0, slices.Index(e.fields, e.fieldOn(e.r.Focus)))
	f := e.firstProblem(i)
	if f == nil {
		return nil
	}
	if e.r.Focus != f.id {
		if err := e.r.SetFocus(f.id); err != nil {
			return err
		}
	}
	switch {
	case f.choose || f.suggest:
		if e.r.Mode.Name == "insert" && f.input == nil {
			if err := e.r.SetMode("normal"); err != nil {
				return err
			}
		}
		return e.suggest(f)
	case insert && f.input != nil:
		return e.r.SetMode("insert")
	case e.r.Mode.Name == "insert" && f.input == nil:
		return e.r.SetMode("normal")
	}
	return nil
}

// run hands Submit every value as shown, once each is valid. A modal
// editor closes first, so the request acts where the form was opened; a
// failure reopens it with the values and says why.
func (e *RecordEditor) run() error {
	if e.spec.Submit == nil {
		return errors.New("this form has nothing to run")
	}
	if f := e.firstProblem(0); f != nil {
		return fmt.Errorf("%s: %s", f.label(), e.problem(f))
	}
	values := e.Values()
	focused := ""
	if f := e.fieldOn(e.r.Focus); f != nil {
		focused = f.Name
	}
	e.ran = true
	if e.modal {
		e.closed = true
		if e.r.TopModal() == e.id {
			if err := e.r.Pop(); err != nil {
				return err
			}
		}
		if e.r.Instances[e.id] != nil {
			if err := e.r.RemoveInstance(e.id); err != nil {
				return err
			}
		}
	}
	res := e.spec.Submit(values)
	if res.err != nil {
		e.ran = false
		return e.runFailed(values, focused, res.err)
	}
	if res.work != nil {
		c := Ctx{actor: e.r.Actor(), Runtime: e.r, Context: e.r.contextOf("app"), Instance: e.r.Instances["app"]}
		if _, err := e.r.Launch("app", formRunVerb, res, c); err != nil {
			return err
		}
	}
	if e.closed && e.spec.OnLeave != nil {
		e.spec.OnLeave()
	}
	return nil
}

// runFailed shows why Submit failed: a modal editor opens again over the
// focus with the values it ran with, on the field that was focused.
func (e *RecordEditor) runFailed(values Record, focused string, err error) error {
	if !e.modal {
		e.writeErr = err.Error()
		return err
	}
	spec := e.spec
	spec.Load = func() (Record, error) { return values, nil }
	spec.Start = focused
	again, openErr := e.r.OpenRecordEditor(e.id, spec)
	if openErr != nil {
		return errors.Join(err, openErr)
	}
	again.writeErr = err.Error()
	return err
}

// leave writes what is pending and closes the editor.
func (e *RecordEditor) leave() error {
	err := e.seal(e.fieldOn(e.r.Focus))
	e.closed = true
	e.stopWatch()
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
	focusLine := 0
	for _, f := range e.fields {
		focused := e.fieldOn(e.r.Focus) == f
		if focused {
			focusLine = len(lines)
		}
		label := f.label()
		if f.Invertible {
			label += " · " + invertWord(f.not)
		}
		style := s.Muted
		if focused {
			style = s.Accent
		}
		if problem := e.problem(f); problem != "" {
			// A form that runs once writes nothing: the label says what
			// the value still needs.
			if e.spec.Submit != nil {
				label += " · " + problem
			} else {
				label += " · not written: " + problem
			}
			style = s.Error
		}
		if f.Free && e.staleChoice(f) {
			label += " · outside current choices"
		}
		lines = append(lines, style.Render(ansi.Truncate(label, width, "…")))
		inner := max(1, width-2)
		if f.chips {
			chipLines := e.chipBlock(f, width, 4)
			if focused {
				li, _ := f.chipAt()
				focusLine = len(lines) + li
			}
			if len(chipLines) > 0 {
				chipLines[0] = e.r.PlacedAt(f.id, chipLines[0], 0, 1)
			}
			lines = append(lines, chipLines...)
			continue
		}
		if f.input != nil {
			h := 1
			if f.Multiline {
				h = min(max(3, strings.Count(string(f.input.Text), "\n")+1), max(3, height/3))
			}
			block := InputBlock(f.input, inner, h)
			if block.Cursor != nil && e.r.Focus == f.id {
				cursor = &Pos{X: block.Cursor.X + 2, Y: len(lines) + block.Cursor.Y}
			}
			if f.Placeholder != "" && len(f.input.Text) == 0 {
				block.Text = s.Muted.Render(ansi.Truncate(f.Placeholder, inner, "…"))
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
		prefix := "! not saved: "
		if e.spec.Submit != nil {
			prefix = "! not run: "
		}
		lines = append(lines, s.Error.Render(ansi.Truncate(prefix+e.writeErr, width, "…")))
	}
	// A form taller than its room scrolls to keep the focused field's
	// line (a chips field's cursor line) in view.
	if len(lines) > height {
		if cursor != nil {
			focusLine = cursor.Y
		}
		top := min(max(0, focusLine-height/2), len(lines)-height)
		lines = lines[top : top+height]
		if cursor != nil {
			cursor.Y -= top
		}
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

// DebounceTick is the pause timer in flight, for a host that delivers it
// at once (explore): false when no change waits on one.
func (e *RecordEditor) DebounceTick() (RecordDebounceMsg, bool) {
	msg := RecordDebounceMsg{Editor: e.id, Seq: e.seq}
	return msg, e.DebounceCurrent(msg)
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
		e.refreshChoices()
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

// Close writes what is pending (the editor is being removed). It is the
// delivery contract's Close: to close an editor, an application calls
// Leave, which also removes it (a closed editor's id is free again either
// way).
func (e *RecordEditor) Close() {
	e.stopWatch()
	if !e.closed {
		e.closed = true
		_ = e.flush()
	}
}

// Leave writes what is pending and closes the editor, as form.leave does:
// a modal editor's modal closes, the editor's instance is removed (the
// focus goes where FocusOnClose says) and OnLeave runs.
func (e *RecordEditor) Leave() error {
	if e.closed {
		if e.r.Instances[e.id] != nil && e.r.Instances[e.id].Component == e {
			return e.r.RemoveInstance(e.id)
		}
		return nil
	}
	return e.leave()
}

func (e *RecordEditor) stopWatch() {
	if e.watch != nil {
		e.watch.Stop()
		e.watch = nil
	}
}

// Reload loads the record again (Spec.Load) and takes in what changed
// outside the editor; what it does then follows forms.outside-write, as a
// RecordSpec.Watch event does.
func (e *RecordEditor) Reload() error { return e.outsideWrite() }

// outsideWrite loads the record again after a change outside the editor.
// A field whose loaded value differs from the one the editor wrote takes
// it: shown and written, its undo steps dropped (they no longer start
// from what is stored). A field with unsaved typing keeps the typing, which
// is written over the new value at the next pause, as the later change;
// a field this editor is still writing (queued or in flight) keeps its
// value. With forms.outside-write close the editor then leaves, writing
// only what was typed. The focus, the field and the mode are kept.
func (e *RecordEditor) outsideWrite() error {
	if e.closed || e.spec.Load == nil || e.loading {
		return nil
	}
	e.loading = true
	defer func() { e.loading = false }()
	loaded, err := e.spec.Load()
	if err != nil {
		return fmt.Errorf("load record: %w", err)
	}
	changed := false
	for _, f := range e.fields {
		v, ok := loaded[f.Name]
		if !ok || v == e.written[f.Name] || slices.Contains(e.dirty, f.Name) || slices.Contains(e.sending, f.Name) {
			continue
		}
		changed = true
		typed := f.text() != e.written[f.Name]
		e.written[f.Name], f.before = v, v
		if !typed {
			if err := e.set(f, v); err != nil {
				return err
			}
		}
		drop := func(s recordStep) bool { return s.field == f.Name }
		e.undo = slices.DeleteFunc(e.undo, drop)
		e.redo = slices.DeleteFunc(e.redo, drop)
	}
	if !changed {
		return nil
	}
	e.refreshChoices()
	e.seen = e.snapshot()
	if v, _ := e.r.settingValue(FormsOutsideWriteSetting); v == "close" {
		return e.leave()
	}
	return nil
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
		Verb("suggest", on(func(e *RecordEditor, c Context, _ Arguments) error { return e.suggest(e.fieldOn(focusOf(c))) }), "Fill the field from its choices, typing to filter; a free field takes what is typed too").
		Verb("run", on(func(e *RecordEditor, _ Context, _ Arguments) error { return e.run() }), "Run the form once every field is valid: the values go to its action and the form closes").When(FormRunnableGuard).
		Verb("next-missing", on(func(e *RecordEditor, _ Context, a Arguments) error { return e.nextMissing(boolArg(a, "insert")) }), "Focus the field that still needs a value (this one, else the next); a field with choices opens its chooser").When(FormMissingGuard)
	r.DefineGuard(FormMissingGuard, func(r *Runtime, ev KeyEvent) bool {
		e := r.recordEditorOf(ev.Focus)
		return e != nil && e.spec.Submit != nil && !e.closed && e.firstProblem(0) != nil
	})
	r.DefineGuard(FormRunnableGuard, func(r *Runtime, ev KeyEvent) bool {
		e := r.recordEditorOf(ev.Focus)
		return e != nil && e.Runnable()
	})
	for _, verb := range []string{"next", "previous", "next-missing"} {
		op := r.Operations[FormNoun+"."+verb]
		op.Parameters = insert
		r.Operations[FormNoun+"."+verb] = op
	}
	r.Define(FormChoiceNoun).Focusable()
	registerFormChips(r)
	r.Define(FormOptionNoun).
		Verb("pick", func(r *Runtime, c Context, _ Arguments) (any, error) {
			inst := r.Instances[focusOf(c)]
			chooser, _ := r.Instances[formChooseID].Component.(*recordChooser)
			if inst == nil || chooser == nil {
				return nil, errors.New("no open chooser")
			}
			v, _ := inst.Data.(string)
			return v, chooser.editor.pick(chooser.field, v)
		}, "Set the field to this choice; written at once")
}

func boolArg(a Arguments, name string) bool {
	v, _ := a[name].(bool)
	return v
}

// DependentChoicesSetting enables refreshing and marking dependent choices as
// sibling fields change. Values are always retained; Free values remain writable.
const DependentChoicesSetting = "forms.dependent-choices"

func (e *RecordEditor) arguments() Arguments {
	args := Arguments{}
	for _, f := range e.fields {
		if f.text() == "" {
			continue
		}
		typed, err := argumentsFrom(Operation{Parameters: map[string]Parameter{f.Name: f.Parameter}}, nil, Record{f.Name: f.text()})
		if err == nil {
			maps.Copy(args, typed)
		}
	}
	return args
}

func (e *RecordEditor) staleChoice(f *recordField) bool {
	return e.r.helpFlag(DependentChoicesSetting, true) && len(f.SourceArgs) > 0 && f.choicesReady && f.text() != "" && !hasChoice(f.choices, f.text())
}

func (e *RecordEditor) refreshChoices() {
	if !e.r.helpFlag(DependentChoicesSetting, true) {
		return
	}
	args := e.arguments()
	for _, f := range e.fields {
		if len(f.SourceArgs) == 0 {
			continue
		}
		op := e.r.Operations[e.r.ChoicesSource(f.Parameter)]
		query := ""
		if e.r.ChoicesTakeQuery(f.Parameter) {
			query = f.text()
		}
		raw, _ := json.Marshal(choicesSourceArgs(op, f.Parameter, query, args))
		key := string(raw)
		if f.choiceKey == key {
			continue
		}
		f.choiceKey = key
		if f.chooser != nil && e.r.Chooser(FormSuggestionsKind) == f.chooser {
			f.chooser.SetRows(nil)
			f.chooser.load()
		}
		f.choiceSeq++
		seq := f.choiceSeq
		f.choicesReady, f.choiceLoading = false, true
		e.r.loadChoicesFor(e.r.contextOf(f.id), f.Parameter, query, e.id, func(choices []Choice, err error) {
			if e.closed || seq != f.choiceSeq {
				return
			}
			f.choices, f.choicesReady, f.choiceLoading = choices, err == nil, false
			if f.choose && !e.typing && f.text() != e.written[f.Name] {
				e.fail(e.seal(f))
			}
		}, args)
	}
}
