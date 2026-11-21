package layout

import (
	"cmp"
	"math"
	"math/big"
	"slices"
)

type focusCandidate struct {
	id       string
	rect     Rect
	dfsIdx   int
	layerIdx int
}

type pendingOverlay struct {
	box            Box
	container      Rect
	fallbackAnchor string
}

type solveState struct {
	rects      map[string]Rect
	innerRects map[string]Rect
	hits       []hitNode
	focus      []focusCandidate
	dfsSeq     int
	nextLayer  int
}

// Solve computes outer and inner rectangles for every named node in root within
// a viewport of width x height terminal cells, along with FocusOrder and
// hit-testing metadata.
func Solve(root Box, width, height int) Solved {
	availW := max(0, width)
	availH := max(0, height)
	rw, rh := root.Constraints.resolveSize(availW, availH)
	rootRect := Rect{X: 0, Y: 0, W: rw, H: rh}

	st := &solveState{
		rects:      make(map[string]Rect),
		innerRects: make(map[string]Rect),
		nextLayer:  1,
	}

	var rootOverlays []pendingOverlay
	st.solveNode(root, rootRect, 0, &rootOverlays)
	if len(rootOverlays) > 0 {
		st.flushOverlays(rootOverlays)
	}

	focusOrder := buildFocusOrder(st.focus, st.rects)
	return Solved{
		Rects:      st.rects,
		InnerRects: st.innerRects,
		FocusOrder: focusOrder,
		hits:       st.hits,
	}
}

func (st *solveState) allocLayer() int {
	idx := st.nextLayer
	st.nextLayer++
	return idx
}

func (st *solveState) recordNode(box Box, outer Rect, layerIdx int) Rect {
	outer = outer.normalize()
	inner := insetRect(outer, box.Border, box.Padding)
	if box.ID != "" {
		st.rects[box.ID] = outer
		st.innerRects[box.ID] = inner
		if outer.W > 0 && outer.H > 0 {
			st.hits = append(st.hits, hitNode{id: box.ID, rect: outer})
			if box.Focusable {
				st.dfsSeq++
				st.focus = append(st.focus, focusCandidate{
					id:       box.ID,
					rect:     outer,
					dfsIdx:   st.dfsSeq,
					layerIdx: layerIdx,
				})
			}
		}
	}
	return inner
}

func (st *solveState) solveNode(box Box, outer Rect, layerIdx int, overlays *[]pendingOverlay) {
	switch box.Kind {
	case BoxStack:
		st.solveStack(box, outer, layerIdx, overlays)
	case BoxSplit:
		st.solveSplit(box, outer, layerIdx, overlays)
	case BoxGrid:
		st.solveGrid(box, outer, layerIdx, overlays)
	case BoxOverlay:
		st.solveOverlayBox(box, outer, layerIdx, overlays)
	default:
		// BoxLeaf and any unrecognized BoxKind fall back to leaf behavior.
		st.recordNode(box, outer, layerIdx)
	}
}

func saturatingSubMul(avail, gap int, countMinus1 int64) int {
	if avail <= 0 {
		return 0
	}
	if gap <= 0 || countMinus1 <= 0 {
		return avail
	}
	if int64(gap) > int64(avail)/countMinus1 {
		return 0
	}
	return avail - int(int64(gap)*countMinus1)
}

func advancePos(cur, size, gap, end int) int {
	if cur >= end {
		return end
	}
	avail := int64(end) - int64(cur)
	stepSize := min(max(int64(0), int64(size)), avail)
	avail -= stepSize
	stepGap := min(max(int64(0), int64(gap)), avail)
	return int(int64(cur) + stepSize + stepGap)
}

func isTransparentWrapper(b Box) bool {
	return b.ID == "" &&
		!b.Border &&
		!b.Focusable &&
		b.Padding == [4]int{} &&
		b.Constraints == (Constraints{})
}

func partitionFlowAndOverlays(children []Box, container Rect) (flow []Box, localOverlays []pendingOverlay) {
	for _, c := range children {
		if c.Kind == BoxOverlay {
			localOverlays = append(localOverlays, pendingOverlay{
				box:       c,
				container: container,
			})
			continue
		}
		flow = append(flow, c)
	}
	return flow, localOverlays
}

func (st *solveState) solveStack(box Box, outer Rect, layerIdx int, overlays *[]pendingOverlay) {
	inner := st.recordNode(box, outer, layerIdx)
	flow, localOverlays := partitionFlowAndOverlays(box.Children, inner)
	if len(flow) > 0 {
		gap := max(0, box.Gap)
		var mainStart, mainAvail, crossAvail int
		if box.Axis == Horizontal {
			mainStart, mainAvail, crossAvail = inner.X, inner.W, inner.H
		} else {
			mainStart, mainAvail, crossAvail = inner.Y, inner.H, inner.W
		}
		mainEnd := mainStart + mainAvail
		rem := saturatingSubMul(mainAvail, gap, int64(len(flow)-1))

		specs := make([]axisSpec, len(flow))
		weights := make([]int, len(flow))
		for i, child := range flow {
			if box.Axis == Horizontal {
				specs[i] = normalizeAxisBounds(child.Constraints.MinW, child.Constraints.MaxW, child.Constraints.FixedW, rem)
			} else {
				specs[i] = normalizeAxisBounds(child.Constraints.MinH, child.Constraints.MaxH, child.Constraints.FixedH, rem)
			}
			weights[i] = child.Constraints.flexWeight()
		}

		sizes := allocateSizes(specs, weights, rem)
		pos := mainStart
		for i, child := range flow {
			cur := min(mainEnd, pos)
			var childRect Rect
			if box.Axis == Horizontal {
				ch := resolveAxis(child.Constraints.MinH, child.Constraints.MaxH, child.Constraints.FixedH, crossAvail)
				childRect = Rect{X: cur, Y: inner.Y, W: sizes[i], H: ch}
			} else {
				cw := resolveAxis(child.Constraints.MinW, child.Constraints.MaxW, child.Constraints.FixedW, crossAvail)
				childRect = Rect{X: inner.X, Y: cur, W: cw, H: sizes[i]}
			}
			st.solveNode(child, childRect, layerIdx, overlays)
			pos = advancePos(cur, sizes[i], gap, mainEnd)
		}
	}
	*overlays = append(*overlays, localOverlays...)
}

func (st *solveState) solveSplit(box Box, outer Rect, layerIdx int, overlays *[]pendingOverlay) {
	inner := st.recordNode(box, outer, layerIdx)
	flow, localOverlays := partitionFlowAndOverlays(box.Children, inner)
	switch len(flow) {
	case 0:
		// no flow children
	case 1:
		cw, ch := flow[0].Constraints.resolveSize(inner.W, inner.H)
		st.solveNode(flow[0], Rect{X: inner.X, Y: inner.Y, W: cw, H: ch}, layerIdx, overlays)
	default:
		c0, c1 := flow[0], flow[1]
		gap := max(0, box.Gap)

		var mainStart, mainAvail, crossAvail int
		if box.Axis == Horizontal {
			mainStart, mainAvail, crossAvail = inner.X, inner.W, inner.H
		} else {
			mainStart, mainAvail, crossAvail = inner.Y, inner.H, inner.W
		}
		mainEnd := mainStart + mainAvail
		rem := saturatingSubMul(mainAvail, gap, 1)

		ratio := box.Ratio
		if math.IsNaN(ratio) || ratio <= 0 {
			ratio = 0.5
		} else if ratio > 1 {
			ratio = 1.0
		}

		var s0, s1 axisSpec
		if box.Axis == Horizontal {
			s0 = normalizeAxisBounds(c0.Constraints.MinW, c0.Constraints.MaxW, c0.Constraints.FixedW, rem)
			s1 = normalizeAxisBounds(c1.Constraints.MinW, c1.Constraints.MaxW, c1.Constraints.FixedW, rem)
		} else {
			s0 = normalizeAxisBounds(c0.Constraints.MinH, c0.Constraints.MaxH, c0.Constraints.FixedH, rem)
			s1 = normalizeAxisBounds(c1.Constraints.MinH, c1.Constraints.MaxH, c1.Constraints.FixedH, rem)
		}

		var m0, m1 int
		switch {
		case s0.isFixed && s1.isFixed:
			m0 = s0.fixed
			m1 = min(s1.fixed, rem-m0)
		case s0.isFixed:
			m0 = s0.fixed
			m1 = rem - m0
			if s1.hasMax && m1 > s1.max {
				m1 = s1.max
			}
		case s1.isFixed:
			m1 = s1.fixed
			m0 = rem - m1
			if s0.hasMax && m0 > s0.max {
				m0 = s0.max
			}
		default:
			if ratio >= 1.0 {
				m0 = rem
			} else {
				f := math.Round(float64(rem) * ratio)
				if f >= float64(rem) {
					m0 = rem
				} else if f <= 0 {
					m0 = 0
				} else {
					m0 = int(f)
				}
			}
			lo0 := s0.min
			hi0 := rem
			if s0.hasMax {
				hi0 = s0.max
			}
			lo1 := 0
			if s1.hasMax {
				lo1 = max(0, rem-s1.max)
			}
			hi1 := max(0, rem-s1.min)
			lo := max(lo0, lo1)
			hi := min(hi0, hi1)
			switch {
			case lo <= hi:
				m0 = clampInt(m0, lo, hi)
				m1 = rem - m0
			case s0.min > rem-s1.min:
				sumMin := new(big.Int).Add(big.NewInt(int64(s0.min)), big.NewInt(int64(s1.min)))
				num := new(big.Int).Mul(big.NewInt(int64(rem)), big.NewInt(int64(s0.min)))
				num.Lsh(num, 1)
				num.Add(num, sumMin)
				denom := new(big.Int).Lsh(sumMin, 1)
				q := new(big.Int).Quo(num, denom)
				m0 = int(q.Int64())
				m1 = rem - m0
			default:
				m0 = clampInt(m0, s0.min, hi0)
				m1 = rem - m0
				if s1.hasMax && m1 > s1.max {
					m1 = s1.max
				}
			}
		}

		secondStart := advancePos(mainStart, m0, gap, mainEnd)
		var r0, r1 Rect
		if box.Axis == Horizontal {
			h0 := resolveAxis(c0.Constraints.MinH, c0.Constraints.MaxH, c0.Constraints.FixedH, crossAvail)
			h1 := resolveAxis(c1.Constraints.MinH, c1.Constraints.MaxH, c1.Constraints.FixedH, crossAvail)
			r0 = Rect{X: mainStart, Y: inner.Y, W: m0, H: h0}
			r1 = Rect{X: secondStart, Y: inner.Y, W: m1, H: h1}
		} else {
			w0 := resolveAxis(c0.Constraints.MinW, c0.Constraints.MaxW, c0.Constraints.FixedW, crossAvail)
			w1 := resolveAxis(c1.Constraints.MinW, c1.Constraints.MaxW, c1.Constraints.FixedW, crossAvail)
			r0 = Rect{X: inner.X, Y: mainStart, W: w0, H: m0}
			r1 = Rect{X: inner.X, Y: secondStart, W: w1, H: m1}
		}
		st.solveNode(c0, r0, layerIdx, overlays)
		st.solveNode(c1, r1, layerIdx, overlays)
	}
	*overlays = append(*overlays, localOverlays...)
}

func (st *solveState) solveGrid(box Box, outer Rect, layerIdx int, overlays *[]pendingOverlay) {
	inner := st.recordNode(box, outer, layerIdx)
	flow, localOverlays := partitionFlowAndOverlays(box.Children, inner)
	if len(flow) > 0 {
		gap := max(0, box.Gap)
		var cols64 int64 = 1
		if box.Cols > 0 {
			cols64 = int64(box.Cols)
		} else if box.MinColW > 0 {
			denom := uint64(box.MinColW) + uint64(gap)
			q := (uint64(inner.W) + uint64(gap)) / denom
			if q > 1 {
				cols64 = int64(q)
			}
		}

		colBudget := saturatingSubMul(inner.W, gap, cols64-1)
		baseColW := int(int64(colBudget) / cols64)
		colRem := int64(colBudget) % cols64
		visCols := int(min(cols64, int64(len(flow))))
		colW := make([]int, visCols)
		colX := make([]int, visCols)
		cx := inner.X
		xEnd := inner.X + inner.W
		for c := 0; c < visCols; c++ {
			w := baseColW
			if int64(c) < colRem {
				w++
			}
			colW[c] = w
			colX[c] = min(xEnd, cx)
			cx = advancePos(colX[c], w, gap, xEnd)
		}

		rows := 1
		if cols64 < int64(len(flow)) {
			rows = 1 + (len(flow)-1)/visCols
		}
		rowBudget := saturatingSubMul(inner.H, gap, int64(rows-1))
		rowSpecs := make([]axisSpec, rows)
		rowWeights := make([]int, rows)

		for r := 0; r < rows; r++ {
			start := r * visCols
			end := min(len(flow), start+visCols)
			rowMin := 0
			rowFixed := 0
			hasFixed := false
			allHaveMax := true
			rowMax := 0
			rowFlex := 1

			for _, child := range flow[start:end] {
				cs := normalizeAxisBounds(child.Constraints.MinH, child.Constraints.MaxH, child.Constraints.FixedH, rowBudget)
				if cs.isFixed {
					hasFixed = true
					rowFixed = max(rowFixed, cs.fixed)
					rowMin = max(rowMin, cs.fixed)
				} else {
					rowMin = max(rowMin, cs.min)
				}
				if cs.hasMax {
					rowMax = max(rowMax, cs.max)
				} else {
					allHaveMax = false
				}
				rowFlex = max(rowFlex, child.Constraints.flexWeight())
			}

			if hasFixed {
				effFixed := max(rowFixed, rowMin)
				rowSpecs[r] = axisSpec{
					min:     effFixed,
					max:     effFixed,
					hasMax:  true,
					fixed:   effFixed,
					isFixed: true,
				}
			} else {
				if allHaveMax && rowMax < rowMin {
					rowMax = rowMin
				}
				rowSpecs[r] = axisSpec{
					min:    rowMin,
					max:    rowMax,
					hasMax: allHaveMax,
				}
			}
			rowWeights[r] = rowFlex
		}

		rowH := allocateSizes(rowSpecs, rowWeights, rowBudget)
		cy := inner.Y
		yEnd := inner.Y + inner.H
		for r := 0; r < rows; r++ {
			ry := min(yEnd, cy)
			start := r * visCols
			end := min(len(flow), start+visCols)
			for c := 0; c < end-start; c++ {
				child := flow[start+c]
				cw := resolveAxis(child.Constraints.MinW, child.Constraints.MaxW, child.Constraints.FixedW, colW[c])
				ch := resolveAxis(child.Constraints.MinH, child.Constraints.MaxH, child.Constraints.FixedH, rowH[r])
				st.solveNode(child, Rect{X: colX[c], Y: ry, W: cw, H: ch}, layerIdx, overlays)
			}
			cy = advancePos(ry, rowH[r], gap, yEnd)
		}
	}
	*overlays = append(*overlays, localOverlays...)
}

func (st *solveState) solveOverlayBox(box Box, outer Rect, layerIdx int, overlays *[]pendingOverlay) {
	inner := st.recordNode(box, outer, layerIdx)
	switch len(box.Children) {
	case 0:
		return
	case 1:
		child := box.Children[0]
		if child.Kind == BoxOverlay || child.AnchorID != "" || isTransparentWrapper(box) {
			*overlays = append(*overlays, pendingOverlay{
				box:            child,
				container:      inner,
				fallbackAnchor: box.AnchorID,
			})
		} else {
			cw, ch := child.Constraints.resolveSize(inner.W, inner.H)
			st.solveNode(child, Rect{X: inner.X, Y: inner.Y, W: cw, H: ch}, layerIdx, overlays)
		}
	default:
		base := box.Children[0]
		bw, bh := base.Constraints.resolveSize(inner.W, inner.H)
		st.solveNode(base, Rect{X: inner.X, Y: inner.Y, W: bw, H: bh}, layerIdx, overlays)
		for _, ov := range box.Children[1:] {
			*overlays = append(*overlays, pendingOverlay{
				box:            ov,
				container:      inner,
				fallbackAnchor: box.AnchorID,
			})
		}
	}
}

func (st *solveState) flushOverlays(overlays []pendingOverlay) {
	for _, p := range overlays {
		st.placeOverlay(p)
	}
}

func (st *solveState) placeOverlay(p pendingOverlay) {
	if p.box.Kind == BoxOverlay && len(p.box.Children) == 1 && isTransparentWrapper(p.box) {
		effectiveAnchor := p.box.AnchorID
		if effectiveAnchor == "" {
			effectiveAnchor = p.fallbackAnchor
		}
		st.placeOverlay(pendingOverlay{
			box:            p.box.Children[0],
			container:      p.container,
			fallbackAnchor: effectiveAnchor,
		})
		return
	}

	availW := max(0, p.container.W)
	availH := max(0, p.container.H)
	ow, oh := p.box.Constraints.resolveSize(availW, availH)

	anchorID := p.box.AnchorID
	if anchorID == "" {
		anchorID = p.fallbackAnchor
	}

	var ox, oy int
	if anchor, ok := st.rects[anchorID]; anchorID != "" && ok {
		cLeft := int64(p.container.X)
		cRightMax := int64(p.container.X) + int64(availW-ow)
		ox = int(clampInt64(int64(anchor.X), cLeft, cRightMax))

		belowY := int64(anchor.Y) + int64(anchor.H)
		aboveY := int64(anchor.Y) - int64(oh)
		cTop := int64(p.container.Y)
		cBot := int64(p.container.Y) + int64(availH)
		var oy64 int64
		switch {
		case belowY >= cTop && belowY <= cBot-int64(oh):
			oy64 = belowY
		case aboveY >= cTop && int64(anchor.Y) <= cBot:
			oy64 = aboveY
		default:
			if cBot-belowY >= int64(anchor.Y)-cTop {
				oy64 = belowY
			} else {
				oy64 = aboveY
			}
		}
		oy = int(clampInt64(oy64, cTop, cBot-int64(oh)))
	} else {
		ox = p.container.X + (availW-ow)/2
		oy = p.container.Y + (availH-oh)/2
	}

	ovRect := Rect{X: ox, Y: oy, W: ow, H: oh}
	ovLayerIdx := st.allocLayer()
	var waveOverlays []pendingOverlay

	if p.box.Kind == BoxOverlay {
		ovInner := st.recordNode(p.box, ovRect, ovLayerIdx)
		switch len(p.box.Children) {
		case 0:
			// nothing more to do
		case 1:
			child := p.box.Children[0]
			if child.Kind == BoxOverlay || child.AnchorID != "" {
				waveOverlays = append(waveOverlays, pendingOverlay{
					box:            child,
					container:      ovInner,
					fallbackAnchor: "",
				})
			} else {
				cw, ch := child.Constraints.resolveSize(ovInner.W, ovInner.H)
				st.solveNode(child, Rect{X: ovInner.X, Y: ovInner.Y, W: cw, H: ch}, ovLayerIdx, &waveOverlays)
			}
		default:
			base := p.box.Children[0]
			bw, bh := base.Constraints.resolveSize(ovInner.W, ovInner.H)
			st.solveNode(base, Rect{X: ovInner.X, Y: ovInner.Y, W: bw, H: bh}, ovLayerIdx, &waveOverlays)
			for _, ov := range p.box.Children[1:] {
				waveOverlays = append(waveOverlays, pendingOverlay{
					box:            ov,
					container:      ovInner,
					fallbackAnchor: "",
				})
			}
		}
	} else {
		st.solveNode(p.box, ovRect, ovLayerIdx, &waveOverlays)
	}

	if len(waveOverlays) > 0 {
		st.flushOverlays(waveOverlays)
	}
}

// allocateSizes distributes integer budget across specs using CSS-Flexbox-style
// fixed-first capping, overflow-proof math/big iterative water-filling for
// Min/Max bounds, and largest-remainder integer rounding.
func allocateSizes(specs []axisSpec, weights []int, budget int) []int {
	n := len(specs)
	sizes := make([]int, n)
	if n == 0 || budget <= 0 {
		return sizes
	}

	rem := budget
	fixed := make([]bool, n)

	for i, s := range specs {
		if s.isFixed {
			take := min(s.fixed, rem)
			sizes[i] = take
			rem -= take
			fixed[i] = true
		}
	}
	if rem <= 0 {
		return sizes
	}

	sumMin := 0
	exceedsRem := false
	flexCount := 0
	for i, s := range specs {
		if !fixed[i] {
			if s.min > rem-sumMin {
				exceedsRem = true
			} else {
				sumMin += s.min
			}
			flexCount++
		}
	}
	if flexCount == 0 {
		return sizes
	}

	effSpecs := make([]axisSpec, n)
	if exceedsRem {
		for i, s := range specs {
			effSpecs[i] = axisSpec{min: 0, max: s.min, hasMax: true}
		}
	} else {
		copy(effSpecs, specs)
	}

	totalWeight := new(big.Int)
	totalViolation := new(big.Int)
	remBig := new(big.Int)
	wBig := new(big.Int)
	numBig := new(big.Int)
	boundBig := new(big.Int)
	diffBig := new(big.Int)

	for {
		totalWeight.SetInt64(0)
		for i := range specs {
			if !fixed[i] {
				wBig.SetInt64(int64(weights[i]))
				totalWeight.Add(totalWeight, wBig)
			}
		}
		if totalWeight.Sign() <= 0 || rem <= 0 {
			break
		}

		totalViolation.SetInt64(0)
		remBig.SetInt64(int64(rem))
		minViol := make([]bool, n)
		maxViol := make([]bool, n)
		anyViol := false

		for i := range specs {
			if fixed[i] {
				continue
			}
			s := effSpecs[i]
			wBig.SetInt64(int64(weights[i]))
			numBig.Mul(remBig, wBig)

			boundBig.SetInt64(int64(s.min))
			boundBig.Mul(boundBig, totalWeight)
			if numBig.Cmp(boundBig) < 0 {
				minViol[i] = true
				anyViol = true
				diffBig.Sub(boundBig, numBig)
				totalViolation.Add(totalViolation, diffBig)
			} else if s.hasMax {
				boundBig.SetInt64(int64(s.max))
				boundBig.Mul(boundBig, totalWeight)
				if numBig.Cmp(boundBig) > 0 {
					maxViol[i] = true
					anyViol = true
					diffBig.Sub(boundBig, numBig)
					totalViolation.Add(totalViolation, diffBig)
				}
			}
		}

		if !anyViol {
			break
		}

		violSign := totalViolation.Sign()
		frozeAny := false
		for i := range specs {
			if fixed[i] {
				continue
			}
			s := effSpecs[i]
			switch {
			case violSign > 0 && minViol[i]:
				take := min(s.min, rem)
				sizes[i] = take
				rem -= take
				fixed[i] = true
				frozeAny = true
			case violSign < 0 && maxViol[i]:
				take := min(s.max, rem)
				sizes[i] = take
				rem -= take
				fixed[i] = true
				frozeAny = true
			case violSign == 0 && (minViol[i] || maxViol[i]):
				var take int
				if minViol[i] {
					take = s.min
				} else {
					take = s.max
				}
				take = min(take, rem)
				sizes[i] = take
				rem -= take
				fixed[i] = true
				frozeAny = true
			}
		}
		if !frozeAny {
			break
		}
	}

	totalWeight.SetInt64(0)
	for i := range specs {
		if !fixed[i] {
			wBig.SetInt64(int64(weights[i]))
			totalWeight.Add(totalWeight, wBig)
		}
	}
	if totalWeight.Sign() <= 0 || rem <= 0 {
		return sizes
	}

	type remItem struct {
		idx int
		rem *big.Int
	}
	var order []remItem
	used := 0
	remBig.SetInt64(int64(rem))
	for i := range specs {
		if fixed[i] {
			continue
		}
		wBig.SetInt64(int64(weights[i]))
		numBig.Mul(remBig, wBig)
		q := new(big.Int)
		r := new(big.Int)
		q.QuoRem(numBig, totalWeight, r)
		base := int(q.Int64())
		sizes[i] = base
		used += base
		if r.Sign() > 0 {
			order = append(order, remItem{idx: i, rem: r})
		}
	}

	leftover := rem - used
	slices.SortStableFunc(order, func(a, b remItem) int {
		if c := b.rem.Cmp(a.rem); c != 0 {
			return c
		}
		return cmp.Compare(a.idx, b.idx)
	})
	for k := 0; k < leftover && k < len(order); k++ {
		sizes[order[k].idx]++
	}
	return sizes
}

func buildFocusOrder(cands []focusCandidate, rects map[string]Rect) []string {
	if len(cands) == 0 {
		return nil
	}

	targetLayer := -1
	for _, c := range cands {
		if r, ok := rects[c.id]; ok && r.W > 0 && r.H > 0 {
			if c.layerIdx > targetLayer {
				targetLayer = c.layerIdx
			}
		}
	}
	if targetLayer < 0 {
		return nil
	}

	byID := make(map[string]focusCandidate)
	for _, c := range cands {
		if c.layerIdx != targetLayer {
			continue
		}
		if r, ok := rects[c.id]; ok && r.W > 0 && r.H > 0 {
			c.rect = r
			byID[c.id] = c
		}
	}
	if len(byID) == 0 {
		return nil
	}

	filtered := make([]focusCandidate, 0, len(byID))
	for _, c := range byID {
		filtered = append(filtered, c)
	}
	slices.SortStableFunc(filtered, func(a, b focusCandidate) int {
		if a.rect.Y != b.rect.Y {
			return cmp.Compare(a.rect.Y, b.rect.Y)
		}
		if a.rect.X != b.rect.X {
			return cmp.Compare(a.rect.X, b.rect.X)
		}
		return cmp.Compare(a.dfsIdx, b.dfsIdx)
	})

	order := make([]string, len(filtered))
	for i, c := range filtered {
		order[i] = c.id
	}
	return order
}
