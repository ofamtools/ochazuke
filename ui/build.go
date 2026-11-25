package ui

import (
	"strconv"
	"strings"

	"ofam.dev/ochazuke/layout"
)

// rnode is a resolved ViewNode: it carries the layout ID the solver knows the
// node by, the IDs of its synthetic parts, and its focus state.
type rnode struct {
	n    *ViewNode
	id   string
	kids []*rnode

	head, foot, strip string // card header/footer and tab strip leaf IDs
	path              string

	isFocus bool // this node is the focus target
	onFocus bool // this node is the focus target or contains it
}

type builder struct {
	dens       string // effective theme density; sizes forms and cards
	border     string // effective theme border style; glyphs only
	cardBorder string // effective theme card border style; glyphs only
	focusID    string
	focusSet   map[string]bool // IDs of the focus target and its ancestors
	userFocus  map[string]bool // scroll IDs the user marked focusable
	hasScrolls bool
}

func newBuilder(root *ViewNode, focusID string) *builder {
	b := &builder{dens: DensityBalanced, border: BorderRounded, focusID: focusID, focusSet: map[string]bool{}, userFocus: map[string]bool{}}
	if focusID != "" {
		b.markFocus(root, "~", focusID)
	}
	return b
}

func (b *builder) markFocus(n *ViewNode, path, id string) bool {
	hit := n.ID == id
	for i := range n.Children {
		if b.markFocus(&n.Children[i], path+"/"+strconv.Itoa(i), id) {
			hit = true
		}
	}
	if hit {
		b.focusSet[nodeID(n, path)] = true
	}
	return hit
}

// chromeOf returns a node's border flag and padding. Chrome kinds default to
// one column of side padding; spacious cards also get a padded top, bottom
// and a wider gutter. Caller-set Padding always wins.
func chromeOf(n *ViewNode, d string) (bool, [4]int) {
	switch n.Kind {
	case KindPanel, KindCard, KindOverlay:
		pad := n.Padding
		if pad == ([4]int{}) {
			pad = [4]int{0, 1, 0, 1}
			if n.Kind == KindCard && d == DensitySpacious {
				pad = [4]int{1, 2, 1, 2}
			}
		}
		return true, pad
	}
	return n.Border, n.Padding
}

func innerSize(border bool, pad [4]int, w, h int) (int, int) {
	bw := 0
	if border {
		bw = 1
	}
	return max(0, w-2*bw-pad[1]-pad[3]), max(0, h-2*bw-pad[0]-pad[2])
}

func chromeV(border bool, pad [4]int) int {
	v := pad[0] + pad[2]
	if border {
		v += 2
	}
	return v
}

func isOverlay(n *ViewNode) bool { return n.Kind == KindOverlay }

func (b *builder) base(n *ViewNode, path string) (layout.Box, *rnode) {
	id := nodeID(n, path)
	rn := &rnode{n: n, id: id, path: path}
	rn.isFocus = b.focusID != "" && n.ID == b.focusID
	rn.onFocus = b.focusSet[id]
	border, pad := chromeOf(n, b.dens)
	box := layout.Box{ID: id, Focusable: n.Focusable, Padding: pad, Gap: max(0, n.Gap), Border: border, Constraints: n.Box}
	return box, rn
}

// build converts a ViewNode into a layout.Box plus its rnode. w and h estimate
// the rect the node will receive; they only size content-dependent children.
func (b *builder) build(n *ViewNode, path string, w, h int) (layout.Box, *rnode) {
	box, rn := b.base(n, path)
	iw, ih := innerSize(box.Border, box.Padding, w, h)
	switch n.Kind {
	case KindVStack, KindHStack:
		box.Kind = layout.BoxStack
		if n.Kind == KindHStack {
			box.Axis = layout.Horizontal
		}
		b.flow(&box, rn, n.Children, box.Axis, iw, ih, false)
	case KindPanel:
		box.Kind = layout.BoxStack
		b.flow(&box, rn, n.Children, layout.Vertical, iw, ih, false)
	case KindCard:
		box.Kind = layout.BoxStack
		head := cardHead(nil, n, iw, false, b.dens)
		if len(head) > 0 {
			rn.head = rn.path + "#head"
			box.Children = append(box.Children, layout.Box{ID: rn.head, Constraints: layout.Constraints{FixedH: len(head)}})
		}
		b.flow(&box, rn, n.Children, layout.Vertical, iw, ih, true)
		if len(n.Actions) > 0 {
			rn.foot = rn.path + "#foot"
			box.Children = append(box.Children, layout.Box{ID: rn.foot, Constraints: layout.Constraints{FixedH: 1}})
		}
	case KindSplit:
		box.Kind = layout.BoxSplit
		box.Ratio = n.Ratio
		box.Axis = splitAxis(n)
		var ws, hs [2]int
		ws, hs = [2]int{iw, iw}, [2]int{ih, ih}
		ratio := n.Ratio
		if ratio <= 0 || ratio > 1 {
			ratio = 0.5
		}
		if box.Axis == layout.Horizontal {
			w0 := int(float64(max(0, iw-box.Gap)) * ratio)
			ws = [2]int{w0, max(0, iw-box.Gap-w0)}
		} else {
			h0 := int(float64(max(0, ih-box.Gap)) * ratio)
			hs = [2]int{h0, max(0, ih-box.Gap-h0)}
		}
		for i := range n.Children {
			k := min(i, 1)
			cb, crn := b.build(&n.Children[i], path+"/"+strconv.Itoa(i), ws[k], hs[k])
			box.Children = append(box.Children, cb)
			rn.kids = append(rn.kids, crn)
		}
	case KindGrid:
		box.Kind = layout.BoxGrid
		box.Cols = n.Cols
		box.MinColW = gridMinColW(n)
		cols := gridCols(n, iw)
		colW := max(0, (iw-box.Gap*(cols-1))/cols)
		var flowIdx []int // indexes of auto-sized flow children
		posOf, pos := map[int]int{}, 0
		for i := range n.Children {
			k := &n.Children[i]
			cb, crn := b.build(k, path+"/"+strconv.Itoa(i), colW, ih)
			if !isOverlay(k) {
				posOf[i] = pos
				pos++
				had := cb.Constraints.FixedH > 0
				fixHeight(&cb, k, colW, false, b.dens)
				if !had && cb.Constraints.FixedH > 0 {
					flowIdx = append(flowIdx, i)
				}
			}
			box.Children = append(box.Children, cb)
			rn.kids = append(rn.kids, crn)
		}
		// Cards in one grid row share the row's tallest height.
		rowOf := func(i int) int { return i / cols }
		rowMax := map[int]int{}
		for _, i := range flowIdx {
			rowMax[rowOf(posOf[i])] = max(rowMax[rowOf(posOf[i])], box.Children[i].Constraints.FixedH)
		}
		for _, i := range flowIdx {
			box.Children[i].Constraints.FixedH = rowMax[rowOf(posOf[i])]
		}
	case KindTabs:
		box.Kind = layout.BoxStack
		rn.strip = rn.path + "#strip"
		box.Children = append(box.Children, layout.Box{ID: rn.strip, Constraints: layout.Constraints{FixedH: 2}})
		if a := activeTab(n); a >= 0 {
			k := &n.Children[a]
			cb, crn := b.build(k, path+"/"+strconv.Itoa(a), iw, max(0, ih-2))
			fixHeight(&cb, k, iw, false, b.dens)
			box.Children = append(box.Children, cb)
			rn.kids = append(rn.kids, crn)
		}
	case KindScroll:
		box.Kind = layout.BoxLeaf
		box.Focusable = true // lets Solve place the viewport in FocusOrder; finalize swaps in its children
		b.userFocus[rn.id] = n.Focusable
		b.hasScrolls = true
	case KindOverlay:
		box.Kind = layout.BoxOverlay
		box.AnchorID = n.propString("anchorId")
		ow, oh := overlaySize(n, w, h, b.dens)
		if box.Constraints.FixedW == 0 {
			box.Constraints.FixedW = ow
		}
		if box.Constraints.FixedH == 0 {
			box.Constraints.FixedH = oh
		}
		biw, bih := innerSize(true, box.Padding, ow, oh)
		bodyID := rn.path + "#body"
		body := layout.Box{ID: bodyID, Kind: layout.BoxStack}
		brn := &rnode{n: &ViewNode{Kind: KindVStack}, id: bodyID, path: rn.path, onFocus: rn.onFocus}
		b.flow(&body, brn, n.Children, layout.Vertical, biw, bih, false)
		box.Children = []layout.Box{body}
		rn.kids = []*rnode{brn}
	default:
		box.Kind = layout.BoxLeaf
		if len(n.Children) > 0 && !isLeafKind(n.Kind) { // unknown container: stack its children
			box.Kind = layout.BoxStack
			b.flow(&box, rn, n.Children, layout.Vertical, iw, ih, false)
		}
	}
	return box, rn
}

func isLeafKind(k string) bool {
	switch k {
	case KindText, KindMarkdown, KindTable, KindTree, KindInput, KindForm, KindPicker, KindTerminal, KindRaw:
		return true
	}
	return false
}

// flow builds kids as a stack along axis into box and rn. With fixAll, every
// non-flex child gets its measured height (used where height is unbounded).
func (b *builder) flow(box *layout.Box, rn *rnode, kids []ViewNode, axis layout.Axis, iw, ih int, fixAll bool) {
	widths := stackWidths(kids, iw, box.Gap)
	for i := range kids {
		k := &kids[i]
		cw, ch := iw, ih
		if axis == layout.Horizontal {
			cw = widths[i]
		}
		cb, crn := b.build(k, rn.path+"/"+strconv.Itoa(i), cw, ch)
		if axis == layout.Vertical && !isOverlay(k) {
			fixHeight(&cb, k, cw, fixAll, b.dens)
		}
		box.Children = append(box.Children, cb)
		rn.kids = append(rn.kids, crn)
	}
}

// fixHeight gives a child its measured height unless it fills or sets its own.
func fixHeight(cb *layout.Box, k *ViewNode, w int, force bool, d string) {
	if cb.Constraints.FixedH > 0 || k.Box.Flex > 0 {
		return
	}
	if force || !fills(k) {
		cb.Constraints.FixedH = max(1, measure(k, w, d))
	}
}

// stackWidths estimates each flow child's width in a horizontal stack.
func stackWidths(kids []ViewNode, iw, gap int) []int {
	out := make([]int, len(kids))
	n, fixed, weights := 0, 0, 0
	for i := range kids {
		if isOverlay(&kids[i]) {
			continue
		}
		n++
		if f := kids[i].Box.FixedW; f > 0 {
			fixed += f
		} else {
			weights += max(1, kids[i].Box.Flex)
		}
	}
	free := max(0, iw-gap*max(0, n-1)-fixed)
	for i := range kids {
		switch {
		case isOverlay(&kids[i]):
			out[i] = iw
		case kids[i].Box.FixedW > 0:
			out[i] = kids[i].Box.FixedW
		default:
			out[i] = free * max(1, kids[i].Box.Flex) / max(1, weights)
		}
	}
	return out
}

func splitAxis(n *ViewNode) layout.Axis {
	switch strings.ToLower(n.propString("axis")) {
	case "vertical", "v", "stacked", "rows":
		return layout.Vertical
	}
	return layout.Horizontal // panes side by side
}

func gridMinColW(n *ViewNode) int {
	if n.MinColW > 0 {
		return n.MinColW
	}
	return 28
}

func gridCols(n *ViewNode, iw int) int {
	if n.Cols > 0 {
		return n.Cols
	}
	gap := max(0, n.Gap)
	return max(1, (iw+gap)/(gridMinColW(n)+gap))
}

func overlaySize(n *ViewNode, w, h int, d string) (int, int) {
	wp, ok := n.propInt("widthPct")
	if !ok || wp <= 0 {
		wp = 60
	}
	ow := max(min(w, 24), w*min(wp, 100)/100)
	if n.Box.FixedW > 0 {
		ow = n.Box.FixedW
	}
	border, pad := chromeOf(n, d)
	var oh int
	if hp, ok := n.propInt("heightPct"); ok && hp > 0 {
		oh = h * min(hp, 100) / 100
	} else {
		iw, _ := innerSize(border, pad, ow, 0)
		oh = flowHeight(n.Children, iw, 0, d) + chromeV(border, pad)
	}
	if n.Box.FixedH > 0 {
		oh = n.Box.FixedH
	}
	return min(ow, max(w, 1)), min(max(oh, 3), max(h, 1))
}

// activeTab returns the index of the active tab child, or -1 when there are none.
func activeTab(n *ViewNode) int {
	if len(n.Children) == 0 {
		return -1
	}
	v, ok := n.prop("active")
	if !ok {
		return 0
	}
	if s, isStr := v.(string); isStr {
		if i, err := strconv.Atoi(s); err == nil {
			return min(max(i, 0), len(n.Children)-1)
		}
		for i := range n.Children {
			if n.Children[i].ID == s || n.Children[i].Title == s {
				return i
			}
		}
		return 0
	}
	if i, ok := anyInt(v); ok {
		return min(max(i, 0), len(n.Children)-1)
	}
	return 0
}

// fills reports whether a node expands to the space it is given rather than
// taking its content height.
func fills(n *ViewNode) bool {
	if n.Box.Flex > 0 {
		return true
	}
	switch n.Kind {
	case KindSplit, KindScroll, KindTerminal, KindTable, KindTree, KindPicker:
		return true
	case KindVStack, KindHStack, KindGrid, KindPanel:
		for i := range n.Children {
			if !isOverlay(&n.Children[i]) && fills(&n.Children[i]) {
				return true
			}
		}
	case KindTabs:
		if a := activeTab(n); a >= 0 {
			return fills(&n.Children[a])
		}
	}
	return false
}

// flowHeight sums the measured heights of non-overlay kids plus gaps.
func flowHeight(kids []ViewNode, w, gap int, d string) int {
	total, cnt := 0, 0
	for i := range kids {
		if isOverlay(&kids[i]) {
			continue
		}
		total += max(1, measure(&kids[i], w, d))
		cnt++
	}
	return total + max(0, gap)*max(0, cnt-1)
}

// measure returns a node's preferred height at width w and density d,
// honouring Box limits.
func measure(n *ViewNode, w int, d string) int {
	if n.Box.FixedH > 0 {
		return n.Box.FixedH
	}
	h := natural(n, w, d)
	if n.Box.MinH > 0 {
		h = max(h, n.Box.MinH)
	}
	if n.Box.MaxH > 0 {
		h = min(h, n.Box.MaxH)
	}
	return h
}

func natural(n *ViewNode, w int, d string) int {
	border, pad := chromeOf(n, d)
	iw, _ := innerSize(border, pad, w, 0)
	cv := chromeV(border, pad)
	switch n.Kind {
	case KindVStack, KindPanel:
		return flowHeight(n.Children, iw, n.Gap, d) + cv
	case KindCard:
		h := flowHeight(n.Children, iw, n.Gap, d) + cv + len(cardHead(nil, n, iw, false, d))
		if len(n.Actions) > 0 {
			h++
		}
		return h
	case KindHStack:
		widths := stackWidths(n.Children, iw, n.Gap)
		h := 0
		for i := range n.Children {
			if !isOverlay(&n.Children[i]) {
				h = max(h, measure(&n.Children[i], widths[i], d))
			}
		}
		return h + cv
	case KindSplit:
		if len(n.Children) == 0 {
			return cv
		}
		if splitAxis(n) == layout.Vertical {
			return flowHeight(n.Children, iw, n.Gap, d) + cv
		}
		h := 0
		for i := range n.Children {
			h = max(h, measure(&n.Children[i], iw/2, d))
		}
		return h + cv
	case KindGrid:
		cols := gridCols(n, iw)
		gap := max(0, n.Gap)
		colW := max(0, (iw-gap*(cols-1))/cols)
		var flow []*ViewNode
		for i := range n.Children {
			if !isOverlay(&n.Children[i]) {
				flow = append(flow, &n.Children[i])
			}
		}
		total, rows := 0, 0
		for i := 0; i < len(flow); i += cols {
			rh := 0
			for _, k := range flow[i:min(len(flow), i+cols)] {
				rh = max(rh, measure(k, colW, d))
			}
			total += rh
			rows++
		}
		return total + gap*max(0, rows-1) + cv
	case KindTabs:
		a := activeTab(n)
		if a < 0 {
			return 2 + cv
		}
		return 2 + measure(&n.Children[a], iw, d) + cv
	case KindScroll:
		return flowHeight(n.Children, w, n.Gap, d)
	case KindOverlay:
		return 0
	case KindText:
		return len(wrapRanges([]rune(textNodePlain(n)), iw)) + cv
	case KindMarkdown:
		return len(mdLines(nil, n.propString("markdown"), iw)) + cv
	case KindTable:
		return 2 + len(toSlice(n.Props["rows"])) + cv
	case KindTree:
		return len(treeRows(n)) + cv
	case KindInput:
		if n.propBool("multiline") {
			if n.propBoolDefault("wrap", true) {
				avail := max(1, iw-2)
				return max(3, len(wrapRanges([]rune(n.propString("value")), avail))) + cv
			}
			return max(3, strings.Count(n.propString("value"), "\n")+1) + cv
		}
		return 1 + cv
	case KindForm:
		return formHeight(n, iw, d) + cv
	case KindPicker:
		return 1 + len(toSlice(n.Props["items"])) + cv
	case KindTerminal:
		return max(3, len(ansiLines(n))) + cv
	case KindRaw:
		return max(1, len(ansiLines(n))) + cv
	}
	if len(n.Children) > 0 {
		return flowHeight(n.Children, iw, n.Gap, d) + cv
	}
	return 1 + cv
}
