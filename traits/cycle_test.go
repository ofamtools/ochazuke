package traits

import (
	"reflect"
	"testing"
)

func TestCycleOrderedChoices(t *testing.T) {
	three := []string{"alpha", "beta", "gamma"}

	cases := []struct {
		name        string
		items       []string
		current     string
		dir         int
		wrap        bool
		want        string
		wantChanged bool
	}{
		{name: "empty forward wrap", items: nil, current: "", dir: 1, wrap: true, want: "", wantChanged: false},
		{name: "empty backward clamp", items: []string{}, current: "x", dir: -1, wrap: false, want: "", wantChanged: false},
		{name: "singleton current forward wrap", items: []string{"only"}, current: "only", dir: 1, wrap: true, want: "only", wantChanged: false},
		{name: "singleton current backward clamp", items: []string{"only"}, current: "only", dir: -1, wrap: false, want: "only", wantChanged: false},
		{name: "singleton absent current", items: []string{"only"}, current: "other", dir: 1, wrap: false, want: "only", wantChanged: true},
		{name: "absent current forward", items: three, current: "missing", dir: 1, wrap: false, want: "alpha", wantChanged: true},
		{name: "absent current dir zero defaults forward", items: three, current: "missing", dir: 0, wrap: false, want: "alpha", wantChanged: true},
		{name: "absent current backward", items: three, current: "missing", dir: -1, wrap: false, want: "gamma", wantChanged: true},
		{name: "dir zero steps forward", items: three, current: "alpha", dir: 0, wrap: true, want: "beta", wantChanged: true},
		{name: "forward middle", items: three, current: "alpha", dir: 1, wrap: true, want: "beta", wantChanged: true},
		{name: "forward wrap end to start", items: three, current: "gamma", dir: 1, wrap: true, want: "alpha", wantChanged: true},
		{name: "forward clamp at end", items: three, current: "gamma", dir: 1, wrap: false, want: "gamma", wantChanged: false},
		{name: "backward wrap start to end", items: three, current: "alpha", dir: -1, wrap: true, want: "gamma", wantChanged: true},
		{name: "backward clamp at start", items: three, current: "alpha", dir: -1, wrap: false, want: "alpha", wantChanged: false},
		{name: "multi-step count forward wrap", items: three, current: "alpha", dir: 2, wrap: true, want: "gamma", wantChanged: true},
		{name: "multi-step count backward wrap", items: three, current: "beta", dir: -4, wrap: true, want: "alpha", wantChanged: true},
		{name: "full cycle back to same item", items: three, current: "beta", dir: 3, wrap: true, want: "beta", wantChanged: false},
		{name: "multi-step clamp forward", items: three, current: "alpha", dir: 9, wrap: false, want: "gamma", wantChanged: true},
		{name: "multi-step clamp backward", items: three, current: "gamma", dir: -9, wrap: false, want: "alpha", wantChanged: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := Cycle(tc.items, tc.current, tc.dir, tc.wrap)
			if got != tc.want || changed != tc.wantChanged {
				t.Fatalf("Cycle(%v, %q, %d, %v) = (%q, %v), want (%q, %v)",
					tc.items, tc.current, tc.dir, tc.wrap, got, changed, tc.want, tc.wantChanged)
			}
		})
	}
}

type stubCollectible struct {
	id         string
	items      map[string][]any
	current    map[string]any
	rejectSet  bool
	setCalls   int
	lastSetKey string
}

func (s *stubCollectible) ID() string             { return s.id }
func (s *stubCollectible) Collect(n string) []any { return s.items[n] }
func (s *stubCollectible) Current(n string) any   { return s.current[n] }
func (s *stubCollectible) SetCurrent(n string, it any) bool {
	s.setCalls++
	s.lastSetKey = n
	if s.rejectSet {
		return false
	}
	if s.current == nil {
		s.current = map[string]any{}
	}
	s.current[n] = it
	return true
}

func TestCycleCollectible(t *testing.T) {
	if got, ok := CycleCollectible(nil, "view", 1, true); got != nil || ok {
		t.Fatalf("nil collectible = (%v, %v), want (nil, false)", got, ok)
	}

	c := &stubCollectible{
		id: "panel.nav",
		items: map[string][]any{
			"view":   {"table", "tree", "cards"},
			"single": {"only"},
			"meta":   {map[string]string{"k": "1"}, map[string]string{"k": "2"}},
		},
		current: map[string]any{
			"view":   "table",
			"single": "only",
			"meta":   map[string]string{"k": "1"},
		},
	}

	if TraitCycle != "cycle" || TraitScope(TraitCycle) != "trait.cycle" {
		t.Fatalf("unexpected TraitCycle=%q scope=%q", TraitCycle, TraitScope(TraitCycle))
	}

	// Empty noun returns nil, false without calling SetCurrent.
	if got, ok := CycleCollectible(c, "missing", 1, true); got != nil || ok || c.setCalls != 0 {
		t.Fatalf("missing noun = (%v, %v) setCalls=%d", got, ok, c.setCalls)
	}

	// Singleton does not call SetCurrent.
	if got, ok := CycleCollectible(c, "single", 1, true); got != "only" || ok || c.setCalls != 0 {
		t.Fatalf("singleton = (%v, %v) setCalls=%d", got, ok, c.setCalls)
	}

	// Forward & wrap around on "view".
	if got, ok := CycleCollectible(c, "view", 1, true); got != "tree" || !ok || c.Current("view") != "tree" {
		t.Fatalf("step 1 = (%v, %v) cur=%v", got, ok, c.Current("view"))
	}
	if got, ok := CycleCollectible(c, "view", -2, true); got != "cards" || !ok || c.Current("view") != "cards" {
		t.Fatalf("step -2 wrap = (%v, %v) cur=%v", got, ok, c.Current("view"))
	}

	// Clamping at end does not call SetCurrent.
	prevCalls := c.setCalls
	if got, ok := CycleCollectible(c, "view", 1, false); got != "cards" || ok || c.setCalls != prevCalls {
		t.Fatalf("clamp at end = (%v, %v) setCalls=%d (prev %d)", got, ok, c.setCalls, prevCalls)
	}

	// Non-comparable items (maps) cycle cleanly without panicking.
	gotMeta, ok := CycleCollectible(c, "meta", 1, true)
	if !ok || !reflect.DeepEqual(gotMeta, map[string]string{"k": "2"}) {
		t.Fatalf("non-comparable cycle = (%v, %v)", gotMeta, ok)
	}

	// Rejected SetCurrent leaves current unchanged and reports false.
	c.rejectSet = true
	if got, ok := CycleCollectible(c, "view", 1, true); got != "cards" || ok {
		t.Fatalf("rejected SetCurrent = (%v, %v), want (cards, false)", got, ok)
	}
}
