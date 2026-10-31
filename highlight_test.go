package ochazuke

import "testing"

func TestHighlightEmptyAndZeroMove(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	for _, wrap := range []bool{false, true} {
		if got := r.moveHighlight(-1, -1, 0, wrap); got != -1 {
			t.Fatalf("empty: %d", got)
		}
		if got := r.moveHighlight(-1, 0, 3, wrap); got != -1 {
			t.Fatalf("zero move from none: %d", got)
		}
		if got := r.moveHighlight(1, 0, 3, wrap); got != 1 {
			t.Fatalf("zero move: %d", got)
		}
	}
	if err := r.Settings().Set("chooser.up-from-none", "invalid"); err == nil {
		t.Fatal("accepted invalid initial-selection policy")
	}
}
