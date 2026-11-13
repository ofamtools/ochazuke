package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// hintGroupsFixture is a list whose rows answer two verbs of their own
// noun, a hinted row and one verb of another noun, then the app's.
func hintGroupsFixture(t *testing.T) *Model {
	t.Helper()
	m := hintFixture(t)
	r := m.Runtime
	detail := Do("row.edit")
	detail.Hint = "J/K detail"
	r.Define("row").
		Verb("edit", func(Ctx) Result { return Result{} }).
		Verb("share", func(Ctx) Result { return Result{} }).
		Mode("normal").
		Bind("e", Do("row.edit")).
		Bind("r", Do("row.share")).
		Bind("J", detail).
		Bind("ctrl+o", Do("palette.open"))
	return m
}

// hints.group noun: a run of hints of one noun is written under it once,
// " │ " where the group meets other hints; a hinted row (J/K detail) names
// no noun and stays in its run, and a lone hint keeps "key noun.verb".
// The data line (BindingHints, JoinHints) is unchanged.
func TestHintsGroupNoun(t *testing.T) {
	m := hintGroupsFixture(t)
	r := m.Runtime
	cells := HintCells(r, r.Focus)
	got := HintText("", cells, 0)
	if !strings.HasPrefix(got, "row: e edit · r share · J/K detail │ ctrl+o palette.open") {
		t.Fatalf("grouped line %q", got)
	}
	if strings.Contains(got, "row.") {
		t.Fatalf("a grouped hint repeats its noun: %q", got)
	}
	if plain := BindingHints(r, r.Focus); !strings.HasPrefix(plain, "e row.edit · r row.share") || plain != JoinHints(cells) {
		t.Fatalf("BindingHints %q", plain)
	}
	if got, want := ansi.Strip(m.View().Content), got; !strings.Contains(got, ansi.Truncate(want, 40, "")) {
		t.Fatalf("the footer does not draw the grouped line:\n%s", got)
	}
}

// hints.group off: the drawn line is today's "key noun.verb" line byte
// for byte.
func TestHintsGroupOff(t *testing.T) {
	m := hintGroupsFixture(t)
	r := m.Runtime
	if err := r.Settings().Set(HintsGroupSetting, HintsGroupOff); err != nil {
		t.Fatal(err)
	}
	cells := HintCells(r, r.Focus)
	plain := JoinHints(cells)
	for _, lead := range []string{"", "Notice: hi"} {
		want := plain
		if lead != "" {
			want = lead + hintSep + plain
		}
		for w := 0; w <= ansi.StringWidth(want)+2; w++ {
			cut := want
			if w > 0 {
				cut = ansi.Truncate(want, w, "…")
			}
			if got := HintText(lead, cells, w); got != cut {
				t.Fatalf("lead %q width %d: %q, want %q", lead, w, got, cut)
			}
		}
	}
}

// At every width the grouped line shows at least the whole hints the
// ungrouped line shows.
func TestHintsGroupKeepsEveryHintThatFitted(t *testing.T) {
	m := hintGroupsFixture(t)
	cells := HintCells(m.Runtime, m.Runtime.Focus)
	plain := JoinHints(cells)
	// whole counts the hints a line shows whole, in either form.
	forms := map[string]bool{}
	for _, c := range cells {
		forms[c.String()] = true
		if c.Noun != "" {
			forms[c.Noun+": "+c.Key+" "+c.Verb], forms[c.Key+" "+c.Verb] = true, true
		}
	}
	whole := func(line string) int {
		n := 0
		for _, part := range strings.Split(line, hintGroupSep) {
			for _, token := range strings.Split(part, hintSep) {
				// A cut in the separator after a hint leaves it whole.
				if forms[strings.TrimRight(strings.TrimSuffix(token, "…"), " ·│")] {
					n++
				}
			}
		}
		return n
	}
	for w := 1; w <= ansi.StringWidth(plain)+2; w++ {
		grouped, ungrouped := HintText("", cells, w), ansi.Truncate(plain, w, "…")
		if ansi.StringWidth(grouped) > w {
			t.Fatalf("width %d: %q is wider", w, grouped)
		}
		if g, u := whole(grouped), whole(ungrouped); g < u {
			t.Fatalf("width %d: grouped %q shows %d hints, ungrouped %q %d", w, grouped, g, ungrouped, u)
		}
	}
}
