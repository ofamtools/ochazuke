package ui

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/buffer"
)

func TestTreeWrapLongDisplayLabel(t *testing.T) {
	th := DefaultDarkTheme()
	selBg := sgrBg(th, "selected")
	nodes := []TreeNode{
		{
			Label: "This is a long label that wraps cleanly across lines",
			Icon:  "●",
			Meta:  []TreeMeta{{Text: "P1", Tone: "muted"}},
		},
		{
			Label: "Second item",
		},
	}
	root := ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props: map[string]any{
			"nodes":    nodes,
			"wrap":     true,
			"selected": 0,
		},
	}

	rows, f, _ := drawTheme(root, th, "tr", 28, 6)
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, "…") {
		t.Fatalf("wrap:true must not truncate label with ellipsis:\n%s", joined)
	}
	for _, word := range []string{"This", "long", "label", "wraps", "cleanly", "across", "lines", "P1", "Second item"} {
		if !strings.Contains(joined, word) {
			t.Errorf("wrapped tree missing %q:\n%s", word, joined)
		}
	}
	// Continuation lines align under label start ("· ● " = 4 cols) and carry selection bg.
	if !strings.HasPrefix(rows[1], "    ") {
		t.Errorf("continuation line not aligned under label start: %q", rows[1])
	}
	ansiRows := strings.Split(f.ANSI, "\n")
	if !strings.Contains(ansiRows[0], selBg) || !strings.Contains(ansiRows[1], selBg) {
		t.Errorf("selected wrapped display row missing selection bg on continuation line:\n%q\n%q", ansiRows[0], ansiRows[1])
	}
}

func TestTreeWrapMultilineDisplayAndInlineEditCursor(t *testing.T) {
	th := DefaultDarkTheme()
	editBg := sgrBg(th, "input.edit")
	nodes := []TreeNode{
		{
			Label: "First line\nSecond line",
			Meta:  []TreeMeta{{Text: "2/5", Tone: "muted"}},
		},
		{
			Label:      "ignored while editing",
			Editing:    true,
			EditPrefix: "[ ] ",
			EditValue:  "hello world\nnext line here",
			EditCursor: 16, // "hello world\nnext| line here" -> line 1 col 4
		},
	}
	root := ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props: map[string]any{
			"nodes":    nodes,
			"wrap":     true,
			"selected": 1,
		},
	}

	// Width 30: display row takes 2 lines (y=0,1); edit row lead="· [ ] " (6 cols), avail=24, takes 2 lines (y=2,3).
	rows, f, _ := drawTheme(root, th, "tr", 30, 6)
	if !strings.Contains(rows[0], "· First line") || !strings.HasSuffix(rows[0], "2/5") {
		t.Fatalf("multiline display line 0 mismatch: %q", rows[0])
	}
	if rows[1] != "  Second line" {
		t.Fatalf("multiline display line 1 mismatch: %q", rows[1])
	}
	if !strings.HasPrefix(rows[2], "· [ ] hello world") {
		t.Fatalf("multiline edit line 0 mismatch: %q", rows[2])
	}
	if rows[3] != "      next line here" {
		t.Fatalf("multiline edit line 1 mismatch: %q", rows[3])
	}
	// Cursor: leadW(6) + col(4) = 10, Y = 3.
	if f.Cursor != (buffer.Cursor{X: 10, Y: 3, Shape: buffer.CursorBar}) {
		t.Fatalf("multiline inline-edit cursor = %+v, want X=10 Y=3 Bar", f.Cursor)
	}
	ansiRows := strings.Split(f.ANSI, "\n")
	if !strings.Contains(ansiRows[2], editBg) || !strings.Contains(ansiRows[3], editBg) {
		t.Errorf("inline edit rows missing input.edit styling:\n%q\n%q", ansiRows[2], ansiRows[3])
	}

	// Tight viewport (h=2) with offset=0 must window down so inline-edit cursor stays visible.
	root.Props["offset"] = 0
	shortRows, fShort, _ := drawTheme(root, th, "tr", 30, 2)
	if fShort.Cursor != (buffer.Cursor{X: 10, Y: 1, Shape: buffer.CursorBar}) {
		t.Fatalf("windowed multiline edit cursor = %+v, want X=10 Y=1 Bar\nrows:\n%s", fShort.Cursor, strings.Join(shortRows, "\n"))
	}
	if !strings.Contains(shortRows[1], "next line here") {
		t.Errorf("windowed row 1 = %q, want edit continuation", shortRows[1])
	}
}

func TestTreeWrapFalsePreservesSingleLineBehavior(t *testing.T) {
	nodes := []TreeNode{
		{
			Label: "A very long single line label that truncates",
			Meta:  []TreeMeta{{Text: "P1", Tone: "muted"}},
		},
		{
			Label: "Next row",
		},
	}
	root := ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props: map[string]any{
			"nodes":    nodes,
			"wrap":     false,
			"selected": 0,
		},
	}
	rows, _, _ := drawTheme(root, DefaultDarkTheme(), "tr", 24, 4)
	if !strings.Contains(rows[0], "…") || !strings.HasSuffix(rows[0], "P1") {
		t.Fatalf("wrap:false should truncate single-line row with meta: %q", rows[0])
	}
	if rows[1] != "· Next row" {
		t.Fatalf("wrap:false row 1 = %q, want %q", rows[1], "· Next row")
	}
}

func TestTreeVisualRangeHighlightSingleAndWrapped(t *testing.T) {
	th := DefaultDarkTheme()
	selBg := sgrBg(th, "selected")
	visBg := sgrBg(th, "selected.inactive")

	nodes := []TreeNode{
		{
			Label: "First group",
			Children: []TreeNode{
				{Label: "Child one"},
			},
		},
		{Label: "Second group"},
		{Label: "Outside range"},
	}

	// Single-line with gapTopLevel: items are 0:First, 1:Child, (spacer), 2:Second, (spacer), 3:Outside.
	root := ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props: map[string]any{
			"nodes":       nodes,
			"gapTopLevel": true,
			"selected":    2,
			"visualFrom":  0,
			"visualTo":    2,
		},
	}
	_, f, _ := drawTheme(root, th, "tr", 30, 8)
	ansiRows := strings.Split(f.ANSI, "\n")
	if !strings.Contains(ansiRows[0], visBg) {
		t.Errorf("row 0 (First group) missing visual highlight %q:\n%q", visBg, ansiRows[0])
	}
	if !strings.Contains(ansiRows[1], visBg) {
		t.Errorf("row 1 (Child one) missing visual highlight %q:\n%q", visBg, ansiRows[1])
	}
	if strings.Contains(ansiRows[2], visBg) || strings.Contains(ansiRows[2], selBg) {
		t.Errorf("spacer row 2 should not carry selection highlight:\n%q", ansiRows[2])
	}
	if !strings.Contains(ansiRows[3], selBg) {
		t.Errorf("selected row 3 (Second group) missing focused selection bg %q:\n%q", selBg, ansiRows[3])
	}
	if strings.Contains(ansiRows[5], visBg) || strings.Contains(ansiRows[5], selBg) {
		t.Errorf("row 5 (Outside range) should not be highlighted:\n%q", ansiRows[5])
	}

	// Wrapped multi-line visual row carries visual highlight on continuation lines too.
	wrapRoot := ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props: map[string]any{
			"nodes": []TreeNode{
				{Label: "A long visual row that wraps cleanly onto two lines"},
				{Label: "Cursor row"},
			},
			"wrap":       true,
			"selected":   1,
			"visualFrom": 0,
			"visualTo":   1,
		},
	}
	_, fw, _ := drawTheme(wrapRoot, th, "tr", 24, 6)
	wAnsi := strings.Split(fw.ANSI, "\n")
	if !strings.Contains(wAnsi[0], visBg) || !strings.Contains(wAnsi[1], visBg) {
		t.Errorf("wrapped visual row missing selected.inactive bg on continuation line:\n%q\n%q", wAnsi[0], wAnsi[1])
	}
	if !strings.Contains(wAnsi[len(wAnsi)-1], selBg) {
		// Find cursor row line
		foundSel := false
		for _, ln := range wAnsi {
			if strings.Contains(ln, "Cursor row") && strings.Contains(ln, selBg) {
				foundSel = true
			}
		}
		if !foundSel {
			t.Errorf("wrapped cursor row missing focused selection bg %q:\n%s", selBg, fw.ANSI)
		}
	}
}

func TestTreeGutterDisabledByDefault(t *testing.T) {
	th := DefaultDarkTheme()
	plainNodes := []TreeNode{
		{
			Label: "Root",
			Children: []TreeNode{
				{Label: "Child"},
			},
		},
	}
	gutterNodes := []TreeNode{
		{
			Label:      "Root",
			Gutter:     "●",
			GutterTone: "state.working",
			Children: []TreeNode{
				{Label: "Child", Gutter: "✓", GutterTone: "success"},
			},
		},
	}
	baseRoot := ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props:     map[string]any{"nodes": plainNodes, "selected": 0},
	}
	defaultRoot := ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props:     map[string]any{"nodes": gutterNodes, "selected": 0},
	}
	falseRoot := ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props:     map[string]any{"nodes": gutterNodes, "gutter": false, "selected": 0},
	}

	baseRows, baseF, _ := drawTheme(baseRoot, th, "tr", 30, 4)
	defRows, defF, _ := drawTheme(defaultRoot, th, "tr", 30, 4)
	falseRows, falseF, _ := drawTheme(falseRoot, th, "tr", 30, 4)

	if strings.Join(defRows, "\n") != strings.Join(baseRows, "\n") || defF.ANSI != baseF.ANSI {
		t.Fatalf("omitted gutter prop changed output:\ngot:\n%s\nwant:\n%s", strings.Join(defRows, "\n"), strings.Join(baseRows, "\n"))
	}
	if strings.Join(falseRows, "\n") != strings.Join(baseRows, "\n") || falseF.ANSI != baseF.ANSI {
		t.Fatalf("gutter:false changed output:\ngot:\n%s\nwant:\n%s", strings.Join(falseRows, "\n"), strings.Join(baseRows, "\n"))
	}
}

func TestTreeGutterAlignmentClampingAndSelectionTone(t *testing.T) {
	th := DefaultDarkTheme()
	selBg := sgrBg(th, "selected")
	workFg := sgrFg(th, "state.working")
	okFg := sgrFg(th, "success")

	nodes := []TreeNode{
		{
			Label:      "Root",
			Gutter:     "●",
			GutterTone: "state.working",
			Children: []TreeNode{
				{
					Label:      "Child",
					Gutter:     "✓extra",
					GutterTone: "success",
					Children: []TreeNode{
						{Label: "Grandchild"},
					},
				},
			},
		},
		{
			Label:      "Second",
			Gutter:     "!?",
			GutterTone: "danger",
		},
		{
			Label:  "WideFallback",
			Gutter: "中",
		},
	}
	root := ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props: map[string]any{
			"nodes":       nodes,
			"gutter":      true,
			"gapTopLevel": true,
			"selected":    0,
		},
	}

	rows, f, _ := drawTheme(root, th, "tr", 30, 8)
	wantRows := []string{
		"● ▾ Root",
		"✓ └─ ▾ Child",
		"     └─ · Grandchild",
		"",
		"! · Second",
		"",
		"  · WideFallback",
	}
	for i, want := range wantRows {
		if rows[i] != want {
			t.Errorf("row %d = %q, want %q\nall rows:\n%s", i, rows[i], want, strings.Join(rows, "\n"))
		}
	}

	ansiRows := strings.Split(f.ANSI, "\n")
	if !strings.Contains(ansiRows[0], selBg) || !strings.Contains(ansiRows[0], workFg) {
		t.Errorf("selected row 0 should preserve gutterTone fg (%q) over selection bg (%q):\n%q", workFg, selBg, ansiRows[0])
	}
	if !strings.Contains(ansiRows[1], okFg) {
		t.Errorf("unselected row 1 missing gutterTone fg (%q):\n%q", okFg, ansiRows[1])
	}
	if strings.Contains(ansiRows[3], selBg) {
		t.Errorf("spacer row 3 should stay unstyled: %q", ansiRows[3])
	}
}

func TestTreeGutterWrapAndMetaAlignment(t *testing.T) {
	th := DefaultDarkTheme()
	nodes := []TreeNode{
		{
			Label:      "Root",
			Gutter:     "●",
			GutterTone: "state.working",
			Children: []TreeNode{
				{
					Label:      "A long nested task label that wraps cleanly",
					Gutter:     "○",
					GutterTone: "muted",
					Meta:       []TreeMeta{{Text: "P1", Tone: "muted"}},
				},
			},
		},
	}
	root := ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props: map[string]any{
			"nodes":    nodes,
			"gutter":   true,
			"wrap":     true,
			"selected": 1,
		},
	}

	// Width 28: child lead = gutter(2) + "└─ "(3) + "· "(2) = 7 cols.
	rows, _, _ := drawTheme(root, th, "tr", 28, 6)
	if rows[0] != "● ▾ Root" {
		t.Fatalf("row 0 = %q, want %q", rows[0], "● ▾ Root")
	}
	if !strings.HasPrefix(rows[1], "○ └─ · ") || !strings.HasSuffix(rows[1], "P1") {
		t.Fatalf("wrapped child line 0 mismatch: %q", rows[1])
	}
	if strW(rows[1]) > 28 {
		t.Fatalf("wrapped child line 0 width = %d, want <= 28: %q", strW(rows[1]), rows[1])
	}
	if !strings.HasPrefix(rows[2], strings.Repeat(" ", 7)) || strings.TrimLeft(rows[2], " ") == "" {
		t.Fatalf("wrapped continuation line should indent 7 cols past gutter+prefix+marker: %q", rows[2])
	}

	// Single-line (wrap:false) with meta also accounts for gutterW without overflowing width.
	root.Props["wrap"] = false
	sRows, _, _ := drawTheme(root, th, "tr", 28, 4)
	if !strings.HasPrefix(sRows[1], "○ └─ · ") || !strings.HasSuffix(sRows[1], "P1") || !strings.Contains(sRows[1], "…") {
		t.Fatalf("single-line gutter row with meta mismatch: %q", sRows[1])
	}
	if strW(sRows[1]) != 28 {
		t.Fatalf("single-line gutter row width = %d, want 28: %q", strW(sRows[1]), sRows[1])
	}
}

func TestTreeGutterInlineEditCursor(t *testing.T) {
	th := DefaultDarkTheme()
	nodes := []TreeNode{
		{
			Label:      "ignored",
			Gutter:     "●",
			GutterTone: "state.working",
			Editing:    true,
			EditPrefix: "[ ] ",
			EditValue:  "hello world\nnext line",
			EditCursor: 16, // line 1 col 4 ("next| line")
		},
	}
	// Wrapped multiline inline edit: lead = gutter(2) + marker(2) + editPrefix(4) = 8 cols.
	root := ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props: map[string]any{
			"nodes":    nodes,
			"gutter":   true,
			"wrap":     true,
			"selected": 0,
		},
	}
	rows, f, _ := drawTheme(root, th, "tr", 30, 4)
	if !strings.HasPrefix(rows[0], "● · [ ] hello world") {
		t.Fatalf("edit line 0 with gutter mismatch: %q", rows[0])
	}
	if rows[1] != "        next line" {
		t.Fatalf("edit continuation line with gutter mismatch: %q", rows[1])
	}
	// Cursor: leadW(8) + col(4) = 12, Y = 1.
	if f.Cursor != (buffer.Cursor{X: 12, Y: 1, Shape: buffer.CursorBar}) {
		t.Fatalf("multiline inline-edit cursor with gutter = %+v, want X=12 Y=1 Bar", f.Cursor)
	}

	// Single-line inline edit: cursor shifts by gutterW (+2) -> leadW(8) + 3 = 11.
	nodes[0].EditValue = "abcdef"
	nodes[0].EditCursor = 3
	root.Props["wrap"] = false
	sRows, sf, _ := drawTheme(root, th, "tr", 30, 4)
	if !strings.HasPrefix(sRows[0], "● · [ ] abcdef") {
		t.Fatalf("single-line edit with gutter mismatch: %q", sRows[0])
	}
	if sf.Cursor != (buffer.Cursor{X: 11, Y: 0, Shape: buffer.CursorBar}) {
		t.Fatalf("single-line inline-edit cursor with gutter = %+v, want X=11 Y=0 Bar", sf.Cursor)
	}
}
