package ochazuke

import (
	"slices"
	"testing"

	"github.com/unixsuperhero/ochazuke/keytest"
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
