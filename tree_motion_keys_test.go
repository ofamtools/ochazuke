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

// A class on a tree's top-level rows lets collection.next-class and
// previous-class walk the folds: from a child (its own fold first, going
// up), from a classed row, with a count, across a collapsed row (not
// opened) and at both edges (the cursor stays).
func TestTreeClassMotionsWalkFolds(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 80, 24
	_ = m
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return r.Tree("t").Block(w, h) }); err != nil {
		t.Fatal(err)
	}
	top := []string{"project"}
	tree, err := r.AddTree("t", []TreeNode{
		{ID: "p1", Label: "one", Classes: top}, {ID: "c1", Label: "c1", Parent: "p1"}, {ID: "g1", Label: "g1", Parent: "c1"},
		{ID: "p2", Label: "two", Classes: top}, {ID: "c2", Label: "c2", Parent: "p2"},
		{ID: "p3", Label: "three", Classes: top}, {ID: "c3", Label: "c3", Parent: "p3"},
	}, Parent("main"), NoSpill())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	if err := tree.Focus(); err != nil {
		t.Fatal(err)
	}
	if err := tree.SetCollapsed("p2", true); err != nil {
		t.Fatal(err)
	}
	for i, s := range []struct {
		from, verb string
		steps      int
		want       string
	}{
		{"g1", "previous-class", 1, "p1"},
		{"p1", "previous-class", 1, "p1"},
		{"p3", "previous-class", 1, "p2"},
		{"c3", "previous-class", 1, "p3"},
		{"c3", "previous-class", 9, "p1"},
		{"c3", "next-class", 1, "c3"},
		{"g1", "next-class", 1, "p2"},
		{"p1", "next-class", 2, "p3"},
		{"p1", "next-class", 9, "p3"},
	} {
		if err := r.SetFocus(s.from); err != nil {
			t.Fatal(i, err)
		}
		if _, err := r.Execute(Request{Noun: "ochazuke.collection", Verb: s.verb, Args: Arguments{"class": "project", "steps": s.steps}}); err != nil {
			t.Fatal(i, err)
		}
		if r.Focus != s.want || !tree.Collapsed("p2") {
			t.Fatalf("case %d %s %s x%d: focus %q, want %q with p2 collapsed", i, s.from, s.verb, s.steps, r.Focus, s.want)
		}
	}
}
