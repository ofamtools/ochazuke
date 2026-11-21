package ochazuke

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The binding help page's filter: free words and field:value terms, all
// ANDed, -term negated, values with spaces quoted (key:"g d"). Each term is
// a numbered chip. A value: it says which rows match, and for an empty
// result which terms emptied it.

// helpField is one field a term may name and the test it runs on a row
// with the term's value.
type helpField struct {
	Name  string
	Match func(v HelpPageRow, val string) bool
}

// helpFields are the fields a term may name, in the order the page lists
// them.
var helpFields = []helpField{
	{"key", keyTermMatches},
	{"noun", func(v HelpPageRow, val string) bool { return strings.Contains(strings.ToLower(v.Noun), val) }},
	{"verb", verbTermMatches},
	{"mode", func(v HelpPageRow, val string) bool {
		return strings.ToLower(v.Mode) == val || val == "any" && v.Mode == modeLabel("")
	}},
	{"source", func(v HelpPageRow, val string) bool {
		return v.Class == val || strings.Contains(strings.ToLower(v.Source), val)
	}},
	{"guard", func(v HelpPageRow, val string) bool { return strings.Contains(strings.ToLower(v.Guard), val) }},
	{"level", func(v HelpPageRow, val string) bool { return strings.ToLower(v.LevelName) == val }},
	{"table", func(v HelpPageRow, val string) bool { return strings.ToLower(v.Table) == val }},
	{"status", func(v HelpPageRow, val string) bool { return strings.Contains(string(v.State), val) }},
	{"is", func(v HelpPageRow, val string) bool {
		is := helpIsNamed(val)
		return is != nil && is(v)
	}},
}

// HelpQueryFields are the fields a term may name.
func HelpQueryFields() []string {
	out := make([]string, len(helpFields))
	for i, f := range helpFields {
		out[i] = f.Name
	}
	return out
}

// helpFieldNamed is the field of that name, nil for none.
func helpFieldNamed(name string) *helpField {
	for i := range helpFields {
		if helpFields[i].Name == name {
			return &helpFields[i]
		}
	}
	return nil
}

// helpIsWord is one is: word and its test of a row.
type helpIsWord struct {
	Word string
	Test func(HelpPageRow) bool
}

// helpIs are the is: words.
var helpIs = []helpIsWord{
	{"bound", func(v HelpPageRow) bool { return !v.Unbound }},
	{"unbound", func(v HelpPageRow) bool { return v.Unbound }},
	{"nokey", func(v HelpPageRow) bool { return v.Unbound }},
	{"shadowed", func(v HelpPageRow) bool { return v.State.Hidden() }},
	{"answers", func(v HelpPageRow) bool { return v.State == StatusAnswers }},
	{"maybe", func(v HelpPageRow) bool { return v.State == StatusMaybe }},
	{"guarded", func(v HelpPageRow) bool { return v.Guard != "" }},
	{"hidden", func(v HelpPageRow) bool { return v.Hidden }},
	{"generated", func(v HelpPageRow) bool { return v.Class == SourceGenerated }},
	{"user", func(v HelpPageRow) bool { return v.Class == SourceUser }},
	{"walk", HelpPageRow.OnWalk},
	{"off", func(v HelpPageRow) bool { return v.State == StatusOffWalk }},
	{"pattern", func(v HelpPageRow) bool { return !v.Unbound && v.Row.Keys == "" }},
	{"changed", func(v HelpPageRow) bool { return v.Changed }},
	{"removed", func(v HelpPageRow) bool { return v.State == StatusRemoved }},
}

// helpIsNamed is the test of an is: word, nil for none.
func helpIsNamed(word string) func(HelpPageRow) bool {
	for _, w := range helpIs {
		if w.Word == word {
			return w.Test
		}
	}
	return nil
}

// HelpIsWords are the words is: takes.
func HelpIsWords() []string {
	out := make([]string, len(helpIs))
	for i, w := range helpIs {
		out[i] = w.Word
	}
	return out
}

// HelpTerm is one chip of the query: its field ("" a free word), value
// (lower case), raw unquoted text (preserving case for regex/smart-case
// matchers), whether it is negated, and its text as typed.
type HelpTerm struct {
	Not   bool   `json:"not,omitempty"`
	Field string `json:"field,omitempty"`
	Value string `json:"value"`
	Raw   string `json:"raw,omitempty"`
	Text  string `json:"text"`
}

func (t HelpTerm) rawValue() string {
	if t.Raw != "" {
		return t.Raw
	}
	return t.Value
}

// HelpQuery is the page's filter, parsed.
type HelpQuery struct {
	Terms []HelpTerm `json:"terms"`
}

var helpTermPattern = regexp.MustCompile(`(-?)(?:(\w+):)?("([^"]*)"|\S+)`)

// ParseHelpQuery reads a filter line. A word with an unknown field is a
// free word, colon and all; nothing typed is ever refused.
func ParseHelpQuery(text string) HelpQuery {
	var q HelpQuery
	for _, m := range helpTermPattern.FindAllStringSubmatch(text, -1) {
		field := strings.ToLower(m[2])
		value := m[3]
		if m[4] != "" || strings.HasPrefix(m[3], `"`) {
			value = m[4]
		}
		if m[2] != "" && helpFieldNamed(field) == nil {
			field, value = "", m[2]+":"+value
		}
		if value == "" {
			continue
		}
		q.Terms = append(q.Terms, HelpTerm{Not: m[1] != "", Field: field, Value: strings.ToLower(value), Raw: value, Text: m[0]})
	}
	return q
}

// String is the query as a filter line: the terms' texts.
func (q HelpQuery) String() string {
	texts := make([]string, len(q.Terms))
	for i, t := range q.Terms {
		texts[i] = t.Text
	}
	return strings.Join(texts, " ")
}

// Empty reports whether the query has no terms (every row matches).
func (q HelpQuery) Empty() bool { return len(q.Terms) == 0 }

type compiledHelpTerm struct {
	term    HelpTerm
	field   *helpField
	matcher Matcher
}

type compiledHelpQuery struct {
	terms []compiledHelpTerm
	err   error
}

// compile compiles q's free terms under matchMode and caseMode once so
// repeated row checks do not recompile regexes.
func (q HelpQuery) compile(matchMode, caseMode string) compiledHelpQuery {
	cq := compiledHelpQuery{terms: make([]compiledHelpTerm, len(q.Terms))}
	for i, t := range q.Terms {
		ct := compiledHelpTerm{term: t, field: helpFieldNamed(t.Field)}
		if ct.field == nil {
			ct.matcher = NewMatcher(matchMode, caseMode, t.rawValue())
			if ct.matcher.Err() != nil && cq.err == nil {
				cq.err = ct.matcher.Err()
			}
		}
		cq.terms[i] = ct
	}
	return cq
}

func (cq compiledHelpQuery) matches(v HelpPageRow) bool {
	if cq.err != nil {
		return false
	}
	var hay string
	var hayBuilt bool
	for _, ct := range cq.terms {
		var ok bool
		if ct.field != nil {
			ok = ct.field.Match(v, ct.term.Value)
		} else {
			if !hayBuilt {
				hay = helpHaystackLines(v)
				hayBuilt = true
			}
			ok = ct.matcher.MatchRow(ct.matcher.Prepare(hay))
		}
		if ok == ct.term.Not {
			return false
		}
	}
	return true
}

func (cq compiledHelpQuery) matching(rows []HelpPageRow) []HelpPageRow {
	if cq.err != nil {
		return nil
	}
	if len(cq.terms) == 0 {
		return rows
	}
	var out []HelpPageRow
	for _, v := range rows {
		if cq.matches(v) {
			out = append(out, v)
		}
	}
	return out
}

// Err reports the first matcher error (such as an invalid regex) among q's
// free terms under matchMode and caseMode, or nil when all terms compile.
func (q HelpQuery) Err(matchMode, caseMode string) error {
	return q.compile(matchMode, caseMode).err
}

// Matches reports whether a row passes every term (case-insensitive substring
// for free terms).
func (q HelpQuery) Matches(v HelpPageRow) bool {
	return q.MatchesWith(v, MatchSubstring, SearchCaseIgnore)
}

// MatchesWith reports whether a row passes every term, evaluating free terms
// with matchMode and caseMode.
func (q HelpQuery) MatchesWith(v HelpPageRow, matchMode, caseMode string) bool {
	return q.compile(matchMode, caseMode).matches(v)
}

// Matching are the rows that pass (case-insensitive substring for free terms).
func (q HelpQuery) Matching(rows []HelpPageRow) []HelpPageRow {
	return q.MatchingWith(rows, MatchSubstring, SearchCaseIgnore)
}

// MatchingWith are the rows that pass, evaluating free terms with matchMode
// and caseMode.
func (q HelpQuery) MatchingWith(rows []HelpPageRow, matchMode, caseMode string) []HelpPageRow {
	return q.compile(matchMode, caseMode).matching(rows)
}

// Matches reports whether a row meets the term, negation aside: its
// field's test, or for a free word the row's text.
func (t HelpTerm) Matches(v HelpPageRow) bool {
	if f := helpFieldNamed(t.Field); f != nil {
		return f.Match(v, t.Value)
	}
	m := NewMatcher(MatchSubstring, SearchCaseIgnore, t.rawValue())
	return m.Err() == nil && m.MatchRow(m.Prepare(helpHaystackLines(v)))
}

// verbTermMatches: the verb or noun.verb exactly; a trailing * a prefix.
func verbTermMatches(v HelpPageRow, val string) bool {
	if prefix, ok := strings.CutSuffix(val, "*"); ok {
		return strings.HasPrefix(strings.ToLower(v.Verb), prefix) || strings.HasPrefix(strings.ToLower(v.Operation), prefix)
	}
	return strings.ToLower(v.Verb) == val || strings.ToLower(v.Operation) == val
}

// keyTermMatches: the key or sequence itself, or a sequence it starts
// (key:g finds g d); a pattern row by its pattern or a one-of member.
func keyTermMatches(v HelpPageRow, val string) bool {
	if v.Unbound {
		return val == "none" || val == "(no key)"
	}
	keys := []string{v.Row.Keys}
	if v.Row.Keys == "" {
		keys = append([]string{v.Row.Pattern}, strings.Fields(strings.TrimPrefix(v.Row.Pattern, "one-of:"))...)
	}
	for _, k := range keys {
		k = strings.ToLower(k)
		if k == val || strings.HasPrefix(k, val+" ") {
			return true
		}
	}
	return false
}

// helpHaystackLines is the text a free word is looked for in: each non-empty
// field on its own line, case preserved, so Matcher.MatchRow checks each
// field independently (and \s never matches synthetic separators).
func helpHaystackLines(v HelpPageRow) string {
	parts := make([]string, 0, 11)
	for _, s := range []string{v.Key, v.Operation, v.Args, v.Mode, v.Table, v.Source, v.Guard, string(v.State), v.LevelName, v.Row.Hint, v.Help} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

// Toggle is the query with term added, or taken out when a chip of that
// exact text is there.
func (q HelpQuery) Toggle(term string) HelpQuery {
	out := HelpQuery{}
	found := false
	for _, t := range q.Terms {
		if t.Text == term {
			found = true
			continue
		}
		out.Terms = append(out.Terms, t)
	}
	if !found {
		out.Terms = append(out.Terms, ParseHelpQuery(term).Terms...)
	}
	return out
}

// With is the query with term added (once).
func (q HelpQuery) With(term string) HelpQuery {
	if slices.ContainsFunc(q.Terms, func(t HelpTerm) bool { return t.Text == term }) {
		return q
	}
	return HelpQuery{Terms: append(slices.Clone(q.Terms), ParseHelpQuery(term).Terms...)}
}

// Without is the query less chip n (1-based); a chip it does not have
// leaves the query as it is.
func (q HelpQuery) Without(n int) HelpQuery {
	if n < 1 || n > len(q.Terms) {
		return q
	}
	return HelpQuery{Terms: slices.Delete(slices.Clone(q.Terms), n-1, n)}
}

// HelpChipEffect is what removing one chip alone would bring back: the
// walk rows that match the other chips.
type HelpChipEffect struct {
	Index   int      `json:"index"`
	Term    HelpTerm `json:"term"`
	Without int      `json:"without"`
}

// Explain says, for a query that leaves no row of the walk, how many walk
// rows removing each chip alone brings back.
func (q HelpQuery) Explain(rows []HelpPageRow) []HelpChipEffect {
	return q.ExplainWith(rows, MatchSubstring, SearchCaseIgnore)
}

// ExplainWith is Explain evaluating free terms under matchMode and caseMode.
func (q HelpQuery) ExplainWith(rows []HelpPageRow, matchMode, caseMode string) []HelpChipEffect {
	var walk []HelpPageRow
	for _, v := range rows {
		if v.OnWalk() {
			walk = append(walk, v)
		}
	}
	out := make([]HelpChipEffect, len(q.Terms))
	for i, t := range q.Terms {
		out[i] = HelpChipEffect{Index: i + 1, Term: t, Without: len(q.Without(i+1).MatchingWith(walk, matchMode, caseMode))}
	}
	return out
}

// ExplainText is Explain as the empty list's lines.
func (q HelpQuery) ExplainText(rows []HelpPageRow) string {
	return q.ExplainTextWith(rows, MatchSubstring, SearchCaseIgnore)
}

// ExplainTextWith is ExplainText evaluating free terms under matchMode and caseMode.
func (q HelpQuery) ExplainTextWith(rows []HelpPageRow, matchMode, caseMode string) string {
	lines := []string{"No row of this walk matches."}
	for _, e := range q.ExplainWith(rows, matchMode, caseMode) {
		lines = append(lines, fmt.Sprintf("x %d drops %s: %d back", e.Index, e.Term.Text, e.Without))
	}
	if len(q.Terms) > 1 {
		lines = append(lines, "x a drops every chip")
	}
	return strings.Join(lines, "\n")
}

// HelpRefocus is what a focus change did to a query: the query on the new
// walk, the chips removed (they named the old walk and have no
// counterpart) and the chips moved to their counterpart, old then new.
type HelpRefocus struct {
	Query     HelpQuery   `json:"query"`
	Removed   []string    `json:"removed,omitempty"`
	Repointed [][2]string `json:"repointed,omitempty"`
}

// Note is the status line's sentence for the change, "" when nothing moved.
func (f HelpRefocus) Note() string {
	var parts []string
	if len(f.Removed) > 0 {
		parts = append(parts, "Removed "+strings.Join(f.Removed, ", ")+": it named the previous focus's walk.")
	}
	if len(f.Repointed) > 0 {
		moved := make([]string, len(f.Repointed))
		for i, p := range f.Repointed {
			moved[i] = p[0] + " to " + p[1]
		}
		parts = append(parts, "Moved "+strings.Join(moved, ", ")+".")
	}
	return strings.Join(parts, " ")
}

// Refocus carries the query from the old walk to the new one: a chip
// naming a level, table or noun of the old walk moves to its exact
// counterpart on the new walk (the old focus's to the new focus's) or is
// removed; chips that do not depend on the focus stay.
func (q HelpQuery) Refocus(old, cur HelpWalk) HelpRefocus {
	lc := strings.ToLower
	quote := func(v string) string {
		if strings.ContainsAny(v, " \t") {
			return `"` + v + `"`
		}
		return v
	}
	var oldFocus, newFocus HelpLevel
	if len(old.Levels) > 0 {
		oldFocus = old.Levels[0]
	}
	if len(cur.Levels) > 0 {
		newFocus = cur.Levels[0]
	}
	levels, tables, nouns, oldNouns := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, l := range cur.Levels {
		levels[lc(l.Label())], nouns[lc(l.Noun)] = true, true
		for _, t := range l.Tables {
			tables[lc(t.Label())] = true
		}
	}
	for _, l := range old.Levels {
		oldNouns[lc(l.Noun)] = true
	}
	at := refocusAt{oldFocus: oldFocus, newFocus: newFocus, levels: levels, tables: tables, nouns: nouns, oldNouns: oldNouns}
	var out HelpRefocus
	for _, t := range q.Terms {
		prefix := t.Field + ":"
		if t.Not {
			prefix = "-" + prefix
		}
		next, keep := t.Text, true
		for _, rule := range refocusRules {
			if rule.Field == t.Field {
				var moved string
				moved, keep = rule.Move(at, t.Value)
				if moved != "" {
					next = prefix + quote(moved)
				}
			}
		}
		if !keep {
			out.Removed = append(out.Removed, t.Text)
			continue
		}
		if next != t.Text {
			out.Repointed = append(out.Repointed, [2]string{t.Text, next})
		}
		out.Query.Terms = append(out.Query.Terms, ParseHelpQuery(next).Terms...)
	}
	return out
}

// refocusAt is what a refocus rule reads: the old and new walks' focus
// levels and the new walk's levels, tables and nouns (lower case), and the
// old walk's nouns.
type refocusAt struct {
	oldFocus, newFocus              HelpLevel
	levels, tables, nouns, oldNouns map[string]bool
}

// refocusRule moves a chip of one field to the new walk: the value it
// moves to ("" stays as it is) and whether it stays at all.
type refocusRule struct {
	Field string
	Move  func(at refocusAt, value string) (string, bool)
}

// refocusRules are the fields that name a walk: a level, a table, a noun.
var refocusRules = []refocusRule{
	{"level", func(at refocusAt, value string) (string, bool) {
		switch {
		case at.levels[value]:
			return "", true
		case value == strings.ToLower(at.oldFocus.Label()):
			return at.newFocus.Label(), true
		}
		return "", false
	}},
	{"table", func(at refocusAt, value string) (string, bool) {
		if at.tables[value] {
			return "", true
		}
		rest, ok := strings.CutPrefix(value, strings.ToLower(at.oldFocus.Noun)+" ")
		if !ok {
			return "", false
		}
		twin := strings.ToLower(at.newFocus.Noun) + " " + rest
		return twin, at.tables[twin]
	}},
	{"noun", func(at refocusAt, value string) (string, bool) {
		switch {
		case at.nouns[value]:
			return "", true
		case value == strings.ToLower(at.oldFocus.Noun):
			return at.newFocus.Noun, true
		}
		return "", !at.oldNouns[value]
	}},
}
