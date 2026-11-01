package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// The shared motions (view.edge, view.page; CursorMotions, UX-RULES
// R5.8): g g, G, a count's G and the page keys answer on the yank
// history through the one interface, as on a list.
func TestViewMotionsOnTheYankHistory(t *testing.T) {
	r, m, _, _ := motionPage(t)
	m.Width, m.Height = 60, 20
	for i := range 40 {
		r.Registers().History = append(r.Registers().History, Yank{Text: fmt.Sprintf("y%d", i+1), Shape: "char"})
	}
	motionKeys(t, r, m, "g y")
	if r.TopModal() != yankHistoryID {
		t.Fatalf("g y: top modal %q", r.TopModal())
	}
	page := r.Instances[yankHistoryID].Component.(*yankHistory).PageLines()
	if page <= 1 {
		t.Fatalf("page %d", page)
	}
	g := r.Registers()
	for _, s := range []struct {
		keys string
		want int
	}{
		{"G", 39},
		{"g g", 0},
		{"5 G", 4},
		{"ctrl+d", 4 + page/2},
		{"ctrl+u", 4},
		{"ctrl+f", 4 + page},
		{"ctrl+b", 4},
		{"ctrl+b", 0}, // clamps
		{"3 ctrl+d", 3},
		{"9 9 G", 39},
	} {
		motionKeys(t, r, m, s.keys)
		if g.selection != s.want || r.TopModal() != yankHistoryID {
			t.Fatalf("%s: selection %d modal %q, want %d", s.keys, g.selection, r.TopModal(), s.want)
		}
	}
	if r.Available(Request{Noun: viewNoun, Verb: "edge", Args: Arguments{"edge": "last-col"}}, r.Focus, r.Mode) {
		t.Fatal("the yank history has no column edge, view.edge last-col is available")
	}
}

// The docs viewer's g g and G go through view.edge, not ±1000000-line
// scrolls; ctrl+d and ctrl+u are half its window.
func TestViewMotionsOnTheDocsViewer(t *testing.T) {
	r, m, _, _ := configFixture(t)
	m.Init()
	plainDocsViewer(t, r)
	m.Width, m.Height = 80, 16
	if _, err := r.Execute(Request{Noun: "ochazuke.docs", Verb: "open", Args: Arguments{"topic": "settings"}}); err != nil {
		t.Fatal(err)
	}
	m.View()
	v, ok := r.Instances[docsViewerID].Component.(*docsViewer)
	if !ok {
		t.Fatalf("top modal %q holds no docs viewer", r.TopModal())
	}
	d := v.detail
	if !d.Overflows() {
		t.Fatal("the settings page fits the window")
	}
	last := d.total - d.height
	for _, s := range []struct {
		keys string
		want int
	}{
		{"G", last},
		{"g g", 0},
		{"ctrl+d", d.height / 2},
		{"ctrl+u", 0},
		{"ctrl+f", min(last, d.height)},
		{"3 G", min(last, 2)},
	} {
		for _, key := range strings.Fields(s.keys) {
			m.handleKey(MacroKey{Key: key})
			m.View()
		}
		if d.Offset() != s.want || r.TopModal() != docsViewerID {
			t.Fatalf("%s: offset %d modal %q, want %d", s.keys, d.Offset(), r.TopModal(), s.want)
		}
	}
}

// A timeline answers the shared motions from inside a card as from its
// cards: g g and G go to the first and last card (Vim's buffer ends), a
// count's G to that card; ctrl+d scrolls the window half a page, the step
// staying in view.
func TestViewMotionsOnTheTimeline(t *testing.T) {
	steps := []CardStep{{Header: true}, {ID: "s1", Title: "one", Body: TextBody(numbered("one", 30))}, {ID: "s2", Title: "two", Body: TextBody("2")}}
	cards := turns(3)
	cards[1] = Card{ID: "c2", Noun: "turn", Header: "q", Open: true, Steps: steps}
	f := newTimelineFixture(t, cards)
	f.press("j", "l")
	if _, _, ok := f.tl.Step(); !ok {
		t.Fatalf("l: focus %s, not inside c2", f.r.Focus)
	}
	if !f.r.Available(Request{Noun: viewNoun, Verb: "page", Args: Arguments{"delta": 1, "half": true}}, f.r.Focus, f.r.Mode) {
		t.Fatal("view.page is unavailable inside an overflowing timeline")
	}
	top := f.tl.absTop()
	f.press("ctrl+d")
	if f.tl.absTop() <= top || !f.tl.InBody() {
		t.Fatalf("ctrl+d inside: top %d (was %d) in body %v", f.tl.absTop(), top, f.tl.InBody())
	}
	f.press("G")
	if f.tl.SelectedID() != "c3" || f.r.Focus != "c3" {
		t.Fatalf("G inside a card: selected %q focus %s, want the last card", f.tl.SelectedID(), f.r.Focus)
	}
	f.press("k", "l", "g", "g")
	if f.tl.SelectedID() != "c1" || f.r.Focus != "c1" {
		t.Fatalf("g g inside a card: selected %q focus %s, want the first card", f.tl.SelectedID(), f.r.Focus)
	}
	if !slices.Equal(f.tl.MotionEdges(), []SpillEdge{EdgeFirst, EdgeLast}) {
		t.Fatalf("timeline edges %v", f.tl.MotionEdges())
	}
	if _, err := f.r.Execute(Request{Noun: viewNoun, Verb: "edge", Args: Arguments{"edge": "first", "row": 2}}); err != nil {
		t.Fatal(err)
	}
	f.m.View()
	if f.tl.SelectedID() != "c2" {
		t.Fatalf("view.edge row 2: %q", f.tl.SelectedID())
	}
}

// A cell-mode table answers all four edges through view.edge (0 and $
// are the column edges); a plain list only first and last.
func TestViewEdgeColumnsOnACellTable(t *testing.T) {
	r, m, a, _ := motionPage(t)
	if slices.Contains(a.MotionEdges(), EdgeLastColumn) {
		t.Fatalf("a plain list has a column edge: %v", a.MotionEdges())
	}
	if r.Available(Request{Noun: viewNoun, Verb: "edge", Args: Arguments{"edge": "first-col"}}, r.Focus, r.Mode) {
		t.Fatal("view.edge first-col is available on a plain list")
	}
	a.columns = []Column{{Title: "one"}, {Title: "two"}, {Title: "three"}}
	a.cellSelect = true
	_ = m.View()
	if !slices.Equal(a.MotionEdges(), SpillEdges) {
		t.Fatalf("cell table edges %v", a.MotionEdges())
	}
	for _, s := range []struct {
		edge SpillEdge
		want int
	}{{EdgeLastColumn, 2}, {EdgeFirstColumn, 0}} {
		if _, err := r.Execute(Request{Noun: viewNoun, Verb: "edge", Args: Arguments{"edge": string(s.edge)}}); err != nil {
			t.Fatal(err)
		}
		if a.position() != s.want {
			t.Fatalf("view.edge %s: column %d, want %d", s.edge, a.position(), s.want)
		}
	}
}
