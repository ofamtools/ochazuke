package ochazuke

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// The list filter: "/" on any focused list (page lists, tables, a
// catalog page's lists, the Settings page's) opens a one-line query above
// its rows, and typing narrows the rows to those whose label or cells hold
// every word of the query, in list order, comparing case as search.case
// says (Runtime.CaseFor: ignored unless the query holds a capital, by
// default). The filter hides rows from drawing and from motions (j/k,
// counts, spills) without removing them: their instances, marks and data
// stay, SetItems while filtered filters the new rows, and clearing the
// filter shows them all.
//
// The query is typed in ListFilterMode, a base mode whose rows are on the
// list noun: printable keys and space type, backspace erases (and on an
// empty query leaves the mode), up/down and ctrl+n/ctrl+p move among the
// matches without spilling, enter keeps the filter (normal mode, the query
// line stays, "/" edits it again), esc clears it and puts the selection
// back where it was when "/" was pressed, ctrl+u empties the query. In
// normal mode esc on a filtered list clears the filter.
//
// A list added with ClearFilterOnEnter (a list one picks a row from)
// answers enter with list.filter-choose instead: the filter clears, the
// selection stays on the match enter was pressed on, every row shows, the
// mode is normal and the window follows the row, as esc after enter does
// on any list. Its verb guard, list-chooses, holds only on such a list
// with a match selected, so with no match enter falls through to
// list.filter-keep and the footer names whichever row answers.

// ListFilterMode is the mode the filter line is typed in.
const ListFilterMode = "list_filter"

// filterOpenRow is the row that opens the filter line: "/" on app in
// normal mode guarded by list-filterable, like J/K detail, so an
// application's own "/" on any level (a row noun, the list, its page, app)
// answers first. NewModel adds it after its other defaults, so on a
// crowded footer "/ filter" is the hint cut first.
func filterOpenRow() Binding {
	return Do("ochazuke.list.filter-open").Guard("list-filterable").Hinted("filter")
}

// filterHay is the text a row is matched against: its label, cells and
// Search text, as m compares them, one per line, so a word never spans two
// cells and a regex's ^ and $ match at each cell's ends.
func filterHay(item ListItem, m Matcher) string {
	parts := make([]string, 0, 1+len(item.Cells)+len(item.Search))
	parts = append(append(append(parts, item.Label), item.Cells...), item.Search...)
	return m.Prepare(strings.Join(parts, "\n"))
}

// OnFilter makes fn hear every change of the list's filter: the text query
// (typing, erasing, clearing, a match mode change, a command that reveals a
// hidden row) and the prop conditions (SetPropFilters, ApplyFilter), on flat
// lists and trees alike, after the rows and selection follow it; nil stops it. For a host
// that draws something by the list's matches, as the Settings page's
// settings follow its section list's filter.
func (l *List) OnFilter(fn func(*List)) { l.onFilter = fn }

// SetMatchNoun names the list's rows, plural (the placeholder's Plural):
// its empty lines read "no NOUN", "no NOUN match 'QUERY'". "" names them
// "rows".
func (l *List) SetMatchNoun(plural string) { l.placeholder.Plural = plural }

// SetPlaceholder sets what the list draws with no rows: its Empty and
// Loading lines and the job that fills it (Placeholder).
func (l *List) SetPlaceholder(p Placeholder) { l.placeholder = p }

// Placeholder is what the list draws with no rows (SetPlaceholder).
func (l *List) Placeholder() Placeholder { return l.placeholder }

func (l *List) filterHeard() {
	if l.onFilter != nil {
		l.onFilter(l)
	}
}

// MatchMode is the filter's match mode: the one search.cycle-match or
// search.set-match chose for this list, else the search.match setting.
func (l *List) MatchMode() string {
	if l.match != "" {
		return l.match
	}
	return l.r.SearchMatch()
}

// matcher is the filter query compiled in the list's match mode.
func (l *List) matcher() Matcher { return l.r.Matcher(l.MatchMode(), l.vs.Text) }

// The matchSearcher contract (search_match.go): the mode decides the
// filter while its line shows.
func (l *List) matchMode() string  { return l.MatchMode() }
func (l *List) matchApplies() bool { return l.vs.Text != "" || l.editingFilter() }
func (l *List) setMatchMode(mode string) {
	l.match = mode
	l.SetFilter(l.vs.Text)
}

// Filter is the list's filter query ("" when every row is shown).
func (l *List) Filter() string { return l.vs.Text }

// Filtering reports whether the filter line shows: a filter is set, or
// its query is being typed on this list.
func (l *List) Filtering() bool {
	return l.vs.Text != "" || l.editingFilter() || len(l.vs.Filters) > 0 || l.vs.Saved != ""
}

// Height is the lines the list needs to draw every row without
// windowing (a table's header aside): Len, plus the blank lines above rows that
// ask for space (ListItem.SpaceAbove) and the filter line while it
// shows. A host that sizes a list to its rows sizes it with Height, so
// opening the filter line neither windows the rows nor moves what is drawn
// below the list as the filter narrows it. A paged list is one page tall
// (its rows, the most headings any page takes, and the page indicator), on
// every page.
func (l *List) Height() int {
	gaps := l.spaceLines() // blank lines above SpaceAbove rows
	rows := l.Len()
	if perPage := l.effectiveView().PerPage; perPage > 0 {
		rows = min(rows, perPage) // a paged list is one page tall whichever page shows
	}
	if l.vs.Text != "" || l.editingFilter() || l.propStrip() != "" || l.viewSummary() != "" {
		return rows + gaps + 1
	}
	if l.Filtering() {
		return max(1, rows+gaps)
	}
	return rows + gaps
}

// Shown is the number of rows the filter lets through (Len without one).
func (l *List) Shown() int { return len(l.items) }

// SetFilter sets the filter query and shows the rows that match it; ""
// shows every row. The selection stays on its row when that row still
// shows, else it moves to the first match (none when nothing matches);
// the focus follows it.
func (l *List) SetFilter(query string) {
	previous, _ := l.Selected()
	l.linger.reset()
	l.vs.Text = query
	keep := previous.ID
	if keep == "" {
		keep = l.back // nothing matched: widening the query finds the row / started on
	}
	l.refilter(keep)
	l.syncFocus(previous, nil)
	l.filterHeard()
}

// refilter recomputes the shown rows from every row and the query, then
// selects keep when it shows, else the first shown row.
func (l *List) refilter(keep string) {
	hidden := l.archiveHidden()
	l.archived = 0
	for _, item := range l.all {
		if hidden[item.ID] {
			l.archived++
		}
	}
	if t := l.tree(); t != nil && len(l.vs.Filters) > 0 {
		kept := t.filterNodes(hidden)
		l.items = make([]ListItem, 0, len(l.all))
		for _, item := range l.all {
			if kept[item.ID] {
				l.items = append(l.items, item)
			}
		}
	} else if strings.TrimSpace(l.vs.Text) == "" && len(l.vs.Filters) == 0 {
		l.items = l.all
		if l.archived > 0 {
			l.items = make([]ListItem, 0, len(l.all)-l.archived)
			for _, item := range l.all {
				if !hidden[item.ID] {
					l.items = append(l.items, item)
				}
			}
		}
	} else {
		m := l.matcher()
		shown := make([]ListItem, 0, len(l.all))
		for _, item := range l.all {
			if !hidden[item.ID] && l.matchesProps(item) && m.MatchPrepared(l.preparedRow(item, m)) {
				shown = append(shown, item)
			}
		}
		l.items = l.withKin(shown, hidden)
	}
	l.items = l.arrange(l.sortedRows(l.lingerRows(l.items)), keep)
	if t := l.tree(); t != nil {
		t.guideRows()
	}
	l.selected = slices.IndexFunc(l.items, func(item ListItem) bool { return item.ID == keep })
	if t := l.tree(); t != nil && l.selected < 0 {
		for p := t.parentOf(keep); p != "" && l.selected < 0; p = t.parentOf(p) {
			l.selected = slices.IndexFunc(l.items, func(item ListItem) bool { return item.ID == p })
		}
		if l.selected < 0 {
			l.selected = slices.IndexFunc(l.items, func(item ListItem) bool { return item.ID == l.back })
		}
	}
	if l.selected < 0 && len(l.items) > 0 {
		l.selected = 0
	}
	l.offset = min(l.offset, max(0, len(l.items)-1))
}

// ListFilterKeepsChildrenSetting makes the filter keep a matching row's
// children and a matching child's parent (ListItem.Under).
const ListFilterKeepsChildrenSetting = "list.filter-keeps-children"

// withKin adds to the matched rows the rows tied to them by Under: the
// children of a matched row and the parents of a matched child, so a
// child is never seen without its parent nor a parent without its items.
// Rows come back in list order.
func (l *List) withKin(shown []ListItem, hidden map[string]bool) []ListItem {
	if !l.r.Settings().Bool(ListFilterKeepsChildrenSetting) {
		return shown
	}
	keep := make(map[string]bool, len(shown))
	for _, item := range shown {
		keep[item.ID] = true
	}
	grew := false
	for _, item := range l.all {
		if item.Under == "" || hidden[item.ID] {
			continue
		}
		switch {
		case keep[item.ID] && !keep[item.Under] && !hidden[item.Under]:
			keep[item.Under], grew = true, true
		case keep[item.Under] && !keep[item.ID]:
			keep[item.ID], grew = true, true
		}
	}
	if !grew {
		return shown
	}
	out := make([]ListItem, 0, len(keep))
	for _, item := range l.all {
		if keep[item.ID] {
			out = append(out, item)
		}
	}
	return out
}

// shows reports whether the row with id is among the shown rows.
func (l *List) shows(id string) bool {
	return slices.ContainsFunc(l.items, func(item ListItem) bool { return item.ID == id })
}

// reveal clears the filter when id is a row it hides, so a command that
// lands on that row (Select, focus.to, a job's focus) shows it. The
// selection is the caller's to set.
func (l *List) reveal(id string) {
	if l.shows(id) || (!slices.ContainsFunc(l.all, func(item ListItem) bool { return item.ID == id }) && !l.treeHasNode(id)) {
		return
	}
	if l.vs.Text == "" && len(l.vs.Filters) == 0 {
		if l.pages > 1 { // a row of another page: show its page
			l.refilter(id)
			l.filterHeard()
		}
		return
	}
	l.linger.reset()
	l.vs.Text, l.back, l.vs.Filters, l.vs.Saved = "", "", nil, ""
	l.refilter(id)
	if t := l.tree(); t != nil {
		_ = t.Select(id)
	}
	l.filterHeard()
}

// editingFilter reports whether the filter line is being typed: the mode
// is ListFilterMode and the list holds the focus, as the list's surface is
// drawn (a list beneath a modal opened while filtering keeps its filter
// line and its matches; on the top surface it is the live state).
func (l *List) editingFilter() bool {
	_, _, focus, mode, _ := l.r.drawnAt(l.id)
	return mode.Name == ListFilterMode && l.holds(focus)
}

// filterLine is the line drawn above the rows while the filter is typed or
// kept: "/" and the query (with a reversed cursor cell while typing) on
// the left; on the right how many rows match (or why none can: an invalid
// regex), then the match mode's tag (search.show-match). "" when there is
// none.
func (l *List) filterLine(width int) string {
	editing := l.editingFilter()
	strip := l.propStrip()
	sort := l.viewSummary()
	if l.vs.Text == "" && !editing && strip == "" && sort == "" {
		return ""
	}
	left := strip
	if l.vs.Text != "" || editing {
		if left != "" {
			left += " "
		}
		left += "/" + modalLabel(l.vs.Text)
	}
	if editing {
		left += SelectionStyle(true).Render(" ")
	}
	if sort != "" {
		if left != "" {
			left += " · "
		}
		left += sort
	}
	var right []string
	if strings.TrimSpace(l.vs.Text) != "" || strip != "" {
		switch err := l.matcher().Err(); {
		case err != nil:
			right = append(right, err.Error())
		case len(l.items) == 0:
			right = append(right, "no "+pluralOr(l.placeholder.Plural)+" match")
		default:
			right = append(right, strconv.Itoa(l.total)+" of "+strconv.Itoa(l.Len()))
		}
	}
	if tag := l.r.matchTagFor(l.MatchMode()); tag != "" && (l.vs.Text != "" || editing) {
		right = append(right, tag)
	}
	return searchLine(left, strings.Join(right, "  "), width)
}

// searchLine is left and right on one line width cells wide, right
// against the right edge; when both do not fit, left alone, cut.
func searchLine(left, right string, width int) string {
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if right == "" || gap < 1 {
		return ansi.Truncate(left, width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

// focusedList is the list whose row (or which) is the focus, or nil the focus is a list or one of its rows
// (an input inside a list is not one).
func (r *Runtime) focusedList(focus string) *List {
	if r.Input(focus) != nil {
		return nil
	}
	if l := r.List(focus); l != nil {
		return l
	}
	if owner := r.rows[focus]; owner != "" {
		return r.List(owner)
	}
	return nil
}

func registerListFilter(r *Runtime) {
	// Turning list.hide-archived on or off shows or hides archived rows now.
	r.Settings().Observe(HideArchivedSetting, func(string, any) {
		for _, inst := range r.Instances {
			if l, ok := inst.Component.(*List); ok {
				prev, _ := l.Selected()
				l.refilter(prev.ID)
				l.syncFocus(prev, nil)
			}
		}
	})
	r.DefineGuard("list-filterable", func(r *Runtime, ev KeyEvent) bool { return r.focusedList(ev.Focus) != nil })
	// list-filtered holds while the focused list has a filter or its line
	// is open: filter-clear's verb guard, so esc in normal mode on an
	// unfiltered list stands aside for the level outside it (a modal's
	// pop, a page's close) in the walk and on the footer alike.
	r.DefineGuard("list-filtered", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && (l.vs.Text != "" || ev.Mode.Name == ListFilterMode)
	})
	// list-query holds while the query has text: filter-erase's verb
	// guard, so on an empty query backspace is the list-unfiltered row
	// that leaves the line.
	r.DefineGuard("list-query", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.vs.Text != ""
	})
	// list-chooses holds on a ClearFilterOnEnter list with a row
	// selected: filter-choose's verb guard, so enter on any other list,
	// or with no match, is the filter-keep row after it in the walk and on
	// the footer. The enter row carries it as its row guard too, which
	// keeps it a row of its own beside the unguarded filter-keep row.
	r.DefineGuard("list-chooses", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.choose && l.selected >= 0
	})
	r.DefineGuard("list-unfiltered", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.vs.Text == ""
	})

	resolve := func(c Context, args Arguments) (*List, error) {
		if id := StringArg(args, "id", ""); id != "" {
			if list := r.List(id); list != nil {
				return list, nil
			}
			return nil, errors.New("unknown list " + strconv.Quote(id))
		}
		for _, id := range r.Ancestry(focusOf(c)) {
			if list := r.List(id); list != nil && c.InputID == "" {
				return list, nil
			}
		}
		return nil, errors.New("no focused list")
	}
	leave := func() {
		if r.Mode.Name == ListFilterMode {
			r.changeMode(ModeState{Name: "normal"}, modeSelectionEnd)
		}
	}
	// unfilter clears the filter and closes the line, selecting the row the
	// line opened on when back is set (esc in the line), else keeping the
	// selection on its row (esc after enter, filter-choose).
	unfilter := func(l *List, back string) {
		// back is cleared after the filter, so a list's OnFilter still
		// reads the row the line opened on.
		l.SetFilter("")
		l.back = ""
		if back != "" && l.shows(back) {
			_ = l.Select(back)
		}
		leave()
	}
	id := map[string]Parameter{"id": {Plumbing: true, Type: "string", Description: "The list (default: the focused one)"}}
	with := func(extra map[string]Parameter) map[string]Parameter {
		out := map[string]Parameter{}
		for k, v := range id {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	for _, spec := range []struct {
		verb, description string
		parameters        map[string]Parameter
		run               func(*List, Arguments) error
		traits            []opTrait
	}{
		{"filter", "Filter the rows to those whose label or cells match query in the list's match mode (search.match; substring: every word) and case as search.case says; an empty query shows every row", with(map[string]Parameter{"query": {Type: "string", Required: true, Description: "The words every shown row holds; empty clears the filter"}}), func(l *List, a Arguments) error {
			l.SetFilter(StringArg(a, "query", ""))
			return nil
		}, nil},
		{"filter-open", "Open the list's filter line and type its query (" + ListFilterMode + " mode); a kept filter is edited", id, func(l *List, _ Arguments) error {
			if !l.Focused() {
				if err := l.Focus(); err != nil {
					return err
				}
			}
			// esc in the line goes back to the row selected now; enter
			// accepts the row reached (filter-keep forgets this one).
			selected, _ := l.Selected()
			l.back = selected.ID
			return r.SetMode(ListFilterMode)
		}, []opTrait{asFocusBound, asHistory(HistoryAction)}},
		{"filter-type", "Type text at the end of the filter query", with(map[string]Parameter{"text": {Type: "string", Required: true}}), func(l *List, a Arguments) error {
			l.SetFilter(l.vs.Text + StringArg(a, "text", ""))
			return nil
		}, nil},
		{"filter-erase", "Erase the filter query's last character", id, func(l *List, _ Arguments) error {
			if l.vs.Text != "" {
				_, size := utf8.DecodeLastRuneInString(l.vs.Text)
				l.SetFilter(l.vs.Text[:len(l.vs.Text)-size])
			}
			return nil
		}, nil},
		{"filter-keep", "Close the filter line keeping the filter (normal mode; / edits it again)", id, func(l *List, _ Arguments) error {
			l.back = ""
			leave()
			return nil
		}, nil},
		{"filter-clear", "Clear the filter, put the selection back where it was when the filter line opened, and close the line", id, func(l *List, _ Arguments) error {
			unfilter(l, l.back)
			return nil
		}, nil},
		{"filter-choose", "Choose the selected match: clear the filter keeping the selection on its row, and close the line (enter on a ClearFilterOnEnter list)", id, func(l *List, _ Arguments) error {
			unfilter(l, "")
			return nil
		}, nil},
	} {
		op := Operation{Key: KeyOf("ochazuke.list", spec.verb), Description: spec.description, Parameters: spec.parameters, Result: "list state", Run: func(_ *Runtime, c Context, args Arguments) (any, error) {
			list, err := resolve(c, args)
			if err != nil {
				return nil, err
			}
			if err := spec.run(list, args); err != nil {
				return nil, err
			}
			return list.state(), nil
		}}
		applyTraits(&op, spec.traits)
		r.Register(op)
	}
	guards := r.Nouns["ochazuke.list"].Guards
	guards["filter-clear"], guards["filter-erase"], guards["filter-choose"] = "list-filtered", "list-query", "list-chooses"
}
