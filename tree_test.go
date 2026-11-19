package ochazuke

import (
	"slices"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// treeFixture is an editable outline on a page, focused, with its writes
// logged.
func treeFixture(t *testing.T) (*Runtime, *Model, *Tree, *[]string) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 80, 24
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return r.Tree("t").Block(w, h) }); err != nil {
		t.Fatal(err)
	}
	tree, err := r.AddTree("t", []TreeNode{{ID: "a", Label: "alpha"}, {ID: "b", Parent: "a", Label: "beta"}}, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	var writes []string
	tree.OnChange = func(c TreeChange) error { writes = append(writes, c.String()); return nil }
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	if err := tree.Focus(); err != nil {
		t.Fatal(err)
	}
	return r, m, tree, &writes
}

// Quitting writes an open line that has not paused yet. i opens the line
// selected (rename.select is on by default), so the typing replaces it.
func TestTreeQuitFlushesTheOpenLine(t *testing.T) {
	r, m, _, writes := treeFixture(t)
	keytest.Press(m, "i")
	for _, k := range []string{"!", "?"} {
		m.Update(keytest.Msg(k, true, false))
	}
	if len(*writes) != 0 {
		t.Fatalf("written before the pause: %v", *writes)
	}
	r.Quit = true
	m.Update(nil)
	if want := []string{"rename a: !?"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
}

// A read-only tree hides the outline verbs: tab is the app's focus jump,
// o is not bound, and the footer offers only expand, collapse and the
// zoom (view state, on every tree).
func TestReadOnlyTreeHidesOutlineVerbs(t *testing.T) {
	r, _, tree, _ := treeFixture(t)
	tree.OnChange = nil
	for _, b := range r.ListBindings(r.Focus) {
		if b.Request.Noun == "ochazuke.outline" && !slices.Contains([]string{"expand", "collapse", "fold", "zoom-in", "zoom-out", "zoom-reset"}, b.Request.Verb) {
			t.Fatalf("read-only tree answers %s with %s", b.Keys, b.Request.Key())
		}
	}
	// Its help has no "d esc": the sequence it cancels does not answer.
	for _, row := range BindingHelp(r, r.Focus) {
		if row.Key == "d esc" {
			t.Fatalf("read-only tree help lists %+v", row)
		}
	}
	if warnings, err := r.ValidateBindings(); err != nil || len(warnings) > 0 {
		t.Fatal(warnings, err)
	}
}

// pasteLines nests by tabs or two-space steps, never deeper than one level
// under the line above, and drops bullets.
func TestPasteLinesNestByIndent(t *testing.T) {
	lines, depths := pasteLines("  - one\n      deep\n  two\n\n\tthree")
	if !slices.Equal(lines, []string{"one", "deep", "two", "three"}) || !slices.Equal(depths, []int{0, 1, 0, 0}) {
		t.Fatalf("lines %q depths %v", lines, depths)
	}
}

// outline.collapse is available only where it acts: on an open parent (it
// folds), on a child (it steps to the parent); on a closed top-level node
// it has nothing to do, so the verb is unavailable and h falls through.
func TestTreeCollapseUnavailableWithNothingToDo(t *testing.T) {
	r, _, tree, _ := treeFixture(t)
	collapse := Request{Noun: "ochazuke.outline", Verb: "collapse"}
	for _, step := range []struct {
		focus string
		fold  bool
		want  bool
	}{
		{"a", false, true},
		{"b", false, true},
		{"a", true, false},
	} {
		if step.fold {
			if err := tree.CollapseAll(); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.SetFocus(step.focus); err != nil {
			t.Fatal(err)
		}
		if got := r.Available(collapse, r.Focus, r.Mode); got != step.want {
			t.Fatalf("collapse on %s (folded %v): available %v, want %v", step.focus, step.fold, got, step.want)
		}
	}
}

// shift+enter on an open line breaks the line inside the row's text: no
// new row, the line stays open, and leaving it writes both lines as the
// row's one label (alt+enter and ctrl+j, for terminals that report no
// shift+enter, do the same).
func TestTreeShiftEnterBreaksTheLine(t *testing.T) {
	t.Parallel()
	r, m, tree, writes := treeFixture(t)
	pressKeys(m, "i", "x", "shift+enter", "y", "ctrl+j", "z", "esc")
	if r.Mode.Name != "normal" || tree.Editing() != "" {
		t.Fatalf("mode %q editing %q", r.Mode.Name, tree.Editing())
	}
	if got := nodeIDs(tree); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("nodes %v: a newline made a row", got)
	}
	if n, _ := tree.Selected(); n.Label != "x\ny\nz" {
		t.Fatalf("label %q, want x\\ny\\nz", n.Label)
	}
	if want := "rename a: x\ny\nz"; len(*writes) == 0 || (*writes)[len(*writes)-1] != want {
		t.Fatalf("writes %q, want last %q", *writes, want)
	}
}

// A standard write an outline's verb runs for a row goes on the outline's
// undo stack (Tree.RecordRequest): u reverts it through its history
// entry, ctrl+r runs it again.
func TestTreeRecordRequestUndoesTheWrite(t *testing.T) {
	r, _, tree, _ := treeFixture(t)
	s := &derivedStore{key: "id", things: []Thing{{"id": "t1", "title": "Original"}}}
	st := s.store()
	st.Undo = true
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title"}).Store(st)
	refreshed := 0
	refresh := func() error { refreshed++; return nil }
	q := Request{Noun: "parcel", Verb: "set", Args: Arguments{"id": "t1", "props": map[string]any{"title": "Changed"}}}
	if _, err := tree.RecordRequest("a", q, refresh); err != nil {
		t.Fatal(err)
	}
	if err := tree.history(false); err != nil || s.find("t1")["title"] != "Original" {
		t.Fatalf("undo: %v %v", err, s.find("t1"))
	}
	if err := tree.history(true); err != nil || s.find("t1")["title"] != "Changed" || refreshed != 2 {
		t.Fatalf("redo: %v %v refreshed %d", err, s.find("t1"), refreshed)
	}
}
