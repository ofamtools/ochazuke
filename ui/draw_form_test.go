package ui

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/buffer"
)

func TestInputEditAndBorderWarnTokens(t *testing.T) {
	for _, p := range ThemePresets() {
		editTS := p.Tone("input.edit")
		focusTS := p.Tone("input.focus")
		plainTS := p.Tone("input")
		if editTS.Fg == "" || editTS.Bg == "" || !editTS.Bold {
			t.Errorf("%s: input.edit incomplete: %+v", p.Name, editTS)
		}
		if editTS == focusTS || editTS == plainTS {
			t.Errorf("%s: input.edit (%+v) must differ from input (%+v) and input.focus (%+v)", p.Name, editTS, plainTS, focusTS)
		}
		warnTS := p.Tone("border.warn")
		if warnTS.Fg == "" || !warnTS.Bold {
			t.Errorf("%s: border.warn incomplete: %+v", p.Name, warnTS)
		}
	}
}

func TestFormInsertEditStylingVsNormalFocus(t *testing.T) {
	th := DefaultDarkTheme()
	editBg := sgrBg(th, "input.edit")
	focusBg := sgrBg(th, "input.focus")

	makeForm := func(editing bool, cursor int, includeCursor bool) ViewNode {
		props := map[string]any{
			"fields": []any{
				map[string]any{"name": "title", "type": "string"},
				map[string]any{"name": "notes", "type": "string"},
			},
			"values":  map[string]any{"title": "hello", "notes": "world"},
			"active":  0,
			"editing": editing,
		}
		if includeCursor {
			props["cursor"] = cursor
		}
		return ViewNode{Kind: KindForm, ID: "f", Focusable: true, Props: props}
	}

	// Normal mode (editing: false, no cursor): calm input.focus bg, hidden cursor.
	_, fNorm, _ := drawTheme(makeForm(false, 0, false), withDensity(DensityCompact), "f", 30, 4)
	normRow0 := strings.Split(fNorm.ANSI, "\n")[0]
	if !strings.Contains(normRow0, focusBg) || strings.Contains(normRow0, editBg) {
		t.Errorf("normal mode active field should use input.focus, not input.edit: %q", normRow0)
	}
	if fNorm.Cursor.Shape != buffer.CursorHidden {
		t.Errorf("normal mode should hide cursor, got %+v", fNorm.Cursor)
	}

	// Insert mode (editing: true, cursor: 3): distinct input.edit bg, bar cursor.
	_, fEdit, _ := drawTheme(makeForm(true, 3, true), withDensity(DensityCompact), "f", 30, 4)
	editRow0 := strings.Split(fEdit.ANSI, "\n")[0]
	if !strings.Contains(editRow0, editBg) {
		t.Errorf("insert mode active field missing input.edit bg: %q", editRow0)
	}
	// Label "title" -> lw = 5+2=7, x0 = 9, leading space = 1, cursor at 3 -> X = 13.
	if fEdit.Cursor != (buffer.Cursor{X: 13, Y: 0, Shape: buffer.CursorBar}) {
		t.Errorf("insert mode cursor = %+v, want X=13 Y=0 Bar", fEdit.Cursor)
	}
}

func TestFormWrappedMultilineAndCursor(t *testing.T) {
	th := withDensity(DensityCompact)
	editBg := sgrBg(th, "input.edit")

	// Width 22: label "notes" -> lw = 7, x0 = 9, iw = 13, room = 12.
	// Value has soft-wrap AND an explicit newline.
	val := "alpha beta gamma\nsecond line"
	// Cursor at rune index 23 ("alpha beta gamma\nsecond| line").
	n := ViewNode{
		Kind:      KindForm,
		ID:        "f",
		Focusable: true,
		Props: map[string]any{
			"fields": []any{
				map[string]any{"name": "notes", "type": "string"},
				map[string]any{"name": "tail", "type": "string"},
			},
			"values":  map[string]any{"notes": val, "tail": "ok"},
			"active":  0,
			"editing": true,
			"cursor":  23,
			"wrap":    true,
		},
	}

	// Measured height accounts for 3 wrapped lines of "notes" + 1 line of "tail" + 1 submit = 5.
	if got := measure(&n, 22, DensityCompact); got != 5 {
		t.Fatalf("measured wrapped form height = %d, want 5", got)
	}

	rows, f, _ := drawTheme(n, th, "f", 22, 6)
	// Line 0: "▸ notes   alpha beta" (x0=9, leading space at col 9, text at col 10)
	// Line 1: "          gamma"
	// Line 2: "          second line"
	// Line 3: "  tail    ok"
	// Line 4: "  ⏎ submit"
	wantRows(t, rows,
		"▸ notes   alpha beta",
		"          gamma",
		"          second line",
		"  tail    ok",
		"  ⏎ submit",
	)
	// Continuation lines carry the same input.edit background style.
	ansiLines := strings.Split(f.ANSI, "\n")
	for _, li := range []int{0, 1, 2} {
		if !strings.Contains(ansiLines[li], editBg) {
			t.Errorf("wrapped line %d missing input.edit bg: %q", li, ansiLines[li])
		}
	}
	// Cursor at index 23 ("second" = 6 chars on line 2): X = x0(9) + 1 + 6 = 16, Y = 2.
	if f.Cursor != (buffer.Cursor{X: 16, Y: 2, Shape: buffer.CursorBar}) {
		t.Errorf("wrapped cursor = %+v, want X=16 Y=2 Bar", f.Cursor)
	}

	// Short viewport (h=3): keeps wrapped cursor line visible + submit row pinned at bottom.
	shortRows, fShort, _ := drawTheme(n, th, "f", 22, 3)
	if !strings.Contains(shortRows[2], "⏎ submit") {
		t.Fatalf("short wrapped form should pin submit row:\n%s", strings.Join(shortRows, "\n"))
	}
	if fShort.Cursor.Shape != buffer.CursorBar || fShort.Cursor.X != 16 || fShort.Cursor.Y < 0 || fShort.Cursor.Y >= 2 {
		t.Fatalf("short wrapped form lost cursor: %+v\nrows:\n%s", fShort.Cursor, strings.Join(shortRows, "\n"))
	}
	if !strings.Contains(shortRows[fShort.Cursor.Y], "second line") {
		t.Errorf("cursor row %d = %q, want second line", fShort.Cursor.Y, shortRows[fShort.Cursor.Y])
	}
}
