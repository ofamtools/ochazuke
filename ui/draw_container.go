package ui

import (
	"slices"
	"strings"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/layout"
)

// drawBody paints one node (never deferring or caching).
func (c *ctx) drawBody(rn *rnode) {
	lr, ok := c.rect(rn.id)
	if !ok || !c.visible(lr) {
		return
	}
	switch n := rn.n; n.Kind {
	case KindCard:
		c.drawCard(rn, lr)
	case KindPanel:
		c.drawPanel(rn, lr)
	case KindOverlay:
		c.drawOverlay(rn, lr)
	case KindTabs:
		c.drawTabs(rn, lr)
	case KindScroll:
		c.drawScroll(rn, lr)
	default:
		if n.Border && !isLeafKind(n.Kind) {
			c.drawFrame(lr, frameOpts{focus: rn.onFocus, border: c.borderStyle(rn), title: n.Title, titleStyle: c.titleStyle(rn), right: c.badgeRow(n.Badges)})
		}
		if isStackKind(n.Kind) || len(rn.kids) > 0 {
			c.drawKids(rn)
			return
		}
		c.drawLeaf(rn, lr)
	}
}

func isStackKind(k string) bool {
	switch k {
	case KindVStack, KindHStack, KindSplit, KindGrid:
		return true
	}
	return false
}

func (c *ctx) drawKids(rn *rnode) {
	for _, k := range rn.kids {
		c.draw(k)
	}
}

// borderStyle picks the border tone: focus.border when focused (or node's Tone
// bolded), unread when unread, or unfocused.border when unfocused.
func (c *ctx) borderStyle(rn *rnode) ToneStyle {
	n := rn.n
	name := n.Tone
	if name == "" && n.Unread {
		name = "unread"
	}
	if rn.onFocus {
		if name == "" {
			if ts := c.st.tone("focus.border"); ts != (ToneStyle{}) {
				return ts
			}
			return c.st.tone("border.focus")
		}
		return mergeTone(c.st.tone(name), ToneStyle{Bold: true})
	}
	if name == "" {
		if ts := c.st.tone("unfocused.border"); ts != (ToneStyle{}) {
			return ts
		}
		return c.st.tone("border")
	}
	return c.st.tone(name)
}

func (c *ctx) badgeRow(bs []Badge) string {
	parts := make([]string, len(bs))
	for i, b := range bs {
		parts[i] = c.st.badgeText(b)
	}
	return strings.Join(parts, " ")
}

type frameOpts struct {
	style      string // optional border/card style override; empty uses c.b.border
	focus      bool   // the frame holds the focus: heavy in rounded/sharp, ┃ on the left in minimal/subtle
	border     ToneStyle
	title      string
	titleStyle ToneStyle
	right      string // pre-styled text placed on the right of the top border
}

// frameGlyphs is one border style's glyph set. left replaces vt on the left
// edge of the interior rows.
type frameGlyphs struct{ tl, tr, bl, br, hz, vt, left string }

func glyphsFor(style string, focus bool) frameGlyphs {
	switch style {
	case CardBorderSubtle:
		if focus {
			return frameGlyphs{" ", " ", " ", " ", " ", " ", "┃"}
		}
		return frameGlyphs{" ", " ", " ", " ", " ", " ", "│"}
	case BorderMinimal:
		g := frameGlyphs{"─", "─", "─", "─", "─", " ", " "}
		if focus {
			g.left = "┃"
		}
		return g
	case BorderNone:
		return frameGlyphs{" ", " ", " ", " ", " ", " ", " "}
	}
	if focus {
		return frameGlyphs{"┏", "┓", "┗", "┛", "━", "┃", "┃"}
	}
	if style == BorderSharp {
		return frameGlyphs{"┌", "┐", "└", "┘", "─", "│", "│"}
	}
	return frameGlyphs{"╭", "╮", "╰", "╯", "─", "│", "│"}
}

// drawFrame paints a border in the theme's border style with an optional
// title and right text embedded in the top edge. Every style occupies the
// same one-cell ring, and the title starts at column 3 in all of them. The
// interior is blanked.
func (c *ctx) drawFrame(lr layout.Rect, o frameOpts) {
	w, h := lr.W, lr.H
	if w < 2 || h < 2 {
		return
	}
	style := o.style
	if style == "" {
		style = c.b.border
	}
	g := glyphsFor(style, o.focus)
	bs := func(s string) string { return c.st.paint(s, o.border) }
	span := w - 2
	left, leftW := "", 0
	if o.title != "" && span >= 5 {
		t := truncate(o.title, span-4)
		if o.focus {
			left = bs(g.hz) + c.st.paint(" "+t+" ", o.titleStyle)
		} else {
			left = bs(g.hz+" ") + c.st.paint(t, o.titleStyle) + bs(" ")
		}
		leftW = strW(t) + 3
	}
	right, rightW := "", 0
	if o.right != "" && span-leftW >= strW(o.right)+3 {
		right = bs(" ") + o.right + bs(" "+g.hz)
		rightW = strW(o.right) + 3
	}
	out := make([]string, h)
	out[0] = bs(g.tl) + left + bs(strings.Repeat(g.hz, span-leftW-rightW)) + right + bs(g.tr)
	mid := bs(g.left) + spaces(span) + bs(g.vt)
	for y := 1; y < h-1; y++ {
		out[y] = mid
	}
	out[h-1] = bs(g.bl + strings.Repeat(g.hz, span) + g.br)
	c.blitLines(lr, out, nil)
}

func (c *ctx) drawPanel(rn *rnode, lr layout.Rect) {
	n := rn.n
	c.drawFrame(lr, frameOpts{
		focus:      rn.onFocus,
		border:     c.borderStyle(rn),
		title:      n.Title,
		titleStyle: c.titleStyle(rn),
		right:      c.badgeRow(n.Badges),
	})
	c.drawKids(rn)
}

func focusTitleTone(st *styler) ToneStyle {
	if ts := st.tone("focus.title"); ts != (ToneStyle{}) {
		return ts
	}
	return mergeTone(st.tone("title"), ToneStyle{Fg: st.tone("accent").Fg})
}

func (c *ctx) titleStyle(rn *rnode) ToneStyle {
	if rn.onFocus {
		ts := focusTitleTone(c.st)
		if rn.n.Tone != "" {
			if fg := c.st.tone(rn.n.Tone).Fg; fg != "" {
				ts.Fg = fg
			}
		}
		return ts
	}
	ts := c.st.tone("unfocused.fg")
	if ts == (ToneStyle{}) {
		ts = c.st.tone("title")
	}
	if rn.n.Tone != "" {
		ts = mergeTone(c.st.tone("title"), ToneStyle{Fg: c.st.tone(rn.n.Tone).Fg})
	}
	return ts
}

func (c *ctx) drawOverlay(rn *rnode, lr layout.Rect) {
	n := rn.n
	border := c.st.tone("accent")
	if n.Tone != "" {
		border = c.st.tone(n.Tone)
	}
	c.drawFrame(lr, frameOpts{focus: rn.onFocus, border: border, title: n.Title, titleStyle: c.titleStyle(rn), right: c.badgeRow(n.Badges)})
	c.drawKids(rn)
}

func (c *ctx) cardBorderStyle(n *ViewNode) string {
	switch s := n.propString("cardBorder"); s {
	case CardBorderSubtle, CardBorderMinimal, CardBorderRounded, CardBorderNone:
		return s
	}
	return c.b.cardBorder
}

func (c *ctx) drawCard(rn *rnode, lr layout.Rect) {
	n := rn.n
	c.drawFrame(lr, frameOpts{style: c.cardBorderStyle(n), focus: rn.onFocus, border: c.borderStyle(rn)})
	drawInterior := func(dst *ctx) {
		if rn.head != "" {
			if hr, ok := dst.rect(rn.head); ok {
				dst.blitLines(hr, cardHead(dst.st, n, hr.W, rn.onFocus, dst.b.dens), nil)
			}
		}
		dst.drawKids(rn)
		if rn.foot != "" {
			if fr, ok := dst.rect(rn.foot); ok {
				dst.blitLines(fr, []string{actionsLine(dst.st, n.Actions)}, nil)
			}
		}
	}
	if rn.onFocus && lr.W > 2 && lr.H > 2 {
		if bg := c.st.tone("focus.bg").Bg; bg != "" {
			inner := layout.Rect{X: lr.X + 1, Y: lr.Y + 1, W: lr.W - 2, H: lr.H - 2}
			sub := &ctx{r: c.r, st: c.st, b: c.b, solved: c.solved, buf: buffer.New(inner.W, inner.H), ox: c.ox + inner.X, oy: c.oy + inner.Y}
			drawInterior(sub)
			c.overlays = append(c.overlays, sub.overlays...)
			c.merges = append(c.merges, sub.merges...)
			tinted := paintCanvas(sub.buf.String(), c.st.sgr(ToneStyle{Bg: bg}))
			var cur *buffer.Cursor
			if sub.buf.Cursor.Shape != buffer.CursorHidden {
				cur = &sub.buf.Cursor
			}
			c.blitLines(inner, strings.Split(tinted, "\n"), cur)
			return
		}
	}
	drawInterior(c)
}

// cardHead renders the title (wrapped, with unread/active marker), badge rows,
// summary and meta of a card. st may be nil to count lines without styling. At
// balanced and spacious density a blank spacer separates a non-empty head
// from the card's body or actions; compact heads carry no extra lines. Build,
// measure and draw all call it with the same arguments, so the head's height
// always agrees.
func cardHead(st *styler, n *ViewNode, w int, focused bool, d string) []string {
	var out []string
	tw := max(1, w-2)
	if n.Title != "" {
		marker := "  "
		if n.Unread {
			if focused {
				marker = st.paint("▸", focusTitleTone(st)) + st.t("unread", "●")
			} else {
				marker = st.t("unread", "● ")
			}
		} else if focused {
			marker = st.paint("▸ ", focusTitleTone(st))
		}
		ts := st.tone("title")
		if focused {
			ts = focusTitleTone(st)
		}
		if n.Tone != "" {
			if fg := st.tone(n.Tone).Fg; fg != "" {
				ts.Fg = fg
			}
		}
		for i, ln := range wrapPlain(n.Title, tw) {
			prefix := "  "
			if i == 0 {
				prefix = marker
			}
			out = append(out, prefix+st.paint(ln, ts))
		}
	}
	if len(n.Badges) > 0 {
		row, rowW := "  ", 2
		for _, b := range n.Badges {
			chip := st.badgeText(b)
			if cw := strW(chip); rowW+cw > w && rowW > 2 {
				out = append(out, row)
				row, rowW = "  ", 2
			}
			row += chip + " "
			rowW += strW(chip) + 1
		}
		out = append(out, row)
	}
	if s := n.propString("summary"); s != "" {
		sumTone := "text"
		if focused && st.tone("focus.fg") != (ToneStyle{}) {
			sumTone = "focus.fg"
		}
		for _, ln := range wrapPlain(s, tw) {
			out = append(out, "  "+st.t(sumTone, ln))
		}
	}
	if m := metaText(n); m != "" {
		for _, ln := range wrapPlain(m, tw) {
			out = append(out, "  "+st.t("muted", ln))
		}
	}
	if len(out) > 0 && d != DensityCompact && (len(n.Actions) > 0 || hasFlowKids(n)) {
		out = append(out, "")
	}
	return out
}

func hasFlowKids(n *ViewNode) bool {
	for i := range n.Children {
		if !isOverlay(&n.Children[i]) {
			return true
		}
	}
	return false
}

func metaText(n *ViewNode) string {
	v, ok := n.prop("meta")
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + ": " + anyString(x[k])
		}
		return strings.Join(parts, " · ")
	}
	return strings.Join(toStrings(v), " · ")
}

func actionsLine(st *styler, acts []ActionHint) string {
	parts := make([]string, 0, len(acts))
	for _, a := range acts {
		label := a.Label
		if label == "" {
			label = a.Command
		}
		if a.Key != "" {
			parts = append(parts, st.t("key", a.Key)+" "+st.t("muted", label))
		} else {
			parts = append(parts, st.t("muted", label))
		}
	}
	return "  " + strings.Join(parts, "  ")
}

func (c *ctx) drawTabs(rn *rnode, lr layout.Rect) {
	n := rn.n
	if sr, ok := c.rect(rn.strip); ok {
		active := activeTab(n)
		var row1, row2 strings.Builder
		used := 0
		for i := range n.Children {
			k := &n.Children[i]
			title := k.Title
			if title == "" {
				title = k.ID
			}
			if title == "" {
				title = "tab " + string(rune('1'+i%9))
			}
			label := " " + title + " "
			if k.Unread {
				label = " ● " + title + " "
			}
			for _, b := range k.Badges {
				label += "[" + b.Text + "] "
			}
			lw := strW(label)
			tone, rule, rtone := "muted", "─", "border"
			if i == active {
				tone, rule, rtone = "accent", "━", "accent"
			}
			ts := c.st.tone(tone)
			if i == active {
				ts = mergeTone(ts, ToneStyle{Bold: true})
			}
			if k.Unread && i != active {
				ts = c.st.tone("unread")
			}
			row1.WriteString(c.st.paint(label, ts))
			row2.WriteString(c.st.t(rtone, strings.Repeat(rule, lw)))
			used += lw
			if i < len(n.Children)-1 {
				row1.WriteString(" ")
				row2.WriteString(c.st.t("border", "─"))
				used++
			}
		}
		row2.WriteString(c.st.t("border", strings.Repeat("─", max(0, sr.W-used))))
		c.blitLines(sr, []string{row1.String(), row2.String()}, nil)
	}
	c.drawKids(rn)
}

// drawScroll lays the scroll content out at full height, paints the visible
// window, adds a scrollbar when the content overflows, and records the visible
// descendants' geometry for the returned layout.Solved.
func (c *ctx) drawScroll(rn *rnode, lr layout.Rect) {
	n := rn.n
	if n.Border {
		c.drawFrame(lr, frameOpts{focus: rn.onFocus, border: c.borderStyle(rn), title: n.Title, titleStyle: c.titleStyle(rn), right: c.badgeRow(n.Badges)})
	}
	ir := c.solved.InnerRects[rn.id]
	vw, vh := ir.W, ir.H
	if vw <= 0 || vh <= 0 {
		return
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
	sub := layout.Solve(box, vw, total)

	maxOff := max(0, total-vh)
	offset := 0
	if o, ok := n.propInt("offset"); ok {
		offset = o
	} else if fr, ok := sub.Rects[c.b.focusID]; ok && c.b.focusID != "" {
		if fr.Y+fr.H > vh {
			offset = fr.Y + fr.H - vh
		}
		offset = min(offset, fr.Y)
	}
	if n.propBool("followTail") {
		offset = maxOff
	}
	offset = min(max(offset, 0), maxOff)

	win := &ctx{r: c.r, st: c.st, b: c.b, solved: sub, buf: buffer.New(vw, vh), oy: offset}
	win.draw(crn)
	win.flushOverlays()
	c.buf.Blit(win.buf, layout.Rect{X: c.local(ir).X, Y: c.local(ir).Y, W: vw, H: vh})

	if bar {
		thumb := max(1, vh*vh/total)
		pos := 0
		if maxOff > 0 {
			pos = offset * (vh - thumb) / maxOff
		}
		col := make([]string, vh)
		for y := range col {
			if y >= pos && y < pos+thumb {
				col[y] = c.st.t("accent", "┃")
			} else {
				col[y] = c.st.t("border", "│")
			}
		}
		l := c.local(ir)
		c.blitLines(layout.Rect{X: l.X + vw, Y: l.Y, W: 1, H: vh}, col, nil)
	}

	// Geometry of visible descendants, translated into this scope's coordinates.
	order := foldNested(sub.Rects, sub.InnerRects, sub.FocusOrder, win.merges, c.b.userFocus)
	dx, dy := ir.X, ir.Y-offset
	view := layout.Rect{X: ir.X, Y: ir.Y, W: vw, H: vh}
	childY := map[string]int{}
	childRects := map[string]layout.Rect{}
	for id, r := range sub.Rects {
		if !IsSyntheticID(id) {
			childY[id] = r.Y
			childRects[id] = r
		}
	}
	scrolls := map[string]layout.ScrollInfo{}
	for _, wm := range win.merges {
		for id, info := range wm.scrolls {
			scrolls[id] = info
		}
	}
	if !IsSyntheticID(rn.id) {
		scrolls[rn.id] = layout.ScrollInfo{
			ContentH:   total,
			ViewH:      vh,
			Offset:     offset,
			MaxOff:     maxOff,
			ChildY:     childY,
			ChildRects: childRects,
		}
	}
	rec := mergeRec{owner: rn.id, rects: map[string]layout.Rect{}, inner: map[string]layout.Rect{}, scrolls: scrolls}
	for id, r := range sub.Rects {
		if cr, ok := clipRect(layout.Rect{X: r.X + dx, Y: r.Y + dy, W: r.W, H: r.H}, view); ok {
			rec.rects[id] = cr
		}
	}
	for id, r := range sub.InnerRects {
		if cr, ok := clipRect(layout.Rect{X: r.X + dx, Y: r.Y + dy, W: r.W, H: r.H}, view); ok {
			rec.inner[id] = cr
		}
	}
	for _, id := range order {
		if _, ok := rec.rects[id]; ok {
			rec.focus = append(rec.focus, id)
		}
	}
	c.merges = append(c.merges, rec)
}
