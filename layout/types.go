// Package layout implements a pure 2D geometry solver for terminal UI trees,
// supporting leaves, stacks, ratio splits, responsive grids, centered/anchored
// overlays, visual focus ordering, hit-testing, and spatial neighbor navigation.
package layout

import "math/big"

// Dir represents a cardinal 2D focus navigation direction.
type Dir uint8

const (
	Left Dir = iota
	Right
	Up
	Down
)

// Axis represents the primary layout axis for stacks and splits.
type Axis uint8

const (
	Vertical Axis = iota
	Horizontal
)

// Rect represents an axis-aligned 2D rectangle in terminal cell coordinates.
type Rect struct {
	X, Y, W, H int
}

// Constraints specifies sizing preferences and bounds for a Box.
// Zero values mean unconstrained (except Flex <= 0 defaults to 1).
type Constraints struct {
	MinW, MaxW int // 0 = unconstrained
	MinH, MaxH int
	FixedW     int // >0 overrides flex
	FixedH     int
	Flex       int // weight when sharing space (default 1)
}

// BoxKind identifies the layout behavior of a Box node.
type BoxKind uint8

const (
	BoxLeaf    BoxKind = iota
	BoxStack           // VStack / HStack
	BoxSplit           // 2-child ratio split
	BoxGrid            // 2D auto-wrapped card grid (MinColW or Cols)
	BoxOverlay         // centered or anchored modal layered over base
)

// Box is a declarative layout node in a UI geometry tree.
type Box struct {
	ID          string
	Kind        BoxKind
	Axis        Axis
	Ratio       float64 // for BoxSplit (0..1; <= 0 defaults to 0.5)
	Cols        int     // for BoxGrid (0 = auto-fit from MinColW)
	MinColW     int     // for BoxGrid
	Gap         int     // cells between children
	Padding     [4]int  // top, right, bottom, left
	Border      bool    // reserves 1 cell on each edge
	Focusable   bool
	AnchorID    string // optional target node ID for BoxOverlay
	Constraints Constraints
	Children    []Box
}

// Solved holds the geometry and focus navigation outputs produced by Solve.
type Solved struct {
	Rects      map[string]Rect // outer Rect per node ID
	InnerRects map[string]Rect // content Rect after border + padding
	FocusOrder []string        // focusable IDs in visual order (top-to-bottom, left-to-right)

	// hits records visible non-empty-ID nodes in painter's order (base DFS
	// followed by overlays bottom-to-top) for HitTest reverse traversal.
	hits []hitNode
}

type hitNode struct {
	id   string
	rect Rect
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt64(v, lo, hi int64) int64 {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (r Rect) normalize() Rect {
	return Rect{
		X: r.X,
		Y: r.Y,
		W: max(0, r.W),
		H: max(0, r.H),
	}
}

func (r Rect) contains(x, y int) bool {
	if r.W <= 0 || r.H <= 0 || x < r.X || y < r.Y {
		return false
	}
	return uint64(int64(x))-uint64(int64(r.X)) < uint64(r.W) &&
		uint64(int64(y))-uint64(int64(r.Y)) < uint64(r.H)
}

func (r Rect) center2() (cx2, cy2 *big.Int) {
	cx2 = big.NewInt(int64(r.X))
	cx2.Lsh(cx2, 1)
	cx2.Add(cx2, big.NewInt(int64(r.W)))

	cy2 = big.NewInt(int64(r.Y))
	cy2.Lsh(cy2, 1)
	cy2.Add(cy2, big.NewInt(int64(r.H)))
	return cx2, cy2
}

func clampInset(edgePad, border, avail int) int {
	b := min(border, avail)
	p := min(max(0, edgePad), avail-b)
	return b + p
}

// insetRect computes InnerRect from an outer Rect after Border (1 cell per edge)
// and Padding [top, right, bottom, left], guaranteeing inner ⊆ outer and W, H >= 0.
func insetRect(r Rect, border bool, pad [4]int) Rect {
	w := max(0, r.W)
	h := max(0, r.H)

	b := 0
	if border {
		b = 1
	}
	top := clampInset(pad[0], b, h)
	bottom := clampInset(pad[2], b, h-top)
	left := clampInset(pad[3], b, w)
	right := clampInset(pad[1], b, w-left)

	ix := r.X + left
	iy := r.Y + top
	iw := w - left - right
	ih := h - top - bottom
	return Rect{X: ix, Y: iy, W: iw, H: ih}
}

func (c Constraints) flexWeight() int {
	if c.Flex <= 0 {
		return 1
	}
	return c.Flex
}

type axisSpec struct {
	min     int
	max     int
	hasMax  bool
	fixed   int
	isFixed bool
}

// normalizeAxisBounds resolves negative values, Min > Max conflicts (Min wins),
// Fixed > 0 overrides, and parent avail clamping into a normalized axisSpec.
func normalizeAxisBounds(minV, maxV, fixedV, avail int) axisSpec {
	avail = max(0, avail)
	minV = max(0, minV)
	maxV = max(0, maxV)
	fixedV = max(0, fixedV)

	if minV > 0 && maxV > 0 && minV > maxV {
		maxV = minV
	}

	if fixedV > 0 {
		if maxV > 0 {
			fixedV = min(fixedV, maxV)
		}
		if minV > 0 {
			fixedV = max(fixedV, minV)
		}
		fixedV = min(fixedV, avail)
		return axisSpec{
			min:     fixedV,
			max:     fixedV,
			hasMax:  true,
			fixed:   fixedV,
			isFixed: true,
		}
	}

	effMin := min(minV, avail)
	effMax := 0
	hasMax := false
	if maxV > 0 {
		effMax = max(effMin, min(maxV, avail))
		hasMax = true
	}
	return axisSpec{
		min:    effMin,
		max:    effMax,
		hasMax: hasMax,
	}
}

func resolveAxis(minV, maxV, fixedV, avail int) int {
	avail = max(0, avail)
	s := normalizeAxisBounds(minV, maxV, fixedV, avail)
	if s.isFixed {
		return s.fixed
	}
	size := avail
	if s.hasMax {
		size = min(size, s.max)
	}
	if s.min > 0 {
		size = max(size, s.min)
	}
	return min(size, avail)
}

func (c Constraints) resolveSize(availW, availH int) (w, h int) {
	return resolveAxis(c.MinW, c.MaxW, c.FixedW, availW),
		resolveAxis(c.MinH, c.MaxH, c.FixedH, availH)
}
