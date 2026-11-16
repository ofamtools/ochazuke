package ochazuke

import "testing"

// The motion keys keep their meaning on a tree that traps its edges
// (NoSpill): v0.143.0 and v0.144.0 bound hidden outline rows on k in
// normal and on j and k in visual-line, which answered where
// collection.move steps aside at an edge. k on the top row stays in
// normal mode and opens no form; in visual-line j and k extend and shrink
// the selection and do nothing past an edge. TestRules/hiddenkeys keeps
// any hidden row off keys like these; this is the case that was reported.
func TestTreeMotionKeysKeepTheirMeaning(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 80, 24
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return r.Tree("t").Block(w, h) }); err != nil {
		t.Fatal(err)
	}
	tree, err := r.AddTree("t", []TreeNode{{ID: "a", Label: "alpha"}, {ID: "b", Label: "beta"}}, Parent("main"), NoSpill())
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
	focus := r.Focus
	pressKeys(m, "k")
	if r.Mode.Name != "normal" || len(r.modalIDs()) != 0 || r.Focus != focus {
		t.Fatalf("k on the top row: mode %q, modals %v, focus %q (was %q)", r.Mode.Name, r.modalIDs(), r.Focus, focus)
	}
	steps := []struct {
		key  string
		rows int
	}{{"V", 1}, {"j", 2}, {"j", 2}, {"k", 1}, {"k", 1}}
	for i, s := range steps {
		pressKeys(m, s.key)
		if got := len(tree.List().Marked()); got != s.rows || r.Mode.Name != "visual-line" || len(r.modalIDs()) != 0 {
			t.Fatalf("step %d (%s): %d rows selected in %q, modals %v; want %d in visual-line", i, s.key, got, r.Mode.Name, r.modalIDs(), s.rows)
		}
	}
	if len(writes) != 0 {
		t.Fatalf("motions wrote %v", writes)
	}
}
