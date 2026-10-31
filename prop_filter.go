package ochazuke

import (
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
)

// PropFilter is one typed condition on a noun's props. Conditions combine
// with AND. Op is = (also the default), contains (literal substring, or an
// item of a list prop), in (Value is an array of alternatives), or one of
// their negations: !=, not-contains, not-in (filterOps). Equality uses the
// same typed comparison as the standard list verb's --where. A row without
// the prop matches no condition, a negated one included.
type PropFilter struct {
	Prop  string `json:"prop"`
	Op    string `json:"op,omitempty"`
	Value any    `json:"value"`
}

// filterOp is one operator: the test it runs and whether it turns that
// test over. A negation is a property of the operator, so every test has
// one (!= is = turned over) and no operator has a code path of its own.
type filterOp struct {
	test  string
	not   bool
	label string // how a filter chip spells it
}

// FilterOps names every prop filter operator, in the order the verb's
// parameter lists them.
var FilterOps = []string{"=", "!=", "contains", "not-contains", "in", "not-in"}

var filterOps = map[string]filterOp{
	"=":            {test: "=", label: "="},
	"!=":           {test: "=", not: true, label: "!="},
	"contains":     {test: "contains", label: "contains"},
	"not-contains": {test: "contains", not: true, label: "not contains"},
	"in":           {test: "in", label: "in"},
	"not-in":       {test: "in", not: true, label: "not in"},
}

// NegateFilterOp is op turned over (= and !=, in and not-in, contains and
// not-contains); an unknown op comes back unchanged.
func NegateFilterOp(op string) string {
	if op == "" {
		op = "="
	}
	o, ok := filterOps[op]
	if !ok {
		return op
	}
	for _, name := range FilterOps {
		if other := filterOps[name]; other.test == o.test && other.not != o.not {
			return name
		}
	}
	return op
}

// Negate is the condition turned over.
func (f PropFilter) Negate() PropFilter {
	f.Op = NegateFilterOp(f.Op)
	return f
}

// Label is the condition as a filter chip spells it: status = done,
// status not in ["done","cancelled"].
func (f PropFilter) Label() string {
	op := f.Op
	if op == "" {
		op = "="
	}
	o, ok := filterOps[op]
	if !ok {
		return f.Prop + " " + op + " " + fmt.Sprint(f.Value)
	}
	value := fmt.Sprint(f.Value)
	if o.test == "in" {
		raw, _ := json.Marshal(f.Value)
		value = string(raw)
	}
	return f.Prop + " " + o.label + " " + value
}

func whereFilters(where map[string]any) []PropFilter {
	out := make([]PropFilter, 0, len(where))
	for _, name := range slices.Sorted(maps.Keys(where)) {
		out = append(out, PropFilter{Prop: name, Op: "=", Value: where[name]})
	}
	return out
}

func filterValues(value any) ([]any, error) {
	if values, ok := value.([]any); ok && values != nil {
		return values, nil
	}
	if word, ok := value.(string); ok {
		if err := json.Unmarshal([]byte(word), &value); err != nil {
			return nil, fmt.Errorf("in takes a JSON array: %w", err)
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var values []any
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, fmt.Errorf("in takes an array of values")
	}
	return values, nil
}

func typedFilter(n *Noun, f PropFilter) (PropFilter, error) {
	p := n.prop(f.Prop)
	if p == nil {
		return f, fmt.Errorf("%s has no prop %q", n.Name, f.Prop)
	}
	return typedPropFilter(*p, f)
}

// typedPropFilter checks and types a condition on one prop, declared or
// derived from the rows (list_props.go).
func typedPropFilter(p PropSpec, f PropFilter) (PropFilter, error) {
	if p.Secret {
		return f, fmt.Errorf("cannot filter secret prop %q", f.Prop)
	}
	if f.Op == "" {
		f.Op = "="
	}
	o, ok := filterOps[f.Op]
	if !ok {
		return f, fmt.Errorf("unknown filter operator %q", f.Op)
	}
	switch o.test {
	case "=":
		v, err := propValue(p, f.Value)
		if err != nil {
			return f, fmt.Errorf("filter %s: %w", f.Prop, err)
		}
		f.Value = v
	case "contains":
		if p.Type != "" && p.Type != SettingString && p.Type != SettingList {
			return f, fmt.Errorf("%s needs a string or list prop: %s", f.Op, f.Prop)
		}
		if _, ok := f.Value.(string); !ok {
			return f, fmt.Errorf("%s needs a string value", f.Op)
		}
	case "in":
		values, err := filterValues(f.Value)
		if err != nil {
			return f, err
		}
		for i, value := range values {
			values[i], err = propValue(p, value)
			if err != nil {
				return f, fmt.Errorf("filter %s: %w", f.Prop, err)
			}
		}
		f.Value = values
	}
	return f, nil
}

// propsMatch is shared by --where, saved CLI filters and live list/tree
// filters. Callers validate conditions before applying them.
func propsMatch(n *Noun, t Thing, filters []PropFilter) bool {
	for _, f := range filters {
		p := n.prop(f.Prop)
		_, present := t[f.Prop]
		if p == nil || !present || !propMatches(*p, t[f.Prop], f) {
			return false
		}
	}
	return true
}

// propMatches runs the condition's test and turns it over for a negated
// operator; an unknown operator matches nothing.
func propMatches(p PropSpec, value any, f PropFilter) bool {
	op := f.Op
	if op == "" {
		op = "="
	}
	o, ok := filterOps[op]
	if !ok {
		return false
	}
	return propTest(p, value, o.test, f.Value) != o.not
}

func propTest(p PropSpec, value any, test string, want any) bool {
	switch test {
	case "=":
		return sameProp(p, value, want)
	case "contains":
		sub, valid := want.(string)
		if !valid {
			return false
		}
		if p.Type == SettingList {
			items, err := coerceList(value)
			return err == nil && slices.Contains(items.([]string), sub)
		}
		if p.loose {
			value = looseText(value)
		}
		text, ok := value.(string)
		return ok && strings.Contains(text, sub)
	case "in":
		values, err := filterValues(want)
		return err == nil && slices.ContainsFunc(values, func(one any) bool { return sameProp(p, value, one) })
	}
	return false
}

// SavedFilter is a named AND of conditions. Where is the convenient
// equality form, matching --where; Conditions supplies other operators.
// List scopes a row to one list/tree id or chooser kind, Noun to every row view of those rows.
// A list-specific row wins over a noun row with the same name.
type SavedFilter struct {
	Name       string         `json:"name"`
	List       string         `json:"list,omitempty"`
	Noun       string         `json:"noun,omitempty"`
	Where      map[string]any `json:"where,omitempty"`
	Conditions []PropFilter   `json:"conditions,omitempty"`
}

func (f SavedFilter) conditions() []PropFilter {
	return append(whereFilters(f.Where), f.Conditions...)
}

// Filters declares app defaults for every list/tree/chooser of this noun's rows.
// Catalogs carry them to hosts; list.filters in settings.json adds or
// replaces defaults by scope and name. An empty filter represents "all".
func (b *NounBuilder) Filters(filters ...SavedFilter) *NounBuilder {
	for _, f := range filters {
		if strings.TrimSpace(f.Name) == "" {
			panic("saved filter needs a name")
		}
		if _, err := json.Marshal(f); err != nil {
			panic(fmt.Errorf("saved filter %s: %w", f.Name, err))
		}
		f.Noun = b.n.Name
		// A condition on a declared prop is checked now; one on a field the
		// rows carry (list_props.go) when a list applies it.
		for _, c := range f.conditions() {
			if b.n.prop(c.Prop) == nil {
				continue
			}
			if _, err := typedFilter(b.n, c); err != nil {
				panic(err)
			}
		}
		b.n.Filters = append(b.n.Filters, cloneSavedFilter(f))
	}
	b.last = ""
	return b
}

// PropsOff names fields of the noun's rows that prop filters never offer: a
// field name or a path.Match pattern over it (list_props.go). Every other
// field of the rows is filterable, declared as a prop or not.
func (b *NounBuilder) PropsOff(fields ...string) *NounBuilder {
	for _, f := range fields {
		if _, err := path.Match(f, ""); err != nil || strings.TrimSpace(f) == "" {
			panic(fmt.Errorf("props off %q: not a field pattern", f))
		}
	}
	b.n.PropsOff = append(b.n.PropsOff, fields...)
	b.last = ""
	return b
}

func cloneFilterValue(value any) any {
	switch v := value.(type) {
	case Thing:
		return cloneFilterValue(map[string]any(v))
	case Arguments:
		return cloneFilterValue(map[string]any(v))
	case map[string]any:
		out := maps.Clone(v)
		for key, value := range out {
			out[key] = cloneFilterValue(value)
		}
		return out
	case []any:
		out := slices.Clone(v)
		for i, value := range out {
			out[i] = cloneFilterValue(value)
		}
		return out
	default:
		return fileForm(value)
	}
}

func clonePropFilters(filters []PropFilter) []PropFilter {
	out := slices.Clone(filters)
	for i := range out {
		out[i].Value = cloneFilterValue(out[i].Value)
	}
	return out
}

func cloneSavedFilter(f SavedFilter) SavedFilter {
	f.Where = maps.Clone(f.Where)
	for name, value := range f.Where {
		f.Where[name] = cloneFilterValue(value)
	}
	f.Conditions = clonePropFilters(f.Conditions)
	return f
}

// SavedFilters resolves the app defaults and the user's list.filters
// setting (a JSON array string), in declaration order. The same name in a
// more specific scope replaces the less specific row without moving it.
// CLI callers pass an empty list id, so only noun-scoped rows apply.
func (r *Runtime) SavedFilters(list string, nouns ...string) ([]SavedFilter, error) {
	wanted := map[string]bool{}
	for _, noun := range nouns {
		wanted[r.CanonicalNoun(noun)] = true
	}
	var candidates []SavedFilter
	for _, noun := range slices.Sorted(maps.Keys(wanted)) {
		if n := r.Noun(noun); n != nil {
			candidates = append(candidates, n.Filters...)
		}
	}
	user, err := r.Settings().savedFilters()
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, user...)
	out := []SavedFilter{}
	specificity := map[string]int{}
	for _, f := range candidates {
		rank := 1
		if f.List != "" {
			if list == "" || f.List != list {
				continue
			}
			rank = 2
		} else if !wanted[r.CanonicalNoun(f.Noun)] {
			continue
		}
		if strings.TrimSpace(f.Name) == "" {
			return nil, fmt.Errorf("list.filters: a saved filter needs a name")
		}
		i := slices.IndexFunc(out, func(old SavedFilter) bool { return old.Name == f.Name })
		if i < 0 {
			out = append(out, cloneSavedFilter(f))
			specificity[f.Name] = rank
		} else if rank >= specificity[f.Name] {
			out[i] = cloneSavedFilter(f)
			specificity[f.Name] = rank
		}
	}
	return out, nil
}

// savedFilterCache belongs to Settings, alongside the source value.
type savedFilterCache struct {
	text   string
	rows   []SavedFilter
	err    error
	loaded bool
}

func parseSavedFilters(text string) ([]SavedFilter, error) {
	var rows []SavedFilter
	if err := json.Unmarshal([]byte(text), &rows); err != nil {
		return nil, fmt.Errorf("list.filters must be a JSON array of saved filters: %w", err)
	}
	if rows == nil {
		return nil, fmt.Errorf("list.filters must be a JSON array, use [] for no filters")
	}
	for _, f := range rows {
		if strings.TrimSpace(f.Name) == "" {
			return nil, fmt.Errorf("list.filters: a saved filter needs a name")
		}
		if f.List == "" && f.Noun == "" {
			return nil, fmt.Errorf("list.filters: %s needs a noun or list scope", f.Name)
		}
	}
	return rows, nil
}

func validSavedFilters(value any) error {
	_, err := parseSavedFilters(value.(string))
	return err
}

func (s *Settings) savedFilters() ([]SavedFilter, error) {
	text := s.String("list.filters")
	if !s.filterCache.loaded || text != s.filterCache.text {
		rows, err := parseSavedFilters(text)
		s.filterCache = savedFilterCache{text: text, rows: rows, err: err, loaded: true}
	}
	return s.filterCache.rows, s.filterCache.err
}
