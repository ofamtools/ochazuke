package ochazuke

import (
	"strings"
	"testing"
)

// wrapped draws in at width with Wrap set, every row shown, and returns
// the rows (trailing blanks cut) and the cursor's cell.
func wrapped(t *testing.T, in *Input, width int) ([]string, Pos) {
	t.Helper()
	in.Wrap = true
	b := InputBlock(in, width, InputRows(in, width))
	rows := strings.Split(b.Text, "\n")
	for i, row := range rows {
		rows[i] = strings.TrimRight(row, " ")
	}
	if b.Cursor == nil {
		t.Fatal("no cursor")
	}
	return rows, *b.Cursor
}

func TestInputWrapBreaksAtTheLastSpace(t *testing.T) {
	for _, tc := range []struct {
		name   string
		text   string
		cursor int
		width  int
		rows   []string
		at     Pos
	}{
		{"fits", "one two", 7, 10, []string{"one two"}, Pos{X: 7}},
		{"typing past the edge", "one two three", 13, 10, []string{"one two", "three"}, Pos{X: 5, Y: 1}},
		{"cursor mid word on the wrapped row", "one two three", 9, 10, []string{"one two", "three"}, Pos{X: 1, Y: 1}},
		{"cursor before the break", "one two three", 2, 10, []string{"one two", "three"}, Pos{X: 2}},
		{"space at the edge hangs", "one two ab", 8, 7, []string{"one two", "ab"}, Pos{Y: 1}},
		{"cursor on a hanging space", "one two ab", 7, 7, []string{"one two", "ab"}, Pos{X: 6}},
		{"long word breaks hard", "abcdefghijkl", 12, 5, []string{"abcde", "fghij", "kl"}, Pos{X: 2, Y: 2}},
		{"full row with the cursor at its end", "abcde", 5, 5, []string{"abcde", ""}, Pos{Y: 1}},
		{"full row, cursor inside", "abcde", 1, 5, []string{"abcde"}, Pos{X: 1}},
		{"second line", "short\none two three four", 24, 10, []string{"short", "one two", "three four", ""}, Pos{Y: 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := NewInput("field", tc.text, true)
			in.Cursor = tc.cursor
			rows, at := wrapped(t, in, tc.width)
			if strings.Join(rows, "|") != strings.Join(tc.rows, "|") {
				t.Fatalf("rows %q, want %q", rows, tc.rows)
			}
			if at != tc.at {
				t.Fatalf("cursor %+v, want %+v", at, tc.at)
			}
			if string(in.Text) != tc.text {
				t.Fatalf("text changed to %q", string(in.Text))
			}
		})
	}
}

func TestInputWrapIndentsContinuationRows(t *testing.T) {
	in := NewInput("field", "- alpha beta gamma delta", true)
	in.Cursor = len(in.Text)
	in.WrapIndent = func(line string) int { return 2 }
	rows, at := wrapped(t, in, 12)
	want := []string{"- alpha beta", "  gamma", "  delta"}
	if strings.Join(rows, "|") != strings.Join(want, "|") {
		t.Fatalf("rows %q, want %q", rows, want)
	}
	if at != (Pos{X: 7, Y: 2}) {
		t.Fatalf("cursor %+v", at)
	}

	// Leading spaces indent by default ('breakindent').
	in = NewInput("field", "    aaa bbb ccc", true)
	in.Cursor = 0
	rows, _ = wrapped(t, in, 12)
	if want := []string{"    aaa bbb", "    ccc"}; strings.Join(rows, "|") != strings.Join(want, "|") {
		t.Fatalf("rows %q, want %q", rows, want)
	}
}

func TestInputWrapOffScrollsSideways(t *testing.T) {
	in := NewInput("field", "one two three", true)
	in.Cursor = 13
	b := InputBlock(in, 10, 1)
	if strings.Contains(b.Text, "\n") || InputRows(in, 10) != 1 {
		t.Fatalf("wrapped without Wrap: %q", b.Text)
	}
	if b.Cursor == nil || b.Cursor.X != 9 {
		t.Fatalf("cursor %+v", b.Cursor)
	}
}

func TestInputWrapRowsFollowWidth(t *testing.T) {
	in := NewInput("field", "one two three four five", true)
	in.Wrap = true
	in.Cursor = 0
	if got := InputRows(in, 40); got != 1 {
		t.Fatalf("rows at 40: %d", got)
	}
	if got := InputRows(in, 10); got != 3 {
		t.Fatalf("rows at 10: %d", got)
	}
}
