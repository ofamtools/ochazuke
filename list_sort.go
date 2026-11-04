package ochazuke

import (
	"cmp"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ListSortIndicatorSetting controls sort arrows and separate labels in row views.
const ListSortIndicatorSetting = "list.sort-indicator"

// ListSortSectionsSetting chooses local section ordering or no section sorting.
const ListSortSectionsSetting = "list.sort-sections"

// SortKey is one prop of a list's view sort. Earlier keys take precedence;
// equal values keep the app's order. Desc reverses nonblank values only.
type SortKey struct {
	Field string `json:"field"`
	Desc  bool   `json:"desc,omitempty"`
}

// NoSort leaves ordering to the source of a list or tree. Use it when the
// rows are a virtual window or a partial result: sorting that window would
// misrepresent the whole source. The sort verbs and key are unavailable.
func NoSort() InstanceOption { return func(s *instanceSpec) { s.noSort = true } }

func (l *List) canSort() bool {
	if l.noSort {
		return false
	}
	if l.r.Settings().String(ListSortSectionsSetting) == "off" {
		for _, item := range l.all {
			if item.Section != "" {
				return false
			}
		}
	}
	if l.filterFocus != "" {
		if c := l.r.Chooser(l.id); c != nil && (c.spec.NoSort || c.live()) {
			return false
		}
	}
	return true
}

// Sortable reports whether local ordering is allowed and the rows expose
// a sortable prop or column. Secrets and props turned off for filters are
// not offered for sorting either.
func (l *List) Sortable() bool { return l.canSort() && len(l.sortSpecs()) > 0 }

// Sort returns a copy of the view's sort keys. They are in-memory state,
// preserved by SetItems, Patch and column changes and included in saved filters.
func (l *List) Sort() []SortKey { return slices.Clone(l.sort) }

// SetSort replaces the view sort without changing Items, marks or the app's
// order. An empty sort restores that order. Selection and focus follow IDs;
// invalid updates leave the view unchanged. Trees sort siblings only.
func (l *List) SetSort(keys []SortKey) error {
	keys, err := l.canonicalSort(keys)
	if err != nil {
		return err
	}
	if !slices.Equal(l.sort, keys) {
		l.savedFilter = ""
	}
	l.sort = keys
	if l.filterChanged != nil {
		l.filterChanged()
		return nil
	}
	previous, _ := l.Selected()
	l.refilter(previous.ID)
	l.syncVisual()
	l.syncFocus(previous, nil)
	return nil
}

func (l *List) canonicalSort(keys []SortKey) ([]SortKey, error) {
	if len(keys) > 0 && !l.canSort() {
		return nil, errors.New("row order is owned by the source")
	}
	specs := l.sortSpecs()
	keys = slices.Clone(keys)
	seen := map[string]bool{}
	for i, k := range keys {
		name, err := l.r.filterPropName(l.rowNouns(), k.Field)
		if err != nil {
			return nil, err
		}
		if _, ok := specs[name]; !ok {
			name = columnField(name)
		}
		if _, ok := specs[name]; !ok {
			return nil, fmt.Errorf("unknown or disabled sort field %q", k.Field)
		}
		if seen[name] {
			return nil, fmt.Errorf("repeated sort field %q", name)
		}
		seen[name], keys[i].Field = true, name
	}
	return keys, nil
}

// sortSummary is separate view information, never a prop condition or chip.
func (l *List) sortSummary() string {
	if len(l.columns) > 0 || !l.r.Settings().Bool(ListSortIndicatorSetting) {
		return ""
	}
	var parts []string
	for i, k := range l.sort {
		arrow := "▲"
		if k.Desc {
			arrow = "▼"
		}
		if i > 0 {
			arrow += strconv.Itoa(i + 1)
		}
		parts = append(parts, modalLabel(k.Field)+" "+arrow)
	}
	if len(parts) == 0 {
		return ""
	}
	return "sort " + strings.Join(parts, " · ")
}

// sortSpecs reuses filter props, including derived props. Only rows with
// no prop record fall back to titled columns (an opaque Data handle is not
// a record). Columns do not create a second schema or change filter fields.
func (l *List) sortSpecs() map[string]PropSpec {
	out := l.filterSpecs()
	if len(l.columns) == 0 {
		return out
	}
	for _, item := range l.filterRows() {
		if !rowWithoutProps(item) {
			continue
		}
		n := l.r.Noun(item.Noun)
		if n == nil {
			continue
		}
		for _, col := range l.columns {
			name := l.sortColumnField(col.Title)
			if col.Part != "" || !propName(name) {
				continue
			}
			p := PropSpec{Name: name, Derived: true, ReadOnly: true}
			if declared := n.prop(name); declared != nil {
				p = *declared
			}
			if !l.r.propFilterable(n, p, l.propFocus()) {
				continue
			}
			if _, ok := out[name]; !ok {
				out[name] = p
			}
		}
	}
	return out
}

func rowWithoutProps(item ListItem) bool {
	if item.Props != nil {
		return len(item.Props) == 0
	}
	record, err := ThingOf(item.Data)
	return err != nil || len(record) == 0
}

func (l *List) sortRaw(item ListItem, field string) any {
	if row := l.cachedRow(item); row != nil {
		item = row.item
	} // tree cells have no drawn prefix
	n := l.r.Noun(item.Noun)
	if n == nil {
		return nil
	}
	if p := l.filterProp(n, field); p != nil {
		if v, ok := l.thing(item)[field]; ok {
			return v
		}
	} else if n.prop(field) != nil {
		return nil
	}
	if !rowWithoutProps(item) {
		return nil
	}
	p := PropSpec{Name: field}
	if declared := n.prop(field); declared != nil {
		p = *declared
	}
	if !l.r.propFilterable(n, p, l.propFocus()) {
		return nil
	}
	for i, col := range l.columns {
		if col.Part == "" && l.sortColumnField(col.Title) == field {
			return cells(item, len(l.columns))[i]
		}
	}
	return nil
}

// sortValue is a normalized comparison value, built once per row and key.
// Type ranks make malformed or mixed values deterministic; blanks are
// compared separately so reversing a key never brings them to the front.
type sortValue struct {
	blank  bool
	rank   int
	number *big.Rat
	date   time.Time
	text   string
}

func sortDate(value any) (time.Time, bool) {
	if t, ok := value.(time.Time); ok {
		return t, true
	}
	s, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.DateOnly} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func sortBlank(value any) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) == ""
	case []string:
		return len(v) == 0
	case []any:
		return len(v) == 0
	case time.Time:
		return v.IsZero()
	}
	return false
}

func normalizedSort(value any, p PropSpec) sortValue {
	out := sortValue{blank: sortBlank(value), text: strings.ToLower(fmt.Sprint(looseText(value)))}
	if out.blank {
		return out
	}
	if len(p.Enum) > 0 {
		out.rank = slices.Index(p.Enum, fmt.Sprint(value))
		if out.rank < 0 {
			out.rank = len(p.Enum)
		}
		return out
	}
	switch p.Type {
	case SettingInteger, SettingNumber:
		out.number, _ = new(big.Rat).SetString(strings.TrimSpace(fmt.Sprint(value)))
		if out.number == nil {
			out.rank = 1
		}
	case SettingDuration:
		d, err := time.ParseDuration(fmt.Sprint(value))
		if err != nil {
			out.rank = 1
		} else {
			out.number = big.NewRat(int64(d), 1)
		}
	case SettingBoolean:
		b, err := strconv.ParseBool(fmt.Sprint(value))
		if err != nil {
			out.rank = 1
		} else {
			var n int64
			if b {
				n = 1
			}
			out.number = big.NewRat(n, 1)
		}
	case "date":
		var ok bool
		out.date, ok = sortDate(value)
		if !ok {
			out.rank = 1
		}
	}
	return out
}

func compareSort(a, b sortValue, desc bool) int {
	if a.blank || b.blank {
		if a.blank == b.blank {
			return 0
		}
		if a.blank {
			return 1
		}
		return -1
	}
	c := cmp.Compare(a.rank, b.rank)
	if c == 0 {
		switch {
		case a.number != nil && b.number != nil:
			c = a.number.Cmp(b.number)
		case !a.date.IsZero() && !b.date.IsZero():
			c = a.date.Compare(b.date)
		default:
			c = strings.Compare(a.text, b.text)
		}
	}
	if desc {
		return -c
	}
	return c
}

// sortOrder computes a stable permutation. Text props containing only ISO
// dates (or time.Time) compare chronologically, including timezone offsets;
// mixed date/text props compare entirely as text, preserving transitivity.
func (l *List) sortOrder(items []ListItem) []int {
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	if len(l.sort) == 0 || !l.canSort() {
		return order
	}
	specs := l.sortSpecs()
	types := make([]PropSpec, len(l.sort))
	for i, key := range l.sort {
		p := specs[key.Field]
		if len(p.Enum) == 0 && (p.Type == "" || p.Type == SettingString) {
			date, seen := true, false
			for _, item := range l.filterRows() {
				v := l.sortRaw(item, key.Field)
				if sortBlank(v) {
					continue
				}
				seen = true
				if _, ok := sortDate(v); !ok {
					date = false
					break
				}
			}
			if date && seen {
				p.Type = "date"
			}
		}
		types[i] = p
	}
	values := make([][]sortValue, len(items))
	for i, item := range items {
		values[i] = make([]sortValue, len(l.sort))
		for j, key := range l.sort {
			values[i][j] = normalizedSort(l.sortRaw(item, key.Field), types[j])
		}
	}
	// Section headings belong to contiguous source runs; a view sort must
	// never interleave their rows or move one run through another.
	groups := make([]int, len(items))
	for i := 1; i < len(items); i++ {
		groups[i] = groups[i-1]
		if items[i].Section != items[i-1].Section {
			groups[i]++
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		if c := cmp.Compare(groups[a], groups[b]); c != 0 {
			return c
		}
		for j, key := range l.sort {
			if c := compareSort(values[a][j], values[b][j], key.Desc); c != 0 {
				return c
			}
		}
		return 0
	})
	return order
}

func (l *List) sortedRows(items []ListItem) []ListItem {
	if len(l.sort) == 0 || !l.canSort() {
		return items
	}
	if t := l.tree(); t != nil {
		ranks := map[string]int{}
		for i, n := range t.sortedNodes() {
			ranks[n.ID] = i
		}
		out := slices.Clone(items)
		slices.SortStableFunc(out, func(a, b ListItem) int { return cmp.Compare(ranks[a.ID], ranks[b.ID]) })
		return out
	}
	out := make([]ListItem, len(items))
	for i, at := range l.sortOrder(items) {
		out[i] = items[at]
	}
	return out
}

// sortedNodes keeps each subtree intact. The source nodes, collapse state,
// zoom and filter ancestry remain in their original representation.
func (t *Tree) sortedNodes() []TreeNode {
	if len(t.list.sort) == 0 || !t.list.canSort() {
		return t.nodes
	}
	items := make([]ListItem, len(t.nodes))
	for i, n := range t.nodes {
		items[i] = t.rowCache[n.ID].item
	}
	children := map[string][]TreeNode{}
	for _, i := range t.list.sortOrder(items) {
		n := t.nodes[i]
		children[n.Parent] = append(children[n.Parent], n)
	}
	out := make([]TreeNode, 0, len(t.nodes))
	var walk func(string)
	walk = func(parent string) {
		for _, n := range children[parent] {
			out = append(out, n)
			walk(n.ID)
		}
	}
	walk("")
	return out
}

func (l *List) sortBy(by string) error {
	var keys []SortKey
	if strings.TrimSpace(by) != "" {
		for _, word := range strings.Split(by, ",") {
			word = strings.TrimSpace(word)
			desc := strings.HasPrefix(word, "-") || strings.HasSuffix(word, "!")
			name := strings.TrimSuffix(strings.TrimPrefix(word, "-"), "!")
			keys = append(keys, SortKey{Field: name, Desc: desc})
		}
	}
	return l.SetSort(keys)
}

func (l *List) cycleSort(field string, desc *bool) error {
	if desc != nil {
		return l.SetSort([]SortKey{{Field: field, Desc: *desc}})
	}
	if len(l.sort) == 1 && l.sort[0].Field == field {
		if l.sort[0].Desc {
			return l.SetSort(nil)
		}
		return l.SetSort([]SortKey{{Field: field, Desc: true}})
	}
	return l.SetSort([]SortKey{{Field: field}})
}

func (l *List) sortColumn(desc *bool) error {
	specs := l.sortSpecs()
	if l.cellSelect && l.col >= 0 && l.col < len(l.columns) {
		name := l.sortColumnField(l.columns[l.col].Title)
		if _, ok := specs[name]; ok {
			return l.cycleSort(name, desc)
		}
	}
	rows := []ChooserRow{}
	for _, name := range sortedKeys(specs) {
		rows = append(rows, ChooserRow{ID: name, Label: name})
	}
	_, err := l.r.OpenChooser(ChooserSpec{Kind: "list-sort-field", Title: "Sort by field", Rows: rows, Choose: func(row ChooserRow) Result {
		if err := l.cycleSort(row.ID, desc); err != nil {
			return Failed(err)
		}
		return Done(nil)
	}})
	return err
}

func (l *List) sortColumnField(title string) string {
	name := columnField(title)
	if main, err := l.r.filterPropName(l.rowNouns(), name); err == nil {
		return main
	}
	return name
}

func (l *List) sortTitle(col Column) string {
	title := col.Title
	if !l.r.Settings().Bool(ListSortIndicatorSetting) || !l.canSort() {
		return title
	}
	for i, key := range l.sort {
		if l.sortColumnField(col.Title) == key.Field {
			arrow := "▲"
			if key.Desc {
				arrow = "▼"
			}
			if i > 0 {
				arrow += strconv.Itoa(i + 1)
			}
			return title + " " + arrow
		}
	}
	return title
}

func registerListSort(r *Runtime) {
	resolve := func(focus string, a Arguments) *List { return r.rowFilterList(StringArg(a, "id", focus)) }
	r.DefineGuard("list-sortable", func(_ *Runtime, ev KeyEvent) bool {
		l := resolve(ev.Focus, ev.Args)
		return l != nil && l.canSort() && (l.Sortable() || len(l.sort) > 0)
	})
	r.DefineGuard("list-sorted", func(_ *Runtime, ev KeyEvent) bool {
		l := resolve(ev.Focus, ev.Args)
		return l != nil && len(l.sort) > 0
	})
	for _, s := range []struct {
		verb, description, guard string
		params                   map[string]Parameter
		run                      func(*List, Arguments) error
	}{
		{"sort", "Sort the view stably by comma-separated props; -field descends, empty restores app order", "list-sortable", map[string]Parameter{"by": {Type: "string"}}, func(l *List, a Arguments) error { return l.sortBy(StringArg(a, "by", "")) }},
		{"sort-column", "Cycle the current column ascending, descending, off; in row mode choose a prop", "list-sortable", map[string]Parameter{"desc": {Type: "boolean", Description: "Set descending or ascending explicitly; omit to cycle"}}, func(l *List, a Arguments) error {
			var desc *bool
			if v, ok := a["desc"].(bool); ok {
				desc = &v
			}
			return l.sortColumn(desc)
		}},
		{"sort-clear", "Restore the app's row order, keeping filters", "list-sorted", nil, func(l *List, _ Arguments) error { return l.SetSort(nil) }},
	} {
		params := s.params
		if params == nil {
			params = map[string]Parameter{}
		}
		params["id"] = Parameter{Type: "string", Description: "List, tree or chooser; default focused row view"}
		run := s.run
		r.Register(Operation{Noun: "ochazuke.list", Verb: s.verb, Description: s.description, Parameters: params, Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
			l := resolve(focusOf(c), a)
			if l == nil {
				return nil, errors.New("no row view to sort")
			}
			if err := run(l, a); err != nil {
				return nil, err
			}
			if l.filterChanged != nil {
				return l.Sort(), nil
			}
			return l.state(), nil
		}})
		r.Nouns["ochazuke.list"].Guards[s.verb] = s.guard
	}
}

// Sort and SetSort expose the chooser's shared row-view controller. Fixed,
// trailing and create rows retain their roles; ordinary rows sort locally.
func (c *Chooser) Sort() []SortKey              { return c.filters.Sort() }
func (c *Chooser) SetSort(keys []SortKey) error { return c.filters.SetSort(keys) }

func (c *Chooser) sortedRows(rows []ChooserRow) []ChooserRow {
	if len(c.filters.sort) == 0 || !c.filters.canSort() {
		return rows
	}
	items := make([]ListItem, len(rows))
	for i, row := range rows {
		items[i] = c.propItem(row)
	}
	out := make([]ChooserRow, len(rows))
	for i, at := range c.filters.sortOrder(items) {
		out[i] = rows[at]
	}
	return out
}
