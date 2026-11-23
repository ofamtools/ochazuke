package ui

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/buffer"
)

func TestTreeGapTopLevelAndInlineEditCursor(t *testing.T) {
	nodes := []map[string]any{
		{
			"label": "[~] Project A",
			"children": []map[string]any{
				{"label": "[ ] Child A1"},
			},
		},
		{
			"label":      "[ ] Project B",
			"editing":    true,
			"editPrefix": "[ ] ",
			"editValue":  "Project B",
			"editCursor": 3,
		},
	}
	root := ViewNode{
		ID:        "tree",
		Kind:      KindTree,
		Focusable: true,
		Props: map[string]any{
			"nodes":       nodes,
			"guides":      true,
			"gapTopLevel": true,
			"selected":    2,
			"offset":      0,
		},
	}
	fr, _ := Render(root, DefaultDarkTheme(), "tree", 40, 6)
	lines := strings.Split(fr.ANSI, "\n")
	if len(lines) < 4 {
		t.Fatalf("expected >=4 lines, got %d", len(lines))
	}
	// Row 0: Project A, Row 1: Child A1, Row 2: blank spacer, Row 3: Project B (editing).
	if strings.TrimSpace(stripANSI(lines[2])) != "" {
		t.Fatalf("expected blank spacer line between top-level projects, got %q", stripANSI(lines[2]))
	}
	if fr.Cursor.Shape != buffer.CursorBar || fr.Cursor.Y != 3 {
		t.Fatalf("expected bar cursor on row 3 during inline edit, got %+v", fr.Cursor)
	}
}
