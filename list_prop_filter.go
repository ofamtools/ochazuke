package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// PropFilters returns a copy of the list's active prop conditions.
func (l *List) PropFilters() []PropFilter { return clonePropFilters(l.propFilters) }

func (l *List) tree() *Tree {
	if inst := l.r.Instances[l.id]; inst != nil {
		return l.r.Tree(inst.Parent)
	}
	return nil
}

func (l *List) filterRows() []ListItem {
	if t := l.tree(); t != nil {
		rows := make([]ListItem, 0, len(t.nodes))
		for _, node := range t.nodes {
			noun := node.Noun
			if noun == "" {
				noun = t.RowNoun
			}
			rows = append(rows, ListItem{ID: node.ID, Label: node.Label, Noun: noun, Data: node.Data, Props: node.Props, Cells: node.Cells})
		}
		return rows
	}
	return l.all
}

func (l *List) rowNouns() []string {
	set := map[string]bool{}
	for _, item := range l.filterRows() {
		set[l.r.CanonicalNoun(item.Noun)] = true
	}
	// Empty lists may declare their row noun as their kind.
	if len(set) == 0 {
		if inst := l.r.Instances[l.id]; inst != nil {
			set[inst.Noun] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}

func rowThing(n *Noun, item ListItem) Thing {
	thing, err := ThingOf(item.Data)
	if item.Props != nil {
		thing = maps.Clone(item.Props)
		err = nil
	}
	if err != nil || thing == nil {
		thing = Thing{}
	}
	if p := n.keyProp(); p != nil {
		if _, ok := thing[p.Name]; !ok {
			thing[p.Name] = item.ID
		}
	}
	if p := n.roleProp(RoleLabel); p != nil {
		if _, ok := thing[p.Name]; !ok {
			thing[p.Name] = item.Label
		}
	}
	return thing
}

func (l *List) matchesProps(item ListItem) bool {
	if len(l.propFilters) == 0 {
		return true
	}
	n := l.r.Noun(item.Noun)
	if n == nil {
		return false
	}
	for _, f := range l.propFilters {
		p := n.prop(f.Prop)
		if p == nil || !l.r.propShown(*p, item.ID, l.r.Mode, nil) {
			return false
		}
	}
	return propsMatch(n, rowThing(n, item), l.propFilters)
}

func (l *List) validatePropFilters(filters []PropFilter) error {
	for _, f := range filters {
		found := false
		for _, noun := range l.rowNouns() {
			n := l.r.Noun(noun)
			if n == nil || n.prop(f.Prop) == nil {
				continue
			}
			if _, err := typedFilter(n, f); err != nil {
				return err
			}
			found = true
		}
		if !found {
			return fmt.Errorf("list has no prop %q", f.Prop)
		}
	}
	return nil
}

// SetPropFilters replaces prop conditions, preserving the / text query,
// marks and selection by id. Invalid updates leave the list unchanged.
// On trees, matching descendants bring their ancestors into view even
// when collapsed; clearing restores the original collapse state.
func (l *List) SetPropFilters(filters []PropFilter) error {
	if err := l.validatePropFilters(filters); err != nil {
		return err
	}
	if _, err := json.Marshal(filters); err != nil {
		return err
	}
	l.propFilters = clonePropFilters(filters)
	l.savedFilter = ""
	l.SetFilter(l.query)
	return nil
}

// SavedFilters returns the defaults and user filters that apply here.
func (l *List) SavedFilters() ([]SavedFilter, error) { return l.r.SavedFilters(l.id, l.rowNouns()...) }

// ApplyFilter applies a named saved filter, keeping the text query.
func (l *List) ApplyFilter(name string) error {
	filters, err := l.SavedFilters()
	if err != nil {
		return err
	}
	for _, f := range filters {
		if f.Name == name {
			if err := l.SetPropFilters(f.conditions()); err != nil {
				return err
			}
			l.savedFilter = name
			return nil
		}
	}
	return fmt.Errorf("unknown saved filter %q", name)
}

func (r *Runtime) namedPropFilter(n *Noun, name string) ([]PropFilter, error) {
	if name == "" {
		return nil, nil
	}
	filters, err := r.SavedFilters("", n.Name)
	if err != nil {
		return nil, err
	}
	for _, f := range filters {
		if f.Name == name {
			out := f.conditions()
			for i, c := range out {
				out[i], err = typedFilter(n, c)
				if err != nil {
					return nil, err
				}
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("unknown saved filter %q for %s", name, n.Name)
}

func (l *List) propStrip() string {
	if !l.r.Settings().Bool("list.filter-strip") {
		return ""
	}
	var parts []string
	if l.savedFilter != "" {
		parts = append(parts, "["+modalLabel(l.savedFilter)+"]")
	}
	for _, f := range l.propFilters {
		op := f.Op
		if op == "" {
			op = "="
		}
		value := fmt.Sprint(f.Value)
		if op == "in" {
			raw, _ := json.Marshal(f.Value)
			value = string(raw)
		}
		parts = append(parts, "["+modalLabel(f.Prop+" "+op+" "+value)+"]")
	}
	return strings.Join(parts, " ")
}

// filterNodes keeps matches and ancestors. Parents precede children, so
// one reverse pass propagates descendant matches without walking every
// ancestor for every node.
func (t *Tree) filterNodes() map[string]bool {
	if len(t.list.propFilters) == 0 && strings.TrimSpace(t.list.query) == "" {
		return nil
	}
	kept := map[string]bool{}
	m := t.list.matcher()
	for _, item := range t.list.filterRows() {
		if t.list.matchesProps(item) && m.MatchPrepared(filterHay(item, m)) {
			kept[item.ID] = true
		}
	}
	for i := len(t.nodes) - 1; i >= 0; i-- {
		n := t.nodes[i]
		if kept[n.ID] && n.Parent != "" {
			kept[n.Parent] = true
		}
	}
	return kept
}

func (l *List) filterProps() []string {
	set := map[string]bool{}
	for _, noun := range l.rowNouns() {
		for _, p := range l.r.Props(noun) {
			if !p.Secret && l.r.propShown(p, l.r.Focus, l.r.Mode, nil) {
				set[p.Name] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(set))
}

func (l *List) pickPropFilter(prop string) error {
	if prop == "" {
		rows := []ChooserRow{}
		for _, name := range l.filterProps() {
			rows = append(rows, ChooserRow{ID: name, Label: name})
		}
		if len(rows) == 0 {
			return errors.New("list rows have no filterable props")
		}
		_, err := l.r.OpenChooser(ChooserSpec{Kind: "list-filter-prop", Title: "Filter by prop", Rows: rows, Choose: func(row ChooserRow) Result {
			if err := l.pickPropFilter(row.ID); err != nil {
				return Failed(err)
			}
			return Done(nil)
		}})
		return err
	}
	// Values come from the rows visible before the picker opened, not from
	// the enum or the store. Raw typed values travel in Data unchanged.
	values := map[string]any{}
	for _, item := range l.items {
		n := l.r.Noun(item.Noun)
		if n == nil {
			continue
		}
		p := n.prop(prop)
		if p == nil || p.Secret || !l.r.propShown(*p, item.ID, l.r.Mode, nil) {
			continue
		}
		value, ok := rowThing(n, item)[prop]
		if !ok {
			continue
		}
		typed, err := propValue(*p, value)
		if err != nil {
			continue
		}
		raw, _ := json.Marshal(fileForm(typed))
		values[string(raw)] = typed
	}
	rows := []ChooserRow{}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		rows = append(rows, ChooserRow{ID: key, Label: fmt.Sprint(values[key]), Data: values[key]})
	}
	if len(rows) == 0 {
		return fmt.Errorf("no visible values for prop %q", prop)
	}
	_, err := l.r.OpenChooser(ChooserSpec{Kind: "list-filter-value", Title: "Filter · " + prop, Rows: rows, Choose: func(row ChooserRow) Result {
		filters := append(l.PropFilters(), PropFilter{Prop: prop, Op: "=", Value: row.Data})
		if err := l.SetPropFilters(filters); err != nil {
			return Failed(err)
		}
		return Done(nil)
	}})
	return err
}

func registerListPropFilter(r *Runtime) {
	resolve := func(c Context, a Arguments) (*List, error) {
		if id := StringArg(a, "id", ""); id != "" {
			if l := r.List(id); l != nil {
				return l, nil
			}
			if t := r.Tree(id); t != nil {
				return t.list, nil
			}
			return nil, fmt.Errorf("unknown list %q", id)
		}
		if l := r.focusedList(focusOf(c)); l != nil {
			return l, nil
		}
		return nil, errors.New("no focused list")
	}
	guardList := func(ev KeyEvent) *List {
		if id := StringArg(ev.Args, "id", ""); id != "" {
			if l := r.List(id); l != nil {
				return l
			}
			if t := r.Tree(id); t != nil {
				return t.list
			}
			return nil
		}
		return r.focusedList(ev.Focus)
	}
	r.DefineGuard("list-prop-conditions", func(_ *Runtime, ev KeyEvent) bool { l := guardList(ev); return l != nil && len(l.propFilters) > 0 })
	r.DefineGuard("list-props", func(r *Runtime, ev KeyEvent) bool {
		l := guardList(ev)
		return l != nil && len(l.filterProps()) > 0
	})
	r.DefineGuard("list-prop-filtered", func(r *Runtime, ev KeyEvent) bool {
		l := guardList(ev)
		return l != nil && (len(l.propFilters) > 0 || l.savedFilter != "")
	})
	r.DefineGuard("list-saved-filters", func(r *Runtime, ev KeyEvent) bool {
		l := guardList(ev)
		if l == nil || !r.Settings().Bool("list.filter-cycle") {
			return false
		}
		filters, err := l.SavedFilters()
		return err == nil && len(filters) > 0
	})
	for _, spec := range []struct {
		verb, description, guard string
		params                   map[string]Parameter
		run                      func(*List, Arguments) error
	}{
		{"filter-prop", "Add a prop condition (AND); omit prop or value to pick from visible rows", "list-props", map[string]Parameter{"prop": {Type: "string"}, "op": {Type: "string", Enum: []string{"=", "!=", "contains", "in"}}, "value": {Type: "any", Description: "Typed value; in takes an array"}}, func(l *List, a Arguments) error {
			prop := StringArg(a, "prop", "")
			value, ok := a["value"]
			if prop == "" || !ok {
				return l.pickPropFilter(prop)
			}
			return l.SetPropFilters(append(l.PropFilters(), PropFilter{Prop: prop, Op: StringArg(a, "op", "="), Value: value}))
		}},
		{"filter-prop-clear", "Remove one prop condition; omit prop and index to pick an active condition", "list-prop-conditions", map[string]Parameter{"prop": {Type: "string"}, "index": {Type: "integer", Description: "1-based condition index"}}, func(l *List, a Arguments) error {
			clear := func(i int) error {
				filters := l.PropFilters()
				if i < 0 || i >= len(filters) {
					return errors.New("no such prop condition")
				}
				return l.SetPropFilters(slices.Delete(filters, i, i+1))
			}
			if i := IntArg(a, "index", 0); i != 0 {
				return clear(i - 1)
			}
			if prop := StringArg(a, "prop", ""); prop != "" {
				return clear(slices.IndexFunc(l.propFilters, func(f PropFilter) bool { return f.Prop == prop }))
			}
			rows := []ChooserRow{}
			for i, f := range l.propFilters {
				rows = append(rows, ChooserRow{ID: strconv.Itoa(i), Label: fmt.Sprintf("%s %s %v", f.Prop, f.Op, f.Value), Data: i})
			}
			_, err := r.OpenChooser(ChooserSpec{Kind: "list-filter-remove", Title: "Clear prop filter", Rows: rows, Choose: func(row ChooserRow) Result {
				if err := clear(row.Data.(int)); err != nil {
					return Failed(err)
				}
				return Done(nil)
			}})
			return err
		}},
		{"filter-props-clear", "Clear every prop condition and saved filter name, keeping / text", "list-prop-filtered", nil, func(l *List, _ Arguments) error { return l.SetPropFilters(nil) }},
		{"filter-saved", "Apply a saved prop filter by name, keeping / text", "", map[string]Parameter{"name": {Type: "string", Required: true}}, func(l *List, a Arguments) error { return l.ApplyFilter(StringArg(a, "name", "")) }},
		{"filter-cycle", "Cycle the saved prop filters declared for this list or its row nouns", "list-saved-filters", map[string]Parameter{"step": {Type: "integer"}}, func(l *List, a Arguments) error {
			filters, err := l.SavedFilters()
			if err != nil {
				return err
			}
			if len(filters) == 0 {
				return errors.New("no saved filters")
			}
			i := slices.IndexFunc(filters, func(f SavedFilter) bool { return f.Name == l.savedFilter })
			step := IntArg(a, "step", 1)
			if i < 0 {
				if step < 0 {
					i = 0
				} else {
					i = -1
				}
			}
			i = ((i+step)%len(filters) + len(filters)) % len(filters)
			return l.ApplyFilter(filters[i].Name)
		}},
	} {
		params := cloneParams(spec.params)
		if params == nil {
			params = map[string]Parameter{}
		}
		params["id"] = Parameter{Type: "string", Description: "List or tree id (default focused)"}
		r.Register(Operation{Noun: "ochazuke.list", Verb: spec.verb, Description: spec.description, Parameters: params, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
			l, err := resolve(c, a)
			if err != nil {
				return nil, err
			}
			if err := spec.run(l, a); err != nil {
				return nil, err
			}
			return l.state(), nil
		}})
		if spec.guard != "" {
			r.Nouns["ochazuke.list"].Guards[spec.verb] = spec.guard
		}
	}
}
