package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
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
			return strings.Contains(row.Label, q) || spellsWords(q, row.Label)
		}
		return fuzzyMatch(q, row.Label)
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
	if strings.Contains(frame, "chooser.move") || !strings.Contains(frame, "enter chooser.pick") || !strings.Contains(frame, "esc chooser.cancel") {
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

func TestPagePickerListsEveryPageInRegistrationOrderAndOpensOne(t *testing.T) {
	r, m := pagePickerTest(t, 50)
	keytest.Press(m, "'")
	c := r.Chooser(pagePickerID)
	if c == nil || r.Mode.Name != "insert" {
		t.Fatalf("' did not open the page picker: modal %q mode %q", r.TopModal(), r.Mode.Name)
	}
	ids := chooserIDs(c)
	if len(ids) != 50 || ids[0] != "p01" || ids[49] != "p50" {
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

func TestPagePickerCtrlNMovesAndEscKeepsThePage(t *testing.T) {
	r, m := pagePickerTest(t, 5)
	keytest.Press(m, "'")
	keytest.Press(m, "ctrl+n")
	keytest.Press(m, "ctrl+n")
	if row, _ := r.Chooser(pagePickerID).Highlighted(); row.ID != "p03" {
		t.Fatalf("highlight %q", row.ID)
	}
	keytest.Press(m, "esc")
	if r.Page != "p01" || r.TopModal() != "" {
		t.Fatalf("esc: page %q modal %q", r.Page, r.TopModal())
	}
	keytest.Press(m, "'")
	keytest.Press(m, "ctrl+n")
	keytest.Press(m, "enter")
	if r.Page != "p02" {
		t.Fatalf("enter on the moved highlight opened %q", r.Page)
	}
}

func TestPagePickRunsFromTheCLIAndThePalette(t *testing.T) {
	r, m := pagePickerTest(t, 3)
	if _, err := r.Execute(Request{Noun: "page", Verb: "pick"}); err != nil {
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
		found = found || row.Keys == "'" && row.Request.Noun == "page" && row.Request.Verb == "pick"
	}
	if !found {
		t.Fatalf("bindings.list lacks ' page.pick: %v", r.ListBindings(r.Focus))
	}
	if help := fmt.Sprint(BindingHelp(r, r.Focus)); !strings.Contains(help, "page.pick") {
		t.Fatalf("? help lacks page.pick: %s", help)
	}
}
