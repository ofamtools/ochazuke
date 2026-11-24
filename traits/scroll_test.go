package traits

import (
	"slices"
	"testing"

	"ofam.dev/ochazuke/core"
)

func TestScrollStepMotionsClampingOverscrollHorizontal(t *testing.T) {
	cfg := ScrollConfig{PageOverlap: 2}
	st := ScrollState{OffsetY: 0, ContentH: 50, ViewH: 10, ContentW: 30, ViewW: 20}

	// ctrl+e / motion.scroll.down with count
	next, moved, handled := ScrollStep(st, cfg, core.Command{ID: "motion.scroll.down", Count: 3})
	if !handled || !moved || next.OffsetY != 3 {
		t.Fatalf("scroll.down 3 = (%+v, moved=%v, handled=%v), want OffsetY=3", next, moved, handled)
	}

	// ctrl+y / motion.scroll.up
	next, moved, handled = ScrollStep(next, cfg, core.Command{ID: "motion.scroll.up", Count: 2})
	if !handled || !moved || next.OffsetY != 1 {
		t.Fatalf("scroll.up 2 = (%+v, moved=%v, handled=%v), want OffsetY=1", next, moved, handled)
	}

	// ctrl+d / motion.half.down (ViewH/2 = 5)
	next, moved, handled = ScrollStep(next, cfg, core.Command{ID: "motion.half.down"})
	if !handled || !moved || next.OffsetY != 6 {
		t.Fatalf("half.down = (%+v, moved=%v, handled=%v), want OffsetY=6", next, moved, handled)
	}

	// ctrl+u / motion.half.up
	next, moved, handled = ScrollStep(next, cfg, core.Command{ID: "motion.half.up"})
	if !handled || !moved || next.OffsetY != 1 {
		t.Fatalf("half.up = (%+v, moved=%v, handled=%v), want OffsetY=1", next, moved, handled)
	}

	// ctrl+f / motion.page.down (ViewH - PageOverlap = 8)
	next, moved, handled = ScrollStep(next, cfg, core.Command{ID: "motion.page.down", Count: 2})
	if !handled || !moved || next.OffsetY != 17 {
		t.Fatalf("page.down 2 = (%+v, moved=%v, handled=%v), want OffsetY=17", next, moved, handled)
	}

	// ctrl+b / motion.page.up
	next, moved, handled = ScrollStep(next, cfg, core.Command{ID: "motion.page.up"})
	if !handled || !moved || next.OffsetY != 9 {
		t.Fatalf("page.up = (%+v, moved=%v, handled=%v), want OffsetY=9", next, moved, handled)
	}

	// motion.bottom -> ContentH - ViewH = 40
	next, moved, handled = ScrollStep(next, cfg, core.Command{ID: "motion.bottom"})
	if !handled || !moved || next.OffsetY != 40 {
		t.Fatalf("bottom = (%+v, moved=%v, handled=%v), want OffsetY=40", next, moved, handled)
	}

	// Clamped at bottom: another scroll.down does not move.
	same, moved, handled := ScrollStep(next, cfg, core.Command{ID: "motion.scroll.down"})
	if !handled || moved || same.OffsetY != 40 {
		t.Fatalf("scroll.down at bottom = (%+v, moved=%v, handled=%v), want clamped at 40", same, moved, handled)
	}

	// Overscroll permits scrolling past ContentH-ViewH up to ContentH-1 (49).
	overCfg := ScrollConfig{Overscroll: true}
	over, moved, handled := ScrollStep(next, overCfg, core.Command{ID: "motion.scroll.down", Count: 20})
	if !handled || !moved || over.OffsetY != 49 {
		t.Fatalf("overscroll down = (%+v, moved=%v, handled=%v), want OffsetY=49", over, moved, handled)
	}

	// motion.top -> 0
	top, moved, handled := ScrollStep(over, overCfg, core.Command{ID: "motion.top"})
	if !handled || !moved || top.OffsetY != 0 {
		t.Fatalf("top = (%+v, moved=%v, handled=%v), want OffsetY=0", top, moved, handled)
	}

	// Direct motion.down / motion.up delegation
	d1, moved, handled := ScrollStep(top, cfg, core.Command{ID: "motion.down", Count: 4})
	if !handled || !moved || d1.OffsetY != 4 {
		t.Fatalf("motion.down 4 = (%+v, moved=%v, handled=%v), want OffsetY=4", d1, moved, handled)
	}
	u1, moved, handled := ScrollStep(d1, cfg, core.Command{ID: "motion.up", Count: 10})
	if !handled || !moved || u1.OffsetY != 0 {
		t.Fatalf("motion.up 10 = (%+v, moved=%v, handled=%v), want OffsetY=0", u1, moved, handled)
	}

	// Horizontal scrolling + clamping (ContentW=30, ViewW=20 -> maxOffsetX=10)
	hr, moved, handled := ScrollStep(st, cfg, core.Command{ID: "motion.scroll.right", Count: 15})
	if !handled || !moved || hr.OffsetX != 10 {
		t.Fatalf("scroll.right 15 = (%+v, moved=%v, handled=%v), want OffsetX=10", hr, moved, handled)
	}
	hl, moved, handled := ScrollStep(hr, cfg, core.Command{ID: "motion.scroll.left", Count: 4})
	if !handled || !moved || hl.OffsetX != 6 {
		t.Fatalf("scroll.left 4 = (%+v, moved=%v, handled=%v), want OffsetX=6", hl, moved, handled)
	}

	// Unknown command returns handled=false and leaves state untouched.
	orig := ScrollState{OffsetY: 3, ContentH: 50, ViewH: 10}
	unk, moved, handled := ScrollStep(orig, cfg, core.Command{ID: "other.cmd"})
	if handled || moved || unk != orig {
		t.Fatalf("unknown cmd = (%+v, moved=%v, handled=%v)", unk, moved, handled)
	}
}

func TestScrollStepTowardRule1BDownwardTrace(t *testing.T) {
	// Rule 1B trace: ViewH=10, ContentH=50, A at Y=0 H=4, E at Y=32 H=6.
	cfg := ScrollConfig{}
	st := ScrollState{OffsetY: 0, ContentH: 50, ViewH: 10}
	targetE := ItemSpan{ID: "E", Y: 32, H: 6}

	type stepExpect struct {
		offsetY       int
		scrolled      bool
		focusTarget   bool
		targetVisible bool
		halfStepped   bool
	}
	want := []stepExpect{
		{offsetY: 5, scrolled: true, focusTarget: false, targetVisible: false, halfStepped: true},  // Press 1: [5,15)
		{offsetY: 10, scrolled: true, focusTarget: false, targetVisible: false, halfStepped: true}, // Press 2: [10,20)
		{offsetY: 15, scrolled: true, focusTarget: false, targetVisible: false, halfStepped: true}, // Press 3: [15,25)
		{offsetY: 20, scrolled: true, focusTarget: false, targetVisible: false, halfStepped: true}, // Press 4: [20,30)
		{offsetY: 25, scrolled: true, focusTarget: false, targetVisible: true, halfStepped: true},  // Press 5: [25,35), E peeking!
		{offsetY: 28, scrolled: true, focusTarget: true, targetVisible: true, halfStepped: false},  // Press 6: E visible -> focus E, reveal [28,38)
	}

	for i, w := range want {
		res := ScrollStepToward(st, cfg, targetE)
		if res.State.OffsetY != w.offsetY ||
			res.Scrolled != w.scrolled ||
			res.FocusTarget != w.focusTarget ||
			res.TargetVisible != w.targetVisible ||
			res.HalfStepped != w.halfStepped {
			t.Fatalf("press %d: got %+v, want %+v", i+1, res, w)
		}
		st = res.State
	}
}

func TestScrollStepTowardRule1BUpwardTrace(t *testing.T) {
	// Upward trace from OffsetY=30 back to A at Y=0, H=4 (ViewH=10, ContentH=50).
	cfg := ScrollConfig{}
	st := ScrollState{OffsetY: 30, ContentH: 50, ViewH: 10}
	targetA := ItemSpan{ID: "A", Y: 0, H: 4}

	// Offsets: 30 -> 25 -> 20 -> 15 -> 10 -> 5 -> 0 (A [0,4) visible at OffsetY=0!)
	wantOffsets := []int{25, 20, 15, 10, 5, 0}
	for i, wantOff := range wantOffsets {
		res := ScrollStepToward(st, cfg, targetA)
		wantVis := wantOff == 0
		if res.State.OffsetY != wantOff || !res.Scrolled || res.FocusTarget || !res.HalfStepped || res.TargetVisible != wantVis {
			t.Fatalf("upward press %d: got %+v (want OffsetY=%d, TargetVisible=%v)", i+1, res, wantOff, wantVis)
		}
		st = res.State
	}

	// Next press: A is visible in [0,10) -> focuses A with no further scroll needed.
	final := ScrollStepToward(st, cfg, targetA)
	if !final.FocusTarget || !final.TargetVisible || final.HalfStepped || final.Scrolled || final.State.OffsetY != 0 {
		t.Fatalf("upward final press: got %+v", final)
	}
}

func TestScrollStepTowardTallItemDirectPolicyAndBounds(t *testing.T) {
	// Tall item (H > ViewH): ScrollReveal aligns top of item so top is never cut off.
	st := ScrollState{OffsetY: 0, ContentH: 50, ViewH: 10}
	tall := ItemSpan{ID: "big", Y: 20, H: 15}
	rev := ScrollReveal(st, ScrollConfig{ScrollOff: 2}, tall)
	if rev.OffsetY != 20 {
		t.Fatalf("tall item reveal OffsetY = %d, want 20", rev.OffsetY)
	}

	// Opt-out LongJumpDirect teleports and reveals in 1 step.
	direct := ScrollStepToward(st, ScrollConfig{LongJumpPolicy: LongJumpDirect}, ItemSpan{ID: "E", Y: 32, H: 6})
	if !direct.FocusTarget || !direct.TargetVisible || direct.HalfStepped || !direct.Scrolled || direct.State.OffsetY != 28 {
		t.Fatalf("LongJumpDirect = %+v, want OffsetY=28 FocusTarget=true HalfStepped=false", direct)
	}

	// Boundary non-deadlock: viewport already pinned at bottom, target out-of-range below -> FocusTarget=true.
	pinned := ScrollState{OffsetY: 40, ContentH: 50, ViewH: 10}
	res := ScrollStepToward(pinned, ScrollConfig{}, ItemSpan{ID: "beyond", Y: 55, H: 2})
	if !res.FocusTarget || res.Scrolled || res.HalfStepped || res.State.OffsetY != 40 {
		t.Fatalf("pinned boundary must not deadlock: got %+v", res)
	}

	// Non-overflowing content immediately focuses target.
	short := ScrollState{OffsetY: 0, ContentH: 8, ViewH: 10}
	shortRes := ScrollStepToward(short, ScrollConfig{}, ItemSpan{ID: "x", Y: 5, H: 2})
	if !shortRes.FocusTarget || !shortRes.TargetVisible || shortRes.Scrolled || shortRes.HalfStepped {
		t.Fatalf("non-overflowing content: got %+v", shortRes)
	}
}

func TestScrollFactsAndHalfStepConfig(t *testing.T) {
	cfg := ScrollConfig{HalfPageStep: 7}
	st := ScrollState{OffsetY: 0, ContentH: 30, ViewH: 10, ContentW: 40, ViewW: 20}
	facts := ScrollFacts(st, cfg)
	if facts["atTop"] != true || facts["atBottom"] != false || facts["atLeft"] != true || facts["atRight"] != false ||
		facts["scrollable"] != true || facts["offsetY"] != 0 || facts["offsetX"] != 0 ||
		facts["contentH"] != 30 || facts["viewH"] != 10 || facts["maxOffsetY"] != 20 || facts["halfStep"] != 7 {
		t.Fatalf("unexpected ScrollFacts at top: %+v", facts)
	}

	st.OffsetY, st.OffsetX = 99, 99
	factsBot := ScrollFacts(st, cfg)
	if factsBot["atTop"] != false || factsBot["atBottom"] != true || factsBot["atLeft"] != false || factsBot["atRight"] != true ||
		factsBot["offsetY"] != 20 || factsBot["offsetX"] != 20 {
		t.Fatalf("unexpected ScrollFacts at bottom-right: %+v", factsBot)
	}
}

func TestTraitScopeAndDefaultTraitBindings(t *testing.T) {
	if got := TraitScope("scroll"); got != "trait.scroll" {
		t.Errorf("TraitScope(scroll) = %q", got)
	}
	if got := TraitScope("trait.scroll"); got != "trait.scroll" {
		t.Errorf("TraitScope(trait.scroll) = %q", got)
	}
	if got := TraitScope(""); got != "" {
		t.Errorf("TraitScope('') = %q", got)
	}

	var all []core.Binding
	for _, tr := range []string{TraitScroll, TraitNav, TraitFold, TraitFilter} {
		bs := DefaultTraitBindings("normal", tr)
		if len(bs) == 0 {
			t.Fatalf("DefaultTraitBindings(%q) returned empty", tr)
		}
		for _, b := range bs {
			if b.Scope != TraitScope(tr) {
				t.Errorf("%s binding scope = %q, want %q", tr, b.Scope, TraitScope(tr))
			}
			if len(b.Keys) > 0 && b.Keys[0] == "g" {
				t.Errorf("%s binding must not use g-prefix: %+v", tr, b)
			}
		}
		all = append(all, bs...)
	}
	if got := DefaultTraitBindings("normal", "unknown"); got != nil {
		t.Errorf("unknown trait bindings = %v, want nil", got)
	}

	km, err := core.NewKeymap([]core.Mode{{Name: "normal"}}, all)
	if err != nil {
		t.Fatalf("core.NewKeymap(DefaultTraitBindings): %v", err)
	}

	// Verify key resolution for each trait scope.
	checks := []struct {
		scope string
		keys  []string
		cmd   string
	}{
		{TraitScope(TraitScroll), []string{"ctrl+d"}, "motion.half.down"},
		{TraitScope(TraitNav), []string{"T"}, "motion.top"},
		{TraitScope(TraitNav), []string{"G"}, "motion.bottom"},
		{TraitScope(TraitFold), []string{"space"}, "tree.fold.toggle"},
		{TraitScope(TraitFold), []string{"z", "M"}, "tree.fold.close.all"},
		{TraitScope(TraitFilter), []string{"/"}, "filter.start"},
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

	// Sanity check trait-prefixed input gives identical result.
	if !slices.EqualFunc(
		DefaultTraitBindings("normal", TraitScroll),
		DefaultTraitBindings("normal", "trait.scroll"),
		func(a, b core.Binding) bool {
			return a.Scope == b.Scope && a.Command == b.Command && slices.Equal(a.Keys, b.Keys)
		},
	) {
		t.Error("DefaultTraitBindings should accept both raw and trait.-prefixed names")
	}
}
