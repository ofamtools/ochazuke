package ochazuke

import (
	"errors"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Search match: how a query picks rows, one of three modes every search
// surface shares (the "/" list filter, choosers, the path picker), the
// way search.case decides case for them all (search_case.go).
//
//   - substring: the row holds every whitespace-separated word of the
//     query, in any order (the list filter's long-standing rule).
//   - fuzzy: the query's runes appear in the row in order (the chooser's
//     and path picker's rule); fuzzyScore ranks them.
//   - regex: the query is a Go RE2 regular expression the row matches.
//     An invalid pattern matches nothing, and its surface says why on its
//     search line ("regex: missing closing )"), never in a modal.
//
// search.match is the mode the list filter starts in (substring),
// chooser.match the one choosers and the path picker start in (fuzzy).
// ochazuke.search.cycle-match (tab, shift+tab back) and
// ochazuke.search.set-match {mode} change the mode of the open search
// only, for as long as it lives; neither writes a setting.
// search.show-match draws the mode as a short tag at the right of the
// search line.

// The match settings: rows of settings/ochazuke.json.
const (
	// SearchMatchSetting is the "/" list filter's starting mode.
	SearchMatchSetting = "search.match"
	// ChooserMatchSetting is the starting mode of choosers and the path
	// picker.
	ChooserMatchSetting = "chooser.match"
	// SearchShowMatchSetting draws the mode's tag on the search line.
	SearchShowMatchSetting = "search.show-match"
)

// The match modes.
const (
	MatchSubstring = "substring"
	MatchFuzzy     = "fuzzy"
	MatchRegex     = "regex"
)

// matchModes is every mode in cycle order, with the tag the search line
// shows and whether its matches may be scattered over the text (only then
// does a chooser weigh how strongly its top row matches before Enter
// takes it over a create row).
var matchModes = []struct {
	name, tag string
	scattered bool
}{
	{MatchSubstring, "substr", false},
	{MatchFuzzy, "fuzzy", true},
	{MatchRegex, "regex", false},
}

// MatchModes lists the match modes in cycle order.
func MatchModes() []string {
	out := make([]string, len(matchModes))
	for i, m := range matchModes {
		out[i] = m.name
	}
	return out
}

// matchIndex is mode's place in matchModes, -1 for an unknown mode.
func matchIndex(mode string) int {
	return slices.IndexFunc(matchModes, func(m struct {
		name, tag string
		scattered bool
	}) bool {
		return m.name == mode
	})
}

// matchOr is mode when it is a match mode, else fallback.
func matchOr(mode, fallback string) string {
	if matchIndex(mode) >= 0 {
		return mode
	}
	return fallback
}

// NextMatch is the mode step places after mode in the cycle (negative
// steps go back), wrapping; an unknown mode counts as substring.
func NextMatch(mode string, step int) string {
	i := max(0, matchIndex(mode))
	n := len(matchModes)
	return matchModes[((i+step)%n+n)%n].name
}

// MatchTag is the short tag the search line shows for mode.
func MatchTag(mode string) string {
	if i := matchIndex(mode); i >= 0 {
		return matchModes[i].tag
	}
	return ""
}

// Matcher is one query compiled in one match mode, case as search.case
// says. Build it with NewMatcher or Runtime.Matcher; the zero value
// matches everything as a substring search.
type Matcher struct {
	mode   string
	cm     CaseMatch
	terms  []string // substring: the words, as cm compares them
	needle []rune   // fuzzy: the query, as cm compares it
	re     *regexp.Regexp
	err    error
}

// NewMatcher compiles query in mode (substring for an unknown mode) with
// case as the search.case value caseMode gives it. A blank query matches
// every text. For regex, smart case folds unless the pattern holds an
// upper-case letter outside an escape (\S, \W and \p{Lu} do not count,
// as in Vim), and folding is the (?i) flag.
func NewMatcher(mode, caseMode, query string) Matcher {
	m := Matcher{mode: matchOr(mode, MatchSubstring)}
	if strings.TrimSpace(query) == "" {
		return m
	}
	switch m.mode {
	case MatchRegex:
		// (?m): a list row's label and cells are its text's lines, so ^
		// and $ match at the ends of each.
		pattern := "(?m)" + query
		if regexFolds(caseMode, query) {
			pattern = "(?mi)" + query
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			m.err = regexError(err)
			return m
		}
		m.re = re
	case MatchFuzzy:
		m.cm = CaseFor(caseMode, query)
		m.needle = []rune(m.cm.String(query))
	default:
		m.cm = CaseFor(caseMode, query)
		m.terms = strings.Fields(m.cm.String(query))
	}
	return m
}

// regexFolds reports whether a regex search ignores case: ignore always,
// sensitive never, smart unless pattern holds an upper-case letter that
// is not escaped.
func regexFolds(caseMode, pattern string) bool {
	switch caseMode {
	case SearchCaseIgnore:
		return true
	case SearchCaseSensitive:
		return false
	}
	return !patternHasUpper(pattern)
}

// patternHasUpper reports whether pattern holds an upper-case letter
// outside an escape: the letter after a backslash (\S, \W, \B) and a
// class name (\p{Greek}, \PL) are syntax, not text.
func patternHasUpper(pattern string) bool {
	for i := 0; i < len(pattern); {
		r, size := utf8.DecodeRuneInString(pattern[i:])
		i += size
		if r != '\\' {
			if unicode.IsUpper(r) {
				return true
			}
			continue
		}
		if i >= len(pattern) {
			break
		}
		next, size := utf8.DecodeRuneInString(pattern[i:])
		i += size
		if (next == 'p' || next == 'P') && i < len(pattern) && pattern[i] == '{' {
			if end := strings.IndexByte(pattern[i:], '}'); end >= 0 {
				i += end + 1
			}
		} else if next == 'p' || next == 'P' {
			_, size := utf8.DecodeRuneInString(pattern[i:])
			i += size
		}
	}
	return false
}

// regexError is the line a surface shows for an invalid pattern:
// "regex: " and RE2's reason ("missing closing )").
func regexError(err error) error {
	var se *syntax.Error
	if errors.As(err, &se) {
		return errors.New("regex: " + string(se.Code))
	}
	return errors.New("regex: " + err.Error())
}

// Mode is the matcher's match mode.
func (m Matcher) Mode() string { return matchOr(m.mode, MatchSubstring) }

// Err is why the query matches nothing (an invalid regex), else nil.
func (m Matcher) Err() error { return m.err }

// Fold reports whether Prepare folds case (a regex folds through (?i)
// instead, so its texts are prepared unchanged).
func (m Matcher) Fold() bool { return m.cm.Fold() }

// Scattered reports whether the mode's matches may be strewn across the
// text (fuzzy), rather than spelled in it.
func (m Matcher) Scattered() bool { return matchModes[max(0, matchIndex(m.Mode()))].scattered }

// Prepare is text as the matcher compares it; a caller that matches the
// same texts often can keep the prepared texts while Fold is unchanged.
func (m Matcher) Prepare(text string) string { return m.cm.String(text) }

// Match reports whether text matches the query.
func (m Matcher) Match(text string) bool { return m.MatchPrepared(m.Prepare(text)) }

// MatchPrepared reports whether hay, a text from Prepare, matches.
func (m Matcher) MatchPrepared(hay string) bool {
	_, ok := m.ScorePrepared(hay)
	return ok
}

// ScorePrepared reports whether hay (from Prepare) matches and how well:
// fuzzyScore for fuzzy (compact runs and word starts score higher), the
// sum of each word's fuzzyScore for substring, 0 for regex. Higher is
// better; the blank query scores 0 everywhere.
func (m Matcher) ScorePrepared(hay string) (int, bool) {
	switch {
	case m.err != nil:
		return 0, false
	case m.re != nil:
		return 0, m.re.MatchString(hay)
	case m.needle != nil:
		return fuzzyScore(hay, m.needle, CaseMatch{})
	}
	score := 0
	for _, term := range m.terms {
		if !strings.Contains(hay, term) {
			return 0, false
		}
		s, _ := fuzzyScore(hay, []rune(term), CaseMatch{})
		score += s
	}
	return score, true
}

// fuzzyScore reports whether needle's runes (as cm compares them) appear
// in text in order, and how well: each rune costs one, a rune right after
// the previous match earns 12, one at a word start (after /, -, _ or a
// space, or at the start) earns 16, and a gap costs its length. The one
// fuzzy rule: choosers, the path picker and the fuzzy list filter all
// use it.
func fuzzyScore(text string, needle []rune, cm CaseMatch) (int, bool) {
	if len(needle) == 0 {
		return 0, true
	}
	matched, position, previous, score := 0, 0, -2, 0
	var before rune
	for _, original := range text {
		ch := cm.Rune(original)
		score--
		if matched < len(needle) && ch == needle[matched] {
			if position == previous+1 {
				score += 12
			}
			if position == 0 || before == '/' || before == '-' || before == '_' || unicode.IsSpace(before) {
				score += 16
			}
			score -= position - previous - 1
			previous = position
			matched++
		}
		before = ch
		position++
	}
	return score, matched == len(needle)
}

// SearchMatch is the search.match setting (substring for any other value
// and on a nil runtime).
func (r *Runtime) SearchMatch() string { return r.matchSetting(SearchMatchSetting, MatchSubstring) }

// ChooserMatch is the chooser.match setting (fuzzy for any other value
// and on a nil runtime).
func (r *Runtime) ChooserMatch() string { return r.matchSetting(ChooserMatchSetting, MatchFuzzy) }

func (r *Runtime) matchSetting(name, fallback string) string {
	if r == nil {
		return fallback
	}
	v, _ := r.settingText(name)
	return matchOr(v, fallback)
}

// ShowMatch is the search.show-match setting (on for a nil runtime).
func (r *Runtime) ShowMatch() bool {
	if r == nil {
		return true
	}
	if v, ok := r.settingValue(SearchShowMatchSetting); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return true
}

// Matcher compiles query in mode with case as search.case says.
func (r *Runtime) Matcher(mode, query string) Matcher {
	return NewMatcher(mode, r.SearchCase(), query)
}

// matchTagFor is the tag a search line shows for mode: "" when
// search.show-match is off.
func (r *Runtime) matchTagFor(mode string) string {
	if !r.ShowMatch() {
		return ""
	}
	return MatchTag(mode)
}

// matchSearcher is a component with a search the match verbs act on: the
// list filter (while its line shows), a chooser (unless its rows come from
// a Live source or its own Filter), the path picker (while open).
type matchSearcher interface {
	// matchMode is the mode the search runs in now.
	matchMode() string
	// setMatchMode sets it for this search and runs the search again.
	setMatchMode(mode string)
	// matchApplies reports whether the mode decides this search now.
	matchApplies() bool
}

// matchSearcherAt is the innermost component on focus's path whose search
// a match mode decides, or nil.
func (r *Runtime) matchSearcherAt(focus string) matchSearcher {
	for _, id := range r.Ancestry(focus) {
		if inst := r.Instances[id]; inst != nil {
			if s, ok := inst.Component.(matchSearcher); ok && s.matchApplies() {
				return s
			}
		}
	}
	if owner := r.rows[focus]; owner != "" {
		if l := r.List(owner); l != nil && l.matchApplies() {
			return l
		}
	}
	return nil
}

func registerSearchMatch(r *Runtime) {
	r.DefineGuard("search-matchable", func(r *Runtime, ev KeyEvent) bool { return r.matchSearcherAt(ev.Focus) != nil })
	find := func(c Context) (matchSearcher, error) {
		focus := focusOf(c)
		if focus == "" {
			focus = r.Focus
		}
		if s := r.matchSearcherAt(focus); s != nil {
			return s, nil
		}
		return nil, errors.New("no open search here (a / filter line, a chooser or the path picker)")
	}
	state := func(s matchSearcher) map[string]any {
		return map[string]any{"match": s.matchMode()}
	}
	r.Register(Operation{Verb: "cycle-match", Noun: "ochazuke.search", Description: "Step the open search (the / filter line, a chooser, the path picker) to the next match mode: substring, fuzzy, regex, round again; for this search only, the setting is unchanged", Parameters: map[string]Parameter{
		"step": {Type: "integer", Description: "How far to step; negative goes back (default 1)"},
	}, Result: "the search's match mode", Run: func(_ *Runtime, c Context, args Arguments) (any, error) {
		s, err := find(c)
		if err != nil {
			return nil, err
		}
		s.setMatchMode(NextMatch(s.matchMode(), IntArg(args, "step", 1)))
		return state(s), nil
	}})
	r.Register(Operation{Verb: "set-match", Noun: "ochazuke.search", Description: "Set the open search's match mode (substring, fuzzy or regex) for this search only; the setting is unchanged", Parameters: map[string]Parameter{
		"mode": {Type: "string", Required: true, Enum: MatchModes(), Description: "substring, fuzzy or regex"},
	}, Result: "the search's match mode", Run: func(_ *Runtime, c Context, args Arguments) (any, error) {
		mode := StringArg(args, "mode", "")
		if matchIndex(mode) < 0 {
			return nil, errors.New("unknown match mode " + mode + " (substring, fuzzy or regex)")
		}
		s, err := find(c)
		if err != nil {
			return nil, err
		}
		s.setMatchMode(mode)
		return state(s), nil
	}})
	noun := r.Nouns["ochazuke.search"]
	if noun.Guards == nil {
		noun.Guards = map[string]string{}
	}
	guards := noun.Guards
	guards["cycle-match"], guards["set-match"] = "search-matchable", "search-matchable"
}
