package ochazuke

import "testing"

// An anchored modal goes beside its anchor, clear of the text of every row
// it spans; with no room there below it, then above it, else nowhere (the
// caller centers it).
func TestAnchoredPlace(t *testing.T) {
	a := screenRect{left: 4, right: 10, top: 9, bottom: 10}
	ends := make([]int, 30)
	ends[7] = 20 // a longer line above the anchor
	if l, top, ok := anchoredPlace(a, ends, 80, 30, 24, 3); !ok || l != 22 || top != 7 {
		t.Fatalf("beside: %d,%d %v; want 22,7", l, top, ok)
	}
	if l, top, ok := anchoredPlace(a, ends, 34, 30, 24, 3); !ok || l != 4 || top != 10 {
		t.Fatalf("below on a narrow screen: %d,%d %v; want 4,10", l, top, ok)
	}
	low := screenRect{left: 4, right: 10, top: 25, bottom: 26}
	if l, top, ok := anchoredPlace(low, ends, 34, 30, 24, 3); !ok || l != 4 || top != 17 {
		t.Fatalf("above near the bottom: %d,%d %v; want 4,17", l, top, ok)
	}
	if _, _, ok := anchoredPlace(screenRect{left: 4, right: 10, top: 5, bottom: 6}, ends, 34, 12, 24, 3); ok {
		t.Fatal("placed with no room beside, below or above")
	}
}
