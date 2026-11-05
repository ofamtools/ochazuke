package ochazuke

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMatchPositions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode, text, query, caseMode string
		want                        []int
	}{
		{MatchSubstring, "Alpha beta ALPHA", "alpha beta", SearchCaseSmart, []int{0, 1, 2, 3, 4, 6, 7, 8, 9, 11, 12, 13, 14, 15}},
		{MatchSubstring, "banana", "ana", SearchCaseSmart, []int{1, 2, 3, 4, 5}},
		{MatchSubstring, "Alpha beta", "Alpha", SearchCaseSmart, []int{0, 1, 2, 3, 4}},
		{MatchSubstring, "alpha beta", "Alpha", SearchCaseSmart, nil},
		{MatchSubstring, "Été 日本", "été", SearchCaseSmart, []int{0, 1, 2}},
		{MatchSubstring, "Été 日本", "日本", SearchCaseSensitive, []int{4, 5}},
		{MatchSubstring, "alpha", "alpha beta", SearchCaseIgnore, nil},
		{MatchFuzzy, "src/main.go", "smg", SearchCaseSmart, []int{0, 4, 9}},
		{MatchFuzzy, "src/main.go", "src ma", SearchCaseSmart, []int{0, 1, 2, 3, 4, 5}},
		{MatchFuzzy, "Été 日本", "é日", SearchCaseSmart, []int{0, 4}},
		{MatchFuzzy, "été 日本", "É日", SearchCaseSmart, nil},
		{MatchFuzzy, "été 日本", "É日", SearchCaseIgnore, []int{0, 4}},
		{MatchFuzzy, "alpha", "az", SearchCaseSensitive, nil},
		{MatchRegex, "Été 日本 ÉTÉ", "été|日本", SearchCaseSmart, []int{0, 1, 2, 4, 5, 7, 8, 9}},
		{MatchRegex, "Été été", "Été", SearchCaseSmart, []int{0, 1, 2}},
		{MatchRegex, "alpha beta", `\S+ta$`, SearchCaseSmart, []int{6, 7, 8, 9}},
		{MatchRegex, "abc", "^|$", SearchCaseSmart, nil},
		{MatchRegex, "abc", "(", SearchCaseIgnore, nil},
		{MatchRegex, "abc", "z", SearchCaseIgnore, nil},
		{MatchFuzzy, "abc", " ", SearchCaseSmart, nil},
		{MatchSubstring, "abc", "", SearchCaseSmart, nil},
	} {
		got := matchPositions(tc.mode, tc.text, tc.query, tc.caseMode)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s %s %q in %q: got %v, want %v", tc.mode, tc.caseMode, tc.query, tc.text, got, tc.want)
		}
	}
}

func TestMatchMarksKeepSelectedBarAndOffText(t *testing.T) {
	t.Parallel()
	m := NewMatcher(MatchFuzzy, SearchCaseSmart, "é日")
	for _, theme := range []string{"dark", "light", "mono"} {
		for _, level := range []string{ChromeRich, ChromeSubtle, ChromeOff} {
			s := NewStyles(Appearance{Theme: theme}).WithChrome(level, []string{"mode.insert", "mode"})
			marks := s.matchSpans(m, "Été 日本", "match")
			segs := cellSegs(marks, "Été 日本", 0, 9)
			if level == ChromeOff {
				if len(marks) != 0 {
					t.Fatal("off must not add marks")
				}
				continue
			}
			selected := paintLine("Été 日本", 9, segs, &s, true, false, span{})
			if ansi.Strip(selected) != "Été 日本 " {
				t.Fatalf("match marks changed cells: %q", selected)
			}
			if !strings.Contains(selected, "\x1b[7;1mÉ") || strings.Contains(selected, "38;") || strings.Contains(selected, "48;") {
				t.Fatalf("%s %s: selected matches must be bold alone inside reverse video: %q", theme, level, selected)
			}
			if plain := ansi.Strip(paintLine("Été 日本", 9, segs, &s, false, false, span{})); plain != "Été 日本" {
				t.Fatalf("marks changed text: %q", plain)
			}
		}
	}
}

func TestPaletteMatchDoesNotChangeCompletionJSON(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(PaletteEntry{Value: "ping", Kind: "verb", Label: "ping", Match: []int{0, 1}})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"value":"ping","kind":"verb","label":"ping"}` {
		t.Fatalf("completion contract changed: %s", got)
	}
}
