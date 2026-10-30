package ochazuke

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCaseForModes(t *testing.T) {
	cases := []struct {
		mode, query string
		fold        bool
	}{
		{SearchCaseSmart, "readme", true},
		{SearchCaseSmart, "README", false},
		{SearchCaseSmart, "readMe", false},
		{SearchCaseSmart, "src/main.go", true},   // separators and dots are not capitals
		{SearchCaseSmart, "a-1_b ~/x", true},     // nor digits, dashes, spaces, ~
		{SearchCaseSmart, "", true},              // an empty query folds
		{SearchCaseSmart, "café", true},          // a lower-case accented letter folds
		{SearchCaseSmart, "Éclair", false},       // É is a capital (unicode.IsUpper)
		{SearchCaseSmart, "ΣΑΣ", false},          // and so is Greek
		{"", "README", false},                    // an unset or unknown mode reads as smart
		{"bogus", "readme", true},                //
		{SearchCaseIgnore, "README", true},       // ignore folds whatever is typed
		{SearchCaseIgnore, "readme", true},       //
		{SearchCaseSensitive, "readme", false},   // sensitive never folds
		{SearchCaseSensitive, "README", false},   //
		{SearchCaseSensitive, "src/main", false}, //
	}
	for _, tc := range cases {
		if got := CaseFor(tc.mode, tc.query).Fold(); got != tc.fold {
			t.Errorf("CaseFor(%q, %q).Fold() = %v, want %v", tc.mode, tc.query, got, tc.fold)
		}
	}
}

func TestCaseMatchCompares(t *testing.T) {
	smart := func(q string) CaseMatch { return CaseFor(SearchCaseSmart, q) }
	if !smart("readme").Contains("README.md", "readme") || !smart("readme").HasPrefix("README.md", "readme") {
		t.Fatal("smart, no capital: readme matches README.md")
	}
	if smart("README").Contains("readme.txt", "README") || smart("README").HasPrefix("readme.txt", "README") {
		t.Fatal("smart, a capital: README does not match readme.txt")
	}
	if !smart("README").Contains("README.md", "README") {
		t.Fatal("smart, a capital: README matches README.md")
	}
	if !smart("éc").Contains("Éclair", "éc") || smart("Éc").Contains("éclair", "Éc") {
		t.Fatal("smart folds É for a lower-case query only")
	}
	if !CaseFor(SearchCaseIgnore, "README").Contains("readme", "README") {
		t.Fatal("ignore: README matches readme")
	}
	if CaseFor(SearchCaseSensitive, "readme").Contains("README", "readme") {
		t.Fatal("sensitive: readme does not match README")
	}
	if got := smart("x").Rune('É'); got != 'é' {
		t.Fatalf("folded rune %q", got)
	}
	if got := smart("X").Rune('É'); got != 'É' {
		t.Fatalf("exact rune %q", got)
	}
}

func TestRuntimeSearchCaseSetting(t *testing.T) {
	var none *Runtime
	if none.SearchCase() != SearchCaseSmart || !none.CaseFor("abc").Fold() || none.CaseFor("Abc").Fold() {
		t.Fatal("a nil runtime is smart")
	}
	r := NewRuntime()
	if r.SearchCase() != SearchCaseSmart {
		t.Fatalf("default %q", r.SearchCase())
	}
	for _, v := range []string{SearchCaseIgnore, SearchCaseSensitive, SearchCaseSmart} {
		if err := r.Settings().Set(SearchCaseSetting, v); err != nil {
			t.Fatal(err)
		}
		if r.SearchCase() != v {
			t.Fatalf("set %q read %q", v, r.SearchCase())
		}
	}
	if err := r.Settings().Set(SearchCaseSetting, "loud"); err == nil {
		t.Fatal("a value outside the enum should be refused")
	}
}

// readmeEntries are a README.md and a readme.txt: distinct names on a
// case-insensitive filesystem too.
var readmeNames = []string{"README.md", "readme.txt", "notes.txt"}

func TestPickerSearchFollowsSearchCase(t *testing.T) {
	var entries []PickerEntry
	for _, name := range readmeNames {
		entries = append(entries, PickerEntry{Path: "docs/" + name})
	}
	search := func(query, mode string) []string {
		t.Helper()
		got, err := searchPicker(context.Background(), entries, query, "any", mode)
		if err != nil {
			t.Fatal(err)
		}
		return pickerEntryPaths(got)
	}
	both := []string{"docs/README.md", "docs/readme.txt"}
	if got := search("readme", SearchCaseSmart); !slices.Equal(got, both) {
		t.Fatalf("smart readme: %q", got)
	}
	if got := search("README", SearchCaseSmart); !slices.Equal(got, []string{"docs/README.md"}) {
		t.Fatalf("smart README: %q", got)
	}
	if got := search("README", SearchCaseIgnore); !slices.Equal(got, both) {
		t.Fatalf("ignore README: %q", got)
	}
	if got := search("readme", SearchCaseSensitive); !slices.Equal(got, []string{"docs/readme.txt"}) {
		t.Fatalf("sensitive readme: %q", got)
	}
}

// A lower-case query ranks the same under smart as under ignore: the
// scores are equal for every entry, so no ranking moves.
func TestPickerFuzzyScoreSmartLowercaseEqualsIgnore(t *testing.T) {
	paths := []string{"src/Main.go", "Source/My App.go", "cmd/o-story/README.md", "x/Y_z-W", "ÉCOLE/été.txt"}
	for _, query := range []string{"mg", "s/ma", "readme", "yzw", "été", "e"} {
		smart, ignore := CaseFor(SearchCaseSmart, query), CaseFor(SearchCaseIgnore, query)
		for _, path := range paths {
			s1, ok1 := pickerFuzzyScore(path, []rune(smart.String(query)), smart)
			s2, ok2 := pickerFuzzyScore(path, []rune(ignore.String(query)), ignore)
			if s1 != s2 || ok1 != ok2 {
				t.Errorf("%q in %q: smart %d,%v ignore %d,%v", query, path, s1, ok1, s2, ok2)
			}
		}
	}
}

func TestPathListDirFollowsSearchCase(t *testing.T) {
	dir := t.TempDir()
	for _, name := range readmeNames {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list := func(prefix, mode string) []string {
		t.Helper()
		rows, err := listDir(dir, prefix, PathPick{}, false, mode)
		if err != nil {
			t.Fatal(err)
		}
		var labels []string
		for _, row := range rows {
			labels = append(labels, row.Label)
		}
		return labels
	}
	both := []string{"README.md", "readme.txt"}
	if got := list("read", SearchCaseSmart); !slices.Equal(got, both) {
		t.Fatalf("smart read: %q", got)
	}
	if got := list("READ", SearchCaseSmart); !slices.Equal(got, []string{"README.md"}) {
		t.Fatalf("smart READ: %q", got)
	}
	if got := list("READ", SearchCaseIgnore); !slices.Equal(got, both) {
		t.Fatalf("ignore READ: %q", got)
	}
	if got := list("read", SearchCaseSensitive); !slices.Equal(got, []string{"readme.txt"}) {
		t.Fatalf("sensitive read: %q", got)
	}
	// A capital in the folder already typed does not make the name's
	// completion case-sensitive: only the name being completed counts.
	sub := filepath.Join(dir, "Docs")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range readmeNames {
		if err := os.WriteFile(filepath.Join(sub, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var labels []string
	for _, row := range typedRows("Docs/read", PathPick{Start: dir}, false, SearchCaseSmart) {
		labels = append(labels, row.Label)
	}
	if !slices.Equal(labels, []string{"Docs/README.md", "Docs/readme.txt"}) {
		t.Fatalf("typed Docs/read: %q", labels)
	}
}

func TestListFilterFollowsSearchCase(t *testing.T) {
	r, _, list := filterFixture(t) // r2 has a cell "idle", r5 "IDLE"
	shown := func() []string {
		var ids []string
		for _, item := range list.Items() {
			if list.shows(item.ID) {
				ids = append(ids, item.ID)
			}
		}
		return ids
	}
	for _, tc := range []struct {
		mode, query string
		want        []string
	}{
		{SearchCaseSmart, "idle", []string{"r2", "r5"}},
		{SearchCaseSmart, "IDLE", []string{"r5"}},
		{SearchCaseSmart, "Idle", nil},
		{SearchCaseIgnore, "IDLE", []string{"r2", "r5"}},
		{SearchCaseSensitive, "idle", []string{"r2"}},
		{SearchCaseSmart, "idle", []string{"r2", "r5"}}, // back to folded: the cached text is rebuilt
	} {
		if err := r.Settings().Set(SearchCaseSetting, tc.mode); err != nil {
			t.Fatal(err)
		}
		list.SetFilter(tc.query)
		if got := shown(); !slices.Equal(got, tc.want) {
			t.Errorf("%s %q: shown %v want %v", tc.mode, tc.query, got, tc.want)
		}
	}
}

func TestChooserFollowsSearchCase(t *testing.T) {
	rows := []ChooserRow{{ID: "upper", Label: "README.md"}, {ID: "lower", Label: "readme.txt"}, {ID: "other", Label: "notes"}}
	for _, tc := range []struct {
		mode, query string
		want        []string
	}{
		{SearchCaseSmart, "readme", []string{"upper", "lower"}},
		{SearchCaseSmart, "README", []string{"upper"}},
		{SearchCaseSmart, "RDM", []string{"upper"}}, // the fuzzy match: R, D, M in order only in README.md
		{SearchCaseIgnore, "README", []string{"upper", "lower"}},
		{SearchCaseSensitive, "readme", []string{"lower"}},
	} {
		r, _, c, _ := chooserTest(t, ChooserSpec{Rows: rows})
		if err := r.Settings().Set(SearchCaseSetting, tc.mode); err != nil {
			t.Fatal(err)
		}
		c.SetQuery(tc.query)
		if got := chooserIDs(c); !slices.Equal(got, tc.want) {
			t.Errorf("%s %q: matches %v want %v", tc.mode, tc.query, got, tc.want)
		}
	}
	// Word spelling and compact matches follow it too.
	smart := func(q string) CaseMatch { return CaseFor(SearchCaseSmart, q) }
	if !spellsWords(smart("no pin"), "no pin", "No Pin (unpin)") || spellsWords(smart("No Pin"), "No Pin", "no pin (unpin)") {
		t.Fatal("spellsWords: a lower-case query spells either case, a capital only its own")
	}
	if !compactMatch(smart("chz"), "chz", "OCHAZUKE") || compactMatch(smart("CHZ"), "CHZ", "ochazuke") {
		t.Fatal("compactMatch: a lower-case query matches either case, a capital only its own")
	}
}
