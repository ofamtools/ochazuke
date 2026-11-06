package ochazuke

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ListQuery is one view of a noun's things: what the standard list verb
// takes, what a store's Query runs, and the whole view state of a List
// widget. Filters are ANDed prop conditions; Sort orders the rows (earlier
// keys first, ties keep the source order); GroupBy gathers the rows that
// share the values of those props into groups, kept in the order their
// first row came unless GroupSort (keys naming GroupBy props) orders them;
// Page (from 1) and PerPage (0: every row) cut the result into pages. A
// page counts rows, never groups: a group runs on across a page edge.
// Archived includes the things the archive prop hides by default
// (list.hide-archived).
//
// Text, Saved and Args are not part of the answer's shape. Text is a
// widget's text query, matched by its matcher; Saved is the name of the
// saved filter whose conditions and sort Filters and Sort still hold (it is
// dropped the moment either changes, ListQuery.WithSort and WithFilters);
// Args are the store's own ListParams, passed through to a Store.
type ListQuery struct {
	Filters   []PropFilter
	Sort      []SortKey
	GroupBy   []string
	GroupSort []SortKey
	Page      int
	PerPage   int
	Archived  bool

	Text  string
	Saved string
	Args  Arguments
}

// WithSort is the query with these sort keys; a saved filter name no
// longer holds when the sort differs from the one it saved.
func (q ListQuery) WithSort(keys []SortKey) ListQuery {
	if !slices.Equal(q.Sort, keys) {
		q.Saved = ""
	}
	q.Sort = keys
	return q
}

// WithFilters is the query with these conditions; the saved filter name is
// dropped when they differ.
func (q ListQuery) WithFilters(filters []PropFilter) ListQuery {
	if !samePropFilters(q.Filters, filters) {
		q.Saved = ""
	}
	q.Filters = filters
	return q
}

// Grouped reports whether the query groups its rows.
func (q ListQuery) Grouped() bool { return len(q.GroupBy) > 0 }

// Paged reports whether the query cuts its rows into pages.
func (q ListQuery) Paged() bool { return q.PerPage > 0 }

func samePropFilters(a, b []PropFilter) bool {
	return len(a) == len(b) && fmt.Sprint(a) == fmt.Sprint(b)
}

// ListPage is what Store.Query answers: the things of the requested page
// in order, how many things the whole query matched (Total), and, when the
// query groups, the group label of each thing of the page (Groups, parallel
// to Items; nil when the store does not label them).
type ListPage struct {
	Items  []Thing
	Total  int
	Groups []string
}

// ListAnswer is what the standard list verb answers: the rows of the page
// and where the page sits. PerPage is 0 when the answer is not paged (every
// row is Items); Prev and Next are the neighbouring page numbers, 0 (left
// out) at the ends.
type ListAnswer struct {
	Items   []map[string]any `json:"items"`
	Page    int              `json:"page"`
	PerPage int              `json:"per_page"`
	Total   int              `json:"total"`
	Prev    int              `json:"prev,omitempty"`
	Next    int              `json:"next,omitempty"`
}

// AnswerRows is the rows a list answer holds: a ListAnswer's Items, the
// items of the same page decoded from JSON (an object holding items, page,
// per_page and total, as a mounted host's or an application's answer
// arrives), else the value as it is. Callers that show a list verb's
// answer as rows (the palette, the shell's tables, a catalog fill) read it
// through here.
func AnswerRows(value any) any {
	switch a := value.(type) {
	case ListAnswer:
		return a.Items
	case *ListAnswer:
		if a != nil {
			return a.Items
		}
	case map[string]any:
		if items, ok := pageObjectItems(a); ok {
			return items
		}
	case Thing:
		if items, ok := pageObjectItems(a); ok {
			return items
		}
	default:
		// Raw JSON text (json.RawMessage, jsontext.Value: the shell's
		// answers): a page object's items, decoded.
		if raw, ok := rawJSON(value); ok && bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
			var m map[string]any
			if json.Unmarshal(raw, &m) == nil {
				if items, ok := pageObjectItems(m); ok {
					return items
				}
			}
		}
	}
	return value
}

// rawJSON is value's bytes when it is a byte slice of any named type (raw
// JSON text), false otherwise.
func rawJSON(value any) ([]byte, bool) {
	v := reflect.ValueOf(value)
	if !v.IsValid() || v.Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 {
		return nil, false
	}
	return v.Bytes(), true
}

// pageObjectItems is the items of a ListAnswer that crossed JSON: an
// object with every one of items (an array or null), page, per_page and
// total, false for any other object (a thing with a prop named items).
func pageObjectItems(m map[string]any) (any, bool) {
	for _, key := range []string{"page", "per_page", "total"} {
		if _, ok := m[key]; !ok {
			return nil, false
		}
	}
	items, ok := m["items"]
	if !ok {
		return nil, false
	}
	switch items.(type) {
	case nil:
		return []any{}, true
	case []any, []map[string]any, []Thing:
		return items, true
	}
	return nil, false
}

// queryRows is the rows a query runs over, read by index: the same
// function arranges the things of a Store and the rows of a List widget.
type queryRows struct {
	count int
	// hidden says a row is out of the default view (an archived thing).
	hidden func(i int) bool
	// match says a row meets every condition.
	match func(i int, f []PropFilter) bool
	// spec is the prop a field names (its type orders the values); a field
	// no row knows answers a zero PropSpec.
	spec func(field string) PropSpec
	// raw is a row's value of a field.
	raw func(i int, field string) any
	// run numbers the contiguous runs of the source (a list's sections):
	// sort never moves a row out of its run. nil: no runs.
	run func(i int) string
	// scan visits every value of a field the date detection reads; nil
	// visits the arranged rows' own.
	scan func(field string, yield func(any))
}

// queryOut is an arranged query: Order holds the page's row indexes in
// display order, Groups the group label of each (nil when the query does
// not group), Total the rows the query matched, and Page, PerPage and Pages
// where the page sits (Pages is 1 for an unpaged answer).
type queryOut struct {
	Order   []int
	Groups  []string
	Total   int
	Page    int
	PerPage int
	Pages   int
	// MaxRuns is the most groups any one page shows (a group that runs on
	// across a page edge counts on both pages); 0 when not grouped.
	MaxRuns int
}

// selectRows are the indexes of the rows the default view shows and the
// conditions let through, in source order.
func selectRows(rows queryRows, q ListQuery) []int {
	out := make([]int, 0, rows.count)
	for i := range rows.count {
		if rows.hidden != nil && !q.Archived && rows.hidden(i) {
			continue
		}
		if len(q.Filters) > 0 && !rows.match(i, q.Filters) {
			continue
		}
		out = append(out, i)
	}
	return out
}

// arrangeRows sorts, groups and pages the rows idx (indexes into rows, in
// source order): the one function every list view runs.
func arrangeRows(rows queryRows, idx []int, q ListQuery) queryOut {
	order := orderQuery(rows, idx, q.Sort)
	out := queryOut{Total: len(order), PerPage: max(0, q.PerPage), Pages: 1}
	if q.Grouped() {
		order, out.Groups = groupQuery(rows, order, q.GroupBy, q.GroupSort)
	}
	out.Page = 1
	if out.Groups != nil {
		step := len(order)
		if out.PerPage > 0 {
			step = out.PerPage
		}
		for lo := 0; lo < len(order); lo += max(step, 1) {
			runs := 1
			for i := lo + 1; i < min(lo+step, len(order)); i++ {
				if out.Groups[i] != out.Groups[i-1] {
					runs++
				}
			}
			out.MaxRuns = max(out.MaxRuns, runs)
		}
	}
	if out.PerPage > 0 {
		out.Pages = max(1, (len(order)+out.PerPage-1)/out.PerPage)
		out.Page = min(max(q.Page, 1), out.Pages)
		lo := (out.Page - 1) * out.PerPage
		hi := min(lo+out.PerPage, len(order))
		order = order[lo:hi]
		if out.Groups != nil {
			out.Groups = out.Groups[lo:hi]
		}
	}
	out.Order = order
	return out
}

// orderQuery orders the rows idx by the keys: a stable sort, so ties keep
// the source order.
func orderQuery(rows queryRows, idx []int, keys []SortKey) []int {
	if len(keys) == 0 {
		return slices.Clone(idx)
	}
	specs := make([]PropSpec, len(keys))
	for j, k := range keys {
		specs[j] = rows.spec(k.Field)
		if rows.scan != nil {
			specs[j] = dateTyped(specs[j], func(yield func(any)) { rows.scan(k.Field, yield) })
		} else {
			specs[j] = dateTyped(specs[j], func(yield func(any)) {
				for _, i := range idx {
					yield(rows.raw(i, k.Field))
				}
			})
		}
	}
	var run func(int) string
	if rows.run != nil {
		run = func(j int) string { return rows.run(idx[j]) }
	}
	at := orderRows(len(idx), keys, specs, func(j int, field string) any { return rows.raw(idx[j], field) }, run)
	out := make([]int, len(at))
	for j, k := range at {
		out[j] = idx[k]
	}
	return out
}

// dateTyped gives a text prop whose values are all ISO dates the date type,
// so they order by time; mixed date and text values order as text, which
// keeps the order transitive.
func dateTyped(p PropSpec, values func(yield func(any))) PropSpec {
	if len(p.Enum) > 0 || (p.Type != "" && p.Type != SettingString) {
		return p
	}
	date, seen := true, false
	values(func(v any) {
		if !date || sortBlank(v) {
			return
		}
		seen = true
		if _, ok := sortDate(v); !ok {
			date = false
		}
	})
	if date && seen {
		p.Type = "date"
	}
	return p
}

// orderRows is a stable permutation of 0..count-1 by the keys: normalized
// values per key (specs[j] types keys[j]), blanks last whichever way a key
// runs. run, when set, numbers contiguous source runs that keep their order
// among themselves.
func orderRows(count int, keys []SortKey, specs []PropSpec, raw func(i int, field string) any, run func(i int) string) []int {
	order := make([]int, count)
	for i := range order {
		order[i] = i
	}
	if len(keys) == 0 {
		return order
	}
	values := make([][]sortValue, count)
	for i := range count {
		values[i] = make([]sortValue, len(keys))
		for j, key := range keys {
			values[i][j] = normalizedSort(raw(i, key.Field), specs[j])
		}
	}
	var runs []int
	if run != nil {
		runs = make([]int, count)
		for i := 1; i < count; i++ {
			runs[i] = runs[i-1]
			if run(i) != run(i-1) {
				runs[i]++
			}
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		if runs != nil && runs[a] != runs[b] {
			return cmp.Compare(runs[a], runs[b])
		}
		for j, key := range keys {
			if c := compareSort(values[a][j], values[b][j], key.Desc); c != 0 {
				return c
			}
		}
		return 0
	})
	return order
}

// NoGroup is the label of the rows whose group props are all blank.
const NoGroup = "(none)"

// groupLabel is the label of the group a row belongs to: its values of the
// props, joined.
func groupLabel(rows queryRows, i int, by []string) string {
	parts := make([]string, len(by))
	for j, field := range by {
		v := rows.raw(i, field)
		if sortBlank(v) {
			parts[j] = NoGroup
			continue
		}
		parts[j] = fmt.Sprint(looseText(v))
	}
	return strings.Join(parts, " / ")
}

// groupQuery gathers the rows of order into groups by the props: groups in
// the order their first row came, or by groupSort (keys naming props of by,
// compared over the group's first row); rows keep their order inside a
// group. It answers the rows in group order and each row's group label.
func groupQuery(rows queryRows, order []int, by []string, groupSort []SortKey) ([]int, []string) {
	type group struct {
		label string
		rows  []int
	}
	var groups []*group
	byLabel := map[string]*group{}
	for _, i := range order {
		label := groupLabel(rows, i, by)
		g := byLabel[label]
		if g == nil {
			g = &group{label: label}
			byLabel[label] = g
			groups = append(groups, g)
		}
		g.rows = append(g.rows, i)
	}
	if len(groupSort) > 0 {
		specs := make([]PropSpec, len(groupSort))
		for j, k := range groupSort {
			specs[j] = dateTyped(rows.spec(k.Field), func(yield func(any)) {
				for _, g := range groups {
					yield(rows.raw(g.rows[0], k.Field))
				}
			})
		}
		at := orderRows(len(groups), groupSort, specs, func(j int, field string) any { return rows.raw(groups[j].rows[0], field) }, nil)
		sorted := make([]*group, len(groups))
		for j, k := range at {
			sorted[j] = groups[k]
		}
		groups = sorted
	}
	flat := make([]int, 0, len(order))
	labels := make([]string, 0, len(order))
	for _, g := range groups {
		for _, i := range g.rows {
			flat = append(flat, i)
			labels = append(labels, g.label)
		}
	}
	return flat, labels
}

// thingRows reads a noun's things as query rows.
func (r *Runtime) thingRows(n *Noun, things []Thing) queryRows {
	archive := n.roleProp(RoleArchive)
	return queryRows{
		count: len(things),
		hidden: func(i int) bool {
			return archive != nil && archivedThing(archive, things[i])
		},
		match: func(i int, f []PropFilter) bool { return propsMatch(n, things[i], f) },
		spec: func(field string) PropSpec {
			if p := n.prop(field); p != nil {
				return *p
			}
			return PropSpec{Name: field}
		},
		raw: func(i int, field string) any {
			if p := n.prop(field); p != nil {
				field = p.Name
			}
			return things[i][field]
		},
	}
}

// queryAll runs a whole query over the things a store listed: the generic
// in-memory form of Store.Query. Archived things are left out unless the
// query includes them or the archive hiding is off for it (archiveHiding).
func (r *Runtime) queryAll(n *Noun, things []Thing, q ListQuery) ListPage {
	rows := r.thingRows(n, things)
	if r.archiveHiding(n, q) == nil {
		rows.hidden = nil
	}
	out := arrangeRows(rows, selectRows(rows, q), q)
	page := ListPage{Items: make([]Thing, len(out.Order)), Total: out.Total, Groups: out.Groups}
	for j, i := range out.Order {
		page.Items[j] = things[i]
	}
	return page
}

// queryThings answers a list request for a noun: its store's own Query when
// it has one, else Store.List filtered, sorted, grouped and paged by
// queryAll. The query given the store's List holds the conditions and
// ListParams only, so a List that reads less is still right.
func (r *Runtime) queryThings(c Ctx, n *Noun, q ListQuery) (ListPage, error) {
	if n.Store.Query != nil {
		return n.Store.Query(c, q)
	}
	read := ListQuery{Filters: q.Filters, Archived: q.Archived, Args: q.Args}
	things, err := n.Store.List(c, read)
	if err != nil {
		return ListPage{}, err
	}
	return r.queryAll(n, things, q), nil
}

// answerOf is a page of things as the list verb answers it: each thing's
// props (names; every one when empty) with its id and label, and its group
// when the query groups.
func answerOf(n *Noun, page ListPage, q ListQuery, names []string) ListAnswer {
	perPage := max(0, q.PerPage)
	a := ListAnswer{Items: make([]map[string]any, 0, len(page.Items)), Page: 1, PerPage: perPage, Total: page.Total}
	key := n.keyProp()
	for j, t := range page.Items {
		row := thingRecord(n, t, names)
		if key != nil {
			row["id"] = fmt.Sprint(t[key.Name])
		}
		if label := n.displayLabel(func(name string) any { return t[name] }); label != "" {
			row["label"] = label
		}
		if j < len(page.Groups) {
			row["group"] = page.Groups[j]
		}
		a.Items = append(a.Items, row)
	}
	if perPage > 0 {
		pages := max(1, (page.Total+perPage-1)/perPage)
		a.Page = min(max(q.Page, 1), pages)
		if a.Page > 1 {
			a.Prev = a.Page - 1
		}
		if a.Page < pages {
			a.Next = a.Page + 1
		}
	}
	return a
}

// sortDirections are the spellings of a sort direction after the colon of
// a sort word, ascending first; a word with none sorts ascending.
var sortDirections = []string{"asc", "desc"}

// splitSortWord reads one sort word, prop[:asc] or prop:desc.
func splitSortWord(word string) (name string, desc bool, err error) {
	name, dir, found := strings.Cut(word, ":")
	at := slices.Index(sortDirections, dir)
	if found && at < 0 {
		return name, false, fmt.Errorf("direction is %s, not %q", strings.Join(sortDirections, " or "), dir)
	}
	return name, at > 0, nil
}

// parseSortWords reads sort keys written prop[:desc] (or :asc), comma- or
// space-separated, each prop a prop of n.
func parseSortWords(n *Noun, what, text string) ([]SortKey, error) {
	var out []SortKey
	for _, word := range listWords(text) {
		name, desc, err := splitSortWord(word)
		if err != nil {
			return nil, &ArgumentError{fmt.Errorf("%s %w", what, err)}
		}
		p := n.prop(name)
		if p == nil {
			return nil, &ArgumentError{fmt.Errorf("%s has no prop %q to %s by", n.Name, name, what)}
		}
		if p.Secret {
			return nil, &ArgumentError{fmt.Errorf("cannot %s by secret prop %q", what, name)}
		}
		k := SortKey{Field: p.Name, Desc: desc}
		if slices.ContainsFunc(out, func(o SortKey) bool { return o.Field == k.Field }) {
			return nil, &ArgumentError{fmt.Errorf("repeated %s prop %q", what, k.Field)}
		}
		out = append(out, k)
	}
	return out, nil
}

// parseGroupWords reads the group-by props of n, comma- or space-separated.
func parseGroupWords(n *Noun, text string) ([]string, error) {
	keys, err := parseSortWords(n, "group", text)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range keys {
		if k.Desc {
			return nil, &ArgumentError{errors.New("group-by takes prop names; order the groups with group-sort")}
		}
		out = append(out, k.Field)
	}
	return out, nil
}

// filterWordOps are the spellings of a prop condition in a filter word, the
// longest first so != is not read as =.
var filterWordOps = []struct{ text, op string }{
	{"!~", "not-contains"},
	{"!=", "!="},
	{"~", "contains"},
	{"=", "="},
}

// parseFilterWord reads one prop condition written prop=value, prop!=value,
// prop~value (contains) or prop!~value; a value written a|b is the
// alternatives of in (not-in with !=). ok is false for a word that is no
// condition (a saved filter's name).
func parseFilterWord(n *Noun, word string) (f PropFilter, ok bool, err error) {
	at, opText, op := -1, "", ""
scan:
	for i := 1; i < len(word); i++ {
		for _, w := range filterWordOps {
			if strings.HasPrefix(word[i:], w.text) {
				at, opText, op = i, w.text, w.op
				break scan
			}
		}
	}
	if at < 0 {
		return f, false, nil
	}
	name, value := word[:at], word[at+len(opText):]
	f = PropFilter{Prop: name, Op: op, Value: value}
	if strings.Contains(value, "|") && (op == "=" || op == "!=") {
		alts := strings.Split(value, "|")
		values := make([]any, len(alts))
		for i, a := range alts {
			values[i] = a
		}
		f.Value = values
		f.Op = map[string]string{"=": "in", "!=": "not-in"}[op]
	}
	typed, err := typedFilter(n, f)
	if err != nil {
		return f, true, &ArgumentError{err}
	}
	return typed, true, nil
}

// parseListFilter reads the filter argument: the name of a saved filter for
// n, else words each a prop condition (parseFilterWord). A word that is no
// condition and no saved filter's name is refused.
func (r *Runtime) parseListFilter(n *Noun, text string) ([]PropFilter, string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, "", nil
	}
	if filters, found, err := r.savedFilterNamed(n, text); err != nil || found {
		return filters, text, err
	}
	var out []PropFilter
	saved := ""
	for _, word := range listWords(text) {
		f, ok, err := parseFilterWord(n, word)
		if err != nil {
			return nil, "", err
		}
		if ok {
			out = append(out, f)
			continue
		}
		filters, found, err := r.savedFilterNamed(n, word)
		if err != nil {
			return nil, "", err
		}
		if !found {
			return nil, "", &ArgumentError{fmt.Errorf("filter takes prop=value words or a saved filter's name; unknown saved filter %q for %s", word, n.Name)}
		}
		out = append(out, filters...)
		saved = word
	}
	if len(listWords(text)) != 1 {
		saved = ""
	}
	return out, saved, nil
}

// pageArg reads a non-negative integer argument.
func pageArg(a Arguments, name string) int { return max(0, IntArg(a, name, 0)) }

// listQueryOf reads the list verb's arguments as the query they ask: the
// where pairs and filter words as conditions, sort, group-by, group-sort,
// page, per-page (limit is a first page of that many), archived, and the
// store's own ListParams.
func (r *Runtime) listQueryOf(n *Noun, a Arguments) (ListQuery, error) {
	var q ListQuery
	where, err := parseWhere(n, StringArg(a, "where", ""))
	if err != nil {
		return q, err
	}
	filters, saved, err := r.parseListFilter(n, StringArg(a, "filter", ""))
	if err != nil {
		return q, err
	}
	q.Filters = append(whereFilters(where), filters...)
	if len(where) == 0 {
		q.Saved = saved
	}
	if q.Sort, err = parseSortWords(n, "sort", StringArg(a, "sort", "")); err != nil {
		return q, err
	}
	if q.GroupBy, err = parseGroupWords(n, StringArg(a, "group-by", "")); err != nil {
		return q, err
	}
	if q.GroupSort, err = parseSortWords(n, "group-sort", StringArg(a, "group-sort", "")); err != nil {
		return q, err
	}
	for _, k := range q.GroupSort {
		if !slices.Contains(q.GroupBy, k.Field) {
			return q, &ArgumentError{fmt.Errorf("group-sort %q is not a group-by prop", k.Field)}
		}
	}
	q.Page, q.PerPage = pageArg(a, "page"), pageArg(a, "per-page")
	if limit := pageArg(a, "limit"); limit > 0 && q.PerPage == 0 {
		q.PerPage, q.Page = limit, 1
	}
	if b, ok := a["archived"].(bool); ok {
		q.Archived = b
	} else if s, ok := a["archived"].(string); ok {
		q.Archived, _ = strconv.ParseBool(s)
	}
	for name := range n.Store.ListParams {
		if v, ok := a[name]; ok {
			if q.Args == nil {
				q.Args = Arguments{}
			}
			q.Args[name] = v
		}
	}
	return q, nil
}
