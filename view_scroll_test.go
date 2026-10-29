package ochazuke

import (
	"strings"
	"testing"
)

// ctrl+e and ctrl+y scroll a self-windowing list's window a line, as Vim's
// CTRL-E and CTRL-Y (:help CTRL-E): the selection stays while it is inside
// the window's scrolloff margin, else it moves to the nearest row inside;
// counts multiply; the window clamps at the ends and the focus never
// leaves the list.
func TestViewScrollMovesAListWindowKeepingTheCursor(t *testing.T) {
	r, m, a, _ := motionPage(t)
	// a windows 40 rows in 9 lines (the 10th is the marker), scrolloff 2.
	motionKeys(t, r, m, "j j j j j")
	steps := []struct {
		keys   string
		offset int
		focus  string
		why    string
	}{
		{"ctrl+e", 1, "a6", "the cursor is inside the margin: it stays"},
		{"3 ctrl+e", 4, "a7", "the window passed the cursor: it moves to the margin's first row"},
		{"ctrl+y", 3, "a7", "up a line, the cursor stays"},
		{"1 0 0 ctrl+e", 31, "a34", "clamped at the last row; the cursor on the margin"},
		{"1 0 0 ctrl+y", 0, "a7", "clamped at the first row; the cursor on the window's last margin row"},
	}
	for _, s := range steps {
		motionKeys(t, r, m, s.keys)
		if a.offset != s.offset || r.Focus != s.focus {
			t.Fatalf("%s: offset %d focus %q, want %d %q (%s)", s.keys, a.offset, r.Focus, s.offset, s.focus, s.why)
		}
	}
	if hints := BindingHints(r, r.Focus); strings.Contains(hints, "view.scroll") {
		t.Fatalf("view.scroll is a motion, off the footer: %q", hints)
	}
}

// A list that shows every row has nothing to scroll: view.scroll is
// unavailable there and ctrl+e does nothing; with an overflowing page
// detail it scrolls the detail, as J does.
func TestViewScrollFallsBackToThePageDetail(t *testing.T) {
	m, _, d := detailModel(t, 100)
	r := m.Runtime
	focus := r.Focus
	for _, key := range []string{"ctrl+e", "ctrl+e", "5", "ctrl+e", "ctrl+y"} {
		m.handleKey(MacroKey{Key: key})
		m.View()
	}
	if d.Offset() != 6 || r.Focus != focus || m.Status != "" {
		t.Fatalf("ctrl+e ctrl+e 5ctrl+e ctrl+y: detail offset %d focus %q status %q; want 6, focus kept", d.Offset(), r.Focus, m.Status)
	}
	short, _, d := detailModel(t, 3)
	if r := short.Runtime; r.Available(Request{Noun: viewNoun, Verb: "scroll"}, r.Focus, r.Mode) {
		t.Fatal("view.scroll is available with nothing to scroll")
	}
	short.handleKey(MacroKey{Key: "ctrl+e"})
	if d.Offset() != 0 {
		t.Fatalf("detail that fits scrolled to %d", d.Offset())
	}
}

// ctrl+y left normal mode's yank history for g y; the yank history opens
// on g y from a list as from an editor (its g_pending y).
func TestYankHistoryOpensOnGY(t *testing.T) {
	r, m, _, _ := motionPage(t)
	r.Registers().History = []Yank{{Text: "x", Shape: "char"}}
	motionKeys(t, r, m, "g y")
	if r.TopModal() != yankHistoryID {
		t.Fatalf("g y on a list: top modal %q", r.TopModal())
	}
	motionKeys(t, r, m, "esc")
	motionKeys(t, r, m, "j g g")
	if r.Focus != "a1" || r.TopModal() != "" {
		t.Fatalf("g g after g y: focus %q modal %q", r.Focus, r.TopModal())
	}
}

// The outline takes Vim's >> and << on its indent and outdent, beside tab
// and shift+tab (the bare > < = no longer resize panes).
func TestOutlineShiftsWithVimOperators(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 80, 24
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return r.Tree("t").Block(w, h) }); err != nil {
		t.Fatal(err)
	}
	tree, err := r.AddTree("t", []TreeNode{{ID: "a", Label: "alpha"}, {ID: "c", Label: "gamma"}}, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	tree.OnChange = func(TreeChange) error { return nil }
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	if err := tree.Focus(); err != nil {
		t.Fatal(err)
	}
	_ = m.View()
	parent := func(id string) string {
		for _, n := range tree.Nodes() {
			if n.ID == id {
				return n.Parent
			}
		}
		return "?"
	}
	motionKeys(t, r, m, "j > >")
	if parent("c") != "a" || r.Mode.Name != "normal" {
		t.Fatalf(">>: c's parent %q, mode %q", parent("c"), r.Mode.Name)
	}
	motionKeys(t, r, m, "< <")
	if parent("c") != "" {
		t.Fatalf("<<: c's parent %q", parent("c"))
	}
}

// Editors keep their g commands with a count beside the app's g y: the
// row stands aside in an input (no-input), so 3gg is not held as a
// sequence that would lose its count.
func TestEditorCountedGGBesideGY(t *testing.T) {
	r, in := editorTestRuntime(t, "one\ntwo\nthree\nfour", true)
	m := NewModel(r)
	modelEditorKeys(t, m, "esc", "G", "2", "g", "g")
	if in.Cursor != 4 {
		t.Fatalf("2gg: cursor %d, want 4 (the second line)", in.Cursor)
	}
	modelEditorKeys(t, m, "g", "g")
	if in.Cursor != 0 {
		t.Fatalf("gg: cursor %d", in.Cursor)
	}
}

// q: opens the command line on the last palette command, Vim's
// command-line window (:help q:): the exact : row of macro_pending beats
// its any row (a register), so q a still records into a.
func TestQColonOpensThePaletteOnItsHistory(t *testing.T) {
	r, p, _ := lineRuntime(t)
	paletteLine(t, r, "save")
	if p.IsOpen() {
		t.Fatal("palette still open")
	}
	for _, key := range []string{"q", ":"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if !p.IsOpen() || p.Text() != "save sheet" || r.Registers().Macro.Recording != "" {
		t.Fatalf("q: open %v line %q recording %q", p.IsOpen(), p.Text(), r.Registers().Macro.Recording)
	}
	paletteKeys(t, p, "esc", "esc")
	for _, key := range []string{"q", "a"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if p.IsOpen() || r.Registers().Macro.Recording != "a" {
		t.Fatalf("q a: palette %v recording %q", p.IsOpen(), r.Registers().Macro.Recording)
	}
}
