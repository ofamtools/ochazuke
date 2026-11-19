package ochazuke

import (
	"slices"
	"strconv"
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

// filtering reports whether a stack query narrows the sections.
func (rel *Related) filtering() bool { return rel.query != "" }

// matching keeps the rows the stack's query matches, in their order.
func (s *relatedSection) matching(items []ListItem) []ListItem {
	m := s.rel.r.Matcher(s.rel.r.Settings().String(SearchMatchSetting), s.rel.query)
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
// typed, "" otherwise: "/" and the query, then how many rows match (or that
// none do) and the match mode's tag.
func (rel *Related) filterLine(width int) string {
	r := rel.r
	_, _, focus, _, _ := r.drawnAt(rel.id)
	l := r.focusedList(focus)
	editing := l != nil && l.editingFilter() && slices.Contains(r.Ancestry(focus), rel.id)
	if rel.query == "" && !editing {
		return ""
	}
	left := "/" + modalLabel(rel.query)
	if editing {
		left += SelectionStyle(true).Render(" ")
	}
	var right string
	if rel.query != "" {
		total := 0
		for _, s := range rel.visible() {
			if !s.grouping {
				total += s.matched
			}
		}
		right = strconv.Itoa(total) + " match"
		if total == 0 {
			right = "no rows match"
		}
	}
	if tag := r.matchTagFor(r.Settings().String(SearchMatchSetting)); tag != "" {
		right += "  " + tag
	}
	return searchLine(left, right, width)
}
