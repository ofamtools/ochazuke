package layout

import (
	"math/big"
	"math/bits"
)

// HitTest finds the topmost overlay node or deepest base node with a non-empty ID
// containing (x, y), returning its ID and coordinates relative to that node's
// outer Rect. Returns ("", 0, 0) if no visible node contains (x, y).
func (s Solved) HitTest(x, y int) (nodeID string, localX, localY int) {
	if len(s.hits) > 0 {
		for i := len(s.hits) - 1; i >= 0; i-- {
			h := s.hits[i]
			if h.rect.contains(x, y) {
				return h.id, x - h.rect.X, y - h.rect.Y
			}
		}
		return "", 0, 0
	}

	// Fallback when Solved is constructed manually without Solve().
	if len(s.Rects) == 0 {
		return "", 0, 0
	}
	var bestID string
	var bestRect Rect
	var bestAreaHi, bestAreaLo uint64
	found := false
	for id, r := range s.Rects {
		if id == "" || !r.contains(x, y) {
			continue
		}
		areaHi, areaLo := bits.Mul64(uint64(r.W), uint64(r.H))
		smallerArea := areaHi < bestAreaHi || (areaHi == bestAreaHi && areaLo < bestAreaLo)
		sameArea := areaHi == bestAreaHi && areaLo == bestAreaLo
		if !found || smallerArea || (sameArea && id < bestID) {
			bestID = id
			bestRect = r
			bestAreaHi = areaHi
			bestAreaLo = areaLo
			found = true
		}
	}
	if !found {
		return "", 0, 0
	}
	return bestID, x - bestRect.X, y - bestRect.Y
}

type neighborScore struct {
	id                string
	hasOverlap        bool
	primaryGap        *big.Int
	crossGap          *big.Int
	overlap           *big.Int
	primaryCenterDist *big.Int
	crossCenterDist   *big.Int
	focusIdx          int
}

func betterNeighbor(a, b neighborScore) bool {
	if a.hasOverlap != b.hasOverlap {
		return a.hasOverlap
	}
	if c := a.primaryGap.Cmp(b.primaryGap); c != 0 {
		return c < 0
	}
	if c := a.crossGap.Cmp(b.crossGap); c != 0 {
		return c < 0
	}
	if c := a.overlap.Cmp(b.overlap); c != 0 {
		return c > 0
	}
	if c := a.primaryCenterDist.Cmp(b.primaryCenterDist); c != 0 {
		return c < 0
	}
	if c := a.crossCenterDist.Cmp(b.crossCenterDist); c != 0 {
		return c < 0
	}
	return a.focusIdx < b.focusIdx
}

// Neighbor selects the best focusable target in s.FocusOrder moving in dir from
// fromID, preferring edge-overlapping neighbors with clean spill fallback.
func (s Solved) Neighbor(fromID string, dir Dir) (nextID string, ok bool) {
	if fromID == "" || len(s.FocusOrder) == 0 || len(s.Rects) == 0 {
		return "", false
	}
	if dir != Left && dir != Right && dir != Up && dir != Down {
		return "", false
	}

	inFocusOrder := false
	for _, id := range s.FocusOrder {
		if id == fromID {
			inFocusOrder = true
			break
		}
	}
	if !inFocusOrder {
		return "", false
	}

	fr, ok := s.Rects[fromID]
	if !ok || fr.W <= 0 || fr.H <= 0 {
		return "", false
	}
	fx2, fy2 := fr.center2()
	frX := big.NewInt(int64(fr.X))
	frY := big.NewInt(int64(fr.Y))
	frEndX := bigAdd(fr.X, fr.W)
	frEndY := bigAdd(fr.Y, fr.H)

	var best neighborScore
	hasBest := false

	for idx, candID := range s.FocusOrder {
		if candID == "" || candID == fromID {
			continue
		}
		cr, ok := s.Rects[candID]
		if !ok || cr.W <= 0 || cr.H <= 0 {
			continue
		}
		cx2, cy2 := cr.center2()
		crX := big.NewInt(int64(cr.X))
		crY := big.NewInt(int64(cr.Y))

		var primaryGap, overlap, crossGap, primaryCD, crossCD *big.Int
		switch dir {
		case Right:
			if crX.Cmp(frEndX) < 0 {
				continue
			}
			primaryGap = new(big.Int).Sub(crX, frEndX)
			overlap = overlapSpanBig(fr.Y, fr.H, cr.Y, cr.H)
			crossGap = edgeGap1DBig(fr.Y, fr.H, cr.Y, cr.H)
			primaryCD = new(big.Int).Sub(cx2, fx2)
			crossCD = new(big.Int).Abs(new(big.Int).Sub(cy2, fy2))
		case Left:
			crEndX := bigAdd(cr.X, cr.W)
			if crEndX.Cmp(frX) > 0 {
				continue
			}
			primaryGap = new(big.Int).Sub(frX, crEndX)
			overlap = overlapSpanBig(fr.Y, fr.H, cr.Y, cr.H)
			crossGap = edgeGap1DBig(fr.Y, fr.H, cr.Y, cr.H)
			primaryCD = new(big.Int).Sub(fx2, cx2)
			crossCD = new(big.Int).Abs(new(big.Int).Sub(cy2, fy2))
		case Down:
			if crY.Cmp(frEndY) < 0 {
				continue
			}
			primaryGap = new(big.Int).Sub(crY, frEndY)
			overlap = overlapSpanBig(fr.X, fr.W, cr.X, cr.W)
			crossGap = edgeGap1DBig(fr.X, fr.W, cr.X, cr.W)
			primaryCD = new(big.Int).Sub(cy2, fy2)
			crossCD = new(big.Int).Abs(new(big.Int).Sub(cx2, fx2))
		case Up:
			crEndY := bigAdd(cr.Y, cr.H)
			if crEndY.Cmp(frY) > 0 {
				continue
			}
			primaryGap = new(big.Int).Sub(frY, crEndY)
			overlap = overlapSpanBig(fr.X, fr.W, cr.X, cr.W)
			crossGap = edgeGap1DBig(fr.X, fr.W, cr.X, cr.W)
			primaryCD = new(big.Int).Sub(fy2, cy2)
			crossCD = new(big.Int).Abs(new(big.Int).Sub(cx2, fx2))
		}

		cand := neighborScore{
			id:                candID,
			hasOverlap:        overlap.Sign() > 0,
			primaryGap:        primaryGap,
			crossGap:          crossGap,
			overlap:           overlap,
			primaryCenterDist: primaryCD,
			crossCenterDist:   crossCD,
			focusIdx:          idx,
		}
		if !hasBest || betterNeighbor(cand, best) {
			best = cand
			hasBest = true
		}
	}

	if !hasBest {
		return "", false
	}
	return best.id, true
}

func bigAdd(a, b int) *big.Int {
	return new(big.Int).Add(big.NewInt(int64(a)), big.NewInt(int64(b)))
}

func overlapSpanBig(a0, aLen, b0, bLen int) *big.Int {
	aStart := big.NewInt(int64(a0))
	aEnd := bigAdd(a0, aLen)
	bStart := big.NewInt(int64(b0))
	bEnd := bigAdd(b0, bLen)

	lo := aStart
	if bStart.Cmp(lo) > 0 {
		lo = bStart
	}
	hi := aEnd
	if bEnd.Cmp(hi) < 0 {
		hi = bEnd
	}
	if hi.Cmp(lo) <= 0 {
		return new(big.Int)
	}
	return new(big.Int).Sub(hi, lo)
}

func edgeGap1DBig(a0, aLen, b0, bLen int) *big.Int {
	aStart := big.NewInt(int64(a0))
	aEnd := bigAdd(a0, aLen)
	bStart := big.NewInt(int64(b0))
	bEnd := bigAdd(b0, bLen)

	if bStart.Cmp(aEnd) > 0 {
		return new(big.Int).Sub(bStart, aEnd)
	}
	if aStart.Cmp(bEnd) > 0 {
		return new(big.Int).Sub(aStart, bEnd)
	}
	return new(big.Int)
}
