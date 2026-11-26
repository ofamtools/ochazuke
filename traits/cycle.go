package traits

import (
	"reflect"
	"slices"
)

// TraitCycle is the canonical trait name for ordered collection cycling.
const TraitCycle = "cycle"

// Collectible exposes ordered items of a given noun (such as views, choices,
// tabs, or records) from an in-memory value object using pure data pointers
// without smuggled side-effecting activation methods.
type Collectible interface {
	ID() string
	Collect(noun string) []any
	Current(noun string) any
	SetCurrent(noun string, item any) bool
}

// Cycle steps through items from current by dir (dir >= 0 steps forward,
// defaulting dir == 0 to +1; dir < 0 steps backward). When current is not in
// items, Cycle selects items[0] for forward movement or items[len(items)-1]
// for backward movement. When wrap is true, stepping past either end wraps
// around; otherwise it clamps to the endpoints. The returned bool reports
// whether the selected item changed.
func Cycle[T comparable](items []T, current T, dir int, wrap bool) (T, bool) {
	if len(items) == 0 {
		var zero T
		return zero, false
	}
	nextIdx, changed := cycleIndex(len(items), slices.Index(items, current), dir, wrap)
	return items[nextIdx], changed
}

// CycleCollectible reads c.Collect(noun) and c.Current(noun), computes the
// next item using the same direction and wrap/clamp rules as Cycle, and writes
// the updated pointer via c.SetCurrent(noun, next) when the selection changes.
func CycleCollectible(c Collectible, noun string, dir int, wrap bool) (any, bool) {
	if c == nil {
		return nil, false
	}
	items := c.Collect(noun)
	if len(items) == 0 {
		return nil, false
	}
	cur := c.Current(noun)
	curIdx := -1
	for i, it := range items {
		if itemEqual(it, cur) {
			curIdx = i
			break
		}
	}
	nextIdx, changed := cycleIndex(len(items), curIdx, dir, wrap)
	if !changed {
		return items[nextIdx], false
	}
	next := items[nextIdx]
	if !c.SetCurrent(noun, next) {
		return cur, false
	}
	return next, true
}

func cycleIndex(n, idx, dir int, wrap bool) (int, bool) {
	if n <= 0 {
		return -1, false
	}
	step := dir
	if step == 0 {
		step = 1
	}
	if idx < 0 || idx >= n {
		if step < 0 {
			return n - 1, true
		}
		return 0, true
	}
	next := idx + step
	if wrap {
		next = ((next % n) + n) % n
	} else {
		next = min(max(next, 0), n-1)
	}
	return next, next != idx
}

func itemEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb {
		return false
	}
	if ta.Comparable() {
		return a == b
	}
	return reflect.DeepEqual(a, b)
}
