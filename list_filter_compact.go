package ochazuke

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	filterSaveNoun       = "ochazuke.filter-save"
	filterFormNoun       = "ochazuke.filter-form"
	filterValuesNoun     = "ochazuke.filter-values"
	filterDirectionNoun  = "ochazuke.filter-direction"
	filterConditionNoun  = "ochazuke.filter-condition"
	filterSortField      = ".sort"
	filterSortKeysField  = ".sort-keys"
	filterDirectionField = ".direction"
	filterKeptField      = ".kept"
)

// filterViewForm composes the existing record writer, choosers and list.
// The record remains the form's modal component and owns validation/undo.
type filterViewForm struct {
	l                       *List
	e                       *RecordEditor
	fields                  []*filterField
	legacy                  bool
	fieldPicker, sortPicker *Chooser
	values                  map[string]*Chooser
	conditions              *List
	saveInput               *Input
	current                 *filterField
	multi                   bool
	seen                    string
	focus                   string
}

func (f *filterField) recordField(hidden bool) RecordField {
	f.sortLabels()
	rf := RecordField{Name: f.prop, Invertible: true, Hidden: hidden, Placeholder: f.placeholder()}
	switch f.kind {
	case filterChips:
		rf.Chips = true
		rf.Enum = slices.Clone(f.labels)
	case filterComplete:
		labels := slices.Clone(f.labels)
		rf.Completions = []CompletionTrigger{{Every: true, Boundaries: ",", Search: SearchStrings(func(_ context.Context, q string) ([]string, error) { return completeLabels(labels, q), nil })}}
	case filterText:
		rf.Multiline = f.spec.Multiline
	}
	rf.Validate = func(v string) error {
		for _, c := range f.conditions(v, false) {
			if _, err := typedPropFilter(f.spec, c); err != nil {
				return err
			}
		}
		return nil
	}
	return rf
}

func (v *filterViewForm) load() (Record, error) {
	return v.loadConditions(v.l.vs.Filters)
}

func (v *filterViewForm) loadConditions(conditions []PropFilter) (Record, error) {
	values := Record{}
	byProp := map[string][]PropFilter{}
	for _, c := range conditions {
		byProp[c.Prop] = append(byProp[c.Prop], c)
	}
	represented := map[string]bool{}
	for _, f := range v.fields {
		value, not, ok := f.absorb(byProp[f.prop])
		if v.e != nil {
			rf := v.e.field(f.prop)
			rf.Enum = slices.Clone(f.labels)
			if rf.chips {
				rf.choices, rf.lines = enumChoices(rf.Parameter), nil
			}
		}
		if !ok {
			continue
		}
		represented[f.prop] = true
		values[f.prop], values[f.prop+RecordInvertSuffix] = value, invertText(not)
	}
	var kept []PropFilter
	for _, c := range conditions {
		if !represented[c.Prop] {
			kept = append(kept, c)
		}
	}
	if len(kept) > 0 {
		raw, err := json.Marshal(kept)
		if err != nil {
			return nil, err
		}
		values[filterKeptField] = string(raw)
	}
	values[filterDirectionField] = "asc"
	keysJSON, err := json.Marshal(v.l.Sort())
	if err != nil {
		return nil, err
	}
	values[filterSortKeysField] = string(keysJSON)
	if keys := v.l.Sort(); len(keys) > 0 {
		values[filterSortField] = keys[0].Field
		if keys[0].Desc {
			values[filterDirectionField] = "desc"
		}
	}
	return values, nil
}

func (v *filterViewForm) write(w RecordWrite) Result {
	var conds []PropFilter
	if raw := w.Values[filterKeptField]; raw != "" {
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&conds); err != nil {
			return Failed(err)
		}
	}
	for _, f := range v.fields {
		conds = append(conds, f.conditions(w.Values[f.prop], w.Values[f.prop+RecordInvertSuffix] == "true")...)
	}
	if err := v.l.SetPropFilters(conds); err != nil {
		return Failed(err)
	}
	if slices.Contains(w.Changed, filterSortField) || slices.Contains(w.Changed, filterDirectionField) || slices.Contains(w.Changed, filterSortKeysField) {
		var keys []SortKey
		if raw := w.Values[filterSortKeysField]; raw != "" {
			if err := json.Unmarshal([]byte(raw), &keys); err != nil {
				return Failed(err)
			}
		}
		if by := w.Values[filterSortField]; by != "" {
			primary := SortKey{Field: by, Desc: w.Values[filterDirectionField] == "desc"}
			if len(keys) == 0 {
				keys = []SortKey{primary}
			} else {
				keys[0] = primary
				tail := keys[1:]
				keys = keys[:1]
				for _, key := range tail {
					if key.Field != primary.Field {
						keys = append(keys, key)
					}
				}
			}
		} else {
			keys = nil
		}
		if err := v.l.SetSort(keys); err != nil {
			return Failed(err)
		}
	}
	if v.sortPicker != nil {
		v.sortPicker.SetQuery(w.Values[filterSortField])
	}
	v.seen = v.signature()
	v.refreshConditions()
	v.refreshValues()
	return Done(nil)
}

func (l *List) openViewForm() error {
	if e := l.r.RecordEditor(listFilterFormID); e != nil && !e.closed && e.RowView() == l {
		if err := l.r.openFromTopLevel(e.id); err != nil {
			return err
		}
		if v := filterViewAt(l.r, e.id); v != nil && v.fieldPicker != nil {
			return v.focusPart("field")
		}
		return l.r.SetFocus(e.fields[e.start()].id)
	}
	v := &filterViewForm{l: l, fields: l.filterFields(), legacy: l.r.Settings().String(ListFilterLayoutSetting) == "fields", values: map[string]*Chooser{}}
	if len(v.fields) == 0 && !l.Sortable() {
		return errors.New("list rows have no filterable props")
	}
	if !v.legacy {
		for _, f := range v.fields {
			if f.kind == filterComplete {
				f.kind = filterChips
			}
		}
	}
	for _, f := range v.fields {
		var conditions []PropFilter
		for _, c := range l.vs.Filters {
			if c.Prop == f.prop {
				conditions = append(conditions, c)
			}
		}
		f.absorb(conditions)
	}
	sortNames := sortedKeys(l.sortSpecs())
	state := []RecordField{{Name: filterSortField, Label: "Sort", Hidden: true, Parameter: Parameter{Enum: sortNames}}, {Name: filterDirectionField, Label: "Direction", Hidden: true, Parameter: Parameter{Enum: []string{"asc", "desc"}}}}
	keepSort := (!l.r.Settings().Bool(ListFilterSortSetting) || !l.Sortable()) && !l.r.Settings().Bool(ListFilterClearHiddenSortSetting)
	for i := range state {
		state[i].KeepOnClear = keepSort
	}
	var schema []RecordField
	for _, f := range v.fields {
		schema = append(schema, f.recordField(!v.legacy))
	}
	state = append(state, RecordField{Name: filterKeptField, Hidden: true}, RecordField{Name: filterSortKeysField, Hidden: true, KeepOnClear: keepSort})
	e, err := l.r.OpenRecordEditor(listFilterFormID, RecordSpec{Title: "Filter", Fields: schema, State: state, Clearable: true, Immediate: !v.legacy, RowView: l, OnClear: v.clearInputs, Load: v.load, Save: v.write, Render: v.render})
	if err != nil {
		return err
	}
	v.e = e
	controller := l.r.Instance(e.id+"/controller", Parent(e.id), Kind(filterFormNoun))
	controller.Component = v
	if l.r.Settings().Bool(ListFilterSortSetting) && l.Sortable() {
		rows := []ChooserRow{{ID: "", Label: "App order", Data: ""}}
		for _, name := range sortNames {
			rows = append(rows, ChooserRow{ID: name, Label: name, Data: name})
		}
		v.sortPicker, err = l.r.AddChooser(ChooserSpec{Kind: e.id + "/sort", Plural: "sort fields", Rows: rows, Stay: true, NormalMode: true, NoPropFilters: true, NoSort: true, Choose: func(row ChooserRow) Result {
			if err := v.setField(filterSortField, row.Data.(string)); err != nil {
				return Failed(err)
			}
			v.sortPicker.SetQuery(row.Data.(string))
			if !v.legacy {
				_ = v.focusPart("field")
			}
			return Done(nil)
		}}, Parent(e.id))
		if err != nil {
			_ = e.Leave()
			return err
		}
		v.sortPicker.SetQuery(e.field(filterSortField).text())
		directionID := e.field(filterDirectionField).id
		if err := l.r.RemoveInstance(directionID); err != nil {
			return err
		}
		l.r.Instance(directionID, Parent(e.id), Kind(filterDirectionNoun))
	}
	if !v.legacy {
		var rows []ChooserRow
		for _, f := range v.fields {
			rows = append(rows, ChooserRow{ID: f.prop, Label: f.prop, Data: f.prop})
		}
		v.fieldPicker, err = l.r.AddChooser(ChooserSpec{Kind: e.id + "/field", Plural: "fields", Rows: rows, Stay: true, NormalMode: true, NoPropFilters: true, NoSort: true, Choose: func(row ChooserRow) Result {
			if err := v.editField(row.ID); err != nil {
				return Failed(err)
			}
			return Done(nil)
		}}, Parent(e.id))
		if err != nil {
			_ = e.Leave()
			return err
		}
		v.conditions, err = l.r.AddList(e.id+"/conditions", nil, Parent(e.id), NoMarks(), NoSpill(), NoSort())
		if err != nil {
			_ = e.Leave()
			return err
		}
		v.conditions.SetPlaceholder(Placeholder{Plural: "conditions"})
		v.refreshConditions()
		if err := v.focusPart("field"); err != nil {
			_ = e.Leave()
			return err
		}
	}
	if l.r.Settings().Bool(ListFilterSaveSetting) {
		v.saveInput = NewInput(e.id+"/save", l.vs.Saved, false)
		if err := l.r.AddInput(v.saveInput, Parent(e.id), Kind(filterSaveNoun)); err != nil {
			_ = e.Leave()
			return err
		}
		l.r.Instances[v.saveInput.ID].Pin = "insert"
	}
	for _, inst := range l.r.Children(e.id) {
		if inst != controller {
			inst.Parent = controller.ID
		}
	}
	v.seen = v.signature()
	return nil
}

func (v *filterViewForm) clearInputs() error {
	if v.sortPicker != nil {
		v.sortPicker.SetQuery("")
	}
	for _, c := range v.values {
		c.SetQuery("")
	}
	if !v.legacy {
		// form.clear is a normal-mode command: focusing the field picker
		// would land in its insert mode, so the next key (u, to take the
		// clear back) would type into the query. The mode stays as it was.
		mode := v.l.r.Mode.Name
		if err := v.focusPart("field"); err != nil {
			return err
		}
		if v.l.r.Mode.Name != mode {
			return v.l.r.SetMode(mode)
		}
	}
	return nil
}

func (v *filterViewForm) signature() string {
	raw, _ := json.Marshal(struct {
		Conditions []PropFilter
		Sort       []SortKey
		Name       string
	}{v.l.vs.Filters, v.l.vs.Sort, v.l.vs.Saved})
	return string(raw)
}

func (v *filterViewForm) setField(name, value string) error {
	f := v.e.field(name)
	if f == nil {
		return fmt.Errorf("unknown form field %q", name)
	}
	if err := v.e.set(f, value); err != nil {
		return err
	}
	return v.e.seal(f)
}

func (v *filterViewForm) hideCurrent() {
	if v.current == nil {
		return
	}
	rf := v.e.field(v.current.prop)
	v.l.r.Instances[rf.id].Unfocusable = true
	if c := v.values[v.current.prop]; c != nil {
		v.l.r.Instances[c.input.ID].Unfocusable = true
	}
}

func (v *filterViewForm) focusPart(part string) error {
	switch part {
	case "save":
		if v.saveInput == nil {
			return errors.New("save controls are disabled")
		}
		if err := v.l.r.SetFocus(v.saveInput.ID); err != nil {
			return err
		}
		return v.saveInput.SelectAll()
	case "sort":
		if v.sortPicker == nil {
			return errors.New("sort controls are disabled")
		}
		if err := v.l.r.SetFocus(v.sortPicker.input.ID); err != nil {
			return err
		}
		return v.sortPicker.input.SelectAll()
	case "conditions":
		if v.conditions != nil {
			return v.conditions.Focus()
		}
	case "field":
		if v.fieldPicker != nil {
			if v.current != nil {
				if err := v.e.seal(v.e.field(v.current.prop)); err != nil {
					return err
				}
			}
			v.hideCurrent()
			v.current = nil
			v.multi = false
			v.fieldPicker.SetQuery("")
			return v.l.r.SetFocus(v.fieldPicker.input.ID)
		}
	}
	return errors.New("no such form control")
}

func (v *filterViewForm) editField(name string) error {
	fIndex := slices.IndexFunc(v.fields, func(f *filterField) bool { return f.prop == name })
	if fIndex < 0 {
		return fmt.Errorf("no filter field %q", name)
	}
	v.hideCurrent()
	v.current = v.fields[fIndex]
	v.multi = false
	if v.fieldPicker != nil {
		v.fieldPicker.SetQuery("")
	}
	rf := v.e.field(name)
	if v.current.kind == filterText {
		v.l.r.Instances[rf.id].Unfocusable = false
		v.l.r.Instances[rf.id].Pin = "insert"
		if err := v.l.r.SetFocus(rf.id); err != nil {
			return err
		}
		return v.l.r.SetMode("insert")
	}
	c := v.values[name]
	if c == nil {
		var err error
		c, err = v.l.r.AddChooser(ChooserSpec{Kind: rf.id + "/values", Plural: "values", Stay: true, NormalMode: true, NoPropFilters: true, NoSort: true, Choose: func(row ChooserRow) Result {
			chosen, _ := ParseChips(v.e.field(name).text())
			if !v.multi && !slices.Contains(chosen, row.Data.(string)) {
				if err := v.toggleValue(row.Data.(string)); err != nil {
					return Failed(err)
				}
			}
			if err := v.focusPart("field"); err != nil {
				return Failed(err)
			}
			return Done(nil)
		}}, Parent(rf.id), Kind(filterValuesNoun))
		if err != nil {
			return err
		}
		v.values[name] = c
	}
	v.l.r.Instances[c.input.ID].Unfocusable = false
	c.SetQuery("")
	c.SetRows(v.valueRows(v.current))
	return v.l.r.SetFocus(c.input.ID)
}

func (v *filterViewForm) valueRows(f *filterField) []ChooserRow {
	counts := map[string]int{}
	for _, row := range v.l.allRows() {
		p := v.l.filterProp(v.l.r.Noun(row.Noun), f.prop)
		if p == nil {
			continue
		}
		value, ok := v.l.thing(row)[f.prop]
		if !ok {
			continue
		}
		typed, err := propValue(*p, value)
		if err != nil {
			continue
		}
		if list, ok := typed.([]string); ok {
			seen := map[string]bool{}
			for _, label := range list {
				if !seen[label] {
					counts[label]++
					seen[label] = true
				}
			}
		} else {
			counts[fmt.Sprint(typed)]++
		}
	}
	chosen, _ := ParseChips(v.e.field(f.prop).text())
	var rows []ChooserRow
	for i, label := range f.labels {
		marker := v.l.r.glyph("check.off")
		if slices.Contains(chosen, label) {
			marker = v.l.r.glyph("check.on")
		}
		shown := label
		if shown == "" {
			shown = "(blank)"
		}
		rows = append(rows, ChooserRow{ID: strconv.Itoa(i), Label: shown, Data: label, Cells: []string{marker + " " + shown, fmt.Sprintf("%d rows", counts[label])}})
	}
	return rows
}

func (v *filterViewForm) refreshValues() {
	if v.e == nil || v.current == nil {
		return
	}
	if c := v.values[v.current.prop]; c != nil {
		c.SetRows(v.valueRows(v.current))
	}
}
func (v *filterViewForm) toggleValue(label string) error {
	if v.current == nil {
		return errors.New("no selected prop")
	}
	f := v.e.field(v.current.prop)
	values, _ := ParseChips(f.text())
	if at := slices.Index(values, label); at >= 0 {
		values = slices.Delete(values, at, at+1)
	} else {
		values = append(values, label)
	}
	if err := v.setField(f.Name, ChipsValue(values)); err != nil {
		return err
	}
	v.refreshValues()
	return nil
}
func (v *filterViewForm) toggleHighlighted() error {
	if v.current == nil {
		return errors.New("no selected values")
	}
	c := v.values[v.current.prop]
	if c == nil {
		return errors.New("this prop takes text")
	}
	row, ok := c.Highlighted()
	if !ok {
		return errors.New("no matching value")
	}
	if err := v.toggleValue(row.Data.(string)); err != nil {
		return err
	}
	v.multi = true
	return nil
}
func (v *filterViewForm) flipDirection() error {
	direction := "desc"
	if v.e.field(filterDirectionField).text() == "desc" {
		direction = "asc"
	}
	return v.setField(filterDirectionField, direction)
}
func (v *filterViewForm) refreshConditions() {
	if v.conditions == nil {
		return
	}
	var rows []ListItem
	for i, c := range v.l.PropFilters() {
		label := c.Label()
		if op, ok := filterOps[c.Op]; ok && op.test == "in" {
			values, _ := filterValues(c.Value)
			labels := make([]string, len(values))
			for j, value := range values {
				labels[j] = modalLabel(fmt.Sprint(value))
			}
			label = c.Prop + " " + op.label + " " + strings.Join(labels, ", ")
		}
		rows = append(rows, ListItem{ID: v.e.id + "/condition/" + strconv.Itoa(i), Noun: filterConditionNoun, Label: label, Data: i})
	}
	_ = v.conditions.SetItems(rows)
}
func (v *filterViewForm) selectedCondition() (int, error) {
	if v.conditions != nil {
		if row, ok := v.conditions.Selected(); ok {
			if i, ok := row.Data.(int); ok && i < len(v.l.vs.Filters) {
				return i, nil
			}
		}
	}
	return 0, errors.New("no selected condition")
}
func (v *filterViewForm) replaceConditions(conds []PropFilter) error {
	// Absorb the new conditions into the same record fields, including its
	// hidden unrepresentable conditions. Written field changes stay undoable.
	loaded, err := v.loadConditions(conds)
	if err != nil {
		return err
	}
	start := len(v.e.undo)
	defer func() {
		if len(v.e.undo)-start > 1 {
			changes := slices.Clone(v.e.undo[start:])
			v.e.undo = append(v.e.undo[:start], recordStep{changes: changes})
		}
	}()
	for _, f := range v.e.fields {
		if f.Name == filterSortField || f.Name == filterDirectionField || f.Name == filterSortKeysField {
			continue
		}
		value := loaded[f.Name]
		not := loaded[f.Name+RecordInvertSuffix] == "true"
		if f.text() != value {
			if err := v.setField(f.Name, value); err != nil {
				return err
			}
		}
		if f.Invertible && f.not != not {
			if err := v.e.invert(f); err != nil {
				return err
			}
		}
	}
	return nil
}
func (v *filterViewForm) conditionAction(action string) error {
	i, err := v.selectedCondition()
	if err != nil {
		return err
	}
	conds := v.l.PropFilters()
	switch action {
	case "edit":
		for _, f := range v.fields {
			if f.prop == conds[i].Prop {
				if _, _, ok := f.absorb([]PropFilter{conds[i]}); !ok {
					return errors.New("this condition is kept as-is; remove it to replace it")
				}
				return v.editField(f.prop)
			}
		}
		return errors.New("this prop is unavailable; its condition is kept as-is")
	case "remove":
		conds = slices.Delete(conds, i, i+1)
	case "invert":
		conds[i] = conds[i].Negate()
	}
	return v.replaceConditions(conds)
}

func (v *filterViewForm) HandleMessage(tea.Msg) bool { return false }
func (v *filterViewForm) Close()                     {}
func (v *filterViewForm) PendingCommand() tea.Cmd {
	if v.e == nil || v.e.closed {
		return nil
	}
	if v.seen != v.signature() {
		loaded, err := v.load()
		if err != nil {
			v.e.fail(err)
			return nil
		}
		var changes []recordStep
		for _, f := range v.e.fields {
			if before := v.e.out.value(f.Name); before != loaded[f.Name] {
				changes = append(changes, recordStep{field: f.Name, from: before, to: loaded[f.Name]})
			}
			if f.Invertible && invertText(f.not) != loaded[f.Name+RecordInvertSuffix] {
				changes = append(changes, recordStep{field: f.Name + RecordInvertSuffix, from: invertText(f.not), to: loaded[f.Name+RecordInvertSuffix]})
			}
			_ = v.e.set(f, loaded[f.Name])
			f.not = loaded[f.Name+RecordInvertSuffix] == "true"
			v.e.out.set(f.Name, loaded[f.Name])
			f.before = loaded[f.Name]
			if f.Invertible {
				v.e.out.set(f.Name+RecordInvertSuffix, invertText(f.not))
			}
		}
		v.e.seen = v.e.snapshot()
		if len(changes) > 0 {
			v.e.undo = append(v.e.undo, recordStep{changes: changes})
			v.e.redo = nil
		}
		if v.saveInput != nil && len(v.saveInput.Text) == 0 && v.l.vs.Saved != "" {
			_ = v.saveInput.Replace(0, len(v.saveInput.Text), v.l.vs.Saved)
		}
		v.seen = v.signature()
		v.refreshConditions()
		v.refreshValues()
	}
	if v.sortPicker != nil {
		if v.l.r.Focus == v.sortPicker.input.ID {
			if v.focus != v.l.r.Focus {
				v.sortPicker.SetQuery("")
			}
		} else if value := v.e.field(filterSortField).text(); v.sortPicker.Query() != value {
			v.sortPicker.SetQuery(value)
		}
	}
	v.focus = v.l.r.Focus
	return nil
}

func (v *filterViewForm) render(s Styles, width, height int) Block {
	if v.e == nil {
		return Text("")
	}
	var lines []string
	var cursor *Pos
	focusLine := 0
	pickerRows := [2]int{} // the sort picker's rows in lines: [from, to)
	add := func(b Block, focus string) {
		if v.l.r.Focus == focus {
			focusLine = len(lines)
			if b.Cursor != nil {
				cursor = &Pos{X: b.Cursor.X, Y: len(lines) + b.Cursor.Y}
			}
		}
		lines = append(lines, strings.Split(b.Text, "\n")...)
	}
	if v.sortPicker != nil {
		marker := ""
		if v.l.r.Settings().Bool(ListSortIndicatorSetting) && len(v.l.vs.Sort) > 0 {
			marker = " " + s.Glyph("sort.asc")
			if v.l.vs.Sort[0].Desc {
				marker = " " + s.Glyph("sort.desc")
			}
		}
		heading := "Sort by" + marker
		if len(v.l.vs.Sort) > 1 {
			var extra []string
			for i, k := range v.l.vs.Sort[1:] {
				label := k.Field
				if v.l.r.Settings().Bool(ListSortIndicatorSetting) {
					arrow := s.Glyph("sort.asc")
					if k.Desc {
						arrow = s.Glyph("sort.desc")
					}
					label += fmt.Sprintf(" %s%d", arrow, i+2)
				}
				extra = append(extra, label)
			}
			heading += " · then " + strings.Join(extra, ", ")
		}
		inputWidth := max(8, width-16)
		// The controls' width holds the longer direction, so reversing it
		// or opening the picker leaves the row as wide as it was.
		controls := min(width, inputWidth+2+len("desc"))
		lines = append(lines, s.Muted.Render(ansi.Truncate(heading, controls, "…")))
		input := InputBlock(v.sortPicker.input, inputWidth, 1)
		direction := v.e.field(filterDirectionField)
		dir := direction.text()
		if dir == "" {
			dir = "asc"
		}
		focusedDirection := v.l.r.Focus == direction.id
		dir = s.Selection(focusedDirection).Render(dir)
		v.l.r.Placed(v.sortPicker.input.ID)
		open := v.l.r.Focus == v.sortPicker.input.ID
		var rows []string
		count := ""
		if open {
			// The picker's own query line is not drawn here (the direction
			// shares this row), so its hidden-row count goes after it.
			rows = strings.Split(v.sortPicker.View(width, 4).Text, "\n")
			if n := v.sortPicker.HiddenRows(); n > 0 {
				count = s.Muted.Render(fmt.Sprintf("  %d more", n))
			}
		}
		// The count takes room from the gap, so the row keeps its width.
		gap := max(1, inputWidth-ansi.StringWidth(input.Text)+2-ansi.StringWidth(count))
		row := ansi.Truncate(input.Text+strings.Repeat(" ", gap)+v.l.r.PlacedAt(direction.id, dir, 0, 1)+count, controls, "")
		row += strings.Repeat(" ", max(0, controls-ansi.StringWidth(row)))
		add(Block{Text: row, Cursor: input.Cursor}, v.sortPicker.input.ID)
		if v.l.r.Focus == direction.id {
			focusLine = len(lines) - 1
		}
		if len(rows) > 1 {
			pickerRows[0] = len(lines)
			lines = append(lines, rows[1:]...)
			pickerRows[1] = len(lines)
		}
		if v.l.r.Focus != v.sortPicker.input.ID {
			lines = append(lines, "", "")
		}
	}
	if v.legacy {
		b := v.e.Block(width, max(1, height-len(lines)-v.saveHeight()))
		if b.Cursor != nil {
			b.Cursor = &Pos{X: b.Cursor.X, Y: b.Cursor.Y + len(lines)}
		}
		lines = append(lines, strings.Split(b.Text, "\n")...)
		if v.saveInput != nil {
			sb := v.saveBlock(s, width)
			if sb.Cursor != nil {
				b.Cursor = &Pos{X: sb.Cursor.X, Y: len(lines) + sb.Cursor.Y}
			}
			lines = append(lines, strings.Split(sb.Text, "\n")...)
		}
		// The fields layout sizes the modal to its controls; the picker's
		// rows are drawn at the full width, so they would widen the modal
		// while the sort query has the focus. They take the controls' width.
		natural := 0
		for i, line := range lines {
			if i < pickerRows[0] || i >= pickerRows[1] {
				natural = max(natural, ansi.StringWidth(line))
			}
		}
		for i := pickerRows[0]; i < pickerRows[1]; i++ {
			lines[i] = ansi.Truncate(lines[i], natural, "")
		}
		return Block{Text: strings.Join(lines, "\n"), Cursor: b.Cursor}
	}
	lines = append(lines, s.Muted.Render("Field"))
	if v.current == nil {
		v.l.r.PlacedAt(v.fieldPicker.input.ID, "", 0, 1)
		add(v.fieldPicker.View(width, 5), v.fieldPicker.input.ID)
	} else {
		v.l.r.PlacedAt(v.fieldPicker.input.ID, "", 0, 1)
		add(InputBlock(v.fieldPicker.input, width, 1), v.fieldPicker.input.ID)
		f := v.e.field(v.current.prop)
		label := v.current.prop + " · " + invertWord(f.not)
		if problem := v.e.problem(f); problem != "" {
			label += " · not written: " + problem
		}
		lines = append(lines, v.e.fieldStyle(f, s.Accent).Render(ansi.Truncate(label, width, "…")))
		if c := v.values[v.current.prop]; c != nil && v.current.kind != filterText {
			v.l.r.PlacedAt(c.input.ID, "", 0, 1)
			add(c.View(width, 5), c.input.ID)
		} else {
			v.l.r.PlacedAt(f.id, "", 0, 1)
			add(InputBlock(f.input, width, 1), f.id)
		}
	}
	lines = append(lines, s.Muted.Render("Active conditions"))
	if v.conditions != nil {
		at := len(lines)
		rows := max(1, v.conditions.Shown())
		if v.conditions.Filtering() || v.conditions.Filter() != "" {
			rows++
		}
		add(Text(v.conditions.View(width, min(3, rows))), v.conditions.id)
		if v.conditions.Focused() {
			focusLine = at
		}
	}
	if v.e.out.err != "" {
		lines = append(lines, s.Error.Render(v.e.out.err))
	}
	if v.saveInput != nil {
		add(v.saveBlock(s, width), v.saveInput.ID)
	}
	room := max(1, height-1)
	if len(lines) > room {
		top := min(max(0, focusLine-1), len(lines)-room)
		lines = lines[top : top+room]
		if cursor != nil {
			cursor.Y -= top
			if cursor.Y < 0 || cursor.Y >= room {
				cursor = nil
			}
		}
	}
	if v.l.r.Settings().Bool(ChooserStableHeightSetting) {
		for len(lines) < room {
			lines = append(lines, "")
		}
	}
	lines = append(lines, s.Muted.Render(fmt.Sprintf("%d of %d", v.l.Shown(), len(v.l.allRows()))))
	return Block{Text: strings.Join(lines, "\n"), Cursor: cursor}
}

func filterViewAt(r *Runtime, focus string) *filterViewForm {
	if e := r.recordEditorOf(focus); e != nil {
		if inst := r.Instances[e.id+"/controller"]; inst != nil {
			v, _ := inst.Component.(*filterViewForm)
			return v
		}
	}
	return nil
}
func registerFilterViewForm(r *Runtime) {
	r.Define(filterFormNoun)
	r.DefineGuard("filter-form-open", func(r *Runtime, ev KeyEvent) bool { return filterViewAt(r, ev.Focus) != nil })
	r.Define(filterSaveNoun)
	r.Define(filterValuesNoun)
	r.Define(filterDirectionNoun).Focusable().Pin("normal")
	r.Define(filterConditionNoun)
	r.DefineGuard("filter-form-text", func(r *Runtime, ev KeyEvent) bool {
		v := filterViewAt(r, ev.Focus)
		return v != nil && v.current != nil && v.current.kind == filterText && v.e.field(v.current.prop).id == r.EventFocus(ev)
	})
	r.DefineGuard("filter-form-tab", func(r *Runtime, ev KeyEvent) bool {
		v := filterViewAt(r, ev.Focus)
		return v != nil && !r.Completer().Open()
	})
	r.DefineGuard("filter-form-save", func(r *Runtime, ev KeyEvent) bool {
		v := filterViewAt(r, ev.Focus)
		return v != nil && v.saveInput != nil
	})
	r.DefineGuard("filter-form-delete-saved", func(r *Runtime, ev KeyEvent) bool {
		v := filterViewAt(r, ev.Focus)
		return v != nil && v.saveInput != nil
	})
	r.DefineGuard("filter-form-sort", func(r *Runtime, ev KeyEvent) bool {
		v := filterViewAt(r, ev.Focus)
		return v != nil && v.sortPicker != nil
	})
	for _, spec := range []struct {
		verb, description string
		params            map[string]Parameter
		run               func(*filterViewForm, Arguments) error
	}{
		{"focus", "Focus a filter form control", map[string]Parameter{"part": {Type: "string", Enum: []string{"field", "sort", "conditions", "save"}}}, func(v *filterViewForm, a Arguments) error { return v.focusPart(StringArg(a, "part", "field")) }},
		{"write-saved", "Save the current conditions and sort through list.filter-save", nil, func(v *filterViewForm, _ Arguments) error { return v.savedAction("filter-save") }},
		{"delete-saved", "Choose a saved filter to delete, with confirmation", nil, func(v *filterViewForm, _ Arguments) error { return v.deleteSaved() }},
		{"next", "Keep the text condition and return to the field picker", nil, func(v *filterViewForm, _ Arguments) error { return v.focusPart("field") }},
		{"toggle", "Toggle the highlighted value without leaving its picker", nil, func(v *filterViewForm, _ Arguments) error { return v.toggleHighlighted() }},
		{"move", "Move between controls in drawing order", map[string]Parameter{"step": {Type: "integer"}}, func(v *filterViewForm, a Arguments) error { return v.l.r.JumpFocus(IntArg(a, "step", 1), "stop") }},
		{"direction", "Reverse the sort direction", nil, func(v *filterViewForm, _ Arguments) error { return v.flipDirection() }},
		{"edit-condition", "Edit the selected active condition", nil, func(v *filterViewForm, _ Arguments) error { return v.conditionAction("edit") }},
		{"remove-condition", "Remove the selected active condition", nil, func(v *filterViewForm, _ Arguments) error { return v.conditionAction("remove") }},
		{"invert-condition", "Invert the selected active condition", nil, func(v *filterViewForm, _ Arguments) error { return v.conditionAction("invert") }},
	} {
		run := spec.run
		r.Register(Operation{Noun: filterFormNoun, Verb: spec.verb, Description: spec.description, Parameters: spec.params, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			v := filterViewAt(r, focusOf(c))
			if v == nil {
				return nil, errors.New("no filter form")
			}
			return nil, run(v, a)
		}})
		_ = r.AddVerbAvailability(filterFormNoun+"."+spec.verb, "filter-form-open")
	}
	op := r.Operations[filterFormNoun+".delete-saved"]
	op.Destructive = true
	r.Operations[op.Key()] = op
	r.Nouns[filterFormNoun].Guards = map[string]string{"direction": "filter-form-sort", "next": "filter-form-text", "move": "filter-form-tab", "write-saved": "filter-form-save", "delete-saved": "filter-form-delete-saved"}
}
