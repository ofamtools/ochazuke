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

// HelpGrouping is one way the page groups its rows (o cycles them): by the
// walk's levels (every level keeps its header, a row of no level groups
// under its noun after them), or by what Of names (noun, verb).
type HelpGrouping struct {
	Name   string
	Levels bool
	Of     func(HelpPageRow) string
}

// HelpSort is one order inside a group (O cycles them): Cmp, walk order
// breaking ties; nil is walk order alone.
type HelpSort struct {
	Name string
	Cmp  func(a, b HelpPageRow) int
}

// HelpGroupings and HelpSorts are what o and O cycle through, the first
// the default.
var (
	HelpGroupings = []HelpGrouping{
		{Name: "level", Levels: true},
		{Name: "noun", Of: func(v HelpPageRow) string { return v.Noun }},
		{Name: "verb", Of: func(v HelpPageRow) string { return v.Verb }},
	}
	HelpSorts = []HelpSort{
		{Name: "walk"},
		{Name: "key", Cmp: func(a, b HelpPageRow) int { return strings.Compare(a.Key, b.Key) }},
		{Name: "verb", Cmp: func(a, b HelpPageRow) int { return strings.Compare(a.Verb, b.Verb) }},
		{Name: "noun", Cmp: func(a, b HelpPageRow) int { return strings.Compare(a.Noun, b.Noun) }},
	}
)

// GroupingNamed is the grouping of that name, the default for none.
func GroupingNamed(name string) HelpGrouping {
	for _, g := range HelpGroupings {
		if g.Name == name {
			return g
		}
	}
	return HelpGroupings[0]
}

// SortNamed is the sort of that name, the default for none.
func SortNamed(name string) HelpSort {
	for _, s := range HelpSorts {
		if s.Name == name {
			return s
		}
	}
	return HelpSorts[0]
}

// groupNames and sortNames are the groupings' and sorts' names.
func groupNames() []string {
	out := make([]string, len(HelpGroupings))
	for i, g := range HelpGroupings {
		out[i] = g.Name
	}
	return out
}

func sortNames() []string {
	out := make([]string, len(HelpSorts))
	for i, s := range HelpSorts {
		out[i] = s.Name
	}
	return out
}

// HelpLineKind is what a line of the arranged list is.
type HelpLineKind int

const (
	// HelpLineGroup is a group's header.
	HelpLineGroup HelpLineKind = iota
	// HelpLineRow is a row.
	HelpLineRow
	// HelpLineNote says a walk level's rows are all in other modes.
	HelpLineNote
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
	Kind  HelpLineKind `json:"kind"`
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

// groupID is the group a row falls in under g.
func (g HelpGrouping) groupID(v HelpPageRow) string {
	switch {
	case !g.Levels:
		return g.Name + ":" + g.Of(v)
	case v.Level >= 0:
		return fmt.Sprintf("level:%d", v.Level)
	}
	return "off:" + v.Noun
}

// rowGroups gathers rows into groups: with a level grouping every walk
// level first, so it keeps its header.
type rowGroups struct {
	by    map[string]*HelpGroup
	order []string
}

func (gs *rowGroups) ensure(id string, make func() HelpGroup) *HelpGroup {
	if g := gs.by[id]; g != nil {
		return g
	}
	g := make()
	g.ID = id
	gs.by[id] = &g
	gs.order = append(gs.order, id)
	return &g
}

// gather puts the rows that match into their groups and counts every row.
func gather(rows, hits []HelpPageRow, w HelpWalk, grouping HelpGrouping) *rowGroups {
	gs := &rowGroups{by: map[string]*HelpGroup{}}
	if grouping.Levels {
		for _, l := range w.Levels {
			sub := l.Role
			if l.Instance != "" {
				sub = "instance " + l.Instance
			}
			gs.ensure(fmt.Sprintf("level:%d", l.Index), func() HelpGroup { return HelpGroup{Label: l.Label(), Level: l.Index, Sub: sub} })
		}
	}
	for _, v := range hits {
		id := grouping.groupID(v)
		g := gs.ensure(id, func() HelpGroup {
			g := HelpGroup{Label: id[strings.Index(id, ":")+1:], Level: -1}
			if strings.HasPrefix(id, "off:") {
				g.Sub = "not on this walk"
			}
			if grouping.Levels && v.Level >= 0 {
				g.Level = v.Level
			}
			return g
		})
		g.Rows = append(g.Rows, v)
	}
	for _, v := range rows {
		if g := gs.by[grouping.groupID(v)]; g != nil {
			g.Total++
		}
	}
	return gs
}

// sorted orders the groups: walk levels in walk order, then (grouped by
// noun or verb) the groups with a walk row first, then by label.
func (gs *rowGroups) sorted(levels bool) []*HelpGroup {
	rank := func(g *HelpGroup) int {
		if g.Level >= 0 {
			return g.Level
		}
		return 1 << 20
	}
	onWalk := func(g *HelpGroup) bool { return slices.ContainsFunc(g.Rows, HelpPageRow.OnWalk) }
	out := make([]*HelpGroup, 0, len(gs.order))
	for _, id := range gs.order {
		if g := gs.by[id]; len(g.Rows) > 0 || g.Level >= 0 {
			out = append(out, g)
		}
	}
	slices.SortStableFunc(out, func(a, b *HelpGroup) int {
		c := cmp.Compare(rank(a), rank(b))
		if c == 0 && !levels {
			c = cmpBool(onWalk(b), onWalk(a))
		}
		return cmp.Or(c, strings.Compare(a.Label, b.Label))
	})
	return out
}

// otherModeCounts are, per walk level, the modes its other-mode rows are
// in and how many.
func otherModeCounts(rows []HelpPageRow) map[int]map[string]int {
	out := map[int]map[string]int{}
	for _, v := range rows {
		if v.State == StatusOtherMode {
			if out[v.Level] == nil {
				out[v.Level] = map[string]int{}
			}
			out[v.Level][v.Mode]++
		}
	}
	return out
}

// Arrange groups and sorts the rows that match q. Grouped by level, every
// walk level keeps its header, matched or not, so the walk never loses a
// level, and a level with no rows in the walk's mode but rows in others
// gets a note naming them.
func Arrange(rows []HelpPageRow, w HelpWalk, q HelpQuery, grouping HelpGrouping, sort HelpSort) HelpArrangement {
	hits := q.Matching(rows)
	var other map[int]map[string]int
	if grouping.Levels {
		other = otherModeCounts(rows)
	}
	out := HelpArrangement{Query: q, Matched: len(hits), Total: len(rows)}
	for _, g := range gather(rows, hits, w, grouping).sorted(grouping.Levels) {
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
	slices.SortFunc(list, func(a, b ModeCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Mode, b.Mode))
	})
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

// IsRow reports whether the line is a binding row (not a verb with no key,
// a header or a note).
func (l HelpLine) IsRow() bool { return l.Kind == HelpLineRow && !l.Row.Unbound }

// SortHelpRows orders rows inside a group: verbs with no key after the
// bound rows, then by sort, walk order breaking ties.
func SortHelpRows(rows []HelpPageRow, sort HelpSort) []HelpPageRow {
	out := slices.Clone(rows)
	slices.SortStableFunc(out, func(a, b HelpPageRow) int {
		c := cmpBool(a.Unbound, b.Unbound)
		if c == 0 && sort.Cmp != nil {
			c = sort.Cmp(a, b)
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

// TraceHit is how a row touches a traced key.
type TraceHit int

const (
	// TraceExact: the row's keys are the traced key.
	TraceExact TraceHit = iota + 1
	// TraceLonger: the traced key starts the row's longer sequence.
	TraceLonger
	// TracePattern: the row's pattern takes the traced key.
	TracePattern
	// TraceStarts: the row's key starts the traced sequence.
	TraceStarts
)

// HelpTraceStep is one row on the walk a traced key touches: how and what
// came of it.
type HelpTraceStep struct {
	Level   string   `json:"level"`
	Table   string   `json:"table"`
	Row     Binding  `json:"row"`
	Hit     TraceHit `json:"hit"`
	Outcome string   `json:"outcome"`
	Capture bool     `json:"capture,omitempty"`
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
		hit := traceHit(r, key, tokens)
		if hit == 0 {
			continue
		}
		var outcome string
		switch {
		case e.State == StatusStepsAside:
			outcome = "steps aside: a declared row on this walk binds its keys"
		case hit == TraceLonger:
			outcome = "waits: this key starts the longer sequence " + r.Keys
		case hit == TraceStarts:
			outcome = "answers " + tokens[0] + " alone; the sequence is looked up after it"
		case answer != nil:
			outcome = "not reached: " + rowKey(answer.Row) + " at " + w.LevelLabel(answer.Level) + " answered first"
		case e.Guard != "":
			outcome = "answers only when " + e.Guard + "; otherwise the walk goes on"
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

// traceHit is how row touches the traced key (tokens its keys), 0 when it
// does not.
func traceHit(row Binding, key string, tokens []string) TraceHit {
	switch {
	case row.Keys == key && key != "":
		return TraceExact
	case row.Keys != "" && strings.HasPrefix(row.Keys, key+" "):
		return TraceLonger
	case row.Keys == "" && len(tokens) == 1:
		if ok, _ := matchesPattern(row.Pattern, MacroKey{Key: key}); ok {
			return TracePattern
		}
	case len(tokens) > 1 && row.Keys == tokens[0]:
		return TraceStarts
	}
	return 0
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
