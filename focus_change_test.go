package ochazuke

import (
	"slices"
	"testing"
)

// forceFocus installs a focus for a test that needs one no public move
// reaches in a single step (an input on a hidden page, a sibling the
// fixture's render reads as unfocused). It goes through the one writer
// with focusAssign, so the guard (TestStateWriteOwnership) holds for tests
// too: the focus is assigned and nothing else runs, as the bare field
// write it replaces did.
func forceFocus(r *Runtime, id string) { r.changeFocus(id, focusMove{cause: focusAssign}) }

// A focus move commits the mode once: a page reopened with a saved mode
// and a modal popped onto its return restore that mode with no default
// landing (normal) before it, and a move across stops or the repair after
// a removal apply the focus rule once. Each step advances the mode
// generation by one and tells an observer at most once.
func TestFocusTransitionSingleModeCommit(t *testing.T) {
	r := NewRuntime()
	r.Define("panel").Region()
	var calls []modeSpyCall
	render := func(*Runtime, int, int) Block { return Text("") }
	for _, page := range []string{"one", "two"} {
		r.Instance(page, Parent("app"))
		for _, child := range []string{page + "-a", page + "-b"} {
			r.Instance(child, Parent(page), Kind("panel")).Component = modeSpy{child, r, &calls}
		}
		if err := r.RegisterPage(&Page{ID: page, Title: page, Focus: page + "-a", Render: render}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.OpenPage("one"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetMode("insert"); err != nil {
		t.Fatal(err)
	}
	step := func(name string, run func() error, focus, mode string, observed []string) {
		t.Helper()
		calls = nil
		before := r.modeGeneration
		if err := run(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r.Focus != focus || r.Mode.Name != mode {
			t.Fatalf("%s: focus %q mode %q, want %q %q", name, r.Focus, r.Mode.Name, focus, mode)
		}
		if got := r.modeGeneration - before; got != 1 {
			t.Fatalf("%s: the mode generation moved %d times, want one commit", name, got)
		}
		var ids []string
		for _, c := range calls {
			ids = append(ids, c.id+":"+c.from+">"+c.to)
		}
		if !slices.Equal(ids, observed) {
			t.Fatalf("%s: observers %v, want %v", name, ids, observed)
		}
	}
	step("open two", func() error { return r.OpenPage("two") }, "two-a", "normal",
		[]string{"two-a:insert>normal", "one-a:insert>normal"})
	step("reopen one at its saved mode", func() error { return r.OpenPage("one") }, "one-a", "insert",
		[]string{"one-a:normal>insert", "two-a:normal>insert"})
	if err := r.Push("modal", ""); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "modal" || r.Mode.Name != "normal" {
		t.Fatalf("push: focus %q mode %q", r.Focus, r.Mode.Name)
	}
	step("pop onto the saved return", r.Pop, "one-a", "insert", []string{"one-a:normal>insert"})
	step("move across stops", func() error { return r.SetFocus("one-b") }, "one-b", "normal",
		[]string{"one-b:insert>normal", "one-a:insert>normal"})
	if err := r.SetMode("insert"); err != nil {
		t.Fatal(err)
	}
	step("remove the focused stop", func() error { return r.RemoveInstance("one-b") }, "one", "normal", nil)
}
