package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// Each matcher under each search.case value: smart folds unless the query
// holds a capital, ignore always folds, sensitive never does.
func TestMatchersUnderEverySearchCase(t *testing.T) {
	texts := []string{"Alpha Beta", "alphabet", "gamma-ray", "ALPHA"}
	for _, tc := range []struct {
		mode, caseMode, query string
		want                  []string
	}{
		// substring: every word, any order
		{MatchSubstring, SearchCaseSmart, "alpha", []string{"Alpha Beta", "alphabet", "ALPHA"}},
		{MatchSubstring, SearchCaseSmart, "Alpha", []string{"Alpha Beta"}},
		{MatchSubstring, SearchCaseIgnore, "Alpha", []string{"Alpha Beta", "alphabet", "ALPHA"}},
		{MatchSubstring, SearchCaseSensitive, "alpha", []string{"alphabet"}},
		{MatchSubstring, SearchCaseSmart, "beta alpha", []string{"Alpha Beta"}},
		{MatchSubstring, SearchCaseSmart, "apa", nil},
		// fuzzy: runes in order
		{MatchFuzzy, SearchCaseSmart, "apa", []string{"Alpha Beta", "alphabet", "ALPHA"}},
		{MatchFuzzy, SearchCaseSmart, "gry", []string{"gamma-ray"}},
		{MatchFuzzy, SearchCaseSmart, "APA", []string{"ALPHA"}},
		{MatchFuzzy, SearchCaseIgnore, "APA", []string{"Alpha Beta", "alphabet", "ALPHA"}},
		{MatchFuzzy, SearchCaseSensitive, "apa", []string{"alphabet"}},
		{MatchFuzzy, SearchCaseSmart, "beta alpha", nil}, // in order, the space included
		// regex: RE2, folded through (?i)
		{MatchRegex, SearchCaseSmart, "^alpha", []string{"Alpha Beta", "alphabet", "ALPHA"}},
		{MatchRegex, SearchCaseSmart, "^Alpha", []string{"Alpha Beta"}},
		{MatchRegex, SearchCaseSmart, `\Sbet$`, []string{"alphabet"}}, // \S is not a capital
		{MatchRegex, SearchCaseIgnore, "^ALPHA$", []string{"ALPHA"}},
		{MatchRegex, SearchCaseIgnore, "^Alpha$", []string{"ALPHA"}},
		{MatchRegex, SearchCaseSensitive, "^alpha", []string{"alphabet"}},
		{MatchRegex, SearchCaseSmart, "a(lph|mm)a", []string{"Alpha Beta", "alphabet", "gamma-ray", "ALPHA"}},
	} {
		m := NewMatcher(tc.mode, tc.caseMode, tc.query)
		var got []string
		for _, text := range texts {
			if m.Match(text) {
				got = append(got, text)
			}
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s %s %q: got %v, want %v", tc.mode, tc.caseMode, tc.query, got, tc.want)
		}
	}
}

func TestMatcherBlankQueryMatchesEverything(t *testing.T) {
	for _, mode := range MatchModes() {
		if m := NewMatcher(mode, SearchCaseSmart, "  "); !m.Match("anything") || m.Err() != nil {
			t.Errorf("%s: a blank query must match", mode)
		}
	}
}

// An invalid regex matches nothing and says why in one line.
func TestMatcherInvalidRegexSaysWhy(t *testing.T) {
	m := NewMatcher(MatchRegex, SearchCaseSmart, "(abc")
	if m.Match("abc") || m.Match("(abc") {
		t.Fatal("an invalid pattern must match nothing")
	}
	if m.Err() == nil || m.Err().Error() != "regex: missing closing )" {
		t.Fatalf("err %v", m.Err())
	}
	if m := NewMatcher(MatchSubstring, SearchCaseSmart, "(abc"); m.Err() != nil || !m.Match("x(abc") {
		t.Fatal("substring takes ( literally")
	}
}

func TestPatternHasUpperSkipsEscapes(t *testing.T) {
	for pattern, want := range map[string]bool{
		`abc`: false, `aBc`: true, `\S+\W\B`: false, `\p{Greek}`: false, `\PL`: false,
		`\p{Lu}x`: false, `\p{Lu}X`: true, `É`: true, `\\A`: true,
	} {
		if got := patternHasUpper(pattern); got != want {
			t.Errorf("%q: %v, want %v", pattern, got, want)
		}
	}
}

func TestNextMatchCyclesBothWays(t *testing.T) {
	if got := []string{NextMatch(MatchSubstring, 1), NextMatch(MatchFuzzy, 1), NextMatch(MatchRegex, 1)}; !slices.Equal(got, []string{MatchFuzzy, MatchRegex, MatchSubstring}) {
		t.Fatalf("forward %v", got)
	}
	if NextMatch(MatchSubstring, -1) != MatchRegex || NextMatch("bogus", 1) != MatchFuzzy {
		t.Fatal("back / unknown")
	}
}

// The fuzzy matcher scores as the path picker always has (one fuzzy rule).
func TestFuzzyMatcherScoresAsThePicker(t *testing.T) {
	m := NewMatcher(MatchFuzzy, SearchCaseSmart, "smg")
	a, okA := m.ScorePrepared(m.Prepare("src/main.go"))
	b, okB := fuzzyScore("src/main.go", []rune("smg"), CaseFor(SearchCaseSmart, "smg"))
	if a != b || !okA || !okB {
		t.Fatalf("matcher %d %v, fuzzyScore %d %v", a, okA, b, okB)
	}
}

// The defaults leave today's behaviour: the list filter substring,
// choosers fuzzy, the tag shown.
func TestMatchSettingsDefaults(t *testing.T) {
	r := NewRuntime()
	r.EnsureConfigSettings()
	if r.SearchMatch() != MatchSubstring || r.ChooserMatch() != MatchFuzzy || !r.ShowMatch() {
		t.Fatalf("search.match %q chooser.match %q show %v", r.SearchMatch(), r.ChooserMatch(), r.ShowMatch())
	}
}

// tab in the filter line cycles substring → fuzzy → regex; each narrows
// the rows its way; the setting is not written.
func TestListFilterTabCyclesTheMatchMode(t *testing.T) {
	r, m, list := filterFixture(t)
	r.EnsureConfigSettings()
	pressKeys(m, "/", "a", "p")
	if list.MatchMode() != MatchSubstring || len(shownIDs(list)) != 0 {
		t.Fatalf("substring ap: %s %v", list.MatchMode(), shownIDs(list))
	}
	if !strings.Contains(m.View().Content, "substr") {
		t.Fatalf("the filter line shows the mode:\n%s", m.View().Content)
	}
	pressKeys(m, "tab")
	if list.MatchMode() != MatchFuzzy || !reflect.DeepEqual(shownIDs(list), []string{"r1", "r4"}) {
		t.Fatalf("fuzzy ap: %s %v", list.MatchMode(), shownIDs(list))
	}
	pressKeys(m, "tab", "ctrl+u", "^", "a")
	if list.MatchMode() != MatchRegex || !reflect.DeepEqual(shownIDs(list), []string{"r1", "r4"}) {
		t.Fatalf("regex ^a: %s %v", list.MatchMode(), shownIDs(list))
	}
	pressKeys(m, "ctrl+u", "i", "d", "l", "e", "$")
	if !reflect.DeepEqual(shownIDs(list), []string{"r2", "r5"}) {
		t.Fatalf("regex idle$ (cells are lines): %v", shownIDs(list))
	}
	pressKeys(m, "tab")
	if list.MatchMode() != MatchSubstring || r.SearchMatch() != MatchSubstring {
		t.Fatalf("round again: %s, setting %s", list.MatchMode(), r.SearchMatch())
	}
	pressKeys(m, "shift+tab")
	if list.MatchMode() != MatchRegex {
		t.Fatalf("shift+tab goes back: %s", list.MatchMode())
	}
}

func TestListFilterInvalidRegexSaysWhyOnTheLine(t *testing.T) {
	r, m, list := filterFixture(t)
	r.EnsureConfigSettings()
	if err := r.Settings().Set(SearchMatchSetting, MatchRegex); err != nil {
		t.Fatal(err)
	}
	pressKeys(m, "/", "(", "a")
	if r.Mode.Name != ListFilterMode || list.Shown() != 0 {
		t.Fatalf("mode %s shown %v", r.Mode.Name, shownIDs(list))
	}
	if line := list.filterLine(60); !strings.Contains(line, "regex: missing closing )") || !strings.Contains(line, "regex") {
		t.Fatalf("line %q", line)
	}
	if len(r.modals) != 0 {
		t.Fatal("an invalid pattern never opens a modal")
	}
	pressKeys(m, ")")
	if !reflect.DeepEqual(shownIDs(list), []string{"r1", "r2", "r3", "r4", "r5"}) {
		t.Fatalf("(a) matches the a's: %v", shownIDs(list))
	}
}

func TestShowMatchOffHidesTheTag(t *testing.T) {
	r, m, list := filterFixture(t)
	r.EnsureConfigSettings()
	if err := r.Settings().Set(SearchShowMatchSetting, false); err != nil {
		t.Fatal(err)
	}
	pressKeys(m, "/", "a")
	if line := list.filterLine(60); strings.Contains(line, "substr") {
		t.Fatalf("line %q", line)
	}
}

// The verbs refuse where no search is open, so tab elsewhere is not theirs.
func TestSearchMatchVerbsNeedAnOpenSearch(t *testing.T) {
	r, _, _ := filterFixture(t)
	if _, err := r.Execute(Request{Verb: "cycle-match", Noun: "ochazuke.search"}); err == nil {
		t.Fatal("cycle-match with no open search must fail")
	}
	if _, err := r.Execute(Request{Verb: "set-match", Noun: "ochazuke.search", Args: Arguments{"mode": "glob"}}); err == nil {
		t.Fatal("an unknown mode must fail")
	}
}

// tab in a chooser cycles its match mode; ctrl+o is next-source.
func TestChooserTabCyclesTheMatchMode(t *testing.T) {
	r, m, c, _ := chooserTest(t, ChooserSpec{Rows: []ChooserRow{
		{ID: "a", Label: "alpha-beta"},
		{ID: "b", Label: "bravo"},
	}})
	r.EnsureConfigSettings()
	keytest.Type(m, "ab")
	if c.MatchMode() != MatchFuzzy || !slices.Equal(chooserIDs(c), []string{"a"}) {
		t.Fatalf("fuzzy ab: %s %v", c.MatchMode(), chooserIDs(c))
	}
	pressLand(m, "tab")
	if c.MatchMode() != MatchRegex || len(chooserIDs(c)) != 0 {
		t.Fatalf("regex ab: %s %v", c.MatchMode(), chooserIDs(c))
	}
	pressLand(m, "tab")
	if c.MatchMode() != MatchSubstring || len(chooserIDs(c)) != 0 || r.ChooserMatch() != MatchFuzzy {
		t.Fatalf("substring ab: %s %v", c.MatchMode(), chooserIDs(c))
	}
	pressLand(m, "backspace")
	if !slices.Equal(chooserIDs(c), []string{"a", "b"}) {
		t.Fatalf("substring a: %v", chooserIDs(c))
	}
	if !strings.Contains(screen(m), "substr") {
		t.Fatalf("the search line shows the mode:\n%s", screen(m))
	}
}

// The path picker ranks fuzzy by default; substring and regex pick the
// entries their way, still best score first.
func TestPickerSearchInEveryMode(t *testing.T) {
	entries := []PickerEntry{{Path: "src/main.go"}, {Path: "src/maps.go"}, {Path: "docs/readme.md"}}
	for _, tc := range []struct {
		mode, query string
		want        []string
	}{
		{MatchFuzzy, "smg", []string{"src/main.go", "src/maps.go"}},
		{MatchSubstring, "ma go", []string{"src/main.go", "src/maps.go"}},
		{MatchSubstring, "smg", nil},
		{MatchRegex, `\.md$`, []string{"docs/readme.md"}},
		{MatchRegex, `(`, nil},
	} {
		got, err := searchPickerMatch(t.Context(), entries, NewMatcher(tc.mode, SearchCaseSmart, tc.query), "any")
		if err != nil {
			t.Fatal(err)
		}
		if paths := pickerEntryPaths(got); !slices.Equal(paths, tc.want) {
			t.Errorf("%s %q: %v, want %v", tc.mode, tc.query, paths, tc.want)
		}
	}
}

// ctrl+t in the path picker cycles its mode; each open starts in
// chooser.match again.
func TestPickerCtrlTCyclesTheMatchMode(t *testing.T) {
	r, p, _ := pickerFixture(t, "", "path", "any", 0)
	r.EnsureConfigSettings()
	pickerDo(t, r, "open", "ochazuke.picker", nil)
	if p.MatchMode() != MatchFuzzy {
		t.Fatalf("opens in %s", p.MatchMode())
	}
	pickerKey(t, r, "ctrl+t")
	if p.MatchMode() != MatchRegex || !strings.Contains(p.View(80, 10), "regex") {
		t.Fatalf("after ctrl+t: %s\n%s", p.MatchMode(), p.View(80, 10))
	}
	pickerDo(t, r, "close", "ochazuke.picker", nil)
	if err := r.SetFocus("origin"); err != nil {
		t.Fatal(err)
	}
	pickerDo(t, r, "open", "ochazuke.picker", nil)
	if p.MatchMode() != MatchFuzzy {
		t.Fatalf("reopens in %s", p.MatchMode())
	}
}
