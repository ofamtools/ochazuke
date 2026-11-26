package ui

import (
	"ofam.dev/ochazuke/layout"
	"ofam.dev/ochazuke/traits"
)

// Scroll follow: how KindScroll containers and scrollable leaf nodes
// (wrapped/multi-line KindTree, and KindTree/KindTable without an explicit
// "offset" prop) pick their drawn offset.
//
// The Renderer remembers the last drawn offset of each scroll viewport with
// a real (non-synthetic) ID. That memory is render-local view state, the same
// category as the subtree buffer cache; it never reads an app store. On each
// frame a KindScroll container picks its offset in this order:
//
//  1. Stateless follow (Renderer.StatelessScrollFollow, the node prop
//     "followStateless", or a container without a real ID): the original
//     computation, unchanged: the "offset" prop, else the focused descendant
//     pinned flush to the bottom edge from 0, then "followTail".
//  2. "followTail": the content end.
//  3. An explicit "offset" prop.
//  4. A focused descendant in the container (directly, or the visible part of
//     it inside a nested scroll container): traits.ScrollReveal from the
//     remembered offset with the scroll-off padding, so the view moves only
//     when the focused unit leaves the padded window.
//  5. Otherwise the remembered offset (0 when none).
//
// Wrapped/multi-line KindTree nodes use the same remembered visual-line offset,
// scroll-off padding, scrollKey reset, and followStateless opt-out via
// revealScroll (seeding from props.offset only before a memo exists, because
// tree.Model's offset is in logical rows rather than wrapped screen lines).
//
// The result is clamped to [0, maxOff] and remembered. A "scrollKey" prop that
// differs from the remembered one drops the remembered offset first.

// maxScrollMemos bounds the remembered offsets; the least recently drawn
// containers are forgotten first.
const maxScrollMemos = 256

type scrollMemo struct {
	offset int
	key    string
	frame  uint64
}

// peekScroll returns the remembered offset of id for content key without
// mutating scrollMemos.
func (r *Renderer) peekScroll(id, key string) (int, bool) {
	m, ok := r.scrollMemos[id]
	if !ok || m.key != key {
		return 0, false
	}
	return m.offset, true
}

// touchScroll refreshes id's LRU frame timestamp on a subtree cache hit.
func (r *Renderer) touchScroll(id string) {
	if m, ok := r.scrollMemos[id]; ok {
		m.frame = r.frame
		r.scrollMemos[id] = m
	}
}

// recallScroll returns the remembered offset of id for content key, dropping
// it when the key changed.
func (r *Renderer) recallScroll(id, key string) (int, bool) {
	m, ok := r.scrollMemos[id]
	if !ok {
		return 0, false
	}
	if m.key != key {
		delete(r.scrollMemos, id)
		return 0, false
	}
	return m.offset, true
}

func (r *Renderer) rememberScroll(id, key string, offset int) {
	if r.scrollMemos == nil {
		r.scrollMemos = map[string]scrollMemo{}
	}
	r.scrollMemos[id] = scrollMemo{offset: offset, key: key, frame: r.frame}
	if len(r.scrollMemos) <= maxScrollMemos {
		return
	}
	oldID, oldFrame := "", r.frame
	for k, m := range r.scrollMemos {
		if m.frame < oldFrame || oldID == "" {
			oldID, oldFrame = k, m.frame
		}
	}
	delete(r.scrollMemos, oldID)
}

// ScrollMemoLen reports how many scroll container offsets are remembered.
func (r *Renderer) ScrollMemoLen() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.scrollMemos)
}

// scrollPlan is one scroll container's laid-out content and chosen offset.
// Plans are computed once per frame (memoized on the builder by tree path)
// so an outer container can look at a nested container's offset before the
// nested one is drawn.
type scrollPlan struct {
	vw, vh, total, maxOff int
	bar                   bool
	crn                   *rnode
	sub                   layout.Solved
	offset                int
}

// planScroll lays out rn's content for the inner rect ir and picks its
// offset. It returns nil when the viewport is empty.
func (c *ctx) planScroll(rn *rnode, ir layout.Rect) *scrollPlan {
	if p, ok := c.b.plans[rn.path]; ok {
		return p
	}
	n := rn.n
	vw, vh := ir.W, ir.H
	if vw <= 0 || vh <= 0 {
		return nil
	}
	total := flowHeight(n.Children, vw, n.Gap, c.b.dens)
	bar := total > vh
	if bar {
		vw = max(1, vw-1)
		total = flowHeight(n.Children, vw, n.Gap, c.b.dens)
	}
	total = max(total, 1)

	cid := rn.path + "#content"
	box := layout.Box{ID: cid, Kind: layout.BoxStack, Gap: max(0, n.Gap)}
	crn := &rnode{n: &ViewNode{Kind: KindVStack}, id: cid, path: rn.path, onFocus: rn.onFocus}
	c.b.flow(&box, crn, n.Children, layout.Vertical, vw, total, true)
	p := &scrollPlan{vw: vw, vh: vh, total: total, maxOff: max(0, total-vh), bar: bar, crn: crn, sub: layout.Solve(box, vw, total)}
	if c.b.plans == nil {
		c.b.plans = map[string]*scrollPlan{}
	}
	c.b.plans[rn.path] = p
	p.offset = c.scrollOffset(rn, p)
	return p
}

func (c *ctx) followStateless(rn *rnode) bool {
	return rn.n.propBoolDefault("followStateless", c.r.StatelessScrollFollow) || IsSyntheticID(rn.id)
}

func (c *ctx) scrollPad(n *ViewNode) int {
	if v, ok := n.propInt("scrollOff"); ok {
		return v
	}
	return c.r.ScrollOff
}

// revealScroll advances rn's remembered vertical offset to keep span inside
// the padded viewport [offset+pad, offset+vh-pad], seeding from seed only
// before a memo exists for rn's current scrollKey.
func (c *ctx) revealScroll(rn *rnode, total, vh, seed int, span traits.ItemSpan, hasSpan bool) int {
	maxOff := max(0, total-vh)
	key := rn.n.propString("scrollKey")
	offset, remembered := c.r.recallScroll(rn.id, key)
	if !remembered {
		offset = seed
	}
	offset = min(max(offset, 0), maxOff)
	if hasSpan {
		st := traits.ScrollState{OffsetY: offset, ContentH: total, ViewH: vh}
		offset = traits.ScrollReveal(st, traits.ScrollConfig{ScrollOff: c.scrollPad(rn.n)}, span).OffsetY
	}
	offset = min(max(offset, 0), maxOff)
	c.r.rememberScroll(rn.id, key, offset)
	return offset
}

func (c *ctx) scrollOffset(rn *rnode, p *scrollPlan) int {
	n := rn.n
	offset := 0
	if c.followStateless(rn) {
		if o, ok := n.propInt("offset"); ok {
			offset = o
		} else if fr, ok := p.sub.Rects[c.b.focusID]; ok && c.b.focusID != "" {
			if fr.Y+fr.H > p.vh {
				offset = fr.Y + fr.H - p.vh
			}
			offset = min(offset, fr.Y)
		}
		if n.propBool("followTail") {
			offset = p.maxOff
		}
		offset = min(max(offset, 0), p.maxOff)
		if !IsSyntheticID(rn.id) {
			c.r.rememberScroll(rn.id, n.propString("scrollKey"), offset)
		}
		return offset
	}

	key := n.propString("scrollKey")
	recalled, _ := c.r.recallScroll(rn.id, key)
	offset = min(max(recalled, 0), p.maxOff)
	switch o, ok := n.propInt("offset"); {
	case n.propBool("followTail"):
		offset = p.maxOff
	case ok:
		offset = o
	default:
		if span, ok := c.focusSpan(p); ok {
			st := traits.ScrollState{OffsetY: offset, ContentH: p.total, ViewH: p.vh}
			offset = traits.ScrollReveal(st, traits.ScrollConfig{ScrollOff: c.scrollPad(n)}, span).OffsetY
		}
	}
	offset = min(max(offset, 0), p.maxOff)
	c.r.rememberScroll(rn.id, key, offset)
	return offset
}

// focusSpan returns the focused unit's vertical span in p's content
// coordinates: the focused descendant's rect when it is laid out directly in
// p, or else the visible part of the focus inside the nested scroll container
// that holds it (using that container's own chosen offset).
func (c *ctx) focusSpan(p *scrollPlan) (traits.ItemSpan, bool) {
	if c.b.focusID == "" {
		return traits.ItemSpan{}, false
	}
	if fr, ok := p.sub.Rects[c.b.focusID]; ok {
		return traits.ItemSpan{Y: fr.Y, H: fr.H}, true
	}
	inner := focusedScroll(p.crn)
	if inner == nil {
		return traits.ItemSpan{}, false
	}
	ir, ok := p.sub.InnerRects[inner.id]
	if !ok {
		return traits.ItemSpan{}, false
	}
	ip := c.planScroll(inner, ir)
	if ip == nil {
		return traits.ItemSpan{}, false
	}
	s, ok := c.focusSpan(ip)
	if !ok {
		return traits.ItemSpan{}, false
	}
	y0 := max(s.Y-ip.offset, 0)
	y1 := min(s.Y+max(1, s.H)-ip.offset, ip.vh)
	if y1 <= y0 {
		return traits.ItemSpan{}, false
	}
	return traits.ItemSpan{Y: ir.Y + y0, H: y1 - y0}, true
}

// focusedScroll finds the nearest scroll container below rn that holds the
// focus target.
func focusedScroll(rn *rnode) *rnode {
	for _, k := range rn.kids {
		if !k.onFocus {
			continue
		}
		if k.n.Kind == KindScroll {
			return k
		}
		if f := focusedScroll(k); f != nil {
			return f
		}
	}
	return nil
}
