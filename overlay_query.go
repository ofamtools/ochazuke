package ochazuke

import (
	"maps"
	"slices"
)

// The overlay over a paged store (overlay-every-store OV3): Store.Query and
// Store.ChangeToken of a filled store. A page is asked of the source once
// and merged row by row; the source is never read whole to lay the
// overlay over it (a 20,000-row tool list pages as before).

// query is a filled store's Query: one call to the source's Query for the
// page, each of its things merged with its overlay row (one lookup), a
// thing the user hid left out, and the things of the user's own after the
// source's, on the last page. Conditions on a prop the overlay gave the
// noun (archived, hidden: the source knows nothing of them) are kept from
// the source and applied to the merged page; when such a condition can
// match only a thing with a row (archived=true, hidden=true) the page is
// read from the rows alone (rowsPage). A thing whose row holds a value is
// checked again against the request's conditions and the archive hiding
// with that value; a thing with none keeps the source's verdict.
//
// Total is the source's total, less the things the user hid or archived
// (every row's, for a request with no condition, search or list argument
// the source applies; else those this page left out), plus the user's own
// things that match.
func (o *overlayOf) query(c Ctx, q ListQuery) (ListPage, error) {
	r, n := o.r, o.n
	rows := r.overlayNounRows(n.Name)
	var mine, theirs []PropFilter
	for _, f := range q.Filters {
		if slices.Contains(o.added, f.Prop) {
			mine = append(mine, f)
		} else {
			theirs = append(theirs, f)
		}
	}
	if len(mine) > 0 && r.queryAll(n, []Thing{{}}, ListQuery{Filters: mine, Archived: true}).Total == 0 {
		return o.rowsPage(c, q, rows, mine)
	}
	sq := q
	sq.Filters = theirs
	page, err := o.srcQuery(c, sq)
	if err != nil {
		return ListPage{}, err
	}
	check := ListQuery{Filters: q.Filters, Archived: q.Archived}
	out := ListPage{Items: make([]Thing, 0, len(page.Items))}
	dropped := 0
	for i, t := range page.Items {
		row := rows[o.keyOf(t)]
		if row != nil {
			t = r.mergeOver(row, t)
			if o.hides(t, q) || r.queryAll(n, []Thing{t}, check).Total == 0 {
				dropped++
				continue
			}
		}
		out.Items = append(out.Items, t)
		if i < len(page.Groups) {
			out.Groups = append(out.Groups, page.Groups[i])
		}
	}
	gone := dropped
	if len(theirs) == 0 && q.Text == "" && len(q.Args) == 0 {
		// The source answered every thing: each row that leaves its thing
		// out (hidden, archived by the user) takes one from its total.
		gone = 0
		for key, row := range rows {
			if t := createdThing(o.keyName, key, row); !row.Created && (o.hides(t, q) || r.queryAll(n, []Thing{t}, check).Total == 0) {
				gone++
			}
		}
	}
	at := max(0, page.Total-gone) // where the user's own things start
	made := o.made(rows, q)
	out.Total = at + len(made.Items)
	lo, hi := 0, len(made.Items)
	if q.PerPage > 0 {
		start := (max(q.Page, 1) - 1) * q.PerPage
		if start >= page.Total {
			out.Items, out.Groups = out.Items[:0], nil // past the source's last page: it answered another
		}
		lo, hi = min(max(0, start-at), hi), min(max(0, start+q.PerPage-at), hi)
	}
	out.Items = append(out.Items, made.Items[lo:hi]...)
	if q.Grouped() && len(made.Groups) >= hi {
		out.Groups = append(out.Groups, made.Groups[lo:hi]...)
	}
	return out, nil
}

// made are the things of the user's own (rows marked Created) a request
// lists, in key order, sorted and grouped as it asks, unpaged.
func (o *overlayOf) made(rows map[string]*OverlayRow, q ListQuery) ListPage {
	var things []Thing
	for _, key := range slices.Sorted(maps.Keys(rows)) {
		if row := rows[key]; row.Created {
			if t := createdThing(o.keyName, key, row); !o.hides(t, q) {
				things = append(things, t)
			}
		}
	}
	all := q
	all.Page, all.PerPage = 0, 0
	return o.r.queryAll(o.n, things, all)
}

// rowsPage is a page read from the overlay rows alone, for a request whose
// conditions on the overlay's own props (mine) only a thing with a row
// matches: the rows that match, in key order, paged as asked; each thing on
// the page is read from the source with Get (one call each; a thing of the
// user's own needs none), and Total is how many rows match.
func (o *overlayOf) rowsPage(c Ctx, q ListQuery, rows map[string]*OverlayRow, mine []PropFilter) (ListPage, error) {
	r, n := o.r, o.n
	var keys []string
	for _, key := range slices.Sorted(maps.Keys(rows)) {
		t := createdThing(o.keyName, key, rows[key])
		if !o.hides(t, q) && r.queryAll(n, []Thing{t}, ListQuery{Filters: mine, Archived: true}).Total == 1 {
			keys = append(keys, key)
		}
	}
	out := ListPage{Total: len(keys), Items: []Thing{}}
	if q.PerPage > 0 {
		lo := min((max(q.Page, 1)-1)*q.PerPage, len(keys))
		keys = keys[lo:min(lo+q.PerPage, len(keys))]
	}
	for _, key := range keys {
		row := rows[key]
		t := createdThing(o.keyName, key, row)
		if !row.Created && o.srcGet != nil {
			got, err := o.srcGet(c, key)
			if err != nil {
				continue // gone from the source: its row is still on ochazuke.overlay
			}
			t = r.mergeOver(row, got)
		}
		out.Items = append(out.Items, t)
	}
	if q.Grouped() {
		arranged := q
		arranged.Page, arranged.PerPage, arranged.Filters, arranged.Archived = 0, 0, nil, true
		out.Groups = r.queryAll(n, out.Items, arranged).Groups
	}
	return out, nil
}

// token is a filled store's ChangeToken: the source's token joined with
// the overlay's version of the noun (overlayVersion), so a write to the
// overlay moves it with no call to the source. A source token that is
// unknown ("") stays unknown, and a noun with no rows keeps the source's
// token as it is.
func (o *overlayOf) token(c Ctx, src func(Ctx) (string, error)) (string, error) {
	t, err := src(c)
	if err != nil || t == "" {
		return t, err
	}
	if v := o.r.overlayVersion(o.n.Name); v != "" {
		t += "/yours-" + v
	}
	return t, nil
}
