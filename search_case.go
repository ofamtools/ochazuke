package ochazuke

import (
	"strings"
	"unicode"
)

// Search case: every search and filter the framework runs (the path
// picker's fuzzy search, the path chooser's typed completion, the "/"
// list filter, a chooser's matching) decides case the same way, from the
// search.case setting and the query, through CaseFor. Identifiers (verb,
// noun and setting names, palette words, column titles, key names) are
// not searches and keep their own case-insensitive lookups.

// SearchCaseSetting is how searches treat case: SearchCaseSmart (the
// default), SearchCaseIgnore or SearchCaseSensitive. A row of
// settings/ochazuke.json.
const SearchCaseSetting = "search.case"

// The values of search.case.
const (
	// SearchCaseSmart ignores case unless the query holds an upper-case
	// letter, then matches case exactly (Vim's ignorecase with smartcase).
	SearchCaseSmart = "smart"
	// SearchCaseIgnore always ignores case (Vim's ignorecase).
	SearchCaseIgnore = "ignore"
	// SearchCaseSensitive always matches case (Vim's noignorecase).
	SearchCaseSensitive = "sensitive"
)

// CaseMatch is how one query compares with text: folded (case ignored)
// or exact. Build it with CaseFor or Runtime.CaseFor; the zero value
// matches exactly.
type CaseMatch struct{ fold bool }

// CaseFor is the CaseMatch search.case value mode gives query: smart
// folds unless query holds an upper-case letter (unicode.IsUpper, so É
// counts and a separator or digit never does); ignore always folds;
// sensitive never does. An unknown mode reads as smart.
func CaseFor(mode, query string) CaseMatch {
	switch mode {
	case SearchCaseIgnore:
		return CaseMatch{fold: true}
	case SearchCaseSensitive:
		return CaseMatch{}
	}
	return CaseMatch{fold: !strings.ContainsFunc(query, unicode.IsUpper)}
}

// Fold reports whether the match ignores case.
func (m CaseMatch) Fold() bool { return m.fold }

// String is s as the match compares it: lower-cased when folding, else
// unchanged. Apply it to both the query and the text.
func (m CaseMatch) String(s string) string {
	if m.fold {
		return strings.ToLower(s)
	}
	return s
}

// Rune is r as the match compares it.
func (m CaseMatch) Rune(r rune) rune {
	if m.fold {
		return unicode.ToLower(r)
	}
	return r
}

// Contains reports whether text holds query.
func (m CaseMatch) Contains(text, query string) bool {
	return strings.Contains(m.String(text), m.String(query))
}

// HasPrefix reports whether text starts with query.
func (m CaseMatch) HasPrefix(text, query string) bool {
	return strings.HasPrefix(m.String(text), m.String(query))
}

// SearchCase is the search.case setting: smart, ignore or sensitive
// (smart for any other value, and on a nil runtime).
func (r *Runtime) SearchCase() string {
	if r == nil {
		return SearchCaseSmart
	}
	switch v, _ := r.settingText(SearchCaseSetting); v {
	case SearchCaseIgnore, SearchCaseSensitive:
		return v
	}
	return SearchCaseSmart
}

// CaseFor is the CaseMatch the search.case setting gives query.
func (r *Runtime) CaseFor(query string) CaseMatch { return CaseFor(r.SearchCase(), query) }
