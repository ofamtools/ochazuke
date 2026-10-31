package ochazuke

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The filter form: the prop filter key (list.filter-prop with no prop)
// opens a record editor over the list, tree or chooser with a field per
// filterable prop (setting list.filter-form; off keeps the prop chooser
// then the value chooser). Every field is Invertible (is, not) and takes
// its kind from the rows and the declared props, never from a noun's name:
//
//   - finite (a declared Enum, a boolean, a list prop, or a text prop whose
//     values repeat across the rows): its values as chips; one chip makes
//     =, several in, and on a list prop each chip a contains;
//   - finite with more values than list.filter-chip-max: a text input of
//     comma-separated values that completes them;
//   - anything else (free text, numbers, dates, durations): a text input,
//     contains on text and = on the rest.
//
// Inverting turns every condition the field makes over (!=, not-in,
// not-contains). The form writes each change at once, so the rows follow
// as it is edited; esc leaves it with the conditions kept, and form.clear
// empties it. It opens on the list's conditions: the ones a field can show
// fill it, the others are kept as they are.

// Filter form settings and its modal id.
const (
	ListFilterFormSetting    = "list.filter-form"
	ListFilterChipMaxSetting = "list.filter-chip-max"
	listFilterFormID         = "list-filter-form"
)

// filterFieldKind is how a filter form field edits its prop.
type filterFieldKind int

const (
	filterChips    filterFieldKind = iota // the values as chips
	filterComplete                        // values typed, completed from the list
	filterText                            // free text
)

// filterField is one prop of the filter form.
type filterField struct {
	prop   string
	spec   PropSpec
	kind   filterFieldKind
	list   bool           // a list prop: each value is a contains
	labels []string       // the values as shown, in order
	values map[string]any // typed values by label
}

// propValues are a prop's typed values over rows by label, whether it is
// a list prop, and how many rows carry it.
func (l *List) propValues(prop string, rows []ListItem) (values map[string]any, list bool, carried int) {
	values = map[string]any{}
	for _, item := range rows {
		p := l.filterProp(l.r.Noun(item.Noun), prop)
		if p == nil {
			continue
		}
		value, ok := l.thing(item)[prop]
		if !ok {
			continue
		}
		typed, err := propValue(*p, value)
		if err != nil {
			continue
		}
		carried++
		if items, ok := typed.([]string); ok {
			list = true
			for _, one := range items {
				values[one] = one
			}
			continue
		}
		values[fmt.Sprint(typed)] = typed
	}
	return values, list, carried
}

// allRows are every row of the list (a tree's every node), whatever the
// filters show.
func (l *List) allRows() []ListItem {
	t := l.tree()
	if t == nil {
		return l.all
	}
	out := make([]ListItem, 0, len(t.nodes))
	for _, n := range t.nodes {
		if row := t.rowCache[n.ID]; row != nil {
			out = append(out, row.item)
		}
	}
	return out
}

// filterFields are the form's fields, in prop order.
func (l *List) filterFields() []*filterField {
	specs := l.filterSpecs()
	rows := l.allRows()
	chipMax := max(1, l.r.Settings().Int(ListFilterChipMaxSetting))
	var out []*filterField
	for _, name := range sortedKeys(specs) {
		spec := specs[name]
		values, list, carried := l.propValues(name, rows)
		f := &filterField{prop: name, spec: spec, list: list || spec.Type == SettingList, values: values}
		for _, e := range spec.Enum {
			if typed, err := propValue(spec, e); err == nil {
				values[fmt.Sprint(typed)] = typed
			}
		}
		if spec.Type == SettingBoolean {
			values["true"], values["false"] = true, false
		}
		text := spec.Type == "" || spec.Type == SettingString
		finite := f.list || len(spec.Enum) > 0 || spec.Type == SettingBoolean || (text && !spec.Multiline && len(values) > 0 && len(values) < carried)
		switch {
		case !finite:
			f.kind = filterText
		case len(values) > chipMax:
			f.kind = filterComplete
		default:
			f.kind = filterChips
		}
		out = append(out, f)
	}
	return out
}

// sortLabels orders the values: the declared Enum's order first, then the
// rest by label.
func (f *filterField) sortLabels() {
	f.labels = f.labels[:0]
	for _, e := range f.spec.Enum {
		if typed, err := propValue(f.spec, e); err == nil {
			if label := fmt.Sprint(typed); !slices.Contains(f.labels, label) {
				f.labels = append(f.labels, label)
			}
		}
	}
	for _, label := range slices.Sorted(maps.Keys(f.values)) {
		if !slices.Contains(f.labels, label) {
			f.labels = append(f.labels, label)
		}
	}
}

// test is the operator test one value makes: contains on a list prop or
// a free text prop, = on the rest.
func (f *filterField) test() string {
	if f.list || f.kind == filterText && (f.spec.Type == "" || f.spec.Type == SettingString) {
		return "contains"
	}
	return "="
}

// conditions are what a field's value (and inverted state) filters by.
func (f *filterField) conditions(value string, not bool) []PropFilter {
	var labels []string
	switch f.kind {
	case filterChips:
		labels, _ = ParseChips(value)
	case filterComplete:
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				labels = append(labels, part)
			}
		}
	case filterText:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return negated([]PropFilter{{Prop: f.prop, Op: f.test(), Value: value}}, not)
	}
	if len(labels) == 0 {
		return nil
	}
	typed := func(label string) any {
		if v, ok := f.values[label]; ok {
			return v
		}
		return label
	}
	var out []PropFilter
	switch {
	case len(labels) == 1 || f.list:
		for _, label := range labels {
			out = append(out, PropFilter{Prop: f.prop, Op: f.test(), Value: typed(label)})
		}
	default:
		values := make([]any, len(labels))
		for i, label := range labels {
			values[i] = typed(label)
		}
		out = []PropFilter{{Prop: f.prop, Op: "in", Value: values}}
	}
	return negated(out, not)
}

func negated(filters []PropFilter, not bool) []PropFilter {
	if not {
		for i := range filters {
			filters[i] = filters[i].Negate()
		}
	}
	return filters
}

// absorb fills the field from the conditions on its prop when it can show
// them (all of one sign, each of the field's test), adding a value no row
// holds any more; false leaves them to be kept as they are.
func (f *filterField) absorb(conds []PropFilter) (value string, not, ok bool) {
	if len(conds) == 0 {
		return "", false, true
	}
	var labels []string
	for i, c := range conds {
		op := c.Op
		if op == "" {
			op = "="
		}
		o, known := filterOps[op]
		if !known || i > 0 && o.not != not {
			return "", false, false
		}
		not = o.not
		switch {
		case o.test == f.test() && (f.list || len(conds) == 1):
			labels = append(labels, fmt.Sprint(c.Value))
		case o.test == "in" && !f.list && f.kind != filterText && len(conds) == 1:
			values, err := filterValues(c.Value)
			if err != nil {
				return "", false, false
			}
			for _, v := range values {
				labels = append(labels, fmt.Sprint(v))
			}
		default:
			return "", false, false
		}
	}
	for _, label := range labels {
		if _, known := f.values[label]; !known {
			f.values[label] = label
		}
	}
	f.sortLabels()
	switch f.kind {
	case filterChips:
		// Chips keep the values' order, as toggling them does.
		slices.SortStableFunc(labels, func(a, b string) int { return slices.Index(f.labels, a) - slices.Index(f.labels, b) })
		return ChipsValue(labels), not, true
	default:
		return strings.Join(labels, ", "), not, true
	}
}

// openFilterForm opens the filter form over the list (a chooser's list
// too: it opens over the chooser and filters its rows).
func (l *List) openFilterForm() error {
	fields := l.filterFields()
	if len(fields) == 0 {
		return fmt.Errorf("list rows have no filterable props")
	}
	byProp := map[string][]PropFilter{}
	for _, c := range l.propFilters {
		byProp[c.Prop] = append(byProp[c.Prop], c)
	}
	var kept []PropFilter
	loaded := Record{}
	for _, f := range fields {
		value, not, ok := f.absorb(byProp[f.prop])
		if !ok {
			kept = append(kept, byProp[f.prop]...)
			continue
		}
		loaded[f.prop], loaded[f.prop+RecordInvertSuffix] = value, invertText(not)
	}
	for _, c := range l.propFilters {
		if !slices.ContainsFunc(fields, func(f *filterField) bool { return f.prop == c.Prop }) {
			kept = append(kept, c)
		}
	}
	var schema []RecordField
	for _, f := range fields {
		f.sortLabels()
		rf := RecordField{Name: f.prop, Invertible: true, Placeholder: f.placeholder()}
		switch f.kind {
		case filterChips:
			rf.Chips = true
			rf.Enum = slices.Clone(f.labels)
		case filterComplete:
			labels := slices.Clone(f.labels)
			rf.Completions = []CompletionTrigger{{Every: true, Boundaries: ",", Search: SearchStrings(func(_ context.Context, q string) ([]string, error) {
				return completeLabels(labels, q), nil
			})}}
		case filterText:
			rf.Multiline = f.spec.Multiline
		}
		field := f
		rf.Validate = func(v string) error {
			for _, c := range field.conditions(v, false) {
				if _, err := typedPropFilter(field.spec, c); err != nil {
					return err
				}
			}
			return nil
		}
		schema = append(schema, rf)
	}
	_, err := l.r.OpenRecordEditor(listFilterFormID, RecordSpec{
		Title:     "Filter",
		Fields:    schema,
		Clearable: true,
		Load:      func() (Record, error) { return loaded, nil },
		Save: func(w RecordWrite) Result {
			conds := slices.Clone(kept)
			for _, f := range fields {
				conds = append(conds, f.conditions(w.Values[f.prop], w.Values[f.prop+RecordInvertSuffix] == "true")...)
			}
			if err := l.SetPropFilters(conds); err != nil {
				return Failed(err)
			}
			return Done(nil)
		},
	})
	return err
}

// placeholder says what an empty text field takes.
func (f *filterField) placeholder() string {
	switch f.kind {
	case filterComplete:
		return "values, comma-separated"
	case filterText:
		return f.test() + " …"
	}
	return ""
}

// completeLabels are the labels holding q (case folded), those starting
// with it first.
func completeLabels(labels []string, q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	var first, rest []string
	for _, label := range labels {
		low := strings.ToLower(label)
		switch {
		case strings.HasPrefix(low, q):
			first = append(first, label)
		case strings.Contains(low, q):
			rest = append(rest, label)
		}
	}
	return append(first, rest...)
}
