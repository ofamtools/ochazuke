package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ListPerPageSetting is the rows per page of the lists the widget draws (0:
// every row on one page).
const ListPerPageSetting = "collection.per-page"

// ListGroupBySetting names the props the lists the widget draws group their
// rows by (empty: no groups).
const ListGroupBySetting = "collection.group-by"

// ListView is a copy of the list's view state: the one ListQuery the list is
// filtered, sorted, grouped and paged by, the same value (and the same
// functions, list_query.go) the standard list verb runs.
func (l *Collection) ListView() ListQuery {
	q := l.vs
	q.Filters = clonePropFilters(q.Filters)
	q.Sort = slices.Clone(q.Sort)
	q.GroupBy = slices.Clone(q.GroupBy)
	q.GroupSort = slices.Clone(q.GroupSort)
	return q
}

// arranges reports whether the list groups and pages its rows: a flat list
// that owns its rows. A tree keeps its own shape, and a detached filter
// controller (a chooser's) serves rows another view draws.
func (l *Collection) arranges() bool { return l.tree() == nil && l.filterChanged == nil }

// groupSetting is collection.group-by as props of these rows; none when any is
// not a prop they have.
func (l *Collection) groupSetting() []string {
	words := listWords(l.r.Settings().String(ListGroupBySetting))
	if len(words) == 0 {
		return nil
	}
	specs := l.sortSpecs()
	for _, w := range words {
		if _, ok := specs[w]; !ok {
			return nil
		}
	}
	return words
}

// effectiveView is the view with the settings' defaults filled in: the
// list's own grouping and page size, else collection.group-by and collection.per-page.
func (l *Collection) effectiveView() ListQuery {
	q := l.vs
	if !l.arranges() {
		q.GroupBy, q.GroupSort, q.PerPage = nil, nil, 0
		return q
	}
	if q.PerPage <= 0 {
		q.PerPage = max(0, l.r.Settings().Int(ListPerPageSetting))
	}
	if len(q.GroupBy) == 0 {
		q.GroupBy, q.GroupSort = l.groupSetting(), nil
	}
	return q
}

// arrange sorts, groups and pages the filtered rows by the view, and notes
// where the page sits. The row keep shows on the page that holds it.
func (l *Collection) arrange(items []ListItem, keep string) []ListItem {
	if l.paged != nil {
		return l.windowArrange(items)
	}
	items = l.sortedRows(l.lingerRows(items))
	q := l.effectiveView()
	l.total, l.pages, l.grouped, l.headCap = len(items), 0, false, 0
	if !q.Grouped() && !q.Paged() {
		return items
	}
	rows := l.queryRows(items)
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	q.Sort = nil // the rows came sorted
	out := arrangeRows(rows, idx, q)
	if keep != "" && q.Paged() && !slices.ContainsFunc(out.Order, func(i int) bool { return items[i].ID == keep }) {
		whole := q
		whole.PerPage = 0
		if all := arrangeRows(rows, idx, whole); len(all.Order) > 0 {
			if at := slices.IndexFunc(all.Order, func(i int) bool { return items[i].ID == keep }); at >= 0 {
				q.Page = at/q.PerPage + 1
				out = arrangeRows(rows, idx, q)
			}
		}
	}
	if q.Paged() {
		l.vs.Page, l.pages = out.Page, out.Pages
	}
	l.grouped, l.headCap = out.Groups != nil, out.MaxRuns
	shown := make([]ListItem, len(out.Order))
	for j, i := range out.Order {
		shown[j] = items[i]
		if out.Groups != nil {
			shown[j].Section = out.Groups[j]
		}
	}
	return shown
}

// Page is the page the list shows, from 1, and Pages how many there are
// (both 1 when the list is not paged).
func (l *Collection) Page() (page, pages int) {
	if l.pages == 0 {
		return 1, 1
	}
	return l.vs.Page, l.pages
}

// pageSummary is the page indicator of the filter line: "page 2/5" while
// the list is paged (collection.per-page), "" otherwise. It shows on a one-page
// list too, so the list keeps its height as filters change the page count.
func (l *Collection) pageSummary() string {
	if l.pages == 0 {
		return ""
	}
	page := "page " + strconv.Itoa(l.vs.Page) + "/" + strconv.Itoa(l.pages)
	if l.paged != nil {
		page += " of " + strconv.Itoa(l.paged.total) // a windowed list holds one page of the store
	}
	return page
}

// ArchivedCountSetting shows how many archived rows a list hides
// ("3 archived hidden") on its filter line; off shows nothing.
const ArchivedCountSetting = "collection.archived-count"

// archivedSummary is "N archived hidden" while the list hides archived
// rows (collection.hide-archived, its archive toggle off): counted by the
// store for a windowed list, among the rows for any other.
func (l *Collection) archivedSummary() string {
	if l.vs.Archived || !l.r.Settings().Bool(ArchivedCountSetting) {
		return ""
	}
	n := l.archived
	if l.paged != nil {
		n = l.windowArchived()
	}
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n) + " archived hidden"
}

// viewSummary is the view information the filter line shows beside the
// conditions: the sort and the page.
func (l *Collection) viewSummary() string {
	var parts []string
	for _, s := range []string{l.sortSummary(), l.pageSummary(), l.archivedSummary()} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// SetPage shows page n (from 1); one past the ends shows the end. A list
// that does not page refuses.
func (l *Collection) SetPage(n int) error {
	if l.paged != nil {
		return l.setWindowPage(n)
	}
	if l.pages == 0 {
		return errors.New("the list is not paged")
	}
	n = min(max(n, 1), l.pages)
	if n == l.vs.Page {
		return nil
	}
	l.vs.Page = n
	l.rearranged("")
	return nil
}

// SetGroupBy groups the rows by these props (none: the collection.group-by
// setting's, which defaults to no groups) and orders the groups by
// groupSort, whose keys name props of by.
func (l *Collection) SetGroupBy(by []string, groupSort []SortKey) error {
	if !l.arranges() {
		return errors.New("this list does not group its rows")
	}
	specs := l.sortSpecs()
	by = slices.Clone(by)
	for i, f := range by {
		name, err := l.r.filterPropName(l.rowNouns(), f)
		if err != nil {
			return err
		}
		if _, ok := specs[name]; !ok {
			return fmt.Errorf("unknown or disabled group field %q", f)
		}
		by[i] = name
	}
	groupSort = slices.Clone(groupSort)
	for _, k := range groupSort {
		if !slices.Contains(by, k.Field) {
			return fmt.Errorf("group order %q is not a group field", k.Field)
		}
	}
	l.vs.GroupBy, l.vs.GroupSort, l.vs.Page = by, groupSort, 1
	previous, _ := l.Selected()
	l.rearranged(previous.ID)
	return nil
}

// rearranged shows the rows again after the view's grouping or page changed,
// keeping the row keep selected (on whichever page it lands) or, with "",
// starting at the first row.
func (l *Collection) rearranged(keep string) {
	previous, _ := l.Selected()
	l.refilter(keep)
	l.syncVisual()
	l.syncFocus(previous, nil)
	l.filterHeard()
}

func registerListQuery(r *Runtime) {
	r.InvalidateBindings()
	resolve := func(focus string, a Arguments) *Collection { return r.rowFilterList(StringArg(a, "id", focus)) }
	r.DefineGuard("list-paged", func(_ *Runtime, ev KeyEvent) bool {
		l := resolve(ev.Focus, ev.Args)
		return l != nil && l.pages > 1
	})
	r.DefineGuard("list-groupable", func(_ *Runtime, ev KeyEvent) bool {
		l := resolve(ev.Focus, ev.Args)
		return l != nil && l.arranges() && len(l.sortSpecs()) > 0
	})
	r.Settings().Observe(ListPerPageSetting, func(string, any) { r.refilterLists() })
	r.Settings().Observe(ListGroupBySetting, func(string, any) { r.refilterLists() })
	for _, s := range []struct {
		verb, description, guard string
		params                   map[string]Parameter
		run                      func(*Collection, Arguments) error
		positional               []string
	}{
		{"page-next", "Show the next page of a paged list", "list-paged", nil, func(l *Collection, _ Arguments) error { return l.SetPage(l.vs.Page + 1) }, nil},
		{"page-prev", "Show the previous page of a paged list", "list-paged", nil, func(l *Collection, _ Arguments) error { return l.SetPage(l.vs.Page - 1) }, nil},
		{"page-first", "Show the first page of a paged list", "list-paged", nil, func(l *Collection, _ Arguments) error { return l.SetPage(1) }, nil},
		{"page-last", "Show the last page of a paged list", "list-paged", nil, func(l *Collection, _ Arguments) error { return l.SetPage(l.pages) }, nil},
		{"group", "Group the rows by comma-separated props, each with a heading; empty restores collection.group-by", "list-groupable", map[string]Parameter{"by": {Type: "string", Description: "Props to group by, comma-separated"}, "order": {Type: "string", Description: "Order the groups by these group props, each prop[:desc]"}}, func(l *Collection, a Arguments) error {
			keys, err := l.groupOrder(StringArg(a, "order", ""))
			if err != nil {
				return err
			}
			return l.SetGroupBy(listWords(StringArg(a, "by", "")), keys)
		}, []string{"by", "order"}},
		{"group-clear", "Restore the grouping collection.group-by gives (none by default)", "list-groupable", nil, func(l *Collection, _ Arguments) error { return l.SetGroupBy(nil, nil) }, nil},
		{"group-fold", "Fold or unfold the named group, or the selected row's group; counts are matched rows on this page", "list-groupable", map[string]Parameter{"group": {Type: "string", Description: "Group label; omitted, the selected row's group"}, "closed": {Type: SettingBoolean, Description: "true folds, false unfolds; omitted toggles"}}, func(l *Collection, a Arguments) error {
			return l.foldGroup(a)
		}, []string{"group"}},
		{"show-archived", "Show or hide the list's archived rows (collection.hide-archived hides them); hidden, the filter line says how many (collection.archived-count)", "", map[string]Parameter{"show": {Type: SettingBoolean, Description: "true shows them, false hides them; omitted, it flips"}}, func(l *Collection, a Arguments) error {
			show := !l.vs.Archived
			if v, ok := a["show"].(bool); ok {
				show = v
			}
			l.SetShowArchived(show)
			return nil
		}, []string{"show"}},
	} {
		params := s.params
		if params == nil {
			params = map[string]Parameter{}
		}
		params["id"] = Parameter{Type: "string", Description: "List or table; default focused row view"}
		run := s.run
		r.Register(Operation{Key: KeyOf("ochazuke.collection", s.verb), Description: s.description, Parameters: params, Positional: s.positional, Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
			l := resolve(focusOf(c), a)
			if l == nil {
				return nil, errors.New("no list to arrange")
			}
			if err := run(l, a); err != nil {
				return nil, err
			}
			return l.state(), nil
		}})
		r.Nouns["ochazuke.collection"].Guards[s.verb] = s.guard
	}
}

// SetShowArchived shows (true) or hides the list's archived rows: its
// view's Archived, which the archive hiding reads (archiveHiding); a
// windowed list reads its page again.
func (l *Collection) SetShowArchived(show bool) {
	if l.vs.Archived == show {
		return
	}
	l.vs.Archived = show
	keep := ""
	if item, ok := l.Selected(); ok {
		keep = item.ID
	}
	l.rearranged(keep)
	l.r.readStaleWindows()
}

// ShowsArchived reports whether the list shows its archived rows.
func (l *Collection) ShowsArchived() bool { return l.vs.Archived }

// groupOrder reads group order keys written prop[:desc].
func (l *Collection) groupOrder(text string) ([]SortKey, error) {
	var out []SortKey
	for _, word := range listWords(text) {
		name, desc, err := splitSortWord(word)
		if err != nil {
			return nil, err
		}
		if name, err = l.r.filterPropName(l.rowNouns(), name); err != nil {
			return nil, err
		}
		out = append(out, SortKey{Field: name, Desc: desc})
	}
	return out, nil
}

// refilterLists shows every list's rows again (a setting they read changed).
func (r *Runtime) refilterLists() {
	for _, inst := range r.Instances {
		if l, ok := inst.Component.(*Collection); ok {
			prev, _ := l.Selected()
			l.refilter(prev.ID)
			l.syncFocus(prev, nil)
		}
	}
}
