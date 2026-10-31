package ochazuke

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
)

// Every field of a list's rows is a filterable prop unless it is turned
// off. A field the row noun declares (PropSpec) keeps its declaration:
// label, type, enum, guard. Any other top-level field of the rows' data
// (ListItem.Props, else ThingOf(Data)) is a derived prop: its type is read
// from the values (string, number, boolean, list of strings; values of
// mixed kinds compare as their text), it is ReadOnly and Derived, and it
// lives only here, never on the noun, so nothing can write it. Objects are
// not filterable (no nested fields: a prop name has no dot); nulls say
// nothing about the type.
//
// Off only explicitly: a Secret prop is never offered; a declared prop
// with NoFilter is not; a noun's PropsOff (NounBuilder.PropsOff, the
// catalog's propsOff) names fields of its rows; the list.props-off setting
// names NOUN.FIELD patterns (path.Match: o-lane.lane.*, *.uuid).
//
// The derived set is read lazily, the first time a picker, a guard or a
// filter asks, and kept until the rows change (List.cacheRows clears it).

// PropsOffSetting lists NOUN.FIELD patterns prop filters never offer.
const PropsOffSetting = "list.props-off"

// propValueCap bounds the distinct values a prop's value picker offers.
const propValueCap = 500

// fieldKind is the prop type a row value suggests, "" for one that says
// nothing (null) and "-" for one that is not filterable (an object).
func fieldKind(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string, time.Time:
		return SettingString
	case bool:
		return SettingBoolean
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return SettingNumber
	case []string:
		return SettingList
	case []any:
		for _, item := range t {
			if _, ok := item.(string); !ok {
				return "-"
			}
		}
		return SettingList
	case map[string]any, Thing, Arguments, Record:
		return "-"
	}
	return "-"
}

// looseText is a scalar as text: a derived prop of mixed kinds compares
// its values this way.
func looseText(v any) any {
	switch t := v.(type) {
	case nil, string:
		return v
	case time.Time:
		return t.Format(time.RFC3339Nano)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

// fieldProps are the derived props of a list's rows by noun and field.
type fieldProps map[string]map[string]PropSpec

// filterRows are every row a filter reads: a tree's nodes, collapsed or
// not, else the list's rows as SetItems gave them.
func (l *List) filterRows() []ListItem {
	if t := l.tree(); t != nil {
		out := make([]ListItem, 0, len(t.nodes))
		for _, n := range t.nodes {
			if row := t.rowCache[n.ID]; row != nil {
				out = append(out, row.item)
			}
		}
		return out
	}
	return l.all
}

// fieldProps reads (once per row generation) the fields of the rows that
// their nouns do not declare.
func (l *List) fieldProps() fieldProps {
	if l.derived != nil {
		return l.derived
	}
	out := fieldProps{}
	kinds := map[string]map[string]string{}
	for _, item := range l.filterRows() {
		noun := l.r.CanonicalNoun(item.Noun)
		n := l.r.Noun(noun)
		if n == nil {
			continue
		}
		for name, value := range l.thing(item) {
			if n.prop(name) != nil || !propName(name) {
				continue
			}
			kind := fieldKind(value)
			if kind == "" {
				if kinds[noun] == nil {
					kinds[noun] = map[string]string{}
				}
				if _, seen := kinds[noun][name]; !seen {
					kinds[noun][name] = ""
				}
				continue
			}
			if kinds[noun] == nil {
				kinds[noun] = map[string]string{}
			}
			switch old, seen := kinds[noun][name]; {
			case !seen || old == "":
				kinds[noun][name] = kind
			case old != kind && old != "-" && kind != "-":
				kinds[noun][name] = "mixed"
			case kind == "-":
				kinds[noun][name] = "-"
			}
		}
	}
	for noun, fields := range kinds {
		for name, kind := range fields {
			if kind == "-" {
				continue
			}
			p := PropSpec{Name: name, ReadOnly: true, Derived: true}
			p.Description = "read from the rows"
			switch kind {
			case "", "mixed":
				p.Type, p.loose = SettingString, true
			default:
				p.Type = kind
			}
			if out[noun] == nil {
				out[noun] = map[string]PropSpec{}
			}
			out[noun][name] = p
		}
	}
	l.derived = out
	return out
}

// propName reports whether a row field can name a prop: one word, no dot.
func propName(name string) bool {
	return name != "" && !strings.ContainsAny(name, " \t\n=\".,") && !strings.HasPrefix(name, "-")
}

// filterProp is the prop a filter on this list's rows of n reads under
// that name: the declared one, else the derived one; nil when there is
// none or it is turned off here.
func (l *List) filterProp(n *Noun, name string) *PropSpec {
	if n == nil {
		return nil
	}
	p := n.prop(name)
	if p == nil {
		d, ok := l.fieldProps()[n.Name][name]
		if !ok {
			return nil
		}
		p = &d
	}
	if !l.r.propFilterable(n, *p, l.propFocus()) {
		return nil
	}
	return p
}

// propFilterable reports whether a prop of n is offered to filters at the
// focus: not Secret, not NoFilter, not in the noun's PropsOff or the
// list.props-off setting, and its guard holds.
func (r *Runtime) propFilterable(n *Noun, p PropSpec, focus string) bool {
	if p.Secret || p.NoFilter || r.propOff(n, p.Name) {
		return false
	}
	return r.propShown(p, focus, r.Mode, nil)
}

// propOff reports whether the noun's PropsOff or list.props-off names the
// field.
func (r *Runtime) propOff(n *Noun, field string) bool {
	for _, pattern := range n.PropsOff {
		if ok, _ := path.Match(pattern, field); ok || pattern == field {
			return true
		}
	}
	patterns := r.Settings().Strings(PropsOffSetting)
	if len(patterns) == 0 {
		return false
	}
	names := []string{n.Name}
	if short := r.DisplayNoun(n.Name); short != n.Name {
		names = append(names, short)
	}
	for _, pattern := range patterns {
		for _, noun := range names {
			key := noun + "." + field
			if ok, _ := path.Match(pattern, key); ok || pattern == key {
				return true
			}
		}
	}
	return false
}

// filterSpecs are the props the list's rows offer to filters by name: a
// declared prop where one of the row nouns declares it, else the derived
// one.
func (l *List) filterSpecs() map[string]PropSpec {
	out := map[string]PropSpec{}
	for _, noun := range l.rowNouns() {
		n := l.r.Noun(noun)
		if n == nil {
			continue
		}
		for _, p := range n.Props {
			if l.r.propFilterable(n, p, l.propFocus()) {
				if old, ok := out[p.Name]; !ok || old.Derived {
					out[p.Name] = p
				}
			}
		}
		for _, name := range sortedKeys(l.fieldProps()[n.Name]) {
			if _, ok := out[name]; ok {
				continue
			}
			if p := l.filterProp(n, name); p != nil {
				out[name] = *p
			}
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
