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

// esc goes back from the help page exactly when modal.esc-closes is on, from
// the list and from the rail; off, it leaves the page open and q goes back.
func TestHelpPageEscFollowsModalEscCloses(t *testing.T) {
	for _, closes := range []bool{true, false} {
		t.Run(fmt.Sprintf("esc-closes=%v", closes), func(t *testing.T) {
			r, m := helpPageFixture(t)
			r.EnsureConfigSettings()
			if err := r.Settings().Set(ModalEscClosesSetting, closes); err != nil {
				t.Fatal(err)
			}
			for _, keys := range [][]string{{"?"}, {"?", "tab"}} {
				for _, key := range keys {
					keytest.Press(m, key)
				}
				keytest.Press(m, "esc")
				if open := r.Page == HelpPageID; open == closes {
					t.Fatalf("after %v esc: page %q with esc-closes %v", keys, r.Page, closes)
				}
				if !closes {
					keytest.Press(m, "q")
				}
				if r.Page == HelpPageID {
					t.Fatalf("after %v the help page is still open", keys)
				}
			}
		})
	}
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
	// Hidden default rows are listed like any other, with their file.
	found := false
	for _, v := range r.helpPage().Rows {
		found = found || v.Hidden && v.Key == "F" && v.Operation == "outline.create" && v.Source == FrameworkBindingsSource+"bindings/tree.json"
	}
	if !found {
		t.Error("the hidden row F outline.create is not listed with its file")
	}
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

// ? on a noun page walks from that page's focused row, and q goes back to
// the noun page with its row focused.
func TestHelpPageFromANounPage(t *testing.T) {
	r, m := nounPageFixture(t)
	m.Width, m.Height = 140, 40
	if err := r.OpenIndex("task", ""); err != nil {
		t.Fatal(err)
	}
	landLoads(m, r.PendingCommands())
	list := r.List(IndexPageID("task") + "/list")
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	page, focus := r.Page, r.Focus
	keytest.Press(m, "?")
	p := r.helpPage()
	if r.Page != HelpPageID || p.Current.Focus != focus || p.Walk.Place.Noun != "task" {
		t.Fatalf("? from the noun page: page %q, walking from %q (%s), want %q", r.Page, p.Current.Focus, p.Walk.Place.Noun, focus)
	}
	keytest.Press(m, "q")
	if r.Page != page || r.Focus != focus {
		t.Fatalf("q returned to page %q focus %q, want %q %q", r.Page, r.Focus, page, focus)
	}
}
