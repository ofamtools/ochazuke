package ui

import (
	"regexp"
	"strings"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/layout"
)

// drawLeaf paints content kinds into the node's inner rect.
func (c *ctx) drawLeaf(rn *rnode, lr layout.Rect) {
	n := rn.n
	if n.Border {
		c.drawFrame(lr, frameOpts{thick: rn.onFocus, border: c.borderStyle(rn), title: n.Title, titleStyle: c.titleStyle(rn), right: c.badgeRow(n.Badges)})
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
		out = c.textLines(n, ir.W)
	case KindMarkdown:
		out = mdLines(c.st, n.propString("markdown"), ir.W)
	case KindTable:
		out = c.tableLines(rn, ir.W, ir.H)
	case KindTree:
		out = c.treeLines(rn, ir.W, ir.H)
	case KindInput:
		out, cur = c.inputLines(rn, ir.W, ir.H)
	case KindForm:
		out, cur = c.formLines(rn, ir.W)
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

func (c *ctx) textLines(n *ViewNode, w int) []string {
	rs := []rune(n.propString("text"))
	spans, _ := propDecode[[]ToneSpan](n, "spans")
	base := c.st.tone(toneOr(n.Tone, "text"))
	rsp := c.st.resolveSpans(spans, base)
	var out []string
	for _, r := range wrapRanges(rs, w) {
		out = append(out, c.st.runs(rs, r[0], r[1], base, rsp))
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

func rowCells(row any, cols []tableCol) []string {
	out := make([]string, len(cols))
	if m, ok := row.(map[string]any); ok {
		for i, col := range cols {
			out[i] = anyString(m[col.key])
		}
		return out
	}
	cells := toSlice(row)
	for i := range cols {
		if i < len(cells) {
			out[i] = anyString(cells[i])
		}
	}
	return out
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
		for i, cell := range rowCells(row, cols) {
			if flexible[i] {
				widths[i] = min(max(widths[i], strW(cell)), 40)
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
	format := func(cells []string) string {
		parts := make([]string, len(cols))
		for i, cell := range cells {
			cell = truncate(cell, widths[i])
			if cols[i].right {
				parts[i] = spaces(widths[i]-strW(cell)) + cell
			} else {
				parts[i] = padPlain(cell, widths[i])
			}
		}
		return strings.Join(parts, "  ")
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
	start := windowStart(n, len(rows), rowsH, sel)
	rule := strings.Repeat("─", w)
	if len(rows) > rowsH && w > 12 {
		pos := " " + itoa(min(len(rows), start+rowsH)) + "/" + itoa(len(rows)) + " "
		rule = strings.Repeat("─", w-strW(pos)-1) + pos + "─"
	}
	out = append(out, c.st.t("border", rule))
	for i := start; i < min(len(rows), start+rowsH); i++ {
		line := format(rowCells(rows[i], cols))
		ts := c.rowStyle(rn, i == sel)
		out = append(out, padLine(c.st.paint(line, ts), w, c.st.sgr(ts)))
	}
	return out
}

func itoa(i int) string { return anyString(float64(i)) }

// --- tree -----------------------------------------------------------------------

type treeNode struct {
	Label    string     `json:"label"`
	Children []treeNode `json:"children"`
	Expanded *bool      `json:"expanded"`
	Tone     string     `json:"tone"`
	Badge    string     `json:"badge"`
	Unread   bool       `json:"unread"`
}

type treeRow struct {
	prefix   string
	marker   string
	label    string
	badge    string
	tone     string
	unread   bool
	hasKids  bool
	expanded bool
}

func treeRows(n *ViewNode) []treeRow {
	nodes, _ := propDecode[[]treeNode](n, "nodes")
	guides := true
	if v, ok := n.prop("guides"); ok {
		if b, isBool := v.(bool); isBool {
			guides = b
		}
	}
	var out []treeRow
	var walk func(ns []treeNode, depth int, cont string)
	walk = func(ns []treeNode, depth int, cont string) {
		for i, nd := range ns {
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
			out = append(out, treeRow{prefix, marker, nd.Label, nd.Badge, nd.Tone, nd.Unread, len(nd.Children) > 0, exp})
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

func (c *ctx) treeLines(rn *rnode, w, h int) []string {
	rows := treeRows(rn.n)
	sel := selectedOf(rn.n)
	start := windowStart(rn.n, len(rows), h, sel)
	var out []string
	for i := start; i < min(len(rows), start+h); i++ {
		r := rows[i]
		badge := ""
		if r.badge != "" {
			badge = " " + r.badge
		}
		if i == sel {
			ts := c.rowStyle(rn, true)
			line := r.prefix + r.marker + r.label + badge
			out = append(out, padLine(c.st.paint(line, ts), w, c.st.sgr(ts)))
			continue
		}
		label := c.st.t(toneOr(r.tone, "text"), r.label)
		if r.unread {
			label = c.st.t("unread", r.label)
		}
		marker := c.st.t("accent", r.marker)
		if !r.hasKids {
			marker = c.st.t("muted", r.marker)
		}
		out = append(out, c.st.t("border", r.prefix)+marker+label+c.st.t("muted", badge))
	}
	return out
}

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

func formFields(n *ViewNode) []core.ParamSpec {
	f, _ := propDecode[[]core.ParamSpec](n, "fields")
	return f
}

func (c *ctx) formLines(rn *rnode, w int) ([]string, *buffer.Cursor) {
	n := rn.n
	fields := formFields(n)
	values, _ := propDecode[map[string]any](n, "values")
	active := 0
	if v, ok := n.prop("active"); ok {
		if i, isInt := anyInt(v); isInt {
			active = clampInt(i, 0, len(fields)-1)
		} else if s, isStr := v.(string); isStr {
			for i, f := range fields {
				if f.Name == s {
					active = i
				}
			}
		}
	}
	lw := 0
	for _, f := range fields {
		lw = max(lw, strW(f.Name)+2)
	}
	var out []string
	var cur *buffer.Cursor
	for i, f := range fields {
		on := rn.onFocus && i == active
		mark := " "
		ls := c.st.tone("muted")
		if on {
			mark, ls = c.st.t("accent", "▸"), mergeTone(c.st.tone("accent"), ToneStyle{Bold: true})
		}
		req := " "
		if f.Required {
			req = c.st.t("error", "*")
		}
		label := mark + " " + c.st.paint(padPlain(f.Name, lw-2), ls) + req + " "
		x0 := 2 + lw + 1
		val, has := values[f.Name]
		if !has {
			val = f.Default
		}
		var body string
		switch {
		case len(f.Enum) > 0:
			parts := make([]string, len(f.Enum))
			for k, opt := range f.Enum {
				if anyString(val) == opt {
					parts[k] = c.st.paint("(•) "+opt, mergeTone(c.st.tone("accent"), ToneStyle{Bold: true}))
				} else {
					parts[k] = c.st.t("muted", "( ) "+opt)
				}
			}
			body = strings.Join(parts, "  ")
		case f.Type == core.ParamBool:
			if b, _ := val.(bool); b {
				body = c.st.t("accent", "[x]")
			} else {
				body = c.st.t("muted", "[ ]")
			}
		default:
			style := c.st.tone("input")
			if on {
				style = c.st.tone("input.focus")
			}
			text := anyString(val)
			if text == "" && !has {
				text = ""
			}
			shown := c.st.paint(" "+text, style)
			if text == "" && f.Description != "" {
				shown = c.st.t("placeholder", " "+f.Description)
			}
			body = padLine(shown, max(0, w-x0), c.st.sgr(style))
			if on && rn.isFocus {
				cur = &buffer.Cursor{X: x0 + 1 + strW(text), Y: i, Shape: buffer.CursorBar}
			}
		}
		out = append(out, label+body)
	}
	submit := n.propString("submitLabel")
	if submit == "" {
		submit = "Submit"
	}
	out = append(out, "  "+c.st.t("key", "⏎")+" "+c.st.t("muted", strings.ToLower(submit)))
	return out, cur
}

// --- picker ------------------------------------------------------------------------

type pickerItem struct{ label, desc, tone, badge string }

func pickerItems(n *ViewNode) []pickerItem {
	var out []pickerItem
	for _, e := range toSlice(n.Props["items"]) {
		switch x := e.(type) {
		case map[string]any:
			out = append(out, pickerItem{
				label: anyString(firstOf(x, "label", "title", "name", "id")),
				desc:  anyString(firstOf(x, "desc", "description")),
				tone:  anyString(x["tone"]),
				badge: anyString(x["badge"]),
			})
		default:
			out = append(out, pickerItem{label: anyString(e)})
		}
	}
	return out
}

// matchRunes marks the runes of label matched by query under mode.
func matchRunes(label, query, mode string) []bool {
	rs := []rune(label)
	hit := make([]bool, len(rs))
	if query == "" {
		return hit
	}
	switch mode {
	case "regex":
		re, err := regexp.Compile("(?i)" + query)
		if err != nil {
			return hit
		}
		if loc := re.FindStringIndex(label); loc != nil {
			from := len([]rune(label[:loc[0]]))
			to := from + len([]rune(label[loc[0]:loc[1]]))
			for i := from; i < to; i++ {
				hit[i] = true
			}
		}
	case "fuzzy":
		q := []rune(strings.ToLower(query))
		qi := 0
		for i, r := range []rune(strings.ToLower(label)) {
			if qi < len(q) && r == q[qi] {
				hit[i] = true
				qi++
			}
		}
		if qi < len(q) {
			return make([]bool, len(rs))
		}
	default:
		low := []rune(strings.ToLower(label))
		q := []rune(strings.ToLower(query))
		if len(low) == len(rs) {
			if i := indexRunes(low, 0, string(q)); i >= 0 {
				for k := range q {
					hit[i+k] = true
				}
			}
		}
	}
	return hit
}

func (c *ctx) pickerLines(rn *rnode, w, h int) ([]string, *buffer.Cursor) {
	n := rn.n
	query := n.propString("query")
	items := pickerItems(n)
	style := c.st.tone("input")
	if rn.onFocus {
		style = c.st.tone("input.focus")
	}
	count := itoa(len(items)) + " "
	prompt := c.st.t("accent", " ❯ ") + c.st.paint(query, style)
	if query == "" && n.propString("placeholder") != "" {
		prompt = c.st.t("accent", " ❯ ") + c.st.t("placeholder", n.propString("placeholder"))
	}
	gap := w - strW(prompt) - strW(count)
	head := prompt + c.st.paint(spaces(gap), style) + c.st.paint(count, mergeTone(style, c.st.tone("muted")))
	out := []string{padLine(head, w, c.st.sgr(style))}
	rowsH := h - 1
	sel := selectedOf(n)
	start := windowStart(n, len(items), rowsH, sel)
	mode := n.propString("matchMode")
	for i := start; i < min(len(items), start+rowsH); i++ {
		it := items[i]
		ts := c.rowStyle(rn, i == sel)
		hit := matchRunes(it.label, query, mode)
		var spans []rspan
		for k, ok := range hit {
			if ok {
				spans = append(spans, rspan{k, k + 1, mergeTone(ts, c.st.tone("match"))})
			}
		}
		rs := []rune(it.label)
		line := c.st.paint("  ", ts) + c.st.runs(rs, 0, len(rs), ts, spans)
		if it.desc != "" {
			ds := ts
			if i != sel {
				ds = c.st.tone("muted")
			} else {
				ds.Dim = true
			}
			line += c.st.paint("  "+it.desc, ds)
		}
		if it.badge != "" {
			line += c.st.paint(" "+it.badge, ts)
		}
		out = append(out, padLine(line, w, c.st.sgr(ts)))
	}
	return out, c.cursorIf(rn, 3+strW(query), 0, buffer.CursorBar)
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
