package tree

import (
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
	"ofam.dev/ochazuke/ui"
)

func TestTreeSelectCommandsMethodsAndEsc(t *testing.T) {
	m := sampleTree()

	// Default selection facts on unmarked initial state.
	v0 := m.View("tr")
	if !slices.Contains(v0.Traits, traits.TraitSelect) {
		t.Fatalf("View Traits missing TraitSelect: %v", v0.Traits)
	}
	if v0.Facts["marked"] != false || v0.Facts["markedCount"] != 0 ||
		v0.Facts["visualActive"] != false || v0.Facts["visualCount"] != 0 ||
		v0.Facts["hasSelection"] != false {
		t.Fatalf("initial select facts: %+v", v0.Facts)
	}
	if got := m.SelectedOrMarkedIDs(); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("SelectedOrMarkedIDs default = %v, want [a]", got)
	}

	// Mark toggle via canonical select command and direct method.
	if !m.HandleCommand(core.Command{ID: traits.SelectCmdMarkToggle}) {
		t.Fatal("select.mark.toggle not handled")
	}
	m.Select("a1")
	m.ToggleMark()
	if got := m.MarkedIDs(); !slices.Equal(got, []string{"a", "a1"}) {
		t.Fatalf("MarkedIDs = %v, want [a a1]", got)
	}
	if got := m.SelectedOrMarkedIDs(); !slices.Equal(got, []string{"a", "a1"}) {
		t.Fatalf("SelectedOrMarkedIDs with marks = %v, want [a a1]", got)
	}
	cf := m.ContextFrame("", "tr")
	if len(cf.Subjects) != 2 || cf.Subjects[0].ID != "a" || cf.Subjects[1].ID != "a1" {
		t.Fatalf("ContextFrame.Subjects = %+v, want [a, a1]", cf.Subjects)
	}
	if cf.Facts["marked"] != true || cf.Facts["markedCount"] != 2 || cf.Facts["hasSelection"] != true {
		t.Fatalf("marked facts = %+v", cf.Facts)
	}

	// Copy clones Marked independently.
	cp := m.Copy()
	cp.Marked["c"] = true
	if m.Marked["c"] {
		t.Fatal("Copy did not clone Marked map")
	}

	// SetNodes prunes vanished marked IDs.
	trimmed := []Node{
		{ID: "a", Label: "Project A"},
		{ID: "c", Label: "Project C"},
	}
	mPrune := m.Copy()
	mPrune.SetNodes(trimmed)
	if got := mPrune.MarkedIDs(); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("SetNodes did not prune Marked: got %v, want [a]", got)
	}

	// Select all, invert, and clear marks.
	if !m.HandleCommand(core.Command{ID: traits.SelectCmdAll}) {
		t.Fatal("select.all not handled")
	}
	if got := len(m.MarkedIDs()); got != 7 {
		t.Fatalf("select.all marked %d nodes, want 7", got)
	}
	if !m.HandleCommand(core.Command{ID: traits.SelectCmdInvert}) {
		t.Fatal("select.invert not handled")
	}
	if len(m.MarkedIDs()) != 0 {
		t.Fatalf("select.invert after select.all should be empty, got %v", m.MarkedIDs())
	}
	m.ToggleMark() // mark a1
	if !m.HandleCommand(core.Command{ID: traits.SelectCmdMarkClear}) || len(m.MarkedIDs()) != 0 {
		t.Fatalf("select.mark.clear failed: %v", m.MarkedIDs())
	}

	// Visual mode via select.visual.toggle, move down, inspect VisualIDs/SelectedOrMarkedIDs, mark via select.mark.toggle.
	m.Select("a1") // index 1
	if !m.HandleCommand(core.Command{ID: traits.SelectCmdVisualToggle}) {
		t.Fatal("select.visual.toggle not handled")
	}
	m.HandleCommand(core.Command{ID: "motion.down", Count: 2}) // index 3 (a2a)
	if got := m.VisualIDs(); !slices.Equal(got, []string{"a1", "a2", "a2a"}) {
		t.Fatalf("VisualIDs = %v, want [a1 a2 a2a]", got)
	}
	if got := m.SelectedOrMarkedIDs(); !slices.Equal(got, []string{"a1", "a2", "a2a"}) {
		t.Fatalf("SelectedOrMarkedIDs in visual mode = %v, want [a1 a2 a2a]", got)
	}
	vVis := m.View("tr")
	if vVis.Props["visualFrom"] != 1 || vVis.Props["visualTo"] != 3 {
		t.Fatalf("visual props = %+v, want visualFrom=1 visualTo=3", vVis.Props)
	}
	if vVis.Facts["visualActive"] != true || vVis.Facts["visualCount"] != 3 || vVis.Facts["hasSelection"] != true {
		t.Fatalf("visual facts = %+v", vVis.Facts)
	}
	if vVis.CacheKey == v0.CacheKey {
		t.Fatal("CacheKey did not change when entering visual mode")
	}

	// select.mark.toggle while visual active marks visual range and exits visual mode.
	if !m.HandleCommand(core.Command{ID: traits.SelectCmdMarkToggle}) {
		t.Fatal("select.mark.toggle in visual mode not handled")
	}
	if m.VisualActive || !slices.Equal(m.MarkedIDs(), []string{"a1", "a2", "a2a"}) {
		t.Fatalf("after visual mark toggle: active=%v marked=%v", m.VisualActive, m.MarkedIDs())
	}

	// esc clears active visual mode, and returns false when visual mode is inactive.
	if m.HandleCommand(core.Command{ID: "esc"}) {
		t.Fatal("esc should not be handled when visual mode is inactive")
	}
	m.HandleCommand(core.Command{ID: traits.SelectCmdVisualLine})
	if !m.VisualActive {
		t.Fatal("select.visual.line did not activate visual mode")
	}
	if !m.HandleCommand(core.Command{ID: "esc"}) || m.VisualActive {
		t.Fatal("esc should clear active visual mode")
	}

	// Direct ToggleVisual / MarkVisual / ClearVisual / ClearMarks helpers.
	m.ClearMarks()
	m.Select("b")
	m.ToggleVisual()
	m.Select("b1")
	m.MarkVisual()
	if m.VisualActive || !slices.Equal(m.MarkedIDs(), []string{"b", "b1"}) {
		t.Fatalf("MarkVisual helper: active=%v marked=%v", m.VisualActive, m.MarkedIDs())
	}
	m.ToggleVisual()
	m.ClearVisual()
	if m.VisualActive {
		t.Fatal("ClearVisual did not exit visual mode")
	}
}

func TestTreeMarkRenderingPreservesRunsSpansAndCacheKey(t *testing.T) {
	m := New([]Node{
		{
			ID: "n1",
			LabelRuns: []ui.InlineRun{
				{Text: "Open "},
				{Kind: "link", Label: "Doc", Ref: &core.Ref{Kind: "document", ID: "d-1"}},
			},
		},
		{
			ID:         "n2",
			Label:      "Styled",
			LabelSpans: []ui.ToneSpan{{From: 0, To: 6, Tone: "warning"}},
		},
	})

	k0 := m.View("tr").CacheKey
	m.Select("n1")
	m.ToggleMark()
	m.Select("n2")
	m.ToggleMark()

	v := m.View("tr")
	if v.CacheKey == k0 {
		t.Fatal("CacheKey did not invalidate after marking nodes")
	}
	links := ui.NodeLinks(&v, 0)
	if len(links) != 1 || links[0].Label != "Doc" || links[0].Ref == nil || links[0].Ref.ID != "d-1" {
		t.Fatalf("marking broke NodeLinks: %+v", links)
	}
	uiNodes := v.Props["nodes"].([]ui.TreeNode)
	if !strings.HasPrefix(uiNodes[1].Label, "● ") {
		t.Fatalf("marked plain node missing mark prefix: %q", uiNodes[1].Label)
	}
	// Existing span should be shifted by 2 ("● ") and accent span prepended for [0,2).
	if len(uiNodes[1].LabelSpans) != 2 || uiNodes[1].LabelSpans[0] != (ui.ToneSpan{From: 0, To: 2, Tone: "accent"}) ||
		uiNodes[1].LabelSpans[1] != (ui.ToneSpan{From: 2, To: 8, Tone: "warning"}) {
		t.Fatalf("marked node LabelSpans = %+v", uiNodes[1].LabelSpans)
	}

	// VisualAnchor change invalidates CacheKey even when visual range stays anchored.
	m.ToggleVisual()
	kVis1 := m.View("tr").CacheKey
	m.VisualAnchor = 0
	kVis2 := m.View("tr").CacheKey
	if kVis1 == kVis2 {
		t.Fatal("CacheKey did not invalidate when VisualAnchor changed")
	}

	fr, _ := ui.Render(v, ui.DefaultDarkTheme(), "tr", 40, 6)
	plain := stripANSI(fr.ANSI)
	if !strings.Contains(plain, "● Open Doc") || !strings.Contains(plain, "● Styled") {
		t.Fatalf("rendered marked tree missing indicators:\n%s", plain)
	}
}

func TestTreeSmartFoldStepInAndStepOut(t *testing.T) {
	m := sampleTree()
	m.FoldAll()

	// Root "a" is collapsed: StepIn unfolds it (returns true), keeping cursor on "a".
	m.Select("a")
	if !m.HandleCommand(core.Command{ID: CmdFoldOrStepIn}) {
		t.Fatal("StepIn on collapsed parent should return true")
	}
	if m.Folded["a"] || m.SelectedID != "a" {
		t.Fatalf("after first StepIn on a: folded=%v sel=%q, want false/a", m.Folded["a"], m.SelectedID)
	}

	// Root "a" is now expanded: StepIn moves cursor to first child "a1".
	if !m.HandleCommand(core.Command{ID: CmdFoldOrStepIn}) {
		t.Fatal("StepIn on expanded parent should return true")
	}
	if m.SelectedID != "a1" {
		t.Fatalf("after second StepIn on a: sel=%q, want a1", m.SelectedID)
	}

	// Leaf "a1": StepIn returns false so outer spill can move focus right.
	if m.HandleCommand(core.Command{ID: CmdFoldOrStepIn}) {
		t.Fatal("StepIn on leaf should return false")
	}

	// Leaf "a1" has parent "a": StepOut moves cursor to "a" without folding "a".
	if !m.HandleCommand(core.Command{ID: CmdFoldOrParent}) {
		t.Fatal("StepOut on leaf with parent should return true")
	}
	if m.SelectedID != "a" || m.Folded["a"] {
		t.Fatalf("StepOut on leaf a1: sel=%q folded=%v, want a / false", m.SelectedID, m.Folded["a"])
	}

	// Collapsed child parent "a2": StepOut moves cursor to parent "a".
	m.Select("a2") // still folded from FoldAll
	vA2 := m.View("tr")
	if vA2.Facts["hasParent"] != true || vA2.Facts["folded"] != true || vA2.Facts["hasChildren"] != true {
		t.Fatalf("a2 facts = %+v", vA2.Facts)
	}
	if !m.StepOut() || m.SelectedID != "a" {
		t.Fatalf("StepOut on collapsed parent a2: sel=%q, want a", m.SelectedID)
	}

	// Expanded root "a": StepOut folds "a" (returns true).
	if !m.StepOut() || !m.Folded["a"] || m.SelectedID != "a" {
		t.Fatalf("StepOut on expanded root a: folded=%v sel=%q", m.Folded["a"], m.SelectedID)
	}

	// Collapsed root "a": StepOut returns false so outer spill can move focus left.
	if m.StepOut() {
		t.Fatal("StepOut on collapsed root should return false")
	}

	// Leaf root "c": both StepIn and StepOut return false.
	m.Select("c")
	vC := m.View("tr")
	if vC.Facts["hasParent"] != false || vC.Facts["hasChildren"] != false {
		t.Fatalf("root leaf c facts = %+v", vC.Facts)
	}
	if m.StepIn() || m.StepOut() {
		t.Fatal("leaf root should return false for both StepIn and StepOut")
	}

	// Hoisted root invariant: direct child of active hoist root does not step onto hoist root.
	m.UnfoldAll()
	m.Hoist("a")
	m.Select("a2a")
	if !m.StepOut() || m.SelectedID != "a2" {
		t.Fatalf("hoisted StepOut on a2a: sel=%q, want a2", m.SelectedID)
	}
	if !m.StepOut() || !m.Folded["a2"] {
		t.Fatalf("hoisted StepOut on expanded a2 should fold it: folded=%v", m.Folded["a2"])
	}
	// Now a2 is collapsed and its parent is active hoist root "a": StepOut returns false.
	if m.StepOut() {
		t.Fatal("StepOut on direct child of active hoist root should return false")
	}
	if m.View("tr").Facts["hasParent"] != false {
		t.Fatalf("direct child of active hoist root should report hasParent=false")
	}
	// Active hoist root itself cannot fold via StepOut.
	m.Select("a")
	if m.StepOut() {
		t.Fatal("StepOut on active hoist root should return false")
	}

	// Specs includes CmdFoldOrStepIn and CmdFoldOrParent.
	specIDs := map[string]bool{}
	for _, s := range Specs() {
		specIDs[s.ID] = true
	}
	if !specIDs[CmdFoldOrStepIn] || !specIDs[CmdFoldOrParent] {
		t.Fatalf("Specs() missing smart fold commands: %v", specIDs)
	}
}
