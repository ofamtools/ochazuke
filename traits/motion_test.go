package traits

import (
	"encoding/json"
	"slices"
	"testing"

	"ofam.dev/ochazuke/core"
)

func markdownFixture() []MotionNode {
	return []MotionNode{
		{ID: "h1", Span: ItemSpan{ID: "h1", Y: 0, H: 2}, Objects: map[string]string{ObjectSection: "sec-1", ObjectBlock: "h1"}},
		{ID: "b1", Span: ItemSpan{ID: "b1", Y: 2, H: 1}, Objects: map[string]string{ObjectSection: "sec-1", ObjectBlock: "list-1"}},
		{ID: "b1a", Span: ItemSpan{ID: "b1a", Y: 3, H: 1}, Objects: map[string]string{ObjectSection: "sec-1", ObjectBlock: "list-1"}},
		{ID: "code", Span: ItemSpan{ID: "code", Y: 4, H: 4}, Objects: map[string]string{ObjectSection: "sec-1", ObjectBlock: "list-1"}},
		{ID: "b2", Span: ItemSpan{ID: "b2", Y: 8, H: 1}, Objects: map[string]string{ObjectSection: "sec-1", ObjectBlock: "list-1"}},
		{ID: "p1", Span: ItemSpan{ID: "p1", Y: 32, H: 6}, Objects: map[string]string{ObjectSection: "sec-1", ObjectBlock: "p1"}},
		{ID: "h2", Span: ItemSpan{ID: "h2", Y: 38, H: 2}, Objects: map[string]string{ObjectSection: "sec-2", ObjectBlock: "h2"}},
		{ID: "p2", Span: ItemSpan{ID: "p2", Y: 40, H: 4}, Objects: map[string]string{ObjectSection: "sec-2", ObjectBlock: "p2"}},
	}
}

func TestMotionMarkdownObjects(t *testing.T) {
	nodes := markdownFixture()

	// NextObject(block): Heading (0) -> Bullet 1 (1) -> skips nested items -> Paragraph (5).
	got, ok := NextObject(nodes, 0, ObjectBlock, 1, false)
	if !ok || got != 1 {
		t.Fatalf("NextObject(block) from Heading = (%d, %v), want (1, true)", got, ok)
	}
	got, ok = NextObject(nodes, 1, ObjectBlock, 1, false)
	if !ok || got != 5 {
		t.Fatalf("NextObject(block) from Bullet 1 = (%d, %v), want (5, true)", got, ok)
	}
	// From Nested Bullet 1a (2) also skips to Paragraph (5).
	got, ok = NextObject(nodes, 2, ObjectBlock, 1, false)
	if !ok || got != 5 {
		t.Fatalf("NextObject(block) from Nested Bullet 1a = (%d, %v), want (5, true)", got, ok)
	}
	// At last block (Paragraph 2, index 7) with wrap=false -> no move.
	got, ok = NextObject(nodes, 7, ObjectBlock, 1, false)
	if ok || got != 7 {
		t.Fatalf("NextObject(block) at end = (%d, %v), want (7, false)", got, ok)
	}

	// PrevObject(block): Bullet 2 (4) -> Bullet 1 (1, start of list-1) -> Heading (0).
	got, ok = PrevObject(nodes, 4, ObjectBlock, 1, false)
	if !ok || got != 1 {
		t.Fatalf("PrevObject(block) from Bullet 2 = (%d, %v), want (1, true)", got, ok)
	}
	got, ok = PrevObject(nodes, 1, ObjectBlock, 1, false)
	if !ok || got != 0 {
		t.Fatalf("PrevObject(block) from Bullet 1 = (%d, %v), want (0, true)", got, ok)
	}
	// PrevObject(block) from Paragraph (5) lands on start of list-1 (1), not end (4).
	got, ok = PrevObject(nodes, 5, ObjectBlock, 1, false)
	if !ok || got != 1 {
		t.Fatalf("PrevObject(block) from Paragraph = (%d, %v), want (1, true)", got, ok)
	}

	// NextObject(section): Heading 1 (0) -> Heading 2 (6).
	got, ok = NextObject(nodes, 0, ObjectSection, 1, false)
	if !ok || got != 6 {
		t.Fatalf("NextObject(section) from Heading 1 = (%d, %v), want (6, true)", got, ok)
	}
	// PrevObject(section): Paragraph 2 (7) -> Heading 2 (6) -> Heading 1 (0).
	got, ok = PrevObject(nodes, 7, ObjectSection, 1, false)
	if !ok || got != 6 {
		t.Fatalf("PrevObject(section) from Paragraph 2 = (%d, %v), want (6, true)", got, ok)
	}
	got, ok = PrevObject(nodes, 6, ObjectSection, 1, false)
	if !ok || got != 0 {
		t.Fatalf("PrevObject(section) from Heading 2 = (%d, %v), want (0, true)", got, ok)
	}

	// CurrentObject(block) from Nested Bullet 1a (2) returns [Bullet 1 .. Bullet 2] = [1..4].
	rng, ok := CurrentObject(nodes, 2, ObjectBlock)
	wantRng := ObjectRange{Kind: ObjectBlock, ID: "list-1", Start: 1, End: 4}
	if !ok || rng != wantRng {
		t.Fatalf("CurrentObject(block) from Nested Bullet 1a = (%+v, %v), want (%+v, true)", rng, ok, wantRng)
	}
}

func TestMotionGrouped2DTable(t *testing.T) {
	// 2 groups (g1: rows 0..1, g2: row 2), 3 rows x 3 cols = 9 cells.
	var nodes []MotionNode
	rows := []struct {
		rowID string
		group string
	}{
		{"r0", "g1"},
		{"r1", "g1"},
		{"r2", "g2"},
	}
	cols := []string{"c0", "c1", "c2"}
	for ri, r := range rows {
		for ci, c := range cols {
			id := r.rowID + "-" + c
			nodes = append(nodes, MotionNode{
				ID:   id,
				Span: ItemSpan{ID: id, Y: ri, H: 1},
				Objects: map[string]string{
					ObjectBlock: r.group,
					ObjectRow:   r.rowID,
					ObjectCol:   c,
					ObjectCell:  id,
				},
			})
			_ = ci
		}
	}

	// NextObject(block) jumps from g1 (0) to g2 (6).
	got, ok := NextObject(nodes, 0, ObjectBlock, 1, false)
	if !ok || got != 6 {
		t.Fatalf("NextObject(block) group jump = (%d, %v), want (6, true)", got, ok)
	}
	// PrevObject(block) from r2-c1 (7) lands at start of g2 (6), then start of g1 (0).
	got, ok = PrevObject(nodes, 7, ObjectBlock, 1, false)
	if !ok || got != 6 {
		t.Fatalf("PrevObject(block) within g2 = (%d, %v), want (6, true)", got, ok)
	}
	got, ok = PrevObject(nodes, 6, ObjectBlock, 1, false)
	if !ok || got != 0 {
		t.Fatalf("PrevObject(block) g2 -> g1 = (%d, %v), want (0, true)", got, ok)
	}

	// NextSame(col) moves down column c1: r0-c1 (1) -> r1-c1 (4) -> r2-c1 (7).
	got, ok = NextSame(nodes, 1, ObjectCol, 1, false)
	if !ok || got != 4 {
		t.Fatalf("NextSame(col) r0-c1 -> r1-c1 = (%d, %v), want (4, true)", got, ok)
	}
	got, ok = NextSame(nodes, 4, ObjectCol, 1, false)
	if !ok || got != 7 {
		t.Fatalf("NextSame(col) r1-c1 -> r2-c1 = (%d, %v), want (7, true)", got, ok)
	}
	// Wrap around column c1 -> back to r0-c1 (1).
	got, ok = NextSame(nodes, 7, ObjectCol, 1, true)
	if !ok || got != 1 {
		t.Fatalf("NextSame(col, wrap) r2-c1 -> r0-c1 = (%d, %v), want (1, true)", got, ok)
	}
	// PrevSame(col) from r2-c1 (7) with count 2 -> r0-c1 (1).
	got, ok = PrevSame(nodes, 7, ObjectCol, 2, false)
	if !ok || got != 1 {
		t.Fatalf("PrevSame(col, count=2) r2-c1 -> r0-c1 = (%d, %v), want (1, true)", got, ok)
	}

	// CurrentObject(row) from r1-c1 (4) selects whole row r1 [3..5].
	rowRng, ok := CurrentObject(nodes, 4, ObjectRow)
	wantRow := ObjectRange{Kind: ObjectRow, ID: "r1", Start: 3, End: 5}
	if !ok || rowRng != wantRow {
		t.Fatalf("CurrentObject(row) = (%+v, %v), want (%+v, true)", rowRng, ok, wantRow)
	}
	// CurrentObject(col) on row-major cells is contiguous [4..4].
	colRng, ok := CurrentObject(nodes, 4, ObjectCol)
	wantCol := ObjectRange{Kind: ObjectCol, ID: "c1", Start: 4, End: 4}
	if !ok || colRng != wantCol {
		t.Fatalf("CurrentObject(col) = (%+v, %v), want (%+v, true)", colRng, ok, wantCol)
	}
}

func TestMotionFourLevelTree(t *testing.T) {
	// 4-level tree:
	// 0: root-1
	// 1:   child-1a (parent: root-1)
	// 2:     grand-1a1 (parent: child-1a)
	// 3:       great-1a1i (parent: grand-1a1)
	// 4:     grand-1a2 (parent: child-1a)
	// 5:   child-1b (parent: root-1)
	// 6: root-2
	// 7:   child-2a (parent: root-2)
	nodes := []MotionNode{
		{ID: "root-1", Objects: map[string]string{ObjectSection: "root-1", ObjectParent: "TOP"}},
		{ID: "child-1a", Objects: map[string]string{ObjectSection: "root-1", ObjectParent: "root-1"}},
		{ID: "grand-1a1", Objects: map[string]string{ObjectSection: "root-1", ObjectParent: "child-1a"}},
		{ID: "great-1a1i", Objects: map[string]string{ObjectSection: "root-1", ObjectParent: "grand-1a1"}},
		{ID: "grand-1a2", Objects: map[string]string{ObjectSection: "root-1", ObjectParent: "child-1a"}},
		{ID: "child-1b", Objects: map[string]string{ObjectSection: "root-1", ObjectParent: "root-1"}},
		{ID: "root-2", Objects: map[string]string{ObjectSection: "root-2", ObjectParent: "TOP"}},
		{ID: "child-2a", Objects: map[string]string{ObjectSection: "root-2", ObjectParent: "root-2"}},
	}

	// NextSame(parent) from child-1a (1) skips grandchildren/great-grandchildren and lands on child-1b (5).
	got, ok := NextSame(nodes, 1, ObjectParent, 1, false)
	if !ok || got != 5 {
		t.Fatalf("NextSame(parent) child-1a -> child-1b = (%d, %v), want (5, true)", got, ok)
	}
	// PrevSame(parent) from child-1b (5) returns to child-1a (1).
	got, ok = PrevSame(nodes, 5, ObjectParent, 1, false)
	if !ok || got != 1 {
		t.Fatalf("PrevSame(parent) child-1b -> child-1a = (%d, %v), want (1, true)", got, ok)
	}
	// NextSame(parent) from grand-1a1 (2) skips great-1a1i (3) and lands on grand-1a2 (4).
	got, ok = NextSame(nodes, 2, ObjectParent, 1, false)
	if !ok || got != 4 {
		t.Fatalf("NextSame(parent) grand-1a1 -> grand-1a2 = (%d, %v), want (4, true)", got, ok)
	}
	// NextObject(section) jumps root-1 (0) -> root-2 (6).
	got, ok = NextObject(nodes, 0, ObjectSection, 1, false)
	if !ok || got != 6 {
		t.Fatalf("NextObject(section) root-1 -> root-2 = (%d, %v), want (6, true)", got, ok)
	}
}

func TestMotionCompositionWithScrollStepTowardRule1B(t *testing.T) {
	nodes := markdownFixture()
	// Start focused on Bullet 1 (index 1, Y=2). Viewport [0,10) over ContentH=50.
	cur := 1
	scrollCfg := ScrollConfig{}
	scrollSt := ScrollState{OffsetY: 0, ContentH: 50, ViewH: 10}

	// NextObject(block) targets Paragraph p1 at index 5 (Span Y=32, H=6) — completely off-screen.
	target, ok := NextObject(nodes, cur, ObjectBlock, 1, false)
	if !ok || target != 5 {
		t.Fatalf("NextObject(block) = (%d, %v), want (5, true)", target, ok)
	}

	// Hand nodes[target].Span to ScrollStepToward: half-steps until p1 peeks into view, then focuses!
	wantOffsets := []int{5, 10, 15, 20, 25}
	for i, wantOff := range wantOffsets {
		res := ScrollStepToward(scrollSt, scrollCfg, nodes[target].Span)
		if res.State.OffsetY != wantOff || !res.HalfStepped || res.FocusTarget {
			t.Fatalf("half-step %d: got %+v, want OffsetY=%d HalfStepped=true FocusTarget=false", i+1, res, wantOff)
		}
		if i == len(wantOffsets)-1 && !res.TargetVisible {
			t.Fatalf("expected target to peek into view at OffsetY=25: %+v", res)
		}
		scrollSt = res.State
	}

	// Final press: p1 is now peeking in [25,35), so ScrollStepToward focuses and reveals [28,38).
	final := ScrollStepToward(scrollSt, scrollCfg, nodes[target].Span)
	if !final.FocusTarget || final.HalfStepped || !final.TargetVisible || final.State.OffsetY != 28 {
		t.Fatalf("final step: got %+v, want FocusTarget=true OffsetY=28", final)
	}
}

func TestMotionCountsWrapEdgeCasesAndJSON(t *testing.T) {
	// Empty slice edge cases.
	if rng, ok := CurrentObject(nil, 0, ObjectBlock); ok || rng != (ObjectRange{}) {
		t.Fatalf("empty CurrentObject = (%+v, %v)", rng, ok)
	}
	if idx, ok := NextObject(nil, 0, ObjectBlock, 1, true); ok || idx != 0 {
		t.Fatalf("empty NextObject = (%d, %v)", idx, ok)
	}
	if idx, ok := PrevObject(nil, 0, ObjectBlock, 1, true); ok || idx != 0 {
		t.Fatalf("empty PrevObject = (%d, %v)", idx, ok)
	}
	if idx, ok := NextSame(nil, 0, ObjectParent, 1, true); ok || idx != 0 {
		t.Fatalf("empty NextSame = (%d, %v)", idx, ok)
	}
	if idx, ok := PrevSame(nil, 0, ObjectParent, 1, true); ok || idx != 0 {
		t.Fatalf("empty PrevSame = (%d, %v)", idx, ok)
	}

	// Nodes with missing/empty object values.
	sparse := []MotionNode{
		{ID: "n0", Objects: map[string]string{ObjectBlock: "b1"}},
		{ID: "n1"}, // no objects
		{ID: "n2", Objects: map[string]string{ObjectBlock: "b2"}},
		{ID: "n3", Objects: map[string]string{ObjectBlock: "b2"}},
		{ID: "n4", Objects: map[string]string{ObjectBlock: "b3"}},
	}

	if _, ok := CurrentObject(sparse, 1, ObjectBlock); ok {
		t.Fatal("CurrentObject on node without block should be false")
	}
	if _, ok := CurrentObject(sparse, -1, ObjectBlock); ok {
		t.Fatal("CurrentObject on negative index should be false")
	}
	if _, ok := CurrentObject(sparse, 99, ObjectBlock); ok {
		t.Fatal("CurrentObject on out-of-range index should be false")
	}
	if got, ok := NextSame(sparse, 1, ObjectBlock, 1, true); ok || got != 1 {
		t.Fatalf("NextSame on empty key = (%d, %v), want (1, false)", got, ok)
	}
	if got, ok := PrevSame(sparse, 1, ObjectBlock, 1, true); ok || got != 1 {
		t.Fatalf("PrevSame on empty key = (%d, %v), want (1, false)", got, ok)
	}

	// NextObject skips empty node n1 and jumps b1 (0) -> b2 (2) -> b3 (4) with count=2.
	if got, ok := NextObject(sparse, 0, ObjectBlock, 2, false); !ok || got != 4 {
		t.Fatalf("NextObject(count=2) = (%d, %v), want (4, true)", got, ok)
	}
	// Wrap forward from b3 (4) -> b1 (0).
	if got, ok := NextObject(sparse, 4, ObjectBlock, 1, true); !ok || got != 0 {
		t.Fatalf("NextObject(wrap) = (%d, %v), want (0, true)", got, ok)
	}
	// Wrap backward from b1 (0) -> b3 (4), and with count=2 -> start of b2 (2).
	if got, ok := PrevObject(sparse, 0, ObjectBlock, 1, true); !ok || got != 4 {
		t.Fatalf("PrevObject(wrap) = (%d, %v), want (4, true)", got, ok)
	}
	if got, ok := PrevObject(sparse, 0, ObjectBlock, 2, true); !ok || got != 2 {
		t.Fatalf("PrevObject(wrap, count=2) = (%d, %v), want (2, true)", got, ok)
	}
	// PrevObject from empty node n1 (1) jumps backward to b1 (0).
	if got, ok := PrevObject(sparse, 1, ObjectBlock, 1, false); !ok || got != 0 {
		t.Fatalf("PrevObject from empty node = (%d, %v), want (0, true)", got, ok)
	}

	// JSON round-trip of pure value structs.
	cfgIn := MotionConfig{Wrap: true, DefaultObject: ObjectBlock, SectionObject: ObjectSection, RelativeObject: ObjectParent}
	raw, err := json.Marshal(cfgIn)
	if err != nil {
		t.Fatalf("marshal MotionConfig: %v", err)
	}
	var cfgOut MotionConfig
	if err := json.Unmarshal(raw, &cfgOut); err != nil {
		t.Fatalf("unmarshal MotionConfig: %v", err)
	}
	if cfgOut != cfgIn {
		t.Fatalf("MotionConfig round-trip = %+v, want %+v", cfgOut, cfgIn)
	}
}

func TestMotionStepAndFactsAndSpecs(t *testing.T) {
	nodes := markdownFixture()
	cfg := MotionConfig{RelativeObject: ObjectSection}

	// MotionStep: default block object, count=2: 0 -> 1 -> 5.
	target, sel, moved, handled := MotionStep(nodes, 0, cfg, core.Command{ID: CmdMotionObjectNext, Count: 2})
	if !handled || !moved || target != 5 || sel != nil {
		t.Fatalf("MotionStep object.next x2 = (%d, %+v, moved=%v, handled=%v)", target, sel, moved, handled)
	}

	// MotionStep: explicit object arg overrides default: section 0 -> 6.
	target, sel, moved, handled = MotionStep(nodes, 0, cfg, core.Command{
		ID:   CmdMotionObjectNext,
		Args: map[string]any{"object": ObjectSection},
	})
	if !handled || !moved || target != 6 || sel != nil {
		t.Fatalf("MotionStep object.next section = (%d, %+v, moved=%v, handled=%v)", target, sel, moved, handled)
	}

	// MotionStep: object.prev from Bullet 2 (4) -> Bullet 1 (1).
	target, sel, moved, handled = MotionStep(nodes, 4, cfg, core.Command{ID: CmdMotionObjectPrev})
	if !handled || !moved || target != 1 || sel != nil {
		t.Fatalf("MotionStep object.prev = (%d, %+v, moved=%v, handled=%v)", target, sel, moved, handled)
	}

	// MotionStep: same.next / same.prev uses cfg.RelativeObject (section).
	target, sel, moved, handled = MotionStep(nodes, 0, cfg, core.Command{ID: CmdMotionSameNext, Count: 2})
	if !handled || !moved || target != 2 || sel != nil {
		t.Fatalf("MotionStep same.next x2 = (%d, %+v, moved=%v, handled=%v)", target, sel, moved, handled)
	}
	target, sel, moved, handled = MotionStep(nodes, 2, cfg, core.Command{ID: CmdMotionSamePrev})
	if !handled || !moved || target != 1 || sel != nil {
		t.Fatalf("MotionStep same.prev = (%d, %+v, moved=%v, handled=%v)", target, sel, moved, handled)
	}

	// MotionStep: object.select returns current ObjectRange without moving cursor.
	target, sel, moved, handled = MotionStep(nodes, 2, cfg, core.Command{
		ID:   CmdMotionObjectSelect,
		Args: map[string]any{"object": ObjectBlock},
	})
	wantSel := ObjectRange{Kind: ObjectBlock, ID: "list-1", Start: 1, End: 4}
	if !handled || !moved || target != 2 || sel == nil || *sel != wantSel {
		t.Fatalf("MotionStep object.select = (%d, %+v, moved=%v, handled=%v), want %+v", target, sel, moved, handled, wantSel)
	}

	// MotionStep: object.select for missing membership returns moved=false, sel=nil, handled=true.
	target, sel, moved, handled = MotionStep(nodes, 2, cfg, core.Command{
		ID:   CmdMotionObjectSelect,
		Args: map[string]any{"object": ObjectCol},
	})
	if !handled || moved || target != 2 || sel != nil {
		t.Fatalf("MotionStep object.select missing = (%d, %+v, moved=%v, handled=%v)", target, sel, moved, handled)
	}

	// MotionStep: unknown command returns handled=false.
	target, sel, moved, handled = MotionStep(nodes, 3, cfg, core.Command{ID: "other.cmd"})
	if handled || moved || target != 3 || sel != nil {
		t.Fatalf("MotionStep unknown = (%d, %+v, moved=%v, handled=%v)", target, sel, moved, handled)
	}

	// MotionFacts: empty state.
	emptyFacts := MotionFacts(nil, 0, cfg)
	if emptyFacts["empty"] != true || emptyFacts["total"] != 0 || emptyFacts["index"] != 0 ||
		emptyFacts["atFirstBlock"] != true || emptyFacts["atLastBlock"] != true ||
		emptyFacts["atFirstSection"] != true || emptyFacts["atLastSection"] != true ||
		emptyFacts["currentBlock"] != "" || emptyFacts["currentSection"] != "" {
		t.Fatalf("empty MotionFacts = %+v", emptyFacts)
	}

	// MotionFacts: mid-document (index 2: sec-1, list-1) and end (index 7: sec-2, p2).
	fMid := MotionFacts(nodes, 2, cfg)
	if fMid["empty"] != false || fMid["index"] != 2 || fMid["total"] != 8 ||
		fMid["currentBlock"] != "list-1" || fMid["currentSection"] != "sec-1" || fMid["currentRelative"] != "sec-1" ||
		fMid["atFirstBlock"] != false || fMid["atLastBlock"] != false ||
		fMid["atFirstSection"] != true || fMid["atLastSection"] != false {
		t.Fatalf("mid MotionFacts = %+v", fMid)
	}
	// Verify objects map is a shallow copy (mutating returned map does not mutate node).
	objs := fMid["objects"].(map[string]string)
	objs[ObjectBlock] = "mutated"
	if nodes[2].Object(ObjectBlock) != "list-1" {
		t.Fatal("MotionFacts objects map must be a shallow copy")
	}

	fEnd := MotionFacts(nodes, 7, cfg)
	if fEnd["atFirstBlock"] != false || fEnd["atLastBlock"] != true ||
		fEnd["atFirstSection"] != false || fEnd["atLastSection"] != true {
		t.Fatalf("end MotionFacts = %+v", fEnd)
	}

	// MotionCommandSpecs: 5 specs, optional object string param, registers cleanly.
	specs := MotionCommandSpecs()
	if len(specs) != 5 {
		t.Fatalf("MotionCommandSpecs len = %d, want 5", len(specs))
	}
	reg := core.Registry{}
	reg.Register(specs...)
	for _, id := range []string{
		CmdMotionObjectNext,
		CmdMotionObjectPrev,
		CmdMotionSameNext,
		CmdMotionSamePrev,
		CmdMotionObjectSelect,
	} {
		sp, ok := reg.Lookup(id)
		if !ok {
			t.Fatalf("spec %q missing from registry", id)
		}
		if len(sp.Params) != 1 || sp.Params[0].Name != "object" || sp.Params[0].Type != core.ParamString || sp.Params[0].Required {
			t.Fatalf("spec %q unexpected params: %+v", id, sp.Params)
		}
	}
}

func TestDefaultTraitBindingsMotionWithKeymap(t *testing.T) {
	bs := DefaultTraitBindings("normal", TraitMotion)
	if len(bs) != 8 {
		t.Fatalf("DefaultTraitBindings(motion) len = %d, want 8", len(bs))
	}
	for _, b := range bs {
		if b.Scope != TraitScope(TraitMotion) {
			t.Errorf("binding scope = %q, want %q", b.Scope, TraitScope(TraitMotion))
		}
		if len(b.Keys) > 0 && b.Keys[0] == "g" {
			t.Errorf("motion binding must not use g-prefix: %+v", b)
		}
	}
	if !slices.EqualFunc(
		bs,
		DefaultTraitBindings("normal", "trait.motion"),
		func(a, b core.Binding) bool {
			return a.Scope == b.Scope && a.Command == b.Command && slices.Equal(a.Keys, b.Keys)
		},
	) {
		t.Error("DefaultTraitBindings should accept both raw and trait.-prefixed motion name")
	}

	km, err := core.NewKeymap([]core.Mode{{Name: "normal"}}, bs, MotionCommandSpecs()...)
	if err != nil {
		t.Fatalf("core.NewKeymap(TraitMotion): %v", err)
	}

	checks := []struct {
		keys   []string
		cmd    string
		objArg string
	}{
		{[]string{"}"}, CmdMotionObjectNext, ObjectBlock},
		{[]string{"{"}, CmdMotionObjectPrev, ObjectBlock},
		{[]string{"]", "]"}, CmdMotionObjectNext, ObjectSection},
		{[]string{"[", "["}, CmdMotionObjectPrev, ObjectSection},
		{[]string{"]", "s"}, CmdMotionSameNext, ""},
		{[]string{"[", "s"}, CmdMotionSamePrev, ""},
		{[]string{"v", "i", "p"}, CmdMotionObjectSelect, ObjectBlock},
		{[]string{"v", "i", "s"}, CmdMotionObjectSelect, ObjectSection},
	}

	chain := []core.ContextFrame{{Scope: TraitScope(TraitMotion)}}
	for _, tc := range checks {
		var in core.InputState
		var res core.Resolution
		for _, k := range tc.keys {
			res = km.Resolve(core.KeyQuery{Chain: chain, Mode: "normal", Input: in, Key: k})
			in = res.Input
		}
		if res.Outcome != core.Matched || res.Command == nil {
			t.Fatalf("keys=%v -> %+v, want Matched %s", tc.keys, res, tc.cmd)
		}
		if res.Command.ID != tc.cmd || res.Command.Arg("object") != tc.objArg {
			t.Fatalf("keys=%v -> cmd=%q object=%q, want cmd=%q object=%q",
				tc.keys, res.Command.ID, res.Command.Arg("object"), tc.cmd, tc.objArg)
		}
	}
}
