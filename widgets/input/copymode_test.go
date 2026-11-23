package input

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

func TestCopyModeNavigationAndReadOnly(t *testing.T) {
	lines := []string{
		"\x1b[31mhello \x1b[1mworld\x1b[0m!",
		"\x1b[32msecond\x1b[0m line here",
		"third \x1b[34mblue\x1b[0m text",
	}
	cm := NewCopyMode(lines)
	if cm.Value() != "hello world!\nsecond line here\nthird blue text" {
		t.Fatalf("plain value = %q", cm.Value())
	}
	// Mutating keys and commands are refused cleanly without altering content.
	for _, k := range []string{"i", "a", "I", "A", "o", "O", "d", "c", "p", "P", "u", "ctrl+r", "."} {
		if cm.HandleKey(k) {
			t.Errorf("mutating key %q was accepted", k)
		}
	}
	for _, id := range []string{CmdInsert, CmdDelete, CmdChange, CmdPasteAfter, CmdPasteBefore, CmdUndo, CmdRedo, CmdRepeat, CmdKillWordBack} {
		if cm.HandleCommand(core.Command{ID: id, Args: map[string]any{"text": "X"}}) {
			t.Errorf("mutating command %q was accepted", id)
		}
	}
	if cm.Mode() != ModeNormal || cm.Value() != "hello world!\nsecond line here\nthird blue text" {
		t.Fatalf("copy mode mutated: mode=%s val=%q", cm.Mode(), cm.Value())
	}

	// Navigation and CursorLineCol.
	cm.HandleKey("j")
	cm.HandleKey("w")
	line, col := cm.CursorLineCol()
	if line != 1 || col != 7 {
		t.Fatalf("CursorLineCol = (%d, %d), want (1, 7)", line, col)
	}
	// Resolved motion command works.
	if !cm.HandleCommand(core.Command{ID: "motion.top"}) {
		t.Fatal("motion.top not handled")
	}
	if l, c := cm.CursorLineCol(); l != 0 || c != 0 {
		t.Fatalf("after motion.top: (%d, %d)", l, c)
	}
}

func TestCopyModeVisualYankStyledAndSearch(t *testing.T) {
	lines := []string{
		"\x1b[31mhello \x1b[1mworld\x1b[0m!",
		"\x1b[32msecond\x1b[0m line",
		"alpha beta",
	}
	cm := NewCopyMode(lines)

	// Charwise visual yank across style boundary on line 0: "lo wor" (cols 3..8 inclusive -> [3,9)).
	for _, k := range []string{"3", "l", "v", "5", "l", "y"} {
		if !cm.HandleKey(k) {
			t.Fatalf("unhandled key %q", k)
		}
	}
	if cm.YankPlain() != "lo wor" {
		t.Fatalf("YankPlain = %q, want %q", cm.YankPlain(), "lo wor")
	}
	styled := cm.YankStyled()
	if stripANSI(styled) != "lo wor" {
		t.Fatalf("stripANSI(YankStyled) = %q, want %q", stripANSI(styled), "lo wor")
	}
	if !strings.Contains(styled, "\x1b[31m") || !strings.Contains(styled, "\x1b[1m") || !strings.HasSuffix(styled, "\x1b[0m") {
		t.Fatalf("YankStyled lost SGR styling: %q", styled)
	}

	// Linewise visual yank across two lines preserves original ANSI lines.
	for _, k := range []string{"g", "g", "V", "j", "y"} {
		if !cm.HandleKey(k) {
			t.Fatalf("unhandled key %q", k)
		}
	}
	if cm.YankPlain() != "hello world!\nsecond line" {
		t.Fatalf("linewise YankPlain = %q", cm.YankPlain())
	}
	if cm.YankStyled() != lines[0]+"\n"+lines[1] {
		t.Fatalf("linewise YankStyled = %q, want %q", cm.YankStyled(), lines[0]+"\n"+lines[1])
	}

	// Interactive search / and repeat n / N.
	for _, k := range []string{"/", "b", "e", "t", "a", "enter"} {
		if !cm.HandleKey(k) {
			t.Fatalf("search key %q unhandled", k)
		}
	}
	if l, c := cm.CursorLineCol(); l != 2 || c != 6 {
		t.Fatalf("after /beta: cursor=(%d, %d), want (2, 6)", l, c)
	}

	// View renders as KindRaw with cursor and ANSI styling intact.
	node := cm.View("copy")
	if node.Kind != ui.KindRaw || !node.Focusable || node.CacheKey == 0 {
		t.Fatalf("unexpected view node: %+v", node)
	}
	frame, _ := ui.Render(node, ui.DefaultDarkTheme(), "copy", 30, 5)
	if frame.Cursor.X != 6 || frame.Cursor.Y != 2 || !strings.Contains(frame.ANSI, "\x1b[31m") {
		t.Fatalf("rendered copy mode frame: cursor=%+v ansi=%q", frame.Cursor, frame.ANSI)
	}
}
