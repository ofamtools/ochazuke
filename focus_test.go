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
	if !r.focusInTopLevel() {
		t.Fatalf("focus %q is outside the top level %q", r.Focus, r.focusRoot())
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
	// Push onto app (an instance the body sits beneath) focuses the body.
	if r.Focus != "dialog" || r.Instances["dialog"].Payload.Return != "field" || r.BaseFocus() != "field" {
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

// jumpFixture: page > [left (region) > [sidebar (list), note (input)],
// status (plain), right (region) > [items (list)]].
func jumpFixture(t *testing.T) (*Runtime, *List, *List) {
	t.Helper()
	r := NewRuntime()
	r.Define("panel").Region()
	r.Instance("page")
	r.Instance("left", Parent("page"), Kind("panel"))
	r.Define("row")
	sidebar, err := r.AddList("sidebar", []ListItem{{ID: "s1", Label: "one", Noun: "row"}, {ID: "s2", Label: "two", Noun: "row"}}, Parent("left"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddInput(NewInput("note", "", false), Parent("left")); err != nil {
		t.Fatal(err)
	}
	r.Instance("status", Parent("page"))
	r.Instance("right", Parent("page"), Kind("panel"))
	items, err := r.AddList("items", []ListItem{{ID: "i1", Label: "one", Noun: "row"}, {ID: "i2", Label: "two", Noun: "row"}}, Parent("right"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("page", "Page", func(*Runtime, int, int) Block { return Text("") }, Focus("sidebar")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	return r, sidebar, items
}

func TestFocusJumpsStopToStopInTreeOrder(t *testing.T) {
	r, sidebar, items := jumpFixture(t)
	if r.Focus != "s1" {
		t.Fatalf("start focus %q", r.Focus)
	}
	sidebar.Move(1)
	items.Move(1)
	for _, step := range []struct {
		keys []string
		want string
	}{
		{[]string{"}"}, "note"},    // the list is one stop; its rows are not
		{[]string{"}"}, "i2"},      // status is not focusable; land on the selected row
		{[]string{"}"}, "s2"},      // wrap
		{[]string{"{"}, "i2"},      // backward wraps too
		{[]string{"2", "{"}, "s2"}, // a count is steps
		{[]string{"tab"}, "note"},  // tab is a stop jump
		{[]string{"shift+tab"}, "s2"},
		{[]string{")"}, "i2"}, // region to region, landing on the first stop
		{[]string{"("}, "s2"}, // back to the left region's remembered focus
	} {
		for _, key := range step.keys {
			if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
				t.Fatalf("%v: %v", step.keys, err)
			}
		}
		if r.Focus != step.want {
			t.Fatalf("after %v: focus %q, want %q", step.keys, r.Focus, step.want)
		}
	}
	// From a focus outside every stop, } takes the first stop after it.
	if err := r.SetFocus("status"); err != nil {
		t.Fatal(err)
	}
	if err := r.JumpFocus(1, "stop"); err != nil || r.Focus != "i2" {
		t.Fatalf("from status: %q %v", r.Focus, err)
	}
	if err := r.SetFocus("status"); err != nil {
		t.Fatal(err)
	}
	if err := r.JumpFocus(-1, "stop"); err != nil || r.Focus != "note" {
		t.Fatalf("back from status: %q %v", r.Focus, err)
	}
	// An instance can opt out.
	r.Instances["note"].Unfocusable = true
	if err := r.SetFocus("s2"); err != nil {
		t.Fatal(err)
	}
	if err := r.JumpFocus(1, "stop"); err != nil || r.Focus != "i2" {
		t.Fatalf("opted-out input still a stop: %q", r.Focus)
	}
	// focus.set is the operation handlers use.
	if _, err := r.Execute(Request{Verb: "set", Noun: "focus", Args: Arguments{"id": "status"}}); err != nil || r.Focus != "status" {
		t.Fatalf("focus.set: %q %v", r.Focus, err)
	}
}

func TestFocusJumpsStayInsideTheModal(t *testing.T) {
	r, _, _ := jumpFixture(t)
	r.Instance("dialog")
	for _, id := range []string{"first", "second"} {
		if err := r.AddInput(NewInput(id, "", false), Parent("dialog")); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Push("dialog", "first"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"second", "first"} {
		if _, err := r.Dispatch(MacroKey{Key: "tab"}); err != nil {
			t.Fatal(err)
		}
		if r.Focus != want {
			t.Fatalf("modal ring: focus %q, want %q", r.Focus, want)
		}
	}
	// In insert mode tab is not a jump: the app row is a normal-mode row.
	if err := r.SetMode("insert"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(MacroKey{Key: "tab"}); err != nil || r.Focus != "first" {
		t.Fatalf("insert tab moved focus to %q", r.Focus)
	}
}

// OpenModal with an input: the body holds the input, the input is focused
// in its pinned mode, and Pop drops both and restores focus; the *Input
// keeps its text and may be opened again.
func TestOpenModalWithInput(t *testing.T) {
	r, _ := focusFixture(t)
	field := NewInput("answer", "draft", false)
	body, err := r.OpenModal("ask", Payload{Prompt: "Name?"}, WithInput(field, Kind("prompt")))
	if err != nil {
		t.Fatal(err)
	}
	if body.Parent != "field" || r.Instances["answer"].Parent != "ask" || r.Focus != "answer" || r.Mode.Name != "insert" {
		t.Fatalf("open: parent=%q focus=%q mode=%q", body.Parent, r.Focus, r.Mode.Name)
	}
	if _, err := r.Execute(Request{Verb: "insert", Noun: "input", Args: Arguments{"text": "!"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Instances["ask"] != nil || r.Instances["answer"] != nil || r.Focus != "field" || string(field.Text) != "!draft" {
		t.Fatalf("pop: focus=%q text=%q", r.Focus, string(field.Text))
	}
	if _, err := r.OpenModal("ask", Payload{}, WithInput(field, Kind("prompt"))); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

// The focus never leaves the top modal: explicit focus moves outside it are
// refused, internal retargeting and removal clamp to the modal, Push adopts
// a focus outside the body for as long as it is open, and Pop restores.
// After every step the live focus has the top modal on its path, so the
// resolver's cut at the modal always applies.
func TestFocusNeverLeavesTopModal(t *testing.T) {
	r, _ := focusFixture(t)
	check := func(label string) {
		t.Helper()
		if !r.focusInTopLevel() {
			t.Fatalf("%s: focus %q outside the top level %q", label, r.Focus, r.focusRoot())
		}
		if top := r.TopModal(); top != "" && !slices.Contains(r.Ancestry(r.Focus), top) {
			t.Fatalf("%s: the walk would not cut at %q", label, top)
		}
		assertPageOnFocusPath(t, r)
	}
	check("page")
	if _, err := r.OpenModal("ask", Payload{Prompt: "?"}, WithInput(NewInput("answer", "", false))); err != nil {
		t.Fatal(err)
	}
	check("open modal")
	if err := r.SetFocus("panel"); err == nil || err.Error() != "focus is outside the open modal" {
		t.Fatalf("SetFocus outside: %v", err)
	}
	if _, err := r.Execute(Request{Verb: "set", Noun: "focus", Args: Arguments{"id": "field"}}); err == nil {
		t.Fatal("focus.set outside the modal must fail")
	}
	check("refused")
	if err := r.SetFocus("ask"); err != nil {
		t.Fatal(err)
	}
	r.RetargetFocus(func(string) bool { return true }, "panel")
	if r.Focus != "ask" {
		t.Fatalf("retarget clamps to the modal: %q", r.Focus)
	}
	check("retarget")
	if err := r.SetFocus("answer"); err != nil {
		t.Fatal(err)
	}
	if err := r.JumpFocus(1, "stop"); err != nil {
		t.Fatal(err)
	}
	check("jump")
	if err := r.SetFocus("answer"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveInstance("answer"); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "ask" {
		t.Fatalf("removing the focused descendant lands on the modal: %q", r.Focus)
	}
	check("remove")
	// Push with a focus outside the body adopts it while the modal is open.
	other := NewInput("loose", "", false)
	if err := r.AddInput(other, Parent("panel")); err != nil {
		t.Fatal(err)
	}
	if err := r.Push("legacy", "loose"); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "loose" || r.Instances["loose"].Parent != "legacy" {
		t.Fatalf("push adopts the focus: focus %q parent %q", r.Focus, r.Instances["loose"].Parent)
	}
	check("push")
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "ask" || r.Instances["loose"].Parent != "panel" {
		t.Fatalf("pop restores the focus and the lent parent: focus %q parent %q", r.Focus, r.Instances["loose"].Parent)
	}
	check("pop to the modal below")
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "panel" { // the retarget above also moved the modal's return
		t.Fatalf("pop to the page: %q", r.Focus)
	}
	check("pop to the page")
}
