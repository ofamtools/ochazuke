package input

import (
	"testing"

	"ofam.dev/ochazuke/core"
)

func TestSurround(t *testing.T) {
	for _, tc := range []struct {
		name, initial, keys, want string
		multiline                 bool
	}{
		{"add inner word", "one two", `y s i w "`, `"one" two`, false},
		{"add closing paren", "one two", "w y s i w )", "one (two)", false},
		{"add around word trims blanks", "one two", "y s a w ]", "[one] two", false},
		{"add motion", "one two", "y s e `", "`one` two", false},
		{"add counted", "a b c", "y s 2 w >", "<a b> c", false},
		{"add line", "  one two", "y s s {", "  {one two}", false},
		{"add paragraph single line", "one two", "y s i p (", "(one two)", false},
		{"add paragraph linewise", "one\ntwo\n\nthree", "y s i p (", "(\none\ntwo\n)\n\nthree", true},
		{"delete quotes", `say "hi" now`, `5 l d s "`, "say hi now", false},
		{"delete nested pair", "f((x))", "3 l d s (", "f(x)", false},
		{"delete counted pair", "f((x))", "3 l 2 d s )", "f(x)", false},
		{"delete alias", "[x]", "l d s r", "x", false},
		{"change braces to brackets", "{a b}", "l c s { [", "[a b]", false},
		{"change quote to paren", "x 'y' z", "3 l c s ' b", "x (y) z", false},
		{"visual S", "abc def", "v e S *", "*abc* def", false},
		{"invalid delimiter cancels", "one", "y s i w q", "one", false},
		{"missing pair no-op", "one", "d s (", "one", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(tc.initial, tc.multiline)
			keys(t, m, tc.keys)
			if m.Value() != tc.want || m.Mode() != ModeNormal {
				t.Fatalf("got %q mode %s, want %q", m.Value(), m.Mode(), tc.want)
			}
		})
	}
}

func TestSurroundUndoRepeat(t *testing.T) {
	m := New("one two", false)
	keys(t, m, `y s i w " W .`)
	if m.Value() != `"one" "two"` {
		t.Fatalf("repeat ys: %q", m.Value())
	}
	keys(t, m, "u u")
	if m.Value() != "one two" {
		t.Fatalf("undo ys: %q", m.Value())
	}
	keys(t, m, "ctrl+r")
	if m.Value() != `"one" two` {
		t.Fatalf("redo ys: %q", m.Value())
	}
	keys(t, m, `0 c s " '`)
	if m.Value() != `'one' two` {
		t.Fatalf("cs: %q", m.Value())
	}
	keys(t, m, "u")
	if m.Value() != `"one" two` {
		t.Fatalf("undo cs: %q", m.Value())
	}
	keys(t, m, `d s " u`)
	if m.Value() != `"one" two` {
		t.Fatalf("undo ds: %q", m.Value())
	}

	// ds repeats at the new cursor.
	m = New(`(a) (b)`, false)
	keys(t, m, "d s ( w .")
	if m.Value() != "a b" {
		t.Fatalf("repeat ds: %q", m.Value())
	}
}

func TestSurroundCommands(t *testing.T) {
	m := New("one two", false)
	run := func(id string, args map[string]any) {
		t.Helper()
		if !m.HandleCommand(core.Command{ID: id, Args: args}) {
			t.Fatalf("unhandled %s", id)
		}
	}
	run(CmdSurroundAdd, map[string]any{"target": "iw", "char": "{"})
	run(CmdSurroundChange, map[string]any{"from": "}", "to": "<"})
	if m.Value() != "<one> two" {
		t.Fatalf("commands: %q", m.Value())
	}
	run(CmdSurroundDelete, map[string]any{"char": ">"})
	if m.Value() != "one two" {
		t.Fatalf("delete command: %q", m.Value())
	}
	if m.Surround("iw", 'x', 0) {
		t.Fatal("letter accepted as delimiter")
	}
	if o, c, ok := SurroundPair('B'); !ok || o != '{' || c != '}' {
		t.Fatalf("SurroundPair(B) = %q %q", o, c)
	}
}
