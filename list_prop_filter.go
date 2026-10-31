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
	if inst := l.r.Instances[l.id]; inst != nil && inst.Component == l {
		return l.r.Tree(inst.Parent)
	}
	return nil
}

// filterRow caches the expensive row decoding and prepared search text until
// SetItems (or a tree node change). Tree display prefixes are not searchable.
type filterRow struct {
	item     ListItem
	node     TreeNode
	thing    Thing
	decoded  bool
	hay      [2]string
	prepared [2]bool
}

func (l *List) treeHasNode(id string) bool {
	t := l.tree()
	return t != nil && t.index(id) >= 0
}

func (l *List) cacheRows() {
	l.rowCache = make(map[string]*filterRow, len(l.all))
	l.derived = nil
	set := map[string]bool{}
	if t := l.tree(); t != nil {
		for _, n := range t.nodes {
			row := t.rowCache[n.ID]
			if row != nil {
				set[l.r.CanonicalNoun(row.item.Noun)] = true
			}
		}
	}
	for _, item := range l.all {
		l.rowCache[item.ID] = &filterRow{item: item}
		set[l.r.CanonicalNoun(item.Noun)] = true
	}
	if len(set) == 0 {
		if inst := l.r.Instances[l.id]; inst != nil {
			set[inst.Noun] = true
		}
	}
	l.nouns = slices.Sorted(maps.Keys(set))
}

func (l *List) cachedRow(item ListItem) *filterRow {
	if t := l.tree(); t != nil {
		if row := t.rowCache[item.ID]; row != nil {
			return row
		}
	}
	return l.rowCache[item.ID]
}

func (l *List) preparedRow(item ListItem, m Matcher) string {
	row := l.cachedRow(item)
	if row == nil {
		return filterHay(item, m)
	}
	i := 0
	if m.Fold() {
		i = 1
	}
	if !row.prepared[i] {
		row.hay[i], row.prepared[i] = filterHay(row.item, m), true
	}
	return row.hay[i]
}

func (l *List) thing(item ListItem) Thing {
	row := l.cachedRow(item)
	if row == nil {
		return l.cellThing(item, rowThing(l.r.Noun(item.Noun), item))
	}
	if !row.decoded {
		row.thing, row.decoded = l.cellThing(item, rowThing(l.r.Noun(item.Noun), item)), true
	}
	return row.thing
}

// cellThing gives a table row that carries no data (no Props, no Data) a
// field per titled column: the title as a prop name (lower case, spaces as
// dashes), the cell as its text, so its columns filter like fields.
func (l *List) cellThing(item ListItem, t Thing) Thing {
	if item.Props != nil || item.Data != nil || len(l.columns) == 0 {
		return t
	}
	for i, col := range l.columns {
		name := columnField(col.Title)
		if i >= len(item.Cells) || name == "" || !propName(name) {
			continue
		}
		if _, ok := t[name]; !ok {
			t[name] = item.Cells[i]
		}
	}
	return t
}

// columnField is a column title as a field name: trimmed, lower case,
// spaces as dashes.
func columnField(title string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(title)), " ", "-")
}

func (l *List) rowNouns() []string { return l.nouns }

// A stable list focus is shared by matching and both picker stages,
// including while a chooser temporarily owns the runtime focus.
func (l *List) propFocus() string {
	if l.filterFocus != "" {
		return l.filterFocus
	}
	return l.id
}

func rowThing(n *Noun, item ListItem) Thing {
	var thing Thing
	var err error
	if item.Props != nil {
		thing = maps.Clone(item.Props)
	} else {
		thing, err = ThingOf(item.Data)
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
	t := l.thing(item)
	for _, f := range l.propFilters {
		p := l.filterProp(n, f.Prop)
		if p == nil {
			return false
		}
		value, present := t[f.Prop]
		if !present || !propMatches(*p, value, f) {
			return false
		}
	}
	return true
}

func (l *List) validatePropFilters(filters []PropFilter) error {
	for _, f := range filters {
		found := false
		for _, noun := range l.rowNouns() {
			n := l.r.Noun(noun)
			p := l.filterProp(n, f.Prop)
			if p == nil {
				continue
			}
			if _, err := typedPropFilter(*p, f); err != nil {
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
	l.linger.reset()
	l.propFilters = clonePropFilters(filters)
	l.savedFilter = ""
	if l.filterChanged != nil {
		l.filterChanged()
		return nil
	}
	if t := l.tree(); t != nil {
		t.filterCollapsed = nil
		return t.refresh("")
	}
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

// SaveFilter writes the current conditions as a list-scoped user definition.
// Existing definitions in other scopes keep their order and content.
func (l *List) SaveFilter(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("saved filter needs a name")
	}
	user, err := l.r.Settings().savedFilters()
	if err != nil {
		return err
	}
	user = slices.Clone(user)
	f := SavedFilter{Name: name, List: l.id, Conditions: l.PropFilters()}
	i := slices.IndexFunc(user, func(old SavedFilter) bool { return old.Name == name && old.List == l.id })
	if i < 0 {
		user = append(user, f)
	} else {
		user[i] = f
	}
	raw, err := json.Marshal(user)
	if err != nil {
		return err
	}
	if err := l.r.Settings().Set("list.filters", string(raw)); err != nil {
		return err
	}
	l.savedFilter = name
	return nil
}

// DeleteSavedFilter removes the effective user definition by name. App
// defaults remain declarations; removing a user override reveals that default.
func (l *List) DeleteSavedFilter(name string) error {
	filters, err := l.SavedFilters()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(filters, func(f SavedFilter) bool { return f.Name == name })
	if i < 0 {
		return fmt.Errorf("unknown saved filter %q", name)
	}
	f := filters[i]
	user, err := l.r.Settings().savedFilters()
	if err != nil {
		return err
	}
	i = slices.IndexFunc(user, func(old SavedFilter) bool { return old.Name == f.Name && old.List == f.List && old.Noun == f.Noun })
	if i < 0 {
		return fmt.Errorf("%q is an app default; only user saved filters can be deleted", name)
	}
	user = slices.Delete(slices.Clone(user), i, i+1)
	raw, err := json.Marshal(user)
	if err != nil {
		return err
	}
	if err := l.r.Settings().Set("list.filters", string(raw)); err != nil {
		return err
	}
	if l.savedFilter == name {
		l.savedFilter = ""
	}
	return nil
}

func (l *List) pickSavedFilter() error {
	filters, err := l.SavedFilters()
	if err != nil {
		return err
	}
	rows := []ChooserRow{}
	for _, f := range filters {
		rows = append(rows, ChooserRow{ID: f.Name, Label: f.Name, Data: f})
	}
	_, err = l.r.OpenChooser(ChooserSpec{Kind: "list-filter-saved", Title: "Saved filters", Rows: rows, Choose: func(row ChooserRow) Result {
		if err := l.ApplyFilter(row.ID); err != nil {
			return Failed(err)
		}
		return Done(nil)
	}})
	return err
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
		parts = append(parts, "["+modalLabel(f.Label())+"]")
	}
	return strings.Join(parts, " ")
}

// filterNodes keeps matches and ancestors. Parents precede children, so
// one reverse pass propagates descendant matches without walking every
// ancestor for every node.
func (t *Tree) filterNodes() map[string]bool {
	kept := map[string]bool{}
	m := t.list.matcher()
	for _, n := range t.nodes {
		item := t.rowCache[n.ID].item
		if t.list.matchesProps(item) && m.MatchPrepared(t.list.preparedRow(item, m)) {
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

func (l *List) filterProps() []string { return sortedKeys(l.filterSpecs()) }

// pickPropFilter opens the filter form (prop empty, list.filter-form on;
// list_filter_form.go), else the prop chooser (prop empty), then the value
// chooser; not turns the picked condition over (!=, not-contains).
func (l *List) pickPropFilter(prop string, offerClear, not bool) error {
	if prop == "" && l.r.Settings().Bool(ListFilterFormSetting) {
		return l.openFilterForm()
	}
	if prop == "" {
		rows := []ChooserRow{}
		for _, name := range l.filterProps() {
			rows = append(rows, ChooserRow{ID: name, Label: name})
		}
		if offerClear && (len(l.propFilters) > 0 || l.savedFilter != "") {
			rows = append(rows, ChooserRow{Label: "Clear prop filters", Fixed: true})
		}
		if len(rows) == 0 {
			return errors.New("list rows have no filterable props")
		}
		_, err := l.r.OpenChooser(ChooserSpec{Kind: "list-filter-prop", Title: "Filter by prop", Rows: rows, Choose: func(row ChooserRow) Result {
			if row.ID == "" {
				if err := l.SetPropFilters(nil); err != nil {
					return Failed(err)
				}
				return Done(nil)
			}
			if err := l.pickPropFilter(row.ID, offerClear, not); err != nil {
				return Failed(err)
			}
			return Done(nil)
		}})
		return err
	}
	// Values come from the rows visible before the picker opened, not from
	// the enum or the store. Raw typed values travel in Data unchanged.
	// A list prop offers its items, and its condition is contains.
	values := map[string]any{}
	op := "="
	for _, item := range l.items {
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
		each := []any{typed}
		if items, ok := typed.([]string); ok {
			op, each = "contains", nil
			for _, one := range items {
				each = append(each, one)
			}
		}
		for _, one := range each {
			raw, _ := json.Marshal(fileForm(one))
			values[string(raw)] = one
		}
	}
	rows := []ChooserRow{}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		if len(rows) == propValueCap {
			break
		}
		rows = append(rows, ChooserRow{ID: key, Label: fmt.Sprint(values[key]), Data: values[key]})
	}
	if len(rows) == 0 {
		return fmt.Errorf("no visible values for prop %q", prop)
	}
	title := "Filter · " + prop
	if not {
		title += " · not"
	}
	_, err := l.r.OpenChooser(ChooserSpec{Kind: "list-filter-value", Title: title, Rows: rows, Choose: func(row ChooserRow) Result {
		f := PropFilter{Prop: prop, Op: op, Value: row.Data}
		if not {
			f = f.Negate()
		}
		if err := l.SetPropFilters(append(l.PropFilters(), f)); err != nil {
			return Failed(err)
		}
		return Done(nil)
	}})
	return err
}

func registerListPropFilter(r *Runtime) {
	resolve := func(c Context, a Arguments) (*List, error) {
		if id := StringArg(a, "id", ""); id != "" {
			if l := r.propFilterList(id); l != nil {
				return l, nil
			}
			return nil, fmt.Errorf("unknown or disabled row view %q", id)
		}
		if l := r.propFilterList(focusOf(c)); l != nil {
			return l, nil
		}
		return nil, errors.New("no focused row view")
	}
	guardList := func(ev KeyEvent) *List {
		if id := StringArg(ev.Args, "id", ""); id != "" {
			return r.propFilterList(id)
		}
		return r.propFilterList(ev.Focus)
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
	r.Register(Operation{Noun: "ochazuke.list", Verb: "flush-lingering", Description: "Remove changed rows waiting to leave the active filter", Parameters: map[string]Parameter{"id": {Type: "string"}}, Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		focus := StringArg(a, "id", focusOf(c))
		l := r.rowFilterList(focus)
		if l == nil {
			return nil, errors.New("no row view at " + focus)
		}
		l.FlushLingering()
		return nil, nil
	}})
	r.Register(Operation{Noun: "ochazuke.list", Verb: "filters", Description: "List the saved prop filters available to a list", Parameters: map[string]Parameter{"id": {Type: "string"}}, Result: "filter[]", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		l, err := resolve(c, a)
		if err != nil {
			return nil, err
		}
		return l.SavedFilters()
	}})
	for _, spec := range []struct {
		verb, description, guard string
		params                   map[string]Parameter
		run                      func(*List, Arguments) error
	}{
		{"filter-prop", "Add a prop condition (AND); omit prop or value to pick from visible rows", "list-props", map[string]Parameter{"clear": {Type: "boolean", Description: "Offer clearing active prop filters in the prop picker"}, "prop": {Type: "string"}, "op": {Type: "string", Enum: FilterOps}, "value": {Type: "any", Description: "Typed value; in and not-in take an array"}, "not": {Type: "boolean", Description: "Turn the condition over (= to !=, in to not-in, contains to not-contains); with the value chooser, its not toggle starts on"}}, func(l *List, a Arguments) error {
			prop := StringArg(a, "prop", "")
			value, ok := a["value"]
			not, _ := a["not"].(bool)
			if prop == "" || !ok {
				offerClear, _ := a["clear"].(bool)
				return l.pickPropFilter(prop, offerClear, not)
			}
			f := PropFilter{Prop: prop, Op: StringArg(a, "op", "="), Value: value}
			if not {
				f = f.Negate()
			}
			return l.SetPropFilters(append(l.PropFilters(), f))
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
				rows = append(rows, ChooserRow{ID: strconv.Itoa(i), Label: f.Label(), Data: i})
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
		{"filter-save", "Save current prop conditions as a list-scoped user filter in list.filters", "", map[string]Parameter{"name": {Type: "string", Required: true}}, func(l *List, a Arguments) error { return l.SaveFilter(StringArg(a, "name", "")) }},
		{"filter-saved-delete", "Delete a user saved filter; removing an override reveals the app default", "", map[string]Parameter{"name": {Type: "string", Required: true}}, func(l *List, a Arguments) error { return l.DeleteSavedFilter(StringArg(a, "name", "")) }},
		{"filter-saved-pick", "List saved prop filters in a chooser and apply the selected one", "", nil, func(l *List, _ Arguments) error { return l.pickSavedFilter() }},
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
			if step == 0 {
				step = 1
			}
			i = ((i+step)%len(filters) + len(filters)) % len(filters)
			direction := 1
			if step < 0 {
				direction = -1
			}
			var skipped []string
			for range len(filters) {
				if err := l.ApplyFilter(filters[i].Name); err == nil {
					if len(skipped) > 0 {
						l.r.UI().Info("Skipped saved filters: " + strings.Join(skipped, "; "))
					}
					return nil
				} else {
					skipped = append(skipped, filters[i].Name+": "+err.Error())
				}
				i = (i + direction + len(filters)) % len(filters)
			}
			return fmt.Errorf("no valid saved filters; skipped %s", strings.Join(skipped, "; "))
		}},
	} {
		params := cloneParams(spec.params)
		if params == nil {
			params = map[string]Parameter{}
		}
		params["id"] = Parameter{Type: "string", Description: "List, tree or chooser id (default focused)"}
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
