package markdown

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

// editorRows draws d at width and returns its lines (trailing blanks
// cut) and the terminal cursor.
func editorRows(t *testing.T, d *Doc, width int) ([]string, ochazuke.Pos) {
	t.Helper()
	b := d.Block(width, 0, ochazuke.Appearance{Theme: "mono"})
	lines := strings.Split(ansi.Strip(b.Text), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	if b.Cursor == nil {
		t.Fatal("no cursor while editing")
	}
	return lines, *b.Cursor
}

func sameRows(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rows %q\nwant %q", got, want)
	}
}

// Typing past the edge wraps the block editor's line at the last space
// that fits; the file gets no line break.
func TestInsertWrapsAtTheLastSpace(t *testing.T) {
	r, d, path := fileDoc(t, "one\n")
	mustPress(t, r, "A")
	typeText(t, r, " two three")
	// The paragraph is drawn 2 cells in: the editor has 10.
	lines, at := editorRows(t, d, 12)
	sameRows(t, lines, "  one two", "  three")
	if at != (ochazuke.Pos{X: 7, Y: 1}) {
		t.Fatalf("cursor %+v", at)
	}
	typeText(t, r, " four")
	lines, at = editorRows(t, d, 12)
	// A row filled to the edge: the cursor's cell starts the next row.
	sameRows(t, lines, "  one two", "  three four", "")
	if at != (ochazuke.Pos{X: 2, Y: 2}) {
		t.Fatalf("cursor %+v", at)
	}
	// Moving the cursor back onto the first row places it there.
	mustPress(t, r, "home")
	if _, at = editorRows(t, d, 12); at != (ochazuke.Pos{X: 2, Y: 0}) {
		t.Fatalf("cursor at home %+v", at)
	}
	// A wider pane (a resize) draws it on one row again.
	lines, _ = editorRows(t, d, 40)
	sameRows(t, lines, "  one two three four")
	mustPress(t, r, "esc")
	if got := readFile(t, path); got != "one two three four\n" {
		t.Fatalf("file %q", got)
	}
}

// A word wider than the editor breaks at its edge.
func TestInsertWrapBreaksALongWord(t *testing.T) {
	r, d, path := fileDoc(t, "x\n")
	mustPress(t, r, "A")
	typeText(t, r, "abcdefghijklmnopqrstuvwxy")
	lines, at := editorRows(t, d, 12)
	sameRows(t, lines, "  xabcdefghi", "  jklmnopqrs", "  tuvwxy")
	if at != (ochazuke.Pos{X: 8, Y: 2}) {
		t.Fatalf("cursor %+v", at)
	}
	mustPress(t, r, "esc")
	if got := readFile(t, path); got != "xabcdefghijklmnopqrstuvwxy\n" {
		t.Fatalf("file %q", got)
	}
}

// A list item's continuation rows line up with its text, after the marker.
func TestInsertWrapIndentsListItems(t *testing.T) {
	r, d, path := fileDoc(t, "- one\n")
	mustPress(t, r, "A")
	typeText(t, r, " two three four")
	lines, at := editorRows(t, d, 16)
	sameRows(t, lines, "  - one two", "    three four")
	if at != (ochazuke.Pos{X: 14, Y: 1}) {
		t.Fatalf("cursor %+v", at)
	}
	mustPress(t, r, "esc")
	if got := readFile(t, path); got != "- one two three four\n" {
		t.Fatalf("file %q", got)
	}
}

// markdown.wrap false keeps the old sideways scroll.
func TestInsertWrapSettingOff(t *testing.T) {
	r, d, _ := fileDoc(t, "one\n")
	if err := r.Settings().Set(WrapSetting, false); err != nil {
		t.Fatal(err)
	}
	mustPress(t, r, "A")
	typeText(t, r, " two three four")
	lines, at := editorRows(t, d, 12)
	if len(lines) != 1 || at.Y != 0 {
		t.Fatalf("wrapped with %s off: %q %+v", WrapSetting, lines, at)
	}
}
