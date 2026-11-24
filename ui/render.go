package ui

import (
	"container/list"
	"strings"
	"sync"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/layout"
)

// Frame is one rendered screen: ANSI text (rows joined by "\n") and the real
// terminal cursor in screen coordinates (CursorHidden when none).
type Frame struct {
	ANSI   string
	Cursor buffer.Cursor
}

// RenderStats counts subtree buffer cache outcomes for the last Render call.
type RenderStats struct {
	CacheHits   int
	CacheMisses int
}

// DefaultCacheEntries bounds a Renderer's subtree cache to keep long-lived
// TUI sessions memory-bounded while preserving subtree reuse.
const DefaultCacheEntries = 96

type cacheKey struct {
	id      string
	key     uint64
	w, h    int
	focused bool
	theme   themeKey
}

// themeKey is the part of a Theme the caches depend on.
type themeKey struct {
	name       string
	version    uint64
	density    string
	border     string
	cardBorder string
}

func keyOf(th Theme) themeKey {
	return themeKey{th.Name, th.Version, th.EffectiveDensity(), th.EffectiveBorderStyle(), th.EffectiveCardBorderStyle()}
}

type cacheEntry struct {
	key cacheKey
	buf *buffer.Buffer
}

// Renderer renders ViewNode trees and keeps an LRU cache of subtree buffers.
// A node with a non-empty ID and a non-zero CacheKey is re-rendered only when
// its (ID, CacheKey, size, focus state, theme name, version, density or
// border style) changes. Subtrees containing
// scroll or overlay nodes are never cached. A Renderer is safe for use from
// one goroutine at a time; Render serializes callers.
type Renderer struct {
	// Stats holds the cache counters of the most recent Render call.
	Stats RenderStats

	mu    sync.Mutex
	max   int
	lru   *list.List // of *cacheEntry, front = most recent
	items map[cacheKey]*list.Element
	st    *styler
	stKey themeKey
}

// NewRenderer returns a Renderer caching at most maxEntries subtree buffers
// (DefaultCacheEntries when maxEntries <= 0).
func NewRenderer(maxEntries int) *Renderer {
	if maxEntries <= 0 {
		maxEntries = DefaultCacheEntries
	}
	return &Renderer{max: maxEntries, lru: list.New(), items: map[cacheKey]*list.Element{}}
}

// CacheLen reports how many subtree buffers are cached.
func (r *Renderer) CacheLen() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lru.Len()
}

// Reset drops every cached buffer.
func (r *Renderer) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lru.Init()
	r.items = map[cacheKey]*list.Element{}
}

func (r *Renderer) get(k cacheKey) *buffer.Buffer {
	if el, ok := r.items[k]; ok {
		r.lru.MoveToFront(el)
		return el.Value.(*cacheEntry).buf
	}
	return nil
}

func (r *Renderer) put(k cacheKey, b *buffer.Buffer) {
	if el, ok := r.items[k]; ok {
		el.Value.(*cacheEntry).buf = b
		r.lru.MoveToFront(el)
		return
	}
	r.items[k] = r.lru.PushFront(&cacheEntry{key: k, buf: b})
	for r.lru.Len() > r.max {
		last := r.lru.Back()
		delete(r.items, last.Value.(*cacheEntry).key)
		r.lru.Remove(last)
	}
}

// Render is the package-level convenience: a fresh Renderer with no cache reuse.
func Render(root ViewNode, theme Theme, focusID string, w, h int) (Frame, layout.Solved) {
	return NewRenderer(1).Render(root, theme, focusID, w, h)
}

// Measure solves the layout geometry of root inside w x h cells headlessly
// (including nested KindScroll ChildRects/ScrollInfo and FocusOrder) without
// needing a terminal.
func Measure(root ViewNode, theme Theme, focusID string, w, h int) layout.Solved {
	_, solved := Render(root, theme, focusID, w, h)
	return solved
}

// Render lays root out in w x h cells, draws it, and returns the frame plus the
// solved geometry. The geometry is keyed by real node IDs only; for scroll
// viewports it includes the visible part of their descendants. Rendering is
// pure with respect to root and focusID.
func (r *Renderer) Render(root ViewNode, theme Theme, focusID string, w, h int) (Frame, layout.Solved) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Stats = RenderStats{}
	w, h = max(w, 0), max(h, 0)

	key := keyOf(theme)
	if r.st == nil || r.stKey != key {
		r.st, r.stKey = newStyler(theme), key
	}

	eff := effectiveRoot(root)
	b := newBuilder(&eff, focusID)
	b.dens, b.border, b.cardBorder = key.density, key.border, key.cardBorder
	box, rn := b.build(&eff, "~", w, h)
	solved := layout.Solve(box, w, h)

	buf := buffer.New(w, h)
	c := &ctx{r: r, st: r.st, b: b, solved: solved, buf: buf}
	c.draw(rn)
	c.flushOverlays()

	solved = finalizeSolved(solved, c.merges, b.userFocus)
	return Frame{ANSI: paintCanvas(buf.String(), r.st.canvas()), Cursor: buf.Cursor}, solved
}

// canvas returns the SGR prefix of the theme's "canvas" background, or "".
func (s *styler) canvas() string {
	if _, ok := s.th.Tones["canvas"]; !ok {
		return ""
	}
	ts := s.tone("canvas")
	if ts.Bg == "" {
		return ""
	}
	return s.sgr(ToneStyle{Fg: ts.Fg, Bg: ts.Bg})
}

// paintCanvas lays the canvas style under every row: it opens each row and
// follows every reset, so cells with their own colors keep them and all
// other cells take the canvas colors. Rows still end in a reset.
func paintCanvas(ansi, canvas string) string {
	if canvas == "" {
		return ansi
	}
	rows := strings.Split(ansi, "\n")
	for i, row := range rows {
		rows[i] = canvas + strings.ReplaceAll(row, reset, reset+canvas) + reset
	}
	return strings.Join(rows, "\n")
}

// ctx is one drawing scope: a solved layout plus the buffer it paints into.
// Solved coordinates map to buffer coordinates by subtracting (ox, oy).
type ctx struct {
	r      *Renderer
	st     *styler
	b      *builder
	solved layout.Solved
	buf    *buffer.Buffer
	ox, oy int

	overlays []*rnode
	merges   []mergeRec
}

func (c *ctx) local(r layout.Rect) layout.Rect {
	return layout.Rect{X: r.X - c.ox, Y: r.Y - c.oy, W: r.W, H: r.H}
}

func (c *ctx) visible(lr layout.Rect) bool {
	return lr.W > 0 && lr.H > 0 && lr.X < c.buf.Width() && lr.Y < c.buf.Height() && lr.X+lr.W > 0 && lr.Y+lr.H > 0
}

func (c *ctx) rect(id string) (layout.Rect, bool) {
	r, ok := c.solved.Rects[id]
	if !ok {
		return layout.Rect{}, false
	}
	return c.local(r), true
}

// draw paints rn, deferring overlays and consulting the subtree cache.
func (c *ctx) draw(rn *rnode) {
	if isOverlay(rn.n) {
		c.overlays = append(c.overlays, rn)
		return
	}
	lr, ok := c.rect(rn.id)
	if !ok || !c.visible(lr) {
		return
	}
	n := rn.n
	if n.ID != "" && n.CacheKey != 0 && !hasVolatile(n) {
		k := cacheKey{n.ID, n.CacheKey, lr.W, lr.H, rn.onFocus, c.r.stKey}
		if cached := c.r.get(k); cached != nil {
			c.r.Stats.CacheHits++
			c.buf.Blit(cached, lr)
			return
		}
		c.r.Stats.CacheMisses++
		sub := &ctx{r: c.r, st: c.st, b: c.b, solved: c.solved, buf: buffer.New(lr.W, lr.H), ox: c.ox + lr.X, oy: c.oy + lr.Y}
		sub.drawBody(rn)
		c.r.put(k, sub.buf)
		c.buf.Blit(sub.buf, lr)
		return
	}
	c.drawBody(rn)
}

func (c *ctx) flushOverlays() {
	for i := 0; i < len(c.overlays); i++ { // overlays may enqueue further overlays
		c.drawBody(c.overlays[i])
	}
	c.overlays = nil
}

// hasVolatile reports whether the subtree holds scroll or overlay nodes, whose
// geometry escapes the cached rectangle.
func hasVolatile(n *ViewNode) bool {
	if n.Kind == KindScroll || n.Kind == KindOverlay {
		return true
	}
	for i := range n.Children {
		if hasVolatile(&n.Children[i]) {
			return true
		}
	}
	return false
}

// blitLines paints ANSI lines into the local rect r, with an optional cursor
// given relative to r.
func (c *ctx) blitLines(r layout.Rect, lines []string, cur *buffer.Cursor) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	tmp := buffer.New(r.W, r.H)
	for y, ln := range lines {
		if y >= r.H {
			break
		}
		tmp.SetLine(y, ln)
	}
	if cur != nil {
		tmp.Cursor = *cur
	}
	c.buf.Blit(tmp, r)
}

// --- scroll merging and Solved finalization -----------------------------------

// mergeRec carries the geometry of a scroll viewport's descendants.
type mergeRec struct {
	owner   string
	rects   map[string]layout.Rect
	inner   map[string]layout.Rect
	focus   []string
	scrolls map[string]layout.ScrollInfo
}

func clipRect(r, clip layout.Rect) (layout.Rect, bool) {
	x0, y0 := max(r.X, clip.X), max(r.Y, clip.Y)
	x1, y1 := min(r.X+r.W, clip.X+clip.W), min(r.Y+r.H, clip.Y+clip.H)
	if x1 <= x0 || y1 <= y0 {
		return layout.Rect{}, false
	}
	return layout.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, true
}

// foldNested applies nested scroll geometry onto rects/inner and returns the
// focus order with each nested viewport replaced by its descendants.
func foldNested(rects, inner map[string]layout.Rect, order []string, nested []mergeRec, userFocus map[string]bool) []string {
	for _, m := range nested {
		for id, r := range m.rects {
			rects[id] = r
		}
		for id, r := range m.inner {
			inner[id] = r
		}
		var next []string
		for _, id := range order {
			if id != m.owner {
				next = append(next, id)
				continue
			}
			if userFocus[id] {
				next = append(next, id)
			}
			next = append(next, m.focus...)
		}
		order = next
	}
	return order
}

func finalizeSolved(s layout.Solved, merges []mergeRec, userFocus map[string]bool) layout.Solved {
	s.FocusOrder = foldNested(s.Rects, s.InnerRects, s.FocusOrder, merges, userFocus)
	for _, m := range merges {
		for id, info := range m.scrolls {
			if IsSyntheticID(id) {
				continue
			}
			if s.Scrolls == nil {
				s.Scrolls = map[string]layout.ScrollInfo{}
			}
			s.Scrolls[id] = info
		}
	}
	for id := range s.Rects {
		if IsSyntheticID(id) {
			delete(s.Rects, id)
		}
	}
	for id := range s.InnerRects {
		if IsSyntheticID(id) {
			delete(s.InnerRects, id)
		}
	}
	var order []string
	for _, id := range s.FocusOrder {
		if !IsSyntheticID(id) {
			order = append(order, id)
		}
	}
	s.FocusOrder = order
	return s
}
