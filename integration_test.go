package ochazuke

import "testing"

// Tests for interactions between features that landed on separate
// branches (the v0.2.0 integration).

// F9 (? help hides a row whose guard is false) and the detail's guarded
// J/K rows: help lists J/K exactly while the detail overflows.
func TestHelpListsDetailJKOnlyWhileItOverflows(t *testing.T) {
	m, _, _ := detailModel(t, 100)
	if got := helpKeys(m.Runtime); got["J"] == "" || got["K"] == "" {
		t.Fatalf("overflowing detail: help keys %v; want J and K", got)
	}
	m, _, _ = detailModel(t, 3)
	if got := helpKeys(m.Runtime); got["J"] != "" || got["K"] != "" {
		t.Fatalf("detail that fits: help keys %v; want no J or K", got)
	}
}
