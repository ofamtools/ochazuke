package traits_test

import (
	"slices"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
)

func TestSelectHandleBoundsPruneAndFacts(t *testing.T) {
	// Include an empty slot ("") to simulate a non-selectable header or draft row.
	ids := []string{"", "a", "b", "c", "d"}
	st := traits.SelectState{}

	// Unknown command is unhandled.
	if _, changed, handled := traits.SelectHandle(st, ids, 1, core.Command{ID: "nope"}); changed || handled {
		t.Fatalf("unknown cmd: changed=%v handled=%v", changed, handled)
	}

	// Mark toggle on empty ID slot is handled but unchanged.
	if next, changed, handled := traits.SelectHandle(st, ids, 0, core.Command{ID: traits.SelectCmdMarkToggle}); !handled || changed || len(next.Marked) != 0 {
		t.Fatalf("mark toggle on empty slot: next=%+v changed=%v handled=%v", next, changed, handled)
	}

	// Mark toggle on "a" marks it without mutating input.
	st1, changed, handled := traits.SelectHandle(st, ids, 1, core.Command{ID: traits.SelectCmdMarkToggle})
	if !handled || !changed || !st1.Marked["a"] || len(st1.Marked) != 1 {
		t.Fatalf("mark toggle on a: st=%+v changed=%v handled=%v", st1, changed, handled)
	}
	if len(st.Marked) != 0 {
		t.Fatal("SelectHandle mutated input Marked map")
	}

	// Toggling "a" again clears it.
	stUn, changed, handled := traits.SelectHandle(st1, ids, 1, core.Command{ID: traits.SelectCmdMarkToggle})
	if !handled || !changed || len(stUn.Marked) != 0 {
		t.Fatalf("unmark a: st=%+v changed=%v handled=%v", stUn, changed, handled)
	}

	// Visual toggle anchors at cursor and computes bounds/range cleanly in both directions.
	stVis, changed, handled := traits.SelectHandle(st1, ids, 3, core.Command{ID: traits.SelectCmdVisualToggle})
	if !handled || !changed || !stVis.VisualActive || stVis.VisualAnchor != 3 {
		t.Fatalf("visual toggle on: st=%+v changed=%v handled=%v", stVis, changed, handled)
	}
	from, to, ok := traits.SelectBounds(stVis, len(ids), 1)
	if !ok || from != 1 || to != 3 {
		t.Fatalf("SelectBounds(anchor=3, cursor=1) = (%d, %d, %v), want (1, 3, true)", from, to, ok)
	}
	if got := traits.SelectRangeIDs(stVis, ids, 0); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("SelectRangeIDs skipping empty slot = %v, want [a b c]", got)
	}

	// Facts while visual is active and cursor is on marked item "a".
	fVis := traits.SelectFacts(stVis, ids, 1)
	if fVis["marked"] != true || fVis["markedCount"] != 1 || fVis["visualActive"] != true ||
		fVis["visualCount"] != 3 || fVis["hasSelection"] != true {
		t.Fatalf("SelectFacts visual: %+v", fVis)
	}

	// SelectCmdMarkToggle while visual is active marks the visual range and exits visual mode.
	stMarked, changed, handled := traits.SelectHandle(stVis, ids, 1, core.Command{ID: traits.SelectCmdMarkToggle})
	if !handled || !changed || stMarked.VisualActive || !stMarked.Marked["a"] || !stMarked.Marked["b"] || !stMarked.Marked["c"] {
		t.Fatalf("mark toggle in visual mode: st=%+v changed=%v handled=%v", stMarked, changed, handled)
	}
	if !stVis.VisualActive || len(stVis.Marked) != 1 {
		t.Fatal("mark toggle in visual mode mutated input state")
	}

	// SelectCmdVisualLine toggles on and SelectCmdVisualMark marks range and exits visual mode.
	stLine, changed, handled := traits.SelectHandle(st, ids, 2, core.Command{ID: traits.SelectCmdVisualLine})
	if !handled || !changed || !stLine.VisualActive || stLine.VisualAnchor != 2 {
		t.Fatalf("visual line on: st=%+v changed=%v handled=%v", stLine, changed, handled)
	}
	stLineMark, changed, handled := traits.SelectHandle(stLine, ids, 4, core.Command{ID: traits.SelectCmdVisualMark})
	if !handled || !changed || stLineMark.VisualActive || len(stLineMark.Marked) != 3 ||
		!stLineMark.Marked["b"] || !stLineMark.Marked["c"] || !stLineMark.Marked["d"] {
		t.Fatalf("visual mark: st=%+v changed=%v handled=%v", stLineMark, changed, handled)
	}

	// SelectCmdVisualClear clears active visual and is a clean no-op when inactive.
	stCleared, changed, handled := traits.SelectHandle(stLine, ids, 2, core.Command{ID: traits.SelectCmdVisualClear})
	if !handled || !changed || stCleared.VisualActive {
		t.Fatalf("visual clear active: st=%+v changed=%v handled=%v", stCleared, changed, handled)
	}
	if _, changed, handled := traits.SelectHandle(stCleared, ids, 2, core.Command{ID: traits.SelectCmdVisualClear}); !handled || changed {
		t.Fatalf("visual clear inactive: changed=%v handled=%v", changed, handled)
	}

	// SelectCmdAll marks all non-empty visible IDs; repeating reports changed=false.
	stAll, changed, handled := traits.SelectHandle(st, ids, 1, core.Command{ID: traits.SelectCmdAll})
	if !handled || !changed || len(stAll.Marked) != 4 || stAll.Marked[""] {
		t.Fatalf("select all: st=%+v changed=%v handled=%v", stAll, changed, handled)
	}
	if _, changed, handled := traits.SelectHandle(stAll, ids, 1, core.Command{ID: traits.SelectCmdAll}); !handled || changed {
		t.Fatalf("repeat select all: changed=%v handled=%v", changed, handled)
	}

	// SelectCmdInvert flips marks across visible IDs.
	stInv, changed, handled := traits.SelectHandle(st1, ids, 1, core.Command{ID: traits.SelectCmdInvert})
	if !handled || !changed || stInv.Marked["a"] || !stInv.Marked["b"] || !stInv.Marked["c"] || !stInv.Marked["d"] || len(stInv.Marked) != 3 {
		t.Fatalf("select invert: st=%+v changed=%v handled=%v", stInv, changed, handled)
	}

	// SelectPrune drops vanished IDs without mutating input.
	pruned := traits.SelectPrune(stAll, []string{"a", "d"})
	if len(pruned.Marked) != 2 || !pruned.Marked["a"] || !pruned.Marked["d"] || pruned.Marked["b"] {
		t.Fatalf("SelectPrune: %+v", pruned)
	}
	if len(stAll.Marked) != 4 {
		t.Fatal("SelectPrune mutated input Marked map")
	}

	// SelectCmdMarkClear clears all marks; repeating reports changed=false.
	stEmpty, changed, handled := traits.SelectHandle(pruned, ids, 1, core.Command{ID: traits.SelectCmdMarkClear})
	if !handled || !changed || len(stEmpty.Marked) != 0 {
		t.Fatalf("mark clear: st=%+v changed=%v handled=%v", stEmpty, changed, handled)
	}
	if _, changed, handled := traits.SelectHandle(stEmpty, ids, 1, core.Command{ID: traits.SelectCmdMarkClear}); !handled || changed {
		t.Fatalf("repeat mark clear: changed=%v handled=%v", changed, handled)
	}

	// Empty list facts and bounds.
	if _, _, ok := traits.SelectBounds(traits.SelectState{VisualActive: true}, 0, 0); ok {
		t.Fatal("SelectBounds on empty list should be false")
	}
	fEmpty := traits.SelectFacts(stEmpty, nil, 0)
	if fEmpty["marked"] != false || fEmpty["markedCount"] != 0 || fEmpty["visualActive"] != false ||
		fEmpty["visualCount"] != 0 || fEmpty["hasSelection"] != false {
		t.Fatalf("empty SelectFacts: %+v", fEmpty)
	}
}

func TestDefaultTraitBindingsSelectAndFoldWithKeymap(t *testing.T) {
	sel := traits.DefaultTraitBindings("normal", traits.TraitSelect)
	if len(sel) != 3 {
		t.Fatalf("DefaultTraitBindings(select) len = %d, want 3", len(sel))
	}
	if !slices.EqualFunc(
		sel,
		traits.DefaultTraitBindings("normal", "trait.select"),
		func(a, b core.Binding) bool {
			return a.Scope == b.Scope && a.Command == b.Command && slices.Equal(a.Keys, b.Keys)
		},
	) {
		t.Error("DefaultTraitBindings should accept both raw and trait.-prefixed select name")
	}

	all := append(slices.Clone(sel), traits.DefaultTraitBindings("normal", traits.TraitFold)...)
	km, err := core.NewKeymap([]core.Mode{{Name: "normal"}}, all, traits.SelectCommandSpecs()...)
	if err != nil {
		t.Fatalf("core.NewKeymap(select+fold): %v", err)
	}

	checks := []struct {
		scope string
		keys  []string
		cmd   string
	}{
		{traits.TraitScope(traits.TraitSelect), []string{"space"}, traits.SelectCmdMarkToggle},
		{traits.TraitScope(traits.TraitSelect), []string{"v"}, traits.SelectCmdVisualToggle},
		{traits.TraitScope(traits.TraitSelect), []string{"V"}, traits.SelectCmdVisualLine},
		{traits.TraitScope(traits.TraitFold), []string{"h"}, "tree.fold.out"},
		{traits.TraitScope(traits.TraitFold), []string{"l"}, "tree.fold.in"},
		{traits.TraitScope(traits.TraitFold), []string{"z", "a"}, "tree.fold.toggle"},
		{traits.TraitScope(traits.TraitFold), []string{"z", "o"}, "tree.fold.open"},
		{traits.TraitScope(traits.TraitFold), []string{"z", "c"}, "tree.fold.close"},
		{traits.TraitScope(traits.TraitFold), []string{"z", "M"}, "tree.fold.close.all"},
		{traits.TraitScope(traits.TraitFold), []string{"z", "R"}, "tree.fold.open.all"},
	}
	for _, tc := range checks {
		chain := []core.ContextFrame{{Scope: tc.scope}}
		var in core.InputState
		var res core.Resolution
		for _, k := range tc.keys {
			res = km.Resolve(core.KeyQuery{Chain: chain, Mode: "normal", Input: in, Key: k})
			in = res.Input
		}
		if res.Outcome != core.Matched || res.Command == nil || res.Command.ID != tc.cmd {
			t.Errorf("scope=%s keys=%v -> %+v, want %s", tc.scope, tc.keys, res, tc.cmd)
		}
	}
}
