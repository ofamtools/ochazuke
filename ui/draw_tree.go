package ui

import (
	"strings"

	"ofam.dev/ochazuke/buffer"
)

// TreeMeta is one right-aligned metadata column on a tree row.
type TreeMeta struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

// TreeNode is one node in a ui.KindTree ViewNode's "nodes" prop.
type TreeNode struct {
	Label      string     `json:"label"`
	Children   []TreeNode `json:"children,omitempty"`
	Expanded   *bool      `json:"expanded,omitempty"`
	Tone       string     `json:"tone,omitempty"`
	Icon       string     `json:"icon,omitempty"`
	IconTone   string     `json:"iconTone,omitempty"`
	Badge      string     `json:"badge,omitempty"`
	Badges     []Badge    `json:"badges,omitempty"`
	Meta       []TreeMeta `json:"meta,omitempty"`
	Unread     bool       `json:"unread,omitempty"`
	Editing    bool       `json:"editing,omitempty"`
	EditPrefix string     `json:"editPrefix,omitempty"`
	EditValue  string     `json:"editValue,omitempty"`
	EditCursor int        `json:"editCursor,omitempty"`
}

type treeRow struct {
	itemIdx    int // logical item index, or -1 for visual spacer rows
	spacer     bool
	prefix     string
	marker     string
	icon       string
	iconTone   string
	label      string
	badge      string
	badges     []Badge
	meta       []TreeMeta
	tone       string
	unread     bool
	hasKids    bool
	expanded   bool
	editing    bool
	editPrefix string
	editValue  string
	editCursor int
}

func treeRows(n *ViewNode) []treeRow {
	nodes, _ := propDecode[[]TreeNode](n, "nodes")
	guides := n.propBoolDefault("guides", true)
	gapTop := n.propBool("gapTopLevel")
	var out []treeRow
	itemIdx := 0
	var walk func(ns []TreeNode, depth int, cont string)
	walk = func(ns []TreeNode, depth int, cont string) {
		for i, nd := range ns {
			if gapTop && depth == 0 && i > 0 {
				out = append(out, treeRow{itemIdx: -1, spacer: true})
			}
			last := i == len(ns)-1
			prefix := ""
			switch {
			case depth == 0:
			case guides && last:
				prefix = cont + "└─ "
			case guides:
				prefix = cont + "├─ "
			default:
				prefix = spaces(2 * depth)
			}
			exp := nd.Expanded == nil || *nd.Expanded
			marker := "· "
			if len(nd.Children) > 0 {
				marker = "▸ "
				if exp {
					marker = "▾ "
				}
			}
			out = append(out, treeRow{
				itemIdx:    itemIdx,
				prefix:     prefix,
				marker:     marker,
				icon:       nd.Icon,
				iconTone:   nd.IconTone,
				label:      nd.Label,
				badge:      nd.Badge,
				badges:     nd.Badges,
				meta:       nd.Meta,
				tone:       nd.Tone,
				unread:     nd.Unread,
				hasKids:    len(nd.Children) > 0,
				expanded:   exp,
				editing:    nd.Editing,
				editPrefix: nd.EditPrefix,
				editValue:  nd.EditValue,
				editCursor: nd.EditCursor,
			})
			itemIdx++
			if len(nd.Children) > 0 && exp {
				next := cont
				if depth > 0 {
					if last {
						next += "   "
					} else {
						next += "│  "
					}
				}
				walk(nd.Children, depth+1, next)
			}
		}
	}
	walk(nodes, 0, "")
	return out
}

func withBg(ts ToneStyle, bg string) ToneStyle {
	if ts.Bg == "" && bg != "" {
		ts.Bg = bg
	}
	return ts
}

func wrapLabelLines(label string, firstW, contW int) []string {
	firstW = max(firstW, 1)
	contW = max(contW, 1)
	if firstW == contW {
		return wrapPlain(label, contW)
	}
	paras := strings.Split(label, "\n")
	var out []string
	for pi, p := range paras {
		rs := []rune(p)
		if pi == 0 {
			rngs := wrapPara(rs, 0, len(rs), firstW)
			out = append(out, string(rs[rngs[0][0]:rngs[0][1]]))
			if len(rngs) > 1 {
				for _, r := range wrapPara(rs, rngs[1][0], len(rs), contW) {
					out = append(out, string(rs[r[0]:r[1]]))
				}
			}
		} else {
			for _, r := range wrapPara(rs, 0, len(rs), contW) {
				out = append(out, string(rs[r[0]:r[1]]))
			}
		}
	}
	return out
}

func (c *ctx) renderTreeRowLines(rn *rnode, r treeRow, selected, wrap bool, w int) (lines []string, curX, curLine int, hasCur bool) {
	if r.spacer {
		return []string{""}, 0, 0, false
	}
	if r.editing {
		style := c.st.tone("input.edit")
		fill := c.st.sgr(style)
		lead := r.prefix + r.marker + r.editPrefix
		leadW := strW(lead)
		styledLead := c.st.t("border", r.prefix) + c.st.t("accent", r.marker) + c.st.t("muted", r.editPrefix)
		if wrap || strings.Contains(r.editValue, "\n") {
			avail := max(1, w-leadW)
			rs := []rune(fieldTextMultiline(r.editValue))
			ci := clampInt(r.editCursor, 0, len(rs))
			rngs := wrapRanges(rs, avail)
			cl, cc := wrapCursor(rs, rngs, ci)
			out := make([]string, len(rngs))
			for li, rng := range rngs {
				seg := string(rs[rng[0]:rng[1]])
				body := padLine(c.st.paint(seg, style), avail, fill)
				if li == 0 {
					out[li] = styledLead + body
				} else {
					out[li] = spaces(leadW) + body
				}
			}
			return out, min(leadW+cc, max(0, w-1)), cl, true
		}
		rs := []rune(r.editValue)
		ci := clampInt(r.editCursor, 0, len(rs))
		avail := max(1, w-leadW-1)
		startRune := 0
		for startRune < ci && runesW(rs[startRune:ci]) > avail-1 {
			startRune++
		}
		line := styledLead + padLine(c.st.paint(string(rs[startRune:]), style), max(1, w-leadW), fill)
		return []string{line}, leadW + runesW(rs[startRune:ci]), 0, true
	}

	// Compute right-aligned meta width and optional label truncation/wrapping.
	var metas []TreeMeta
	metaW := 0
	for _, m := range r.meta {
		if m.Text == "" {
			continue
		}
		if len(metas) > 0 {
			metaW += 2
		}
		metaW += strW(m.Text)
		metas = append(metas, m)
	}
	badgesW := 0
	if r.badge != "" {
		badgesW += 1 + strW(r.badge)
	}
	for _, b := range r.badges {
		badgesW += 1 + strW(b.Text) + 2
	}
	iconW := 0
	if r.icon != "" {
		iconW = strW(r.icon)
		if r.label != "" {
			iconW++
		}
	}
	leadW := strW(r.prefix) + strW(r.marker) + iconW
	doWrap := wrap || strings.Contains(r.label, "\n")
	showMeta := false
	metaGap := 0
	var labelLines []string

	if doWrap {
		cleanLabel := fieldTextMultiline(r.label)
		availCont := max(1, w-leadW)
		availFirst := max(1, w-leadW-badgesW)
		if metaW > 0 {
			firstPara := cleanLabel
			if nl := strings.IndexByte(firstPara, '\n'); nl >= 0 {
				firstPara = firstPara[:nl]
			}
			minLabel := min(strW(firstPara), 4)
			if leadW+minLabel+badgesW+2+metaW <= w {
				showMeta = true
				availFirst = max(1, w-leadW-badgesW-2-metaW)
			}
		}
		labelLines = wrapLabelLines(cleanLabel, availFirst, availCont)
		if showMeta {
			metaGap = max(0, w-(leadW+strW(labelLines[0])+badgesW)-metaW)
		}
	} else {
		shownLabel := r.label
		if metaW > 0 {
			minLabel := min(strW(r.label), 4)
			if leadW+minLabel+badgesW+2+metaW <= w {
				showMeta = true
				availLabel := w - leadW - badgesW - 2 - metaW
				if strW(shownLabel) > availLabel {
					shownLabel = truncate(shownLabel, availLabel)
				}
				metaGap = w - (leadW + strW(shownLabel) + badgesW) - metaW
			}
		}
		labelLines = []string{shownLabel}
	}

	rowTS := c.st.tone(toneOr(r.tone, "text"))
	firstLabel := labelLines[0]

	if selected {
		selTS := c.rowStyle(rn, true)
		selPadTS := selTS
		selPadTS.Strikethrough = false
		labelTS := selPadTS
		labelTS.Strikethrough = rowTS.Strikethrough

		var sb strings.Builder
		if r.prefix != "" {
			sb.WriteString(c.st.paint(r.prefix, selPadTS))
		}
		if r.marker != "" {
			sb.WriteString(c.st.paint(r.marker, selPadTS))
		}
		if r.icon != "" {
			var resolvedIcon ToneStyle
			switch {
			case r.iconTone != "":
				resolvedIcon = c.st.tone(r.iconTone)
			case r.tone != "":
				resolvedIcon = c.st.tone(r.tone)
			}
			iconTS := selPadTS
			if resolvedIcon.Fg != "" {
				iconTS.Fg = resolvedIcon.Fg
			}
			iconTS.Bg = selTS.Bg
			iconTS.Bold = selTS.Bold || resolvedIcon.Bold
			iconTS.Strikethrough = false
			sb.WriteString(c.st.paint(r.icon, iconTS))
			if firstLabel != "" || len(labelLines) > 1 {
				sb.WriteString(c.st.paint(" ", selPadTS))
			}
		}
		if firstLabel != "" {
			sb.WriteString(c.st.paint(firstLabel, labelTS))
		}
		if r.badge != "" {
			sb.WriteString(c.st.paint(" "+r.badge, selPadTS))
		}
		for _, b := range r.badges {
			sb.WriteString(c.st.paint(" ", selPadTS))
			bs := c.st.badgeStyle(b.Tone)
			if bs.Bg == "" {
				bs.Bg = selTS.Bg
			}
			bs.Strikethrough = false
			sb.WriteString(c.st.paint(" "+b.Text+" ", bs))
		}
		if showMeta {
			if metaGap > 0 {
				sb.WriteString(c.st.paint(spaces(metaGap), selPadTS))
			}
			for mi, m := range metas {
				if mi > 0 {
					sb.WriteString(c.st.paint("  ", selPadTS))
				}
				mt := c.st.tone(toneOr(m.Tone, "muted"))
				mTS := ToneStyle{
					Fg:     mt.Fg,
					Bg:     selTS.Bg,
					Bold:   mt.Bold,
					Dim:    mt.Dim,
					Italic: mt.Italic,
				}
				if mTS.Fg == "" {
					mTS.Fg = selTS.Fg
				}
				sb.WriteString(c.st.paint(m.Text, mTS))
			}
		}
		out := make([]string, 0, len(labelLines))
		out = append(out, padLine(sb.String(), w, c.st.sgr(selPadTS)))
		for _, contSeg := range labelLines[1:] {
			cont := c.st.paint(spaces(leadW), selPadTS)
			if contSeg != "" {
				cont += c.st.paint(contSeg, labelTS)
			}
			out = append(out, padLine(cont, w, c.st.sgr(selPadTS)))
		}
		return out, 0, 0, false
	}

	padTS := ToneStyle{Bg: rowTS.Bg}
	labelTS := rowTS
	if r.unread {
		labelTS = withBg(c.st.tone("unread"), rowTS.Bg)
	}
	var sb strings.Builder
	if r.prefix != "" {
		sb.WriteString(c.st.paint(r.prefix, withBg(c.st.tone("border"), rowTS.Bg)))
	}
	if r.marker != "" {
		markerTone := "accent"
		if !r.hasKids {
			markerTone = "muted"
		}
		sb.WriteString(c.st.paint(r.marker, withBg(c.st.tone(markerTone), rowTS.Bg)))
	}
	if r.icon != "" {
		iconToneName := toneOr(r.iconTone, toneOr(r.tone, "text"))
		iconTS := withBg(c.st.tone(iconToneName), rowTS.Bg)
		iconTS.Strikethrough = false
		sb.WriteString(c.st.paint(r.icon, iconTS))
		if firstLabel != "" || len(labelLines) > 1 {
			sb.WriteString(c.st.paint(" ", padTS))
		}
	}
	if firstLabel != "" {
		sb.WriteString(c.st.paint(firstLabel, labelTS))
	}
	if r.badge != "" {
		sb.WriteString(c.st.paint(" "+r.badge, withBg(c.st.tone("muted"), rowTS.Bg)))
	}
	for _, b := range r.badges {
		sb.WriteString(c.st.paint(" ", padTS))
		sb.WriteString(c.st.badgeText(b))
	}
	if showMeta {
		if metaGap > 0 {
			sb.WriteString(c.st.paint(spaces(metaGap), padTS))
		}
		for mi, m := range metas {
			if mi > 0 {
				sb.WriteString(c.st.paint("  ", padTS))
			}
			mTS := withBg(c.st.tone(toneOr(m.Tone, "muted")), rowTS.Bg)
			sb.WriteString(c.st.paint(m.Text, mTS))
		}
	}
	out := make([]string, 0, len(labelLines))
	line := sb.String()
	if rowTS.Bg != "" {
		line = padLine(line, w, c.st.sgr(padTS))
	}
	out = append(out, line)
	for _, contSeg := range labelLines[1:] {
		cont := c.st.paint(spaces(leadW), padTS)
		if contSeg != "" {
			cont += c.st.paint(contSeg, labelTS)
		}
		if rowTS.Bg != "" {
			cont = padLine(cont, w, c.st.sgr(padTS))
		}
		out = append(out, cont)
	}
	return out, 0, 0, false
}

func (c *ctx) treeLines(rn *rnode, w, h int) ([]string, *buffer.Cursor) {
	rows := treeRows(rn.n)
	selItem := selectedOf(rn.n)
	selRow := -1
	for i, r := range rows {
		if !r.spacer && r.itemIdx == selItem {
			selRow = i
			break
		}
	}
	wrap := rn.n.propBool("wrap")
	anyMultiLine := wrap
	if !anyMultiLine {
		for _, r := range rows {
			if strings.Contains(r.label, "\n") || (r.editing && strings.Contains(r.editValue, "\n")) {
				anyMultiLine = true
				break
			}
		}
	}
	if !anyMultiLine {
		start := windowStart(rn.n, len(rows), h, selRow)
		var out []string
		var cur *buffer.Cursor
		for i := start; i < min(len(rows), start+h); i++ {
			rLines, cx, cl, hasCur := c.renderTreeRowLines(rn, rows[i], i == selRow, false, w)
			if hasCur {
				cur = c.cursorIf(rn, cx, i-start+cl, buffer.CursorBar)
			}
			out = append(out, rLines...)
		}
		return out, cur
	}

	var allLines []string
	var cur *buffer.Cursor
	rowStarts := make([]int, len(rows))
	rowEnds := make([]int, len(rows))
	for i, r := range rows {
		rowStarts[i] = len(allLines)
		rLines, cx, cl, hasCur := c.renderTreeRowLines(rn, r, i == selRow, wrap, w)
		if hasCur {
			cur = c.cursorIf(rn, cx, rowStarts[i]+cl, buffer.CursorBar)
		}
		allLines = append(allLines, rLines...)
		rowEnds[i] = len(allLines) - 1
	}

	total := len(allLines)
	if h <= 0 || total <= h {
		return allLines, cur
	}

	visStart := 0
	if o, ok := rn.n.propInt("offset"); ok && len(rows) > 0 {
		oi := clampInt(o, 0, len(rows)-1)
		visStart = rowStarts[oi]
	}
	visStart = min(max(visStart, 0), max(0, total-h))

	if selRow >= 0 && selRow < len(rows) {
		selStart, selEnd := rowStarts[selRow], rowEnds[selRow]
		if selEnd-selStart+1 <= h {
			if selEnd >= visStart+h {
				visStart = selEnd - h + 1
			}
			if selStart < visStart {
				visStart = selStart
			}
		} else if cur == nil {
			if selStart >= visStart+h || selEnd < visStart {
				visStart = selStart
			}
		}
	}
	if cur != nil {
		if cur.Y >= visStart+h {
			visStart = cur.Y - h + 1
		}
		if cur.Y < visStart {
			visStart = cur.Y
		}
	}
	visStart = min(max(visStart, 0), max(0, total-h))

	win := allLines[visStart:min(total, visStart+h)]
	if cur != nil {
		cur.Y -= visStart
		if cur.Y < 0 || cur.Y >= len(win) {
			cur = nil
		}
	}
	return win, cur
}
