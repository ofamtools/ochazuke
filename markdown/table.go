package markdown

import (
	"bytes"
	"html"
	"slices"
	"strings"

	gansi "charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	gast "github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Tables are drawn here, not by Glamour. Glamour stretches every table to
// the full width, splits the room evenly between columns whatever they
// hold, breaks words in the middle, and counts the Doc's element markers
// as cells, so a row's first line drew one column left of its other lines
// and of the header rule's junctions. Each table is cut out of the source
// before Glamour runs, a placeholder line stands in its place, and the
// table drawn by drawTable replaces the placeholder's line.

// tableMarker+i is the placeholder of the source's i-th table: a private
// use rune of plane 16, apart from the element markers of plane 15.
const tableMarker = rune(0x100000)

// srcTable is one table cut out of a source.
type srcTable struct {
	node *east.Table
	src  []byte
}

// cutTables replaces each table in text with a line holding its
// placeholder rune, where the table's first cell started, so a table in a
// list item or a quote stays there.
func cutTables(source string) (string, []srcTable) {
	src := []byte(source)
	doc := parser.Parser().Parse(text.NewReader(src))
	type cut struct{ from, to int }
	var tables []srcTable
	var cuts []cut
	_ = gast.Walk(doc, func(n gast.Node, entering bool) (gast.WalkStatus, error) {
		t, ok := n.(*east.Table)
		if !entering || !ok {
			return gast.WalkContinue, nil
		}
		from, to, ok := tableRange(src, t)
		if ok && (len(cuts) == 0 || from >= cuts[len(cuts)-1].to) {
			cuts = append(cuts, cut{from, to})
			tables = append(tables, srcTable{node: t, src: src})
		}
		return gast.WalkSkipChildren, nil
	})
	if len(cuts) == 0 {
		return source, nil
	}
	var b strings.Builder
	at := 0
	for i, c := range cuts {
		b.Write(src[at:c.from])
		b.WriteRune(tableMarker + rune(i))
		at = c.to
	}
	b.Write(src[at:])
	return b.String(), tables
}

// tableRange is the bytes a table covers: from its header's first cell
// (back over its leading pipe) to the end of its last row's line. A GFM
// table is one line per row and the delimiter line under the header.
func tableRange(src []byte, t *east.Table) (from, to int, ok bool) {
	first := -1
	rows := 0
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		rows++
		for cell := row.FirstChild(); cell != nil && first < 0 && rows == 1; cell = cell.NextSibling() {
			if l := cell.Lines(); l.Len() > 0 {
				first = l.At(0).Start
			}
		}
	}
	if first < 0 || rows == 0 {
		return 0, 0, false
	}
	from = first
	for from > 0 && (src[from-1] == ' ' || src[from-1] == '\t') {
		from--
	}
	if from > 0 && src[from-1] == '|' {
		from--
	}
	to = from
	for range rows + 1 {
		nl := bytes.IndexByte(src[to:], '\n')
		if nl < 0 {
			return from, len(src), true
		}
		to += nl + 1
	}
	return from, to - 1, true
}

// spliceTables replaces each placeholder line Glamour drew with its table.
// The text before the placeholder is the table's indent when it is only
// spaces or a quote's bar; other text before it keeps a line of its own.
// rule colours the rules (#rrggbb, "" for none: unstyled).
func spliceTables(lines []string, tables []srcTable, width int, style, rule string) []string {
	if len(tables) == 0 {
		return lines
	}
	cfg := styleConfig(style)
	margin := 0
	if cfg.Document.Margin != nil {
		margin = int(*cfg.Document.Margin)
	}
	var out []string
	for _, line := range lines {
		at, k := -1, -1
		for i, r := range line {
			if r >= tableMarker && r < tableMarker+rune(len(tables)) {
				at, k = i, int(r-tableMarker)
				break
			}
		}
		if at < 0 {
			out = append(out, line)
			continue
		}
		before := line[:at]
		after := strings.TrimSpace(ansi.Strip(line[at+len(string(tableMarker+rune(k))):]))
		plain := ansi.Strip(before)
		prefix := before
		if strings.Trim(plain, " │") != "" {
			out = append(out, before)
			prefix = strings.Repeat(" ", len(plain)-len(strings.TrimLeft(plain, " ")))
		}
		avail := width - ansi.StringWidth(prefix) - margin
		for _, tl := range drawTable(tables[k], avail, cfg, rule) {
			out = append(out, prefix+tl)
		}
		if after != "" {
			out = append(out, prefix+after)
		}
	}
	return out
}

// glyph is one grapheme of a cell: its text, its display width, its style
// and whether a line may break at it (a plain space).
type glyph struct {
	text  string
	width int
	style int
	space bool
}

// cellText is a cell's glyphs and the element markers it held, which are
// drawn at the end of the row's first (start markers) or last line.
type cellText struct {
	glyphs     []glyph
	starts     []rune
	ends       []rune
	widthTotal int
}

// inline turns a cell's inline Markdown into glyphs in Glamour's styles.
type inline struct {
	src    []byte
	cfg    gansi.StyleConfig
	styles []lipgloss.Style
	keys   map[string]int
	cell   *cellText
}

func (in *inline) styleIndex(s lipgloss.Style) int {
	key := s.Render("\x00")
	if i, ok := in.keys[key]; ok {
		return i
	}
	in.styles = append(in.styles, s)
	in.keys[key] = len(in.styles) - 1
	return len(in.styles) - 1
}

func (in *inline) add(s string, st lipgloss.Style) {
	idx := in.styleIndex(st)
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		t := g.Str()
		if r := []rune(t); len(r) == 1 && isElementMarker(r[0]) {
			if r[0] >= endMarker {
				in.cell.ends = append(in.cell.ends, r[0])
			} else {
				in.cell.starts = append(in.cell.starts, r[0])
			}
			continue
		}
		if t == "\n" || t == "\t" {
			t = " "
		}
		w := ansi.StringWidth(t)
		in.cell.glyphs = append(in.cell.glyphs, glyph{text: t, width: w, style: idx, space: t == " "})
		in.cell.widthTotal += w
	}
}

func isElementMarker(r rune) bool { return r >= startMarker && r < endMarker+maxMarkers }

func (in *inline) node(n gast.Node, st lipgloss.Style) {
	switch n := n.(type) {
	case *gast.Text:
		s := string(n.Segment.Value(in.src))
		if n.SoftLineBreak() || n.HardLineBreak() {
			s += " "
		}
		in.add(html.UnescapeString(s), st)
	case *gast.String:
		in.add(html.UnescapeString(string(n.Value)), st)
	case *gast.CodeSpan:
		code := in.cfg.Code.StylePrimitive
		var b strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if t, ok := c.(*gast.Text); ok {
				b.Write(t.Segment.Value(in.src))
			}
		}
		in.add(code.Prefix+html.UnescapeString(b.String())+code.Suffix, primitive(st, code))
	case *gast.Emphasis:
		p := in.cfg.Emph
		if n.Level >= 2 {
			p = in.cfg.Strong
		}
		in.children(n, primitive(st, p))
	case *east.Strikethrough:
		in.children(n, primitive(st, in.cfg.Strikethrough))
	case *gast.Link:
		in.children(n, primitive(st, in.cfg.LinkText))
		if url := string(n.Destination); url != "" && url != string(n.Text(in.src)) { //nolint:staticcheck
			in.add(" "+url, primitive(st, in.cfg.Link))
		}
	case *gast.AutoLink:
		in.add(string(n.URL(in.src)), primitive(st, in.cfg.Link))
	case *gast.Image:
		in.add(string(n.Text(in.src)), primitive(st, in.cfg.ImageText)) //nolint:staticcheck
	case *gast.RawHTML:
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			in.add(string(seg.Value(in.src)), st)
		}
	default:
		if n.HasChildren() {
			in.children(n, st)
		}
	}
}

func (in *inline) children(n gast.Node, st lipgloss.Style) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		in.node(c, st)
	}
}

// primitive is st with a Glamour style primitive's attributes over it.
func primitive(st lipgloss.Style, p gansi.StylePrimitive) lipgloss.Style {
	if p.Color != nil {
		st = st.Foreground(lipgloss.Color(*p.Color))
	}
	if p.BackgroundColor != nil {
		st = st.Background(lipgloss.Color(*p.BackgroundColor))
	}
	flag := func(b *bool, set func(bool) lipgloss.Style) {
		if b != nil {
			st = set(*b)
		}
	}
	flag(p.Bold, st.Bold)
	flag(p.Italic, st.Italic)
	flag(p.Underline, st.Underline)
	flag(p.CrossedOut, st.Strikethrough)
	flag(p.Faint, st.Faint)
	flag(p.Inverse, st.Reverse)
	flag(p.Blink, st.Blink)
	return st
}

// drawTable draws a table no wider than avail: each column as wide as
// its widest cell when they all fit, else narrowed in proportion to how
// much each wants beyond a floor (fitColumns), its cells wrapped at
// spaces. Columns are a space either side and │ between them, the header
// under a ─ rule with ┼ at the columns, as Glamour draws them; each row's
// element markers end its first and last lines.
func drawTable(t srcTable, avail int, cfg gansi.StyleConfig, rule string) []string {
	base := primitive(primitive(lipgloss.NewStyle(), cfg.Document.StylePrimitive), cfg.Table.StylePrimitive)
	in := &inline{src: t.src, cfg: cfg, keys: map[string]int{}}
	var rows [][]cellText
	for row := t.node.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []cellText
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, cellText{})
			in.cell = &cells[len(cells)-1]
			in.children(cell, primitive(base, cfg.Text))
			trimSpaces(in.cell)
		}
		rows = append(rows, cells)
	}
	cols := len(t.node.Alignments)
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	if cols == 0 {
		return nil
	}
	natural, word := make([]int, cols), make([]int, cols)
	for _, r := range rows {
		for c, cell := range r {
			natural[c] = max(natural[c], cell.widthTotal)
			word[c] = max(word[c], longestWord(cell.glyphs))
		}
	}
	// a space either side of each cell and a separator between columns
	widths := fitColumns(natural, word, avail-3*cols+1)

	lines := make([][][]string, len(rows))
	heights := make([]int, len(rows))
	for i, r := range rows {
		lines[i] = make([][]string, cols)
		for c := range cols {
			var cell cellText
			if c < len(r) {
				cell = r[c]
			}
			lines[i][c] = wrapCell(cell.glyphs, widths[c], in.styles)
			heights[i] = max(heights[i], len(lines[i][c]))
		}
	}
	ruleColor := rule
	sep, rule, cross := "│", "─", "┼"
	if r := cfg.Table; r.RowSeparator != nil && r.ColumnSeparator != nil && r.CenterSeparator != nil {
		sep, rule, cross = *r.ColumnSeparator, *r.RowSeparator, *r.CenterSeparator
	}
	paint := func(s string) string { return s }
	if ruleColor != "" {
		st := lipgloss.NewStyle().Foreground(lipgloss.Color(ruleColor))
		paint = func(s string) string { return st.Render(s) }
	}
	var out []string
	for i, r := range rows {
		var starts, ends []rune
		for _, cell := range r {
			starts = append(starts, cell.starts...)
			ends = append(ends, cell.ends...)
		}
		for l := range heights[i] {
			parts := make([]string, cols)
			for c := range cols {
				text := ""
				if l < len(lines[i][c]) {
					text = lines[i][c][l]
				}
				parts[c] = " " + align(text, widths[c], alignOf(t.node, c)) + " "
			}
			line := strings.Join(parts, paint(sep))
			if l == 0 {
				line += string(starts)
			}
			if l == heights[i]-1 {
				line += string(ends)
			}
			out = append(out, line)
		}
		if i == 0 {
			parts := make([]string, cols)
			for c := range cols {
				parts[c] = strings.Repeat(rule, widths[c]+2)
			}
			out = append(out, paint(strings.Join(parts, cross)))
		}
	}
	return out
}

func alignOf(t *east.Table, col int) east.Alignment {
	if col < len(t.Alignments) {
		return t.Alignments[col]
	}
	return east.AlignNone
}

// align pads s to width display cells: on the right, or the left for a
// right-aligned column, or both for a centred one.
func align(s string, width int, a east.Alignment) string {
	pad := max(0, width-ansi.StringWidth(s))
	switch a {
	case east.AlignRight:
		return strings.Repeat(" ", pad) + s
	case east.AlignCenter:
		return strings.Repeat(" ", pad/2) + s + strings.Repeat(" ", pad-pad/2)
	}
	return s + strings.Repeat(" ", pad)
}

// trimSpaces drops a cell's leading and trailing plain spaces.
func trimSpaces(c *cellText) {
	for len(c.glyphs) > 0 && c.glyphs[0].space {
		c.widthTotal -= c.glyphs[0].width
		c.glyphs = c.glyphs[1:]
	}
	for len(c.glyphs) > 0 && c.glyphs[len(c.glyphs)-1].space {
		c.widthTotal -= c.glyphs[len(c.glyphs)-1].width
		c.glyphs = c.glyphs[:len(c.glyphs)-1]
	}
}

// minColumn is the least a column narrows to, as the chooser's
// fitWidths: below it a column holds too little to read. A column whose
// longest word is wider keeps that word whole, up to maxWordColumn, while
// the floors fit.
const (
	minColumn     = 6
	maxWordColumn = 12
)

// longestWord is the display width of the widest run of glyphs between
// plain spaces.
func longestWord(glyphs []glyph) int {
	best, w := 0, 0
	for _, g := range glyphs {
		if g.space {
			w = 0
			continue
		}
		w += g.width
		best = max(best, w)
	}
	return best
}

// fitColumns is each column's width: its natural width when they all fit
// in avail; else each keeps a floor (its longest word up to
// maxWordColumn, at least minColumn, at most its natural width; just
// min(natural, minColumn) when those floors do not fit) and the room left
// is shared in proportion to what each wants beyond its floor, the
// remainders going to the largest fractions first. With no room even for
// the floors the columns stay at them and the lines are cut at the width.
func fitColumns(natural, word []int, avail int) []int {
	total := 0
	for _, n := range natural {
		total += n
	}
	out := make([]int, len(natural))
	if total <= avail {
		for i, n := range natural {
			out[i] = max(n, 1)
		}
		return out
	}
	floors := func(least func(i int) int) int {
		sum := 0
		for i, n := range natural {
			out[i] = max(1, min(n, least(i)))
			sum += out[i]
		}
		return sum
	}
	floor := floors(func(i int) int { return max(minColumn, min(word[i], maxWordColumn)) })
	if floor > avail {
		floor = floors(func(int) int { return minColumn })
	}
	want := 0
	for i, n := range natural {
		want += n - out[i]
	}
	room := avail - floor
	if room <= 0 || want == 0 {
		return out
	}
	frac := make([]int, len(natural))
	given := 0
	for i, n := range natural {
		extra := n - out[i]
		out[i] += extra * room / want
		given += extra * room / want
		frac[i] = extra * room % want
	}
	for ; given < room; given++ {
		best := -1
		for i := range frac {
			if natural[i] > out[i] && frac[i] >= 0 && (best < 0 || frac[i] > frac[best]) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		out[best]++
		frac[best] = -1
	}
	return out
}

// blank is whether a line holds only spaces (a code span's no-break
// edge left alone at a break), which a space after it clears.
func blank(line []glyph) bool {
	for _, g := range line {
		if strings.TrimSpace(strings.ReplaceAll(g.text, "\u00a0", " ")) != "" {
			return false
		}
	}
	return true
}

// breakAt is where a word too long for its line breaks: after its last
// - or / or where its style changes (a code span's edge), past the word's
// start; else at the line's end.
func breakAt(line []glyph, word int) int {
	for p := len(line) - 1; p > word; p-- {
		if t := line[p-1].text; t == "-" || t == "/" || line[p-1].style != line[p].style {
			return p
		}
	}
	return len(line)
}

// wrapCell wraps glyphs into lines at most width wide, breaking at plain
// spaces, a word longer than the width broken by breakAt. Each
// line is styled on its own, so a code span's background stops at the
// line's end and starts again on the next.
func wrapCell(glyphs []glyph, width int, styles []lipgloss.Style) []string {
	width = max(1, width)
	var lines [][]glyph
	var cur []glyph
	curW := 0
	flush := func() {
		for len(cur) > 0 && cur[len(cur)-1].space {
			curW -= cur[len(cur)-1].width
			cur = cur[:len(cur)-1]
		}
		lines = append(lines, cur)
		cur, curW = nil, 0
	}
	for i := 0; i < len(glyphs); {
		if glyphs[i].space {
			if blank(cur) {
				cur, curW = nil, 0
			} else if curW+glyphs[i].width <= width {
				cur = append(cur, glyphs[i])
				curW += glyphs[i].width
			}
			i++
			continue
		}
		j, w := i, 0
		for j < len(glyphs) && !glyphs[j].space {
			w += glyphs[j].width
			j++
		}
		if curW+w > width && curW > 0 {
			flush()
		}
		word := len(cur)
		for k := i; k < j; k++ {
			if curW+glyphs[k].width > width && curW > 0 {
				p := breakAt(cur, word)
				tail := slices.Clone(cur[p:])
				cur = cur[:p]
				flush()
				cur, word = tail, 0
				for _, g := range tail {
					curW += g.width
				}
			}
			cur = append(cur, glyphs[k])
			curW += glyphs[k].width
		}
		i = j
	}
	if len(cur) > 0 || len(lines) == 0 {
		flush()
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		var b strings.Builder
		for s := 0; s < len(l); {
			e := s
			var run strings.Builder
			for e < len(l) && l[e].style == l[s].style {
				run.WriteString(l[e].text)
				e++
			}
			b.WriteString(styles[l[s].style].Render(run.String()))
			s = e
		}
		out[i] = b.String()
	}
	return out
}
