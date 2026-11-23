package core

import "testing"

func TestViewportClampScrollOffFreeMovementAndEdges(t *testing.T) {
	// 50 items, viewport height 20, scrollOff 5 -> maxOff = 30.
	total, h, so := 50, 20, 5

	// Jump to bottom (row 49): offset = 30.
	off := ViewportClamp(0, 49, total, h, so)
	if off != 30 {
		t.Fatalf("at bottom offset = %d, want 30", off)
	}

	// Moving UP from 49 down to 35 MUST NOT scroll the viewport (cursor moves freely up!).
	for sel := 48; sel >= 35; sel-- {
		off = ViewportClamp(off, sel, total, h, so)
		if off != 30 {
			t.Fatalf("moving up to sel=%d scrolled prematurely: offset=%d, want 30", sel, off)
		}
	}

	// Crossing into the top 5-line padding zone (sel=34 < 30+5) scrolls offset to 29!
	off = ViewportClamp(off, 34, total, h, so)
	if off != 29 {
		t.Fatalf("sel=34 offset = %d, want 29", off)
	}

	// Continue up to sel=5 -> offset reaches 0.
	for sel := 33; sel >= 5; sel-- {
		off = ViewportClamp(off, sel, total, h, so)
	}
	if off != 0 {
		t.Fatalf("sel=5 offset = %d, want 0", off)
	}

	// At top edge (sel=4..0), offset stays clamped at 0 so cursor moves to the edge.
	for sel := 4; sel >= 0; sel-- {
		off = ViewportClamp(off, sel, total, h, so)
		if off != 0 {
			t.Fatalf("top edge sel=%d offset = %d, want 0", sel, off)
		}
	}

	// Zero scrollOff: cursor moves freely across all 20 visible rows without scrolling.
	off = ViewportClamp(0, 49, total, h, 0)
	for sel := 48; sel >= 30; sel-- {
		off = ViewportClamp(off, sel, total, h, 0)
		if off != 30 {
			t.Fatalf("scrolloff=0 sel=%d offset = %d, want 30", sel, off)
		}
	}
	off = ViewportClamp(off, 29, total, h, 0)
	if off != 29 {
		t.Fatalf("scrolloff=0 sel=29 offset = %d, want 29", off)
	}
}

func TestViewportScrollCtrlEAndCtrlY(t *testing.T) {
	// ctrl+e (delta +1) scrolls viewport down and nudges cursor only when it crosses top padding.
	off, sel := ViewportScroll(0, 0, 50, 20, 5, 1)
	if off != 1 || sel != 6 {
		t.Fatalf("ctrl+e from (0,0) = (%d,%d), want (1,6)", off, sel)
	}
	// When cursor is already well inside viewport, ctrl+e keeps cursor on same item!
	off, sel = ViewportScroll(1, 10, 50, 20, 5, 1)
	if off != 2 || sel != 10 {
		t.Fatalf("ctrl+e with interior cursor = (%d,%d), want (2,10)", off, sel)
	}
	// ctrl+y (delta -1) scrolls viewport up, keeping interior cursor fixed.
	off, sel = ViewportScroll(10, 18, 50, 20, 5, -1)
	if off != 9 || sel != 18 {
		t.Fatalf("ctrl+y with interior cursor = (%d,%d), want (9,18)", off, sel)
	}
}
