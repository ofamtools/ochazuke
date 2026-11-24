package tree

import (
	"slices"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/widgets/input"
)

func hoistFixtureNodes() []Node {
	return []Node{
		{
			ID:    "alpha",
			Label: "Alpha",
			Children: []Node{
				{ID: "alpha-1", Label: "Alpha 1"},
				{
					ID:    "alpha-2",
					Label: "Alpha 2",
					Children: []Node{
						{
							ID:    "alpha-2-a",
							Label: "Alpha 2a",
							Children: []Node{
								{ID: "alpha-2-a-1", Label: "Alpha 2a1"},
							},
						},
						{ID: "alpha-2-b", Label: "Alpha 2b"},
					},
				},
			},
		},
		{
			ID:    "beta",
			Label: "Beta",
			Children: []Node{
				{ID: "beta-1", Label: "Beta 1"},
			},
		},
		{
			ID:    "gamma",
			Label: "Gamma",
		},
	}
}

func rowIDs(rows []VisibleRow) []string {
	ids := make([]string, len(rows))
	for i, r := range rows {
		if r.Draft {
			ids[i] = "<draft>"
		} else {
			ids[i] = r.ID
		}
	}
	return ids
}

func TestHoistUnhoistClearAndNestedStack(t *testing.T) {
	m := New(hoistFixtureNodes())
	m.FoldAll()

	// Unhoist / ClearHoist are no-ops when not hoisted.
	if m.Unhoist() || m.ClearHoist() || m.Hoisted() || m.HoistID() != "" || m.HoistPath() != nil {
		t.Fatalf("initial hoist state unexpected: hoisted=%v id=%q path=%v", m.Hoisted(), m.HoistID(), m.HoistPath())
	}

	// Hoist unknown or empty ID is a no-op.
	if m.Hoist("") || m.Hoist("missing") {
		t.Fatal("Hoist should reject empty or unknown IDs")
	}

	// Hoist into folded "alpha": unfolds alpha so its direct children become visible.
	if !m.Hoist("alpha") {
		t.Fatal("Hoist(alpha) returned false")
	}
	if !m.Hoisted() || m.HoistID() != "alpha" || len(m.HoistStack) != 1 {
		t.Fatalf("after Hoist(alpha): hoisted=%v id=%q stack=%v", m.Hoisted(), m.HoistID(), m.HoistStack)
	}
	if m.Folded["alpha"] {
		t.Fatal("Hoist(alpha) should unfold alpha")
	}
	if m.Hoist("alpha") {
		t.Fatal("re-hoisting active HoistID should be a no-op")
	}
	// alpha-2 is still folded from FoldAll; visible rows are alpha (depth 0), alpha-1 (depth 1), alpha-2 (depth 1).
	rows := m.Flatten()
	if got, want := rowIDs(rows), []string{"alpha", "alpha-1", "alpha-2"}; !slices.Equal(got, want) {
		t.Fatalf("hoisted alpha rows = %v, want %v", got, want)
	}
	if rows[0].Depth != 0 || rows[1].Depth != 1 || rows[2].Depth != 1 {
		t.Fatalf("hoisted alpha depths = [%d %d %d], want [0 1 1]", rows[0].Depth, rows[1].Depth, rows[2].Depth)
	}

	// Nested hoist into alpha-2 via HandleCommand(CmdHoistIn).
	if !m.Select("alpha-2") {
		t.Fatal("Select(alpha-2) failed")
	}
	if !m.HandleCommand(core.Command{ID: CmdHoistIn}) {
		t.Fatal("CmdHoistIn not handled")
	}
	if m.HoistID() != "alpha-2" || !slices.Equal(m.HoistStack, []string{"alpha", "alpha-2"}) {
		t.Fatalf("nested hoist stack = %v", m.HoistStack)
	}
	if got, want := rowIDs(m.Flatten()), []string{"alpha-2", "alpha-2-a", "alpha-2-b"}; !slices.Equal(got, want) {
		t.Fatalf("hoisted alpha-2 rows = %v, want %v", got, want)
	}

	// HoistPath returns full ancestor chain from root to active hoisted node.
	hp := m.HoistPath()
	if len(hp) != 2 || hp[0].ID != "alpha" || hp[1].ID != "alpha-2" {
		t.Fatalf("HoistPath = %+v, want [alpha, alpha-2]", hp)
	}

	// Nested hoist into alpha-2-a, then Unhoist pops one level keeping alpha-2-a selected.
	if !m.Hoist("alpha-2-a") {
		t.Fatal("Hoist(alpha-2-a) failed")
	}
	hp = m.HoistPath()
	if len(hp) != 3 || hp[0].ID != "alpha" || hp[1].ID != "alpha-2" || hp[2].ID != "alpha-2-a" {
		t.Fatalf("deep HoistPath = %+v", hp)
	}
	if !m.HandleCommand(core.Command{ID: CmdHoistOut}) {
		t.Fatal("CmdHoistOut not handled")
	}
	if m.HoistID() != "alpha-2" || m.SelectedID != "alpha-2-a" {
		t.Fatalf("after Unhoist: hoistID=%q sel=%q, want alpha-2 / alpha-2-a", m.HoistID(), m.SelectedID)
	}

	// ClearHoist returns to full tree keeping alpha-2 selected.
	if !m.HandleCommand(core.Command{ID: CmdHoistClear}) {
		t.Fatal("CmdHoistClear not handled")
	}
	if m.Hoisted() || m.HoistID() != "" || m.SelectedID != "alpha-2" {
		t.Fatalf("after ClearHoist: hoisted=%v id=%q sel=%q", m.Hoisted(), m.HoistID(), m.SelectedID)
	}
}

func TestHoistPreservesSelectionInsideSubtreeAndLeafHoist(t *testing.T) {
	m := New(hoistFixtureNodes())
	m.Select("alpha-2-b")
	if !m.Hoist("alpha-2") {
		t.Fatal("Hoist(alpha-2) failed")
	}
	if m.SelectedID != "alpha-2-b" {
		t.Fatalf("Hoist should preserve valid selection inside subtree: got %q, want alpha-2-b", m.SelectedID)
	}

	// Hoisting a leaf (0 children) keeps it visible and editable at depth 0.
	m.ClearHoist()
	if !m.Hoist("gamma") {
		t.Fatal("Hoist(gamma) failed")
	}
	rows := m.Flatten()
	if got, want := rowIDs(rows), []string{"gamma"}; !slices.Equal(got, want) || rows[0].Depth != 0 {
		t.Fatalf("leaf hoist rows = %v depth=%d", got, rows[0].Depth)
	}
	if !m.StartRename("") {
		t.Fatal("StartRename on hoisted leaf failed")
	}
	m.HandleEditCommand(core.Command{ID: input.CmdInsert, Args: map[string]any{"text": "!"}})
	res, ok := m.CommitEdit()
	if !ok || res.Kind != EditRename || res.TargetID != "gamma" || res.Value != "Gamma!" {
		t.Fatalf("hoisted leaf rename = %+v ok=%v", res, ok)
	}
}

func TestHoistPrunesMissingNodesOnSetNodes(t *testing.T) {
	m := New(hoistFixtureNodes())
	m.Hoist("alpha")
	m.Hoist("alpha-2")

	// Remove alpha-2 while keeping alpha: stack prunes down to ["alpha"].
	trimmed := hoistFixtureNodes()
	trimmed[0].Children = trimmed[0].Children[:1] // only alpha-1 remains
	m.SetNodes(trimmed)
	if !m.Hoisted() || m.HoistID() != "alpha" || !slices.Equal(m.HoistStack, []string{"alpha"}) {
		t.Fatalf("after deleting alpha-2: stack=%v, want [alpha]", m.HoistStack)
	}
	if got, want := rowIDs(m.Flatten()), []string{"alpha", "alpha-1"}; !slices.Equal(got, want) {
		t.Fatalf("visible rows after partial prune = %v, want %v", got, want)
	}

	// Remove alpha entirely: stack clears and full remaining tree renders cleanly.
	m.SetNodes(trimmed[1:])
	if m.Hoisted() || m.HoistID() != "" {
		t.Fatalf("after deleting alpha: hoisted=%v stack=%v", m.Hoisted(), m.HoistStack)
	}
	if got, want := rowIDs(m.Flatten()), []string{"beta", "beta-1", "gamma"}; !slices.Equal(got, want) {
		t.Fatalf("visible rows after full prune = %v, want %v", got, want)
	}
}

func TestHoistedAuthoringInvariants(t *testing.T) {
	m := New(hoistFixtureNodes())
	m.Hoist("alpha-2")
	m.Select("alpha-2")

	// StartAdd(EditAddBelow) on active hoisted root promotes to EditAddChild under alpha-2.
	if !m.StartAdd(EditAddBelow, "+ ") {
		t.Fatal("StartAdd(EditAddBelow) on hoisted root failed")
	}
	e := m.Edit()
	if e == nil || e.Kind != EditAddChild || e.ParentID != "alpha-2" || e.AfterID != "alpha-2-b" || e.Depth != 1 {
		t.Fatalf("promoted edit state = %+v, want EditAddChild under alpha-2 depth 1", e)
	}
	// Draft appears inside hoisted Flatten() at depth 1.
	rows := m.Flatten()
	if got, want := rowIDs(rows), []string{"alpha-2", "alpha-2-a", "alpha-2-a-1", "alpha-2-b", "<draft>"}; !slices.Equal(got, want) {
		t.Fatalf("hoisted draft rows = %v, want %v", got, want)
	}
	if rows[len(rows)-1].Depth != 1 {
		t.Fatalf("draft row depth = %d, want 1", rows[len(rows)-1].Depth)
	}

	// EditOutdent at depth 1 inside hoist clamps and refuses to escape above HoistID.
	if m.EditOutdent() {
		t.Fatal("EditOutdent should clamp at depth 1 under HoistID")
	}
	if e := m.Edit(); e.ParentID != "alpha-2" || e.Depth != 1 {
		t.Fatalf("clamped outdent mutated edit: %+v", e)
	}

	// EditIndent into alpha-2-b (depth 2) succeeds, then EditOutdent returns to depth 1 and clamps there.
	if !m.EditIndent() {
		t.Fatal("EditIndent under alpha-2-b failed")
	}
	if e := m.Edit(); e.ParentID != "alpha-2-b" || e.Depth != 2 {
		t.Fatalf("after indent inside hoist: %+v", e)
	}
	if !m.EditOutdent() {
		t.Fatal("EditOutdent from depth 2 to depth 1 inside hoist should succeed")
	}
	if e := m.Edit(); e.ParentID != "alpha-2" || e.Depth != 1 {
		t.Fatalf("after outdent to depth 1: %+v", e)
	}
	if m.EditOutdent() {
		t.Fatal("EditOutdent should clamp again at depth 1")
	}

	m.HandleEditCommand(core.Command{ID: input.CmdInsert, Args: map[string]any{"text": "Child Item"}})
	res, ok := m.CommitEdit()
	if !ok || res.Kind != EditAddChild || res.ParentID != "alpha-2" || res.Depth != 1 || res.Value != "Child Item" {
		t.Fatalf("CommitEdit = %+v ok=%v", res, ok)
	}
}

func TestDefaultFoldedInitialAndPreservesUserUnfold(t *testing.T) {
	m := New(nil)
	m.DefaultFolded = true

	// Empty SetNodes does not trip foldInitialized.
	m.SetNodes(nil)

	// First non-empty SetNodes folds all collapsible nodes.
	m.SetNodes(hoistFixtureNodes())
	if !m.Folded["alpha"] || !m.Folded["alpha-2"] || !m.Folded["alpha-2-a"] || !m.Folded["beta"] {
		t.Fatalf("DefaultFolded did not collapse all parents: %v", m.Folded)
	}
	if m.Folded["gamma"] {
		t.Fatalf("leaf node should not be folded: %v", m.Folded)
	}
	if got, want := rowIDs(m.Flatten()), []string{"alpha", "beta", "gamma"}; !slices.Equal(got, want) {
		t.Fatalf("initial DefaultFolded rows = %v, want %v", got, want)
	}

	// User unfolds alpha; subsequent SetNodes preserves the user's unfold state.
	m.Select("alpha")
	if !m.Unfold() {
		t.Fatal("Unfold(alpha) failed")
	}
	m.SetNodes(hoistFixtureNodes())
	if m.Folded["alpha"] {
		t.Fatal("subsequent SetNodes should preserve user unfold of alpha")
	}
	if !m.Folded["beta"] {
		t.Fatal("subsequent SetNodes should keep beta folded")
	}

	// ResetFoldInit causes the next non-empty SetNodes to re-apply DefaultFolded.
	m.ResetFoldInit()
	m.SetNodes(hoistFixtureNodes())
	if !m.Folded["alpha"] || !m.Folded["beta"] {
		t.Fatalf("after ResetFoldInit + SetNodes, expected all parents folded: %v", m.Folded)
	}
}

func TestHoistFactsCacheKeyAndSpecs(t *testing.T) {
	m := New(hoistFixtureNodes())
	v0 := m.View("tr")
	if v0.Facts["hoisted"] != false || v0.Facts["hoistDepth"] != 0 || v0.Facts["hoistLabel"] != "" {
		t.Fatalf("unhoisted facts = %+v", v0.Facts)
	}

	m.Hoist("alpha")
	v1 := m.View("tr")
	if v1.Facts["hoisted"] != true || v1.Facts["hoistDepth"] != 1 || v1.Facts["hoistLabel"] != "Alpha" {
		t.Fatalf("hoisted alpha facts = %+v", v1.Facts)
	}
	if v1.CacheKey == v0.CacheKey {
		t.Fatal("View CacheKey did not change when hoisting")
	}

	m.Hoist("alpha-2")
	v2 := m.View("tr")
	if v2.Facts["hoisted"] != true || v2.Facts["hoistDepth"] != 2 || v2.Facts["hoistLabel"] != "Alpha 2" {
		t.Fatalf("nested hoist facts = %+v", v2.Facts)
	}
	if v2.CacheKey == v1.CacheKey {
		t.Fatal("View CacheKey did not change on nested hoist")
	}

	// Even on a single-root tree where uiNodes are identical, hoisting changes CacheKey.
	single := New([]Node{{ID: "only", Label: "Only"}})
	sv0 := single.View("tr")
	single.Hoist("only")
	sv1 := single.View("tr")
	if sv0.CacheKey == sv1.CacheKey {
		t.Fatal("CacheKey must differ between unhoisted and hoisted single-root tree")
	}

	// Copy clones HoistStack independently.
	cp := m.Copy()
	cp.HoistStack[0] = "mutated"
	if m.HoistStack[0] != "alpha" {
		t.Fatalf("Copy did not clone HoistStack: %v", m.HoistStack)
	}

	// Specs includes fold and hoist commands.
	specs := Specs()
	ids := make(map[string]bool, len(specs))
	for _, s := range specs {
		ids[s.ID] = true
	}
	for _, want := range []string{CmdFoldToggle, CmdFoldOpen, CmdFoldClose, CmdFoldOpenAll, CmdFoldCloseAll, CmdHoistIn, CmdHoistOut, CmdHoistClear} {
		if !ids[want] {
			t.Errorf("Specs() missing %q", want)
		}
	}
}
