package ui

import (
	"regexp"
	"strings"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/layout"
)

// drawLeaf paints content kinds into the node's inner rect.
func (c *ctx) drawLeaf(rn *rnode, lr layout.Rect) {
	n := rn.n
	if n.Border {
		c.drawFrame(lr, frameOpts{focus: rn.onFocus, border: c.borderStyle(rn), title: n.Title, titleStyle: c.titleStyle(rn), right: c.badgeRow(n.Badges)})
	}
	ir := lr
	if r, ok := c.solved.InnerRects[rn.id]; ok {
		ir = c.local(r)
	}
	if ir.W <= 0 || ir.H <= 0 {
		return
	}
	var out []string
	var cur *buffer.Cursor
	switch n.Kind {
	case KindText:
		out = c.textLines(rn, ir.W)
	case KindMarkdown:
		out = c.markdownLines(rn, ir.W)
	case KindTable:
		out = c.tableLines(rn, ir.W, ir.H)
	case KindTree:
		out, cur = c.treeLines(rn, ir.W, ir.H)
	case KindInput:
		out, cur = c.inputLines(rn, ir.W, ir.H)
	case KindForm:
		out, cur = c.formLines(rn, ir.W, ir.H)
	case KindPicker:
		out, cur = c.pickerLines(rn, ir.W, ir.H)
	case KindTerminal:
		out, cur = c.terminalLines(rn, ir.W, ir.H)
	case KindRaw:
		out = ansiLines(n)
		cur = c.nodeCursor(rn)
	default:
		out = []string{c.st.t("muted", "‹"+n.Kind+"›")}
	}
	c.blitLines(ir, out, cur)
}

func toneOr(name, def string) string {
	if name == "" {
		return def
	}
	return name
}

func (c *ctx) focusedTextStyle() ToneStyle {
	if ts := c.st.tone("selected"); ts != (ToneStyle{}) {
		return ts
	}
	return mergeTone(c.st.tone("focus.fg"), c.st.tone("focus.bg"))
}

func (c *ctx) textLines(rn *rnode, w int) []string {
	n := rn.n
	focused := rn.isFocus && n.Focusable
	rawText, spans := resolveTextNode(n)
	if focused {
		if ft := n.propString("focusedText"); ft != "" {
			rawText = ft
			spans = nil
		}
	}
	rs := []rune(rawText)
	base := c.st.tone(toneOr(n.Tone, "text"))
	if focused {
		base = c.focusedTextStyle()
	}
	rsp := c.st.resolveSpans(spans, base)
	if focused && base.Bg != "" {
		for i := range rsp {
			if rsp[i].ts.Bg == "" {
				rsp[i].ts.Bg = base.Bg
			}
			rsp[i].ts.Bold = rsp[i].ts.Bold || base.Bold
		}
	}
	fill := ""
	if focused {
		fill = c.st.sgr(base)
	}
	var out []string
	for _, r := range wrapRanges(rs, w) {
		line := c.st.runs(rs, r[0], r[1], base, rsp)
		if focused {
			line = padLine(line, w, fill)
		}
		out = append(out, line)
	}
	return out
}

// --- markdown -----------------------------------------------------------------

// Heading is one Markdown heading, for building a table of contents.
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"` // 0-based source line
}

func headingOf(trim string) (int, string, bool) {
	lvl := 0
	for lvl < len(trim) && trim[lvl] == '#' {
		lvl++
	}
	if lvl == 0 || lvl > 6 || len(trim) <= lvl || trim[lvl] != ' ' {
		return 0, "", false
	}
	return lvl, strings.TrimSpace(strings.TrimRight(trim[lvl+1:], "# ")), true
}

// MarkdownHeadings lists the headings of src, skipping fenced code blocks.
func MarkdownHeadings(src string) []Heading {
	var out []Heading
	inCode := false
	for i, ln := range strings.Split(src, "\n") {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		if lvl, txt, ok := headingOf(trim); ok {
			out = append(out, Heading{Level: lvl, Text: txt, Line: i})
		}
	}
	return out
}

var orderedRe = regexp.MustCompile(`^(\d+)[.)] `)

// mdLines renders basic Markdown (headings, lists, checkboxes, quotes, rules,
// fenced code, and **bold**, *em*, `code`, [links](url)). st may be nil for
// plain output.
func mdLines(st *styler, text string, w int) []string {
	return mdLinesWith(st, text, w, mdRenderOpts{activeLink: -1, activeCheck: -1})
}

func indexRunes(rs []rune, from int, sub string) int {
	t := []rune(sub)
	for i := from; i+len(t) <= len(rs); i++ {
		if string(rs[i:i+len(t)]) == sub {
			return i
		}
	}
	return -1
}

// --- list windowing -------------------------------------------------------------

func selectedOf(n *ViewNode) int {
	if s, ok := n.propInt("selected"); ok {
		return s
	}
	return -1
}

// windowStart picks the first visible row: props.offset if set, else the least
// scroll that keeps the selected row in view.
func windowStart(n *ViewNode, total, h, sel int) int {
	maxStart := max(0, total-h)
	if o, ok := n.propInt("offset"); ok {
		return min(max(o, 0), maxStart)
	}
	if sel < h {
		return 0
	}
	return min(sel-h+1, maxStart)
}

func (c *ctx) rowStyle(rn *rnode, selected bool) ToneStyle {
	switch {
	case selected && rn.onFocus:
		return c.st.tone("selected")
	case selected:
		return c.st.tone("selected.inactive")
	}
	return c.st.tone("text")
}

// --- table ----------------------------------------------------------------------

type tableCol struct {
	title, key string
	width      int
	right      bool
}

func tableCols(n *ViewNode) []tableCol {
	var cols []tableCol
	for _, e := range toSlice(n.Props["columns"]) {
		switch x := e.(type) {
		case map[string]any:
			col := tableCol{title: anyString(firstOf(x, "title", "label", "name", "key")), key: anyString(firstOf(x, "key", "name", "title"))}
			col.width, _ = anyInt(x["width"])
			col.right = anyString(x["align"]) == "right"
			cols = append(cols, col)
		default:
			s := anyString(e)
			cols = append(cols, tableCol{title: s, key: s})
		}
	}
	return cols
}

func firstOf(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

// TableRowSpec describes one styled table row with optional whole-row tone,
// per-cell tones, per-cell leading icons, per-cell icon tones, and per-cell
// rune ToneSpans.
type TableRowSpec struct {
	Cells     []any        `json:"cells"`
	Tone      string       `json:"tone,omitempty"`
	CellTones []string     `json:"cellTones,omitempty"`
	CellIcons []string     `json:"cellIcons,omitempty"`
	IconTones []string     `json:"iconTones,omitempty"`
	CellSpans [][]ToneSpan `json:"cellSpans,omitempty"`
}

type parsedRow struct {
	cells     []string
	tone      string
	cellTones []string
	cellIcons []string
	iconTones []string
	cellSpans [][]ToneSpan
	rich      bool
}

func hasAnyNonEmpty(ss []string) bool {
	for _, s := range ss {
		if s != "" {
			return true
		}
	}
	return false
}

func hasAnyCellSpans(ss [][]ToneSpan) bool {
	for _, s := range ss {
		if len(s) > 0 {
			return true
		}
	}
	return false
}

func cellSpansAt(ss [][]ToneSpan, i int) []ToneSpan {
	if i >= 0 && i < len(ss) {
		return ss[i]
	}
	return nil
}

func sliceAt(ss []string, i int) string {
	if i >= 0 && i < len(ss) {
		return ss[i]
	}
	return ""
}

func decodeTableCells(rawCells []any, colCount int, explicitSpans [][]ToneSpan, activeLink int) ([]string, [][]ToneSpan) {
	out := make([]string, colCount)
	var spans [][]ToneSpan
	setSpans := func(i int, sp []ToneSpan) {
		if len(sp) == 0 {
			return
		}
		if spans == nil {
			spans = make([][]ToneSpan, colCount)
		}
		spans[i] = sp
	}
	linkOffset := 0
	for i := 0; i < colCount; i++ {
		var cellSp []ToneSpan
		if i < len(rawCells) {
			if runs, ok := DecodeInlineRuns(rawCells[i]); ok {
				localActive := -1
				if activeLink >= 0 {
					localActive = activeLink - linkOffset
				}
				plain, runSpans, links := CompileInlineRuns(runs, localActive)
				linkOffset += len(links)
				out[i] = plain
				cellSp = runSpans
			} else {
				out[i] = anyString(rawCells[i])
			}
		}
		if exp := cellSpansAt(explicitSpans, i); len(exp) > 0 {
			if len(cellSp) > 0 {
				combined := make([]ToneSpan, 0, len(cellSp)+len(exp))
				combined = append(combined, cellSp...)
				combined = append(combined, exp...)
				cellSp = combined
			} else {
				cellSp = exp
			}
		}
		setSpans(i, cellSp)
	}
	return out, spans
}

func parseTableRow(row any, cols []tableCol) parsedRow {
	return parseTableRowWith(row, cols, -1)
}

func parseTableRowWith(row any, cols []tableCol, activeLink int) parsedRow {
	switch x := row.(type) {
	case TableRowSpec:
		cells, cellSpans := decodeTableCells(x.Cells, len(cols), x.CellSpans, activeLink)
		return parsedRow{
			cells:     cells,
			tone:      x.Tone,
			cellTones: x.CellTones,
			cellIcons: x.CellIcons,
			iconTones: x.IconTones,
			cellSpans: cellSpans,
			rich:      x.Tone != "" || hasAnyNonEmpty(x.CellTones) || hasAnyNonEmpty(x.CellIcons) || hasAnyNonEmpty(x.IconTones) || hasAnyCellSpans(cellSpans),
		}
	case *TableRowSpec:
		if x == nil {
			return parsedRow{cells: make([]string, len(cols))}
		}
		cells, cellSpans := decodeTableCells(x.Cells, len(cols), x.CellSpans, activeLink)
		return parsedRow{
			cells:     cells,
			tone:      x.Tone,
			cellTones: x.CellTones,
			cellIcons: x.CellIcons,
			iconTones: x.IconTones,
			cellSpans: cellSpans,
			rich:      x.Tone != "" || hasAnyNonEmpty(x.CellTones) || hasAnyNonEmpty(x.CellIcons) || hasAnyNonEmpty(x.IconTones) || hasAnyCellSpans(cellSpans),
		}
	case []any:
		cells, cellSpans := decodeTableCells(x, len(cols), nil, activeLink)
		return parsedRow{cells: cells, cellSpans: cellSpans, rich: hasAnyCellSpans(cellSpans)}
	case map[string]any:
		if rawCells, ok := x["cells"]; ok {
			if cs := toSlice(rawCells); cs != nil {
				expSpans, _ := decodeAny[[][]ToneSpan](x["cellSpans"])
				cells, cellSpans := decodeTableCells(cs, len(cols), expSpans, activeLink)
				tone := anyString(x["tone"])
				cellTones := toStrings(x["cellTones"])
				cellIcons := toStrings(x["cellIcons"])
				iconTones := toStrings(x["iconTones"])
				return parsedRow{
					cells:     cells,
					tone:      tone,
					cellTones: cellTones,
					cellIcons: cellIcons,
					iconTones: iconTones,
					cellSpans: cellSpans,
					rich:      tone != "" || hasAnyNonEmpty(cellTones) || hasAnyNonEmpty(cellIcons) || hasAnyNonEmpty(iconTones) || hasAnyCellSpans(cellSpans),
				}
			}
		}
		rawCols := make([]any, len(cols))
		for i, col := range cols {
			rawCols[i] = x[col.key]
		}
		cells, cellSpans := decodeTableCells(rawCols, len(cols), nil, activeLink)
		return parsedRow{cells: cells, cellSpans: cellSpans, rich: hasAnyCellSpans(cellSpans)}
	default:
		cells, cellSpans := decodeTableCells(toSlice(row), len(cols), nil, activeLink)
		return parsedRow{cells: cells, cellSpans: cellSpans, rich: hasAnyCellSpans(cellSpans)}
	}
}

func (c *ctx) renderRichTableRow(pr parsedRow, cols []tableCol, widths []int, w int, selected bool, baseRowTS ToneStyle, activeCol int, activeColTS ToneStyle) string {
	rowTS := c.st.tone(toneOr(pr.tone, "text"))
	var rowPadTS ToneStyle
	if selected {
		rowPadTS = baseRowTS
		rowPadTS.Strikethrough = false
	} else {
		rowPadTS = ToneStyle{Bg: rowTS.Bg}
	}

	var sb strings.Builder
	for ci, cell := range pr.cells {
		if ci > 0 {
			sb.WriteString(c.st.paint("  ", rowPadTS))
		}
		cw := widths[ci]
		icon := sliceAt(pr.cellIcons, ci)
		ct := sliceAt(pr.cellTones, ci)
		it := sliceAt(pr.iconTones, ci)

		var cellTS, cellPadTS, iconTS ToneStyle
		if selected {
			baseTS := baseRowTS
			if ci == activeCol {
				baseTS = activeColTS
			}
			cellTS = baseTS
			cellTS.Strikethrough = rowTS.Strikethrough || (ct != "" && c.st.tone(ct).Strikethrough)
			cellPadTS = baseTS
			cellPadTS.Strikethrough = false
			if icon != "" {
				var resolvedIcon ToneStyle
				switch {
				case it != "":
					resolvedIcon = c.st.tone(it)
				case ct != "":
					resolvedIcon = c.st.tone(ct)
				case pr.tone != "":
					resolvedIcon = c.st.tone(pr.tone)
				}
				iconTS = cellPadTS
				if resolvedIcon.Fg != "" {
					iconTS.Fg = resolvedIcon.Fg
				}
				iconTS.Bg = baseTS.Bg
				iconTS.Bold = baseTS.Bold || resolvedIcon.Bold
				iconTS.Strikethrough = false
			}
		} else {
			cellTS = rowTS
			if ct != "" {
				cellTS = c.st.tone(ct)
				if cellTS.Bg == "" {
					cellTS.Bg = rowTS.Bg
				}
				cellTS.Strikethrough = cellTS.Strikethrough || rowTS.Strikethrough
			}
			cellPadTS = ToneStyle{Bg: cellTS.Bg}
			if icon != "" {
				switch {
				case it != "":
					iconTS = c.st.tone(it)
					if iconTS.Bg == "" {
						iconTS.Bg = cellTS.Bg
					}
				case ct != "":
					iconTS = cellTS
				default:
					iconTS = rowTS
				}
				iconTS.Strikethrough = false
			}
		}

		spans := cellSpansAt(pr.cellSpans, ci)
		if icon == "" {
			painted, visW := paintSpannedText(c.st, cell, cw, cellTS, spans)
			pad := cw - visW
			if cols[ci].right {
				if pad > 0 {
					sb.WriteString(c.st.paint(spaces(pad), cellPadTS))
				}
				sb.WriteString(painted)
			} else {
				sb.WriteString(painted)
				if pad > 0 {
					sb.WriteString(c.st.paint(spaces(pad), cellPadTS))
				}
			}
			continue
		}

		iw := strW(icon)
		iconPart := icon
		paintedTxt := ""
		pad := 0
		switch {
		case iw >= cw:
			iconPart = truncate(icon, cw)
			pad = cw - strW(iconPart)
		case cell == "":
			pad = cw - iw
		default:
			rem := cw - iw - 1
			if rem <= 0 {
				pad = cw - iw
			} else {
				var visW int
				paintedTxt, visW = paintSpannedText(c.st, cell, rem, cellTS, spans)
				pad = cw - iw - 1 - visW
			}
		}

		if cols[ci].right {
			if pad > 0 {
				sb.WriteString(c.st.paint(spaces(pad), cellPadTS))
			}
			sb.WriteString(c.st.paint(iconPart, iconTS))
			if paintedTxt != "" {
				sb.WriteString(c.st.paint(" ", cellPadTS))
				sb.WriteString(paintedTxt)
			}
		} else {
			sb.WriteString(c.st.paint(iconPart, iconTS))
			if paintedTxt != "" {
				sb.WriteString(c.st.paint(" ", cellPadTS))
				sb.WriteString(paintedTxt)
			}
			if pad > 0 {
				sb.WriteString(c.st.paint(spaces(pad), cellPadTS))
			}
		}
	}

	fill := ""
	if selected || rowTS.Bg != "" {
		fill = c.st.sgr(rowPadTS)
	}
	return padLine(sb.String(), w, fill)
}

func (c *ctx) tableLines(rn *rnode, w, h int) []string {
	n := rn.n
	cols := tableCols(n)
	rows := toSlice(n.Props["rows"])
	if len(cols) == 0 || h < 1 {
		return nil
	}
	// Natural widths from the header and a sample of rows.
	widths := make([]int, len(cols))
	flexible := make([]bool, len(cols))
	for i, col := range cols {
		if col.width > 0 {
			widths[i] = col.width
		} else {
			widths[i], flexible[i] = strW(col.title), true
		}
	}
	for _, row := range rows[:min(len(rows), 200)] {
		pr := parseTableRow(row, cols)
		for i, cell := range pr.cells {
			if flexible[i] {
				cw := strW(cell)
				if icon := sliceAt(pr.cellIcons, i); icon != "" {
					if cell == "" {
						cw = strW(icon)
					} else {
						cw += strW(icon) + 1
					}
				}
				widths[i] = min(max(widths[i], cw), 40)
			}
		}
	}
	total := 2 * (len(cols) - 1)
	for _, cw := range widths {
		total += cw
	}
	for total > w {
		best := -1
		for i := range widths {
			if flexible[i] && widths[i] > 4 && (best < 0 || widths[i] > widths[best]) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		widths[best]--
		total--
	}
	formatCells := func(cells []string) []string {
		parts := make([]string, len(cols))
		for i, cell := range cells {
			cell = truncate(cell, widths[i])
			if cols[i].right {
				parts[i] = spaces(widths[i]-strW(cell)) + cell
			} else {
				parts[i] = padPlain(cell, widths[i])
			}
		}
		return parts
	}
	format := func(cells []string) string {
		return strings.Join(formatCells(cells), "  ")
	}
	heads := make([]string, len(cols))
	for i, col := range cols {
		heads[i] = col.title
	}
	out := []string{c.st.t("table.header", format(heads))}
	if h == 1 {
		return out
	}
	rowsH := h - 2
	sel := selectedOf(n)
	selCol, hasSelCol := n.propInt("selectedCol")
	if !hasSelCol {
		selCol = -1
	}
	vFrom, hasVFrom := n.propInt("visualFrom")
	vTo, hasVTo := n.propInt("visualTo")
	hasVisual := hasVFrom && hasVTo && vFrom <= vTo
	start := windowStart(n, len(rows), rowsH, sel)
	rule := strings.Repeat("─", w)
	if len(rows) > rowsH && w > 12 {
		pos := " " + itoa(min(len(rows), start+rowsH)) + "/" + itoa(len(rows)) + " "
		rule = strings.Repeat("─", w-strW(pos)-1) + pos + "─"
	}
	out = append(out, c.st.t("border", rule))
	activeLink, hasActiveLink := n.propInt("activeLink")
	if !hasActiveLink {
		activeLink = -1
	}
	for i := start; i < min(len(rows), start+rowsH); i++ {
		rowActiveLink := -1
		if i == sel && activeLink >= 0 {
			rowActiveLink = activeLink
		}
		pr := parseTableRowWith(rows[i], cols, rowActiveLink)
		if pr.rich {
			switch {
			case i == sel && selCol >= 0 && selCol < len(cols):
				rowTS := c.st.tone("selected.inactive")
				cellTS := mergeTone(c.rowStyle(rn, true), ToneStyle{Fg: c.st.tone("accent").Fg, Bold: true})
				out = append(out, c.renderRichTableRow(pr, cols, widths, w, true, rowTS, selCol, cellTS))
			case i == sel:
				out = append(out, c.renderRichTableRow(pr, cols, widths, w, true, c.rowStyle(rn, true), -1, ToneStyle{}))
			case hasVisual && i >= vFrom && i <= vTo:
				out = append(out, c.renderRichTableRow(pr, cols, widths, w, true, c.st.tone("selected.inactive"), -1, ToneStyle{}))
			default:
				out = append(out, c.renderRichTableRow(pr, cols, widths, w, false, ToneStyle{}, -1, ToneStyle{}))
			}
			continue
		}
		if i == sel && selCol >= 0 && selCol < len(cols) {
			parts := formatCells(pr.cells)
			rowTS := c.st.tone("selected.inactive")
			cellTS := mergeTone(c.rowStyle(rn, true), ToneStyle{Fg: c.st.tone("accent").Fg, Bold: true})
			var sb strings.Builder
			for ci, p := range parts {
				if ci > 0 {
					sb.WriteString(c.st.paint("  ", rowTS))
				}
				if ci == selCol {
					sb.WriteString(c.st.paint(p, cellTS))
				} else {
					sb.WriteString(c.st.paint(p, rowTS))
				}
			}
			out = append(out, padLine(sb.String(), w, c.st.sgr(rowTS)))
			continue
		}
		line := format(pr.cells)
		var ts ToneStyle
		if i == sel {
			ts = c.rowStyle(rn, true)
		} else if hasVisual && i >= vFrom && i <= vTo {
			ts = c.st.tone("selected.inactive")
		} else {
			ts = c.rowStyle(rn, false)
		}
		out = append(out, padLine(c.st.paint(line, ts), w, c.st.sgr(ts)))
	}
	return out
}

func itoa(i int) string { return anyString(float64(i)) }

// --- input and form ---------------------------------------------------------------

func clampInt(v, lo, hi int) int { return min(max(v, lo), max(lo, hi)) }

func (c *ctx) inputLines(rn *rnode, w, h int) ([]string, *buffer.Cursor) {
	n := rn.n
	value := n.propString("value")
	rs := []rune(value)
	ci := len(rs)
	if v, ok := n.propInt("cursor"); ok {
		ci = clampInt(v, 0, len(rs))
	}
	style := c.st.tone("input")
	if rn.onFocus {
		style = c.st.tone("input.focus")
	}
	fill := c.st.sgr(style)
	ph := n.propString("placeholder")

	if n.propBool("multiline") {
		if n.propBoolDefault("wrap", true) {
			avail := max(1, w-2)
			rngs := wrapRanges(rs, avail)
			cl, cc := wrapCursor(rs, rngs, ci)
			start := windowStart(&ViewNode{}, len(rngs), h, cl)
			var out []string
			for i := start; i < min(len(rngs), start+h); i++ {
				out = append(out, padLine(c.st.paint(" "+string(rs[rngs[i][0]:rngs[i][1]]), style), w, fill))
			}
			for len(out) < h {
				out = append(out, padLine("", w, fill))
			}
			if value == "" && ph != "" {
				phLines := wrapPlain(ph, avail)
				for i := 0; i < min(len(phLines), len(out)); i++ {
					out[i] = padLine(c.st.t("placeholder", " "+phLines[i]), w, fill)
				}
			}
			return out, c.cursorIf(rn, min(1+cc, max(0, w-1)), cl-start, buffer.CursorBar)
		}
		all := strings.Split(value, "\n")
		cl, cc := 0, 0
		for _, r := range rs[:ci] {
			if r == '\n' {
				cl++
				cc = 0
			} else {
				cc += runeW(r)
			}
		}
		start := windowStart(&ViewNode{}, len(all), h, cl)
		var out []string
		for i := start; i < min(len(all), start+h); i++ {
			out = append(out, padLine(c.st.paint(" "+all[i], style), w, fill))
		}
		for len(out) < h {
			out = append(out, padLine("", w, fill))
		}
		if value == "" && ph != "" && len(out) > 0 {
			out[0] = padLine(c.st.t("placeholder", " "+ph), w, fill)
		}
		return out, c.cursorIf(rn, 1+cc, cl-start, buffer.CursorBar)
	}

	label, lw := "", 0
	if n.Title != "" {
		label = c.st.t("muted", n.Title+" ")
		lw = strW(n.Title) + 1
	}
	avail := max(1, w-lw-1)
	startRune := 0
	for startRune < ci && runesW(rs[startRune:ci]) > avail-1 {
		startRune++
	}
	var body string
	if value == "" && ph != "" {
		body = c.st.t("placeholder", " "+truncate(ph, avail))
	} else {
		body = c.st.paint(" "+string(rs[startRune:]), style)
	}
	line := padLine(label+body, w, fill)
	return []string{line}, c.cursorIf(rn, lw+1+runesW(rs[startRune:ci]), 0, buffer.CursorBar)
}

func (c *ctx) cursorIf(rn *rnode, x, y int, shape buffer.CursorShape) *buffer.Cursor {
	if !rn.isFocus {
		return nil
	}
	return &buffer.Cursor{X: x, Y: y, Shape: shape}
}

// --- raw and terminal -----------------------------------------------------------------

func ansiLines(n *ViewNode) []string {
	if s := n.propString("ansi"); s != "" {
		return strings.Split(s, "\n")
	}
	return toStrings(n.Props["lines"])
}

type cursorProp struct {
	X     int `json:"x"`
	Y     int `json:"y"`
	Shape any `json:"shape"`
}

// nodeCursor returns props.cursor as a buffer.Cursor relative to the node,
// only while the node is the focus target.
func (c *ctx) nodeCursor(rn *rnode) *buffer.Cursor {
	if !rn.isFocus {
		return nil
	}
	cp, ok := propDecode[cursorProp](rn.n, "cursor")
	if !ok {
		return nil
	}
	shape := buffer.CursorBlock
	switch s := cp.Shape.(type) {
	case string:
		switch strings.ToLower(s) {
		case "bar":
			shape = buffer.CursorBar
		case "underline":
			shape = buffer.CursorUnderline
		case "hidden":
			shape = buffer.CursorHidden
		}
	case float64:
		shape = buffer.CursorShape(int(s))
	}
	return &buffer.Cursor{X: cp.X, Y: cp.Y, Shape: shape}
}

func (c *ctx) terminalLines(rn *rnode, w, h int) ([]string, *buffer.Cursor) {
	n := rn.n
	if ls := ansiLines(n); len(ls) > 0 {
		return ls, c.nodeCursor(rn)
	}
	title := "▣  terminal"
	if id := n.propString("streamId"); id != "" {
		title += "  " + id
	}
	if n.propBool("altScreen") {
		title += "  (alt screen)"
	}
	out := make([]string, h)
	mid := h / 2
	for y := range out {
		if y == mid {
			out[y] = spaces(max(0, (w-strW(title))/2)) + c.st.t("muted", title)
		}
	}
	return out, nil
}
