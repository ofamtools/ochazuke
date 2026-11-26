package ui

import (
	"fmt"
	"testing"

	"ofam.dev/ochazuke/layout"
)

// cardColumn is a KindScroll of n focusable cards, each h rows tall.
func cardColumn(n, h int, props map[string]any) ViewNode {
	var kids []ViewNode
	for i := range n {
		kids = append(kids, ViewNode{ID: fmt.Sprintf("c%d", i), Kind: KindText, Focusable: true,
			Box: layout.Constraints{FixedH: h}, Props: map[string]any{"text": fmt.Sprintf("card %d", i)}})
	}
	if props == nil {
		props = map[string]any{}
	}
	return ViewNode{ID: "col", Kind: KindScroll, Children: kids, Props: props}
}

func scrollOff(t *testing.T, r *Renderer, root ViewNode, focus string, h int) int {
	t.Helper()
	_, s := r.Render(root, DefaultDarkTheme(), focus, 30, h)
	info, ok := s.Scrolls["col"]
	if !ok {
		t.Fatalf("no ScrollInfo for col")
	}
	return info.Offset
}

// statelessOff is the original offset for a focused card [top, top+h).
func statelessOff(top, h, vh, maxOff int) int {
	off := 0
	if top+h > vh {
		off = top + h - vh
	}
	return min(max(min(off, top), 0), maxOff)
}

func TestScrollFollowPaddedAndStateful(t *testing.T) {
	const cards, ch, vh, pad = 20, 3, 12, 2
	maxOff := cards*ch - vh
	r := NewRenderer(0)
	r.ScrollOff = pad
	col := cardColumn(cards, ch, nil)

	off := 0
	for i := range cards {
		off = scrollOff(t, r, col, fmt.Sprintf("c%d", i), vh)
		bottom := i*ch + ch
		if want := min(max(0, bottom+pad-vh), maxOff); off != want {
			t.Fatalf("down to c%d: offset %d, want %d", i, off, want)
		}
		if off > 0 && off < maxOff && off+vh-bottom != pad {
			t.Fatalf("down to c%d: bottom gap %d, want %d", i, off+vh-bottom, pad)
		}
	}
	for i := cards - 1; i >= 10; i-- {
		prev := off
		off = scrollOff(t, r, col, fmt.Sprintf("c%d", i), vh)
		top := i * ch
		if top >= prev+pad {
			if off != prev {
				t.Fatalf("up to c%d inside the padded window: offset moved %d -> %d", i, prev, off)
			}
			continue
		}
		if top-off != pad {
			t.Fatalf("up to c%d: top gap %d, want %d", i, top-off, pad)
		}
	}

	// ScrollOff 0 moving down matches the original flush-bottom offsets.
	r0 := NewRenderer(0)
	for i := range cards {
		if got, want := scrollOff(t, r0, col, fmt.Sprintf("c%d", i), vh), statelessOff(i*ch, ch, vh, maxOff); got != want {
			t.Fatalf("pad 0 down to c%d: offset %d, want %d", i, got, want)
		}
	}
}

func TestScrollFollowExplicitOffsetThenDropped(t *testing.T) {
	r := NewRenderer(0)
	if got := scrollOff(t, r, cardColumn(20, 3, map[string]any{"offset": 20}), "c9", 12); got != 20 {
		t.Fatalf("explicit offset drawn as %d", got)
	}
	if got := scrollOff(t, r, cardColumn(20, 3, nil), "c9", 12); got != 20 {
		t.Fatalf("after dropping offset: %d, want 20 (no snap)", got)
	}
}

func TestScrollFollowStatelessOptOut(t *testing.T) {
	const cards, ch, vh = 20, 3, 12
	maxOff := cards*ch - vh
	field := NewRenderer(0)
	field.ScrollOff, field.StatelessScrollFollow = 2, true
	prop := NewRenderer(0)
	prop.ScrollOff = 2
	propCol := cardColumn(cards, ch, map[string]any{"followStateless": true})
	seq := []int{0, 5, 19, 17, 15, 8, 9, 2}
	for _, i := range seq {
		id := fmt.Sprintf("c%d", i)
		want := statelessOff(i*ch, ch, vh, maxOff)
		if got := scrollOff(t, field, cardColumn(cards, ch, nil), id, vh); got != want {
			t.Errorf("Renderer opt-out at %s: %d, want %d", id, got, want)
		}
		if got := scrollOff(t, prop, propCol, id, vh); got != want {
			t.Errorf("node opt-out at %s: %d, want %d", id, got, want)
		}
	}
}

func TestScrollFollowScrollKeyDropsMemory(t *testing.T) {
	r := NewRenderer(0)
	scrollOff(t, r, cardColumn(20, 3, map[string]any{"offset": 30, "scrollKey": "rec-a"}), "", 12)
	if got := scrollOff(t, r, cardColumn(20, 3, map[string]any{"scrollKey": "rec-a"}), "", 12); got != 30 {
		t.Fatalf("same key: %d, want 30", got)
	}
	// A new key drops the memory before the reveal in the same frame.
	if got := scrollOff(t, r, cardColumn(20, 3, map[string]any{"scrollKey": "rec-b"}), "c4", 12); got != 3 {
		t.Fatalf("new key revealing c4: %d, want 3 (from 0)", got)
	}
	r.Reset()
	if r.ScrollMemoLen() != 0 {
		t.Fatal("Reset should forget scroll offsets")
	}
}

func TestScrollFollowNestedRevealsVisibleFocus(t *testing.T) {
	inner := cardColumn(10, 3, nil)
	inner.ID, inner.Box = "inner", layout.Constraints{FixedH: 6}
	filler := func(id string) ViewNode {
		return ViewNode{ID: id, Kind: KindText, Box: layout.Constraints{FixedH: 10}, Props: map[string]any{"text": id}}
	}
	outer := ViewNode{ID: "outer", Kind: KindScroll, Children: []ViewNode{filler("head"), inner, filler("foot")}}
	r := NewRenderer(0)
	for _, tc := range []struct {
		focus            string
		outerOff, innOff int
	}{
		{"c0", 5, 0},  // inner rows 10..16; c0 visible at 10..13 -> outer 13-8
		{"c5", 8, 12}, // c5 at inner 15..18, inner offset 12 -> visible 13..16 -> outer 16-8
		{"c1", 8, 3},  // c1 at inner 3..6, inner offset 3 -> visible 10..13: inside outer window
	} {
		_, s := r.Render(outer, DefaultDarkTheme(), tc.focus, 30, 8)
		if got := s.Scrolls["outer"].Offset; got != tc.outerOff {
			t.Errorf("focus %s: outer offset %d, want %d", tc.focus, got, tc.outerOff)
		}
		if got := s.Scrolls["inner"].Offset; got != tc.innOff {
			t.Errorf("focus %s: inner offset %d, want %d", tc.focus, got, tc.innOff)
		}
		if fr := s.Rects[tc.focus]; fr.H != 3 {
			t.Errorf("focus %s: visible rect %+v, want fully visible", tc.focus, fr)
		}
	}
}
