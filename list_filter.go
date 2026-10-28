package ochazuke

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The list filter: "/" on any focused list (page lists, tables, a
// catalog page's lists, the Settings page's) opens a one-line query above
// its rows, and typing narrows the rows to those whose label or cells hold
// every word of the query, ignoring case, in list order. The filter hides
// rows from drawing and from motions (j/k, counts, spills) without
// removing them: their instances, marks and data stay, SetItems while
// filtered filters the new rows, and clearing the filter shows them all.
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

// filterTerms splits a query into the lowercased words a row must hold.
func filterTerms(query string) []string { return strings.Fields(strings.ToLower(query)) }

// filterHay is the text a row is matched against: its label and cells,
// lowercased, separated so a word never spans two cells.
func filterHay(item ListItem) string {
	parts := append([]string{item.Label}, item.Cells...)
	return strings.ToLower(strings.Join(parts, "\x00"))
}

// filterMatches reports whether hay holds every term.
func filterMatches(hay string, terms []string) bool {
	for _, term := range terms {
		if !strings.Contains(hay, term) {
			return false
		}
	}
	return true
}

// Filter is the list's filter query ("" when every row is shown).
func (l *List) Filter() string { return l.query }

// Filtering reports whether the filter line shows: a filter is set, or
// its query is being typed on this list.
func (l *List) Filtering() bool { return l.query != "" || l.editingFilter() }

// Height is the lines the list needs to draw every row without
// windowing (a table's header aside): Len, plus the filter line while it
// shows. A host that sizes a list to its rows sizes it with Height, so
// opening the filter line neither windows the rows nor moves what is drawn
// below the list as the filter narrows it.
func (l *List) Height() int {
	if l.Filtering() {
		return len(l.all) + 1
	}
	return len(l.all)
}

// Shown is the number of rows the filter lets through (Len without one).
func (l *List) Shown() int { return len(l.items) }

// SetFilter sets the filter query and shows the rows that match it; ""
// shows every row. The selection stays on its row when that row still
// shows, else it moves to the first match (none when nothing matches);
// the focus follows it.
func (l *List) SetFilter(query string) {
	previous, _ := l.Selected()
	l.query = query
	keep := previous.ID
	if keep == "" {
		keep = l.back // nothing matched: widening the query finds the row / started on
	}
	l.refilter(keep)
	l.syncFocus(previous, nil)
}

// refilter recomputes the shown rows from every row and the query, then
// selects keep when it shows, else the first shown row.
func (l *List) refilter(keep string) {
	if strings.TrimSpace(l.query) == "" {
		l.items = l.all
	} else {
		if l.hay == nil {
			l.hay = make([]string, len(l.all))
			for i, item := range l.all {
				l.hay[i] = filterHay(item)
			}
		}
		terms := filterTerms(l.query)
		shown := make([]ListItem, 0, len(l.all))
		for i, item := range l.all {
			if filterMatches(l.hay[i], terms) {
				shown = append(shown, item)
			}
		}
		l.items = shown
	}
	l.selected = slices.IndexFunc(l.items, func(item ListItem) bool { return item.ID == keep })
	if l.selected < 0 && len(l.items) > 0 {
		l.selected = 0
	}
	l.offset = min(l.offset, max(0, len(l.items)-1))
}

// shows reports whether the row with id is among the shown rows.
func (l *List) shows(id string) bool {
	return slices.ContainsFunc(l.items, func(item ListItem) bool { return item.ID == id })
}

// reveal clears the filter when id is a row it hides, so a command that
// lands on that row (Select, focus.set, a job's focus) shows it. The
// selection is the caller's to set.
func (l *List) reveal(id string) {
	if l.query == "" || l.shows(id) || !slices.ContainsFunc(l.all, func(item ListItem) bool { return item.ID == id }) {
		return
	}
	l.query, l.back = "", ""
	l.items = l.all
}

// editingFilter reports whether the filter line is being typed: the mode
// is ListFilterMode and the list holds the focus.
func (l *List) editingFilter() bool {
	return l.r.Mode.Name == ListFilterMode && l.Focused()
}

// filterLine is the line drawn above the rows while the filter is typed or
// kept: "/" and the query (with a reversed cursor cell while typing) on
// the left, how many rows match on the right. "" when there is none.
func (l *List) filterLine(width int) string {
	editing := l.editingFilter()
	if l.query == "" && !editing {
		return ""
	}
	left := "/" + modalLabel(l.query)
	if editing {
		left += lipgloss.NewStyle().Reverse(true).Render(" ")
	}
	right := ""
	if strings.TrimSpace(l.query) != "" {
		switch len(l.items) {
		case 0:
			right = "no matches"
		default:
			right = strconv.Itoa(len(l.items)) + " of " + strconv.Itoa(len(l.all))
		}
	}
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
	r.DefineGuard("list-filterable", func(r *Runtime, ev KeyEvent) bool { return r.focusedList(ev.Focus) != nil })
	// list-filtered holds while the focused list has a filter or its line
	// is open: filter-clear's verb guard, so esc in normal mode on an
	// unfiltered list stands aside for the level outside it (a modal's
	// pop, a page's close) in the walk and on the footer alike.
	r.DefineGuard("list-filtered", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && (l.query != "" || ev.Mode.Name == ListFilterMode)
	})
	// list-query holds while the query has text: filter-erase's verb
	// guard, so on an empty query backspace is the list-unfiltered row
	// that leaves the line.
	r.DefineGuard("list-query", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.query != ""
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
		return l != nil && l.query == ""
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
			r.setMode(ModeState{Name: "normal"})
		}
	}
	// unfilter clears the filter and closes the line, selecting the row the
	// line opened on when back is set (esc in the line), else keeping the
	// selection on its row (esc after enter, filter-choose).
	unfilter := func(l *List, back string) {
		l.back = ""
		l.SetFilter("")
		if back != "" && l.shows(back) {
			_ = l.Select(back)
		}
		leave()
	}
	id := map[string]Parameter{"id": {Type: "string", Description: "The list (default: the focused one)"}}
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
	}{
		{"filter", "Filter the rows to those whose label or cells hold every word of query, ignoring case; an empty query shows every row", with(map[string]Parameter{"query": {Type: "string", Required: true, Description: "The words every shown row holds; empty clears the filter"}}), func(l *List, a Arguments) error {
			l.SetFilter(StringArg(a, "query", ""))
			return nil
		}},
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
		}},
		{"filter-type", "Type text at the end of the filter query", with(map[string]Parameter{"text": {Type: "string", Required: true}}), func(l *List, a Arguments) error {
			l.SetFilter(l.query + StringArg(a, "text", ""))
			return nil
		}},
		{"filter-erase", "Erase the filter query's last character", id, func(l *List, _ Arguments) error {
			if l.query != "" {
				_, size := utf8.DecodeLastRuneInString(l.query)
				l.SetFilter(l.query[:len(l.query)-size])
			}
			return nil
		}},
		{"filter-keep", "Close the filter line keeping the filter (normal mode; / edits it again)", id, func(l *List, _ Arguments) error {
			l.back = ""
			leave()
			return nil
		}},
		{"filter-clear", "Clear the filter, put the selection back where it was when the filter line opened, and close the line", id, func(l *List, _ Arguments) error {
			unfilter(l, l.back)
			return nil
		}},
		{"filter-choose", "Choose the selected match: clear the filter keeping the selection on its row, and close the line (enter on a ClearFilterOnEnter list)", id, func(l *List, _ Arguments) error {
			unfilter(l, "")
			return nil
		}},
	} {
		r.Register(Operation{Verb: spec.verb, Noun: "ochazuke.list", Description: spec.description, Parameters: spec.parameters, Result: "list state", Run: func(_ *Runtime, c Context, args Arguments) (any, error) {
			list, err := resolve(c, args)
			if err != nil {
				return nil, err
			}
			if err := spec.run(list, args); err != nil {
				return nil, err
			}
			return list.state(), nil
		}})
	}
	guards := r.Nouns["ochazuke.list"].Guards
	guards["filter-clear"], guards["filter-erase"], guards["filter-choose"] = "list-filtered", "list-query", "list-chooses"
}
