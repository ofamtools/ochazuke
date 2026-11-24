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
		out = mdLines(c.st, n.propString("markdown"), ir.W)
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
	rawText := n.propString("text")
	spans, _ := propDecode[[]ToneSpan](n, "spans")
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

// mdLines renders basic Markdown (headings, lists, quotes, rules, fenced code,
// and **bold**, *em*, `code`, [links](url)). st may be nil for plain output.
func mdLines(st *styler, text string, w int) []string {
	w = max(w, 1)
	var out []string
	var para []string
	blank := func() {
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
	}
	flush := func() {
		if len(para) > 0 {
			out = append(out, mdInline(st, strings.Join(para, " "), w, "", "")...)
			para = nil
		}
	}
	rule := func() string { return st.t("border", strings.Repeat("─", w)) }
	inCode := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		trim := strings.TrimLeft(line, " ")
		indent := (len(line) - len(trim)) / 2 * 2
		switch {
		case strings.HasPrefix(trim, "```"):
			flush()
			inCode = !inCode
		case inCode:
			ts := st.tone("md.code")
			out = append(out, padLine(st.paint(" "+line, ts), w, st.sgr(ts)))
		case trim == "":
			flush()
			blank()
		case strings.Trim(trim, "-*_") == "" && len(trim) >= 3:
			flush()
			out = append(out, rule())
		default:
			if lvl, txt, ok := headingOf(trim); ok {
				flush()
				blank()
				hs := st.tone("heading." + string(rune('0'+min(lvl, 3))))
				for _, ln := range wrapPlain(txt, w) {
					out = append(out, st.paint(ln, hs))
				}
				if lvl == 1 {
					out = append(out, rule())
				}
				continue
			}
			if strings.HasPrefix(trim, "> ") {
				flush()
				q := st.t("md.quote", "│ ")
				out = append(out, mdInline(st, trim[2:], max(1, w-2), q, q)...)
				continue
			}
			marker, body := "", ""
			switch {
			case len(trim) > 2 && (trim[0] == '-' || trim[0] == '*' || trim[0] == '+') && trim[1] == ' ':
				marker, body = "• ", trim[2:]
			case orderedRe.MatchString(trim):
				loc := orderedRe.FindString(trim)
				marker, body = loc, trim[len(loc):]
			}
			if marker != "" {
				flush()
				mw := strW(marker)
				first := spaces(indent) + st.t("md.bullet", marker)
				rest := spaces(indent + mw)
				out = append(out, mdInline(st, body, max(1, w-indent-mw), first, rest)...)
				continue
			}
			para = append(para, trim)
		}
	}
	flush()
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// mdInline parses inline Markdown into spans, wraps to w, and prefixes lines.
func mdInline(st *styler, text string, w int, first, rest string) []string {
	var rs []rune
	var spans []ToneSpan
	src := []rune(text)
	for i := 0; i < len(src); {
		closer, tone, skip := rune(0), "", 1
		switch {
		case src[i] == '`':
			closer, tone = '`', "md.code"
		case src[i] == '*' && i+1 < len(src) && src[i+1] == '*':
			closer, tone, skip = '*', "md.bold", 2
		case (src[i] == '*' || src[i] == '_') && i+1 < len(src) && src[i+1] != ' ':
			closer, tone = src[i], "md.em"
		case src[i] == '[':
			if j := indexRunes(src, i+1, "]("); j > 0 {
				if k := indexRunes(src, j+2, ")"); k > 0 {
					from := len(rs)
					rs = append(rs, src[i+1:j]...)
					spans = append(spans, ToneSpan{From: from, To: len(rs), Tone: "link"})
					i = k + 1
					continue
				}
			}
		}
		if closer != 0 {
			end := -1
			for j := i + skip; j < len(src); j++ {
				if src[j] == closer && (skip == 1 || (j+1 < len(src) && src[j+1] == closer)) {
					end = j
					break
				}
			}
			if end > i+skip {
				from := len(rs)
				rs = append(rs, src[i+skip:end]...)
				spans = append(spans, ToneSpan{From: from, To: len(rs), Tone: tone})
				i = end + skip
				continue
			}
		}
		rs = append(rs, src[i])
		i++
	}
	base := st.tone("text")
	rsp := st.resolveSpans(spans, base)
	var out []string
	for n, r := range wrapRanges(rs, w) {
		prefix := rest
		if n == 0 {
			prefix = first
		}
		out = append(out, prefix+st.runs(rs, r[0], r[1], base, rsp))
	}
	return out
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
// per-cell tones, per-cell leading icons, and per-cell icon tones.
type TableRowSpec struct {
	Cells     []any    `json:"cells"`
	Tone      string   `json:"tone,omitempty"`
	CellTones []string `json:"cellTones,omitempty"`
	CellIcons []string `json:"cellIcons,omitempty"`
	IconTones []string `json:"iconTones,omitempty"`
}

type parsedRow struct {
	cells     []string
	tone      string
	cellTones []string
	cellIcons []string
	iconTones []string
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

func sliceAt(ss []string, i int) string {
	if i >= 0 && i < len(ss) {
		return ss[i]
	}
	return ""
}

func parseTableRow(row any, cols []tableCol) parsedRow {
	out := make([]string, len(cols))
	switch x := row.(type) {
	case TableRowSpec:
		for i := range cols {
			if i < len(x.Cells) {
				out[i] = anyString(x.Cells[i])
			}
		}
		return parsedRow{
			cells:     out,
			tone:      x.Tone,
			cellTones: x.CellTones,
			cellIcons: x.CellIcons,
			iconTones: x.IconTones,
			rich:      x.Tone != "" || hasAnyNonEmpty(x.CellTones) || hasAnyNonEmpty(x.CellIcons) || hasAnyNonEmpty(x.IconTones),
		}
	case *TableRowSpec:
		if x == nil {
			return parsedRow{cells: out}
		}
		for i := range cols {
			if i < len(x.Cells) {
				out[i] = anyString(x.Cells[i])
			}
		}
		return parsedRow{
			cells:     out,
			tone:      x.Tone,
			cellTones: x.CellTones,
			cellIcons: x.CellIcons,
			iconTones: x.IconTones,
			rich:      x.Tone != "" || hasAnyNonEmpty(x.CellTones) || hasAnyNonEmpty(x.CellIcons) || hasAnyNonEmpty(x.IconTones),
		}
	case []any:
		for i := range cols {
			if i < len(x) {
				out[i] = anyString(x[i])
			}
		}
		return parsedRow{cells: out}
	case map[string]any:
		if rawCells, ok := x["cells"]; ok {
			if cs := toSlice(rawCells); cs != nil {
				for i := range cols {
					if i < len(cs) {
						out[i] = anyString(cs[i])
					}
				}
				tone := anyString(x["tone"])
				cellTones := toStrings(x["cellTones"])
				cellIcons := toStrings(x["cellIcons"])
				iconTones := toStrings(x["iconTones"])
				return parsedRow{
					cells:     out,
					tone:      tone,
					cellTones: cellTones,
					cellIcons: cellIcons,
					iconTones: iconTones,
					rich:      tone != "" || hasAnyNonEmpty(cellTones) || hasAnyNonEmpty(cellIcons) || hasAnyNonEmpty(iconTones),
				}
			}
		}
		for i, col := range cols {
			out[i] = anyString(x[col.key])
		}
		return parsedRow{cells: out}
	default:
		cells := toSlice(row)
		for i := range cols {
			if i < len(cells) {
				out[i] = anyString(cells[i])
			}
		}
		return parsedRow{cells: out}
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

		if icon == "" {
			txt := truncate(cell, cw)
			pad := cw - strW(txt)
			if cols[ci].right {
				if pad > 0 {
					sb.WriteString(c.st.paint(spaces(pad), cellPadTS))
				}
				sb.WriteString(c.st.paint(txt, cellTS))
			} else {
				sb.WriteString(c.st.paint(txt, cellTS))
				if pad > 0 {
					sb.WriteString(c.st.paint(spaces(pad), cellPadTS))
				}
			}
			continue
		}

		iw := strW(icon)
		iconPart := icon
		txt := ""
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
				txt = truncate(cell, rem)
				pad = cw - iw - 1 - strW(txt)
			}
		}

		if cols[ci].right {
			if pad > 0 {
				sb.WriteString(c.st.paint(spaces(pad), cellPadTS))
			}
			sb.WriteString(c.st.paint(iconPart, iconTS))
			if txt != "" {
				sb.WriteString(c.st.paint(" ", cellPadTS))
				sb.WriteString(c.st.paint(txt, cellTS))
			}
		} else {
			sb.WriteString(c.st.paint(iconPart, iconTS))
			if txt != "" {
				sb.WriteString(c.st.paint(" ", cellPadTS))
				sb.WriteString(c.st.paint(txt, cellTS))
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
	for i := start; i < min(len(rows), start+rowsH); i++ {
		pr := parseTableRow(rows[i], cols)
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
		if value == "" && ph != "" {
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
