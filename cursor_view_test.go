package ochazuke

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// The mask records reverse video (the selection) per terminal cell, not per
// rune or ANSI byte. The cursor is not painted; the block reports its cell.
func assertCursorViewCells(t *testing.T, block Block, wantText, wantMask string, wantCursor Pos) {
	t.Helper()
	view := block.Text
	if got := ansi.Strip(view); got != wantText {
		t.Fatalf("visible text = %q, want %q (rendered %q)", got, wantText, view)
	}
	if block.Cursor == nil || *block.Cursor != wantCursor {
		t.Fatalf("cursor = %+v, want %+v (rendered %q)", block.Cursor, wantCursor, view)
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
		t.Fatal("selection style leaks beyond the input")
	}
}

// cellIndex returns the cell column where text starts in a plain row, or -1.
func cellIndex(row, text string) int {
	i := strings.Index(row, text)
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(row[:i])
}

// cursorOnly reads a mask whose only marks are the cursor cell(s): it returns
// the blank selection mask and the cursor position.
func cursorOnly(mask string) (string, Pos) {
	var cursor Pos
	rows := strings.Split(mask, "\n")
	for y, row := range rows {
		if x := strings.IndexByte(row, '^'); x >= 0 {
			cursor = Pos{X: x, Y: y}
		}
		rows[y] = strings.ReplaceAll(row, "^", ".")
	}
	return strings.Join(rows, "\n"), cursor
}

func TestCursorViewKeepsTextPositions(t *testing.T) {
	for _, mode := range []string{"normal", "insert"} {
		for cursor, mask := range []string{"^....", ".^...", "..^..", "...^.", "....^"} {
			t.Run(fmt.Sprintf("%s/%d", mode, cursor), func(t *testing.T) {
				in := NewInput("field", "abcde", false)
				in.Mode, in.Cursor = mode, cursor
				selection, pos := cursorOnly(mask)
				assertCursorViewCells(t, InputBlock(in, 5, 1), "abcde", selection, pos)
			})
		}
	}
}

func TestCursorViewReportsGraphemeStart(t *testing.T) {
	for cursor, mask := range []string{
		"^......", ".^.....", ".^.....", "..^^...",
		"....^^.", "....^^.", "....^^.", "......^",
	} {
		t.Run(fmt.Sprint(cursor), func(t *testing.T) {
			in := NewInput("field", "Ae\u0301界👩‍💻Z", false)
			in.Cursor = cursor
			selection, pos := cursorOnly(mask)
			assertCursorViewCells(t, InputBlock(in, 7, 1), "Ae\u0301界👩‍💻Z", selection, pos)
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
			selection, pos := cursorOnly(tc.mask)
			assertCursorViewCells(t, InputBlock(in, tc.width, 1), tc.wantText, selection, pos)
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
			selection, pos := cursorOnly(tc.mask)
			assertCursorViewCells(t, InputBlock(in, tc.width, tc.height), tc.wantText, selection, pos)
		})
	}
}

// Selections are painted whole; the cursor cell inside them is reported, not toggled.
func TestCursorViewComposesWithSelection(t *testing.T) {
	for _, tc := range []struct {
		name, text, mode           string
		anchor, cursor, goalColumn int
		wantText, mask             string
		pos                        Pos
	}{
		{"char", "abcd", "visual", 0, 1, -1, "abcd", "^^..", Pos{1, 0}},
		{"reverse char", "abcd", "visual", 2, 0, -1, "abcd", "^^^.", Pos{0, 0}},
		{"line", "ab\ncd", "visual-line", 0, 1, -1, "ab \ncd", "^^^\n..", Pos{1, 0}},
		{"selected newline", "ab\ncd", "visual-line", 0, 2, -1, "ab \ncd", "^^^\n..", Pos{2, 0}},
		{"block", "abcd\nxyzz", "visual-block", 1, 7, -1, "abcd\nxyzz", ".^^.\n.^^.", Pos{2, 1}},
		{"block short line", "abcd\nx\nwxyz", "visual-block", 2, 6, 2, "abcd\nx  \nwxyz", "..^.\n..^\n....", Pos{2, 1}},
		{"block virtual end", "abcd\nx", "visual-block", 2, 6, 3, "abcd\nx   ", "..^^\n..^^", Pos{3, 1}},
		{"block partial wide cursor", "a界z\nabcd", "visual-block", 6, 1, 2, "a  z\nabcd", "..^.\n..^.", Pos{1, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := NewInput("field", tc.text, true)
			in.Mode, in.Anchor, in.Cursor, in.goalColumn = tc.mode, tc.anchor, tc.cursor, tc.goalColumn
			assertCursorViewCells(t, InputBlock(in, 4, 3), tc.wantText, tc.mask, tc.pos)
		})
	}
}

func TestCursorViewPreservesEditorState(t *testing.T) {
	in := NewInput("field", "abcd\nx", true)
	in.Mode, in.Anchor, in.Cursor, in.goalColumn = "visual-block", 2, 6, 3
	before := *in
	before.Text = append([]rune(nil), in.Text...)
	before.History = append([]EditNode(nil), in.History...)
	assertCursorViewCells(t, InputBlock(in, 4, 3), "abcd\nx   ", "..^^\n..^^", Pos{3, 1})
	assertCursorViewCells(t, InputBlock(in, 2, 1), "  ", "^^", Pos{1, 0})
	if !reflect.DeepEqual(*in, before) {
		t.Fatalf("rendering changed editor state: before=%+v after=%+v", before, *in)
	}
}

// The shell reports the focused input's cursor through page chrome and modal
// panels, shaped by mode, and no cursor when nothing is focused or a modal
// without an input covers the page.
func TestModelViewCursorFollowsFocusedInput(t *testing.T) {
	r := NewRuntime()
	page := pageTestRegister(t, r, "alpha")
	in := r.Input("alpha-input")
	page.Render = func(r *Runtime, width, height int) Block {
		if r.Context().InputID != in.ID {
			return Text("no field focused")
		}
		return InputBlock(in, width, height)
	}
	m := NewModel(r)
	if err := r.OpenPage("alpha"); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if view := m.View(); view.Cursor != nil {
		t.Fatalf("cursor %+v reported with no focused input", view.Cursor)
	}
	if err := r.SetFocus(in.ID); err != nil {
		t.Fatal(err)
	}
	in.Cursor = 3
	locate := func(view tea.View, text string, offset int) {
		t.Helper()
		if view.Cursor == nil {
			t.Fatalf("no cursor:\n%s", ansi.Strip(view.Content))
		}
		rows := strings.Split(ansi.Strip(view.Content), "\n")
		if view.Cursor.Y >= len(rows) {
			t.Fatalf("cursor row %d beyond %d rows", view.Cursor.Y, len(rows))
		}
		if cell := cellIndex(rows[view.Cursor.Y], text); cell < 0 || cell+offset != view.Cursor.X {
			t.Fatalf("cursor %+v is not %d cells into %q on row %q", view.Cursor.Position, offset, text, rows[view.Cursor.Y])
		}
	}
	for _, tc := range []struct {
		mode  string
		shape tea.CursorShape
		blink bool
	}{{"normal", tea.CursorBlock, false}, {"insert", tea.CursorBar, true}, {"visual", tea.CursorUnderline, false}, {"visual-line", tea.CursorUnderline, false}, {"visual-block", tea.CursorUnderline, false}} {
		in.SetMode(tc.mode)
		view := m.View()
		locate(view, "alpha", 3)
		if view.Cursor.Shape != tc.shape || view.Cursor.Blink != tc.blink {
			t.Fatalf("%s: shape %v blink %v, want %v %v", tc.mode, view.Cursor.Shape, view.Cursor.Blink, tc.shape, tc.blink)
		}
	}
	in.SetMode("normal")
	m.Sidebar = true
	locate(m.View(), "alpha", 3)
	m.Sidebar = false

	// A modal with its own input reports that input, not the page's.
	r.Instance("dialog")
	if err := r.AddInput(NewInput("modal-input", "xyz", false), Parent("dialog")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Verb: "push", Noun: "modal", Args: Arguments{"id": "draft", "input": "modal-input"}}); err != nil {
		t.Fatal(err)
	}
	r.Input("modal-input").Cursor = 2
	view := m.View()
	locate(view, "xyz", 2)
	if strings.Contains(strings.Split(ansi.Strip(view.Content), "\n")[view.Cursor.Y], "alpha") {
		t.Fatal("modal cursor landed on the page row")
	}
	// A modal without an input hides the cursor of everything beneath it.
	if err := r.Push("note", "app"); err != nil {
		t.Fatal(err)
	}
	m.ModalViews["note"] = ModalView{Title: "Note", Render: func(*Runtime, *Instance, int, int) Block { return Text("read me") }}
	if view := m.View(); view.Cursor != nil {
		t.Fatalf("cursor %+v reported under a modal without an input", view.Cursor)
	}
	if _, err := r.Execute(Request{Verb: "pop", Noun: "modal"}); err != nil {
		t.Fatal(err)
	}
	locate(m.View(), "xyz", 2)
	if _, err := r.Execute(Request{Verb: "pop", Noun: "modal"}); err != nil {
		t.Fatal(err)
	}
	locate(m.View(), "alpha", 3)
}

// The palette's command line reports its cursor too.
func TestModelViewCursorFollowsPaletteEditor(t *testing.T) {
	r, in := editorTestRuntime(t, "abc", false)
	m := NewModel(r)
	in.SetMode("normal")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	keytest.Press(m, ":")
	if !m.Palette.IsActive() {
		t.Fatal("palette did not open")
	}
	keytest.Press(m, "o")
	view := m.View()
	if view.Cursor == nil {
		t.Fatalf("palette line reported no cursor:\n%s", ansi.Strip(view.Content))
	}
	row := strings.Split(ansi.Strip(view.Content), "\n")[view.Cursor.Y]
	if cell := cellIndex(row, ": o"); cell < 0 || view.Cursor.X != cell+3 {
		t.Fatalf("cursor %+v is not after the line's text %q", view.Cursor.Position, row)
	}
}
