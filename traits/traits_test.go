package traits_test

import (
	"encoding/json"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
)

func TestViewportClampAndReveal(t *testing.T) {
	t.Run("empty and zero height", func(t *testing.T) {
		st := traits.ViewportClamp(traits.ViewportState{Cursor: 5, Offset: 3, Total: 0, Height: 10, ScrollX: 4}, traits.ViewportConfig{})
		if st.Cursor != 0 || st.Offset != 0 || st.ScrollX != 0 {
			t.Fatalf("empty clamp: %+v", st)
		}
		st = traits.ViewportClamp(traits.ViewportState{Cursor: 5, Offset: 3, Total: 10, Height: 0}, traits.ViewportConfig{})
		if st.Cursor != 5 || st.Offset != 0 {
			t.Fatalf("zero height cursor mode clamp: %+v", st)
		}
		st = traits.ViewportClamp(traits.ViewportState{Offset: 5, Total: 10, Height: 0}, traits.ViewportConfig{Mode: traits.ViewportModeOffset})
		if st.Offset != 0 {
			t.Fatalf("zero height offset mode clamp: %+v", st)
		}
	})

	t.Run("cursor mode scrollOff", func(t *testing.T) {
		cfg := traits.ViewportConfig{ScrollOff: 2, Mode: traits.ViewportModeCursor}
		st := traits.ViewportClamp(traits.ViewportState{Cursor: 8, Offset: 0, Total: 20, Height: 10}, cfg)
		if st.Cursor != 8 || st.Offset != 1 {
			t.Fatalf("scrollOff down: %+v, want cursor=8 offset=1", st)
		}
		// Moving freely up inside viewport does not shift offset until hitting top padding.
		st.Cursor = 3
		st = traits.ViewportClamp(st, cfg)
		if st.Offset != 1 {
			t.Fatalf("free movement inside window shifted offset: %+v", st)
		}
		st.Cursor = 2
		st = traits.ViewportClamp(st, cfg)
		if st.Offset != 0 {
			t.Fatalf("crossing top padding: %+v, want offset=0", st)
		}
	})

	t.Run("offset mode clamp and overscroll", func(t *testing.T) {
		cfg := traits.ViewportConfig{Mode: traits.ViewportModeOffset}
		st := traits.ViewportClamp(traits.ViewportState{Offset: 50, Total: 20, Height: 8}, cfg)
		if st.Offset != 12 {
			t.Fatalf("offset clamp = %d, want 12", st.Offset)
		}
		st = traits.ViewportClamp(traits.ViewportState{Offset: -5, Total: 20, Height: 8}, cfg)
		if st.Offset != 0 {
			t.Fatalf("negative offset clamp = %d, want 0", st.Offset)
		}
		cfgOver := traits.ViewportConfig{Mode: traits.ViewportModeOffset, Overscroll: true}
		st = traits.ViewportClamp(traits.ViewportState{Offset: 50, Total: 20, Height: 8}, cfgOver)
		if st.Offset != 19 {
			t.Fatalf("overscroll clamp = %d, want 19", st.Offset)
		}
	})

	t.Run("horizontal clamp", func(t *testing.T) {
		st := traits.ViewportClamp(traits.ViewportState{Total: 5, Height: 5, ScrollX: 50, ContentWidth: 100, ViewWidth: 40}, traits.ViewportConfig{})
		if st.ScrollX != 50 {
			t.Fatalf("in-bounds ScrollX = %d, want 50", st.ScrollX)
		}
		st = traits.ViewportClamp(traits.ViewportState{Total: 5, Height: 5, ScrollX: 80, ContentWidth: 100, ViewWidth: 40}, traits.ViewportConfig{})
		if st.ScrollX != 60 {
			t.Fatalf("clamped ScrollX = %d, want 60", st.ScrollX)
		}
		st = traits.ViewportClamp(traits.ViewportState{Total: 5, Height: 5, ScrollX: 5, ContentWidth: 30, ViewWidth: 40}, traits.ViewportConfig{})
		if st.ScrollX != 0 {
			t.Fatalf("fits width ScrollX = %d, want 0", st.ScrollX)
		}
	})

	t.Run("reveal", func(t *testing.T) {
		cfg := traits.ViewportConfig{Mode: traits.ViewportModeOffset, ScrollOff: 1}
		st := traits.ViewportState{Offset: 0, Total: 20, Height: 6}
		st = traits.ViewportReveal(st, cfg, 10)
		// targetRow=10 with scrollOff=1 -> 10 - 6 + 1 + 1 = 6
		if st.Offset != 6 {
			t.Fatalf("reveal below: offset = %d, want 6", st.Offset)
		}
		// Already in view (6+1..6+6-1-1 = 7..10): no change.
		st2 := traits.ViewportReveal(st, cfg, 8)
		if st2.Offset != 6 {
			t.Fatalf("reveal already visible: offset = %d, want 6", st2.Offset)
		}
		// Reveal above: targetRow=2 with scrollOff=1 -> 1.
		st3 := traits.ViewportReveal(st, cfg, 2)
		if st3.Offset != 1 {
			t.Fatalf("reveal above: offset = %d, want 1", st3.Offset)
		}
	})
}

func TestViewportStepCursorMode(t *testing.T) {
	cfg := traits.ViewportConfig{ScrollOff: 1, PageOverlap: 2}
	st := traits.ViewportState{Cursor: 0, Offset: 0, Total: 30, Height: 10}

	step := func(id string, count int) (bool, bool) {
		var moved, handled bool
		st, moved, handled = traits.ViewportStep(st, cfg, core.Command{ID: id, Count: count})
		return moved, handled
	}

	if moved, handled := step("unknown.cmd", 0); moved || handled {
		t.Fatalf("unknown command: moved=%v handled=%v", moved, handled)
	}
	if moved, handled := step("motion.up", 1); moved || !handled || st.Cursor != 0 {
		t.Fatalf("up at top: moved=%v handled=%v st=%+v", moved, handled, st)
	}
	if moved, handled := step("motion.down", 3); !moved || !handled || st.Cursor != 3 {
		t.Fatalf("down x3: moved=%v handled=%v st=%+v", moved, handled, st)
	}
	// PageOverlap=2 with Height=10 -> pageStep=8. 3 + 8 = 11.
	if moved, handled := step("motion.page.down", 1); !moved || !handled || st.Cursor != 11 {
		t.Fatalf("page down with overlap: moved=%v handled=%v st=%+v", moved, handled, st)
	}
	if st.Offset != 3 { // cursor 11, height 10, scrollOff 1 -> 11 - 10 + 1 + 1 = 3
		t.Fatalf("page down offset = %d, want 3", st.Offset)
	}
	// Half up: height/2 = 5 -> 11 - 5 = 6.
	if moved, handled := step("motion.half.up", 1); !moved || !handled || st.Cursor != 6 {
		t.Fatalf("half up: moved=%v handled=%v st=%+v", moved, handled, st)
	}
	if moved, handled := step("motion.half.down", 2); !moved || !handled || st.Cursor != 16 {
		t.Fatalf("half down x2: moved=%v handled=%v st=%+v", moved, handled, st)
	}
	if moved, handled := step("motion.page.up", 1); !moved || !handled || st.Cursor != 8 {
		t.Fatalf("page up: moved=%v handled=%v st=%+v", moved, handled, st)
	}
	if moved, handled := step("motion.bottom", 0); !moved || !handled || st.Cursor != 29 || st.Offset != 20 {
		t.Fatalf("bottom: moved=%v handled=%v st=%+v", moved, handled, st)
	}
	if moved, handled := step("motion.top", 0); !moved || !handled || st.Cursor != 0 || st.Offset != 0 {
		t.Fatalf("top: moved=%v handled=%v st=%+v", moved, handled, st)
	}
	// Scroll down shifts offset and nudges cursor inside padding (offset=2 -> lo=3).
	if moved, handled := step("motion.scroll.down", 2); !moved || !handled || st.Offset != 2 || st.Cursor != 3 {
		t.Fatalf("scroll down: moved=%v handled=%v st=%+v", moved, handled, st)
	}
	// Scroll up shifts offset back; interior cursor stays fixed.
	st.Cursor = 7
	if moved, handled := step("motion.scroll.up", 1); !moved || !handled || st.Offset != 1 || st.Cursor != 7 {
		t.Fatalf("scroll up interior cursor: moved=%v handled=%v st=%+v", moved, handled, st)
	}

	// Wrap mode.
	wrapCfg := traits.ViewportConfig{Wrap: true}
	wst := traits.ViewportState{Cursor: 0, Total: 4, Height: 4}
	wst, moved, handled := traits.ViewportStep(wst, wrapCfg, core.Command{ID: "motion.up"})
	if !moved || !handled || wst.Cursor != 3 {
		t.Fatalf("wrap up: moved=%v handled=%v st=%+v", moved, handled, wst)
	}
	wst, moved, handled = traits.ViewportStep(wst, wrapCfg, core.Command{ID: "motion.down"})
	if !moved || !handled || wst.Cursor != 0 {
		t.Fatalf("wrap down: moved=%v handled=%v st=%+v", moved, handled, wst)
	}

	// Empty list handles motion without moving.
	est, moved, handled := traits.ViewportStep(traits.ViewportState{Total: 0, Height: 10}, cfg, core.Command{ID: "motion.down"})
	if moved || !handled || est.Cursor != 0 || est.Offset != 0 {
		t.Fatalf("empty down: moved=%v handled=%v st=%+v", moved, handled, est)
	}
}

func TestViewportStepOffsetMode(t *testing.T) {
	cfg := traits.ViewportConfig{Mode: traits.ViewportModeOffset, PageOverlap: 1}
	st := traits.ViewportState{Offset: 0, Total: 20, Height: 6}

	step := func(id string, count int) bool {
		var moved, handled bool
		st, moved, handled = traits.ViewportStep(st, cfg, core.Command{ID: id, Count: count})
		if !handled {
			t.Fatalf("%s not handled", id)
		}
		return moved
	}

	if !step("motion.down", 3) || st.Offset != 3 {
		t.Fatalf("down x3: %+v", st)
	}
	if !step("motion.scroll.down", 2) || st.Offset != 5 {
		t.Fatalf("scroll down x2: %+v", st)
	}
	// pageStep = 6 - 1 = 5 -> 5 + 5 = 10.
	if !step("motion.page.down", 1) || st.Offset != 10 {
		t.Fatalf("page down: %+v", st)
	}
	// bottom -> max(0, 20-6) = 14.
	if !step("motion.bottom", 0) || st.Offset != 14 {
		t.Fatalf("bottom: %+v", st)
	}
	if step("motion.down", 5) || st.Offset != 14 {
		t.Fatalf("down at bottom should not move: %+v", st)
	}
	if !step("motion.half.up", 1) || st.Offset != 11 {
		t.Fatalf("half up: %+v", st)
	}
	if !step("motion.scroll.up", 2) || st.Offset != 9 {
		t.Fatalf("scroll up: %+v", st)
	}
	if !step("motion.page.up", 1) || st.Offset != 4 {
		t.Fatalf("page up: %+v", st)
	}
	if !step("motion.top", 0) || st.Offset != 0 {
		t.Fatalf("top: %+v", st)
	}

	// Offset mode with Wrap.
	wrapCfg := traits.ViewportConfig{Mode: traits.ViewportModeOffset, Wrap: true}
	wst := traits.ViewportState{Offset: 0, Total: 10, Height: 6} // maxOff = 4
	wst, moved, _ := traits.ViewportStep(wst, wrapCfg, core.Command{ID: "motion.up"})
	if !moved || wst.Offset != 4 {
		t.Fatalf("offset wrap up: moved=%v st=%+v", moved, wst)
	}
	wst, moved, _ = traits.ViewportStep(wst, wrapCfg, core.Command{ID: "motion.down"})
	if !moved || wst.Offset != 0 {
		t.Fatalf("offset wrap down: moved=%v st=%+v", moved, wst)
	}
}

func TestViewportFactsAndMerge(t *testing.T) {
	// Cursor mode top/middle/bottom + horizontal.
	cfgC := traits.ViewportConfig{Mode: traits.ViewportModeCursor}
	fTop := traits.ViewportFacts(traits.ViewportState{Cursor: 0, Offset: 0, Total: 10, Height: 4, ScrollX: 0, ContentWidth: 80, ViewWidth: 40}, cfgC)
	if fTop["atTop"] != true || fTop["atBottom"] != false || fTop["atLeft"] != true || fTop["atRight"] != false ||
		fTop["empty"] != false || fTop["scrollable"] != true || fTop["cursor"] != 0 || fTop["offset"] != 0 ||
		fTop["total"] != 10 || fTop["height"] != 4 {
		t.Fatalf("cursor top facts: %+v", fTop)
	}
	fBot := traits.ViewportFacts(traits.ViewportState{Cursor: 9, Offset: 6, Total: 10, Height: 4, ScrollX: 40, ContentWidth: 80, ViewWidth: 40}, cfgC)
	if fBot["atTop"] != false || fBot["atBottom"] != true || fBot["atLeft"] != false || fBot["atRight"] != true {
		t.Fatalf("cursor bottom facts: %+v", fBot)
	}

	// Offset mode.
	cfgO := traits.ViewportConfig{Mode: traits.ViewportModeOffset}
	fOffMid := traits.ViewportFacts(traits.ViewportState{Offset: 2, Total: 10, Height: 4}, cfgO)
	if fOffMid["atTop"] != false || fOffMid["atBottom"] != false {
		t.Fatalf("offset mid facts: %+v", fOffMid)
	}
	fOffBot := traits.ViewportFacts(traits.ViewportState{Offset: 6, Total: 10, Height: 4}, cfgO)
	if fOffBot["atTop"] != false || fOffBot["atBottom"] != true {
		t.Fatalf("offset bottom facts: %+v", fOffBot)
	}

	// Empty state.
	fEmpty := traits.ViewportFacts(traits.ViewportState{Total: 0, Height: 4}, cfgC)
	if fEmpty["empty"] != true || fEmpty["atTop"] != true || fEmpty["atBottom"] != true || fEmpty["scrollable"] != false {
		t.Fatalf("empty facts: %+v", fEmpty)
	}

	// Small non-scrollable list.
	fSmall := traits.ViewportFacts(traits.ViewportState{Cursor: 1, Total: 3, Height: 10}, cfgC)
	if fSmall["scrollable"] != false || fSmall["atTop"] != false || fSmall["atBottom"] != false {
		t.Fatalf("small facts: %+v", fSmall)
	}

	// MergeFacts allocates nil dst and merges cleanly.
	merged := traits.MergeFacts(nil, map[string]any{"a": 1, "b": 2})
	merged = traits.MergeFacts(merged, map[string]any{"b": 3, "c": 4})
	if merged["a"] != 1 || merged["b"] != 3 || merged["c"] != 4 {
		t.Fatalf("MergeFacts: %+v", merged)
	}
}

func TestResolveSpill(t *testing.T) {
	allOn := traits.SpillConfig{Top: true, Bottom: true, Left: true, Right: true}
	actor := core.Actor{Kind: "user", ID: "u1"}

	cases := []struct {
		name      string
		facts     map[string]any
		cfg       traits.SpillConfig
		cmdID     string
		wantID    string
		wantSpill bool
	}{
		{"top spills", map[string]any{"atTop": true}, allOn, "motion.up", core.CmdFocusUp, true},
		{"bottom spills", map[string]any{"atBottom": true}, allOn, "motion.down", core.CmdFocusDown, true},
		{"left spills", map[string]any{"atLeft": true}, allOn, "motion.left", core.CmdFocusLeft, true},
		{"right spills", map[string]any{"atRight": true}, allOn, "motion.right", core.CmdFocusRight, true},
		{"top locked", map[string]any{"atTop": true}, traits.SpillConfig{Bottom: true}, "motion.up", "motion.up", false},
		{"bottom locked", map[string]any{"atBottom": true}, traits.SpillConfig{Top: true}, "motion.down", "motion.down", false},
		{"left locked", map[string]any{"atLeft": true}, traits.SpillConfig{Right: true}, "motion.left", "motion.left", false},
		{"right locked", map[string]any{"atRight": true}, traits.SpillConfig{Left: true}, "motion.right", "motion.right", false},
		{"not at edge", map[string]any{"atTop": false, "atBottom": false}, allOn, "motion.up", "motion.up", false},
		{"non-directional motion", map[string]any{"atTop": true}, allOn, "motion.top", "motion.top", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := core.Command{ID: tc.cmdID, Count: 2, Source: "key:k", Actor: actor}
			out, spilled := traits.ResolveSpill(tc.facts, tc.cfg, in)
			if spilled != tc.wantSpill || out.ID != tc.wantID {
				t.Fatalf("got (%q, %v), want (%q, %v)", out.ID, spilled, tc.wantID, tc.wantSpill)
			}
			if out.Count != in.Count || out.Source != in.Source || out.Actor != in.Actor {
				t.Fatalf("metadata not preserved: %+v", out)
			}
		})
	}
}

func TestResolveDismiss(t *testing.T) {
	verbs := traits.DismissVerbs{
		StopEdit:     "form.stop",
		StopFilter:   "picker.filter.stop",
		ClearQuery:   "picker.filter.clear",
		ClearVisual:  "table.visual.clear",
		FocusPrimary: "palette.focus.list",
		Close:        "modal.cancel",
	}

	t.Run("1-step close", func(t *testing.T) {
		cfg := traits.DismissConfig{Steps: []string{traits.DismissClose}}
		id, ok := traits.ResolveDismiss(map[string]any{"filtering": true, "editing": true}, cfg, verbs)
		if !ok || id != "modal.cancel" {
			t.Fatalf("1-step close = (%q, %v)", id, ok)
		}
	})

	t.Run("2-step filter then close", func(t *testing.T) {
		cfg := traits.DismissConfig{Steps: []string{traits.DismissStopFilter, traits.DismissClose}}
		if id, ok := traits.ResolveDismiss(map[string]any{"filtering": true}, cfg, verbs); !ok || id != "picker.filter.stop" {
			t.Fatalf("step 1 = (%q, %v)", id, ok)
		}
		if id, ok := traits.ResolveDismiss(map[string]any{"filtering": false}, cfg, verbs); !ok || id != "modal.cancel" {
			t.Fatalf("step 2 = (%q, %v)", id, ok)
		}
	})

	t.Run("3-step stop_filter -> clear_query -> close", func(t *testing.T) {
		cfg := traits.DismissConfig{Steps: []string{traits.DismissStopFilter, traits.DismissClearQuery, traits.DismissClose}}
		if id, ok := traits.ResolveDismiss(map[string]any{"filtering": true, "queryEmpty": false}, cfg, verbs); !ok || id != "picker.filter.stop" {
			t.Fatalf("filtering active = (%q, %v)", id, ok)
		}
		if id, ok := traits.ResolveDismiss(map[string]any{"filtering": false, "queryEmpty": false}, cfg, verbs); !ok || id != "picker.filter.clear" {
			t.Fatalf("query non-empty = (%q, %v)", id, ok)
		}
		if id, ok := traits.ResolveDismiss(map[string]any{"filtering": false, "hasFilter": true}, cfg, verbs); !ok || id != "picker.filter.clear" {
			t.Fatalf("hasFilter true = (%q, %v)", id, ok)
		}
		if id, ok := traits.ResolveDismiss(map[string]any{"filtering": false, "queryEmpty": true}, cfg, verbs); !ok || id != "modal.cancel" {
			t.Fatalf("query empty = (%q, %v)", id, ok)
		}
	})

	t.Run("edit -> close", func(t *testing.T) {
		cfg := traits.DismissConfig{Steps: []string{traits.DismissStopEdit, traits.DismissClose}}
		if id, ok := traits.ResolveDismiss(map[string]any{"editing": true}, cfg, verbs); !ok || id != "form.stop" {
			t.Fatalf("editing = (%q, %v)", id, ok)
		}
		if id, ok := traits.ResolveDismiss(map[string]any{"editing": false}, cfg, verbs); !ok || id != "modal.cancel" {
			t.Fatalf("not editing = (%q, %v)", id, ok)
		}
	})

	t.Run("visual and focus_primary", func(t *testing.T) {
		cfg := traits.DismissConfig{Steps: []string{traits.DismissClearVisual, traits.DismissFocusPrimary, traits.DismissClose}}
		if id, ok := traits.ResolveDismiss(map[string]any{"visualActive": true}, cfg, verbs); !ok || id != "table.visual.clear" {
			t.Fatalf("visualActive = (%q, %v)", id, ok)
		}
		if id, ok := traits.ResolveDismiss(map[string]any{"focusSecondary": true}, cfg, verbs); !ok || id != "palette.focus.list" {
			t.Fatalf("focusSecondary = (%q, %v)", id, ok)
		}
		if id, ok := traits.ResolveDismiss(nil, cfg, verbs); !ok || id != "modal.cancel" {
			t.Fatalf("fallback close = (%q, %v)", id, ok)
		}
	})

	t.Run("empty config and default verbs", func(t *testing.T) {
		if id, ok := traits.ResolveDismiss(map[string]any{"editing": true}, traits.DismissConfig{}, traits.DismissVerbs{}); ok || id != "" {
			t.Fatalf("empty steps = (%q, %v)", id, ok)
		}
		if id, ok := traits.ResolveDismiss(map[string]any{"editing": true}, traits.DismissConfig{Steps: []string{traits.DismissStopEdit}}, traits.DismissVerbs{}); !ok || id == "" {
			t.Fatalf("default verb failed = (%q, %v)", id, ok)
		}
	})

	t.Run("JSON round-trip", func(t *testing.T) {
		in := struct {
			V traits.ViewportConfig `json:"viewport"`
			S traits.SpillConfig    `json:"spill"`
			D traits.DismissConfig  `json:"dismiss"`
		}{
			V: traits.ViewportConfig{ScrollOff: 3, PageOverlap: 1, Wrap: true, Mode: traits.ViewportModeCursor},
			S: traits.SpillConfig{Top: true, Left: true},
			D: traits.DismissConfig{Steps: []string{traits.DismissStopFilter, traits.DismissClose}},
		}
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out struct {
			V traits.ViewportConfig `json:"viewport"`
			S traits.SpillConfig    `json:"spill"`
			D traits.DismissConfig  `json:"dismiss"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if out.V != in.V || out.S != in.S || len(out.D.Steps) != 2 || out.D.Steps[0] != traits.DismissStopFilter {
			t.Fatalf("round-trip mismatch: got %+v, want %+v", out, in)
		}
	})
}
