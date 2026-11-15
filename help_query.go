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

// HelpQueryFields are the fields a term may name.
var HelpQueryFields = []string{"key", "noun", "verb", "mode", "source", "guard", "level", "table", "status", "is"}

// helpIs are the is: words, each a test of a row.
var helpIs = map[string]func(HelpPageRow) bool{
	"bound":     func(v HelpPageRow) bool { return v.Kind == HelpRowBinding },
	"unbound":   func(v HelpPageRow) bool { return v.Kind == HelpRowVerb },
	"nokey":     func(v HelpPageRow) bool { return v.Kind == HelpRowVerb },
	"shadowed":  func(v HelpPageRow) bool { return v.State.Hidden() },
	"answers":   func(v HelpPageRow) bool { return v.State == StatusAnswers },
	"maybe":     func(v HelpPageRow) bool { return v.State == StatusMaybe },
	"guarded":   func(v HelpPageRow) bool { return v.Guard != "" },
	"hidden":    func(v HelpPageRow) bool { return v.Hidden },
	"generated": func(v HelpPageRow) bool { return v.Class == SourceGenerated },
	"user":      func(v HelpPageRow) bool { return v.Class == SourceUser },
	"walk":      func(v HelpPageRow) bool { return v.OnWalk() },
	"off":       func(v HelpPageRow) bool { return v.State == StatusOffWalk },
	"pattern":   func(v HelpPageRow) bool { return v.Kind == HelpRowBinding && v.Row.Keys == "" },
}

// HelpIsWords are the words is: takes, sorted.
func HelpIsWords() []string {
	out := make([]string, 0, len(helpIs))
	for w := range helpIs {
		out = append(out, w)
	}
	slices.Sort(out)
	return out
}

// HelpTerm is one chip of the query: its field ("" a free word), value
// (lower case), whether it is negated, and its text as typed.
type HelpTerm struct {
	Not   bool   `json:"not,omitempty"`
	Field string `json:"field,omitempty"`
	Value string `json:"value"`
	Text  string `json:"text"`
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
		if m[2] != "" && !slices.Contains(HelpQueryFields, field) {
			field, value = "", m[2]+":"+value
		}
		if value == "" {
			continue
		}
		q.Terms = append(q.Terms, HelpTerm{Not: m[1] != "", Field: field, Value: strings.ToLower(value), Text: m[0]})
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

// Matches reports whether a row passes every term.
func (q HelpQuery) Matches(v HelpPageRow) bool {
	for _, t := range q.Terms {
		if t.Matches(v) == t.Not {
			return false
		}
	}
	return true
}

// Matching are the rows that pass.
func (q HelpQuery) Matching(rows []HelpPageRow) []HelpPageRow {
	if q.Empty() {
		return rows
	}
	var out []HelpPageRow
	for _, v := range rows {
		if q.Matches(v) {
			out = append(out, v)
		}
	}
	return out
}

// Matches reports whether a row meets the term, negation aside.
func (t HelpTerm) Matches(v HelpPageRow) bool {
	val := t.Value
	low := strings.ToLower
	switch t.Field {
	case "key":
		return keyTermMatches(v, val)
	case "noun":
		return strings.Contains(low(v.Noun), val)
	case "verb":
		if prefix, ok := strings.CutSuffix(val, "*"); ok {
			return strings.HasPrefix(low(v.Verb), prefix) || strings.HasPrefix(low(v.Operation), prefix)
		}
		return low(v.Verb) == val || low(v.Operation) == val
	case "mode":
		return low(v.Mode) == val || val == "any" && v.Mode == modeLabel("")
	case "source":
		return v.Class == val || strings.Contains(low(v.Source), val)
	case "guard":
		return strings.Contains(low(v.Guard), val)
	case "level":
		return low(v.LevelName) == val
	case "table":
		return low(v.Table) == val
	case "status":
		return strings.Contains(string(v.State), val)
	case "is":
		is, ok := helpIs[val]
		return ok && is(v)
	}
	return strings.Contains(helpHaystack(v), val)
}

// keyTermMatches: the key or sequence itself, or a sequence it starts
// (key:g finds g d); a pattern row by its pattern or a one-of member.
func keyTermMatches(v HelpPageRow, val string) bool {
	if v.Kind != HelpRowBinding {
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

// helpHaystack is the text a free word is looked for in.
func helpHaystack(v HelpPageRow) string {
	return strings.ToLower(strings.Join([]string{v.Key, v.Operation, v.Args, v.Mode, v.Table, v.Source, v.Guard, string(v.State), v.LevelName, v.Row.Hint, v.Help}, " "))
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
	var walk []HelpPageRow
	for _, v := range rows {
		if v.OnWalk() {
			walk = append(walk, v)
		}
	}
	out := make([]HelpChipEffect, len(q.Terms))
	for i, t := range q.Terms {
		out[i] = HelpChipEffect{Index: i + 1, Term: t, Without: len(q.Without(i + 1).Matching(walk))}
	}
	return out
}

// ExplainText is Explain as the empty list's lines.
func (q HelpQuery) ExplainText(rows []HelpPageRow) string {
	lines := []string{"No row of this walk matches."}
	for _, e := range q.Explain(rows) {
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
	var out HelpRefocus
	for _, t := range q.Terms {
		prefix := t.Field + ":"
		if t.Not {
			prefix = "-" + prefix
		}
		next, keep := t.Text, true
		switch t.Field {
		case "level":
			switch {
			case levels[t.Value]:
			case t.Value == lc(oldFocus.Label()):
				next = prefix + quote(newFocus.Label())
			default:
				keep = false
			}
		case "table":
			switch {
			case tables[t.Value]:
			case strings.HasPrefix(t.Value, lc(oldFocus.Noun)+" "):
				twin := lc(newFocus.Noun) + t.Value[len(oldFocus.Noun):]
				next, keep = prefix+quote(twin), tables[twin]
			default:
				keep = false
			}
		case "noun":
			switch {
			case nouns[t.Value]:
			case t.Value == lc(oldFocus.Noun):
				next = prefix + quote(newFocus.Noun)
			case oldNouns[t.Value]:
				keep = false
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
