package ui

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/buffer"
)

func TestRenderMultilineInputSoftWrapAndCursor(t *testing.T) {
	// Width 12 -> avail = 10 columns for wrapped text.
	// "alpha beta gamma delta" wraps to:
	//   line 0: "alpha beta" (runes 0..10)
	//   line 1: "gamma"      (runes 11..16)
	//   line 2: "delta"      (runes 17..22)
	val := "alpha beta gamma delta"
	node := ViewNode{
		ID:        "ed",
		Kind:      KindInput,
		Focusable: true,
		Props: map[string]any{
			"multiline": true,
			"value":     val,
			"cursor":    14, // 'm' in "gamma" -> line 1, col 3 -> X = 1+3 = 4
		},
	}
	rows, f, _ := draw(node, "ed", 12, 4)
	wantRows(t, rows, " alpha beta", " gamma", " delta", "")
	if want := (buffer.Cursor{X: 4, Y: 1, Shape: buffer.CursorBar}); f.Cursor != want {
		t.Fatalf("wrapped cursor = %+v, want %+v", f.Cursor, want)
	}

	// Cursor at end of text lands on line 2, col 5 -> X = 1+5 = 6.
	node.Props["cursor"] = len([]rune(val))
	_, fEnd, _ := draw(node, "ed", 12, 4)
	if want := (buffer.Cursor{X: 6, Y: 2, Shape: buffer.CursorBar}); fEnd.Cursor != want {
		t.Fatalf("end cursor = %+v, want %+v", fEnd.Cursor, want)
	}
}

func TestRenderMultilineInputWrapVerticalScroll(t *testing.T) {
	// Width 10 -> avail = 8. Four words wrap to 4 lines; viewport height is 2.
	val := "one1111 two2222 three33 four444"
	node := ViewNode{
		ID:        "ed",
		Kind:      KindInput,
		Focusable: true,
		Props: map[string]any{
			"multiline": true,
			"value":     val,
			"cursor":    len([]rune(val)), // last wrapped line (index 3)
		},
	}
	rows, f, _ := draw(node, "ed", 10, 2)
	wantRows(t, rows, " three33", " four444")
	if want := (buffer.Cursor{X: 8, Y: 1, Shape: buffer.CursorBar}); f.Cursor != want {
		t.Fatalf("scrolled cursor = %+v, want %+v", f.Cursor, want)
	}
}

func TestRenderMultilineInputPlaceholderWrap(t *testing.T) {
	node := ViewNode{
		ID:        "ed",
		Kind:      KindInput,
		Focusable: true,
		Props: map[string]any{
			"multiline":   true,
			"value":       "",
			"placeholder": "type a long note here",
		},
	}
	// Width 12 -> avail = 10 -> "type a" / "long note" / "here"
	rows, f, _ := draw(node, "ed", 12, 4)
	wantRows(t, rows, " type a", " long note", " here", "")
	if want := (buffer.Cursor{X: 1, Y: 0, Shape: buffer.CursorBar}); f.Cursor != want {
		t.Fatalf("placeholder cursor = %+v, want %+v", f.Cursor, want)
	}
}

func TestRenderMultilineInputWrapOptOut(t *testing.T) {
	val := "alpha beta gamma delta\nsecond"
	node := ViewNode{
		ID:        "ed",
		Kind:      KindInput,
		Focusable: true,
		Props: map[string]any{
			"multiline": true,
			"wrap":      false,
			"value":     val,
			"cursor":    len([]rune("alpha beta gamma delta\nsec")),
		},
	}
	rows, f, _ := draw(node, "ed", 12, 3)
	// First line is NOT soft-wrapped (clips to width 12); second line is on row 1.
	if !strings.HasPrefix(rows[0], " alpha beta") || rows[1] != " second" {
		t.Fatalf("wrap:false should preserve unwrapped lines, got:\n%s", strings.Join(rows, "\n"))
	}
	if want := (buffer.Cursor{X: 4, Y: 1, Shape: buffer.CursorBar}); f.Cursor != want {
		t.Fatalf("wrap:false cursor = %+v, want %+v", f.Cursor, want)
	}
}
