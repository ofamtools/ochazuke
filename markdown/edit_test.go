package markdown

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// typeText types s into the focus key by key (space and newline by name).
func typeText(t *testing.T, r *ochazuke.Runtime, s string) {
	t.Helper()
	for _, c := range s {
		k := string(c)
		switch c {
		case ' ':
			k = "space"
		case '\n':
			k = "enter"
		case '\t':
			k = "tab"
		}
		if _, err := r.Dispatch(ochazuke.MacroKey{Key: k}); err != nil {
			t.Fatalf("type %q: %v", k, err)
		}
	}
}

func mustPress(t *testing.T, r *ochazuke.Runtime, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, err := r.Dispatch(ochazuke.MacroKey{Key: k}); err != nil {
			t.Fatalf("press %q: %v", k, err)
		}
	}
}

func fileDoc(t *testing.T, text string, opts ...Option) (*ochazuke.Runtime, *Doc, string) {
	t.Helper()
	path := writeFile(t, text)
	r, d := newDoc(t, File(path), opts...)
	d.Now = func() time.Time { return time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC) }
	d.View(60, 0, ochazuke.Appearance{Theme: "mono"})
	return r, d, path
}

func cursorKind(d *Doc) string {
	e, _ := d.Cursor()
	if e < 0 {
		return ""
	}
	return d.Elements()[e].Kind
}

func TestElementsKindsAndNotes(t *testing.T) {
	src := "# T\n\npara\n\n- [ ] item\n  > **owner** 2026-10-27: one\n  > more\n  > **claude** 2026-10-27 ✓: two\n  > plain line\n- b\n  > just a quote\n1. n\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```\nc\n```\n\n---\n\n> q\n"
	var got []string
	for _, e := range Elements(src) {
		got = append(got, e.Kind)
	}
	want := "heading paragraph task note note bullet quote numbered table table code rule quote"
	if strings.Join(got, " ") != want {
		t.Fatalf("kinds %s\nwant  %s", strings.Join(got, " "), want)
	}
	notes := Notes(src)
	if len(notes) != 2 || notes[0].Author != "owner" || notes[0].Text != "one\nmore" || notes[1].Author != "claude" || !notes[1].Resolved || notes[1].Text != "two\nplain line" {
		t.Fatalf("notes %+v", notes)
	}
	if notes[0].ID != "1.1" || notes[1].ID != "1.2" || notes[0].Checked == nil || *notes[0].Checked {
		t.Fatalf("ids %+v", notes)
	}
}

// }/{ move by block, ]] by the same kind, ]h/]t/]x/]n by kind, a table
// counting as one for ]t; counts repeat.
func TestBlockMotions(t *testing.T) {
	src := "# One\n\npara\n\n- [ ] a\n  > **owner** 2026-10-27: note\n- [ ] b\n\n| x | y |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n\n## Two\n\n| p |\n|---|\n| q |\n"
	r, d, _ := fileDoc(t, src)
	mustPress(t, r, "}")
	if cursorKind(d) != KindParagraph {
		t.Fatalf("} → %s", cursorKind(d))
	}
	mustPress(t, r, "2", "}")
	if cursorKind(d) != KindNote {
		t.Fatalf("2} → %s", cursorKind(d))
	}
	mustPress(t, r, "{")
	if sel, _ := d.Selected(); sel.Text != "a" {
		t.Fatalf("{ → %+v", sel)
	}
	mustPress(t, r, "]", "]")
	if sel, _ := d.Selected(); sel.Text != "b" {
		t.Fatalf("]] → %+v", sel)
	}
	mustPress(t, r, "]", "t")
	e, _ := d.Cursor()
	if d.Elements()[e].Line != 9 {
		t.Fatalf("]t at line %d", d.Elements()[e].Line)
	}
	mustPress(t, r, "]", "t")
	if e, _ := d.Cursor(); d.Elements()[e].Line != 16 {
		t.Fatalf("second ]t at line %d", d.Elements()[e].Line)
	}
	mustPress(t, r, "[", "h")
	if e, _ := d.Cursor(); d.Elements()[e].Line != 14 {
		t.Fatalf("[h at line %d", d.Elements()[e].Line)
	}
	mustPress(t, r, "[", "n")
	if cursorKind(d) != KindNote {
		t.Fatalf("[n → %s", cursorKind(d))
	}
	if err := press(t, r, "[", "n"); err != nil || cursorKind(d) != KindNote {
		t.Fatalf("[n at the first note is quiet: %v", err)
	}
	mustPress(t, r, "g", "g")
	mustPress(t, r, "ctrl+f")
	if e, _ := d.Cursor(); e <= 0 {
		t.Fatal("ctrl+f did not move")
	}
}

// o continues each structure; text outside the new lines stays byte for
// byte; numbered lists renumber; esc writes.
func TestOpenContinuesStructure(t *testing.T) {
	cases := []struct {
		name, src, keys, typed, want string
	}{
		{"bullet", "- a\n- b\n", "o", "new", "- a\n- new\n- b\n"},
		{"checklist", "- [x] a\n- [ ] b\n", "o", "new", "- [x] a\n- [ ] new\n- [ ] b\n"},
		{"numbered", "1. a\n2. b\n3. c\n", "o", "new", "1. a\n2. new\n3. b\n4. c\n"},
		{"numbered above", "1. a\n2. b\n", "jO", "new", "1. a\n2. new\n3. b\n"},
		{"lazy numbers", "1. a\n1. b\n", "o", "new", "1. a\n1. new\n1. b\n"},
		{"nested", "- a\n  - b\n- c\n", "jo", "new", "- a\n  - b\n  - new\n- c\n"},
		{"after subtree", "- a\n  - b\n- c\n", "o", "new", "- a\n  - b\n- new\n- c\n"},
		{"table", "| a | b |\n|---|---|\n| 1 | 2 |\n", "jo", "3", "| a   | b   |\n| --- | --- |\n| 1   | 2   |\n| 3   |     |\n"},
		{"paragraph", "one\n\ntwo\n", "o", "mid", "one\n\nmid\n\ntwo\n"},
		{"paragraph above", "one\n\ntwo\n", "jO", "mid", "one\n\nmid\n\ntwo\n"},
		{"heading", "# H\ntext\n", "o", "p", "# H\n\np\n\ntext\n"},
		{"empty doc", "", "o", "first", "first\n"},
		{"untouched o writes the marker", "- a\n", "o", "", "- a\n- \n"},
		{"untouched paragraph writes nothing", "one\n", "o", "", "one\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, d, path := fileDoc(t, c.src)
			for _, k := range c.keys {
				mustPress(t, r, string(k))
			}
			if !d.Editing() || r.Mode.Name != "insert" {
				t.Fatalf("not editing in insert: %v %s", d.Editing(), r.Mode.Name)
			}
			typeText(t, r, c.typed)
			mustPress(t, r, "esc")
			if got := readFile(t, path); got != c.want {
				t.Fatalf("got\n%q\nwant\n%q", got, c.want)
			}
			if d.Editing() || r.Mode.Name != "normal" {
				t.Fatal("esc did not leave the editor")
			}
			if c.typed != "" {
				mustPress(t, r, "u")
				if got := readFile(t, path); got != c.src {
					t.Fatalf("undo: %q", got)
				}
				mustPress(t, r, "ctrl+r")
				if got := readFile(t, path); got != c.want {
					t.Fatalf("redo: %q", got)
				}
			}
		})
	}
}

// i edits a block as its Markdown in place; only that block's bytes
// change, the awkward ones around it (CRLF, tabs, no final newline) stay.
func TestInsertEditsOnlyTheBlock(t *testing.T) {
	r, d, path := fileDoc(t, awkward)
	mustPress(t, r, "]", "x", "]", "x", "A")
	if got := string(r.Input(d.EditID()).Text); got != "- [X] two  " {
		t.Fatalf("buffer %q", got)
	}
	typeText(t, r, "more")
	view := ansi.Strip(d.View(60, 0, ochazuke.Appearance{Theme: "mono"}))
	if !strings.Contains(view, "- [X] two  more") || !strings.Contains(view, "[ ] café 日本") {
		t.Fatalf("only the block is source:\n%s", view)
	}
	mustPress(t, r, "esc")
	want := strings.Replace(awkward, "- [X] two  \r\n", "- [X] two  more\r\n", 1)
	if got := readFile(t, path); got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	mustPress(t, r, "i")
	mustPress(t, r, "esc")
	if got := readFile(t, path); got != want {
		t.Fatalf("an untouched edit wrote: %q", got)
	}
}

// Typing is written after a pause (the debounce timer), not only on esc.
func TestTypingIsDebounced(t *testing.T) {
	r, d, path := fileDoc(t, "one\n")
	mustPress(t, r, "A")
	typeText(t, r, "!")
	if d.PendingCommand() == nil {
		t.Fatal("typing must set the pause timer")
	}
	msg, ok := d.DebounceTick()
	if !ok {
		t.Fatal("no tick waiting")
	}
	d.HandleMessage(msg)
	if got := readFile(t, path); got != "one!\n" {
		t.Fatalf("debounced write: %q", got)
	}
	if !d.Editing() {
		t.Fatal("a pause does not leave the block")
	}
}

// enter continues an item (the rest of the line goes with it), enter on
// an empty item ends the list, tab nests, shift+tab takes out, backspace
// on an empty marker leaves a continuation line.
func TestInsertModeListKeys(t *testing.T) {
	r, _, path := fileDoc(t, "- [ ] a\n\nafter\n")
	mustPress(t, r, "A")
	typeText(t, r, "\nb\n")
	mustPress(t, r, "tab")
	typeText(t, r, "c\n")
	mustPress(t, r, "shift+tab")
	typeText(t, r, "d\n\nend")
	mustPress(t, r, "esc")
	want := "- [ ] a\n- [ ] b\n  - [ ] c\n- [ ] d\n\nend\n\nafter\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	r, _, path = fileDoc(t, "- [ ] a\n")
	mustPress(t, r, "o", "backspace")
	typeText(t, r, "a note")
	mustPress(t, r, "esc")
	if got := readFile(t, path); got != "- [ ] a\n  a note\n" {
		t.Fatalf("continuation: %q", got)
	}
	r, _, path = fileDoc(t, "- one two\n")
	mustPress(t, r, "i", "right", "right", "right", "enter")
	mustPress(t, r, "esc")
	if got := readFile(t, path); got != "- one\n- two\n" {
		t.Fatalf("split: %q", got)
	}
}

// tab moves between table cells, past the last a new row; the pipes
// line up when the table is left.
func TestTableCells(t *testing.T) {
	r, _, path := fileDoc(t, "| a | b |\n|---|---|\n| 1 | 2 |\n")
	mustPress(t, r, "j", "o")
	typeText(t, r, "long")
	mustPress(t, r, "tab")
	typeText(t, r, "x")
	mustPress(t, r, "tab")
	typeText(t, r, "y")
	mustPress(t, r, "esc")
	want := "| a    | b   |\n| ---- | --- |\n| 1    | 2   |\n| long | x   |\n| y    |     |\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// / on an empty line chooses a block kind; a table asks for its columns.
func TestBlockChooser(t *testing.T) {
	r, _, path := fileDoc(t, "intro\n")
	mustPress(t, r, "o", "/")
	if r.TopModal() == "" {
		t.Fatal("/ did not open the chooser")
	}
	typeText(t, r, "checklist")
	mustPress(t, r, "enter")
	typeText(t, r, "task")
	mustPress(t, r, "esc")
	if got := readFile(t, path); got != "intro\n\n- [ ] task\n" {
		t.Fatalf("checklist: %q", got)
	}
	mustPress(t, r, "G", "o", "enter", "/")
	typeText(t, r, "table")
	mustPress(t, r, "enter")
	typeText(t, r, "3")
	mustPress(t, r, "enter")
	typeText(t, r, "h")
	mustPress(t, r, "esc")
	if got := readFile(t, path); !strings.Contains(got, "| h   |     |     |\n| --- | --- | --- |") {
		t.Fatalf("table: %q", got)
	}
	r, _, path = fileDoc(t, "intro\n")
	mustPress(t, r, "A")
	typeText(t, r, " a/b")
	mustPress(t, r, "esc")
	if got := readFile(t, path); got != "intro a/b\n" {
		t.Fatalf("/ in text types: %q", got)
	}
}

// dd deletes a line or an item with its subtree (a heading only its line),
// yanked; p puts it back below; u and ctrl+r undo and redo both.
func TestDeleteYankPaste(t *testing.T) {
	src := "# H\n\ntext\n\n- a\n  - a1\n- b\n"
	r, d, path := fileDoc(t, src)
	mustPress(t, r, "d", "d")
	if got := readFile(t, path); got != "text\n\n- a\n  - a1\n- b\n" {
		t.Fatalf("dd heading: %q", got)
	}
	mustPress(t, r, "u")
	if got := readFile(t, path); got != src {
		t.Fatalf("undo: %q", got)
	}
	mustPress(t, r, "]", "l", "d", "d")
	if got := readFile(t, path); got != "# H\n\ntext\n\n- b\n" {
		t.Fatalf("dd item: %q", got)
	}
	if sel := cursorKind(d); sel != KindBullet {
		t.Fatalf("cursor %s", sel)
	}
	mustPress(t, r, "p")
	if got := readFile(t, path); got != "# H\n\ntext\n\n- b\n- a\n  - a1\n" {
		t.Fatalf("p: %q", got)
	}
	mustPress(t, r, "y", "y", "P")
	if got := readFile(t, path); got != "# H\n\ntext\n\n- b\n- a\n  - a1\n- a\n  - a1\n" {
		t.Fatalf("yy P: %q", got)
	}
}

// Notes: n opens a signed note line under the item, ]n finds it, x
// resolves it, za folds it to a count of the open ones.
func TestNotes(t *testing.T) {
	r, d, path := fileDoc(t, "- [ ] Inbox page\n- [ ] other\n")
	mustPress(t, r, "n")
	if got := string(r.Input(d.EditID()).Text); got != "> **owner** 2026-10-27: " {
		t.Fatalf("prefill %q", got)
	}
	typeText(t, r, "start here")
	mustPress(t, r, "esc")
	want := "- [ ] Inbox page\n  > **owner** 2026-10-27: start here\n- [ ] other\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("got %q", got)
	}
	d.Author = "claude"
	mustPress(t, r, "n")
	typeText(t, r, "done")
	mustPress(t, r, "esc")
	want = "- [ ] Inbox page\n  > **owner** 2026-10-27: start here\n  > **claude** 2026-10-27: done\n- [ ] other\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("second note: %q", got)
	}
	mustPress(t, r, "n", "esc")
	if got := readFile(t, path); got != want {
		t.Fatalf("an empty note wrote: %q", got)
	}
	mono := ochazuke.Appearance{Theme: "mono"}
	view := ansi.Strip(d.View(60, 0, mono))
	if !strings.Contains(view, "╭") || !strings.Contains(view, "owner 2026-10-27  start here") {
		t.Fatalf("note box:\n%s", view)
	}
	mustPress(t, r, "g", "g", "]", "n")
	if cursorKind(d) != KindNote {
		t.Fatal("]n")
	}
	mustPress(t, r, "x")
	if got := readFile(t, path); !strings.Contains(got, "**owner** 2026-10-27 ✓: start here") {
		t.Fatalf("resolve: %q", got)
	}
	mustPress(t, r, "z", "a")
	view = ansi.Strip(d.View(60, 0, mono))
	if !strings.Contains(view, "· 1 note, 1 resolved") || strings.Contains(view, "start here") {
		t.Fatalf("folded:\n%s", view)
	}
	mustPress(t, r, "z", "a")
	if !strings.Contains(ansi.Strip(d.View(60, 0, mono)), "start here") {
		t.Fatal("unfold")
	}
}

// The note functions the CLI uses: add, reply, resolve, find, and the
// cap and cleaning of note text.
func TestNoteFunctions(t *testing.T) {
	src := "# Plan\n\n- [ ] Inbox page\n  - [ ] nested\n- [ ] Prompts\n"
	item, err := FindItem(src, "inbox")
	if err != nil {
		t.Fatal(err)
	}
	src, _, err = AddNote(src, item, "owner", "2026-10-27", "first\x07 line\nsecond")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Plan\n\n- [ ] Inbox page\n  > **owner** 2026-10-27: first line\n  > second\n  - [ ] nested\n- [ ] Prompts\n"
	if src != want {
		t.Fatalf("add:\n%q\nwant\n%q", src, want)
	}
	notes := Notes(src)
	if len(notes) != 1 || notes[0].Text != "first line\nsecond" {
		t.Fatalf("notes %+v", notes)
	}
	src, _, err = ReplyNote(src, notes[0].Element, "pane-1", "2026-10-27", "answer")
	if err != nil {
		t.Fatal(err)
	}
	notes = Notes(src)
	if len(notes) != 2 || notes[1].Author != "pane-1" || notes[1].ID != "1.2" {
		t.Fatalf("reply %+v", notes)
	}
	els := Elements(src)
	resolved, err := SetResolved(src, els[notes[0].Element], true)
	if err != nil || !strings.Contains(resolved, "**owner** 2026-10-27 ✓: first line") {
		t.Fatalf("resolve %q %v", resolved, err)
	}
	back, _ := SetResolved(resolved, Elements(resolved)[notes[0].Element], false)
	if back != src {
		t.Fatalf("unresolve %q", back)
	}
	if _, err := FindItem(src, "promts page"); err == nil || !strings.Contains(err.Error(), "Prompts") {
		t.Fatalf("close matches: %v", err)
	}
	if _, _, err := AddNote(src, item, "a", "", strings.Repeat("x", NoteMax+1)); err == nil {
		t.Fatal("a note over the cap must be refused")
	}
}

// An edit made on disk since the Doc read the file is seen first: the
// block is found again by its text, or the edit is refused.
func TestEditRefindsAfterExternalChange(t *testing.T) {
	r, d, path := fileDoc(t, "- a\n- b\n")
	mustPress(t, r, "j")
	if err := os.WriteFile(path, []byte("# New\n\n- a\n- b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustPress(t, r, "d", "d")
	if got := readFile(t, path); got != "# New\n\n- a\n" {
		t.Fatalf("refound: %q", got)
	}
	if err := os.WriteFile(path, []byte("- z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := press(t, r, "d", "d"); !errors.Is(err, ErrRefused) {
		t.Fatalf("gone: %v", err)
	}
	if d.Text() != "- z\n" {
		t.Fatal("the view must show the source as it is now")
	}
}

// A read-only Doc refuses every edit.
func TestReadOnlyRefusesEdits(t *testing.T) {
	r, _ := newDoc(t, Source{Load: func() (string, error) { return "- a\n", nil }})
	for _, keys := range [][]string{{"i"}, {"o"}, {"d", "d"}, {"n"}, {"p"}} {
		if err := press(t, r, keys...); err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("%v: %v", keys, err)
		}
	}
}

// Edit runs an application's change on the source as it is now, as one
// undo step.
func TestEditIsOneUndoStep(t *testing.T) {
	r, d, path := fileDoc(t, "- [ ] a\n  > **owner** 2026-10-27: hi\n")
	if err := d.Edit(func(text string) (string, int, error) {
		els := Elements(text)
		next, err := SetSent(text, els[1], true)
		return next, els[1].Start, err
	}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "- [ ] a\n  > **owner** 2026-10-27 · sent: hi\n" {
		t.Fatalf("sent: %q", got)
	}
	// Editing the note's text clears the marker.
	mustPress(t, r, "j", "A")
	typeText(t, r, "!")
	mustPress(t, r, "esc")
	if got := readFile(t, path); got != "- [ ] a\n  > **owner** 2026-10-27: hi!\n" {
		t.Fatalf("edited: %q", got)
	}
	mustPress(t, r, "u", "u")
	if got := readFile(t, path); got != "- [ ] a\n  > **owner** 2026-10-27: hi\n" {
		t.Fatalf("undo: %q", got)
	}
}
