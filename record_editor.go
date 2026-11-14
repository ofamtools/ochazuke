package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
// through a chooser, never as free text. Noun references always use the
// searchable chooser, even with Free: typing filters, choosing writes an id,
// and an optional reference offers an explicit "(none)" to clear it.
// A Free field with Enum or Source choices is a text field with suggestions (form-suggest):
// typing stays free text, enter in normal mode or ctrl+space opens the
// framework chooser over the suggestions (the Enum, then the Source's
// result, loaded off the loop when the Source verb is Pending), a pick
// fills the field and is written at once, and a value outside the
// suggestions is written as typed; it is one line high (Multiline is
// ignored). The rest are text inputs, Multiline ones several lines high.
type RecordField struct {
	Name  string `json:"name"`
	Label string `json:"label,omitempty"` // shown above the value (default Name)
	// Hidden keeps backing state in the record without drawing a field or focus stop.
	Hidden bool `json:"hidden,omitempty"`
	// ReadOnly draws the field muted, label and value, with no focus stop:
	// it is never edited or written (a prop set cannot write, shown beside
	// the ones it can; forms.show-read-only). Its Parameter is not checked.
	ReadOnly bool `json:"readOnly,omitempty"`
	// KeepOnClear preserves this field when form.clear empties the record.
	KeepOnClear bool `json:"keepOnClear,omitempty"`
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
// changes, in schema order. Walk says the write is form.undo's (-1) or
// form.redo's (+1) of its field, 0 for an edit, so a Save that writes
// several things can put each back to its own value rather than the
// form's (the standard edit over a selection does).
type RecordWrite struct {
	Values  Record
	Changed []string
	Walk    int
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
	// State holds backing values used by composed controls, outside the field
	// schema. They use the same validation, write, clear and undo behavior;
	// they have no field layout or focus stops unless the composer supplies one.
	State []RecordField
	// Render optionally composes the editor with other controls. Block remains
	// the standard field layout, usable within this callback.
	Render func(Styles, int, int) Block
	// RowView is the backing list of a composite record view. Shared list verbs
	// resolve it from descendants, including embedded choosers.
	RowView *Collection
	// Immediate writes valid typing on every event, for inexpensive view state.
	// Otherwise text retains the normal debounce behavior.
	Immediate bool
	// OnClear resets composed controls after form.clear empties the record.
	OnClear func() error
	// Load reads the record when the editor opens (nil: every field empty).
	Load func() (Record, error)
	// Reload reads outside changes instead of Load when set. It returns a
	// Record as Done or Pending; pending reads never block editing and are
	// cancelled when the editor closes.
	Reload func() Result
	// Save writes one change: Done, Failed, or Pending work that runs off
	// the loop. At most one write is in flight; changes made meanwhile go
	// in the next write. A failed write keeps the values on screen, says
	// so, and is retried with the next change.
	// Done(ticket) follows an already-started Execute job instead of marking
	// it complete. Queued saves survive closing the form and drain on quit.
	Save func(RecordWrite) Result
	// FieldWrites sends Save one changed field at a time. A failed value stays
	// marked on that field without preventing other fields from being saved.
	FieldWrites bool
	// WriteKey identifies the stored thing across editor lifetimes. Nonempty
	// keys serialize writes from every editor of that thing; unrelated things
	// never wait for each other.
	WriteKey string
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
	// WatchArgs narrows the existing watch, for example to one subject key.
	WatchArgs Arguments
	// OnOpen is called with the editor once it is built, so a spec built away
	// from its editor (Runtime.EditSpec) can reach it from its Save and Submit.
	OnOpen func(*RecordEditor)
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
	// FormReadOnlyNoun is a read-only field's instance noun (no focus stop).
	FormReadOnlyNoun = "ochazuke.form-read-only"
	formChooseID     = "form-choose"
	formChooseList   = "__form-choose/list"
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
	// FormOneFieldGuard holds in a form of one shown field that writes
	// each change: enter closes it (bindings/form.json).
	FormOneFieldGuard = "form-one-field"
)

// shownFields counts the fields the form draws with a focus stop (not
// Hidden or ReadOnly).
func (e *RecordEditor) shownFields() int {
	n := 0
	for _, f := range e.fields {
		if !f.skipped() {
			n++
		}
	}
	return n
}

// editKind is how a form edits a field: through a chooser (choose), as
// free text with suggestions (suggest), as chips (chips), and whether its
// chooser is the path chooser (path). Neither choose nor suggest nor chips
// is a plain text input.
type editKind struct {
	choose  bool // a choice field: a stop whose enter opens its chooser
	suggest bool // a free text field with suggestions
	path    bool // a path field: its chooser is the path chooser
	chips   bool // a choice field that takes several choices (Chips)
	list    bool // a list with Items: rows of items (record_list.go)
}

// editKindOf is the one test of how a field is edited, read by the form
// and by edit {field} (which opens a choice field's chooser in place of
// the form, edit.field-picker): a boolean, a noun reference, or a field
// with choices that is not Free is a choice field; a path field follows
// path.field.
func (r *Runtime) editKindOf(field RecordField) editKind {
	var k editKind
	if field.ReadOnly {
		return k
	}
	if isList(field) {
		k.list = true
		return k
	}
	k.chips = field.Chips && (paramHasChoices(field.Parameter) || field.Type == "boolean")
	k.choose = !k.chips && (field.Type == "boolean" || field.Noun != "" || (paramHasChoices(field.Parameter) && !field.TakesAny()))
	k.suggest = !k.chips && !k.choose && paramHasChoices(field.Parameter)
	if field.Type == PathParameterType {
		// A path field is a stop that opens the path chooser, or a
		// text field whose suggestions are the path chooser
		// (path.field).
		k.path = true
		v, _ := r.settingValue(PathFieldSetting)
		k.choose, k.suggest = v != "text", v == "text"
	}
	return k
}

// paramHasChoices reports whether a parameter's values come from a list:
// an Enum, a Source, or a Noun's things (Runtime.Choices).
func paramHasChoices(p Parameter) bool {
	return len(p.Enum) > 0 || p.Source != "" || p.Noun != ""
}

// recordStep is one undo step: a field's written value before and after.
type recordStep struct {
	field    string
	from, to string
	changes  []recordStep
}

// opened tells the spec's OnOpen, if it has one, that the editor is built.
func (e *RecordEditor) opened() {
	if e.spec.OnOpen != nil {
		e.spec.OnOpen(e)
	}
}

// valueNoun is the instance noun of a field that holds its value as text
// and is not an input or a choice (read-only, list, chips), "" for the
// rest.
func (f *recordField) valueNoun() string {
	switch {
	case f.ReadOnly:
		return FormReadOnlyNoun
	case f.list:
		return FormItemNoun
	case f.chips:
		return FormChipsNoun
	}
	return ""
}

// skipped reports whether the field has no focus stop: Hidden (not drawn)
// or ReadOnly (drawn muted).
func (f *recordField) skipped() bool { return f.Hidden || f.ReadOnly }

type recordField struct {
	RecordField
	id            string
	input         *Input // nil for a choice field
	value         string // a choice field's value
	chipOrder     string // the palette.choice-label order the chips were last laid out in
	before        string // the written value when the current edit began
	chooser       *Chooser
	choiceKey     string
	choiceSeq     uint64
	choicesReady  bool
	choiceLoading bool
	choices       []Choice
	editKind
	chip int // the chip under the cursor, an index into choices
	// item is a list field's cursor: an item's index, len(items) the add row.
	item int
	// lines are the chips' rows as last drawn (indexes into choices), so
	// j and k move by what is on screen.
	lines                 [][]int
	not                   bool // an Invertible field's state as shown
	writeErr, failedValue string
	// walk is the form.undo (-1) or form.redo (+1) that queued the field's
	// pending write, 0 for an edit (RecordWrite.Walk).
	walk int
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

func (f *recordField) choiceLabel(c Choice) string { return f.RecordField.choiceLabel(c) }

func (f *recordField) choiceRow(c Choice) ChooserRow { return f.RecordField.choiceRow(c) }

// choiceLabel is how a field shows one of its choices in a plain list
// (Choice.Entry in the order palette.choice-label says; label-first without
// a runtime).
func (f RecordField) choiceLabel(c Choice) string { return c.Entry(ChoiceLabelFirst).Text }

// choiceRow is a choice as a picker row: Choice.Row for every field.
func (f RecordField) choiceRow(c Choice) ChooserRow { return c.Row() }

// heldChoice reports whether a picker row is the thing a field holds: its
// id, or the id after the noun prefix a list row's id carries
// ("question/q-1" holds q-1).
func heldChoice(value, id string) bool {
	return value != "" && (id == value || strings.HasSuffix(id, "/"+value))
}

// RecordEditor is a form over one record: a field per schema entry, each a
// focus stop, and every change written as it is made. Typing in a text
// field is written after a pause (Debounce) and at once when the field is
// left or insert mode ends; a choice, an edit in normal mode, an undo or a
// redo is written at once. A value that is not valid (Required, the type,
// the choices, Validate) is not written, and its label says why. u and
// ctrl+r walk one chain from any stop: a text input's own history first,
// then the record's written changes, each written like any other; past the
// end they do nothing. q and esc leave, and nothing is ever discarded.
// There is no save key.
//
// The editor is an instance of the form noun (id) with a child per field
// (id/name): text fields are inputs of form-field (form-text when
// multiline), choice fields instances of form-choice, whose enter opens the
// chooser (a modal list of form-option rows).
type RecordEditor struct {
	r  *Runtime
	id string
	// over is the modal the editor opened over ("" for none): a failed run
	// reopens it over the same level, whatever is on top of the focus by
	// then (runFailed).
	over   string
	spec   RecordSpec
	fields []*recordField
	// Styles colour the labels (the zero value draws them plain); a modal
	// editor uses the Model's.
	Styles Styles

	out         recordOutbox // what this editor still owes the store
	watch       *Watch       // the Watch noun's subscription while open
	undo        []recordStep
	redo        []recordStep
	seq         uint64
	seen        string
	focus       string // the focus at the last PendingCommand
	typing      bool   // whether a text field was being typed in then
	modal       bool
	closed      bool
	loading     bool // outsideWrite is loading the record
	reloadAgain bool
	walking     int  // history's undo (-1) or redo (+1) while it flushes
	ran         bool // form.run handed the values to Submit
	failure     error
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
	e.over = r.TopModal()
	if err := r.Push(id, e.fields[e.start()].id); err != nil {
		_ = r.RemoveInstance(id)
		return nil, err
	}
	// esc and q (modal.close) leave the form: the field is sealed and
	// OnLeave runs, as form.leave does for an editor placed on a page.
	if err := r.SetModalClose(id, Request{Noun: FormNoun, Verb: "leave"}); err != nil {
		return e, err
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
	fields := slices.Clone(spec.State)
	for i := range fields {
		fields[i].Hidden = true
	}
	fields = append(fields, spec.Fields...)
	if len(fields) == 0 {
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
	for _, f := range fields {
		if f.Name == "" || names[f.Name] || strings.ContainsAny(f.Name, "/"+RecordInvertSuffix) {
			return nil, fmt.Errorf("record field name %q must be nonempty, unique and without / or %s", f.Name, RecordInvertSuffix)
		}
		names[f.Name] = true
	}
	params := map[string]Parameter{}
	for _, f := range fields {
		params[f.Name] = f.Parameter
	}
	if err := r.CheckSourceArgs(params); err != nil {
		return nil, err
	}
	e := &RecordEditor{r: r, id: id, spec: spec, out: newRecordOutbox(values, parent)}
	r.Instance(id, Parent(parent), Kind(FormNoun), FocusOnClose(closeFocus)).Component = e
	for _, field := range fields {
		f := &recordField{RecordField: field, id: id + "/" + field.Name}
		f.editKind = r.editKindOf(field)
		v := values[field.Name]
		e.out.set(field.Name, v)
		f.before = v
		if field.Invertible {
			inv := values[field.Name+RecordInvertSuffix]
			f.not = inv == "true"
			e.out.set(field.Name+RecordInvertSuffix, invertText(f.not))
		}
		switch noun := f.valueNoun(); {
		case noun != "":
			f.value = v
			r.Instance(f.id, Parent(id), Kind(noun))
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
		r.Instances[f.id].Unfocusable = f.skipped()
		if (f.choose || f.chips) && len(f.SourceArgs) == 0 {
			e.loadChoices(f)
		}
	}
	e.refreshChoices()
	e.seen = e.snapshot()
	e.focus = r.Focus
	if spec.Watch != "" {
		w, err := r.Watch(spec.Watch, spec.WatchArgs, func(WatchEvent) { e.fail(e.outsideWrite()) })
		if err != nil {
			_ = r.RemoveInstance(id)
			return nil, fmt.Errorf("record editor watch: %w", err)
		}
		e.watch = w
	}
	e.opened()
	return e, nil
}

// RecordFields is a form's schema from a verb's parameters, in the order
// bare CLI words fill them (Operation.ParameterOrder): a mounted tool's
// edit verb gets a form from its catalog alone.
func RecordFields(op Operation) []RecordField {
	var out []RecordField
	for _, name := range op.ParameterOrder() {
		p := op.Parameters[name]
		out = append(out, RecordField{Name: name, Parameter: p, Multiline: jsonField(p.Type) && p.Items == nil})
	}
	return out
}

// RecordEditor is the record editor with this id, or nil.
func (r *Runtime) RecordEditor(id string) *RecordEditor { return componentAt[*RecordEditor](r, id) }

// recordEditorOf is the editor on a focus's ancestry, or nil.
func (r *Runtime) recordEditorOf(focus string) *RecordEditor {
	return nearestComponent[*RecordEditor](r, focus)
}

// ID is the editor's instance id.
func (e *RecordEditor) ID() string { return e.id }

// Closed reports whether the editor was left (form.leave) or removed.
func (e *RecordEditor) Closed() bool { return e.closed }

// Values is every editable field's value as shown, valid or not (a
// ReadOnly field is drawn, not a value of the form).
func (e *RecordEditor) Values() Record {
	out := Record{}
	for _, f := range e.fields {
		if f.ReadOnly {
			continue
		}
		out[f.Name] = f.text()
	}
	return out
}

// Written is the record as written (or queued to be): the last valid value
// of every field.
func (e *RecordEditor) Written() Record { return e.out.record() }

// Problem says why a field's value is not written, "" when it is.
func (e *RecordEditor) Problem(name string) string {
	if f := e.field(name); f != nil {
		return e.problem(f)
	}
	return ""
}

// WriteError is the last write's failure, "" once a write succeeds.
func (e *RecordEditor) WriteError() string { return e.out.err }

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
		if f.Name == e.spec.Start && !f.skipped() {
			return i
		}
	}
	for i, f := range e.fields {
		if !f.skipped() {
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
	if e.r.modes[e.r.Mode.Name].Typing {
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
		f.choices = booleanChoices()
		return
	}
	if !paramHasChoices(f.Parameter) {
		return
	}
	if f.choicesReady || f.choiceLoading {
		return
	}
	f.choiceLoading = true
	e.r.loadChoices(e.r.contextOf(f.id), f.Parameter, e.id, func(choices []Choice, err error) {
		f.choices, f.choicesReady, f.choiceLoading = choices, err == nil, false
	}, e.arguments())
}

// problem says why a value is not written: required and empty, not of the
// type, not one of the choices, or Validate's error.
func (e *RecordEditor) problem(f *recordField) string {
	if f.ReadOnly {
		return ""
	}
	v := f.text()
	if f.writeErr != "" && v == f.failedValue {
		return f.writeErr
	}
	if strings.TrimSpace(v) == "" {
		if f.Required {
			return "required"
		}
		return ""
	}
	if f.list {
		return e.listProblem(f, v)
	}
	if f.chips {
		return e.chipsProblem(f, v)
	}
	if k := kindOf(f.Type); k.parse != nil {
		text := v
		if k.trimmed {
			text = strings.TrimSpace(v)
		}
		if _, ok := k.parse(text); !ok {
			return k.phrase
		}
	}
	if !f.TakesAny() && f.choiceLoading {
		return "loading choices"
	}
	if !f.TakesAny() && paramHasChoices(f.Parameter) && (len(f.SourceArgs) == 0 && len(f.choices) > 0 || len(f.SourceArgs) > 0 && f.choicesReady) && !hasChoice(f.choices, v) {
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
			if inv := invertText(f.not); inv != e.out.value(f.Name+RecordInvertSuffix) {
				e.out.set(f.Name+RecordInvertSuffix, inv)
				e.out.queue(f.Name)
			}
		}
		v := f.text()
		if v != f.failedValue {
			f.writeErr = ""
		}
		if v == e.out.value(f.Name) || e.problem(f) != "" {
			continue
		}
		e.out.set(f.Name, v)
		f.walk = e.walking
		e.out.queue(f.Name)
	}
	return e.send()
}

// send hands Save the record when fields are dirty and no write is in
// flight; a pending write's completion sends what changed meanwhile.
func (e *RecordEditor) send() error {
	if e.spec.Submit != nil && (!e.ran || e.spec.WriteKey == "") {
		return nil
	}
	if e.out.inflight {
		return nil
	}
	if e.out.idle() {
		e.releaseWrite()
		return nil
	}
	if !e.claimWrite() {
		return nil
	}
	changed := e.out.next(e.fieldNames(), e.spec.FieldWrites, func(name string) bool {
		f := e.field(name)
		return f.writeErr == "" || f.text() != f.failedValue
	})
	if len(changed) == 0 {
		e.releaseWrite()
		return nil
	}
	e.out.take(changed)
	if e.spec.Save == nil && e.spec.Submit == nil {
		e.releaseWrite()
		return nil
	}
	e.out.stage(changed)
	w := e.recordWrite(changed)
	var res Result
	if e.spec.Submit != nil {
		res = e.spec.Submit(w.Values)
	} else {
		res = e.spec.Save(w)
	}
	e.out.settle()
	if res.err != nil {
		e.saveFailed(w, res.err)
		err := fmt.Errorf("write %s: %w", strings.Join(changed, ", "), res.err)
		if e.spec.FieldWrites {
			return errors.Join(err, e.send())
		}
		e.releaseWrite()
		return err
	}
	return e.dispatch(w, res)
}

// dispatch follows the result of a write that did not fail at once: a ticket
// for an existing application operation, work to launch, or nothing, which
// ends the write.
func (e *RecordEditor) dispatch(w RecordWrite, res Result) error {
	// Execute returns a ticket when Save dispatches an existing application
	// operation. Observe that job instead of treating its queued write as done.
	if ticket, ok := res.value.(Ticket); ok && e.r.Accepts(ticket) {
		j := e.r.jobs[ticket.Job]
		e.out.launch(w.Changed)
		e.markFlush(j, w)
		j.alsoThen(func(c Ctx, value any, err error) error {
			e.saved(w, err)
			return err
		})
		return nil
	}
	if res.work == nil {
		e.out.err = ""
		if e.spec.FieldWrites {
			return e.send()
		}
		e.releaseWrite()
		return nil
	}
	owner := e.out.owner
	if e.r.Instances[owner] == nil {
		owner = "app"
	}
	res = res.also(func(c Ctx, value any, err error) { e.saved(w, err) })
	e.out.launch(w.Changed)
	c := Ctx{actor: e.r.Actor(), Runtime: e.r, Context: e.r.contextOf(owner), Instance: e.r.Instances[owner]}
	ticket, err := e.r.Launch(owner, formWriteVerb, res, c)
	if err != nil {
		e.out.settle()
		e.saveFailed(w, err)
		e.releaseWrite()
		return err
	}
	e.markFlush(e.r.jobs[ticket.Job], w)
	return nil
}

// fieldNames are the form's field names in order.
func (e *RecordEditor) fieldNames() []string {
	names := make([]string, len(e.fields))
	for i, f := range e.fields {
		names[i] = f.Name
	}
	return names
}

// recordWrite is the write of changed: the record as written and, for a
// field write, the form.undo or form.redo that queued it (Walk).
func (e *RecordEditor) recordWrite(changed []string) RecordWrite {
	w := RecordWrite{Values: e.out.record(), Changed: slices.Clone(changed)}
	if e.spec.FieldWrites {
		f := e.field(changed[0])
		w.Walk, f.walk = f.walk, 0
	}
	return w
}

func (e *RecordEditor) saveFailed(w RecordWrite, err error) {
	e.out.abandon(err.Error(), w.Changed)
	if e.spec.FieldWrites {
		for _, name := range w.Changed {
			f := e.field(name)
			f.writeErr, f.failedValue = err.Error(), w.Values[name]
		}
	}
	if e.closed {
		e.reportUnsaved(w.Changed, w.Values, err.Error())
	}
}

// notWritten marks names not written for err, as a failed field write
// does, while the write as a whole stood (a selection whose focused row
// refused): the form shows no "not saved" line and does not retry.
func (e *RecordEditor) notWritten(names []string, err error) {
	for _, name := range names {
		if f := e.field(name); f != nil {
			f.writeErr, f.failedValue = err.Error(), e.out.value(name)
		}
	}
}

// reportUnsaved preserves values after their field and its inline error are gone.
// Secrets stay redacted in notices and in the message history.
func (e *RecordEditor) reportUnsaved(names []string, values Record, reason string) []string {
	var messages []string
	for _, name := range names {
		value := values[name]
		if f := e.field(name); f != nil && f.Secret {
			value = "[redacted]"
		}
		text := fmt.Sprintf("Not saved: %s · %s = %q: %s", e.spec.Title, name, value, reason)
		e.r.UI().Notify(text)
		messages = append(messages, text)
	}
	return messages
}

func (e *RecordEditor) saved(w RecordWrite, err error) {
	e.out.settle()
	if err != nil {
		e.saveFailed(w, err)
		if !e.spec.FieldWrites {
			e.releaseWrite()
			return
		}
	} else {
		e.out.err = ""
	}
	if nextErr := e.send(); nextErr != nil {
		e.fail(nextErr)
	}
}

// seal writes the record and makes a field's change since its edit began
// one undo step. A change the input's own undo or redo made is a step along
// the chain, not a new one (walked).
func (e *RecordEditor) seal(f *recordField) error {
	err := e.flush()
	if f == nil {
		return err
	}
	if to := e.out.value(f.Name); to != f.before {
		if f.input != nil && f.input.walk != 0 {
			e.walked(f, to)
		} else {
			e.undo = append(e.undo, recordStep{field: f.Name, from: f.before, to: to})
			e.redo = nil
		}
		f.before = to
	}
	if f.input != nil {
		f.input.walk = 0
	}
	return err
}

// walked keeps the two histories one chain when a text input takes a step of
// its own: the step leaves the side it came from and goes on the other, as
// form.undo and form.redo would move it. The field's latest step is the one
// taken even when another field's is newer, since the input's history comes
// first; a step the record no longer holds (a later change dropped the redo)
// just goes on the other side, so the way back stays.
func (e *RecordEditor) walked(f *recordField, to string) {
	from, other := &e.undo, &e.redo
	if f.input.walk > 0 {
		from, other = &e.redo, &e.undo
	}
	*from, _ = takeStep(*from, f.Name, f.before, to)
	*other = append(*other, recordStep{field: f.Name, from: f.before, to: to})
}

// takeStep takes the move of a field from at to now out of the latest of
// steps that holds the field: a step that goes no further is gone; one that
// goes on (several input states, one record step) now ends at now.
func takeStep(steps []recordStep, name, at, now string) ([]recordStep, bool) {
	for i := len(steps) - 1; i >= 0; i-- {
		s := &steps[i]
		if s.changes != nil {
			changes, found := takeStep(slices.Clone(s.changes), name, at, now)
			if !found {
				continue
			}
			s.changes = changes
			if len(changes) == 0 {
				return slices.Delete(steps, i, i+1), true
			}
			return steps, true
		}
		if s.field != name {
			continue
		}
		if s.to != at {
			return steps, false
		}
		if s.from == now {
			return slices.Delete(steps, i, i+1), true
		}
		s.to = now
		return steps, true
	}
	return steps, false
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

// restore puts a history step's value in a field: a text field walks its
// input's undo tree there, so the input's own u and ctrl+r carry on from
// the same place; a value the tree does not hold is set like any other.
func (e *RecordEditor) restore(f *recordField, v string, redo bool) error {
	if f.input != nil && f.input.walkTo(v, redo) {
		return nil
	}
	return e.set(f, v)
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
	steps := step.changes
	if steps == nil {
		steps = []recordStep{step}
	}
	var inverse recordStep
	if step.changes != nil {
		inverse.changes = make([]recordStep, 0, len(steps))
	}
	var focus *recordField
	for i := len(steps) - 1; i >= 0; i-- {
		change := steps[i]
		name, inverted := strings.CutSuffix(change.field, RecordInvertSuffix)
		f := e.field(name)
		if f == nil {
			continue
		}
		if inverted {
			f.not = change.from == "true"
		} else if serr := e.restore(f, change.from, redo); serr != nil {
			return serr
		}
		back := recordStep{field: change.field, from: change.to, to: change.from}
		if step.changes == nil {
			inverse = back
		} else {
			inverse.changes = append(inverse.changes, back)
		}
		if !f.skipped() {
			focus = f
		}
	}
	e.walking = 1
	if !redo {
		e.walking = -1
	}
	werr := e.flush()
	e.walking = 0
	for _, f := range e.fields {
		f.before = e.out.value(f.Name)
	}
	*to = append(*to, inverse)
	e.seen = e.snapshot()
	if focus != nil && e.r.Focus != focus.id {
		if ferr := e.r.SetFocus(focus.id); ferr != nil && werr == nil {
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
	for e.fields[j].skipped() && j != i {
		next := j + delta
		if next < 0 || next >= len(e.fields) {
			j = i
			break
		}
		j = next
	}
	if j != i {
		if err := e.r.SetFocus(e.fields[j].id); err != nil {
			return err
		}
		e.landChips(e.fields[j], delta)
		e.landItems(e.fields[j], delta)
	}
	if insert && e.fields[j].input != nil {
		return e.r.SetMode("insert")
	}
	if e.r.modes[e.r.Mode.Name].Typing && e.fields[j].input == nil {
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
	if f.Noun != "" || len(f.SourceArgs) > 0 || e.r.ChoicesSource(f.Parameter) != "" {
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
		items = append(items, ListItem{ID: fmt.Sprintf("%s/%d", formChooseList, len(items)), Label: f.choiceLabel(c), Noun: FormOptionNoun, Data: c.ID})
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
	// fillAfter fills the field with v once err (what made v) is nil.
	fillAfter := func(v string, err error) Result {
		if err == nil {
			err = e.fill(f, v)
		}
		if err != nil {
			return Failed(err)
		}
		return Done(v)
	}
	fill := func(v string) Result { return fillAfter(v, nil) }
	spec := ChooserSpec{
		Kind:   FormSuggestionsKind,
		Title:  "Suggest " + f.label(),
		Empty:  "loading…",
		Choose: func(row ChooserRow) Result { return fill(row.ID) },
		// The highlight opens on the value the field holds (a Focus
		// parameter holds the focused thing), not the top of a long list.
		Start: func(row ChooserRow) bool { return heldChoice(f.text(), row.ID) },
	}
	var fixed []ChooserRow
	if f.suggest {
		spec.Create, spec.CreateRow = fill, true
		spec.CreateLabel = func(query string) string { return "use " + strconv.Quote(query) }
	}
	if !f.suggest {
		spec.Title = "Choose " + f.label()
		if !f.Required {
			fixed = append(fixed, noneChoice().Row())
		}
		if e.r.pickerCreates(f.Noun) {
			// A name no thing has makes one (pickers.create-row).
			noun := f.Noun
			spec.CreateRow = true
			spec.CreateLabel = func(query string) string { return "create " + strconv.Quote(query) }
			spec.Create = func(query string) Result { return fillAfter(e.r.createNamed(noun, query)) }
		}
	}
	c, err := e.r.openChoiceChooser(spec, choiceLoad{
		ctx:      e.r.contextOf(f.id),
		param:    f.Parameter,
		label:    f.choiceLabel,
		row:      f.choiceRow,
		siblings: e.arguments,
		fixed:    fixed,
		ask:      e.spec.Submit != nil && f.Required && f.text() == "",
		landed: func(choices []Choice) {
			if len(f.SourceArgs) == 0 {
				f.choices = choices
			}
		},
	})
	if e.r.ChoicesTakeQuery(f.Parameter) || len(f.SourceArgs) > 0 {
		f.chooser = c
	}
	return err
}

// choiceLoad is where a choice chooser's rows come from: a parameter's
// choices (its Enum, then its Source's result, loaded off the loop when the
// source is Pending; true and false for a boolean without choices), read
// in ctx with the siblings a dependent source takes, each labelled by
// label, then the fixed rows ("(none)"). landed gets the choices each load
// produced.
type choiceLoad struct {
	ctx   Context
	param Parameter
	label func(Choice) string
	// row, when set, builds a choice's row (label, tones); else label.
	row      func(Choice) ChooserRow
	siblings func() Arguments
	fixed    []ChooserRow
	landed   func([]Choice)
	// ask enables where-from scopes for a missing request subject and opens
	// them in normal mode. Ordinary record-choice editing is unchanged.
	ask bool
}

// openChoiceChooser opens the framework chooser (spec) over a parameter's
// choices: the form's choice and suggestion fields and edit {field} on a
// choice field (edit.field-picker) open it alike. A source that takes the
// query, or one that depends on siblings, reloads as the chooser's source
// (while typing, and when a sibling changes); otherwise the choices load
// once, as a job of the chooser (closing it cancels the load).
func (r *Runtime) openChoiceChooser(spec ChooserSpec, l choiceLoad) (*Chooser, error) {
	spec, l.param = r.nounPicker(spec, l.param)
	if l.siblings == nil {
		l.siblings = func() Arguments { return nil }
	}
	if scoped, ok := r.subjectScopes(spec, l); ok {
		return r.OpenChooser(scoped)
	}
	if scopes := r.scopesOf(l.param, l.siblings()); len(scopes) > 0 {
		// A picker with scopes opens on the first and S steps to the
		// others, the last holding every possible value (Scope).
		spec.Scoped = true
		for _, sc := range scopes {
			param := sc.within(l.param, l.siblings())
			spec.Sources = append(spec.Sources, ChooserSource{
				Name: sc.Name,
				Live: r.ChoicesTakeQuery(param),
				Load: r.choiceSourceLoad(l, param),
				Count: func(query string) (int, bool) {
					choices, err := r.ChoicesFor(l.ctx, param, query, l.siblings())
					return len(choices), err == nil
				},
			})
		}
		return r.OpenChooser(spec)
	}
	landed := func(choices []Choice) {
		if l.landed != nil {
			l.landed(choices)
		}
	}
	if r.ChoicesTakeQuery(l.param) || len(l.param.SourceArgs) > 0 {
		// Query sources reload while typing; dependent sources also reload
		// when a sibling changes while their chooser is open.
		src := ChooserSource{
			Name: "suggestions",
			Live: r.ChoicesTakeQuery(l.param),
			Load: r.choiceSourceLoad(l, l.param),
		}
		if l.param.Noun == AnyNoun && l.param.Source == "" {
			anyRefNavigation(&src)
		}
		spec.Sources = []ChooserSource{src}
		return r.OpenChooser(spec)
	}
	c, err := r.OpenChooser(spec)
	if err != nil {
		return nil, err
	}
	done := func(choices []Choice, err error) {
		if r.Chooser(spec.Kind) != c {
			return
		}
		landed(choices)
		rows := make([]ChooserRow, len(choices))
		for i, ch := range choices {
			rows[i] = l.choiceRow(ch)
		}
		switch plural := r.choicesPlural(l.param); {
		case err != nil && len(rows) == 0:
			c.spec.Empty = EmptyText(plural) + ": " + err.Error()
		case len(rows) == 0:
			c.spec.Empty = EmptyText(plural)
		}
		c.SetRows(append(rows, l.fixed...))
	}
	if l.param.Type == "boolean" && !paramHasChoices(l.param) {
		done(booleanChoices(), nil)
		return c, nil
	}
	r.loadChoices(l.ctx, l.param, spec.Kind, done, l.siblings())
	return c, nil
}

// choiceSourceLoad is the chooser source Load of a parameter's choices in
// param (l.param, or the parameter within one of its scopes): the choices
// for the query and the siblings, each as a row, then the fixed rows.
func (r *Runtime) choiceSourceLoad(l choiceLoad, param Parameter) func(query string) Result {
	landed := func(choices []Choice) {
		if l.landed != nil {
			l.landed(choices)
		}
	}
	return func(query string) Result {
		res := r.choicesLoad(l.ctx, param, query, l.siblings())
		rows := func(value any) []ChooserRow {
			choices, _ := value.([]Choice)
			out := make([]ChooserRow, 0, len(choices)+len(l.fixed))
			for _, ch := range choices {
				out = append(out, l.choiceRow(ch))
			}
			return append(out, l.fixed...)
		}
		if res.work == nil {
			if res.err != nil {
				return res
			}
			choices, _ := res.value.([]Choice)
			landed(choices)
			return Done(rows(res.value))
		}
		return res.bare().Map(func(value any, err error) (any, error) {
			if err != nil {
				return nil, err
			}
			return rows(value), nil
		}).Then(func(_ Ctx, value any, _ error) {
			// value is the rows; the choices they came from are
			// their ids.
			got, _ := value.([]ChooserRow)
			var choices []Choice
			for _, row := range got {
				if !row.Fixed {
					choices = append(choices, Choice{ID: row.ID, Label: row.Label})
				}
			}
			landed(choices)
		})
	}
}

// choiceRow is a choice as a row of the chooser.
func (l choiceLoad) choiceRow(c Choice) ChooserRow {
	if l.row != nil {
		return l.row(c)
	}
	c.Label, c.Bare = l.label(c), true
	return c.Row()
}

// booleanChoices are a boolean field's choices: true and false.
func booleanChoices() []Choice {
	return []Choice{{ID: "true", Label: "true"}, {ID: "false", Label: "false"}}
}

// choosePath opens the path chooser for a path field; the chosen path
// fills it and is written at once.
func (e *RecordEditor) choosePath(f *recordField) error {
	_, err := e.r.OpenPathChooser(PathChooserSpec{
		Title:     "Choose " + f.label(),
		Pick:      PathPick{Name: f.Name, Parameter: f.Parameter, Value: f.text()},
		whereFrom: e.spec.Submit != nil && f.text() == "",
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
		if e.r.modes[e.r.Mode.Name].Typing && f.input == nil {
			if err := e.r.SetMode("normal"); err != nil {
				return err
			}
		}
		return e.suggest(f)
	case insert && f.input != nil:
		return e.r.SetMode("insert")
	case e.r.modes[e.r.Mode.Name].Typing && f.input == nil:
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
	if e.spec.WriteKey != "" {
		for _, f := range e.fields {
			if values[f.Name] != e.out.value(f.Name) {
				e.out.queue(f.Name)
			}
		}
		e.out.adopt(values)
		e.out.queueAllIfIdle(e.fieldNames())
		if err := e.send(); err != nil {
			e.ran = false
			return e.runFailed(values, focused, err)
		}
		if e.closed && e.spec.OnLeave != nil {
			e.spec.OnLeave()
		}
		return nil
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
		e.out.err = err.Error()
		return err
	}
	spec := e.spec
	spec.Load = func() (Record, error) { return values, nil }
	spec.Start = focused
	if e.r.TopModal() == e.over {
		defer func(before string) { e.r.reopening = before }(e.r.reopening)
		e.r.reopening = e.id
	}
	again, openErr := e.r.OpenRecordEditor(e.id, spec)
	if openErr != nil {
		return errors.Join(err, openErr)
	}
	again.out.err = err.Error()
	return err
}

// leave writes what is pending and closes the editor.
func (e *RecordEditor) leave() error {
	err := e.seal(e.fieldOn(e.r.Focus))
	e.closed = true
	if e.out.err != "" {
		e.reportUnsaved(e.out.queued(), e.out.record(), e.out.err)
	}
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
	// The form draws in its own presentation (DrawnFocus): a picker or
	// prompt opened over it leaves the field it opened from marked.
	drawn := e.r.DrawnFocus(e.id)
	var cursor *Pos
	focusLine := 0
	for _, f := range e.fields {
		if f.Hidden {
			continue
		}
		if f.ReadOnly {
			lines = append(lines, e.readOnlyLines(f, width)...)
			continue
		}
		focused := e.fieldOn(drawn) == f
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
		if f.TakesAny() && e.staleChoice(f) {
			label += " · outside current choices"
		}
		lines = append(lines, e.fieldStyle(f, style).Render(ansi.Truncate(label, width, "…")))
		inner := max(1, width-2)
		if rows, at, ok := e.valueRows(f, width); ok {
			if focused {
				focusLine = len(lines) + at
			}
			lines = append(lines, rows...)
			continue
		}
		if f.input != nil {
			h := 1
			if f.input.Multiline {
				h = min(max(3, strings.Count(string(f.input.Text), "\n")+1), max(3, height/3))
			}
			block := InputBlock(f.input, inner, h)
			if block.Cursor != nil && drawn == f.id {
				cursor = &Pos{X: block.Cursor.X + 2, Y: len(lines) + block.Cursor.Y}
			}
			if f.Placeholder != "" && len(f.input.Text) == 0 {
				block.Text = e.fieldStyle(f, s.Muted).Render(ansi.Truncate(f.Placeholder, inner, "…"))
			}
			for _, row := range strings.Split(block.Text, "\n") {
				lines = append(lines, "  "+e.fieldStyle(f, lipgloss.NewStyle()).Render(row))
			}
			continue
		}
		value := "(none)"
		for _, c := range f.choices {
			if c.ID == f.value {
				value = c.Label
			}
		}
		if f.value != "" && value == "(none)" {
			value = f.value
		}
		value = ansi.Truncate(value+" "+e.Styles.Glyph("fold.open"), inner, "…")
		value = e.fieldStyle(f, lipgloss.NewStyle()).Render(value)
		if focused && drawn == f.id {
			value = reverseVideo(value)
		}
		lines = append(lines, e.r.PlacedAt(f.id, "  "+value, 0, 1))
	}
	if e.out.err != "" {
		prefix := "! not saved: "
		if e.spec.Submit != nil {
			prefix = "! not run: "
		}
		lines = append(lines, s.Error.Render(ansi.Truncate(prefix+e.out.err, width, "…")))
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

// valueRows draws the rows under the label of a list or chips field and the
// row the cursor is on (ok false for any other field). The first row is
// placed so a click or a jump finds the field.
func (e *RecordEditor) valueRows(f *recordField, width int) (rows []string, at int, ok bool) {
	switch {
	case f.list:
		rows, at = e.listBlock(f, width, 4), f.itemCursor()
	case f.chips:
		rows = e.chipBlock(f, width, 4)
		at, _ = f.chipAt()
	default:
		return nil, 0, false
	}
	if len(rows) > 0 {
		rows[0] = e.r.PlacedAt(f.id, rows[0], 0, 1)
	}
	return rows, at, true
}

// readOnlyLines draws a read-only field muted: its label, then its value
// on one line (newlines shown as spaces), "(none)" when empty.
func (e *RecordEditor) readOnlyLines(f *recordField, width int) []string {
	value := strings.Join(strings.Fields(f.value), " ")
	if value == "" {
		value = "(none)"
	}
	muted := e.Styles.Muted
	return []string{
		muted.Render(ansi.Truncate(f.label(), width, "…")),
		muted.Render(ansi.Truncate("  "+value, width, "…")),
	}
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
	var b Block
	if e.spec.Render != nil {
		b = e.spec.Render(s, width, height)
	} else {
		b = e.Block(width, height)
	}
	// The footer's hints widen a narrow panel up to modalHintWidth and they
	// follow the focus and the mode. A form takes that width from the start,
	// so its panel does not change size on the way through its stops.
	first, rest, more := strings.Cut(b.Text, "\n")
	if pad := min(width, modalHintWidth) - ansi.StringWidth(first); pad > 0 {
		first += strings.Repeat(" ", pad)
	}
	b.Text = first
	if more {
		b.Text += "\n" + rest
	}
	return b
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
	typing := e.r.modes[e.r.Mode.Name].Typing && f != nil && f.input != nil
	var cmd tea.Cmd
	changed := false
	if snap := e.snapshot(); snap != e.seen {
		e.refreshChoices()
		e.seen, changed = snap, true
		e.seq++
		if typing && e.spec.Immediate {
			e.fail(e.flush())
		} else if typing {
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
	if e.closed || e.spec.Load == nil && e.spec.Reload == nil {
		return nil
	}
	if e.loading {
		e.reloadAgain = true
		return nil
	}
	seq := e.seq
	e.loading = true
	var res Result
	if e.spec.Reload != nil {
		res = e.spec.Reload()
	} else {
		value, err := e.spec.Load()
		res = Done(value)
		res.err = err
	}
	apply := func(value any, err error) error {
		e.loading = false
		if e.closed {
			return nil
		}
		if e.reloadAgain || e.seq != seq {
			e.reloadAgain = false
			return e.outsideWrite()
		}
		if err != nil {
			return fmt.Errorf("load record: %w", err)
		}
		loaded, ok := value.(Record)
		if !ok {
			return errors.New("load record must return a Record")
		}
		return e.applyLoaded(loaded)
	}
	if res.work == nil {
		return apply(res.value, res.err)
	}
	res = res.also(func(c Ctx, value any, err error) { e.fail(apply(value, err)) })
	_, err := e.r.Launch(e.id, "ochazuke.form.load", res, Ctx{Runtime: e.r, Context: e.r.contextOf(e.id)})
	if err != nil {
		e.loading = false
	}
	return err
}

func (e *RecordEditor) applyLoaded(loaded Record) error {
	changed := false
	for _, f := range e.fields {
		v, ok := loaded[f.Name]
		if !ok {
			continue
		}
		if !e.out.load(f.Name, v) {
			continue
		}
		changed = true
		typed := f.text() != e.out.value(f.Name)
		e.out.set(f.Name, v)
		f.before = v
		if !typed {
			if f.input != nil {
				// Its own history starts over too: it no longer leads to
				// what is stored.
				f.input.SetText(v)
				f.input.Cursor = len(f.input.Text)
			} else if err := e.set(f, v); err != nil {
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
		Verb("undo", on(func(e *RecordEditor, _ Context, _ Arguments) error { return e.history(false) }), "Undo the record's last change; the undo is written like any change").History(HistoryChange).
		Verb("redo", on(func(e *RecordEditor, _ Context, _ Arguments) error { return e.history(true) }), "Redo the record's last undone change").History(HistoryChange).
		Verb("next", on(func(e *RecordEditor, _ Context, a Arguments) error { return e.move(1, boolArg(a, "insert")) }), "Focus the next field; the field left is written").Motion().
		Verb("previous", on(func(e *RecordEditor, _ Context, a Arguments) error { return e.move(-1, boolArg(a, "insert")) }), "Focus the previous field; the field left is written").Motion().
		Verb("choose", on(func(e *RecordEditor, c Context, _ Arguments) error { return e.choose(e.fieldOn(focusOf(c))) }), "Choose the field's value from its choices").
		Verb("suggest", on(func(e *RecordEditor, c Context, _ Arguments) error { return e.suggest(e.fieldOn(focusOf(c))) }), "Fill the field from its choices, typing to filter; a free field takes what is typed too").History(HistoryChange).
		Verb("run", on(func(e *RecordEditor, _ Context, _ Arguments) error { return e.run() }), "Run the form once every field is valid: the values go to its action and the form closes").When(FormRunnableGuard).
		Verb("next-missing", on(func(e *RecordEditor, _ Context, a Arguments) error { return e.nextMissing(boolArg(a, "insert")) }), "Focus the field that still needs a value (this one, else the next); a field with choices opens its picker").When(FormMissingGuard)
	r.DefineGuard(FormMissingGuard, func(r *Runtime, ev KeyEvent) bool {
		e := r.recordEditorOf(ev.Focus)
		return e != nil && e.spec.Submit != nil && !e.closed && e.firstProblem(0) != nil
	})
	r.DefineGuard(FormRunnableGuard, func(r *Runtime, ev KeyEvent) bool {
		e := r.recordEditorOf(ev.Focus)
		return e != nil && e.Runnable()
	})
	// A form of one field that writes each change (no Submit: edit
	// {field}) has nothing to move to: enter closes it (form.leave, which
	// writes what is typed).
	r.DefineGuard(FormOneFieldGuard, func(r *Runtime, ev KeyEvent) bool {
		e := r.recordEditorOf(ev.Focus)
		return e != nil && e.spec.Submit == nil && !e.closed && e.shownFields() == 1
	})
	for _, verb := range []string{"next", "previous", "next-missing"} {
		op := r.Operations.At(Key(FormNoun + "." + verb))
		op.Parameters = insert
		r.Operations.Set(op)
	}
	// One undo chain in every record form: a text input's own history comes
	// first, then the same key falls through to the record's history. The
	// keys' rows yield to the input (input.undo and input.redo answer from
	// the editor level while it has a step to take); the verbs themselves
	// stay available, so the palette and the CLI reach the record's history
	// from any stop.
	r.DefineGuard("form-record-history", func(r *Runtime, ev KeyEvent) bool {
		return r.recordEditorOf(ev.Focus) != nil
	})
	r.DefineGuard("input-no-undo", func(r *Runtime, ev KeyEvent) bool {
		in := r.Input(ev.Focus)
		return in == nil || !in.undoChangesText()
	})
	r.DefineGuard("input-no-redo", func(r *Runtime, ev KeyEvent) bool {
		in := r.Input(ev.Focus)
		return in == nil || !in.redoChangesText()
	})
	_ = r.AddVerbAvailability(FormNoun+".undo", "form-record-history")
	_ = r.AddVerbAvailability(FormNoun+".redo", "form-record-history")
	r.Define(FormChoiceNoun).Focusable()
	registerFormChips(r)
	registerFormItems(r)
	r.Define(FormOptionNoun).
		Verb("pick", func(r *Runtime, c Context, _ Arguments) (any, error) {
			inst := r.Instances[focusOf(c)]
			chooser, _ := r.Instances[formChooseID].Component.(*recordChooser)
			if inst == nil || chooser == nil {
				return nil, errors.New("no open picker")
			}
			v, _ := inst.Data.(string)
			return v, chooser.editor.pick(chooser.field, v)
		}, "Set the field to this choice; written at once").History(HistoryChange)
	registerStandardEdit(r)
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
		op := e.r.Operations.At(Key(e.r.ChoicesSource(f.Parameter)))
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
			if f.choose && !e.typing && f.text() != e.out.value(f.Name) {
				e.fail(e.seal(f))
			}
		}, args)
	}
}

// RowView exposes an explicitly declared backing view to shared list verbs.
func (e *RecordEditor) RowView() *Collection { return e.spec.RowView }

// noneChoice is the fixed "(none)" row of a chooser for an optional field.
func noneChoice() Choice {
	return Choice{Label: "(none)", RowFlags: RowFlags{Fixed: true, Bare: true}}
}
