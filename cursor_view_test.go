package ochazuke

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// The mask records reverse video per terminal cell, not per rune or ANSI byte.
func assertCursorViewCells(t *testing.T, view, wantText, wantMask string) {
	t.Helper()
	if got := ansi.Strip(view); got != wantText {
		t.Fatalf("visible text = %q, want %q (rendered %q)", got, wantText, view)
	}
	var mask strings.Builder
	reversed := false
	for len(view) > 0 {
		switch {
		case strings.HasPrefix(view, "\x1b[7m"):
			reversed = true
			view = view[len("\x1b[7m"):]
		case strings.HasPrefix(view, "\x1b[27m"):
			reversed = false
			view = view[len("\x1b[27m"):]
		default:
			end := strings.IndexByte(view, '\x1b')
			if end == 0 {
				t.Fatalf("unexpected terminal sequence %q", view)
			}
			if end < 0 {
				end = len(view)
			}
			clusters := uniseg.NewGraphemes(view[:end])
			for clusters.Next() {
				text := clusters.Str()
				if text == "\n" {
					mask.WriteByte('\n')
					continue
				}
				cell := "."
				if reversed {
					cell = "^"
				}
				mask.WriteString(strings.Repeat(cell, ansi.StringWidth(text)))
			}
			view = view[end:]
		}
	}
	if got := mask.String(); got != wantMask {
		t.Fatalf("reverse-video cells = %q, want %q", got, wantMask)
	}
	if reversed {
		t.Fatal("cursor style leaks beyond the input")
	}
}

func TestCursorViewKeepsTextPositions(t *testing.T) {
	for _, mode := range []string{"normal", "insert"} {
		for cursor, mask := range []string{"^....", ".^...", "..^..", "...^.", "....^"} {
			t.Run(fmt.Sprintf("%s/%d", mode, cursor), func(t *testing.T) {
				in := NewInput("field", "abcde", false)
				in.Mode, in.Cursor = mode, cursor
				assertCursorViewCells(t, InputView(in, 5, 1), "abcde", mask)
			})
		}
	}
}

func TestCursorViewInvertsWholeGrapheme(t *testing.T) {
	for cursor, mask := range []string{
		"^......", ".^.....", ".^.....", "..^^...",
		"....^^.", "....^^.", "....^^.", "......^",
	} {
		t.Run(fmt.Sprint(cursor), func(t *testing.T) {
			in := NewInput("field", "Ae\u0301界👩‍💻Z", false)
			in.Cursor = cursor
			assertCursorViewCells(t, InputView(in, 7, 1), "Ae\u0301界👩‍💻Z", mask)
		})
	}
}

func TestCursorViewClipsByTerminalCell(t *testing.T) {
	for _, tc := range []struct {
		name, text     string
		cursor, width  int
		wantText, mask string
	}{
		{"wide cursor at right edge", "ab界cd", 2, 3, "b界", ".^^"},
		{"wide glyph clipped on left", "界abcd", 3, 4, " abc", "...^"},
		{"wide glyph clipped on right", "ab界cd", 1, 3, "ab ", ".^."},
		{"wide cursor in one cell", "ab界cd", 2, 1, " ", "^"},
		{"combining cursor at right edge", "abe\u0301cd", 3, 3, "abe\u0301", "..^"},
		{"combining glyph before cursor", "abe\u0301cd", 4, 3, "be\u0301c", "..^"},
		{"joined emoji cursor at right edge", "A👩‍💻Z", 3, 3, "A👩‍💻", ".^^"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := NewInput("field", tc.text, false)
			in.Cursor = tc.cursor
			assertCursorViewCells(t, InputView(in, tc.width, 1), tc.wantText, tc.mask)
		})
	}
}

func TestCursorViewEmptyAndLineEnds(t *testing.T) {
	for _, tc := range []struct {
		name, text            string
		cursor, width, height int
		wantText, mask        string
	}{
		{"empty", "", 0, 4, 2, " ", "^"},
		{"before newline", "ab\ncd", 2, 5, 2, "ab \ncd", "..^\n.."},
		{"after trailing newline", "ab\n", 3, 5, 2, "ab\n ", "..\n^"},
		{"empty middle line", "a\n\nz", 2, 5, 3, "a\n \nz", ".\n^\n."},
		{"full line end", "abc", 3, 3, 1, "bc ", "..^"},
		{"vertical cursor following", "abc\ndef\nghi", 9, 3, 2, "def\nghi", "...\n.^."},
		{"end after wide glyph", "界", 1, 2, 1, "  ", ".^"},
		{"negative cursor", "ab", -1, 2, 1, "ab", "^."},
		{"cursor beyond end", "ab", 90, 2, 1, "b ", ".^"},
		{"minimum dimensions", "ab", 1, 0, 0, "b", "^"},
		{"sanitized text", "a\tb\x1bc", 2, 5, 1, "a b�c", "..^.."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := NewInput("field", tc.text, true)
			in.Cursor = tc.cursor
			assertCursorViewCells(t, InputView(in, tc.width, tc.height), tc.wantText, tc.mask)
		})
	}
}

func TestCursorViewComposesWithSelection(t *testing.T) {
	for _, tc := range []struct {
		name, text, mode           string
		anchor, cursor, goalColumn int
		wantText, mask             string
	}{
		{"char", "abcd", "visual", 0, 1, -1, "abcd", "^..."},
		{"reverse char", "abcd", "visual", 2, 0, -1, "abcd", ".^^."},
		{"line", "ab\ncd", "visual-line", 0, 1, -1, "ab \ncd", "^.^\n.."},
		{"selected newline", "ab\ncd", "visual-line", 0, 2, -1, "ab \ncd", "^^.\n.."},
		{"block", "abcd\nxyzz", "visual-block", 1, 7, -1, "abcd\nxyzz", ".^^.\n.^.."},
		{"block short line", "abcd\nx\nwxyz", "visual-block", 2, 6, 2, "abcd\nx  \nwxyz", "..^.\n...\n...."},
		{"block virtual end", "abcd\nx", "visual-block", 2, 6, 3, "abcd\nx   ", "..^^\n..^."},
		{"block partial wide cursor", "a界z\nabcd", "visual-block", 6, 1, 2, "a  z\nabcd", ".^..\n..^."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := NewInput("field", tc.text, true)
			in.Mode, in.Anchor, in.Cursor, in.goalColumn = tc.mode, tc.anchor, tc.cursor, tc.goalColumn
			assertCursorViewCells(t, InputView(in, 4, 3), tc.wantText, tc.mask)
		})
	}
}

func TestCursorViewPreservesEditorState(t *testing.T) {
	in := NewInput("field", "abcd\nx", true)
	in.Mode, in.Anchor, in.Cursor, in.goalColumn = "visual-block", 2, 6, 3
	in.Pending, in.Count, in.operatorCount = "g", 3, 2
	before := *in
	before.Text = append([]rune(nil), in.Text...)
	before.History = append([]EditNode(nil), in.History...)
	assertCursorViewCells(t, InputView(in, 4, 3), "abcd\nx   ", "..^^\n..^.")
	assertCursorViewCells(t, InputView(in, 2, 1), "  ", "^.")
	if !reflect.DeepEqual(*in, before) {
		t.Fatalf("rendering changed editor state: before=%+v after=%+v", before, *in)
	}
}
