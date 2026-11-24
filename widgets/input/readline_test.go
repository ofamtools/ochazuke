package input

import (
	"testing"

	"ofam.dev/ochazuke/core"
)

func TestReadlineLineEditing(t *testing.T) {
	l := NewLine("alpha beta gamma")
	if l.Cursor != 16 {
		t.Fatalf("initial cursor = %d, want 16", l.Cursor)
	}
	cmd := func(id string) {
		_, ok := l.HandleCommand(core.Command{ID: id})
		if !ok {
			t.Fatalf("unhandled command %q", id)
		}
	}

	// ctrl+a / ctrl+e / ctrl+f / ctrl+b
	cmd(CmdLineStart)
	if l.Cursor != 0 {
		t.Fatalf("line.start cursor = %d", l.Cursor)
	}
	cmd(CmdCharRight)
	cmd(CmdCharRight)
	cmd(CmdCharLeft)
	if l.Cursor != 1 {
		t.Fatalf("char motions cursor = %d, want 1", l.Cursor)
	}
	cmd(CmdLineEnd)
	if l.Cursor != 16 {
		t.Fatalf("line.end cursor = %d, want 16", l.Cursor)
	}

	// alt+b / ctrl+w / ctrl+y
	cmd(CmdWordLeft)
	if l.Cursor != 11 {
		t.Fatalf("word.left cursor = %d, want 11", l.Cursor)
	}
	cmd(CmdKillWordBack)
	if l.Value() != "alpha gamma" || l.Cursor != 6 || l.Kill != "beta " {
		t.Fatalf("after kill.word.back: val=%q cursor=%d kill=%q", l.Value(), l.Cursor, l.Kill)
	}
	cmd(CmdLineEnd)
	l.Insert(" ")
	cmd(CmdYankKill)
	if l.Value() != "alpha gamma beta " {
		t.Fatalf("after yank.kill: val=%q", l.Value())
	}

	// ctrl+k kills to end, ctrl+u kills to start
	cmd(CmdLineStart)
	cmd(CmdWordRight)
	cmd(CmdKillLineForward)
	if l.Value() != "alpha" || l.Kill != " gamma beta " {
		t.Fatalf("after kill.line.forward: val=%q kill=%q", l.Value(), l.Kill)
	}
	cmd(CmdYankKill)
	cmd(CmdKillLineBack)
	if l.Value() != "" || l.Cursor != 0 {
		t.Fatalf("after kill.line.back: val=%q cursor=%d", l.Value(), l.Cursor)
	}

	// alt+d kills word forward; ctrl+d deletes char under cursor; ctrl+t transposes
	l.Set("one two three")
	cmd(CmdLineStart)
	cmd(CmdKillWordForward)
	if l.Value() != " two three" || l.Kill != "one" {
		t.Fatalf("after kill.word.forward: val=%q kill=%q", l.Value(), l.Kill)
	}
	cmd(CmdDeleteChar)
	if l.Value() != "two three" {
		t.Fatalf("after delete.char: val=%q", l.Value())
	}
	cmd(CmdCharRight)
	cmd(CmdCharRight)
	cmd(CmdTranspose)
	if l.Value() != "tow three" {
		t.Fatalf("after transpose: val=%q", l.Value())
	}
}

func TestReadlineOnInputModelInsertMode(t *testing.T) {
	m := New("first line\nsecond word", true)
	m.HandleKey("j")
	m.HandleKey("A") // insert mode at end of line 2
	if m.Mode() != ModeInsert {
		t.Fatalf("mode = %q", m.Mode())
	}
	// ctrl+w kills "word" on line 2 only
	if !m.HandleCommand(core.Command{ID: CmdKillWordBack}) {
		t.Fatal("CmdKillWordBack not handled in insert mode")
	}
	if m.Value() != "first line\nsecond " {
		t.Fatalf("after ctrl+w: %q", m.Value())
	}
	// ctrl+a jumps to start of line 2 (not line 1!)
	m.HandleKey("ctrl+a")
	if m.Cursor() != len("first line\n") {
		t.Fatalf("ctrl+a cursor = %d, want %d", m.Cursor(), len("first line\n"))
	}
	// ctrl+y yanks killed "word" at start of line 2
	m.HandleKey("ctrl+y")
	if m.Value() != "first line\nwordsecond " {
		t.Fatalf("after ctrl+y: %q", m.Value())
	}
	// esc -> u undoes the entire insert session cleanly
	m.HandleKey("esc")
	m.HandleKey("u")
	if m.Value() != "first line\nsecond word" {
		t.Fatalf("after undo: %q", m.Value())
	}
}

func TestReadlineSpecsAndBindings(t *testing.T) {
	specs := ReadlineSpecs()
	ids := ReadlineCommandIDs()
	if len(specs) != len(ids) {
		t.Fatalf("len(specs)=%d != len(ids)=%d", len(specs), len(ids))
	}
	bindings := ReadlineBindings("insert", "input")
	if len(bindings) < len(ids) {
		t.Fatalf("expected at least %d default bindings, got %d", len(ids), len(bindings))
	}
	for _, b := range bindings {
		if b.Mode != "insert" || b.Scope != "input" || !IsReadlineCommand(b.Command) {
			t.Fatalf("unexpected binding %+v", b)
		}
	}
}

func TestReadlineLineMultilineEditing(t *testing.T) {
	l := NewLine("first")
	if changed, handled := l.HandleCommand(core.Command{ID: CmdInsert, Args: map[string]any{"text": "\n"}}); !changed || !handled {
		t.Fatalf("CmdInsert newline = (%v, %v)", changed, handled)
	}
	l.HandleCommand(core.Command{ID: CmdInsert, Args: map[string]any{"text": "second"}})
	if l.Value() != "first\nsecond" || l.Cursor != 12 {
		t.Fatalf("after insert: val=%q cursor=%d", l.Value(), l.Cursor)
	}
	// CmdNewline inserts at cursor.
	l.HandleCommand(core.Command{ID: CmdLineStart})
	if l.Cursor != 6 {
		t.Fatalf("line.start on line 2 = %d, want 6", l.Cursor)
	}
	l.HandleCommand(core.Command{ID: CmdLineEnd})
	if l.Cursor != 12 {
		t.Fatalf("line.end on line 2 = %d, want 12", l.Cursor)
	}
	l.HandleCommand(core.Command{ID: CmdLineStart})
	// CharLeft crosses newline to end of line 1; CharRight crosses back.
	l.HandleCommand(core.Command{ID: CmdCharLeft})
	if l.Cursor != 5 {
		t.Fatalf("char.left across newline = %d, want 5", l.Cursor)
	}
	l.HandleCommand(core.Command{ID: CmdCharRight})
	if l.Cursor != 6 {
		t.Fatalf("char.right across newline = %d, want 6", l.Cursor)
	}
	// Backspace removes the newline and joins lines.
	l.HandleCommand(core.Command{ID: CmdBackspace})
	if l.Value() != "firstsecond" || l.Cursor != 5 {
		t.Fatalf("after backspace across newline: val=%q cursor=%d", l.Value(), l.Cursor)
	}
	// CmdNewline re-splits at cursor.
	if changed, handled := l.HandleCommand(core.Command{ID: CmdNewline}); !changed || !handled {
		t.Fatalf("CmdNewline = (%v, %v)", changed, handled)
	}
	if l.Value() != "first\nsecond" || l.Cursor != 6 {
		t.Fatalf("after CmdNewline: val=%q cursor=%d", l.Value(), l.Cursor)
	}
}
