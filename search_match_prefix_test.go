package ochazuke

import (
	"slices"
	"testing"
)

// A query spelled at the start of a text or of one of its words ranks
// above every scattered (fuzzy) hit, in every match mode that scores.
func TestMatcherPrefixBeatsScattered(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		query string
		texts []string // in the order the score must put them, best first
	}{
		{"ta", []string{"tag", "task", "attach", "meta-data"}},
		{"con", []string{"config", "context-item", "cmd.cmd.on"}},
		{"con", []string{"context-item", "cmd.cmd.on"}},
		{"ta", []string{"git.tag", "attach"}},
		{"pin", []string{"pin", "spin", "p-i-n"}},
	} {
		for _, mode := range []string{MatchFuzzy, MatchSubstring} {
			m := NewMatcher(mode, SearchCaseIgnore, tc.query)
			type scored struct {
				text  string
				score int
			}
			var got []scored
			for _, text := range tc.texts {
				if score, ok := m.ScorePrepared(m.Prepare(text)); ok {
					got = append(got, scored{text, score})
				}
			}
			slices.SortStableFunc(got, func(a, b scored) int { return b.score - a.score })
			var order []string
			for _, g := range got {
				order = append(order, g.text)
			}
			if len(order) == 0 || order[0] != tc.texts[0] {
				t.Fatalf("%s %q: best is %v, want %q first", mode, tc.query, order, tc.texts[0])
			}
		}
	}
	// The tag beats the scattered hit however the scattered one scores.
	m := NewMatcher(MatchFuzzy, SearchCaseIgnore, "ta")
	tag, _ := m.ScorePrepared("tag")
	attach, ok := m.ScorePrepared("attach")
	if !ok || tag <= attach {
		t.Fatalf("tag %d, attach %d: a prefix must outrank a scattered hit", tag, attach)
	}
	if _, ok := m.ScorePrepared("xyz"); ok {
		t.Fatal("xyz matched")
	}
}
