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
			c.drawFrame(lr, frameOpts{border: c.borderStyle(rn), title: n.Title, titleStyle: c.st.tone("title"), right: c.badgeRow(n.Badges)})
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

// borderStyle picks the border tone: focus accent, the node's Tone, unread, or plain.
func (c *ctx) borderStyle(rn *rnode) ToneStyle {
	n := rn.n
	name := n.Tone
	if name == "" && n.Unread {
		name = "unread"
	}
	if rn.onFocus {
		if name == "" {
			return c.st.tone("border.focus")
		}
		return mergeTone(c.st.tone(name), ToneStyle{Bold: true})
	}
	if name == "" {
		name = "border"
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
	thick      bool
	border     ToneStyle
	title      string
	titleStyle ToneStyle
	right      string // pre-styled text placed on the right of the top border
}

// drawFrame paints a rounded (or heavy) border with an optional title and right
// text embedded in the top edge. The interior is blanked.
func (c *ctx) drawFrame(lr layout.Rect, o frameOpts) {
	w, h := lr.W, lr.H
	if w < 2 || h < 2 {
		return
	}
	tl, tr, bl, br, hz, vt := "╭", "╮", "╰", "╯", "─", "│"
	if o.thick {
		tl, tr, bl, br, hz, vt = "┏", "┓", "┗", "┛", "━", "┃"
	}
	bs := func(s string) string { return c.st.paint(s, o.border) }
	span := w - 2
	left, leftW := "", 0
	if o.title != "" && span >= 5 {
		t := truncate(o.title, span-4)
		left = bs(hz+" ") + c.st.paint(t, o.titleStyle) + bs(" ")
		leftW = strW(t) + 3
	}
	right, rightW := "", 0
	if o.right != "" && span-leftW >= strW(o.right)+3 {
		right = bs(" ") + o.right + bs(" "+hz)
		rightW = strW(o.right) + 3
	}
	out := make([]string, h)
	out[0] = bs(tl) + left + bs(strings.Repeat(hz, span-leftW-rightW)) + right + bs(tr)
	mid := bs(vt) + spaces(span) + bs(vt)
	for y := 1; y < h-1; y++ {
		out[y] = mid
	}
	out[h-1] = bs(bl + strings.Repeat(hz, span) + br)
	c.blitLines(lr, out, nil)
}

func (c *ctx) drawPanel(rn *rnode, lr layout.Rect) {
	n := rn.n
	c.drawFrame(lr, frameOpts{
		thick:      false,
		border:     c.borderStyle(rn),
		title:      n.Title,
		titleStyle: c.titleStyle(rn),
		right:      c.badgeRow(n.Badges),
	})
	c.drawKids(rn)
}

func (c *ctx) titleStyle(rn *rnode) ToneStyle {
	ts := c.st.tone("title")
	if rn.n.Tone != "" {
		ts = mergeTone(ts, ToneStyle{Fg: c.st.tone(rn.n.Tone).Fg})
	}
	if rn.onFocus {
		ts = mergeTone(ts, ToneStyle{Fg: c.st.tone("accent").Fg})
	}
	return ts
}

func (c *ctx) drawOverlay(rn *rnode, lr layout.Rect) {
	n := rn.n
	border := c.st.tone("accent")
	if n.Tone != "" {
		border = c.st.tone(n.Tone)
	}
	c.drawFrame(lr, frameOpts{border: border, title: n.Title, titleStyle: c.titleStyle(rn), right: c.badgeRow(n.Badges)})
	c.drawKids(rn)
}

func (c *ctx) drawCard(rn *rnode, lr layout.Rect) {
	n := rn.n
	c.drawFrame(lr, frameOpts{thick: rn.onFocus, border: c.borderStyle(rn)})
	if rn.head != "" {
		if hr, ok := c.rect(rn.head); ok {
			c.blitLines(hr, cardHead(c.st, n, hr.W, rn.onFocus), nil)
		}
	}
	c.drawKids(rn)
	if rn.foot != "" {
		if fr, ok := c.rect(rn.foot); ok {
			c.blitLines(fr, []string{actionsLine(c.st, n.Actions)}, nil)
		}
	}
}

// cardHead renders the title (wrapped, with unread marker), badge rows, summary
// and meta of a card. st may be nil to count lines without styling.
func cardHead(st *styler, n *ViewNode, w int, focused bool) []string {
	var out []string
	tw := max(1, w-2)
	if n.Title != "" {
		marker := "  "
		if n.Unread {
			marker = st.t("unread", "● ")
		}
		ts := st.tone("title")
		if n.Tone != "" {
			ts = mergeTone(ts, ToneStyle{Fg: st.tone(n.Tone).Fg})
		}
		if focused {
			ts = mergeTone(ts, ToneStyle{Fg: st.tone("accent").Fg})
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
		for _, ln := range wrapPlain(s, tw) {
			out = append(out, "  "+st.t("text", ln))
		}
	}
	if m := metaText(n); m != "" {
		for _, ln := range wrapPlain(m, tw) {
			out = append(out, "  "+st.t("muted", ln))
		}
	}
	return out
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
		c.drawFrame(lr, frameOpts{border: c.borderStyle(rn), title: n.Title, titleStyle: c.titleStyle(rn), right: c.badgeRow(n.Badges)})
	}
	ir := c.solved.InnerRects[rn.id]
	vw, vh := ir.W, ir.H
	if vw <= 0 || vh <= 0 {
		return
	}
	total := flowHeight(n.Children, vw, n.Gap)
	bar := total > vh
	if bar {
		vw = max(1, vw-1)
		total = flowHeight(n.Children, vw, n.Gap)
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
	rec := mergeRec{owner: rn.id, rects: map[string]layout.Rect{}, inner: map[string]layout.Rect{}}
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
