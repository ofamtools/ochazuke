package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Spans gives each match's rune offsets in every mode, case as search.case
// says.
func TestMatcherSpans(t *testing.T) {
	for _, tc := range []struct {
		mode, caseMode, query, text string
		want                        [][2]int
	}{
		{MatchSubstring, SearchCaseSmart, "foo", "a Foo foo", [][2]int{{2, 5}, {6, 9}}},
		{MatchSubstring, SearchCaseSmart, "Foo", "a Foo foo", [][2]int{{2, 5}}},
		{MatchSubstring, SearchCaseSensitive, "foo", "a Foo foo", [][2]int{{6, 9}}},
		{MatchSubstring, SearchCaseSmart, "b a", "a b", [][2]int{{0, 1}, {2, 3}}},
		{MatchSubstring, SearchCaseSmart, "b z", "a b", nil},
		{MatchSubstring, SearchCaseSmart, "é", "café é", [][2]int{{3, 4}, {5, 6}}},
		{MatchRegex, SearchCaseSmart, `o+`, "fOo boo", [][2]int{{1, 3}, {5, 7}}},
		{MatchRegex, SearchCaseSmart, `(`, "x", nil},
		{MatchFuzzy, SearchCaseSmart, "fb", "a foo bar", [][2]int{{2, 7}}},
		{MatchSubstring, SearchCaseSmart, "  ", "x", nil},
	} {
		got := NewMatcher(tc.mode, tc.caseMode, tc.query).Spans(tc.text)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s %s %q in %q: %v, want %v", tc.mode, tc.caseMode, tc.query, tc.text, got, tc.want)
		}
	}
}

// FindLine walks after the row, wrapping once to the row itself.
func TestFindLineWraps(t *testing.T) {
	match := func(rows ...int) func(int) bool { return func(i int) bool { return slices.Contains(rows, i) } }
	for _, tc := range []struct {
		n, from int
		back    bool
		rows    []int
		want    int
		ok      bool
	}{
		{5, 1, false, []int{1, 3}, 3, true},
		{5, 3, false, []int{1, 3}, 1, true},
		{5, 3, false, []int{3}, 3, true},
		{5, 3, true, []int{1, 3}, 1, true},
		{5, 1, true, []int{1, 3}, 3, true},
		{5, -1, false, []int{0}, 0, true},
		{5, 5, true, []int{4}, 4, true},
		{5, 2, false, nil, 0, false},
		{0, 0, false, []int{0}, 0, false},
	} {
		got, ok := FindLine(tc.n, tc.from, tc.back, match(tc.rows...))
		if got != tc.want || ok != tc.ok {
			t.Errorf("FindLine(%d, %d, %v) over %v = %d %v, want %d %v", tc.n, tc.from, tc.back, tc.rows, got, ok, tc.want, tc.ok)
		}
	}
}

// An input finds the next match's first character after the cursor,
// wrapping, and back finds the one before it.
func TestInputFindText(t *testing.T) {
	in := NewInput("x", "one foo\ntwo\nfoo three foo", true)
	m := NewMatcher(MatchSubstring, SearchCaseSmart, "foo")
	steps := []struct {
		back bool
		want int
	}{
		{false, 4}, {false, 12}, {false, 22}, {false, 4}, {true, 22}, {true, 12},
	}
	for i, s := range steps {
		if !in.FindText(m, s.back) || in.Cursor != s.want {
			t.Fatalf("step %d: cursor %d, want %d", i, in.Cursor, s.want)
		}
	}
	if in.FindText(NewMatcher(MatchSubstring, SearchCaseSmart, "absent"), false) || in.Cursor != 12 {
		t.Fatalf("a miss moved the cursor to %d", in.Cursor)
	}
}

// / in the text view opens the find line; enter jumps to the match, n and
// N repeat the find either way (a count works on the viewers whose keys
// are not an editor's); search.case decides case; an
// invalid regex keeps the line open and says why on it; esc closes it.
func TestTextViewSlashFinds(t *testing.T) {
	r, m := textResultFixture(t)
	m.Width, m.Height = 80, 24
	in, err := r.OpenTextView("demo", "alpha\nBeta one\nbeta two\nGamma\nbeta three")
	if err != nil {
		t.Fatal(err)
	}
	m.View()
	pressKeys(m, "/")
	if r.TopModal() != findLineID || r.Mode.Name != "insert" {
		t.Fatalf("/ opened %q in %q", r.TopModal(), r.Mode.Name)
	}
	pressKeys(m, "b", "e", "t", "a", "enter")
	if r.TopModal() != TextViewID || r.Focus != in.ID || in.Cursor != 6 {
		t.Fatalf("after enter: modal %q focus %q cursor %d", r.TopModal(), r.Focus, in.Cursor)
	}
	pressKeys(m, "n")
	if in.Cursor != 15 {
		t.Fatalf("n: cursor %d", in.Cursor)
	}
	pressKeys(m, "n", "n")
	if in.Cursor != 6 {
		t.Fatalf("n n wraps: cursor %d", in.Cursor)
	}
	pressKeys(m, "N")
	if in.Cursor != 30 {
		t.Fatalf("N: cursor %d", in.Cursor)
	}
	if q, back := r.LastFind(); q != "beta" || back {
		t.Fatalf("last find %q back %v: N must keep n's direction", q, back)
	}
	// Smart case: a capital matches exactly.
	pressKeys(m, "/", "B", "enter")
	if in.Cursor != 6 {
		t.Fatalf("/B: cursor %d", in.Cursor)
	}
	pressKeys(m, "n")
	if in.Cursor != 6 {
		t.Fatalf("/B n: cursor %d (the only capital B)", in.Cursor)
	}
	// An invalid regex says why on the line and keeps it.
	pressKeys(m, "/", "tab", "tab", "(", "enter")
	if r.TopModal() != findLineID || !strings.Contains(ansi.Strip(m.View().Content), "regex: missing closing )") {
		t.Fatalf("invalid regex: modal %q\n%s", r.TopModal(), ansi.Strip(m.View().Content))
	}
	pressKeys(m, "esc")
	if r.TopModal() != TextViewID || r.Focus != in.ID {
		t.Fatalf("esc: modal %q focus %q", r.TopModal(), r.Focus)
	}
}

// A find with nothing to search refuses, and n before any find is
// unavailable: its key stands aside.
func TestSearchFindNeedsATextFinder(t *testing.T) {
	r, _ := textResultFixture(t)
	if _, err := r.Execute(Request{Noun: "ochazuke.search", Verb: "find", Args: Arguments{"query": "x"}}); err == nil {
		t.Fatal("find on a record ran")
	}
	if _, err := r.OpenTextView("demo", "a\nb"); err != nil {
		t.Fatal(err)
	}
	if r.Available(Request{Noun: "ochazuke.search", Verb: "next"}, r.Focus, r.Mode) {
		t.Fatal("search.next available before any find")
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.search", Verb: "find", Args: Arguments{"query": "zzz"}}); err == nil || !strings.Contains(err.Error(), "pattern not found") {
		t.Fatalf("missing pattern: %v", err)
	}
}

// The last pattern, match mode and direction outlive a viewer; another
// runtime starts without them.
func TestSearchFindSurvivesViewCloseAndStaysRuntimeLocal(t *testing.T) {
	r, _ := textResultFixture(t)
	if query, back := r.LastFind(); query != "" || back {
		t.Fatalf("initial find: %q, back %v", query, back)
	}
	if _, err := r.OpenTextView("first", "zero\none\ntwo"); err != nil {
		t.Fatal(err)
	}
	if err := r.FindText(r.Focus, "^one$", MatchRegex, true, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.modal", Verb: "close"}); err != nil {
		t.Fatal(err)
	}
	in, err := r.OpenTextView("second", "zero\none\ntwo\none")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.search", Verb: "next"}); err != nil {
		t.Fatal(err)
	}
	if in.Cursor != 13 {
		t.Fatalf("repeat in the new viewer: cursor %d, want 13", in.Cursor)
	}
	if query, back := r.LastFind(); query != "^one$" || !back {
		t.Fatalf("last find: %q, back %v", query, back)
	}
	other, _ := textResultFixture(t)
	if _, err := other.OpenTextView("other", "one"); err != nil {
		t.Fatal(err)
	}
	if query, back := other.LastFind(); query != "" || back {
		t.Fatalf("another runtime inherited find: %q, back %v", query, back)
	}
	if other.Available(Request{Noun: "ochazuke.search", Verb: "next"}, other.Focus, other.Mode) {
		t.Fatal("another runtime can repeat a search it never ran")
	}
}

// Shared modal closing runs the find line's cancel verb, including its
// checks; callers can also run cancel directly.
func TestFindLineCloseRunsCancel(t *testing.T) {
	for _, req := range []Request{
		{Noun: "ochazuke.modal", Verb: "close"},
		{Noun: FindLineNoun, Verb: "cancel"},
	} {
		t.Run(req.Key(), func(t *testing.T) {
			r, _ := textResultFixture(t)
			in, err := r.OpenTextView("demo", "one\ntwo")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.Execute(Request{Noun: "ochazuke.search", Verb: "find"}); err != nil {
				t.Fatal(err)
			}
			cancelled := 0
			if err := r.AddVerbCheck("ochazuke.find-line.cancel", VerbCheck{
				Name: "observe-cancel", Check: func(*Runtime, Context, Arguments) Verdict {
					cancelled++
					return Allow()
				},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Execute(req); err != nil {
				t.Fatal(err)
			}
			if cancelled != 1 || r.TopModal() != TextViewID || r.Focus != in.ID || r.Mode.Name != "normal" {
				t.Fatalf("cancel checks %d, modal %q, focus %q, mode %q", cancelled, r.TopModal(), r.Focus, r.Mode.Name)
			}
		})
	}
}

// The docs viewer (help.markdown off) scrolls the next matching line to
// its top; n goes on from the line found.
func TestDocsViewerFinds(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 100, 20
	if _, err := r.OpenModal(docsViewerID, Payload{}); err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = "line"
	}
	lines[30], lines[45] = "needle one", "needle two"
	v := &docsViewer{r: r, topic: "t", text: strings.Join(lines, "\n"), found: -1}
	d, err := r.AddDetail(docsTextID, nil, Parent(docsViewerID))
	if err != nil {
		t.Fatal(err)
	}
	v.detail = d
	r.Instances[docsViewerID].Component = v
	m.View()
	if _, err := r.Execute(Request{Noun: "ochazuke.search", Verb: "find", Args: Arguments{"query": "needle"}}); err != nil {
		t.Fatal(err)
	}
	if d.Offset() != 30 {
		t.Fatalf("offset %d", d.Offset())
	}
	m.View()
	if _, err := r.Execute(Request{Noun: "ochazuke.search", Verb: "next"}); err != nil {
		t.Fatal(err)
	}
	if v.found != 45 {
		t.Fatalf("n found %d", v.found)
	}
}

// A timeline find selects the next card whose text matches; a card found
// by its body opens unless search.open-folds is off.
func TestTimelineFinds(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 80, 30
	tl, err := r.AddTimeline("tl", []Card{
		{ID: "a", Header: "first"},
		{ID: "b", Header: "second", Body: TextBody("the needle is here")},
		{ID: "c", Header: "needle in the header"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tl.Focus(); err != nil {
		t.Fatal(err)
	}
	m.View()
	find := func(args Arguments) {
		t.Helper()
		if _, err := r.Execute(Request{Noun: "ochazuke.search", Verb: "find", Args: args}); err != nil {
			t.Fatal(err)
		}
		m.View()
	}
	find(Arguments{"query": "needle"})
	if tl.SelectedID() != "b" || !tl.IsOpen("b") {
		t.Fatalf("selected %q open %v", tl.SelectedID(), tl.IsOpen("b"))
	}
	find(Arguments{"query": "needle"})
	if tl.SelectedID() != "c" || tl.IsOpen("c") {
		t.Fatalf("selected %q open %v", tl.SelectedID(), tl.IsOpen("c"))
	}
	_ = tl.SetOpen("b", false)
	if err := r.Settings().Set(SearchOpenFoldsSetting, false); err != nil {
		t.Fatal(err)
	}
	find(Arguments{"query": "needle", "back": true})
	if tl.SelectedID() != "b" || tl.IsOpen("b") {
		t.Fatalf("open-folds off: selected %q open %v", tl.SelectedID(), tl.IsOpen("b"))
	}
}
