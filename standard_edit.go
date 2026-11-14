package ochazuke

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
)

// EditFormsSetting makes the standard edit verbs available: off, every
// thing noun still has edit, unavailable with the reason "forms.edit is
// off". Set it before declaring stores or mounting a catalog; an
// application's own edit is unchanged.
const EditFormsSetting = "forms.edit"

// EditApplySetting chooses immediate debounced writes or one confirmed write.
const EditApplySetting = "forms.edit-apply"

// editApplyConfirm is the forms.edit-apply value that writes once, on submit.
const editApplyConfirm = "confirm"

// EditFieldOnlySetting: edit with a field (edit {field: "@label"}) opens a
// form holding only that field, opened as a rename (RecordSpec.Rename: its
// value selected under rename.select). Off, the full form opens with that
// field focused. Default on.
const EditFieldOnlySetting = "edit.field-only"

// EditFieldChooserSetting: edit with a field that takes a choice (an
// enum, a choice source, a noun reference, a boolean) opens that field's chooser in place of the one-field
// form; the pick is the write (one undo step) and closes it, esc closes it
// with nothing written. Read with edit.field-only: off, or edit.field-only
// off, the form opens as before. A request's chooser argument overrides
// both. Default on.
const EditFieldChooserSetting = "edit.field-picker"

// EditShowReadOnlySetting: the standard edit form (the modal and a placed
// EditSpec alike) also draws the thing's read-only props (a prop set cannot
// write: ReadOnly, CreateOnly; not its key, a computed prop or a secret),
// muted, in prop order, with no focus stop. Off, the form holds only what
// can be edited. Default on. Read when the form opens.
const EditShowReadOnlySetting = "forms.show-read-only"

// editFieldParam is edit's parameter naming the one prop to edit.
const editFieldParam = "field"

// editChooserParam is edit's parameter choosing, for a field that takes a
// choice, its chooser (true) or the one-field form (false); omitted,
// edit.field-picker decides.
const editChooserParam = "chooser"

// EditChooserKind is the kind of the chooser edit {field} opens on a
// choice field (Runtime.Chooser(EditChooserKind) while it is open).
const EditChooserKind = "edit-choose"

// editLoadingValue ends the edit chooser's title while the thing's
// current value is loading.
const editLoadingValue = "loading current value…"

const editFormID = "edit-form"
const editOpenSlot = "standard-edit-open"

func editParams(n *Noun) map[string]Parameter {
	params := map[string]Parameter{}
	var fields []string
	for _, p := range writableProps(n) {
		param := propParameter(p)
		param.Required = false // omitted props keep their values; the form enforces Required
		param.Named = true     // prop=value only: a word after field is no prop's value
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
	params[editFieldParam] = Parameter{Type: "string", Enum: fields, Accept: AcceptKeep, Plumbing: true,
		Description: "The one prop to edit (a name, or @label, @owner, @archive for the prop with that role): with edit.field-only the form holds only it, else the full form opens on it"}
	params[editChooserParam] = Parameter{Type: SettingBoolean, Plumbing: true,
		Description: "With field on a prop that takes a choice: true opens its picker (the pick writes and closes), false the one-field form; omitted, edit.field-picker decides (the argument keeps its old name chooser)"}
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
// history-entry.undo takes the form back in one step.
func (r *Runtime) editHandler(noun string) AppHandler {
	return func(c Ctx) Result {
		es, changes, err := r.newEditSession(noun, c)
		if err != nil {
			return Failed(err)
		}
		if len(changes) > 0 {
			res, _ := es.write(changes, false, 0)
			return res
		}
		if e := r.RecordEditor(editFormID); e != nil {
			if err := e.Leave(); err != nil {
				return Failed(err)
			}
			defer r.actingFor(r.Focus)()
		}
		res, ran := r.readEdit(c, noun, es.key)
		if res.err != nil {
			return res
		}
		if !ran {
			return Done(nil)
		} // a check or confirmation took over
		if res.work == nil {
			if err := es.open(res.value); err != nil {
				return Failed(err)
			}
			return Done(nil)
		}
		if early, ok := r.editChooserEarly(c, es.n, es.only, es.key, es.count(), es.write, res); ok {
			return early
		}
		return r.editOpenLater(c, res, es.open)
	}
}

// editSession is one run of the edit verb: the subjects it writes to
// (editSubject), the one prop asked for (only, nil for the full form),
// and the writer that applies a change to every subject as one history
// group.
type editSession struct {
	editSubject
	c     Ctx
	only  *PropSpec
	write editWrite
}

// editSubject is the thing an edit opens on and the others it writes to
// too (a selection, marks): the noun, the focused subject's key, the
// rest, and the history/write key one form's writes share.
type editSubject struct {
	r        *Runtime
	n        *Noun
	key      Arguments
	others   []any
	writeKey string
}

// newEditSession reads the request's subjects and arguments. changes are
// the props the request supplies (written without a form).
func (r *Runtime) newEditSession(noun string, c Ctx) (editSession, map[string]any, error) {
	n := r.Noun(noun)
	key, others, err := r.editSubjects(n, c)
	if err != nil {
		return editSession{}, nil, err
	}
	writeKey := noun
	if kp := n.keyProp(); kp != nil {
		writeKey += "\x00" + fmt.Sprint(key[kp.Name])
	}
	only, changes, err := editArgs(n, c.Args)
	if err != nil {
		return editSession{}, nil, err
	}
	s := editSubject{r: r, n: n, key: key, others: r.humanKeyAlone(n, only, others), writeKey: writeKey}
	write := r.editWriter(n, key, s.others, r.newHistoryGroup("edit "+writeKey))
	return editSession{editSubject: s, c: c, only: only, write: write}, changes, nil
}

// count is how many subjects a write reaches.
func (s editSubject) count() int { return 1 + len(s.others) }

// reload re-reads the subject for an open form (spec.Reload).
func (s editSubject) reload(c Ctx) Result {
	res, _ := s.r.readEdit(c, s.n.Name, s.key)
	if res.work == nil {
		if res.err != nil {
			return res
		}
		loaded, err := editRecord(s.n, res.value)
		if err != nil {
			return Failed(err)
		}
		return Done(loaded)
	}
	var raw any
	res = res.Map(func(value any, err error) (any, error) {
		if err != nil {
			return nil, err
		}
		raw = value
		return editRecord(s.n, raw)
	})
	if then := res.then; then != nil {
		res.then = func(c Ctx, _ any, err error) { then(c, raw, err) }
	}
	return res
}

// open shows the edit for the loaded value: the field chooser when the
// request asks for one, else the form.
func (es editSession) open(value any) error {
	r, n, c := es.r, es.n, es.c
	thing, err := ThingOf(value)
	if err != nil {
		return err
	}
	values, err := editRecord(n, thing)
	if err != nil {
		return err
	}
	fieldOnly := es.only != nil && r.Settings().Bool(EditFieldOnlySetting)
	fields := r.editFields(n, c, thing, es.only, fieldOnly)
	if field, ok := r.editByChooser(c.Args, es.only, fieldOnly, fields); ok {
		_, _, err := r.openEditChooser(c, n, field, values, r.editChooserTitle(n, c, thing, es.key, es.only, es.count()), es.write)
		return err
	}
	spec := es.formSpec(thing, values, fields, fieldOnly)
	_, err = r.OpenRecordEditor(editFormID, spec)
	return err
}

// formSpec is the standard edit form for the loaded thing (values its
// record, fields its schema): the one builder behind the modal edit and a
// placed EditSpec, so the two cannot drift.
func (es editSession) formSpec(thing Thing, values Record, fields []RecordField, fieldOnly bool) RecordSpec {
	r, n, c := es.r, es.n, es.c
	r.pendingRecord(es.writeKey, values)
	label := r.editLabel(n, c, thing, es.key)
	noun := r.DisplayNoun(n.Name)
	spec := RecordSpec{Title: "Edit " + noun + ": " + label, Fields: fields, WriteKey: es.writeKey, Load: func() (Record, error) { return values, nil }}
	editStart(&spec, es.only, fieldOnly, noun, label)
	spec.Watch = n.Name
	spec.WatchArgs = es.key
	spec.Reload = func() Result { return es.reload(c) }
	var editor *RecordEditor
	es.bindWrites(&spec, fields, &editor)
	spec.OnOpen = func(e *RecordEditor) { editor = e }
	return spec
}

// EditSpec is the standard edit form of the thing id of a noun (its key
// prop's value; "" for a singleton) as a RecordSpec: every writable prop as
// a field, loaded with the noun's get, written with its set (one history
// group for the whole form, forms.edit-apply deciding change or confirm),
// watching the noun for outside writes, with its read-only props muted
// (forms.show-read-only). It is what the noun's edit verb opens as a
// modal; an application gives it to AddRecordEditor to place the same form
// in a panel. A get still pending (a mounted noun's) leaves the form empty
// until it lands and fills it then. The result is the one forms.edit
// governs for the verb; EditSpec itself ignores that setting.
func (r *Runtime) EditSpec(noun, id string) (RecordSpec, error) {
	n := r.Noun(noun)
	if n == nil {
		return RecordSpec{}, fmt.Errorf("unknown noun %q", noun)
	}
	if _, ok := r.Operation(noun + ".get"); !ok {
		return RecordSpec{}, fmt.Errorf("%s has no get to load the form from", noun)
	}
	if _, ok := r.Operation(noun + ".set"); !ok {
		return RecordSpec{}, fmt.Errorf("%s has no set to write the form with", noun)
	}
	key := Arguments{}
	writeKey := noun
	if kp := n.keyProp(); kp != nil {
		typed, err := propValue(*kp, id)
		if err != nil {
			return RecordSpec{}, err
		}
		key[kp.Name] = typed
		writeKey += "\x00" + fmt.Sprint(typed)
	}
	r.knownTarget(n.Name, id) // the caller names the key itself: no probe read
	c := Ctx{Runtime: r, Context: r.contextOf("app"), Args: Arguments{}}
	es := editSession{
		editSubject: editSubject{r: r, n: n, key: key, writeKey: writeKey},
		c:           c,
		write:       r.editWriter(n, key, nil, r.newHistoryGroup("edit "+writeKey)),
	}
	res, _ := r.readEdit(c, noun, key)
	if res.err != nil {
		return RecordSpec{}, res.err
	}
	var thing Thing
	pending := res.work != nil
	if !pending {
		var err error
		if thing, err = ThingOf(res.value); err != nil {
			return RecordSpec{}, err
		}
	}
	values, err := editRecord(n, thing)
	if err != nil {
		return RecordSpec{}, err
	}
	spec := es.formSpec(thing, values, r.editFields(n, c, thing, nil, false), false)
	if pending {
		bound := spec.OnOpen
		spec.OnOpen = func(e *RecordEditor) {
			bound(e)
			e.fail(e.Reload())
		}
	}
	return spec, nil
}

// bindWrites sets how the form writes, by forms.edit-apply: confirm sends
// the changed fields at once on submit; the default writes each field as
// it changes. editor is the form once opened (the writers read its
// baseline).
func (es editSession) bindWrites(spec *RecordSpec, fields []RecordField, editor **RecordEditor) {
	if es.r.Settings().String(EditApplySetting) == editApplyConfirm {
		es.bindSubmit(spec, fields, editor)
		return
	}
	es.bindFieldWrites(spec, fields, editor)
}

// bindSubmit writes every changed field at once when the form submits.
func (es editSession) bindSubmit(spec *RecordSpec, fields []RecordField, editor **RecordEditor) {
	spec.Submit = func(rec Record) Result {
		changed := map[string]any{}
		for _, name := range (*editor).out.changedSince(rec, fieldNames(slices.DeleteFunc(slices.Clone(fields), func(f RecordField) bool { return f.ReadOnly }))) {
			value, err := editPropValue(*es.n.prop(name), rec[name])
			if err != nil {
				return Failed(fmt.Errorf("%s %w", name, err))
			}
			changed[name] = value
		}
		if len(changed) == 0 {
			return Done(nil)
		}
		res, own := es.write(changed, false, 0)
		return (*editor).editSaved(changed, res, own)
	}
}

// bindFieldWrites writes each field as it changes.
func (es editSession) bindFieldWrites(spec *RecordSpec, _ []RecordField, editor **RecordEditor) {
	spec.FieldWrites = true
	spec.Save = func(w RecordWrite) Result {
		name := w.Changed[0]
		value, err := editPropValue(*es.n.prop(name), w.Values[name])
		if err != nil {
			return Failed(err)
		}
		changes := map[string]any{name: value}
		res, own := es.write(changes, false, w.Walk)
		return (*editor).editSaved(changes, res, own)
	}
}

// editOpenLater opens the form (open) when a pending get lands, only
// where the user still is: the same focus, page, mode and top modal, and
// the program not quitting.
func (r *Runtime) editOpenLater(c Ctx, res Result, open func(any) error) Result {
	focus, page, mode, modal := r.Focus, r.Page, c.mode().Name, r.TopModal()
	return res.ReplacingAt("app", editOpenSlot).also(func(ctx Ctx, value any, err error) {
		if err == nil && !r.Quit && r.Focus == focus && r.Page == page && r.Mode.Name == mode && r.TopModal() == modal {
			if err := open(value); err != nil {
				ctx.Error(err.Error())
			}
		}
	})
}

// editChooserTitle is the edit chooser's title: the noun, the field and
// the thing (editLabel); over several subjects (a selection or marks, the
// rows a pick writes) the count and the plural noun in place of the thing
// ("Edit 2 tasks project"), as R5.15's "Move 3 rows to" counts them.
func (r *Runtime) editChooserTitle(n *Noun, c Ctx, thing Thing, subject Arguments, only *PropSpec, count int) string {
	if count > 1 {
		return fmt.Sprintf("Edit %d %s %s", count, pluralWord(r.DisplayNoun(n.Name)), only.Name)
	}
	return "Edit " + r.DisplayNoun(n.Name) + " " + only.Name + ": " + r.editLabel(n, c, thing, subject)
}

// editChooserEarly opens edit {field}'s chooser while the thing's get is
// still pending (res, off the loop: a mounted noun's remote get), so the
// keys typed meanwhile reach the chooser's query and never the page
// behind it; the get's answer fills it (the current value highlighted,
// the title) when it lands, and a failed get closes it (the job reports
// the error). Until it lands the title says the value is loading
// (editLoadingValue) and an enter with no choice made waits for it
// (openEditChooser). Once the chooser has picked or closed, a failed get
// is not reported: the pick's write is the result.
// ok is false where the field opens the form, which still waits for the
// get.
func (r *Runtime) editChooserEarly(c Ctx, n *Noun, only *PropSpec, subject Arguments, count int, write editWrite, res Result) (Result, bool) {
	fieldOnly := only != nil && r.Settings().Bool(EditFieldOnlySetting)
	field, ok := r.editByChooser(c.Args, only, fieldOnly, r.editFields(n, c, Thing{}, only, fieldOnly))
	if !ok {
		return Result{}, false
	}
	ch, fill, err := r.openEditChooser(c, n, field, nil, r.editChooserTitle(n, c, Thing{}, subject, only, count)+" · "+editLoadingValue, write)
	if err != nil {
		return Failed(err), true
	}
	open := func() bool { return r.Chooser(EditChooserKind) == ch }
	// Once the chooser has picked or closed, the get is no longer the
	// verb's answer: its failure is dropped, and the pick's own result
	// (its write) is what is shown.
	res = res.mapOnLoop(func(value any, err error) (any, error) {
		if err != nil && !open() {
			return nil, nil
		}
		return value, err
	})
	return res.ReplacingAt("app", editOpenSlot).also(func(ctx Ctx, value any, err error) {
		if !open() {
			return
		}
		var thing Thing
		var values Record
		if err == nil {
			if thing, err = ThingOf(value); err == nil {
				values, err = editRecord(n, thing)
			}
			if err != nil {
				ctx.Error(err.Error())
			}
		}
		fill(values, r.editChooserTitle(n, c, thing, subject, only, count), err)
	}), true
}

// editLabel names the thing an edit opens on: the focused row's label
// when the row is that thing, else the thing's label, else its key.
func (r *Runtime) editLabel(n *Noun, c Ctx, thing Thing, subject Arguments) string {
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
	return label
}

// humanKeyAlone is the other subjects an edit of field only writes: none
// when only is the noun's human key (humanProp), which names one thing, so
// one name typed is not every selected row's. The focused row takes it,
// and the notice says the rest were left as they were.
func (r *Runtime) humanKeyAlone(n *Noun, only *PropSpec, others []any) []any {
	human := n.humanProp()
	if human == nil || only == nil || only.Name != human.Name || len(others) == 0 {
		return others
	}
	r.UI().Info(fmt.Sprintf("left %d as they were: %s names one row", len(others), human.Name))
	return nil
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

// editWrite writes changes to an edit's subjects; held says the subject
// (the focused one) already holds them, so only the others are written.
// walk is a form's undo (-1) or redo (+1) of one field (RecordWrite.Walk),
// 0 for an edit. own is the focused subject's refusal when other subjects
// took the write (the result is then Done and the notice counts them).
type editWrite func(changes map[string]any, held bool, walk int) (res Result, own error)

// editStep is one write of a field by an edit: the rows it reached, each
// with the value it held before (read from the write's history change).
type editStep struct {
	field string
	rows  []editRow
}

// editRow is a subject an edit wrote: key nil for the one it opened on;
// before is the value it held, known when the store recorded it.
type editRow struct {
	key    any
	before any
	known  bool
}

// editLog is an edit's writes of each field, newest last (editStep):
// done the ones a form.undo takes back, undone the ones a form.redo
// writes again.
type editLog struct{ done, undone []editStep }

// rows are the subjects a write of changes reaches, and the field a
// form.undo or form.redo (walk) walks: for an edit, the subject and
// every other; for a walk of one field, the rows its newest step reached
// (the focused row alone when none was recorded), taken off its stack.
func (l *editLog) rows(changes map[string]any, others []any, walk int) ([]editRow, string) {
	if walk == 0 || len(changes) != 1 {
		rows := []editRow{{}}
		for _, k := range others {
			rows = append(rows, editRow{key: k})
		}
		return rows, ""
	}
	field := ""
	for f := range changes {
		field = f
	}
	from := &l.done
	if walk > 0 {
		from = &l.undone
	}
	i := lastEditStep(*from, field)
	if i < 0 {
		return []editRow{{}}, field
	}
	rows := (*from)[i].rows
	*from = slices.Delete(*from, i, i+1)
	return rows, field
}

// push records a write's rows per field: an undo's on undone, any other
// on done; an edit drops the field's undone steps, as a new change drops
// a redo.
func (l *editLog) push(changes map[string]any, wrote map[string][]editRow, walk int) {
	for f := range changes {
		step := editStep{field: f, rows: wrote[f]}
		switch {
		case walk < 0 && len(changes) == 1:
			l.undone = append(l.undone, step)
		default:
			l.done = append(l.done, step)
			if walk == 0 {
				l.undone = slices.DeleteFunc(l.undone, func(s editStep) bool { return s.field == f })
			}
		}
	}
}

// editWriter is the form's write: set {props} on the subject and on each
// other subject, every one recorded in group, so history-entry.undo takes the
// whole form back in one step. The noun's human key (humanProp) is
// written to the subject alone: one name typed is never every row's. A
// subject that refuses is counted and the rest are still written, the
// focused one as any other: a write over several subjects posts the one
// notice a run over many does (subjectsNotice: done on N rows, or how
// many failed), and fails only when none applied.
//
// It keeps each field's writes (editLog) so a form's undo of a field
// (walk -1) puts every row it reached back to that row's own earlier
// value, as history-entry.undo does, and leaves the rows it did not reach (a
// refusal) alone; a redo (walk +1) writes again only the rows the undo
// reached. A row whose earlier value the store did not record is left as
// it is, and a notice says so.
func (r *Runtime) editWriter(n *Noun, subject Arguments, others []any, group string) editWrite {
	var log editLog
	return func(changes map[string]any, held bool, walk int) (Result, error) {
		defer r.inHistoryGroup(group)()
		rows, field := log.rows(changes, others, walk)
		var w editWriting
		for _, row := range rows {
			props, ok := r.editProps(n, row, changes, field, walk)
			if !ok {
				w.unknown++
				continue
			}
			if len(props) == 0 || row.key == nil && held {
				continue
			}
			w.write(r, n, subject, row, props)
		}
		log.push(changes, w.wrote, walk)
		if w.unknown > 0 {
			r.UI().Info(fmt.Sprintf("left %d as they were: their earlier %s is not known", w.unknown, field))
		}
		switch {
		case len(w.applied) == 0 && len(w.skipped) == 1:
			return Failed(w.first), nil
		case len(w.applied)+len(w.skipped) > 1:
			if err := r.subjectsNotice(r.DisplayKey(n.Name+".edit"), w.applied, w.skipped, w.first); err != nil {
				return Failed(err), nil
			}
		}
		return Done(w.value), w.own
	}
}

// editProps are the props a write gives row: the changes, less the human
// key on a row other than the focused one; a form.undo of field gives
// each other row its own earlier value, and ok is false when that is not
// known.
func (r *Runtime) editProps(n *Noun, row editRow, changes map[string]any, field string, walk int) (map[string]any, bool) {
	props := maps.Clone(changes)
	if row.key == nil {
		return props, true
	}
	if human := n.humanProp(); human != nil {
		delete(props, human.Name)
	}
	if field != "" && walk < 0 {
		return map[string]any{field: row.before}, row.known
	}
	return props, true
}

// editWriting is one editWriter write as it goes: the rows applied and
// skipped, the first failure and the focused row's own, the focused
// row's answer, the rows written per field, and the rows left because
// their earlier value is not known.
type editWriting struct {
	value            any
	applied, skipped []string
	first, own       error
	wrote            map[string][]editRow
	unknown          int
}

// write sets props on row (the subject when its key is nil) and notes
// what it held before.
func (w *editWriting) write(r *Runtime, n *Noun, subject Arguments, row editRow, props map[string]any) {
	key := n.keyProp()
	args := maps.Clone(subject)
	if row.key != nil {
		args[key.Name] = row.key
	}
	args["props"] = props
	since := r.history.seq
	// The subject key, not the ephemeral row, identifies this write.
	v, err := r.ExecuteAt(r.contextOf("app"), Request{Noun: n.Name, Verb: "set", Args: args})
	id := ""
	if key != nil {
		id = fmt.Sprint(args[key.Name])
	}
	if err != nil {
		w.skipped = append(w.skipped, id+": "+err.Error())
		w.first = cmp.Or(w.first, err)
		if row.key == nil {
			w.own = err
		}
		return
	}
	w.applied = append(w.applied, id)
	if row.key == nil {
		w.value = v
	}
	before, known := r.editBefore(since, n.Name, id)
	if w.wrote == nil {
		w.wrote = map[string][]editRow{}
	}
	for f := range props {
		w.wrote[f] = append(w.wrote[f], editRow{key: row.key, before: before[f], known: known})
	}
}

// lastEditStep is the index of the newest of steps that wrote field, -1
// for none.
func lastEditStep(steps []editStep, field string) int {
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].field == field {
			return i
		}
	}
	return -1
}

// editBefore is what the thing key of noun held before the write recorded
// after since (its history change's Before); known is false when the
// write recorded no values (a store without Undo, a remote write still
// running).
func (r *Runtime) editBefore(since uint64, noun, key string) (map[string]any, bool) {
	entries := r.history.entries
	for i := len(entries) - 1; i >= 0 && entries[i].Seq > since; i-- {
		for _, ch := range entries[i].Changes {
			if ch.Noun == noun && ch.Key == key && ch.Before != nil {
				return ch.Before, true
			}
		}
	}
	return nil, false
}

// editSaved is a form's write result: a focused subject that refused
// leaves the form's fields in changes marked not written (their labels
// say why), while the write over the other subjects stands and its
// notice counts the done and the failed; the save fails only when no
// subject took it (res failed).
func (e *RecordEditor) editSaved(changes map[string]any, res Result, own error) Result {
	if res.err == nil && own != nil {
		e.notWritten(slices.Sorted(maps.Keys(changes)), own)
	}
	return res
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
	delete(rest, editChooserParam)
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
	showRO := !fieldOnly && r.Settings().Bool(EditShowReadOnlySetting)
	for _, p := range n.Props {
		if showRO && readOnlyShown(p) && r.propShown(p, focusOf(c.Context), c.mode(), c.Args) {
			field := PropField(p)
			field.ReadOnly = true
			fields = append(fields, field)
			continue
		}
		if !p.writable() {
			continue
		}
		if !r.propShown(p, focusOf(c.Context), c.mode(), c.Args) || fieldOnly && p.Name != only.Name {
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
		if (p.Type == SettingList && p.Items == nil) || p.Type == PropObject || p.Type == SettingDuration {
			field.Type = AnyParameterType
		}
		// The JSON field kind: an object is JSON text, several lines high;
		// text that is not an object is kept as typed and marked.
		if p.Type == PropObject {
			field.Multiline = true
		}
		fields = append(fields, field)
	}
	return fields
}

// readOnlyShown reports whether the standard edit form draws a prop it
// cannot write (forms.show-read-only): a read-only or create-only prop that
// is stored with the thing, not its key, not computed, not a secret.
func readOnlyShown(p PropSpec) bool {
	return !p.writable() && !p.Key && !p.computed() && !p.Secret
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
		if op.AppHandler == nil {
			value, err := op.Run(r, ctx, args)
			res = Done(value)
			return value, err
		}
		c.Context, c.Args, c.Invoked = ctx, args, r.Invoked()
		c.Instance = r.Instances[focusOf(ctx)]
		res = op.AppHandler(c)
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
	for _, p := range n.Props {
		if !p.writable() && !readOnlyShown(p) {
			continue
		}
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
	r.Define("ochazuke.noun").Verb("edit", func(r *Runtime, c Context, _ Arguments) (any, error) {
		q, _ := r.focusedEdit(focusOf(c))
		return r.ExecuteAt(c, q)
	}, "Open the focused thing's standard edit form; existing inner edit keys take precedence").When("noun-editable").FocusBound()
	r.DefineGuard("edit-opening", func(r *Runtime, _ KeyEvent) bool {
		return r.HasRunningJob("app", editOpenSlot)
	})
	r.Define("ochazuke.noun").Verb("cancel-edit", func(c Ctx) Result {
		if j := r.slotJob("app", editOpenSlot); j != nil {
			r.finish(j, JobCancelled, nil)
		}
		return Done(nil)
	}, "Cancel a pending edit open without writing").When("edit-opening")
	op := r.Operations.At(Key("ochazuke.noun.cancel-edit"))
	op.HistoryKind = HistoryAction
	r.Operations.Set(op)
}

// fieldNames are the names of fields, in order.
func fieldNames(fields []RecordField) []string {
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	return names
}
