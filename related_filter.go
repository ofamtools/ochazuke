package ochazuke

import (
	"slices"
	"strconv"
	"strings"
)

// One filter for a whole related stack. With RelatedFilterScopeSetting at
// stack (the default), every section's list hands its text filter to the
// stack (Collection.SharedFilter): "/" on any row of the stack opens one
// query, typing narrows every section at once, enter keeps it and esc
// clears it everywhere. While a query is set each section shows only the
// rows it matches, its heading counting them (a section with none is its
// heading and (0)), folded sections and folded groups included: they are
// read whole while the query lasts and go back to their counts after. The
// stack draws the one filter line, above its first section. At section,
// each section's list keeps its own filter, as any list does.

// RelatedFilterScopeSetting says what "/" filters in a related stack:
// RelatedFilterStack (every section at once, the default) or
// RelatedFilterSection (the section's own list, as before).
const RelatedFilterScopeSetting = "related.filter-scope"

// The values of RelatedFilterScopeSetting.
const (
	RelatedFilterStack   = "stack"
	RelatedFilterSection = "section"
)

// sharedFilter is the stack as its sections' lists' filter while the scope
// is the stack, nil while each section filters itself.
func (rel *Related) sharedFilter() SharedFilter {
	if _, ok := rel.r.settingValue(RelatedFilterScopeSetting); ok && rel.r.Settings().String(RelatedFilterScopeSetting) != RelatedFilterStack {
		return nil
	}
	return rel
}

// FilterQuery is the stack's filter query ("" none).
func (rel *Related) FilterQuery() string { return rel.query }

// SetFilterQuery filters every section by query ("" shows every row).
// The sections redraw what they hold at once; starting or ending a filter
// also reads them again (a folded section is read whole while it lasts).
func (rel *Related) SetFilterQuery(query string) {
	started := (query != "") != (rel.query != "")
	rel.query = query
	for _, s := range rel.sections {
		if s.list != nil && !s.grouping {
			s.render() // what each section holds, narrowed now
		}
	}
	if started {
		rel.fillAll() // folded sections read whole (or back to their counts)
	}
}

// MatchMode is the stack filter's match mode: the one search.cycle-match or
// search.set-match chose for this stack, else the search.match setting.
func (rel *Related) MatchMode() string {
	if rel.match != "" {
		return rel.match
	}
	return rel.r.SearchMatch()
}

// matcher is the stack filter query compiled in the stack's match mode.
func (rel *Related) matcher() Matcher { return rel.r.Matcher(rel.MatchMode(), rel.query) }

// editingFilter reports whether a list in the stack is typing the stack's
// filter line.
func (rel *Related) editingFilter() bool {
	_, _, focus, _, _ := rel.r.drawnAt(rel.id)
	l := rel.r.focusedList(focus)
	return l != nil && l.editingFilter() && slices.Contains(rel.r.Ancestry(focus), rel.id)
}

// The matchSearcher contract (search_match.go): the mode decides the
// stack's filter while its line shows.
func (rel *Related) matchMode() string { return rel.MatchMode() }
func (rel *Related) matchApplies() bool {
	return rel.sharedFilter() != nil && (rel.query != "" || rel.editingFilter())
}
func (rel *Related) setMatchMode(mode string) {
	rel.match = mode
	for _, s := range rel.sections {
		if s.list != nil && !s.grouping {
			s.render()
		}
	}
}

// filtering reports whether a stack query narrows the sections.
func (rel *Related) filtering() bool { return rel.query != "" }

// matching keeps the rows the stack's query matches, in their order.
func (s *relatedSection) matching(items []ListItem) []ListItem {
	m := s.rel.matcher()
	out := items[:0:0]
	for _, item := range items {
		if m.MatchRow(s.list.preparedRow(item, m)) {
			out = append(out, item)
		}
	}
	return out
}

// shownCount is the count the heading shows: the matches while the stack
// is filtered, else the things.
func (s *relatedSection) shownCount() int {
	if s.rel.filtering() {
		return s.matched
	}
	return s.count
}

// filterLine is the stack's one filter line while its query is set or
// typed, "" otherwise: "/" and the query, then how many rows match (or why
// none can: an invalid regex) and the match mode's tag.
func (rel *Related) filterLine(width int) string {
	r := rel.r
	editing := rel.editingFilter()
	if rel.query == "" && !editing {
		return ""
	}
	left := "/" + modalLabel(rel.query)
	if editing {
		left += SelectionStyle(true).Render(" ")
	}
	var right []string
	if strings.TrimSpace(rel.query) != "" {
		switch err := rel.matcher().Err(); {
		case err != nil:
			right = append(right, err.Error())
		default:
			total := 0
			for _, s := range rel.visible() {
				if !s.grouping {
					total += s.matched
				}
			}
			if total == 0 {
				right = append(right, "no rows match"+r.RegexHint(rel.MatchMode(), rel.query))
			} else {
				right = append(right, strconv.Itoa(total)+" match")
			}
		}
	}
	if tag := r.matchTagFor(rel.MatchMode()); tag != "" {
		right = append(right, tag)
	}
	return searchLine(left, strings.Join(right, "  "), width)
}
