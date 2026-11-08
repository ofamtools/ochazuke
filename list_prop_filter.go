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
func (l *Collection) PropFilters() []PropFilter { return clonePropFilters(l.vs.Filters) }

func (l *Collection) tree() *Tree {
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

func (l *Collection) treeHasNode(id string) bool {
	t := l.tree()
	return t != nil && t.index(id) >= 0
}

func (l *Collection) cacheRows() {
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
	if len(set) == 0 && l.declaredNoun != "" {
		set[l.r.CanonicalNoun(l.declaredNoun)] = true
	}
	if len(set) == 0 && len(l.nouns) > 0 {
		return // no rows now: keep the nouns of the last rows
	}
	if len(set) == 0 {
		if inst := l.r.Instances[l.id]; inst != nil {
			set[inst.Noun] = true
		}
	}
	l.nouns = slices.Sorted(maps.Keys(set))
}

func (l *Collection) cachedRow(item ListItem) *filterRow {
	if item.ID == "" {
		return nil
	}
	if t := l.tree(); t != nil {
		if row := t.rowCache[item.ID]; row != nil {
			return row
		}
	}
	return l.rowCache[item.ID]
}

func (l *Collection) preparedRow(item ListItem, m Matcher) string {
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

func (l *Collection) thing(item ListItem) Thing {
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
func (l *Collection) cellThing(item ListItem, t Thing) Thing {
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

func (l *Collection) rowNouns() []string { return l.nouns }

// A stable list focus is shared by matching and both picker stages,
// including while a chooser temporarily owns the runtime focus.
func (l *Collection) propFocus() string {
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

// archiveHidden is the ids collection.hide-archived hides: the rows of a noun
// with an archive-role prop (rowThing, so props from a row-fed page and
// from a store alike) whose prop is true, decided by the same predicate as
// runList (Runtime.archiveHiding). In a tree an archived node hides its
// whole subtree: a child cannot stand without its parent, and filterNodes
// would otherwise bring the archived parent back as context. nil when
// nothing hides. A prop filter naming the archive prop turns hiding off,
// so the saved "archived" filter shows the archived rows.
func (l *Collection) archiveHidden() map[string]bool {
	var hidden map[string]bool
	byNoun := map[string]*PropSpec{}
	hide := func(item ListItem, parentHidden bool) bool {
		p, seen := byNoun[item.Noun]
		if !seen {
			p = l.r.archiveHiding(l.r.Noun(item.Noun), l.vs)
			byNoun[item.Noun] = p
		}
		if !parentHidden && (p == nil || !archivedThing(p, l.thing(item))) {
			return false
		}
		if hidden == nil {
			hidden = map[string]bool{}
		}
		hidden[item.ID] = true
		return true
	}
	if t := l.tree(); t != nil {
		for _, n := range t.nodes { // parents precede children
			if row := t.rowCache[n.ID]; row != nil {
				hide(row.item, n.Parent != "" && hidden[n.Parent])
			}
		}
		return hidden
	}
	for _, item := range l.all {
		hide(item, false)
	}
	return hidden
}

func (l *Collection) matchesProps(item ListItem) bool {
	if len(l.vs.Filters) == 0 {
		return true
	}
	n := l.r.Noun(item.Noun)
	if n == nil {
		return false
	}
	t := l.thing(item)
	for _, f := range l.vs.Filters {
		p := l.filterProp(n, f.Prop)
		if p == nil {
			return false
		}
		value, present := t[p.Name]
		if !present || !propMatches(*p, value, f) {
			return false
		}
	}
	return true
}

func (l *Collection) validatePropFilters(filters []PropFilter) error {
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
func (l *Collection) SetPropFilters(filters []PropFilter) error {
	filters, err := l.canonicalPropFilters(filters)
	if err != nil {
		return err
	}
	return l.replacePropFilters(filters, "")
}

// replacePropFilters swaps the conditions and names the saved filter they
// came from ("" for none) before the rows follow, so the list's OnFilter
// hears the new conditions and saved name, on flat lists and trees alike.
func (l *Collection) replacePropFilters(filters []PropFilter, saved string) error {
	l.linger.reset()
	l.vs.Filters = filters
	l.vs.Saved = saved
	if l.filterChanged != nil {
		l.filterChanged()
		return nil
	}
	if t := l.tree(); t != nil {
		t.filterCollapsed = nil
		if err := t.refresh(""); err != nil {
			return err
		}
		l.filterHeard()
		return nil
	}
	l.SetFilter(l.vs.Text)
	return nil
}

func (l *Collection) canonicalPropFilters(filters []PropFilter) ([]PropFilter, error) {
	filters = clonePropFilters(filters)
	for i := range filters {
		name, err := l.r.filterPropName(l.rowNouns(), filters[i].Prop)
		if err != nil {
			return nil, err
		}
		filters[i].Prop = name
	}
	if err := l.validatePropFilters(filters); err != nil {
		return nil, err
	}
	if _, err := json.Marshal(filters); err != nil {
		return nil, err
	}
	return filters, nil
}

// SavedFilters returns the defaults and user filters that apply here.
func (l *Collection) SavedFilters() ([]SavedFilter, error) {
	return l.r.SavedFilters(l.id, l.rowNouns()...)
}

// ApplyFilter applies a named saved filter, keeping the text query.
func (l *Collection) ApplyFilter(name string) error {
	filters, err := l.SavedFilters()
	if err != nil {
		return err
	}
	for _, f := range filters {
		if f.Name == name {
			var keys []SortKey
			if l.canSort() {
				keys, err = l.canonicalSort(f.Sort)
			}
			if err != nil {
				return err
			}
			// Validate both halves before replacing either view state.
			conditions, err := l.canonicalPropFilters(f.conditions())
			if err != nil {
				return err
			}
			l.vs.Sort = keys
			if err := l.replacePropFilters(conditions, name); err != nil {
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("unknown saved filter %q", name)
}

// SaveFilter writes the current conditions and sort as a list-scoped user definition.
// Existing definitions in other scopes keep their order and content.
func (l *Collection) SaveFilter(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("saved filter needs a name")
	}
	keys, err := l.canonicalSort(l.Sort())
	if err != nil {
		return err
	}
	user, err := l.r.Settings().savedFilters()
	if err != nil {
		return err
	}
	user = slices.Clone(user)
	f := SavedFilter{Name: name, List: l.id, Conditions: l.PropFilters(), Sort: keys}
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
	if err := l.r.Settings().Set("collection.filters", string(raw)); err != nil {
		return err
	}
	l.vs.Saved = name
	return nil
}

// DeleteSavedFilter removes the effective user definition by name. App
// defaults remain declarations; removing a user override reveals that default.
func (l *Collection) DeleteSavedFilter(name string) error {
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
	i = slices.IndexFunc(user, f.sameDefinition)
	if i < 0 {
		return fmt.Errorf("%q is an app default; only user saved filters can be deleted", name)
	}
	user = slices.Delete(slices.Clone(user), i, i+1)
	raw, err := json.Marshal(user)
	if err != nil {
		return err
	}
	if err := l.r.Settings().Set("collection.filters", string(raw)); err != nil {
		return err
	}
	if l.vs.Saved == name {
		l.vs.Saved = ""
	}
	return nil
}

func (l *Collection) pickSavedFilter() error {
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

// savedFilterNamed is the typed conditions of the saved filter called name
// for n; found is false when there is none.
func (r *Runtime) savedFilterNamed(n *Noun, name string) ([]PropFilter, bool, error) {
	filters, err := r.SavedFilters("", n.Name)
	if err != nil {
		return nil, false, err
	}
	for _, f := range filters {
		if f.Name == name {
			out := f.conditions()
			for i, c := range out {
				if out[i], err = typedFilter(n, c); err != nil {
					return nil, true, err
				}
			}
			return out, true, nil
		}
	}
	return nil, false, nil
}

func (l *Collection) propStrip() string {
	if !l.r.Settings().Bool("collection.filter-strip") {
		return ""
	}
	var parts []string
	if l.vs.Saved != "" {
		parts = append(parts, "["+modalLabel(l.vs.Saved)+"]")
	}
	for _, f := range l.vs.Filters {
		parts = append(parts, "["+modalLabel(f.Label())+"]")
	}
	return strings.Join(parts, " ")
}

// filterNodes keeps matches and ancestors. Parents precede children, so
// one reverse pass propagates descendant matches without walking every
// ancestor for every node.
func (t *Tree) filterNodes(hidden map[string]bool) map[string]bool {
	kept := map[string]bool{}
	m := t.list.matcher()
	for _, n := range t.nodes {
		item := t.rowCache[n.ID].item
		if !hidden[item.ID] && t.list.matchesProps(item) && m.MatchPrepared(t.list.preparedRow(item, m)) {
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

func (l *Collection) filterProps() []string { return sortedKeys(l.filterSpecs()) }

// pickPropFilter opens the filter form (prop empty, collection.filter-form on;
// list_filter_form.go), else the prop chooser (prop empty), then the value
// chooser; not turns the picked condition over (!=, not-contains).
func (l *Collection) pickPropFilter(prop string, offerClear, not bool) error {
	if prop == "" && l.r.Settings().Bool(ListFilterFormSetting) {
		return l.openFilterForm()
	}
	if prop == "" {
		rows := []ChooserRow{}
		for _, name := range l.filterProps() {
			rows = append(rows, ChooserRow{ID: name, Label: name})
		}
		if l.r.Settings().Bool(ListFilterPickerSortSetting) && l.Sortable() {
			rows = append(rows, ChooserRow{Label: "Sort…", Fixed: true, Data: Request{Noun: "ochazuke.collection", Verb: "sort-column", Args: Arguments{"id": l.id, "pick": true}}})
		}
		if offerClear && (len(l.vs.Filters) > 0 || l.vs.Saved != "") {
			rows = append(rows, ChooserRow{Label: "Clear prop filters", Fixed: true})
		}
		if len(rows) == 0 {
			return errors.New("list rows have no filterable props")
		}
		_, err := l.r.OpenChooser(ChooserSpec{Kind: "list-filter-prop", Title: "Filter by prop", Rows: rows, Choose: func(row ChooserRow) Result {
			if q, ok := row.Data.(Request); ok {
				if _, err := l.r.Execute(q); err != nil {
					return Failed(err)
				}
				return Done(nil)
			}
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
	name, err := l.r.filterPropName(l.rowNouns(), prop)
	if err != nil {
		return err
	}
	prop = name
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
	_, err = l.r.OpenChooser(ChooserSpec{Kind: "list-filter-value", Title: title, Rows: rows, Choose: func(row ChooserRow) Result {
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
	registerFilterViewForm(r)
	resolve := func(c Context, a Arguments) (*Collection, error) {
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
	guardList := func(ev KeyEvent) *Collection {
		if id := StringArg(ev.Args, "id", ""); id != "" {
			return r.propFilterList(id)
		}
		return r.propFilterList(ev.Focus)
	}
	r.DefineGuard("list-prop-conditions", func(_ *Runtime, ev KeyEvent) bool { l := guardList(ev); return l != nil && len(l.vs.Filters) > 0 })
	r.DefineGuard("list-props", func(r *Runtime, ev KeyEvent) bool {
		l := guardList(ev)
		return l != nil && (len(l.filterProps()) > 0 || l.r.Settings().Bool(ListFilterSortSetting) && l.Sortable())
	})
	r.DefineGuard("list-prop-filtered", func(r *Runtime, ev KeyEvent) bool {
		l := guardList(ev)
		return l != nil && (len(l.vs.Filters) > 0 || l.vs.Saved != "")
	})
	r.DefineGuard("list-saved-filters", func(r *Runtime, ev KeyEvent) bool {
		l := guardList(ev)
		if l == nil || !r.Settings().Bool("collection.filter-cycle") {
			return false
		}
		filters, err := l.SavedFilters()
		return err == nil && len(filters) > 0
	})
	r.Register(Operation{Key: "ochazuke.collection.flush-lingering", Description: "Remove changed rows waiting to leave the active filter", Parameters: map[string]Parameter{"id": {Type: "string"}}, Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		focus := StringArg(a, "id", focusOf(c))
		l := r.rowFilterList(focus)
		if l == nil {
			return nil, errors.New("no row view at " + focus)
		}
		l.FlushLingering()
		return nil, nil
	}})
	r.Register(Operation{Key: "ochazuke.collection.filters", Description: "List the saved prop filters available to a list", Parameters: map[string]Parameter{"id": {Type: "string"}}, Result: "filter[]", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		l, err := resolve(c, a)
		if err != nil {
			return nil, err
		}
		return l.SavedFilters()
	}})
	for _, spec := range []struct {
		verb, description, guard string
		params                   map[string]Parameter
		run                      func(*Collection, Arguments) error
	}{
		{"filter-prop", "Add a prop condition (AND); omit prop or value to pick from visible rows", "list-props", map[string]Parameter{"clear": {Type: "boolean", Description: "Offer clearing active prop filters in the prop picker"}, "prop": {Type: "string"}, "op": {Type: "string", Enum: FilterOps}, "value": {Type: "any", Description: "Typed value; in and not-in take an array"}, "not": {Type: "boolean", Description: "Turn the condition over (= to !=, in to not-in, contains to not-contains); with the value picker, its not toggle starts on"}}, func(l *Collection, a Arguments) error {
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
		{"filter-prop-clear", "Remove one prop condition; omit prop and index to pick an active condition", "list-prop-conditions", map[string]Parameter{"prop": {Type: "string"}, "index": {Type: "integer", Description: "1-based condition index"}}, func(l *Collection, a Arguments) error {
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
				prop, err := r.filterPropName(l.rowNouns(), prop)
				if err != nil {
					return err
				}
				return clear(slices.IndexFunc(l.vs.Filters, func(f PropFilter) bool { return f.Prop == prop }))
			}
			rows := []ChooserRow{}
			for i, f := range l.vs.Filters {
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
		{"filter-props-clear", "Clear every prop condition and saved filter name, keeping / text", "list-prop-filtered", nil, func(l *Collection, _ Arguments) error { return l.SetPropFilters(nil) }},
		{"filter-saved", "Apply saved conditions and sort by name, keeping / text", "", map[string]Parameter{"name": {Type: "string", Required: true}}, func(l *Collection, a Arguments) error { return l.ApplyFilter(StringArg(a, "name", "")) }},
		{"filter-save", "Save current prop conditions and sort as a list-scoped user filter in collection.filters", "", map[string]Parameter{"name": {Type: "string", Required: true}}, func(l *Collection, a Arguments) error { return l.SaveFilter(StringArg(a, "name", "")) }},
		{"filter-saved-delete", "Delete a user saved filter; removing an override reveals the app default", "", map[string]Parameter{"name": {Type: "string", Required: true}}, func(l *Collection, a Arguments) error { return l.DeleteSavedFilter(StringArg(a, "name", "")) }},
		{"filter-saved-pick", "List saved prop filters in a picker and apply the selected one", "", nil, func(l *Collection, _ Arguments) error { return l.pickSavedFilter() }},
		{"filter-cycle", "Cycle the saved prop filters declared for this list or its row nouns", "list-saved-filters", map[string]Parameter{"step": {Type: "integer"}}, func(l *Collection, a Arguments) error {
			filters, err := l.SavedFilters()
			if err != nil {
				return err
			}
			if len(filters) == 0 {
				return errors.New("no saved filters")
			}
			i := slices.IndexFunc(filters, func(f SavedFilter) bool { return f.Name == l.vs.Saved })
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
		params["id"] = Parameter{Type: "string", Description: "List, tree or picker id (default focused)"}
		r.Register(Operation{Key: KeyOf("ochazuke.collection", spec.verb), Description: spec.description, Parameters: params, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
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
			r.Nouns["ochazuke.collection"].Guards[spec.verb] = spec.guard
		}
	}
	_ = r.SetConfirm("ochazuke.collection.filter-saved-delete", "Delete saved filter {{name}}?")
	op := r.Operations.At(Key("ochazuke.collection.filter-saved-delete"))
	op.Destructive = true
	r.Operations.Set(op)
}
