package ochazuke

import (
	"fmt"
	"slices"
)

// Windowed lists (noun scaffold plan, a): a list From a noun whose store
// pages (Store.Query) holds one page of the store at a time, never all of
// it. Its filters, sort, groups, text query and archive toggle go into the
// query; the store answers the page, its group labels and the total. Page keys
// (collection.page-next, page-prev, page-first, page-last) read the
// neighbouring page. A store with a ChangeToken is not read again while the
// token and the query are unchanged. collection.window is the page size (0
// reads the store whole, as a list over List does).

// ListWindowSetting is the rows a list fed by a paging store reads per
// page; 0 reads every thing at once.
const ListWindowSetting = "collection.window"

// listReadSlot is the slot of a From list's read job (a remote store's,
// windowed or not): a newer read of the same list replaces an older one,
// and the list draws its loading line while one runs.
const listReadSlot = "window-read"

// listWindow is a windowed list's page: which one it shows, its size, the
// store's total and archived counts, and what the last read asked.
type listWindow struct {
	page, per, total int
	// archived is how many things the archive hiding leaves out of the
	// query's total (counted by the store), -1 when not counted.
	archived int
	// key is the query the rows on screen answer, token the store's
	// change token when they were read; asked is the query of a read
	// under way.
	key, token, asked string
	filled            bool
	// selectNext settles a page-edge motion when the read lands, including
	// remote reads; selectLast chooses the last row after moving up.
	selectNext, selectLast bool
}

// Windowed reports whether the list holds one page of a paging store.
func (l *Collection) Windowed() bool { return l.paged != nil }

// WindowTotal is how many things the windowed list's query matched in the
// store (its rows are one page of them); 0 for a list that is not windowed.
func (l *Collection) WindowTotal() int {
	if l.paged == nil {
		return 0
	}
	return l.paged.total
}

// startWindow makes a From list over a paging store windowed, unless
// collection.window is 0.
func (l *Collection) startWindow(n *Noun) {
	per := l.r.Settings().Int(ListWindowSetting)
	if n.Store.Query == nil || per <= 0 {
		return
	}
	l.paged = &listWindow{page: 1, per: per, archived: -1}
}

// windowQuery is the store query of the windowed list's page: the effective
// filters, sort, grouping, text and archive toggle, and the bounded page.
func (l *Collection) windowQuery() ListQuery {
	q := l.effectiveView()
	q.Filters, q.Sort = clonePropFilters(q.Filters), slices.Clone(q.Sort)
	q.GroupBy, q.GroupSort = slices.Clone(q.GroupBy), slices.Clone(q.GroupSort)
	q.Page, q.PerPage = l.paged.page, l.paged.per
	return q
}

// windowKey is a query as the window compares it.
func windowKey(q ListQuery) string {
	return fmt.Sprintf("%v|%v|%v|%v|%q|%v|%d|%d", q.Filters, q.Sort, q.GroupBy, q.GroupSort, q.Text, q.Archived, q.Page, q.PerPage)
}

// refillWindow reads the windowed list's page: at once from a local store,
// as a job from a remote one (the list draws its loading line meanwhile).
// Nothing is read while the store's token and the query are unchanged.
func (l *Collection) refillWindow(n *Noun, c Ctx) error {
	w, r := l.paged, l.r
	q := l.windowQuery()
	key := windowKey(q)
	token := storeToken(n.Store, c)
	if w.filled && key == w.key && token != "" && token == w.token {
		return nil
	}
	if key == w.asked {
		return nil // the same read is under way
	}
	arch := r.archiveHiding(n, q)
	var page ListPage
	archived := -1
	unmoved := false
	initialToken := token
	body := func(c Ctx) (any, error) {
		if token, unmoved = jobToken(n.Store, c, initialToken, w.token, w.filled && key == w.key); unmoved {
			return nil, nil
		}
		p, err := r.queryThings(c, n, q)
		if err != nil {
			return nil, err
		}
		page = p
		if arch != nil {
			hidden := q
			hidden.Archived, hidden.Page, hidden.PerPage = true, 1, 1
			hidden.Filters = append(clonePropFilters(q.Filters), PropFilter{Prop: arch.Name, Value: true})
			hp, err := r.queryThings(c, n, hidden)
			if err != nil {
				return nil, err
			}
			archived = hp.Total
		}
		return nil, nil
	}
	finish := func(_ any, err error) (any, error) {
		w.asked = ""
		if err != nil || unmoved {
			return nil, err
		}
		if pages := windowPages(page.Total, w.per); w.page > pages && page.Total > 0 {
			w.page = pages // the store shrank under the page: show its last page
			return nil, l.Refill()
		}
		w.total, w.archived, w.key, w.token, w.filled = page.Total, archived, key, token, true
		l.refills++
		items := r.thingItems(c, n, l.id, page.Items, l.columns)
		rows := r.thingRows(n, page.Items)
		for i := range items {
			if i < len(page.Groups) {
				items[i].Section = page.Groups[i]
			} else if q.Grouped() {
				items[i].Section = groupLabel(rows, i, q.GroupBy)
			}
		}
		if err := l.SetItems(items); err != nil {
			return nil, err
		}
		if w.selectNext {
			l.windowSelect()
		}
		return nil, nil
	}
	w.asked = key
	res := r.runSteppedBody(c, n.Name+".list", body, finish)
	if res.err != nil {
		w.asked = ""
		return res.err
	}
	if res.work == nil {
		return nil
	}
	_, err := r.Launch(l.id, n.Name+".list", res.Replacing(listReadSlot).Then(func(c Ctx, _ any, err error) {
		w.asked = ""
		if err != nil {
			c.Error(fmt.Sprintf("%s list: %v", r.DisplayNoun(n.Name), err))
		}
	}), c)
	return err
}

// windowPages is how many pages of per rows total things fill (at least 1).
func windowPages(total, per int) int {
	if per <= 0 || total <= per {
		return 1
	}
	return (total + per - 1) / per
}

// windowArrange is arrange for a windowed list: the rows are the store's
// page already (filtered, sorted, paged), so it only notes where the page
// sits; the rows are not paged again.
func (l *Collection) windowArrange(items []ListItem) []ListItem {
	w := l.paged
	l.total, l.grouped, l.headCap = w.total, l.effectiveView().Grouped(), 0
	for i, item := range items {
		if i == 0 || item.Section != items[i-1].Section {
			l.headCap++
		}
	}
	l.pages = windowPages(w.total, w.per)
	l.vs.Page = w.page
	return items
}

// windowStale notes a windowed list whose view no longer matches the rows
// on screen; it reads its page once the request running has run
// (readStaleWindows), never inside the refilter that noticed it.
func (l *Collection) windowStale() {
	w := l.paged
	if w == nil || !w.filled || w.asked != "" {
		return
	}
	q := l.windowQuery()
	if windowKey(q) == w.key {
		return
	}
	h := l.r.watchHub()
	if !slices.Contains(h.stale, l.id) {
		h.stale = append(h.stale, l.id)
	}
}

// readStaleWindows reads the page of every windowed list noted stale.
func (r *Runtime) readStaleWindows() {
	h := r.watches
	if h == nil || len(h.stale) == 0 {
		return
	}
	ids := h.stale
	h.stale = nil
	defer r.syncIndexForm()
	for _, id := range ids {
		if l := r.List(id); l != nil {
			l.windowChanged()
		}
	}
}

// windowChanged reads the page again when the list's view (filters, sort,
// text, archive toggle) no longer matches the rows on screen: page 1 of
// the new query.
func (l *Collection) windowChanged() {
	w := l.paged
	if w == nil || !w.filled || w.asked != "" {
		return
	}
	q := l.windowQuery()
	q.Page = w.page
	if windowKey(q) == w.key {
		return
	}
	at := w.page
	w.page = 1
	if err := l.Refill(); err != nil {
		w.page = at
		l.r.UI().Error(fmt.Sprintf("%s list: %v", l.r.DisplayNoun(l.from), err))
	}
}

// setWindowPage reads page n (from 1, clamped) of a windowed list.
func (l *Collection) setWindowPage(n int) error {
	w := l.paged
	n = max(1, min(n, windowPages(w.total, w.per)))
	if n == w.page && w.filled {
		return nil
	}
	w.page = n
	return l.Refill()
}

// windowEdge moves a windowed list past the edge of its page onto the
// neighbouring page when there is one (j on the last row loads the next
// page, its first row selected; k on the first row the previous page, its
// last row selected) and reports whether it did; at the store's ends the
// move spills as on any list.
func (l *Collection) windowEdge(delta int) bool {
	w := l.paged
	if w == nil || !w.filled || l.selected < 0 {
		return false
	}
	pages := windowPages(w.total, w.per)
	switch {
	case delta > 0 && l.selected == len(l.items)-1 && w.page < pages:
		w.selectNext = true
		w.selectLast = false
		_ = l.setWindowPage(w.page + 1)
		return true
	case delta < 0 && l.selected == 0 && w.page > 1:
		w.selectNext, w.selectLast = true, true
		_ = l.setWindowPage(w.page - 1)
		return true
	}
	return false
}

// windowSelect selects the first row of a page just read, or its last
// after a move up; a read still under way selects when it lands.
func (l *Collection) windowSelect() {
	w := l.paged
	if w.asked != "" || len(l.items) == 0 {
		return
	}
	at := 0
	if w.selectLast {
		at = len(l.items) - 1
	}
	w.selectNext, w.selectLast = false, false
	_ = l.Select(l.items[at].ID)
	if r := l.r; slices.Contains(r.InstancePath(r.Focus), l.id) {
		r.changeFocus(l.focusTarget(), focusMove{cause: focusSettle})
	}
}

// windowArchived is how many archived things the windowed list's store
// counts as hidden by the query; -1 when not counted.
func (l *Collection) windowArchived() int {
	if l.paged == nil {
		return -1
	}
	return l.paged.archived
}

// storeToken is the store's change token read now, "" when it keeps none or
// cannot answer here (a remote store answers only inside a job).
func storeToken(s *Store, c Ctx) string {
	if s.ChangeToken == nil {
		return ""
	}
	t, err := s.ChangeToken(c)
	if err != nil {
		return ""
	}
	return t
}

// jobToken reads the token inside a read's job when none was read before it
// (a remote store, a mounted noun, answers only there): the token, and
// whether it is the one the reader last saw (seen) while what it shows is
// that read's (current), so nothing more need be read. token is the immutable
// loop-read token: replaying a body must consume the same calls each time.
func jobToken(s *Store, c Ctx, token, seen string, current bool) (string, bool) {
	if token != "" {
		return token, false
	}
	t := storeToken(s, c)
	return t, t != "" && current && t == seen
}
