package ui

import "strings"

type mdTableAlign uint8

const (
	mdAlignLeft mdTableAlign = iota
	mdAlignCenter
	mdAlignRight
)

type mdTable struct {
	headers []string
	aligns  []mdTableAlign
	rows    [][]string
}

// parseMarkdownTable checks whether lines[start:] begins with a GFM Markdown
// table (header row + delimiter row + 0 or more body rows). It returns the
// parsed table, the count of source lines consumed, and whether a table matched.
func parseMarkdownTable(lines []string, start int) (mdTable, int, bool) {
	if start+1 >= len(lines) {
		return mdTable{}, 0, false
	}
	headTrim := strings.TrimSpace(lines[start])
	sepTrim := strings.TrimSpace(lines[start+1])
	if headTrim == "" || sepTrim == "" || !hasTablePipe(headTrim) || !hasTablePipe(sepTrim) {
		return mdTable{}, 0, false
	}
	aligns, ok := parseTableAligns(sepTrim)
	if !ok {
		return mdTable{}, 0, false
	}
	headers := splitTableCells(headTrim)
	if len(headers) != len(aligns) {
		return mdTable{}, 0, false
	}
	tbl := mdTable{
		headers: headers,
		aligns:  aligns,
	}
	consumed := 2
	for j := start + 2; j < len(lines); j++ {
		trim := strings.TrimSpace(lines[j])
		if trim == "" || strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "> ") {
			break
		}
		if strings.Trim(trim, "-*_") == "" && len(trim) >= 3 {
			break
		}
		if _, _, isHeading := headingOf(trim); isHeading {
			break
		}
		if !hasTablePipe(trim) {
			break
		}
		tbl.rows = append(tbl.rows, splitTableCells(trim))
		consumed++
	}
	return tbl, consumed, true
}

// hasTablePipe reports whether line contains at least one unescaped '|' outside
// inline code spans.
func hasTablePipe(line string) bool {
	rs := []rune(line)
	for i := 0; i < len(rs); {
		if rs[i] == '\\' && i+1 < len(rs) && rs[i+1] == '|' {
			i += 2
			continue
		}
		if rs[i] == '`' {
			if end := findClosingBacktick(rs, i+1); end > i+1 {
				i = end + 1
				continue
			}
		}
		if rs[i] == '|' {
			return true
		}
		i++
	}
	return false
}

// splitTableCells splits a GFM table row into trimmed cell strings, respecting
// escaped pipes (\|) and inline code spans (`a|b`).
func splitTableCells(line string) []string {
	rs := []rune(strings.TrimSpace(line))
	if len(rs) == 0 {
		return nil
	}
	var cells []string
	var cur strings.Builder
	startedWithPipe := false
	endedWithPipe := false
	for i := 0; i < len(rs); {
		if rs[i] == '\\' && i+1 < len(rs) && rs[i+1] == '|' {
			cur.WriteRune('|')
			i += 2
			continue
		}
		if rs[i] == '`' {
			if end := findClosingBacktick(rs, i+1); end > i+1 {
				// Copy code span, unescaping \| inside code per GFM table rules.
				cur.WriteRune('`')
				for k := i + 1; k < end; {
					if rs[k] == '\\' && k+1 < end && rs[k+1] == '|' {
						cur.WriteRune('|')
						k += 2
						continue
					}
					cur.WriteRune(rs[k])
					k++
				}
				cur.WriteRune('`')
				i = end + 1
				continue
			}
		}
		if rs[i] == '|' {
			if i == 0 {
				startedWithPipe = true
			}
			if i == len(rs)-1 {
				endedWithPipe = true
			}
			cells = append(cells, cur.String())
			cur.Reset()
			i++
			continue
		}
		cur.WriteRune(rs[i])
		i++
	}
	cells = append(cells, cur.String())
	if startedWithPipe && len(cells) > 0 {
		cells = cells[1:]
	}
	if endedWithPipe && len(cells) > 0 {
		cells = cells[:len(cells)-1]
	}
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// parseTableAligns parses a GFM delimiter row (e.g. "| :--- | :---: | ---: |").
func parseTableAligns(sepLine string) ([]mdTableAlign, bool) {
	cells := splitTableCells(sepLine)
	if len(cells) == 0 {
		return nil, false
	}
	aligns := make([]mdTableAlign, len(cells))
	for i, cell := range cells {
		if cell == "" {
			return nil, false
		}
		leftColon := strings.HasPrefix(cell, ":")
		rightColon := strings.HasSuffix(cell, ":")
		body := cell
		if leftColon {
			body = body[1:]
		}
		if rightColon && len(body) > 0 {
			body = body[:len(body)-1]
		}
		if len(body) == 0 || strings.Trim(body, "-") != "" {
			return nil, false
		}
		switch {
		case leftColon && rightColon:
			aligns[i] = mdAlignCenter
		case rightColon:
			aligns[i] = mdAlignRight
		default:
			aligns[i] = mdAlignLeft
		}
	}
	return aligns, true
}

// renderMarkdownTable renders a parsed GFM table into aligned, styled lines
// fitting cleanly within width w. Inline Markdown inside every cell is parsed
// in source order so MarkdownLinks indices stay exact.
func renderMarkdownTable(st *styler, tbl mdTable, w int, state *mdRenderState) []string {
	numCols := len(tbl.aligns)
	if numCols == 0 {
		return nil
	}
	w = max(w, 1)

	headerBase := st.tone("table.header")
	if headerBase == (ToneStyle{}) {
		headerBase = mergeTone(st.tone("text"), ToneStyle{Bold: true})
	}
	bodyBase := st.tone("text")

	parsedHeaders := make([]mdParsedInline, numCols)
	nat := make([]int, numCols)
	for c := 0; c < numCols; c++ {
		parsedHeaders[c] = parseMdInline(st, tbl.headers[c], headerBase, state)
		nat[c] = max(1, runesW(parsedHeaders[c].rs))
	}

	parsedRows := make([][]mdParsedInline, len(tbl.rows))
	for r, row := range tbl.rows {
		pr := make([]mdParsedInline, numCols)
		for c := 0; c < numCols; c++ {
			cellText := ""
			if c < len(row) {
				cellText = row[c]
			}
			pr[c] = parseMdInline(st, cellText, bodyBase, state)
			nat[c] = max(nat[c], runesW(pr[c].rs))
		}
		// Consume link indices on any excess cells so MarkdownLinks order never drifts.
		for c := numCols; c < len(row); c++ {
			parseMdInline(st, row[c], bodyBase, state)
		}
		parsedRows[r] = pr
	}

	colSep, ruleSep, sepW := " │ ", "─┼─", 3
	if w < numCols*3+(numCols-1)*3 {
		colSep, ruleSep, sepW = " ", "─", 1
	}
	if w < numCols+(numCols-1) {
		colSep, ruleSep, sepW = "", "", 0
	}

	widths := computeTableColWidths(nat, w, sepW)
	visCols := 0
	for _, cw := range widths {
		if cw > 0 {
			visCols++
		}
	}
	if visCols == 0 {
		return nil
	}

	var out []string
	out = append(out, renderTableRowLines(st, parsedHeaders[:visCols], tbl.aligns[:visCols], widths[:visCols], colSep)...)

	var rule strings.Builder
	for c := 0; c < visCols; c++ {
		if c > 0 {
			rule.WriteString(ruleSep)
		}
		rule.WriteString(strings.Repeat("─", widths[c]))
	}
	out = append(out, st.t("border", rule.String()))

	for _, pr := range parsedRows {
		out = append(out, renderTableRowLines(st, pr[:visCols], tbl.aligns[:visCols], widths[:visCols], colSep)...)
	}
	return out
}

// computeTableColWidths allocates column widths fitting inside total width w
// with separator width sepW, shrinking the widest columns first.
func computeTableColWidths(nat []int, w, sepW int) []int {
	n := len(nat)
	widths := make([]int, n)
	if n == 0 || w <= 0 {
		return widths
	}
	avail := w - sepW*(n-1)
	if avail < n {
		// Extreme narrow viewport: fit as many 1-cell columns as possible.
		vis := min(n, max(1, w))
		for c := 0; c < vis; c++ {
			widths[c] = 1
		}
		return widths
	}
	total := 0
	for c, cw := range nat {
		widths[c] = max(1, cw)
		total += widths[c]
	}
	for total > avail {
		widest := -1
		for c, cw := range widths {
			if cw > 1 && (widest < 0 || cw > widths[widest]) {
				widest = c
			}
		}
		if widest < 0 {
			break
		}
		widths[widest]--
		total--
	}
	return widths
}

// renderTableRowLines wraps and aligns one logical table row across columns.
func renderTableRowLines(st *styler, cells []mdParsedInline, aligns []mdTableAlign, widths []int, colSep string) []string {
	n := len(cells)
	wrapped := make([][][2]int, n)
	rowH := 1
	for c := 0; c < n; c++ {
		wrapped[c] = wrapRanges(cells[c].rs, widths[c])
		if len(wrapped[c]) > rowH {
			rowH = len(wrapped[c])
		}
	}
	sepStyled := st.t("border", colSep)
	lines := make([]string, rowH)
	for sub := 0; sub < rowH; sub++ {
		var sb strings.Builder
		for c := 0; c < n; c++ {
			if c > 0 {
				sb.WriteString(sepStyled)
			}
			cw := widths[c]
			if sub >= len(wrapped[c]) {
				sb.WriteString(spaces(cw))
				continue
			}
			rng := wrapped[c][sub]
			segW := runesW(cells[c].rs[rng[0]:rng[1]])
			styled := st.runs(cells[c].rs, rng[0], rng[1], cells[c].base, cells[c].rsp)
			pad := max(0, cw-segW)
			switch aligns[c] {
			case mdAlignRight:
				sb.WriteString(spaces(pad))
				sb.WriteString(styled)
			case mdAlignCenter:
				left := pad / 2
				sb.WriteString(spaces(left))
				sb.WriteString(styled)
				sb.WriteString(spaces(pad - left))
			default:
				sb.WriteString(styled)
				sb.WriteString(spaces(pad))
			}
		}
		lines[sub] = sb.String()
	}
	return lines
}
