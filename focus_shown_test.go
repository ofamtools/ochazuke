package ochazuke

import (
	"strings"
	"testing"
)

// shownPage is a page holding list a (rows a1, a2) and list n nested under
// row a2 (rows n1, n2), drawn by draw: the ids of the lists the frame
// draws. The page starts on a1.
func shownPage(t *testing.T, draw *[]string) (*Model, *Runtime) {
	t.Helper()
	r := NewRuntime()
	r.Instance("page")
	if _, err := r.AddList("a", []ListItem{{ID: "a1", Label: "a1", Noun: "ochazuke.list"}, {ID: "a2", Label: "a2", Noun: "ochazuke.list"}}, Parent("page")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddList("n", []ListItem{{ID: "n1", Label: "n1", Noun: "ochazuke.list"}, {ID: "n2", Label: "n2", Noun: "ochazuke.list"}}, Parent("a2")); err != nil {
		t.Fatal(err)
	}
	render := func(r *Runtime, width, height int) Block {
		var parts []string
		for _, id := range *draw {
			parts = append(parts, r.List(id).View(width, 2))
		}
		return Text(strings.Join(parts, "\n"))
	}
	if _, err := r.AddPage("page", "Page", render, Focus("a")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 60, 20
	return m, r
}

// A stop nested under a row that the frame stops drawing gives the focus
// to the nearest ancestor the frame drew (the row), not to the page's
// first stop; the hints are the row's in the same frame.
func TestFocusHiddenNestedStopGoesToNearestDrawnAncestor(t *testing.T) {
	draw := []string{"a", "n"}
	m, r := shownPage(t, &draw)
	m.View()
	if err := r.SetFocus("n2"); err != nil {
		t.Fatal(err)
	}
	m.View()
	if r.Focus != "n2" {
		t.Fatalf("drawn: focus %q, want n2", r.Focus)
	}
	draw = []string{"a"}
	m.View()
	if r.Focus != "a2" {
		t.Fatalf("hidden: focus %q, want the row a2", r.Focus)
	}
	if sel, _ := r.List("a").Selected(); sel.ID != "a2" {
		t.Fatalf("selected %q, want a2", sel.ID)
	}
}

// The rule waits for a frame: before the first frame (the CLI, a headless
// caller) a focus on any stop stands, and a page whose frame placed no stop
// at all (views that never call Placed) is never judged.
func TestFocusHiddenWaitsForAFrameThatPlacedStops(t *testing.T) {
	draw := []string{"a"}
	m, r := shownPage(t, &draw)
	if err := r.SetFocus("n1"); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "n1" {
		t.Fatalf("before a frame: focus %q, want n1", r.Focus)
	}
	draw = nil
	m.View()
	if r.Focus != "n1" {
		t.Fatalf("a frame that placed nothing: focus %q, want n1", r.Focus)
	}
	draw = []string{"a"}
	m.View()
	if r.Focus != "a2" {
		t.Fatalf("after a frame: focus %q, want a2", r.Focus)
	}
}

// While a modal is open the frame draws the level beneath it and the modal
// owns the focus: nothing moves, not the modal's focus and not the return
// it closes back to. The first frame after it closes applies the rule.
func TestFocusHiddenWaitsForTheModalToClose(t *testing.T) {
	draw := []string{"a", "n"}
	m, r := shownPage(t, &draw)
	m.View()
	if err := r.SetFocus("n1"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenModal("confirm", Payload{Prompt: "Sure?"}); err != nil {
		t.Fatal(err)
	}
	modal := r.Focus
	draw = []string{"a"}
	m.View()
	if r.Focus != modal || r.BaseFocus() != "n1" {
		t.Fatalf("modal open: focus %q base %q, want %q over n1", r.Focus, r.BaseFocus(), modal)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "n1" {
		t.Fatalf("popped: focus %q, want n1 back", r.Focus)
	}
	m.View()
	if r.Focus != "a2" {
		t.Fatalf("first frame after the modal: focus %q, want a2", r.Focus)
	}
}

// Every stop hidden: the focus rests on the page itself.
func TestFocusHiddenWithNoStopDrawnRestsOnThePage(t *testing.T) {
	draw := []string{"a", "n"}
	m, r := shownPage(t, &draw)
	m.View()
	if err := r.SetFocus("n1"); err != nil {
		t.Fatal(err)
	}
	// A frame drawing only a list that is no stop (Unfocusable) keeps the
	// rule's precondition (something was placed) while a and n are gone.
	if _, err := r.AddList("z", nil, Parent("page")); err != nil {
		t.Fatal(err)
	}
	r.Instances["z"].Unfocusable = true
	draw = []string{"z"}
	m.View()
	if r.Focus != "page" {
		t.Fatalf("focus %q, want the page", r.Focus)
	}
}
