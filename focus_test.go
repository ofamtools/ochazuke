package ochazuke

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// focusFixture: page > panel > field (an input), and a second page.
func focusFixture(t *testing.T) (*Runtime, *Input) {
	t.Helper()
	r := NewRuntime()
	r.Instance("page")
	r.Instance("panel", Parent("page"))
	in := NewInput("field", "abc", false)
	if err := r.AddInput(in, Parent("panel")); err != nil {
		t.Fatal(err)
	}
	r.Instance("other")
	render := func(*Runtime, int, int) Block { return Text("") }
	if _, err := r.AddPage("page", "Page", render, Focus("field")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("other", "Other", render); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	return r, in
}

func assertPageOnFocusPath(t *testing.T, r *Runtime) {
	t.Helper()
	if !slices.Contains(r.Ancestry(r.BaseFocus()), r.Page) {
		t.Fatalf("page %q is not an ancestor of base focus %q", r.Page, r.BaseFocus())
	}
}

func TestFocusIsOneFieldAndPagesRememberIt(t *testing.T) {
	r, _ := focusFixture(t)
	if r.Focus != "field" {
		t.Fatalf("focus = %q", r.Focus)
	}
	assertPageOnFocusPath(t, r)
	if err := r.SetMode("insert"); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("other"); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "other" || r.Mode.Name != "normal" || r.PageFocus("page") != "field" {
		t.Fatalf("other page: focus=%q mode=%q remembered=%q", r.Focus, r.Mode.Name, r.PageFocus("page"))
	}
	assertPageOnFocusPath(t, r)
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "field" || r.Mode.Name != "insert" {
		t.Fatalf("page.open restores focus and mode: focus=%q mode=%q", r.Focus, r.Mode.Name)
	}
	assertPageOnFocusPath(t, r)
}

func TestModalPopRestoresReturnFocus(t *testing.T) {
	r, _ := focusFixture(t)
	if err := r.Push("dialog", "app"); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "app" || r.Instances["dialog"].Payload.Return != "field" || r.BaseFocus() != "field" {
		t.Fatalf("push: focus=%q return=%q", r.Focus, r.Instances["dialog"].Payload.Return)
	}
	assertPageOnFocusPath(t, r)
	if _, err := r.Execute(Request{Verb: "pop", Noun: "modal"}); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "field" || len(r.Modals()) != 0 || r.Instances["dialog"] != nil {
		t.Fatalf("pop: focus=%q modals=%d", r.Focus, len(r.Modals()))
	}
}

func TestModalPopFallsBackToNearestSurvivingAncestor(t *testing.T) {
	r, _ := focusFixture(t)
	if err := r.Push("dialog", "app"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveInstance("panel"); err != nil {
		t.Fatal(err)
	}
	if r.Instances["panel"] != nil || r.Instances["field"] != nil {
		t.Fatal("RemoveInstance must drop the subtree")
	}
	if got := r.Instances["dialog"].Payload.Return; got != "page" {
		t.Fatalf("modal return = %q, want the surviving ancestor page", got)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "page" || r.Mode.Name != "normal" {
		t.Fatalf("pop fell back to %q mode %q", r.Focus, r.Mode.Name)
	}
	assertPageOnFocusPath(t, r)
}

type closeRecorder struct{ closed *[]string }

func (c closeRecorder) HandleMessage(tea.Msg) bool { return false }
func (c closeRecorder) PendingCommand() tea.Cmd    { return nil }
func (c closeRecorder) Close()                     { *c.closed = append(*c.closed, "widget") }

func TestRemoveInstanceFixesFocusAndClosesComponents(t *testing.T) {
	r, _ := focusFixture(t)
	var closed []string
	r.Instance("widget", Parent("panel")).Component = closeRecorder{&closed}
	if err := r.SetMode("insert"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveInstance("panel"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(closed, []string{"widget"}) || r.Instances["widget"] != nil {
		t.Fatalf("descendant component not closed: %v", closed)
	}
	if r.Focus != "page" || r.Mode.Name != "normal" {
		t.Fatalf("focus=%q mode=%q", r.Focus, r.Mode.Name)
	}
	if err := r.RemoveInstance("app"); err == nil {
		t.Fatal("app must not be removable")
	}
	if err := r.RemoveInstance("panel"); err == nil {
		t.Fatal("removing an unknown instance must fail")
	}
}
