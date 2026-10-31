package ochazuke

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// PropFilter is one typed condition on a noun's props. Conditions combine
// with AND. Op is = (also the default), !=, contains (literal substring),
// or in (Value is an array of alternatives). Equality uses the same typed
// comparison as the standard list verb's --where.
type PropFilter struct {
	Prop  string `json:"prop"`
	Op    string `json:"op,omitempty"`
	Value any    `json:"value"`
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
	if p.Secret {
		return f, fmt.Errorf("cannot filter secret prop %q", f.Prop)
	}
	if f.Op == "" {
		f.Op = "="
	}
	switch f.Op {
	case "=", "!=":
		v, err := propValue(*p, f.Value)
		if err != nil {
			return f, fmt.Errorf("filter %s: %w", f.Prop, err)
		}
		f.Value = v
	case "contains":
		if p.Type != "" && p.Type != "string" {
			return f, fmt.Errorf("contains needs a string prop: %s", f.Prop)
		}
		if _, ok := f.Value.(string); !ok {
			return f, fmt.Errorf("contains needs a string value")
		}
	case "in":
		values, err := filterValues(f.Value)
		if err != nil {
			return f, err
		}
		for i, value := range values {
			values[i], err = propValue(*p, value)
			if err != nil {
				return f, fmt.Errorf("filter %s: %w", f.Prop, err)
			}
		}
		f.Value = values
	default:
		return f, fmt.Errorf("unknown filter operator %q", f.Op)
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

func propMatches(p PropSpec, value any, f PropFilter) bool {
	switch f.Op {
	case "", "=":
		return sameProp(p, value, f.Value)
	case "!=":
		return !sameProp(p, value, f.Value)
	case "contains":
		text, ok := value.(string)
		want, valid := f.Value.(string)
		return ok && valid && strings.Contains(text, want)
	case "in":
		values, err := filterValues(f.Value)
		return err == nil && slices.ContainsFunc(values, func(want any) bool { return sameProp(p, value, want) })
	}
	return false
}

// SavedFilter is a named AND of conditions. Where is the convenient
// equality form, matching --where; Conditions supplies other operators.
// List scopes a user row to one list id, Noun to every list of those rows.
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

// Filters declares app defaults for every list/tree of this noun's rows.
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
		for _, c := range f.conditions() {
			if _, err := typedFilter(b.n, c); err != nil {
				panic(err)
			}
		}
		b.n.Filters = append(b.n.Filters, cloneSavedFilter(f))
	}
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
