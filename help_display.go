package ochazuke

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// The binding help page's arrangement and traces: the rows grouped (by
// level, noun or verb) and sorted (walk order, key, verb, noun) into lines,
// a walk level with rows only in other modes noted, and the trace of one
// key from the walk's focus. Values: no drawing.

// Groupings and sorts the page cycles through (o and O).
var (
	HelpGroupings = []string{"level", "noun", "verb"}
	HelpSorts     = []string{"walk", "key", "verb", "noun"}
)

// Line kinds of HelpLine.Kind.
const (
	HelpLineGroup = "group"
	HelpLineRow   = "row"
	HelpLineNote  = "note"
)

// HelpGroup is one group of the arrangement: its id ("level:2",
// "noun:pane", "verb:archive"), label, the walk level it is (-1 for a
// group that is no level), its rows, and the rows it has unfiltered.
type HelpGroup struct {
	ID    string        `json:"id"`
	Label string        `json:"label"`
	Level int           `json:"level"`
	Sub   string        `json:"sub,omitempty"`
	Rows  []HelpPageRow `json:"rows"`
	Total int           `json:"total"`
}

// HelpLine is one line of the arranged list: a group header, a row, or a
// note under a walk level whose rows are all in other modes (Modes, the
// busiest first; Mode is the one enter walks in).
type HelpLine struct {
	Kind  string       `json:"kind"`
	Group *HelpGroup   `json:"group,omitempty"`
	Row   *HelpPageRow `json:"row,omitempty"`
	Level int          `json:"level"`
	Modes []ModeCount  `json:"modes,omitempty"`
	Mode  string       `json:"mode,omitempty"`
}

// ModeCount is a mode and how many rows it has.
type ModeCount struct {
	Mode  string `json:"mode"`
	Count int    `json:"count"`
}

// HelpArrangement is the arranged list: its lines, the query, how many rows
// match and how many there are.
type HelpArrangement struct {
	Lines   []HelpLine `json:"lines"`
	Query   HelpQuery  `json:"query"`
	Matched int        `json:"matched"`
	Total   int        `json:"total"`
}

// Arrange groups and sorts the rows that match q. With group level every
// walk level keeps its header, matched or not, so the walk never loses a
// level; a row of no level groups under its noun after them.
func Arrange(rows []HelpPageRow, w HelpWalk, q HelpQuery, group, sort string) HelpArrangement {
	hits := q.Matching(rows)
	groupOf := func(v HelpPageRow) string {
		switch {
		case group == "verb":
			return "verb:" + v.Verb
		case group == "noun":
			return "noun:" + v.Noun
		case v.Level >= 0:
			return fmt.Sprintf("level:%d", v.Level)
		}
		return "off:" + v.Noun
	}
	groups := map[string]*HelpGroup{}
	var order []string
	ensure := func(id string, make func() HelpGroup) *HelpGroup {
		if g := groups[id]; g != nil {
			return g
		}
		g := make()
		g.ID = id
		groups[id] = &g
		order = append(order, id)
		return &g
	}
	if group == "level" || group == "" {
		for _, l := range w.Levels {
			sub := l.Role
			if l.Instance != "" {
				sub = "instance " + l.Instance
			}
			ensure(fmt.Sprintf("level:%d", l.Index), func() HelpGroup { return HelpGroup{Label: l.Label(), Level: l.Index, Sub: sub} })
		}
	}
	for _, v := range hits {
		id := groupOf(v)
		g := ensure(id, func() HelpGroup {
			label := id[strings.Index(id, ":")+1:]
			sub := ""
			if strings.HasPrefix(id, "off:") {
				sub = "not on this walk"
			}
			level := -1
			if strings.HasPrefix(id, "level:") {
				level = v.Level
			}
			return HelpGroup{Label: label, Level: level, Sub: sub}
		})
		g.Rows = append(g.Rows, v)
	}
	for _, v := range rows {
		if g := groups[groupOf(v)]; g != nil {
			g.Total++
		}
	}
	rank := func(g *HelpGroup) int {
		if g.Level >= 0 {
			return g.Level
		}
		return 1 << 20
	}
	onWalk := func(g *HelpGroup) bool { return slices.ContainsFunc(g.Rows, HelpPageRow.OnWalk) }
	slices.SortStableFunc(order, func(a, b string) int {
		ga, gb := groups[a], groups[b]
		if c := cmp.Compare(rank(ga), rank(gb)); c != 0 {
			return c
		}
		if group != "level" {
			if c := cmpBool(onWalk(gb), onWalk(ga)); c != 0 {
				return c
			}
		}
		return strings.Compare(ga.Label, gb.Label)
	})
	other := map[int]map[string]int{}
	if group == "level" {
		for _, v := range rows {
			if v.State == StatusOtherMode {
				if other[v.Level] == nil {
					other[v.Level] = map[string]int{}
				}
				other[v.Level][v.Mode]++
			}
		}
	}
	out := HelpArrangement{Query: q, Matched: len(hits), Total: len(rows)}
	for _, id := range order {
		g := groups[id]
		if len(g.Rows) == 0 && g.Level < 0 {
			continue
		}
		g.Rows = SortHelpRows(g.Rows, sort)
		out.Lines = append(out.Lines, HelpLine{Kind: HelpLineGroup, Group: g, Level: g.Level})
		if g.Level >= 0 && g.Level < len(w.Levels) && w.Levels[g.Level].Counts.Rows == 0 && len(other[g.Level]) > 0 {
			out.Lines = append(out.Lines, otherModesNote(g.Level, other[g.Level]))
		}
		for i := range g.Rows {
			out.Lines = append(out.Lines, HelpLine{Kind: HelpLineRow, Row: &g.Rows[i], Level: g.Rows[i].Level})
		}
	}
	return out
}

// otherModesNote is the note under a walk level whose rows are all in
// other modes: the modes, busiest first, and the one to walk in.
func otherModesNote(level int, modes map[string]int) HelpLine {
	var list []ModeCount
	for m, n := range modes {
		list = append(list, ModeCount{Mode: m, Count: n})
	}
	slices.SortFunc(list, func(a, b ModeCount) int { return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Mode, b.Mode)) })
	mode := list[0].Mode
	if mode == modeLabel("") {
		mode = ""
	}
	return HelpLine{Kind: HelpLineNote, Level: level, Modes: list, Mode: mode}
}

// NoteText is a note line's text.
func (l HelpLine) NoteText() string {
	parts := make([]string, len(l.Modes))
	for i, m := range l.Modes {
		parts[i] = fmt.Sprintf("%s %d", m.Mode, m.Count)
	}
	return "no rows in this mode; rows in " + strings.Join(parts, ", ")
}

// SortHelpRows orders rows inside a group: verbs with no key after the
// bound rows, then by sort (walk order, key, verb or noun), walk order
// breaking ties.
func SortHelpRows(rows []HelpPageRow, sort string) []HelpPageRow {
	out := slices.Clone(rows)
	slices.SortStableFunc(out, func(a, b HelpPageRow) int {
		if c := cmpBool(a.Kind == HelpRowVerb, b.Kind == HelpRowVerb); c != 0 {
			return c
		}
		var c int
		switch sort {
		case "key":
			c = strings.Compare(a.Key, b.Key)
		case "verb":
			c = strings.Compare(a.Verb, b.Verb)
		case "noun":
			c = strings.Compare(a.Noun, b.Noun)
		}
		return cmp.Or(c, cmp.Compare(a.Order, b.Order))
	})
	return out
}

// nextOf is the value step places after current in list, wrapping.
func nextOf(list []string, current string, step int) string {
	i := max(0, slices.Index(list, current))
	n := len(list)
	return list[((i+step)%n+n)%n]
}

// HelpTraceStep is one row on the walk a traced key touches: how (exact,
// longer: the key starts this row's sequence, prefix: this row's key
// starts the traced sequence, pattern) and what came of it.
type HelpTraceStep struct {
	Level   string  `json:"level"`
	Table   string  `json:"table"`
	Row     Binding `json:"row"`
	Hit     string  `json:"hit"`
	Outcome string  `json:"outcome"`
	Capture bool    `json:"capture,omitempty"`
}

// HelpTrace is the trace of a key from a walk's focus: every row that
// touches it, in resolution order, and the row that answers (nil: none
// unguarded does; the key goes past app or waits).
type HelpTrace struct {
	Key    string          `json:"key"`
	Steps  []HelpTraceStep `json:"steps"`
	Answer *Binding        `json:"answer,omitempty"`
}

// Trace follows key (a key or a sequence, "g d") along the walk: the
// capture pass then the bubbling pass, as the resolver reads them.
func (w HelpWalk) Trace(key string) HelpTrace {
	key = strings.Join(strings.Fields(key), " ")
	tokens := strings.Fields(key)
	t := HelpTrace{Key: key}
	var answer *HelpEntry
	for i := range w.Entries {
		e := &w.Entries[i]
		r := e.Row
		hit := ""
		switch {
		case r.Keys == key && key != "":
			hit = "exact"
		case r.Keys != "" && strings.HasPrefix(r.Keys, key+" "):
			hit = "longer"
		case r.Keys == "" && len(tokens) == 1:
			if ok, _ := matchesPattern(r.Pattern, MacroKey{Key: key}); ok {
				hit = "pattern"
			}
		case len(tokens) > 1 && r.Keys == tokens[0]:
			hit = "prefix"
		}
		if hit == "" {
			continue
		}
		var outcome string
		switch {
		case e.State == StatusStepsAside:
			outcome = "steps aside: a declared row on this walk binds its keys"
		case hit == "longer":
			outcome = "waits: this key starts the longer sequence " + r.Keys
		case hit == "prefix":
			outcome = "answers " + tokens[0] + " alone; the sequence is looked up after it"
		case answer != nil:
			outcome = "not reached: " + rowKey(answer.Row) + " at " + w.LevelLabel(answer.Level) + " answered first"
		case r.When != "":
			outcome = "answers only when " + r.When + "; otherwise the walk goes on"
		default:
			outcome = "ANSWERS"
			answer = e
		}
		t.Steps = append(t.Steps, HelpTraceStep{Level: w.LevelLabel(e.Level), Table: w.TableOf(*e).Label(), Row: r, Hit: hit, Outcome: outcome, Capture: e.Capture})
	}
	if answer != nil {
		row := answer.Row
		t.Answer = &row
	}
	return t
}

// Lines are the trace as the page's detail lines.
func (t HelpTrace) Lines(r *Runtime) []string {
	if len(t.Steps) == 0 {
		return []string{"Trace " + t.Key + ": no row on this walk takes it; the key goes past app."}
	}
	head := "Trace " + t.Key + ": "
	if t.Answer != nil {
		head += r.displayRequest(t.Answer.Request) + " answers."
	} else {
		head += "no unguarded row answers it here."
	}
	lines := []string{head}
	for _, s := range t.Steps {
		pass := ""
		if s.Capture {
			pass = " (capture)"
		}
		lines = append(lines, fmt.Sprintf("  %s%s · %s → %s: %s", s.Level, pass, rowKey(s.Row), r.displayRequest(s.Row.Request), s.Outcome))
	}
	return lines
}
