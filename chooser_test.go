package ochazuke

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/keytest"
)

// chooserTest opens a chooser over a bare runtime and model; chosen and
// created record the callbacks.
func chooserTest(t *testing.T, spec ChooserSpec) (*Runtime, *Model, *Chooser, *[]string) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	m.Appearance.Theme = "mono"
	var calls []string
	spec.Kind = "test-chooser"
	spec.Choose = func(row ChooserRow) Result { calls = append(calls, "choose "+row.ID); return Done(nil) }
	c, err := r.OpenChooser(spec)
	if err != nil {
		t.Fatal(err)
	}
	return r, m, c, &calls
}

func chooserIDs(c *Chooser) []string {
	var ids []string
	for _, row := range c.Matches() {
		ids = append(ids, row.ID)
	}
	return ids
}

func highlighted(c *Chooser) string {
	row, _ := c.Highlighted()
	return row.ID
}

func TestChooserOpensInInsertWithTheTopMatchHighlighted(t *testing.T) {
	r, m, c, _ := chooserTest(t, ChooserSpec{Title: "Things", Rows: []ChooserRow{{ID: "a", Label: "alpha"}, {ID: "b", Label: "bravo"}}})
	if r.TopModal() != "test-chooser" || r.Focus != "test-chooser/query" || r.Mode.Name != "insert" {
		t.Fatalf("modal %q focus %q mode %q", r.TopModal(), r.Focus, r.Mode.Name)
	}
	if r.Chooser("test-chooser") != c || highlighted(c) != "a" {
		t.Fatalf("lookup or highlight: %q", highlighted(c))
	}
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Things") || !strings.Contains(frame, "alpha") {
		t.Fatalf("chooser frame:\n%s", frame)
	}
}

func TestChooserRanksSubstringMatchesFirstThenRowsOrder(t *testing.T) {
	_, m, c, _ := chooserTest(t, ChooserSpec{Rows: []ChooserRow{
		{ID: "scattered", Label: "a-b-c-x"},
		{ID: "other", Label: "zzz"},
		{ID: "whole", Label: "abc"},
		{ID: "late", Label: "xabcx"},
	}})
	keytest.Type(m, "abc")
	if got := chooserIDs(c); !slices.Equal(got, []string{"whole", "late", "scattered"}) {
		t.Fatalf("matches %v", got)
	}
	if highlighted(c) != "whole" {
		t.Fatalf("highlight %q", highlighted(c))
	}
}

func TestChooserSpelledTrailingRowLeadsUnlessAnOrdinaryRowIsSpelled(t *testing.T) {
	rows := []ChooserRow{{ID: "notes", Label: "notes"}, {ID: "nopin", Label: "no pin (unpin)", Trailing: true}}
	strict := func(q string, row ChooserRow) bool {
		if row.Trailing {
			return strings.Contains(row.Label, q) || spellsWords(CaseFor(SearchCaseIgnore, q), q, row.Label)
		}
		return fuzzyMatch(CaseFor(SearchCaseIgnore, q), q, row.Label)
	}
	_, m, c, _ := chooserTest(t, ChooserSpec{Rows: rows, Filter: strict})
	keytest.Type(m, "unp")
	if highlighted(c) != "nopin" {
		t.Fatalf("unp: matches %v highlight %q", chooserIDs(c), highlighted(c))
	}
	_, m, c, _ = chooserTest(t, ChooserSpec{Rows: rows, Filter: strict})
	keytest.Type(m, "no")
	if got := chooserIDs(c); !slices.Equal(got, []string{"notes", "nopin"}) || highlighted(c) != "notes" {
		t.Fatalf("no: matches %v highlight %q", got, highlighted(c))
	}
}

func TestChooserFixedRowsStayAtTheBottomAndAnswerOnlyWhenSpelledOrMoved(t *testing.T) {
	_, m, c, calls := chooserTest(t, ChooserSpec{Rows: []ChooserRow{
		{ID: "none", Label: "no pin", Fixed: true},
		{ID: "a", Label: "alpha"},
		{ID: "b", Label: "bravo"},
	}})
	if got := chooserIDs(c); !slices.Equal(got, []string{"a", "b", "none"}) {
		t.Fatalf("empty query %v", got)
	}
	keytest.Type(m, "zz")
	if got := chooserIDs(c); !slices.Equal(got, []string{"none"}) || highlighted(c) != "" {
		t.Fatalf("no match: %v highlight %q", got, highlighted(c))
	}
	keytest.Press(m, "ctrl+p")
	if highlighted(c) != "" {
		t.Fatalf("ctrl+p from none moved to %q", highlighted(c))
	}
	keytest.Press(m, "ctrl+n")
	if highlighted(c) != "none" {
		t.Fatalf("ctrl+n from none: %q", highlighted(c))
	}
	keytest.Press(m, "enter")
	if !slices.Equal(*calls, []string{"choose none"}) {
		t.Fatalf("calls %v", *calls)
	}
	_, m, c, _ = chooserTest(t, ChooserSpec{Rows: []ChooserRow{{ID: "none", Label: "no pin", Fixed: true}, {ID: "a", Label: "alpha"}}})
	keytest.Type(m, "no p")
	if highlighted(c) != "none" {
		t.Fatalf("spelled fixed row: %v highlight %q", chooserIDs(c), highlighted(c))
	}
}

func TestChooserCreateRowTakesAScatteredQuery(t *testing.T) {
	var created []string
	r := NewRuntime()
	m := NewModel(r)
	c, err := r.OpenChooser(ChooserSpec{
		Kind:        "projects",
		Rows:        []ChooserRow{{ID: "t", Label: "tuios-inbox  ~/proj/tuios-inbox"}, {ID: "o", Label: "ochazuke"}},
		Choose:      func(row ChooserRow) Result { return Done(row.ID) },
		Create:      func(q string) Result { created = append(created, q); return Done(nil) },
		CreateLabel: func(q string) string { return "new project " + q },
		CreateRow:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	keytest.Type(m, "chz")
	if highlighted(c) != "o" {
		t.Fatalf("compact match: %v highlight %q", chooserIDs(c), highlighted(c))
	}
	keytest.Press(m, "ctrl+u")
	keytest.Type(m, "unpin")
	if got := chooserIDs(c); !slices.Equal(got, []string{"t", chooserCreateRow}) || highlighted(c) != chooserCreateRow {
		t.Fatalf("scattered: %v highlight %q", got, highlighted(c))
	}
	keytest.Press(m, "enter")
	if !slices.Equal(created, []string{"unpin"}) || r.TopModal() != "" {
		t.Fatalf("created %v modal %q", created, r.TopModal())
	}
}

func TestChooserMoveClampsAndTypingResetsTheHighlight(t *testing.T) {
	_, m, c, _ := chooserTest(t, ChooserSpec{Rows: []ChooserRow{{ID: "a", Label: "alpha"}, {ID: "b", Label: "bravo"}, {ID: "c", Label: "charlie"}}})
	for range 5 {
		keytest.Press(m, "ctrl+n")
	}
	if highlighted(c) != "c" || !c.Moved() {
		t.Fatalf("down clamps: %q", highlighted(c))
	}
	for range 5 {
		keytest.Press(m, "ctrl+p")
	}
	if highlighted(c) != "a" {
		t.Fatalf("up clamps: %q", highlighted(c))
	}
	keytest.Press(m, "ctrl+n")
	keytest.Type(m, "a")
	if c.Moved() || highlighted(c) != "a" {
		t.Fatalf("typing keeps a moved highlight: %q moved=%t", highlighted(c), c.Moved())
	}
}

func TestChooserSetRowsKeepsAMovedHighlight(t *testing.T) {
	_, m, c, _ := chooserTest(t, ChooserSpec{Empty: "loading…"})
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "loading…") {
		t.Fatalf("empty line missing:\n%s", frame)
	}
	c.SetRows([]ChooserRow{{ID: "a", Label: "alpha"}, {ID: "b", Label: "bravo"}})
	keytest.Press(m, "ctrl+n")
	c.SetRows([]ChooserRow{{ID: "z", Label: "zulu"}, {ID: "a", Label: "alpha"}, {ID: "b", Label: "bravo"}})
	if highlighted(c) != "b" {
		t.Fatalf("highlight after reload %q", highlighted(c))
	}
}

func TestChooserEscClosesAndRestoresFocusAndMode(t *testing.T) {
	r := NewRuntime()
	pageTestRegister(t, r, "one")
	m := NewModel(r)
	if err := r.OpenPage("one"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenChooser(ChooserSpec{Kind: "k", Rows: []ChooserRow{{ID: "a", Label: "alpha"}}}); err != nil {
		t.Fatal(err)
	}
	keytest.Type(m, "al")
	keytest.Press(m, "esc")
	if r.TopModal() != "" || r.Focus != "one" || r.Mode.Name != "normal" {
		t.Fatalf("after esc: modal %q focus %q mode %q", r.TopModal(), r.Focus, r.Mode.Name)
	}
}

func TestChooserFooterListsNoMotions(t *testing.T) {
	_, m, _, _ := chooserTest(t, ChooserSpec{Rows: []ChooserRow{{ID: "a", Label: "alpha"}}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	frame := ansi.Strip(m.View().Content)
	if strings.Contains(frame, "ochazuke.chooser.move") || !strings.Contains(frame, "enter chooser.pick") || !strings.Contains(frame, "esc chooser.cancel") {
		t.Fatalf("chooser hints:\n%s", frame)
	}
}

func TestChooserBindingsAreAtomic(t *testing.T) {
	r := NewRuntime()
	if warnings, err := r.ValidateBindings(); err != nil || len(warnings) > 0 {
		t.Fatal(err, warnings)
	}
}

// pagePickerTest registers n pages p01..pNN titled Page NN.
func pagePickerTest(t *testing.T, n int) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	for i := range n {
		pageTestRegister(t, r, fmt.Sprintf("p%02d", i+1)).Title = fmt.Sprintf("Page %02d", i+1)
	}
	m := NewModel(r)
	m.Appearance.Theme = "mono"
	if err := r.OpenPage("p01"); err != nil {
		t.Fatal(err)
	}
	return r, m
}

func TestPagePickerListsEveryPageAndOpensOne(t *testing.T) {
	r, m := pagePickerTest(t, 50)
	keytest.Press(m, "'")
	c := r.Chooser(pagePickerID)
	if c == nil || r.Mode.Name != "insert" {
		t.Fatalf("' did not open the page picker: modal %q mode %q", r.TopModal(), r.Mode.Name)
	}
	ids := chooserIDs(c)
	if len(ids) != 50 || ids[0] != "p02" || ids[48] != "p50" || ids[49] != "p01" {
		t.Fatalf("rows %v", ids)
	}
	keytest.Type(m, "37")
	if got := chooserIDs(c); !slices.Equal(got, []string{"p37"}) {
		t.Fatalf("filter 37: %v", got)
	}
	keytest.Press(m, "enter")
	if r.Page != "p37" || r.TopModal() != "" || r.Mode.Name != "normal" {
		t.Fatalf("after enter: page %q modal %q mode %q", r.Page, r.TopModal(), r.Mode.Name)
	}
}

// The page picker lists the pages visited before, most recent first, then
// the never-visited ones in registration order, then the current page,
// marked; enter on the first row goes back to the page last left.
func TestPagePickerListsRecentPagesFirstAndTheCurrentPageLast(t *testing.T) {
	r, m := pagePickerTest(t, 6)
	for _, id := range []string{"p03", "p05", "p04"} {
		if err := r.OpenPage(id); err != nil {
			t.Fatal(err)
		}
	}
	keytest.Press(m, "'")
	c := r.Chooser(pagePickerID)
	if got, want := chooserIDs(c), []string{"p05", "p03", "p01", "p02", "p06", "p04"}; !slices.Equal(got, want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Page 04  current") || strings.Contains(view, "Page 05  current") {
		t.Fatalf("the current page is not the one marked:\n%s", view)
	}
	keytest.Press(m, "enter")
	if r.Page != "p05" {
		t.Fatalf("enter on the first row opened %q, want the page last left, p05", r.Page)
	}
	keytest.Press(m, "'")
	if got, want := chooserIDs(r.Chooser(pagePickerID)), []string{"p04", "p03", "p01", "p02", "p06", "p05"}; !slices.Equal(got, want) {
		t.Fatalf("after going back: rows %v, want %v", got, want)
	}
}

func TestPagePickerCtrlNMovesAndEscKeepsThePage(t *testing.T) {
	r, m := pagePickerTest(t, 5)
	keytest.Press(m, "'")
	keytest.Press(m, "ctrl+n")
	keytest.Press(m, "ctrl+n")
	if row, _ := r.Chooser(pagePickerID).Highlighted(); row.ID != "p04" {
		t.Fatalf("highlight %q", row.ID)
	}
	keytest.Press(m, "esc")
	if r.Page != "p01" || r.TopModal() != "" {
		t.Fatalf("esc: page %q modal %q", r.Page, r.TopModal())
	}
	keytest.Press(m, "'")
	keytest.Press(m, "ctrl+n")
	keytest.Press(m, "enter")
	if r.Page != "p03" {
		t.Fatalf("enter on the moved highlight opened %q", r.Page)
	}
}

func TestPagePickRunsFromTheCLIAndThePalette(t *testing.T) {
	r, m := pagePickerTest(t, 3)
	if _, err := r.Execute(Request{Noun: "ochazuke.page", Verb: "pick"}); err != nil {
		t.Fatal(err)
	}
	if r.Chooser(pagePickerID) == nil {
		t.Fatal("page.pick opened no chooser")
	}
	keytest.Press(m, "esc")
	keytest.Press(m, ":")
	keytest.Type(m, "page pick")
	keytest.Press(m, "enter")
	if r.TopModal() != pagePickerID {
		t.Fatalf("palette page pick: modal %q status %q", r.TopModal(), m.Status)
	}
}

func TestPagePickerKeyAppearsInHelpAndBindings(t *testing.T) {
	r, _ := pagePickerTest(t, 2)
	found := false
	for _, row := range r.ListBindings(r.Focus) {
		found = found || row.Keys == "'" && row.Request.Noun == "ochazuke.page" && row.Request.Verb == "pick"
	}
	if !found {
		t.Fatalf("bindings.list lacks ' page.pick: %v", r.ListBindings(r.Focus))
	}
	if help := fmt.Sprint(BindingHelp(r, r.Focus)); !strings.Contains(help, "page.pick") {
		t.Fatalf("? help lacks page.pick: %s", help)
	}
}

// A job a chooser's Choose launches outlives the chooser: picking closes
// the modal, input and all, before Choose runs, so the job belongs to the
// focus the chooser opened over and its completion lands there.
func TestChooserJobBelongsToTheOpener(t *testing.T) {
	r := NewRuntime()
	r.Define("row")
	if _, err := r.AddPage("page", "Page", func(*Runtime, int, int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddList("rows", []ListItem{{ID: "r1", Label: "one", Noun: "row"}}, Parent("page")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("r1"); err != nil {
		t.Fatal(err)
	}
	var landed []string
	if _, err := r.OpenChooser(ChooserSpec{Kind: "launch", Rows: []ChooserRow{{ID: "claude", Label: "claude"}}, Choose: func(row ChooserRow) Result {
		return Pending(func(context.Context) (any, error) { return row.ID, nil }).Label(row.ID).Then(func(c Ctx, v any, err error) {
			landed = append(landed, fmt.Sprint(v, err))
		})
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.chooser", Verb: "pick"}); err != nil {
		t.Fatal(err)
	}
	jobs := r.Jobs()
	if len(jobs) != 1 || jobs[0].Owner != "r1" || jobs[0].State != JobRunning || jobs[0].Label != "claude" {
		t.Fatalf("jobs = %+v, want one running job owned by r1", jobs)
	}
	for _, msg := range runCmd(r.PendingCommands()) {
		r.Deliver(msg)
	}
	if !slices.Equal(landed, []string{"claude<nil>"}) || r.Jobs()[0].State != JobDone {
		t.Fatalf("completion: landed %q, jobs %+v", landed, r.Jobs())
	}
}

// Keep (mtm UX review 2 F22): the directory chooser cuts the branch and
// projects columns before the path; without a kept column the widest goes
// first.
func TestFitWidthsCutsKeptColumnsLast(t *testing.T) {
	widths := []int{10, 24, 18, 8, 16} // name, path, branch, used, projects
	got := fitWidths(widths, 70, 1)
	if got[1] != 24 {
		t.Fatalf("kept path cut: %v", got)
	}
	n := 2 * (len(got) - 1)
	for _, w := range got {
		n += w
	}
	if n > 70 {
		t.Fatalf("does not fit: %v (%d)", got, n)
	}
	if plain := fitWidths(widths, 70); plain[1] >= 24 {
		t.Fatalf("without keep the widest (path) narrows first: %v", plain)
	}
	// Once the others are at their minimum, the kept column narrows too.
	if tight := fitWidths(widths, 50, 1); tight[1] >= 24 || tight[0] != 6 || tight[2] != 6 {
		t.Fatalf("tight: %v", tight)
	}
}

func TestChooserKeepDrawsTheKeptColumnWhole(t *testing.T) {
	rows := []ChooserRow{
		{ID: "m", Label: "matomeru", Cells: []string{"matomeru", "~/proj/matomeru", "feat/a-very-long-branch-name", "2d"}},
		{ID: "o", Label: "ochazuke", Cells: []string{"ochazuke", "~/proj/ochazuke", "main", "1h"}},
	}
	_, _, keep, _ := chooserTest(t, ChooserSpec{Rows: rows, Keep: []int{1}, Filter: func(string, ChooserRow) bool { return true }})
	if v := ansi.Strip(keep.View(50, 6).Text); !strings.Contains(v, "~/proj/matomeru") || strings.Contains(v, "a-very-long-branch-name") {
		t.Fatalf("keep:\n%s", v)
	}
	_, _, plain, _ := chooserTest(t, ChooserSpec{Rows: rows, Filter: func(string, ChooserRow) bool { return true }})
	if v := ansi.Strip(plain.View(44, 6).Text); strings.Contains(v, "~/proj/matomeru") {
		t.Fatalf("without keep the path is cut too:\n%s", v)
	}
}

// A chooser narrower than ChooserPreviewMin draws no preview (mtm's
// context picker test).
func TestChooserPreviewNeedsTheWidth(t *testing.T) {
	_, _, c, _ := chooserTest(t, ChooserSpec{Rows: []ChooserRow{{ID: "a", Label: "a"}}, Preview: func(ChooserRow) string { return "preview text" }, Filter: func(string, ChooserRow) bool { return true }})
	if v := c.View(ChooserPreviewMin-1, 6).Text; strings.Contains(v, "preview text") {
		t.Fatalf("narrow:\n%s", v)
	}
	if v := c.View(ChooserPreviewMin, 6).Text; !strings.Contains(v, "│ preview text") {
		t.Fatalf("wide:\n%s", v)
	}
}

// The preview follows the highlight, keeps its column's width whatever it
// holds, wraps, and marks a cut at the bottom with ….
func TestChooserPreviewFollowsTheHighlightAtAFixedWidth(t *testing.T) {
	previews := map[string]string{
		"a": "first answer",
		"b": strings.Repeat("word ", 40),
		"c": "",
	}
	_, m, c, _ := chooserTest(t, ChooserSpec{
		Rows:    []ChooserRow{{ID: "a", Label: "alpha"}, {ID: "b", Label: "bravo"}, {ID: "c", Label: "charlie"}},
		Preview: func(row ChooserRow) string { return previews[row.ID] },
	})
	widths := func(v string) []int {
		var w []int
		for _, line := range strings.Split(ansi.Strip(v), "\n") {
			w = append(w, ansi.StringWidth(line))
		}
		return w
	}
	first := c.View(80, 6).Text
	if !strings.Contains(first, "│ first answer") {
		t.Fatalf("top match's preview:\n%s", first)
	}
	keytest.Press(m, "ctrl+n")
	second := c.View(80, 6).Text
	if strings.Contains(second, "first answer") || !strings.Contains(second, "│ word word") || !strings.Contains(second, "│ …") {
		t.Fatalf("moved preview, wrapped and cut:\n%s", second)
	}
	keytest.Press(m, "ctrl+n")
	third := c.View(80, 6).Text
	if strings.Contains(third, "word") {
		t.Fatalf("an empty preview draws nothing:\n%s", third)
	}
	for _, v := range []string{first, second, third} {
		for _, w := range widths(v) {
			if w != 80 {
				t.Fatalf("every line is the full width (%v):\n%s", widths(v), v)
			}
		}
	}
}

func TestChooserFillsTheModalWidthWhileTyping(t *testing.T) {
	_, m, c, _ := chooserTest(t, ChooserSpec{Title: "Things", Rows: []ChooserRow{{ID: "a", Label: "alpha"}, {ID: "b", Label: "bravo"}}})
	m.Width, m.Height = 160, 20
	contentWidth, _ := ModalSize(m.Width, m.Height)
	panel := func() int {
		widest := 0
		for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			if i, j := strings.Index(line, "╭"), strings.LastIndex(line, "╮"); i >= 0 && j > i {
				widest = max(widest, ansi.StringWidth(line[i:j])+1)
			}
		}
		return widest
	}
	before := panel()
	if before != contentWidth+4 {
		t.Fatalf("panel %d cells, want the modal's %d", before, contentWidth+4)
	}
	keytest.Type(m, "br")
	if after := panel(); after != before {
		t.Fatalf("panel resized from %d to %d while typing", before, after)
	}
	for _, line := range strings.Split(c.View(contentWidth, 10).Text, "\n") {
		if w := ansi.StringWidth(line); w != contentWidth {
			t.Fatalf("line %q is %d cells, want %d", ansi.Strip(line), w, contentWidth)
		}
	}
}
