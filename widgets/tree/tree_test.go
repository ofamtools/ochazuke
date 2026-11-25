package tree

import (
	"regexp"
	"strings"
	"testing"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/input"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func sampleTree() *Model {
	return New([]Node{
		{
			ID:       "a",
			Label:    "Project A",
			Scope:    "project",
			Ref:      &core.Ref{Kind: "project", ID: "a"},
			Tone:     "row.active",
			Icon:     "●",
			IconTone: "state.working",
			Facts:    map[string]any{"status": "active"},
			Children: []Node{
				{ID: "a1", Label: "Task A1", Ref: &core.Ref{Kind: "task", ID: "a1"}},
				{
					ID:    "a2",
					Label: "Task A2",
					Children: []Node{
						{ID: "a2a", Label: "Subtask A2a"},
					},
				},
			},
		},
		{
			ID:    "b",
			Label: "Project B",
			Children: []Node{
				{ID: "b1", Label: "Task B1"},
			},
		},
		{
			ID:    "c",
			Label: "Project C",
		},
	})
}

func TestFlattenAndGapTopLevelSpacerMath(t *testing.T) {
	m := sampleTree()
	rows := m.Flatten()
	// Visual layout with GapTopLevel=true:
	// 0: a, 1: a1, 2: a2, 3: a2a, (4: spacer), 5: b, 6: b1, (7: spacer), 8: c
	wantVis := map[string]int{"a": 0, "a1": 1, "a2": 2, "a2a": 3, "b": 5, "b1": 6, "c": 8}
	if len(rows) != len(wantVis) {
		t.Fatalf("len(rows) = %d, want %d", len(rows), len(wantVis))
	}
	for i, r := range rows {
		if r.Index != i {
			t.Errorf("row %s Index = %d, want %d", r.ID, r.Index, i)
		}
		if r.VisualRow != wantVis[r.ID] {
			t.Errorf("row %s VisualRow = %d, want %d", r.ID, r.VisualRow, wantVis[r.ID])
		}
	}

	// Disabling GapTopLevel removes spacers.
	m.GapTopLevel = false
	for i, r := range m.Flatten() {
		if r.VisualRow != i {
			t.Errorf("without gaps: row %s VisualRow = %d, want %d", r.ID, r.VisualRow, i)
		}
	}
}

func TestScrollOffAndScrollAcrossSpacers(t *testing.T) {
	m := sampleTree()
	m.PageSize = 5
	m.ScrollOff = 1

	// Moving down from a -> a1 -> a2 -> a2a -> b (vis 5) should scroll viewport with ScrollOff=1.
	m.HandleCommand(core.Command{ID: "motion.down", Count: 4}) // b (visual 5)
	if m.SelectedID != "b" {
		t.Fatalf("SelectedID = %q, want b", m.SelectedID)
	}
	if m.Offset != 2 {
		t.Fatalf("Offset = %d, want 2 (vis 5 with page 5 scrollOff 1)", m.Offset)
	}

	// Scroll down onto spacer: reset to top (offset=0, selected=a at vis 0), page=4, scrollOff=0.
	// Visual rows: 0:a, 1:a1, 2:a2, 3:a2a, 4:spacer, 5:b, 6:b1, 7:spacer, 8:c.
	m.ScrollOff = 0
	m.PageSize = 4
	m.Offset = 0
	m.Select("a")
	// Scroll down by 4 -> newOffset=4 (spacer!), targetVis=4 -> breaks tie DOWN to b (vis 5).
	m.HandleCommand(core.Command{ID: "motion.scroll.down", Count: 4})
	if m.SelectedID != "b" || m.Offset != 4 {
		t.Fatalf("scroll down onto spacer: sel=%q offset=%d, want b / 4", m.SelectedID, m.Offset)
	}

	// Scroll up by 3 -> newOffset=1, window [1..4]; b (vis 5) clamps to targetVis=4 (spacer!) -> breaks tie UP to a2a (vis 3).
	m.HandleCommand(core.Command{ID: "motion.scroll.up", Count: 3})
	if m.SelectedID != "a2a" || m.Offset != 1 {
		t.Fatalf("scroll up onto spacer: sel=%q offset=%d, want a2a / 1", m.SelectedID, m.Offset)
	}
}

func TestBlockNextPrevRootJumps(t *testing.T) {
	m := sampleTree()
	// Start on a (root 0).
	m.HandleCommand(core.Command{ID: "motion.block.next"})
	if m.SelectedID != "b" {
		t.Fatalf("block.next from a = %q, want b", m.SelectedID)
	}
	m.HandleCommand(core.Command{ID: "motion.down"}) // b1
	m.HandleCommand(core.Command{ID: "motion.block.prev"})
	if m.SelectedID != "b" {
		t.Fatalf("block.prev from b1 = %q, want b", m.SelectedID)
	}
	m.HandleCommand(core.Command{ID: "motion.block.prev"})
	if m.SelectedID != "a" {
		t.Fatalf("block.prev from b = %q, want a", m.SelectedID)
	}
	m.HandleCommand(core.Command{ID: "motion.block.next", Count: 2})
	if m.SelectedID != "c" {
		t.Fatalf("2x block.next from a = %q, want c", m.SelectedID)
	}
}

func TestFoldingLeafFoldsParentAndReveal(t *testing.T) {
	m := sampleTree()
	// Select leaf a1 and ToggleFold -> folds parent a and moves selection to a.
	if !m.Select("a1") {
		t.Fatal("Select(a1) failed")
	}
	if !m.HandleCommand(core.Command{ID: CmdFoldToggle}) {
		t.Fatal("CmdFoldToggle not handled")
	}
	if m.SelectedID != "a" || !m.Folded["a"] {
		t.Fatalf("leaf ToggleFold: sel=%q folded=%v, want a / true", m.SelectedID, m.Folded)
	}
	rows := m.Flatten()
	if rows[0].HiddenDescendant != 3 {
		t.Fatalf("a HiddenDescendant = %d, want 3 (a1, a2, a2a)", rows[0].HiddenDescendant)
	}

	// FoldAll collapses everything, then Reveal("a2a") unfolds ancestors and selects a2a.
	m.FoldAll()
	if len(m.Flatten()) != 3 {
		t.Fatalf("FoldAll visible rows = %d, want 3", len(m.Flatten()))
	}
	if !m.Reveal("a2a") {
		t.Fatal("Reveal(a2a) failed")
	}
	if m.SelectedID != "a2a" || m.Folded["a"] || m.Folded["a2"] {
		t.Fatalf("after Reveal(a2a): sel=%q folded=%v", m.SelectedID, m.Folded)
	}
	if m.Reveal("nonexistent") {
		t.Error("Reveal(nonexistent) should return false")
	}

	m.UnfoldAll()
	if len(m.Folded) != 0 {
		t.Errorf("UnfoldAll left Folded=%v", m.Folded)
	}
}

func TestInlineEditRenameAddIndentOutdentCommitCancel(t *testing.T) {
	// Rename on existing node.
	m := sampleTree()
	if !m.StartRename("[ ] ") || !m.Editing() {
		t.Fatal("StartRename failed")
	}
	m.HandleEditCommand(core.Command{ID: input.CmdInsert, Args: map[string]any{"text": "!"}})
	res, ok := m.CommitEdit()
	if !ok || res.Kind != EditRename || res.TargetID != "a" || res.Value != "Project A!" {
		t.Fatalf("CommitEdit rename = %+v, ok=%v", res, ok)
	}
	if m.Editing() {
		t.Fatal("should not be editing after CommitEdit")
	}

	// AddBelow on Project A -> indent under A -> outdent back to root.
	if !m.StartAdd(EditAddBelow, "+ ") {
		t.Fatal("StartAdd(EditAddBelow) failed")
	}
	if e := m.Edit(); e == nil || e.ParentID != "" || e.AfterID != "a" || e.Depth != 0 {
		t.Fatalf("initial AddBelow state: %+v", e)
	}
	if !m.EditIndent() {
		t.Fatal("EditIndent should demote draft under Project A")
	}
	if e := m.Edit(); e.ParentID != "a" || e.AfterID != "a2" || e.Depth != 1 {
		t.Fatalf("after EditIndent: %+v", e)
	}
	if !m.EditOutdent() {
		t.Fatal("EditOutdent should promote draft back to root")
	}
	if e := m.Edit(); e.ParentID != "" || e.AfterID != "a" || e.Depth != 0 {
		t.Fatalf("after EditOutdent: %+v", e)
	}
	if m.EditOutdent() {
		t.Error("EditOutdent at root should return false")
	}
	m.HandleEditCommand(core.Command{ID: input.CmdInsert, Args: map[string]any{"text": "New Root"}})
	res, ok = m.CommitEdit()
	if !ok || res.Kind != EditAddBelow || res.Value != "New Root" || res.ParentID != "" || res.AfterID != "a" {
		t.Fatalf("CommitEdit AddBelow = %+v, ok=%v", res, ok)
	}

	// CancelEdit aborts cleanly.
	m.StartAdd(EditAddChild, "")
	if !m.CancelEdit() || m.Editing() {
		t.Fatal("CancelEdit failed")
	}

	// StartAdd works on an empty tree.
	empty := New(nil)
	if !empty.StartAdd(EditAddBelow, "") || !empty.Editing() {
		t.Fatal("StartAdd on empty tree failed")
	}
	empty.HandleEditCommand(core.Command{ID: input.CmdInsert, Args: map[string]any{"text": "First"}})
	eRes, ok := empty.CommitEdit()
	if !ok || eRes.Value != "First" || eRes.ParentID != "" || eRes.Depth != 0 {
		t.Fatalf("empty tree CommitEdit = %+v, ok=%v", eRes, ok)
	}
}

func TestContextFrameFactsAndViewRender(t *testing.T) {
	m := sampleTree()
	m.Fold() // folds Project A (3 hidden descendants)

	cf := m.ContextFrame("", "tree")
	if cf.Scope != "project" || cf.Subject == nil || cf.Subject.ID != "a" {
		t.Fatalf("ContextFrame scope/subject: %+v", cf)
	}
	for k, want := range map[string]any{
		"isTree":      true,
		"rowCount":    4, // a, b, b1, c
		"rootCount":   3,
		"editing":     false,
		"depth":       0,
		"hasChildren": true,
		"folded":      true,
		"isRoot":      true,
		"status":      "active",
	} {
		if cf.Facts[k] != want {
			t.Errorf("Facts[%q] = %v, want %v", k, cf.Facts[k], want)
		}
	}

	v := m.View("tr")
	if v.Kind != ui.KindTree || !v.Focusable || v.CacheKey == 0 {
		t.Fatalf("ViewNode = %+v", v)
	}
	if _, ok := v.Props["nodes"].([]ui.TreeNode); !ok {
		t.Fatalf("nodes prop should be typed []ui.TreeNode, got %T", v.Props["nodes"])
	}
	if m.View("tr").CacheKey != v.CacheKey {
		t.Error("CacheKey not deterministic")
	}

	// Render with ui.Render and check folded count (+3) and GapTopLevel blank line.
	fr, _ := ui.Render(v, ui.DefaultDarkTheme(), "tr", 40, 8)
	lines := strings.Split(stripANSI(fr.ANSI), "\n")
	if !strings.Contains(lines[0], "▸ ● Project A") || !strings.Contains(lines[0], "(+3)") {
		t.Fatalf("folded root line mismatch: %q", lines[0])
	}
	if strings.TrimSpace(lines[1]) != "" {
		t.Fatalf("expected GapTopLevel spacer on row 1, got %q", lines[1])
	}

	// Turning off ShowFoldCount hides (+3) and invalidates CacheKey.
	m.ShowFoldCount = false
	v2 := m.View("tr")
	if v2.CacheKey == v.CacheKey {
		t.Error("CacheKey did not change when ShowFoldCount changed")
	}
	fr2, _ := ui.Render(v2, ui.DefaultDarkTheme(), "tr", 40, 8)
	if strings.Contains(stripANSI(fr2.ANSI), "(+3)") {
		t.Errorf("ShowFoldCount=false should hide (+3):\n%s", stripANSI(fr2.ANSI))
	}

	// Inline edit renders bar cursor on the edited row.
	m.StartRename("[ ] ")
	vEdit := m.View("tr")
	if vEdit.Facts["editing"] != true {
		t.Errorf("editing fact should be true: %v", vEdit.Facts)
	}
	frEdit, _ := ui.Render(vEdit, ui.DefaultDarkTheme(), "tr", 40, 8)
	if frEdit.Cursor.Shape != buffer.CursorBar || frEdit.Cursor.Y != 0 {
		t.Errorf("expected bar cursor on row 0 during rename, got %+v", frEdit.Cursor)
	}

	// Wrap prop, CacheKey invalidation, and EditInsertNewline / CmdInsert("\n").
	if vEdit.Props["wrap"] != false {
		t.Fatalf("default wrap prop = %v, want false", vEdit.Props["wrap"])
	}
	m.Wrap = true
	vWrap := m.View("tr")
	if vWrap.Props["wrap"] != true || vWrap.CacheKey == vEdit.CacheKey {
		t.Fatalf("Wrap=true should emit wrap prop and update CacheKey")
	}
	if !m.EditInsertNewline() {
		t.Fatal("EditInsertNewline returned false")
	}
	m.HandleEditCommand(core.Command{ID: input.CmdInsert, Args: map[string]any{"text": "line2"}})
	res, ok := m.CommitEdit()
	if !ok || res.Value != "Project A\nline2" {
		t.Fatalf("multiline CommitEdit = %+v, ok=%v", res, ok)
	}
}

func TestGutterPropAndCacheKey(t *testing.T) {
	m := New([]Node{
		{
			ID:         "a",
			Label:      "Task A",
			Gutter:     "●",
			GutterTone: "state.working",
			Children: []Node{
				{ID: "a1", Label: "Subtask A1"},
			},
		},
	})
	if m.ShowGutter {
		t.Fatal("ShowGutter should default to false")
	}

	vOff := m.View("tr")
	if vOff.Props["gutter"] != false {
		t.Fatalf("default gutter prop = %v, want false", vOff.Props["gutter"])
	}
	frOff, _ := ui.Render(vOff, ui.DefaultDarkTheme(), "tr", 30, 4)
	linesOff := strings.Split(stripANSI(frOff.ANSI), "\n")
	if !strings.HasPrefix(linesOff[0], "▾ Task A") {
		t.Fatalf("ShowGutter=false should not draw gutter: %q", linesOff[0])
	}

	m.ShowGutter = true
	vOn := m.View("tr")
	if vOn.Props["gutter"] != true {
		t.Fatalf("ShowGutter=true gutter prop = %v, want true", vOn.Props["gutter"])
	}
	if vOn.CacheKey == vOff.CacheKey {
		t.Error("CacheKey did not change when ShowGutter changed")
	}
	frOn, _ := ui.Render(vOn, ui.DefaultDarkTheme(), "tr", 30, 4)
	linesOn := strings.Split(stripANSI(frOn.ANSI), "\n")
	if !strings.HasPrefix(linesOn[0], "● ▾ Task A") {
		t.Fatalf("ShowGutter=true root line mismatch: %q", linesOn[0])
	}
	if !strings.HasPrefix(linesOn[1], "  └─ · Subtask A1") {
		t.Fatalf("ShowGutter=true child line mismatch: %q", linesOn[1])
	}

	m.Nodes[0].Gutter = "✓"
	vGlyph := m.View("tr")
	if vGlyph.CacheKey == vOn.CacheKey {
		t.Error("CacheKey did not change when Node.Gutter changed")
	}

	m.Nodes[0].GutterTone = "success"
	vTone := m.View("tr")
	if vTone.CacheKey == vGlyph.CacheKey {
		t.Error("CacheKey did not change when Node.GutterTone changed")
	}
}
