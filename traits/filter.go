package traits

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MatchMode selects how a filter query matches candidate text.
type MatchMode string

// Standard match modes.
const (
	MatchSubstring MatchMode = "substring"
	MatchFuzzy     MatchMode = "fuzzy"
	MatchRegex     MatchMode = "regex"
)

var defaultMatchModes = []MatchMode{MatchSubstring, MatchFuzzy, MatchRegex}

// FilterConfig declares filter defaults, mode cycle order, case sensitivity,
// and whether filter editing starts active.
type FilterConfig struct {
	DefaultMode   MatchMode   `json:"defaultMode,omitempty"`
	Modes         []MatchMode `json:"modes,omitempty"`
	CaseSensitive bool        `json:"caseSensitive,omitempty"`
	StartActive   bool        `json:"startActive,omitempty"`
}

// FilterState captures the current filter query, cursor, edit state, match
// mode, and compilation error of a filterable widget.
type FilterState struct {
	Query  string
	Cursor int
	Active bool
	Mode   MatchMode
	Err    string
}

// Matcher is a compiled, reusable predicate and rune-highlight locator for a
// filter query. Its zero value matches all text and highlights no runes.
type Matcher struct {
	mode          MatchMode
	query         string
	lowerQuery    string
	queryRunes    []rune
	caseSensitive bool
	re            *regexp.Regexp
	invalid       bool
}

func resolveMatchMode(mode MatchMode, cfg FilterConfig) MatchMode {
	if mode != "" {
		return mode
	}
	if cfg.DefaultMode != "" {
		return cfg.DefaultMode
	}
	if len(cfg.Modes) > 0 && cfg.Modes[0] != "" {
		return cfg.Modes[0]
	}
	return MatchSubstring
}

// CompileFilter compiles query under mode and cfg into a Matcher. An empty
// query matches all text with no error. When mode is MatchRegex and query is
// not a valid regular expression, CompileFilter returns the compiler error
// string along with a Matcher that falls back to literal substring matching
// for Match and highlights no runes in MatchRunes.
func CompileFilter(query string, mode MatchMode, cfg FilterConfig) (Matcher, string) {
	resolved := resolveMatchMode(mode, cfg)
	m := Matcher{
		mode:          resolved,
		query:         query,
		caseSensitive: cfg.CaseSensitive,
	}
	if query == "" {
		return m, ""
	}
	if cfg.CaseSensitive {
		m.queryRunes = []rune(query)
	} else {
		m.lowerQuery = strings.ToLower(query)
		m.queryRunes = []rune(m.lowerQuery)
	}
	if resolved == MatchRegex {
		pat := query
		if !cfg.CaseSensitive {
			pat = "(?i)" + query
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			m.invalid = true
			return m, err.Error()
		}
		m.re = re
	}
	return m, ""
}

// Match reports whether text satisfies m.
func (m Matcher) Match(text string) bool {
	if m.query == "" {
		return true
	}
	switch m.mode {
	case MatchFuzzy:
		return matchFuzzy(m.normText(text), m.queryRunes)
	case MatchRegex:
		if m.re != nil {
			return m.re.MatchString(text)
		}
		// Invalid regex falls back to literal substring matching.
		return m.matchSubstring(text)
	default:
		return m.matchSubstring(text)
	}
}

// MatchAny reports whether any element of texts satisfies m. An empty query
// always returns true.
func (m Matcher) MatchAny(texts ...string) bool {
	if m.query == "" {
		return true
	}
	for _, s := range texts {
		if m.Match(s) {
			return true
		}
	}
	return false
}

// MatchRunes returns a slice parallel to []rune(text) whose true entries mark
// the runes matched by m. An empty query, non-matching text, or an invalid
// regex returns an all-false slice.
func (m Matcher) MatchRunes(text string) []bool {
	rs := []rune(text)
	hit := make([]bool, len(rs))
	if m.query == "" || len(rs) == 0 {
		return hit
	}
	switch m.mode {
	case MatchRegex:
		if m.invalid || m.re == nil {
			return hit
		}
		if loc := m.re.FindStringIndex(text); loc != nil {
			from := utf8.RuneCountInString(text[:loc[0]])
			to := from + utf8.RuneCountInString(text[loc[0]:loc[1]])
			for i := from; i < to && i < len(hit); i++ {
				hit[i] = true
			}
		}
	case MatchFuzzy:
		hay := m.normRunes(text, rs)
		qi := 0
		for i, r := range hay {
			if qi < len(m.queryRunes) && r == m.queryRunes[qi] {
				hit[i] = true
				qi++
				if qi == len(m.queryRunes) {
					break
				}
			}
		}
		if qi < len(m.queryRunes) {
			clear(hit)
		}
	default:
		hay := m.normRunes(text, rs)
		if idx := indexRuneSlice(hay, m.queryRunes); idx >= 0 {
			for k := range m.queryRunes {
				hit[idx+k] = true
			}
		}
	}
	return hit
}

func (m Matcher) normText(text string) string {
	if m.caseSensitive {
		return text
	}
	return strings.ToLower(text)
}

func (m Matcher) normRunes(text string, orig []rune) []rune {
	if m.caseSensitive {
		return orig
	}
	low := []rune(strings.ToLower(text))
	if len(low) == len(orig) {
		return low
	}
	out := make([]rune, len(orig))
	for i, r := range orig {
		out[i] = unicode.ToLower(r)
	}
	return out
}

func (m Matcher) matchSubstring(text string) bool {
	if m.caseSensitive {
		return strings.Contains(text, m.query)
	}
	return strings.Contains(strings.ToLower(text), m.lowerQuery)
}

func matchFuzzy(hay string, needle []rune) bool {
	if len(needle) == 0 {
		return true
	}
	qi := 0
	for _, r := range hay {
		if r == needle[qi] {
			qi++
			if qi == len(needle) {
				return true
			}
		}
	}
	return false
}

func indexRuneSlice(hay, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(hay) {
		return -1
	}
	last := len(hay) - len(needle)
	first := needle[0]
	for i := 0; i <= last; i++ {
		if hay[i] != first {
			continue
		}
		ok := true
		for j := 1; j < len(needle); j++ {
			if hay[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// CycleMatchMode advances cur to the next mode in cfg.Modes (or the default
// sequence substring -> fuzzy -> regex when cfg.Modes is empty).
func CycleMatchMode(cur MatchMode, cfg FilterConfig) MatchMode {
	modes := cfg.Modes
	if len(modes) == 0 {
		modes = defaultMatchModes
	}
	active := resolveMatchMode(cur, cfg)
	for i, m := range modes {
		if m == active {
			return modes[(i+1)%len(modes)]
		}
	}
	return modes[0]
}

// FilterFacts derives truthful read-only filter facts from st under cfg.
func FilterFacts(st FilterState, cfg FilterConfig) map[string]any {
	mode := resolveMatchMode(st.Mode, cfg)
	return map[string]any{
		"filtering":   st.Active,
		"query":       st.Query,
		"queryEmpty":  st.Query == "",
		"hasFilter":   st.Query != "",
		"matchMode":   string(mode),
		"filterError": st.Err,
	}
}
