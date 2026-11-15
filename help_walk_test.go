package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

// helpWalkFixture is a page holding a panel holding a list of tile rows,
// the focus on the first row. The rows are a test file's, so their source
// is known.
func helpWalkFixture(t *testing.T, file string) *Runtime {
	t.Helper()
	r := NewRuntime()
	noop := func(Ctx) Result { return Done(nil) }
	for _, verb := range []string{"open", "cut", "fold", "show", "wide", "yank", "annotate"} {
		r.Define("tile").Verb(verb, noop, "The tile's "+verb)
	}
	r.Define("panel").Verb("refresh", noop, "Refresh the panel")
	r.Define("panel").Verb("clear", noop, "Clear the panel")
	r.DefineGuard("folded", func(*Runtime, KeyEvent) bool { return true })
	if _, err := r.AddPage("ctx", "Context", func(r *Runtime, w, h int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	r.Instance("panel", Parent("ctx"), Kind("panel"))
	list, err := r.AddList("rows", []ListItem{{ID: "a", Label: "one", Noun: "tile"}, {ID: "b", Label: "two", Noun: "tile"}}, Parent("panel"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.LoadDefaultBindings("test/tiles.json", []byte(file)); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("ctx"); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	return r
}

const helpWalkRows = `{"bindings": [
  {"context": "tile/normal", "bindings": [
    {"key": "x", "action": "tile.cut"},
    {"key": "z", "action": "tile.fold", "when": "folded"},
    {"key": "enter", "action": "tile.open"}
  ]},
  {"context": "tile/visual", "bindings": [{"key": "v", "action": "tile.show"}]},
  {"context": "tile/visual-line", "extends": "visual", "bindings": []},
  {"context": "tile", "bindings": [{"key": "W", "action": "tile.wide"}]},
  {"context": "panel/normal", "bindings": [
    {"key": "x", "action": "panel.clear"},
    {"key": "z", "action": "panel.refresh"}
  ]}
]}`

func helpEntry(t *testing.T, w HelpWalk, key, op string) HelpEntry {
	t.Helper()
	for _, e := range w.Entries {
		if e.Row.Keys == key && e.Row.Request.Key() == op {
			return e
		}
	}
	t.Fatalf("no entry %s %s on the walk", key, op)
	return HelpEntry{}
}

// The walk of a live row runs from the row out to app, each level's tables
// in the resolver's order: the mode table, what it extends, any mode.
func TestHelpWalkLevelsAndTables(t *testing.T) {
	r := helpWalkFixture(t, helpWalkRows)
	w := r.HelpWalkAt(r.Focus, ModeState{Name: "normal"}, nil)
	var nouns []string
	for _, l := range w.Levels {
		nouns = append(nouns, l.Noun)
	}
	if want := []string{"tile", "ochazuke.collection", "panel", "ochazuke.page", "app"}; !slices.Equal(nouns[:5], want) {
		t.Fatalf("levels %v, want %v first", nouns, want)
	}
	if w.Levels[0].Role != LevelFocus || w.Levels[4].Role != LevelApp || w.Levels[3].Role != LevelPage {
		t.Fatalf("roles %+v", w.Levels)
	}
	vl := r.HelpWalkAt(r.Focus, ModeState{Name: "visual-line"}, nil)
	var kinds []string
	for _, table := range vl.Levels[0].Tables {
		kinds = append(kinds, table.Kind+" "+table.Mode)
	}
	if want := []string{"mode visual-line", "extends visual"}; !slices.Equal(kinds[:2], want) || kinds[len(kinds)-1] != "any " {
		t.Fatalf("visual-line tables %v, want %v then any mode last", kinds, want)
	}
	if e := helpEntry(t, vl, "v", "tile.show"); e.Status() != StatusAnswers {
		t.Fatalf("v from visual-line is %s, want it read through extends", e.Status())
	}
}

// A key a nearer level takes shadows the outer row, named by it; a nearer
// guarded row makes the outer one maybe; a row with its own guard answers
// if it holds; a generated row steps aside for a declared prefix.
func TestHelpWalkStatuses(t *testing.T) {
	r := helpWalkFixture(t, helpWalkRows)
	if err := r.Bind(Binding{Noun: "tile", Mode: "normal", Keys: "y y", Request: Request{Noun: "tile", Verb: "yank"}, Generated: true},
		Binding{Noun: "panel", Mode: "normal", Keys: "y", Request: Request{Noun: "panel", Verb: "clear"}}); err != nil {
		t.Fatal(err)
	}
	w := r.HelpWalkAt(r.Focus, ModeState{Name: "normal"}, nil)
	cases := []struct {
		key, op string
		want    RowStatus
		by      string
	}{
		{"x", "tile.cut", StatusAnswers, ""},
		{"x", "panel.clear", StatusShadowed, "tile.cut"},
		{"z", "tile.fold", StatusGuarded, ""},
		{"z", "panel.refresh", StatusMaybe, "tile.fold"},
		{"y y", "tile.yank", StatusStepsAside, "y"},
	}
	for _, c := range cases {
		e := helpEntry(t, w, c.key, c.op)
		by := ""
		if e.By >= 0 {
			by = w.Entries[e.By].Row.Request.Key()
			if e.Status() == StatusStepsAside {
				by = w.Entries[e.By].Row.Keys // the first declared row on y, whoever's
			}
		}
		if e.Status() != c.want || by != c.by {
			t.Errorf("%s %s: %s by %q, want %s by %q", c.key, c.op, e.Status(), by, c.want, c.by)
		}
	}
	if c := w.Levels[2].Counts; c.Shadowed != 1 || c.Answers < 1 {
		t.Errorf("panel counts %+v", c)
	}
	rows := r.HelpRows(w)
	for _, v := range rows {
		if v.Key == "x" && v.Operation == "tile.cut" && v.Source != "test/tiles.json" {
			t.Errorf("x is defined in %q, want the file it loaded from", v.Source)
		}
		if v.Key == "y" && v.Noun == "panel" && v.Source != SourceGo {
			t.Errorf("a Go row's source is %q", v.Source)
		}
	}
}

// The query: quoted values, negation, unknown fields read as words; terms
// intersect; key: finds the sequences a key starts.
func TestHelpQuery(t *testing.T) {
	q := ParseHelpQuery(`key:"g d" -is:hidden pane odd:x`)
	var got []string
	for _, term := range q.Terms {
		got = append(got, strings.Join([]string{map[bool]string{true: "-"}[term.Not], term.Field, term.Value}, "|"))
	}
	if want := []string{"|key|g d", "-|is|hidden", "||pane", "||odd:x"}; !slices.Equal(got, want) {
		t.Fatalf("terms %v, want %v", got, want)
	}
	r := helpWalkFixture(t, helpWalkRows)
	w := r.HelpWalkAt(r.Focus, ModeState{Name: "normal"}, nil)
	rows := r.HelpRows(w)
	ids := func(text string) map[string]bool {
		out := map[string]bool{}
		for _, v := range ParseHelpQuery(text).Matching(rows) {
			out[v.ID] = true
		}
		return out
	}
	a, b, ab := ids("key:x"), ids("is:walk"), ids("key:x is:walk")
	if len(ab) == 0 || len(a) < len(ab) || len(b) <= len(ab) {
		t.Fatalf("key:x %d, is:walk %d, both %d", len(a), len(b), len(ab))
	}
	for id := range ab {
		if !a[id] || !b[id] {
			t.Fatalf("%s matches both terms but not each", id)
		}
	}
	if neg := ids("key:x -noun:panel"); len(neg) != len(a)-1 {
		t.Fatalf("negation kept %d of %d", len(neg), len(a))
	}
	if g := ids("key:g"); !slices.ContainsFunc(rows, func(v HelpPageRow) bool { return g[v.ID] && v.Row.Keys == "g g" }) {
		t.Fatalf("key:g does not find g g")
	}
}

// When the focus changes a chip naming the old walk moves to its exact
// counterpart or goes; the others stay; and a level chip moved keeps the
// new focus's rows.
func TestHelpRefocus(t *testing.T) {
	r := helpWalkFixture(t, helpWalkRows)
	old := r.HelpWalkAt(r.Focus, ModeState{Name: "normal"}, nil)
	cur := r.HelpWalkAtPlace(NounPlace{Noun: "panel", Ancestors: []string{"ochazuke.page"}, Page: "ctx"}, ModeState{Name: "normal"})
	f := ParseHelpQuery(`level:tile table:"tile normal" level:ochazuke.collection noun:tile key:x words`).Refocus(old, cur)
	if want := `level:panel table:"panel normal" noun:panel key:x words`; f.Query.String() != want {
		t.Fatalf("refocused %q, want %q", f.Query.String(), want)
	}
	if !slices.Equal(f.Removed, []string{"level:ochazuke.collection"}) || len(f.Repointed) != 3 || f.Note() == "" {
		t.Fatalf("removed %v repointed %v", f.Removed, f.Repointed)
	}
	rows := r.HelpRows(cur)
	shown := ParseHelpQuery("level:panel").Matching(rows)
	c := cur.Levels[0].Counts
	if n := len(shown); n != c.Rows+c.NoKey+countOther(rows, 0) {
		t.Fatalf("level:panel shows %d rows; the rail counts %+v", n, c)
	}
}

func countOther(rows []HelpPageRow, level int) int {
	n := 0
	for _, v := range rows {
		if v.Level == level && v.State == StatusOtherMode {
			n++
		}
	}
	return n
}

// The rail's counts and the list agree: a table node's rows are what its
// table: chip shows; a filter that empties the walk says which chips did.
func TestHelpCountsAndEmptyExplain(t *testing.T) {
	r := helpWalkFixture(t, helpWalkRows)
	w := r.HelpWalkAt(r.Focus, ModeState{Name: "normal"}, nil)
	rows := r.HelpRows(w)
	for _, l := range w.Levels {
		for _, table := range l.Tables {
			n := 0
			for _, v := range ParseHelpQuery(`table:"` + table.Label() + `"`).Matching(rows) {
				if v.Level == l.Index && v.State.OnWalk() {
					n++
				}
			}
			if n != len(table.Rows) {
				t.Errorf("%s: the chip shows %d, the rail counts %d", table.Label(), n, len(table.Rows))
			}
		}
	}
	q := ParseHelpQuery("key:zz is:shadowed")
	e := q.Explain(rows)
	shadowed := 0
	for _, v := range rows {
		if v.OnWalk() && v.State.Hidden() {
			shadowed++
		}
	}
	if len(e) != 2 || e[0].Without != shadowed || e[1].Without != 0 || !strings.Contains(q.ExplainText(rows), "x 1 drops key:zz") {
		t.Fatalf("explain %+v (shadowed %d)", e, shadowed)
	}
}

// Arranged by level, every walk level keeps its header, filtered or not,
// before its rows; a level with rows only in other modes is noted, and
// ModeFor walks a focus like that in its busiest mode.
func TestHelpArrange(t *testing.T) {
	r := helpWalkFixture(t, helpWalkRows)
	w := r.HelpWalkAt(r.Focus, ModeState{Name: "normal"}, nil)
	rows := r.HelpRows(w)
	a := Arrange(rows, w, ParseHelpQuery("verb:panel.refresh"), GroupingNamed("level"), SortNamed("walk"))
	headers := 0
	var panel, refresh = -1, -1
	for i, line := range a.Lines {
		if line.Kind == HelpLineGroup {
			headers++
			if line.Group.Label == "panel" {
				panel = i
			}
		}
		if line.Kind == HelpLineRow && line.Row.Operation == "panel.refresh" {
			refresh = i
		}
	}
	if headers < len(w.Levels) || panel < 0 || refresh < panel || a.Matched != 1 {
		t.Fatalf("headers %d of %d levels, panel at %d, refresh at %d, matched %d", headers, len(w.Levels), panel, refresh, a.Matched)
	}
	place := NounPlace{Noun: "tile"}
	if got := r.ModeFor(place, "insert"); got != "normal" {
		t.Fatalf("ModeFor tile in insert: %q, want normal", got)
	}
	r2 := helpWalkFixture(t, `{"bindings": [{"context": "tile/visual", "bindings": [{"key": "v", "action": "tile.show"}, {"key": "w", "action": "tile.wide"}]}]}`)
	if got := r2.ModeFor(place, "normal"); got != "visual" {
		t.Fatalf("ModeFor a noun with visual rows only: %q", got)
	}
	w2 := r2.HelpWalkAt(r2.Focus, ModeState{Name: "normal"}, nil)
	a2 := Arrange(r2.HelpRows(w2), w2, HelpQuery{}, GroupingNamed("level"), SortNamed("walk"))
	note := slices.IndexFunc(a2.Lines, func(l HelpLine) bool { return l.Kind == HelpLineNote })
	if note < 1 || a2.Lines[note].Mode != "visual" || a2.Lines[note-1].Kind != HelpLineGroup || a2.Lines[note].Level != 0 {
		t.Fatalf("no visual note under the tile level: %+v", a2.Lines)
	}
}

// A trace names the row that answers and why each other candidate did not.
func TestHelpTrace(t *testing.T) {
	r := helpWalkFixture(t, helpWalkRows)
	w := r.HelpWalkAt(r.Focus, ModeState{Name: "normal"}, nil)
	tr := w.Trace("z")
	if tr.Answer == nil || tr.Answer.Request.Key() != "panel.refresh" || len(tr.Steps) < 2 || !strings.HasPrefix(tr.Steps[0].Outcome, "answers only when folded") {
		t.Fatalf("trace z: %+v", tr)
	}
	if x := w.Trace("x"); x.Answer == nil || x.Answer.Request.Key() != "tile.cut" || !strings.HasPrefix(x.Steps[1].Outcome, "not reached") {
		t.Fatalf("trace x: %+v", x)
	}
}
