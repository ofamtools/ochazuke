package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// helpPageFixture is the walk fixture with a Model at 140x40.
func helpPageFixture(t *testing.T) (*Runtime, *Model) {
	t.Helper()
	r := helpWalkFixture(t, helpWalkRows)
	m := NewModel(r)
	m.Width, m.Height = 140, 40
	return r, m
}

// ? opens the binding help page over the focus: the rail draws the walk,
// the list the rows; j in the rail walks the levels and the list follows
// without narrowing; / filters through the help query and x a clears it;
// q goes back to the page it came from. help.style modal opens the modal.
func TestHelpPageOpensWalksAndFilters(t *testing.T) {
	r, m := helpPageFixture(t)
	keytest.Press(m, "?")
	if r.Page != HelpPageID || r.TopModal() != "" {
		t.Fatalf("? opened page %q modal %q (status %q)", r.Page, r.TopModal(), m.Status)
	}
	p := r.helpPage()
	if p.Walk.Place.Noun != "tile" || len(p.Walk.Levels) < 5 {
		t.Fatalf("walked from %+v with %d levels", p.Walk.Place, len(p.Walk.Levels))
	}
	frame := m.View().Content
	for _, want := range []string{"Binding help", "focus tile", "stack, focus first", "tile  ", "tile.cut", "status here", "answers if guard"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame lacks %q:\n%s", want, frame)
		}
	}
	list := r.List(helpRowsID)
	all := list.Shown()
	keytest.Press(m, "tab")
	keytest.Press(m, "j")
	keytest.Press(m, "j") // past the focus level's tables
	if !r.focusInside(helpRailID) || list.Shown() != all {
		t.Fatalf("rail focus %v, shown %d of %d", r.focusInside(helpRailID), list.Shown(), all)
	}
	keytest.Press(m, "]")
	if line, ok := p.selectedLine(); !ok || line.Level != p.RailLevel || p.RailLevel < 1 {
		t.Fatalf("] left the list on %+v, rail level %d", line, p.RailLevel)
	}
	tree := r.Tree(helpRailID)
	level := fmt.Sprintf("level/%d", p.RailLevel)
	keytest.Press(m, "l")
	if tree.Collapsed(level) {
		t.Fatalf("l left %s closed", level)
	}
	keytest.Press(m, "tab")
	keytest.Press(m, "/")
	keytest.Type(m, "key:x")
	keytest.Press(m, "enter")
	if list.Filter() != "key:x" || list.Shown() >= all || !strings.Contains(m.View().Content, "1 key:x") {
		t.Fatalf("filter %q shows %d of %d", list.Filter(), list.Shown(), all)
	}
	keytest.Press(m, "x")
	keytest.Press(m, "a")
	if list.Filter() != "" || list.Shown() != all {
		t.Fatalf("x a left %q, %d shown", list.Filter(), list.Shown())
	}
	keytest.Press(m, "q")
	if r.Page != "ctx" || r.PageOf(HelpPageID) != nil {
		t.Fatalf("q left page %q", r.Page)
	}
	if err := r.Settings().Set(HelpStyleSetting, HelpStyleModal); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "?")
	if r.TopModal() != helpID || r.Page != "ctx" {
		t.Fatalf("help.style modal: modal %q page %q", r.TopModal(), r.Page)
	}
}

// The page's own keys take no key another table on its walk answers: from
// a list line and from a rail node, every row of the page's file answers
// (or answers by its guard) and shadows no row of another table.
func TestHelpPageKeysTakeNoKeyOfTheWalk(t *testing.T) {
	r, m := helpPageFixture(t)
	keytest.Press(m, "?")
	own := map[string]bool{helpPageNoun: true, helpRowNoun: true, helpNodeNoun: true}
	check := func(focus string) {
		w := r.HelpWalkAt(focus, ModeState{Name: "normal"}, nil)
		for _, e := range w.Entries {
			mine := own[e.Row.Noun]
			if mine && !e.Status().Answering() {
				t.Errorf("from %s: %s %s is %s", focus, rowKey(e.Row), e.Row.Request.Key(), e.Status())
			}
			if !mine && e.By >= 0 && own[w.Entries[e.By].Row.Noun] {
				t.Errorf("from %s: the page's %s takes %s from %s %s", focus, rowKey(w.Entries[e.By].Row), e.Row.Keys, e.Row.Noun, e.Row.Request.Key())
			}
		}
	}
	check(r.List(helpRowsID).FocusTarget())
	check(r.Tree(helpRailID).FocusTarget())
}

// The hint line can be turned off: hints.group hidden draws none.
func TestHintLineHidden(t *testing.T) {
	r, m := helpPageFixture(t)
	if len(m.footer().Hints) == 0 {
		t.Fatalf("the footer shows no hints by default")
	}
	if err := r.Settings().Set(HintsGroupSetting, HintsGroupHidden); err != nil {
		t.Fatal(err)
	}
	if f := m.footer(); len(f.Hints) != 0 || strings.Contains(f.Text, "binding.help") {
		t.Fatalf("hints.group hidden still draws the hint line: %q", f.Text)
	}
}

// modalHelp sets help.style modal: for the tests of the ? modal, which ?
// opens only with it since the page became the default.
func modalHelp(r *Runtime) *Runtime {
	if err := r.Settings().Set(HelpStyleSetting, HelpStyleModal); err != nil {
		panic(err)
	}
	return r
}
