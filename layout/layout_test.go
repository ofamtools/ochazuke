package layout_test

import (
	"math"
	"reflect"
	"testing"

	"ofam.dev/ochazuke/layout"
)

func assertInnerSubset(t *testing.T, id string, outer, inner layout.Rect) {
	t.Helper()
	if outer.W < 0 || outer.H < 0 {
		t.Fatalf("%s: negative outer dims %+v", id, outer)
	}
	if inner.W < 0 || inner.H < 0 {
		t.Fatalf("%s: negative inner dims %+v", id, inner)
	}
	if inner.X < outer.X || inner.Y < outer.Y ||
		inner.X+inner.W > outer.X+outer.W ||
		inner.Y+inner.H > outer.Y+outer.H {
		t.Fatalf("%s: inner %+v escapes outer %+v", id, inner, outer)
	}
}

func TestSolve_ViewportAndEmptyID(t *testing.T) {
	t.Run("negative and zero viewports", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			w, h int
		}{
			{"negative", -5, -10},
			{"zero", 0, 0},
			{"zero height", 80, 0},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s := layout.Solve(layout.Box{ID: "root", Focusable: true}, tc.w, tc.h)
				if s.Rects == nil || s.InnerRects == nil {
					t.Fatalf("expected non-nil maps, got Rects=%v InnerRects=%v", s.Rects, s.InnerRects)
				}
				r, ok := s.Rects["root"]
				if !ok {
					t.Fatalf("expected root in Rects")
				}
				if r.W < 0 || r.H < 0 {
					t.Fatalf("expected non-negative rect, got %+v", r)
				}
				if len(s.FocusOrder) != 0 {
					t.Fatalf("zero-area root should not be in FocusOrder, got %v", s.FocusOrder)
				}
				if id, lx, ly := s.HitTest(0, 0); id != "" || lx != 0 || ly != 0 {
					t.Fatalf("zero-area HitTest should return empty, got (%q, %d, %d)", id, lx, ly)
				}
			})
		}
	})

	t.Run("empty Box and anonymous wrappers", func(t *testing.T) {
		empty := layout.Solve(layout.Box{}, 80, 24)
		if len(empty.Rects) != 0 || len(empty.InnerRects) != 0 || len(empty.FocusOrder) != 0 {
			t.Fatalf("empty root should produce empty solved maps/order, got %+v", empty)
		}

		root := layout.Box{
			Kind:    layout.BoxStack,
			Padding: [4]int{1, 1, 1, 1},
			Children: []layout.Box{
				{
					Kind: layout.BoxStack,
					Axis: layout.Horizontal,
					Children: []layout.Box{
						{ID: "leaf", Focusable: true},
					},
				},
			},
		}
		s := layout.Solve(root, 40, 12)
		if _, ok := s.Rects[""]; ok {
			t.Fatalf("empty ID must never be stored in Rects")
		}
		if len(s.Rects) != 1 {
			t.Fatalf("expected only leaf in Rects, got %+v", s.Rects)
		}
		want := layout.Rect{X: 1, Y: 1, W: 38, H: 10}
		if got := s.Rects["leaf"]; got != want {
			t.Fatalf("leaf rect = %+v, want %+v", got, want)
		}
		if !reflect.DeepEqual(s.FocusOrder, []string{"leaf"}) {
			t.Fatalf("FocusOrder = %v, want [leaf]", s.FocusOrder)
		}
		if id, _, _ := s.HitTest(0, 0); id != "" {
			t.Fatalf("padding on anonymous root should hit nothing, got %q", id)
		}
		if id, lx, ly := s.HitTest(1, 1); id != "leaf" || lx != 0 || ly != 0 {
			t.Fatalf("HitTest(1,1) = (%q,%d,%d), want (leaf,0,0)", id, lx, ly)
		}
	})

	t.Run("unknown BoxKind falls back to BoxLeaf", func(t *testing.T) {
		root := layout.Box{
			ID:        "weird",
			Kind:      layout.BoxKind(99),
			Focusable: true,
			Children:  []layout.Box{{ID: "ignored"}},
		}
		s := layout.Solve(root, 20, 10)
		if got := s.Rects["weird"]; got != (layout.Rect{X: 0, Y: 0, W: 20, H: 10}) {
			t.Fatalf("unknown kind rect = %+v", got)
		}
		if _, ok := s.Rects["ignored"]; ok {
			t.Fatalf("leaf fallback should not solve children")
		}
	})
}

func TestSolve_BorderAndPadding(t *testing.T) {
	cases := []struct {
		name      string
		w, h      int
		border    bool
		padding   [4]int
		wantOuter layout.Rect
		wantInner layout.Rect
	}{
		{
			name:      "border and asymmetric padding TRBL",
			w:         80,
			h:         24,
			border:    true,
			padding:   [4]int{1, 2, 3, 4},
			wantOuter: layout.Rect{X: 0, Y: 0, W: 80, H: 24},
			wantInner: layout.Rect{X: 5, Y: 2, W: 72, H: 18},
		},
		{
			name:      "negative padding clamped to 0",
			w:         20,
			h:         10,
			border:    false,
			padding:   [4]int{-2, 1, -1, 2},
			wantOuter: layout.Rect{X: 0, Y: 0, W: 20, H: 10},
			wantInner: layout.Rect{X: 2, Y: 0, W: 17, H: 10},
		},
		{
			name:      "border only on 2x2 clamps inner to 0x0",
			w:         2,
			h:         2,
			border:    true,
			wantOuter: layout.Rect{X: 0, Y: 0, W: 2, H: 2},
			wantInner: layout.Rect{X: 1, Y: 1, W: 0, H: 0},
		},
		{
			name:      "over-inset clamps inner origin inside outer",
			w:         1,
			h:         1,
			border:    true,
			padding:   [4]int{5, 5, 5, 5},
			wantOuter: layout.Rect{X: 0, Y: 0, W: 1, H: 1},
			wantInner: layout.Rect{X: 1, Y: 1, W: 0, H: 0},
		},
		{
			name:      "zero outer size",
			w:         0,
			h:         0,
			border:    true,
			padding:   [4]int{1, 1, 1, 1},
			wantOuter: layout.Rect{X: 0, Y: 0, W: 0, H: 0},
			wantInner: layout.Rect{X: 0, Y: 0, W: 0, H: 0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := layout.Solve(layout.Box{
				ID:      "n",
				Border:  tc.border,
				Padding: tc.padding,
			}, tc.w, tc.h)
			outer := s.Rects["n"]
			inner := s.InnerRects["n"]
			if outer != tc.wantOuter {
				t.Fatalf("outer = %+v, want %+v", outer, tc.wantOuter)
			}
			if inner != tc.wantInner {
				t.Fatalf("inner = %+v, want %+v", inner, tc.wantInner)
			}
			assertInnerSubset(t, "n", outer, inner)
		})
	}
}

func TestSolve_BoxLeafAndConstraints(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		c    layout.Constraints
		want layout.Rect
	}{
		{
			name: "unconstrained fills viewport",
			w:    50, h: 20,
			want: layout.Rect{X: 0, Y: 0, W: 50, H: 20},
		},
		{
			name: "fixed overrides flex",
			w:    50, h: 20,
			c:    layout.Constraints{FixedW: 30, FixedH: 10, Flex: 5},
			want: layout.Rect{X: 0, Y: 0, W: 30, H: 10},
		},
		{
			name: "max caps unconstrained",
			w:    50, h: 20,
			c:    layout.Constraints{MaxW: 25, MaxH: 12},
			want: layout.Rect{X: 0, Y: 0, W: 25, H: 12},
		},
		{
			name: "min greater than max wins and caps at avail",
			w:    15, h: 20,
			c:    layout.Constraints{MinW: 20, MaxW: 10, MinH: 12, MaxH: 8},
			want: layout.Rect{X: 0, Y: 0, W: 15, H: 12},
		},
		{
			name: "negative constraints ignored",
			w:    20, h: 10,
			c:    layout.Constraints{MinW: -5, MaxW: -3, FixedH: -2},
			want: layout.Rect{X: 0, Y: 0, W: 20, H: 10},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := layout.Solve(layout.Box{ID: "leaf", Constraints: tc.c}, tc.w, tc.h)
			if got := s.Rects["leaf"]; got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSolve_BoxStack(t *testing.T) {
	t.Run("horizontal fixed and weighted flex with gap and remainder", func(t *testing.T) {
		root := layout.Box{
			ID:   "stack",
			Kind: layout.BoxStack,
			Axis: layout.Horizontal,
			Gap:  1,
			Children: []layout.Box{
				{ID: "fixed", Constraints: layout.Constraints{FixedW: 10}},
				{ID: "a", Constraints: layout.Constraints{Flex: 0}}, // defaults to 1
				{ID: "b", Constraints: layout.Constraints{Flex: 2}},
				{ID: "c", Constraints: layout.Constraints{Flex: 1}},
			},
		}
		// avail=53, totalGap=3, rem=50; fixed=10 -> flexRem=40 across weights 1:2:1 => 10, 20, 10
		s := layout.Solve(root, 53, 10)
		want := map[string]layout.Rect{
			"fixed": {X: 0, Y: 0, W: 10, H: 10},
			"a":     {X: 11, Y: 0, W: 10, H: 10},
			"b":     {X: 22, Y: 0, W: 20, H: 10},
			"c":     {X: 43, Y: 0, W: 10, H: 10},
		}
		for id, w := range want {
			if got := s.Rects[id]; got != w {
				t.Fatalf("%s = %+v, want %+v", id, got, w)
			}
		}
	})

	t.Run("largest remainder distribution 10 across 3 equal flex", func(t *testing.T) {
		root := layout.Box{
			Kind: layout.BoxStack,
			Axis: layout.Horizontal,
			Children: []layout.Box{
				{ID: "c0"}, {ID: "c1"}, {ID: "c2"},
			},
		}
		s := layout.Solve(root, 10, 5)
		if s.Rects["c0"].W != 4 || s.Rects["c1"].W != 3 || s.Rects["c2"].W != 3 {
			t.Fatalf("got widths [%d %d %d], want [4 3 3]", s.Rects["c0"].W, s.Rects["c1"].W, s.Rects["c2"].W)
		}
	})

	t.Run("water-filling simultaneous Min and Max violation", func(t *testing.T) {
		// budget=10, 3 Flex=1 items: Min=8, Max=3, unconstrained.
		// Freezing Min=8 leaves 2 for the remaining two -> [8, 1, 1], sum=10.
		root := layout.Box{
			Kind: layout.BoxStack,
			Axis: layout.Vertical,
			Children: []layout.Box{
				{ID: "min", Constraints: layout.Constraints{MinH: 8}},
				{ID: "max", Constraints: layout.Constraints{MaxH: 3}},
				{ID: "free"},
			},
		}
		s := layout.Solve(root, 20, 10)
		if s.Rects["min"].H != 8 || s.Rects["max"].H != 1 || s.Rects["free"].H != 1 {
			t.Fatalf("got heights [%d %d %d], want [8 1 1]", s.Rects["min"].H, s.Rects["max"].H, s.Rects["free"].H)
		}
	})

	t.Run("water-filling Max cap redistributes surplus and Min pulls", func(t *testing.T) {
		root := layout.Box{
			Kind: layout.BoxStack,
			Axis: layout.Horizontal,
			Children: []layout.Box{
				{ID: "capped", Constraints: layout.Constraints{MaxW: 4}},
				{ID: "floored", Constraints: layout.Constraints{MinW: 12}},
				{ID: "free"},
			},
		}
		// budget=24: capped=4, rem=20 across floored(min 12) & free => floored=12, free=8
		s := layout.Solve(root, 24, 5)
		if s.Rects["capped"].W != 4 || s.Rects["floored"].W != 12 || s.Rects["free"].W != 8 {
			t.Fatalf("got widths [%d %d %d], want [4 12 8]", s.Rects["capped"].W, s.Rects["floored"].W, s.Rects["free"].W)
		}
	})

	t.Run("sum(Min) > budget and sum(Fixed) > budget never overflow", func(t *testing.T) {
		sMin := layout.Solve(layout.Box{
			Kind: layout.BoxStack,
			Axis: layout.Horizontal,
			Children: []layout.Box{
				{ID: "a", Constraints: layout.Constraints{MinW: 8}},
				{ID: "b", Constraints: layout.Constraints{MinW: 8}},
			},
		}, 10, 4)
		if sMin.Rects["a"].W+sMin.Rects["b"].W != 10 || sMin.Rects["a"].W != 5 {
			t.Fatalf("sum(Min)>budget gave a=%+v b=%+v", sMin.Rects["a"], sMin.Rects["b"])
		}

		sFixed := layout.Solve(layout.Box{
			Kind: layout.BoxStack,
			Axis: layout.Vertical,
			Children: []layout.Box{
				{ID: "f1", Constraints: layout.Constraints{FixedH: 7}},
				{ID: "f2", Constraints: layout.Constraints{FixedH: 7}},
			},
		}, 10, 10)
		if sFixed.Rects["f1"].H != 7 || sFixed.Rects["f2"].H != 3 {
			t.Fatalf("sum(Fixed)>budget gave f1=%+v f2=%+v", sFixed.Rects["f1"], sFixed.Rects["f2"])
		}
	})

	t.Run("negative gap clamped, excessive gap clamps children, cross-axis constraints, floating overlay", func(t *testing.T) {
		sNegGap := layout.Solve(layout.Box{
			Kind: layout.BoxStack,
			Axis: layout.Vertical,
			Gap:  -3,
			Children: []layout.Box{
				{ID: "a", Constraints: layout.Constraints{MaxW: 10}},
				{ID: "b", Constraints: layout.Constraints{FixedW: 5}},
			},
		}, 20, 10)
		if sNegGap.Rects["a"] != (layout.Rect{X: 0, Y: 0, W: 10, H: 5}) ||
			sNegGap.Rects["b"] != (layout.Rect{X: 0, Y: 5, W: 5, H: 5}) {
			t.Fatalf("unexpected negative-gap layout: %+v", sNegGap.Rects)
		}

		sBigGap := layout.Solve(layout.Box{
			Kind: layout.BoxStack,
			Axis: layout.Horizontal,
			Gap:  50,
			Children: []layout.Box{
				{ID: "a"},
				{ID: "b"},
			},
		}, 20, 5)
		if sBigGap.Rects["a"].W != 0 || sBigGap.Rects["b"].W != 0 || sBigGap.Rects["b"].X > 20 {
			t.Fatalf("excessive gap overflowed: %+v", sBigGap.Rects)
		}

		// Floating BoxOverlay before its AnchorID sibling does not consume gap or flow space.
		sFloat := layout.Solve(layout.Box{
			Kind: layout.BoxStack,
			Axis: layout.Vertical,
			Gap:  2,
			Children: []layout.Box{
				{ID: "tip", Kind: layout.BoxOverlay, AnchorID: "btn", Constraints: layout.Constraints{FixedW: 12, FixedH: 3}},
				{ID: "top"},
				{ID: "btn"},
			},
		}, 40, 20)
		// top=9, gap=2, btn=9 (Y=11..20) => tip flips above btn at Y=8..11
		if sFloat.Rects["top"] != (layout.Rect{X: 0, Y: 0, W: 40, H: 9}) ||
			sFloat.Rects["btn"] != (layout.Rect{X: 0, Y: 11, W: 40, H: 9}) ||
			sFloat.Rects["tip"] != (layout.Rect{X: 0, Y: 8, W: 12, H: 3}) {
			t.Fatalf("floating overlay in stack gave %+v", sFloat.Rects)
		}
	})
}

func TestSolve_BoxSplit(t *testing.T) {
	cases := []struct {
		name string
		box  layout.Box
		w, h int
		want map[string]layout.Rect
	}{
		{
			name: "default ratio <= 0 uses 0.5 with gap",
			box: layout.Box{
				Kind: layout.BoxSplit, Axis: layout.Horizontal, Ratio: 0, Gap: 1,
				Children: []layout.Box{{ID: "l"}, {ID: "r"}},
			},
			w: 41, h: 10,
			want: map[string]layout.Rect{
				"l": {X: 0, Y: 0, W: 20, H: 10},
				"r": {X: 21, Y: 0, W: 20, H: 10},
			},
		},
		{
			name: "explicit ratio and vertical axis",
			box: layout.Box{
				Kind: layout.BoxSplit, Axis: layout.Vertical, Ratio: 0.25,
				Children: []layout.Box{{ID: "t"}, {ID: "b"}},
			},
			w: 20, h: 20,
			want: map[string]layout.Rect{
				"t": {X: 0, Y: 0, W: 20, H: 5},
				"b": {X: 0, Y: 5, W: 20, H: 15},
			},
		},
		{
			name: "NaN ratio defaults to 0.5 and >1 clamps to 1.0",
			box: layout.Box{
				Kind: layout.BoxSplit, Axis: layout.Horizontal, Ratio: math.NaN(),
				Children: []layout.Box{{ID: "l"}, {ID: "r"}},
			},
			w: 20, h: 10,
			want: map[string]layout.Rect{
				"l": {X: 0, Y: 0, W: 10, H: 10},
				"r": {X: 10, Y: 0, W: 10, H: 10},
			},
		},
		{
			name: "ratio > 1 clamps to 1.0",
			box: layout.Box{
				Kind: layout.BoxSplit, Axis: layout.Horizontal, Ratio: 1.5,
				Children: []layout.Box{{ID: "l"}, {ID: "r"}},
			},
			w: 20, h: 10,
			want: map[string]layout.Rect{
				"l": {X: 0, Y: 0, W: 20, H: 10},
				"r": {X: 20, Y: 0, W: 0, H: 10},
			},
		},
		{
			name: "fixed first child and max second child",
			box: layout.Box{
				Kind: layout.BoxSplit, Axis: layout.Horizontal, Ratio: 0.5,
				Children: []layout.Box{
					{ID: "l", Constraints: layout.Constraints{FixedW: 10}},
					{ID: "r", Constraints: layout.Constraints{MaxW: 15}},
				},
			},
			w: 40, h: 10,
			want: map[string]layout.Rect{
				"l": {X: 0, Y: 0, W: 10, H: 10},
				"r": {X: 10, Y: 0, W: 15, H: 10},
			},
		},
		{
			name: "min and max clamping with sum(Min) > rem",
			box: layout.Box{
				Kind: layout.BoxSplit, Axis: layout.Horizontal, Ratio: 0.2,
				Children: []layout.Box{
					{ID: "l", Constraints: layout.Constraints{MinW: 15}},
					{ID: "r", Constraints: layout.Constraints{MinW: 15}},
				},
			},
			w: 20, h: 5,
			want: map[string]layout.Rect{
				"l": {X: 0, Y: 0, W: 10, H: 5},
				"r": {X: 10, Y: 0, W: 10, H: 5},
			},
		},
		{
			name: "single flow child fills inner without subtracting gap",
			box: layout.Box{
				Kind: layout.BoxSplit, Axis: layout.Horizontal, Gap: 4,
				Children: []layout.Box{
					{ID: "only"},
					{ID: "ov", Kind: layout.BoxOverlay, Constraints: layout.Constraints{FixedW: 6, FixedH: 2}},
				},
			},
			w: 20, h: 10,
			want: map[string]layout.Rect{
				"only": {X: 0, Y: 0, W: 20, H: 10},
				"ov":   {X: 7, Y: 4, W: 6, H: 2},
			},
		},
		{
			name: "more than two flow children ignores extras",
			box: layout.Box{
				Kind: layout.BoxSplit, Axis: layout.Horizontal,
				Children: []layout.Box{{ID: "a"}, {ID: "b"}, {ID: "c"}},
			},
			w: 20, h: 10,
			want: map[string]layout.Rect{
				"a": {X: 0, Y: 0, W: 10, H: 10},
				"b": {X: 10, Y: 0, W: 10, H: 10},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := layout.Solve(tc.box, tc.w, tc.h)
			if len(s.Rects) != len(tc.want) {
				t.Fatalf("Rects count = %d (%+v), want %d", len(s.Rects), s.Rects, len(tc.want))
			}
			for id, w := range tc.want {
				if got := s.Rects[id]; got != w {
					t.Fatalf("%s = %+v, want %+v", id, got, w)
				}
			}
		})
	}
}

func TestSolve_BoxGrid(t *testing.T) {
	t.Run("explicit Cols 3 with 5 cards, gap, and mixed row heights", func(t *testing.T) {
		root := layout.Box{
			Kind: layout.BoxGrid,
			Cols: 3,
			Gap:  1,
			Children: []layout.Box{
				{ID: "c0", Constraints: layout.Constraints{FixedH: 4}},
				{ID: "c1", Constraints: layout.Constraints{FixedH: 6}},
				{ID: "c2"},
				{ID: "c3"},
				{ID: "c4"},
			},
		}
		// W=32, gap=1 => colBudget=30 => cols=10,10,10 at X=0,11,22
		// H=17, gap=1 => rowBudget=16 => row0 fixed=6, row1 gets remaining 10 (Y=7)
		s := layout.Solve(root, 32, 17)
		want := map[string]layout.Rect{
			"c0": {X: 0, Y: 0, W: 10, H: 4},
			"c1": {X: 11, Y: 0, W: 10, H: 6},
			"c2": {X: 22, Y: 0, W: 10, H: 6},
			"c3": {X: 0, Y: 7, W: 10, H: 10},
			"c4": {X: 11, Y: 7, W: 10, H: 10},
		}
		for id, w := range want {
			if got := s.Rects[id]; got != w {
				t.Fatalf("%s = %+v, want %+v", id, got, w)
			}
		}
	})

	t.Run("auto-fit MinColW boundary and narrow clamp", func(t *testing.T) {
		mk := func() layout.Box {
			return layout.Box{
				Kind:    layout.BoxGrid,
				MinColW: 36,
				Gap:     1,
				Children: []layout.Box{
					{ID: "c0"}, {ID: "c1"}, {ID: "c2"},
				},
			}
		}
		// 72 < 36+1+36=73 -> 1 col; 73 >= 73 -> 2 cols; 110 >= 36*3+2 -> 3 cols; 20 < 36 -> 1 col
		if s := layout.Solve(mk(), 72, 15); s.Rects["c1"].Y == 0 {
			t.Fatalf("width=72 should wrap to 1 col, got %+v", s.Rects)
		}
		if s := layout.Solve(mk(), 73, 15); s.Rects["c1"].Y != 0 || s.Rects["c2"].Y == 0 {
			t.Fatalf("width=73 should fit 2 cols, got %+v", s.Rects)
		}
		if s := layout.Solve(mk(), 110, 15); s.Rects["c2"].Y != 0 {
			t.Fatalf("width=110 should fit 3 cols, got %+v", s.Rects)
		}
		if s := layout.Solve(mk(), 20, 15); s.Rects["c0"].W != 20 || s.Rects["c1"].X != 0 {
			t.Fatalf("narrow width=20 should clamp to 1 col, got %+v", s.Rects)
		}
	})

	t.Run("zero flow children with floating overlay", func(t *testing.T) {
		s := layout.Solve(layout.Box{
			ID:   "g",
			Kind: layout.BoxGrid,
			Children: []layout.Box{
				{ID: "ov", Kind: layout.BoxOverlay, Constraints: layout.Constraints{FixedW: 10, FixedH: 4}},
			},
		}, 30, 12)
		if got := s.Rects["ov"]; got != (layout.Rect{X: 10, Y: 4, W: 10, H: 4}) {
			t.Fatalf("ov = %+v, want {10 4 10 4}", got)
		}
	})
}

func TestSolve_BoxOverlay(t *testing.T) {
	t.Run("wrapper mode with base + multiple overlays and anchoring", func(t *testing.T) {
		root := layout.Box{
			ID:   "root",
			Kind: layout.BoxOverlay,
			Children: []layout.Box{
				{
					ID:   "page",
					Kind: layout.BoxStack,
					Axis: layout.Vertical,
					Children: []layout.Box{
						{ID: "top-btn", Constraints: layout.Constraints{FixedH: 3}},
						{ID: "spacer"},
						{ID: "bot-btn", Constraints: layout.Constraints{FixedH: 3}},
					},
				},
				{
					ID:          "centered",
					Constraints: layout.Constraints{FixedW: 20, FixedH: 6},
				},
				{
					ID:          "below-menu",
					AnchorID:    "top-btn",
					Constraints: layout.Constraints{FixedW: 15, FixedH: 4},
				},
				{
					ID:          "above-menu",
					AnchorID:    "bot-btn",
					Constraints: layout.Constraints{FixedW: 15, FixedH: 4},
				},
				{
					ID:          "missing-anchor",
					AnchorID:    "does-not-exist",
					Constraints: layout.Constraints{FixedW: 10, FixedH: 2},
				},
			},
		}
		s := layout.Solve(root, 40, 20)
		if got := s.Rects["centered"]; got != (layout.Rect{X: 10, Y: 7, W: 20, H: 6}) {
			t.Fatalf("centered = %+v, want {10 7 20 6}", got)
		}
		if got := s.Rects["below-menu"]; got != (layout.Rect{X: 0, Y: 3, W: 15, H: 4}) {
			t.Fatalf("below-menu = %+v, want {0 3 15 4}", got)
		}
		// bot-btn is at Y=17..20; below (20..24) overflows, so above at Y=13..17
		if got := s.Rects["above-menu"]; got != (layout.Rect{X: 0, Y: 13, W: 15, H: 4}) {
			t.Fatalf("above-menu = %+v, want {0 13 15 4}", got)
		}
		if got := s.Rects["missing-anchor"]; got != (layout.Rect{X: 15, Y: 9, W: 10, H: 2}) {
			t.Fatalf("missing-anchor = %+v, want {15 9 10 2}", got)
		}
	})

	t.Run("anchor X clamping and largest-side vertical clamping", func(t *testing.T) {
		root := layout.Box{
			Kind: layout.BoxOverlay,
			Children: []layout.Box{
				{
					Kind: layout.BoxStack,
					Axis: layout.Horizontal,
					Children: []layout.Box{
						{ID: "left"},
						{ID: "right-btn", Constraints: layout.Constraints{FixedW: 5}},
					},
				},
				{
					ID:          "popup",
					AnchorID:    "right-btn",
					Constraints: layout.Constraints{FixedW: 12, FixedH: 8},
				},
			},
		}
		// Viewport 20x10; right-btn at X=15, Y=0, H=10 -> belowY=10 (0 space), aboveY=-8 (0 space); clamped inside 20x10
		s := layout.Solve(root, 20, 10)
		p := s.Rects["popup"]
		if p.X != 8 || p.Y != 2 || p.W != 12 || p.H != 8 {
			t.Fatalf("clamped popup = %+v, want {8 2 12 8}", p)
		}
	})

	t.Run("single-child modal frame vs anonymous wrapper and nested overlay", func(t *testing.T) {
		// Modal frame (BoxOverlay with ID, Border, FixedW/H, Focusable) solves child in same layer;
		// anonymous zero-config BoxOverlay queues child into next layer.
		root := layout.Box{
			Kind: layout.BoxStack,
			Children: []layout.Box{
				{ID: "base-btn", Focusable: true},
				{
					ID:          "modal",
					Kind:        layout.BoxOverlay,
					Border:      true,
					Focusable:   true,
					Constraints: layout.Constraints{FixedW: 30, FixedH: 10},
					Children: []layout.Box{
						{
							ID:        "modal-body",
							Kind:      layout.BoxStack,
							Focusable: true,
							Children: []layout.Box{
								{ID: "modal-input", Focusable: true, Constraints: layout.Constraints{FixedH: 2}},
								{ID: "dropdown", Kind: layout.BoxOverlay, AnchorID: "modal-input", Focusable: true, Constraints: layout.Constraints{FixedW: 10, FixedH: 3}},
							},
						},
					},
				},
			},
		}
		s := layout.Solve(root, 60, 20)
		if got := s.Rects["modal"]; got != (layout.Rect{X: 15, Y: 5, W: 30, H: 10}) {
			t.Fatalf("modal = %+v", got)
		}
		if got := s.Rects["modal-body"]; got != (layout.Rect{X: 16, Y: 6, W: 28, H: 8}) {
			t.Fatalf("modal-body = %+v", got)
		}
		if got := s.Rects["dropdown"]; got != (layout.Rect{X: 16, Y: 8, W: 10, H: 3}) {
			t.Fatalf("nested dropdown = %+v, want {16 8 10 3}", got)
		}
		// Nested dropdown is in higher layerIdx and focusable, so it traps FocusOrder.
		if !reflect.DeepEqual(s.FocusOrder, []string{"dropdown"}) {
			t.Fatalf("FocusOrder with focusable nested dropdown = %v, want [dropdown]", s.FocusOrder)
		}

		// Anonymous zero-config wrapper BoxOverlay delegates anchoring to its single child.
		sAnon := layout.Solve(layout.Box{
			Kind: layout.BoxStack,
			Children: []layout.Box{
				{ID: "anchor", Constraints: layout.Constraints{FixedH: 2}},
				{
					Kind: layout.BoxOverlay,
					Children: []layout.Box{
						{ID: "anchored-child", AnchorID: "anchor", Constraints: layout.Constraints{FixedW: 10, FixedH: 3}},
					},
				},
			},
		}, 40, 20)
		if got := sAnon.Rects["anchored-child"]; got != (layout.Rect{X: 0, Y: 2, W: 10, H: 3}) {
			t.Fatalf("anchored-child via anon wrapper = %+v, want {0 2 10 3}", got)
		}
	})
}

func TestFocusOrder(t *testing.T) {
	t.Run("visual Y then X sort, excludes unfocusable, empty ID, and zero-area", func(t *testing.T) {
		root := layout.Box{
			Kind: layout.BoxSplit,
			Axis: layout.Horizontal,
			Children: []layout.Box{
				{
					Kind: layout.BoxStack,
					Axis: layout.Vertical,
					Children: []layout.Box{
						{ID: "left-top", Focusable: true},
						{ID: "left-skip", Focusable: false},
						{ID: "left-bot", Focusable: true},
					},
				},
				{
					Kind: layout.BoxStack,
					Axis: layout.Vertical,
					Children: []layout.Box{
						{ID: "right-top", Focusable: true},
						{ID: "", Focusable: true}, // excluded: empty ID
						{ID: "right-zero", Focusable: true, Constraints: layout.Constraints{FixedW: 0, MaxW: 0, MaxH: 0}},
						{ID: "right-bot", Focusable: true},
					},
				},
			},
		}
		// Manually test zero-area exclusion via FixedW: 10? Wait: MaxW/MaxH 0 = unconstrained; use 0-width split/stack
		root.Children[1].Children[2] = layout.Box{
			Kind: layout.BoxStack,
			Children: []layout.Box{
				{ID: "right-zero", Focusable: true},
			},
			Constraints: layout.Constraints{FixedH: 1},
		}
		// Actually construct a clean zero-area sibling via Ratio=1.0 split!
		root.Children[1].Children[2] = layout.Box{
			Kind: layout.BoxSplit, Axis: layout.Horizontal, Ratio: 1.0,
			Children: []layout.Box{
				{ID: "right-mid", Focusable: true},
				{ID: "right-zero", Focusable: true},
			},
		}
		s := layout.Solve(root, 40, 12)
		if s.Rects["right-zero"].W != 0 {
			t.Fatalf("expected right-zero W=0, got %+v", s.Rects["right-zero"])
		}
		want := []string{"left-top", "right-top", "right-mid", "left-bot", "right-bot"}
		if !reflect.DeepEqual(s.FocusOrder, want) {
			t.Fatalf("FocusOrder = %v, want %v (rects=%+v)", s.FocusOrder, want, s.Rects)
		}
	})

	t.Run("modal overlay traps FocusOrder while non-focusable toast falls back to base", func(t *testing.T) {
		modalTree := layout.Box{
			Kind: layout.BoxOverlay,
			Children: []layout.Box{
				{
					Kind: layout.BoxStack,
					Children: []layout.Box{
						{ID: "page-a", Focusable: true},
						{ID: "page-b", Focusable: true},
					},
				},
				{
					ID:          "modal",
					Kind:        layout.BoxOverlay,
					Border:      true,
					Focusable:   true,
					Constraints: layout.Constraints{FixedW: 20, FixedH: 8},
					Children: []layout.Box{
						{
							Kind: layout.BoxStack,
							Children: []layout.Box{
								{ID: "m-ok", Focusable: true},
								{ID: "m-cancel", Focusable: true},
							},
						},
					},
				},
			},
		}
		sModal := layout.Solve(modalTree, 40, 20)
		wantModal := []string{"modal", "m-ok", "m-cancel"}
		if !reflect.DeepEqual(sModal.FocusOrder, wantModal) {
			t.Fatalf("modal FocusOrder = %v, want %v", sModal.FocusOrder, wantModal)
		}

		// Adding a non-focusable toast above the modal still keeps FocusOrder on the modal layer.
		toastTree := modalTree
		toastTree.Children = append(toastTree.Children, layout.Box{
			ID:          "toast",
			Focusable:   false,
			Constraints: layout.Constraints{FixedW: 15, FixedH: 2},
		})
		sToast := layout.Solve(toastTree, 40, 20)
		if !reflect.DeepEqual(sToast.FocusOrder, wantModal) {
			t.Fatalf("toast fallback FocusOrder = %v, want %v", sToast.FocusOrder, wantModal)
		}

		// Non-focusable overlay over focusable base falls back to base.
		baseOnlyTree := layout.Box{
			Kind: layout.BoxOverlay,
			Children: []layout.Box{
				{ID: "base", Focusable: true},
				{ID: "passive-ov", Focusable: false, Constraints: layout.Constraints{FixedW: 10, FixedH: 4}},
			},
		}
		sBase := layout.Solve(baseOnlyTree, 30, 10)
		if !reflect.DeepEqual(sBase.FocusOrder, []string{"base"}) {
			t.Fatalf("base fallback FocusOrder = %v, want [base]", sBase.FocusOrder)
		}
	})
}

func TestHitTest(t *testing.T) {
	t.Run("overlays, deepest named node, anonymous wrapper pass-through, border/padding local coords", func(t *testing.T) {
		root := layout.Box{
			ID:   "root",
			Kind: layout.BoxOverlay,
			Children: []layout.Box{
				{
					ID:      "parent",
					Kind:    layout.BoxStack,
					Border:  true,
					Padding: [4]int{1, 1, 1, 1},
					Children: []layout.Box{
						{
							Kind: layout.BoxStack, // anonymous wrapper
							Children: []layout.Box{
								{ID: "leaf"},
							},
						},
					},
				},
				{
					ID:          "ov1",
					Constraints: layout.Constraints{FixedW: 10, FixedH: 6},
				},
				{
					ID:          "ov2",
					Constraints: layout.Constraints{FixedW: 6, FixedH: 4},
				},
			},
		}
		// 40x20: ov1=[15..25, 7..13), ov2=[17..23, 8..12)
		s := layout.Solve(root, 40, 20)

		cases := []struct {
			x, y   int
			wantID string
			wx, wy int
		}{
			{18, 9, "ov2", 1, 1},   // top overlay over ov1
			{15, 7, "ov1", 0, 0},   // lower overlay outside ov2
			{2, 2, "leaf", 0, 0},   // inside parent's border+padding, passed through anonymous wrapper
			{0, 0, "parent", 0, 0}, // parent's border cell
			{1, 1, "parent", 1, 1}, // parent's padding cell
			{-1, 0, "", 0, 0},      // out of bounds left
			{40, 0, "", 0, 0},      // out of bounds right (exclusive)
			{0, 20, "", 0, 0},      // out of bounds bottom (exclusive)
		}
		for _, tc := range cases {
			id, lx, ly := s.HitTest(tc.x, tc.y)
			if id != tc.wantID || lx != tc.wx || ly != tc.wy {
				t.Fatalf("HitTest(%d,%d) = (%q,%d,%d), want (%q,%d,%d)",
					tc.x, tc.y, id, lx, ly, tc.wantID, tc.wx, tc.wy)
			}
		}
	})

	t.Run("zero-value Solved and manual Rects fallback", func(t *testing.T) {
		var zero layout.Solved
		if id, lx, ly := zero.HitTest(5, 5); id != "" || lx != 0 || ly != 0 {
			t.Fatalf("zero Solved HitTest = (%q,%d,%d)", id, lx, ly)
		}

		manual := layout.Solved{
			Rects: map[string]layout.Rect{
				"outer": {X: 0, Y: 0, W: 20, H: 10},
				"inner": {X: 5, Y: 2, W: 8, H: 4},
			},
		}
		if id, lx, ly := manual.HitTest(6, 3); id != "inner" || lx != 1 || ly != 1 {
			t.Fatalf("manual HitTest = (%q,%d,%d), want (inner,1,1)", id, lx, ly)
		}
	})
}

func TestNeighbor(t *testing.T) {
	t.Run("2x2 grid all 4 directions and edges", func(t *testing.T) {
		s := layout.Solve(layout.Box{
			Kind: layout.BoxGrid,
			Cols: 2,
			Gap:  1,
			Children: []layout.Box{
				{ID: "tl", Focusable: true},
				{ID: "tr", Focusable: true},
				{ID: "bl", Focusable: true},
				{ID: "br", Focusable: true},
			},
		}, 21, 11)

		checks := []struct {
			from   string
			dir    layout.Dir
			want   string
			wantOK bool
		}{
			{"tl", layout.Right, "tr", true},
			{"tl", layout.Down, "bl", true},
			{"tl", layout.Left, "", false},
			{"tl", layout.Up, "", false},
			{"br", layout.Left, "bl", true},
			{"br", layout.Up, "tr", true},
			{"br", layout.Right, "", false},
			{"br", layout.Down, "", false},
		}
		for _, tc := range checks {
			got, ok := s.Neighbor(tc.from, tc.dir)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("Neighbor(%q, %v) = (%q, %v), want (%q, %v)", tc.from, tc.dir, got, ok, tc.want, tc.wantOK)
			}
		}
	})

	t.Run("asymmetric tall-left split does not jump sideways on Down", func(t *testing.T) {
		s := layout.Solve(layout.Box{
			Kind: layout.BoxSplit, Axis: layout.Horizontal, Ratio: 0.3,
			Children: []layout.Box{
				{ID: "sidebar", Focusable: true},
				{
					Kind: layout.BoxSplit, Axis: layout.Vertical, Ratio: 0.4,
					Children: []layout.Box{
						{ID: "top-right", Focusable: true},
						{ID: "bot-right-unfocusable", Focusable: false},
					},
				},
			},
		}, 80, 24)
		if got, ok := s.Neighbor("top-right", layout.Down); ok || got != "" {
			t.Fatalf("Neighbor(top-right, Down) = (%q, %v), want (\"\", false)", got, ok)
		}
		if got, ok := s.Neighbor("top-right", layout.Left); !ok || got != "sidebar" {
			t.Fatalf("Neighbor(top-right, Left) = (%q, %v), want (sidebar, true)", got, ok)
		}
	})

	t.Run("overlap and primary gap beat closer diagonal center", func(t *testing.T) {
		s := layout.Solved{
			Rects: map[string]layout.Rect{
				"from":   {X: 0, Y: 0, W: 20, H: 4},
				"adj":    {X: 20, Y: 0, W: 60, H: 4},  // primaryGap=0, overlap=4, centerDist=40
				"nonadj": {X: 22, Y: 5, W: 10, H: 10}, // primaryGap=2, overlap=0, centerDist~18.8
			},
			FocusOrder: []string{"from", "adj", "nonadj"},
		}
		if got, ok := s.Neighbor("from", layout.Right); !ok || got != "adj" {
			t.Fatalf("Neighbor(from, Right) = (%q, %v), want (adj, true)", got, ok)
		}
	})

	t.Run("ragged 5-card grid spill fallback and non-focusable skip", func(t *testing.T) {
		s := layout.Solve(layout.Box{
			Kind: layout.BoxGrid,
			Cols: 3,
			Children: []layout.Box{
				{ID: "c0", Focusable: true},
				{ID: "c1", Focusable: true},
				{ID: "c2", Focusable: true},
				{ID: "c3", Focusable: true},
				{ID: "c4", Focusable: true},
			},
		}, 30, 10)
		// c2 is row 0 col 2; row 1 only has c3 (col 0) and c4 (col 1) -> Down spills to c4.
		if got, ok := s.Neighbor("c2", layout.Down); !ok || got != "c4" {
			t.Fatalf("Neighbor(c2, Down) = (%q, %v), want (c4, true)", got, ok)
		}

		// Non-focusable spacer between left and right is skipped.
		sSpacer := layout.Solve(layout.Box{
			Kind: layout.BoxStack, Axis: layout.Horizontal,
			Children: []layout.Box{
				{ID: "left", Focusable: true},
				{ID: "spacer", Focusable: false},
				{ID: "right", Focusable: true},
			},
		}, 30, 5)
		if got, ok := sSpacer.Neighbor("left", layout.Right); !ok || got != "right" {
			t.Fatalf("Neighbor(left, Right) = (%q, %v), want (right, true)", got, ok)
		}
	})

	t.Run("guards: zero-value, empty, unknown, single node, overlapping centers, invalid Dir", func(t *testing.T) {
		var zero layout.Solved
		if got, ok := zero.Neighbor("a", layout.Right); ok || got != "" {
			t.Fatalf("zero Neighbor = (%q, %v)", got, ok)
		}
		single := layout.Solved{
			Rects:      map[string]layout.Rect{"a": {X: 0, Y: 0, W: 10, H: 10}},
			FocusOrder: []string{"a"},
		}
		for _, d := range []layout.Dir{layout.Left, layout.Right, layout.Up, layout.Down, layout.Dir(99)} {
			if got, ok := single.Neighbor("a", d); ok || got != "" {
				t.Fatalf("single node dir %v = (%q, %v)", d, got, ok)
			}
		}
		if got, ok := single.Neighbor("", layout.Right); ok || got != "" {
			t.Fatalf("empty fromID = (%q, %v)", got, ok)
		}
		if got, ok := single.Neighbor("missing", layout.Right); ok || got != "" {
			t.Fatalf("missing fromID = (%q, %v)", got, ok)
		}

		// Concentric overlapping rects must not jump spuriously.
		concentric := layout.Solved{
			Rects: map[string]layout.Rect{
				"outer": {X: 0, Y: 0, W: 10, H: 10},
				"inner": {X: 2, Y: 2, W: 6, H: 6},
			},
			FocusOrder: []string{"outer", "inner"},
		}
		for _, d := range []layout.Dir{layout.Left, layout.Right, layout.Up, layout.Down} {
			if got, ok := concentric.Neighbor("outer", d); ok || got != "" {
				t.Fatalf("concentric dir %v = (%q, %v)", d, got, ok)
			}
		}
	})
}

func TestRegression_OverlayOrderingAndWrappers(t *testing.T) {
	t.Run("outer flow-container overlays paint and focus above inner flow-child overlays", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			kind layout.BoxKind
			cols int
		}{
			{"BoxStack", layout.BoxStack, 0},
			{"BoxSplit", layout.BoxSplit, 0},
			{"BoxGrid", layout.BoxGrid, 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				root := layout.Box{
					ID:   "app",
					Kind: tc.kind,
					Cols: tc.cols,
					Children: []layout.Box{
						{
							ID:   "page",
							Kind: layout.BoxStack,
							Children: []layout.Box{
								{ID: "input", Focusable: true},
								{
									ID:          "local-dropdown",
									Kind:        layout.BoxOverlay,
									Focusable:   true,
									Constraints: layout.Constraints{FixedW: 10, FixedH: 4},
								},
							},
						},
						{
							ID:          "global-modal",
							Kind:        layout.BoxOverlay,
							Focusable:   true,
							Constraints: layout.Constraints{FixedW: 10, FixedH: 4},
						},
					},
				}
				s := layout.Solve(root, 40, 20)
				wantRect := layout.Rect{X: 15, Y: 8, W: 10, H: 4}
				if s.Rects["local-dropdown"] != wantRect || s.Rects["global-modal"] != wantRect {
					t.Fatalf("unexpected overlay rects: local=%+v global=%+v", s.Rects["local-dropdown"], s.Rects["global-modal"])
				}
				if id, lx, ly := s.HitTest(16, 9); id != "global-modal" || lx != 1 || ly != 1 {
					t.Fatalf("HitTest(16,9) = (%q,%d,%d), want (global-modal,1,1)", id, lx, ly)
				}
				if !reflect.DeepEqual(s.FocusOrder, []string{"global-modal"}) {
					t.Fatalf("FocusOrder = %v, want [global-modal]", s.FocusOrder)
				}
			})
		}
	})

	t.Run("root single-child configured BoxOverlay keeps root in FocusOrder", func(t *testing.T) {
		modal := layout.Box{
			ID:          "modal",
			Kind:        layout.BoxOverlay,
			Border:      true,
			Focusable:   true,
			Constraints: layout.Constraints{FixedW: 20, FixedH: 8},
			Children: []layout.Box{
				{
					Kind: layout.BoxStack,
					Children: []layout.Box{
						{ID: "m-ok", Focusable: true},
						{ID: "m-cancel", Focusable: true},
					},
				},
			},
		}
		sRoot := layout.Solve(modal, 40, 20)
		want := []string{"modal", "m-ok", "m-cancel"}
		if !reflect.DeepEqual(sRoot.FocusOrder, want) {
			t.Fatalf("root modal FocusOrder = %v, want %v", sRoot.FocusOrder, want)
		}
		if id, _, _ := sRoot.HitTest(0, 0); id != "modal" {
			t.Fatalf("HitTest(0,0) = %q, want modal", id)
		}

		// When a configured root BoxOverlay has a BoxOverlay child, that child promotes to a higher layer.
		nestedModal := modal
		nestedModal.Children = []layout.Box{
			{
				ID:          "nested-ov",
				Kind:        layout.BoxOverlay,
				Focusable:   true,
				Constraints: layout.Constraints{FixedW: 8, FixedH: 2},
			},
		}
		sNested := layout.Solve(nestedModal, 40, 20)
		if !reflect.DeepEqual(sNested.FocusOrder, []string{"nested-ov"}) {
			t.Fatalf("nested overlay FocusOrder = %v, want [nested-ov]", sNested.FocusOrder)
		}
	})

	t.Run("anchored single-child transparent BoxOverlay wrapper delegates anchor to child", func(t *testing.T) {
		root := layout.Box{
			Kind: layout.BoxStack,
			Children: []layout.Box{
				{ID: "spacer", Constraints: layout.Constraints{FixedH: 5}},
				{ID: "btn", Constraints: layout.Constraints{FixedH: 3}},
				{
					Kind:     layout.BoxOverlay,
					AnchorID: "btn",
					Children: []layout.Box{
						{ID: "tip", Constraints: layout.Constraints{FixedW: 10, FixedH: 2}},
					},
				},
			},
		}
		s := layout.Solve(root, 40, 20)
		if got := s.Rects["btn"]; got != (layout.Rect{X: 0, Y: 5, W: 40, H: 3}) {
			t.Fatalf("btn = %+v, want {0 5 40 3}", got)
		}
		if got := s.Rects["tip"]; got != (layout.Rect{X: 0, Y: 8, W: 10, H: 2}) {
			t.Fatalf("tip = %+v, want {0 8 10 2}", got)
		}

		// Nested transparent wrappers also preserve outer AnchorID.
		nested := layout.Box{
			Kind: layout.BoxStack,
			Children: []layout.Box{
				{ID: "spacer", Constraints: layout.Constraints{FixedH: 5}},
				{ID: "btn", Constraints: layout.Constraints{FixedH: 3}},
				{
					Kind:     layout.BoxOverlay,
					AnchorID: "btn",
					Children: []layout.Box{
						{
							Kind: layout.BoxOverlay,
							Children: []layout.Box{
								{ID: "tip", Constraints: layout.Constraints{FixedW: 10, FixedH: 2}},
							},
						},
					},
				},
			},
		}
		sNested := layout.Solve(nested, 40, 20)
		if got := sNested.Rects["tip"]; got != (layout.Rect{X: 0, Y: 8, W: 10, H: 2}) {
			t.Fatalf("nested tip = %+v, want {0 8 10 2}", got)
		}
	})
}

func TestRegression_ExtremeValuesAndOverflowSafety(t *testing.T) {
	assertChildContained := func(t *testing.T, id string, r, parentInner layout.Rect) {
		t.Helper()
		if r.W < 0 || r.H < 0 {
			t.Fatalf("%s negative dims %+v", id, r)
		}
		if r.X < parentInner.X || r.Y < parentInner.Y ||
			int64(r.X)+int64(r.W) > int64(parentInner.X)+int64(parentInner.W) ||
			int64(r.Y)+int64(r.H) > int64(parentInner.Y)+int64(parentInner.H) {
			t.Fatalf("%s %+v escaped parent inner %+v", id, r, parentInner)
		}
	}

	t.Run("MaxInt32 4-child water-filling", func(t *testing.T) {
		root := layout.Box{
			ID:   "root",
			Kind: layout.BoxStack,
			Axis: layout.Horizontal,
			Children: []layout.Box{
				{ID: "c0", Constraints: layout.Constraints{Flex: math.MaxInt32, MaxW: 1_500_000_000}},
				{ID: "c1", Constraints: layout.Constraints{Flex: math.MaxInt32, MaxW: 1_500_000_000}},
				{ID: "c2", Constraints: layout.Constraints{Flex: math.MaxInt32, MaxW: 1_500_000_000}},
				{ID: "c3", Constraints: layout.Constraints{Flex: math.MaxInt32, MaxW: 1_500_000_000}},
			},
		}
		s := layout.Solve(root, math.MaxInt32, 10)
		wantW := []int{536870912, 536870912, 536870912, 536870911}
		for i, id := range []string{"c0", "c1", "c2", "c3"} {
			if s.Rects[id].W != wantW[i] {
				t.Fatalf("%s.W = %d, want %d (rects=%+v)", id, s.Rects[id].W, wantW[i], s.Rects)
			}
		}
	})

	t.Run("MaxInt Flex weight", func(t *testing.T) {
		root := layout.Box{
			ID:   "root",
			Kind: layout.BoxStack,
			Axis: layout.Horizontal,
			Children: []layout.Box{
				{ID: "a", Constraints: layout.Constraints{Flex: math.MaxInt}},
				{ID: "b", Constraints: layout.Constraints{Flex: math.MaxInt}},
			},
		}
		s := layout.Solve(root, 80, 24)
		if s.Rects["a"] != (layout.Rect{X: 0, Y: 0, W: 40, H: 24}) ||
			s.Rects["b"] != (layout.Rect{X: 40, Y: 0, W: 40, H: 24}) {
			t.Fatalf("MaxInt Flex gave %+v", s.Rects)
		}
	})

	t.Run("MaxInt Cols and MinColW+Gap in BoxGrid", func(t *testing.T) {
		sCols := layout.Solve(layout.Box{
			ID:       "grid",
			Kind:     layout.BoxGrid,
			Cols:     math.MaxInt,
			Children: []layout.Box{{ID: "a"}},
		}, 80, 24)
		assertChildContained(t, "a", sCols.Rects["a"], sCols.InnerRects["grid"])
		if sCols.Rects["a"] != (layout.Rect{X: 0, Y: 0, W: 1, H: 24}) {
			t.Fatalf("MaxInt Cols (gap=0) child a = %+v, want {0 0 1 24}", sCols.Rects["a"])
		}

		sColsGap := layout.Solve(layout.Box{
			ID:       "grid",
			Kind:     layout.BoxGrid,
			Cols:     math.MaxInt,
			Gap:      1,
			Children: []layout.Box{{ID: "a"}},
		}, 80, 24)
		assertChildContained(t, "a", sColsGap.Rects["a"], sColsGap.InnerRects["grid"])
		if sColsGap.Rects["a"] != (layout.Rect{X: 0, Y: 0, W: 0, H: 24}) {
			t.Fatalf("MaxInt Cols (gap=1) child a = %+v, want {0 0 0 24}", sColsGap.Rects["a"])
		}

		sMinCol := layout.Solve(layout.Box{
			ID:       "grid",
			Kind:     layout.BoxGrid,
			MinColW:  math.MaxInt,
			Gap:      math.MaxInt,
			Children: []layout.Box{{ID: "a"}, {ID: "b"}},
		}, math.MaxInt, 10)
		assertChildContained(t, "a", sMinCol.Rects["a"], sMinCol.InnerRects["grid"])
		assertChildContained(t, "b", sMinCol.Rects["b"], sMinCol.InnerRects["grid"])
	})

	t.Run("MaxInt Padding with and without Border", func(t *testing.T) {
		for _, border := range []bool{true, false} {
			s := layout.Solve(layout.Box{
				ID:      "root",
				Border:  border,
				Padding: [4]int{math.MaxInt, math.MaxInt, math.MaxInt, math.MaxInt},
			}, 80, 24)
			assertInnerSubset(t, "root", s.Rects["root"], s.InnerRects["root"])
			if s.InnerRects["root"].W != 0 || s.InnerRects["root"].H != 0 {
				t.Fatalf("border=%v inner = %+v, want 0x0", border, s.InnerRects["root"])
			}
		}
	})

	t.Run("MaxInt Gap in BoxStack, BoxGrid, and BoxSplit with nonzero mainStart", func(t *testing.T) {
		sStack := layout.Solve(layout.Box{
			ID:       "root",
			Kind:     layout.BoxStack,
			Axis:     layout.Horizontal,
			Gap:      math.MaxInt,
			Padding:  [4]int{0, 0, 0, 5},
			Children: []layout.Box{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		}, 80, 24)
		for _, id := range []string{"a", "b", "c"} {
			assertChildContained(t, id, sStack.Rects[id], sStack.InnerRects["root"])
		}

		sGrid := layout.Solve(layout.Box{
			ID:       "root",
			Kind:     layout.BoxGrid,
			Cols:     3,
			Gap:      math.MaxInt,
			Padding:  [4]int{1, 0, 0, 5},
			Children: []layout.Box{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}},
		}, 80, 24)
		for _, id := range []string{"a", "b", "c", "d"} {
			assertChildContained(t, id, sGrid.Rects[id], sGrid.InnerRects["root"])
		}

		sSplit := layout.Solve(layout.Box{
			ID:       "root",
			Kind:     layout.BoxSplit,
			Axis:     layout.Horizontal,
			Gap:      math.MaxInt,
			Padding:  [4]int{0, 0, 0, 5},
			Children: []layout.Box{{ID: "a"}, {ID: "b"}},
		}, 80, 24)
		assertChildContained(t, "a", sSplit.Rects["a"], sSplit.InnerRects["root"])
		assertChildContained(t, "b", sSplit.Rects["b"], sSplit.InnerRects["root"])
	})

	t.Run("BoxSplit MaxInt Ratio=1.0 and Min conflict scaling", func(t *testing.T) {
		sRatio := layout.Solve(layout.Box{
			ID:       "root",
			Kind:     layout.BoxSplit,
			Axis:     layout.Horizontal,
			Ratio:    1.0,
			Children: []layout.Box{{ID: "a"}, {ID: "b"}},
		}, math.MaxInt, 10)
		if sRatio.Rects["a"].W != math.MaxInt || sRatio.Rects["b"].W != 0 {
			t.Fatalf("MaxInt Ratio=1.0 split = %+v", sRatio.Rects)
		}

		sMin := layout.Solve(layout.Box{
			ID:   "root",
			Kind: layout.BoxSplit,
			Axis: layout.Horizontal,
			Children: []layout.Box{
				{ID: "a", Constraints: layout.Constraints{MinW: math.MaxInt}},
				{ID: "b", Constraints: layout.Constraints{MinW: math.MaxInt}},
			},
		}, math.MaxInt, 10)
		if int64(sMin.Rects["a"].W)+int64(sMin.Rects["b"].W) != int64(math.MaxInt) {
			t.Fatalf("MaxInt Min conflict sum != MaxInt: %+v", sMin.Rects)
		}
		assertChildContained(t, "a", sMin.Rects["a"], sMin.InnerRects["root"])
		assertChildContained(t, "b", sMin.Rects["b"], sMin.InnerRects["root"])
	})

	t.Run("extreme coordinate Neighbor and HitTest", func(t *testing.T) {
		s := layout.Solve(layout.Box{
			Kind: layout.BoxStack,
			Axis: layout.Horizontal,
			Children: []layout.Box{
				{ID: "a", Focusable: true},
				{ID: "b", Focusable: true},
			},
		}, math.MaxInt, math.MaxInt)
		if got, ok := s.Neighbor("a", layout.Right); !ok || got != "b" {
			t.Fatalf("Neighbor(a, Right) = (%q, %v), want (b, true)", got, ok)
		}
		if got, ok := s.Neighbor("b", layout.Left); !ok || got != "a" {
			t.Fatalf("Neighbor(b, Left) = (%q, %v), want (a, true)", got, ok)
		}
		if id, lx, ly := s.HitTest(math.MaxInt-1, math.MaxInt-1); id != "b" || lx != s.Rects["b"].W-1 || ly != math.MaxInt-1 {
			t.Fatalf("HitTest extreme = (%q, %d, %d)", id, lx, ly)
		}

		manual := layout.Solved{
			Rects: map[string]layout.Rect{
				"big":   {X: 0, Y: 0, W: math.MaxInt, H: math.MaxInt},
				"small": {X: math.MaxInt - 10, Y: math.MaxInt - 10, W: 10, H: 10},
			},
			FocusOrder: []string{"big", "small"},
		}
		if id, lx, ly := manual.HitTest(math.MaxInt-1, math.MaxInt-1); id != "small" || lx != 9 || ly != 9 {
			t.Fatalf("manual HitTest extreme = (%q, %d, %d), want (small, 9, 9)", id, lx, ly)
		}
	})

	t.Run("bottom-anchored and centered overlay at MaxInt dimensions", func(t *testing.T) {
		root := layout.Box{
			Kind: layout.BoxOverlay,
			Children: []layout.Box{
				{
					Kind: layout.BoxStack,
					Axis: layout.Vertical,
					Children: []layout.Box{
						{ID: "spacer"},
						{ID: "bot-btn", Constraints: layout.Constraints{FixedH: 10}},
					},
				},
				{
					ID:          "menu",
					AnchorID:    "bot-btn",
					Constraints: layout.Constraints{FixedW: 10, FixedH: 5},
				},
			},
		}
		s := layout.Solve(root, 40, math.MaxInt)
		wantBtn := layout.Rect{X: 0, Y: math.MaxInt - 10, W: 40, H: 10}
		if s.Rects["bot-btn"] != wantBtn {
			t.Fatalf("bot-btn = %+v, want %+v", s.Rects["bot-btn"], wantBtn)
		}
		wantMenu := layout.Rect{X: 0, Y: math.MaxInt - 15, W: 10, H: 5}
		if s.Rects["menu"] != wantMenu {
			t.Fatalf("menu = %+v, want %+v", s.Rects["menu"], wantMenu)
		}

		// Exact fit below at math.MaxInt boundary (belowY == math.MaxInt-5, oh == 5).
		rootFitBelow := layout.Box{
			Kind: layout.BoxOverlay,
			Children: []layout.Box{
				{
					Kind: layout.BoxStack,
					Axis: layout.Vertical,
					Children: []layout.Box{
						{ID: "spacer"},
						{ID: "mid-btn", Constraints: layout.Constraints{FixedH: 5}},
						{ID: "tail", Constraints: layout.Constraints{FixedH: 5}},
					},
				},
				{
					ID:          "menu",
					AnchorID:    "mid-btn",
					Constraints: layout.Constraints{FixedW: 10, FixedH: 5},
				},
			},
		}
		sBelow := layout.Solve(rootFitBelow, 40, math.MaxInt)
		wantBelow := layout.Rect{X: 0, Y: math.MaxInt - 5, W: 10, H: 5}
		if sBelow.Rects["menu"] != wantBelow {
			t.Fatalf("exact-fit below menu = %+v, want %+v", sBelow.Rects["menu"], wantBelow)
		}

		// Bottom-right corner anchor + centered overlay at (math.MaxInt, math.MaxInt).
		rootCorner := layout.Box{
			Kind: layout.BoxOverlay,
			Children: []layout.Box{
				{
					Kind: layout.BoxStack,
					Axis: layout.Vertical,
					Children: []layout.Box{
						{ID: "top-spacer"},
						{
							Kind:        layout.BoxStack,
							Axis:        layout.Horizontal,
							Constraints: layout.Constraints{FixedH: 10},
							Children: []layout.Box{
								{ID: "left-spacer"},
								{ID: "corner-btn", Constraints: layout.Constraints{FixedW: 10}},
							},
						},
					},
				},
				{
					ID:          "corner-menu",
					AnchorID:    "corner-btn",
					Constraints: layout.Constraints{FixedW: 15, FixedH: 5},
				},
				{
					ID:          "centered",
					Constraints: layout.Constraints{FixedW: 10, FixedH: 6},
				},
			},
		}
		sCorner := layout.Solve(rootCorner, math.MaxInt, math.MaxInt)
		wantCornerMenu := layout.Rect{X: math.MaxInt - 15, Y: math.MaxInt - 15, W: 15, H: 5}
		if sCorner.Rects["corner-menu"] != wantCornerMenu {
			t.Fatalf("corner-menu = %+v, want %+v", sCorner.Rects["corner-menu"], wantCornerMenu)
		}
		wantCentered := layout.Rect{
			X: (math.MaxInt - 10) / 2,
			Y: (math.MaxInt - 6) / 2,
			W: 10,
			H: 6,
		}
		if sCorner.Rects["centered"] != wantCentered {
			t.Fatalf("centered = %+v, want %+v", sCorner.Rects["centered"], wantCentered)
		}
	})
}
