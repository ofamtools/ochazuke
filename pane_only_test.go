package ochazuke

import (
	"strings"
	"testing"
)

func TestPaneOnlyKeepsAPaneUntilTheFocusLeavesIt(t *testing.T) {
	r, _, _ := jumpFixture(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// A named pane takes the focus; the page's layout draws it alone.
	must(r.ShowPaneOnly("page", "right"))
	if r.PaneOnly("page") != "right" || r.Focus != "i1" {
		t.Fatalf("only: pane %q focus %q", r.PaneOnly("page"), r.Focus)
	}
	if !r.PaneLayout("i1", Layout{}).Only {
		t.Fatal("PaneLayout does not carry Only")
	}
	// Moving inside the pane keeps it; a modal does not restore.
	must(r.SetFocus("i2"))
	r.Define("dlg")
	if _, err := r.OpenModal("dlg", Payload{}); err != nil {
		t.Fatal(err)
	}
	must(r.Pop())
	if r.PaneOnly("page") != "right" {
		t.Fatal("a move inside the pane or a modal restored the panes")
	}
	// Leaving the pane brings the other back.
	must(r.SetFocus("s1"))
	if r.PaneOnly("page") != "" {
		t.Fatal("leaving the pane kept it alone")
	}
	// The verbs: default the focused stop; restore.
	if _, err := r.Execute(Request{Verb: "only", Noun: "ochazuke.pane"}); err != nil {
		t.Fatal(err)
	}
	if r.PaneOnly("page") != "sidebar" {
		t.Fatalf("pane.only default: %q", r.PaneOnly("page"))
	}
	if _, err := r.Execute(Request{Verb: "restore", Noun: "ochazuke.pane"}); err != nil {
		t.Fatal(err)
	}
	if r.PaneOnly("page") != "" {
		t.Fatal("pane.restore kept the pane alone")
	}
	if err := r.ShowPaneOnly("page", "nope"); err == nil {
		t.Fatal("an unknown pane was accepted")
	}
}

func TestSplitOnlyDrawsTheFocusedPanel(t *testing.T) {
	l := Layout{Width: 80, Height: 12, Styles: NewStyles(Appearance{Theme: "mono"}), Only: true}
	got := l.SplitFocusBlock(SplitRight, "left", Text("L"), "right", Text("R")).Text
	if strings.Contains(got, "left") || !strings.Contains(got, "right") {
		t.Fatalf("Only, right focused:\n%s", got)
	}
	both := l.SplitFocusBlock(SplitNone, "left", Text("L"), "right", Text("R")).Text
	if !strings.Contains(both, "left") || !strings.Contains(both, "right") {
		t.Fatalf("Only with no focused side draws both:\n%s", both)
	}
	l.Only = false
	if l.SplitFocusBlock(SplitRight, "left", Text("L"), "right", Text("R")).Text == got {
		t.Fatal("Only off drew one panel")
	}
}
