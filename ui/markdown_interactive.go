package ui

import (
	"strings"
	"unicode"
)

// MarkdownLink describes one [label](href) link in Markdown source order.
type MarkdownLink struct {
	Index int    `json:"index"`
	Label string `json:"label"`
	Href  string `json:"href"`
	Line  int    `json:"line"` // 0-based source line
}

// MarkdownCheckbox describes one GFM task-list checkbox (- [ ] / - [x]) in
// Markdown source order. ByteOffset points directly at the status byte inside
// the brackets so toggling rewrites one byte without disturbing surrounding text.
type MarkdownCheckbox struct {
	Index      int    `json:"index"`
	Checked    bool   `json:"checked"`
	Text       string `json:"text"`
	Line       int    `json:"line"`       // 0-based source line
	ByteOffset int    `json:"byteOffset"` // byte offset of ' ', 'x', or 'X' in src
}

// parseTaskMarker checks whether a trimmed list line starts with a GFM task
// checkbox ("- [ ] ", "* [x] ", "+ [X] "). Returns (checked, body, boxColInTrim, ok).
func parseTaskMarker(trim string) (bool, string, int, bool) {
	if len(trim) < 6 {
		return false, "", 0, false
	}
	if (trim[0] != '-' && trim[0] != '*' && trim[0] != '+') || trim[1] != ' ' {
		return false, "", 0, false
	}
	if trim[2] != '[' || trim[4] != ']' || trim[5] != ' ' {
		return false, "", 0, false
	}
	switch trim[3] {
	case ' ':
		return false, trim[6:], 3, true
	case 'x', 'X':
		return true, trim[6:], 3, true
	}
	return false, "", 0, false
}

// MarkdownCheckboxes lists every GFM task checkbox in src, skipping fenced code blocks.
func MarkdownCheckboxes(src string) []MarkdownCheckbox {
	var out []MarkdownCheckbox
	inCode := false
	offset := 0
	lineIdx := 0
	for offset <= len(src) {
		end := strings.IndexByte(src[offset:], '\n')
		var raw string
		nextOffset := len(src) + 1
		if end >= 0 {
			raw = src[offset : offset+end]
			nextOffset = offset + end + 1
		} else {
			raw = src[offset:]
		}
		line := strings.TrimRight(raw, "\r")
		trim := strings.TrimLeft(line, " \t")
		leadBytes := len(line) - len(trim)
		if strings.HasPrefix(trim, "```") {
			inCode = !inCode
		} else if !inCode {
			if checked, body, boxCol, ok := parseTaskMarker(trim); ok {
				out = append(out, MarkdownCheckbox{
					Index:      len(out),
					Checked:    checked,
					Text:       strings.TrimSpace(body),
					Line:       lineIdx,
					ByteOffset: offset + leadBytes + boxCol,
				})
			}
		}
		offset = nextOffset
		lineIdx++
	}
	return out
}

// ToggleMarkdownCheckbox flips the index-th GFM task checkbox in src between
// "[ ]" and "[x]". It rewrites only that single byte and returns the updated
// source, the new checked state, and whether index was valid.
func ToggleMarkdownCheckbox(src string, index int) (string, bool, bool) {
	checks := MarkdownCheckboxes(src)
	if index < 0 || index >= len(checks) {
		return src, false, false
	}
	c := checks[index]
	if c.ByteOffset < 0 || c.ByteOffset >= len(src) {
		return src, false, false
	}
	nextChecked := !c.Checked
	ch := byte(' ')
	if nextChecked {
		ch = 'x'
	}
	b := []byte(src)
	b[c.ByteOffset] = ch
	return string(b), nextChecked, true
}

// matchInlineLink checks whether src[i:] starts a valid [label](href) link
// without crossing nested brackets or inline code backticks.
func matchInlineLink(src []rune, i int) (labelEnd, hrefEnd int, ok bool) {
	if i >= len(src) || src[i] != '[' || (i > 0 && src[i-1] == '!') {
		return 0, 0, false
	}
	j := -1
	for p := i + 1; p < len(src); p++ {
		if src[p] == '[' || src[p] == '`' || src[p] == '\n' {
			return 0, 0, false
		}
		if src[p] == ']' {
			if p+1 < len(src) && src[p+1] == '(' {
				j = p
			}
			break
		}
	}
	if j < 0 {
		return 0, 0, false
	}
	k := -1
	for p := j + 2; p < len(src); p++ {
		if src[p] == '\n' || src[p] == ')' {
			if src[p] == ')' {
				k = p
			}
			break
		}
	}
	if k <= j+2 || strings.TrimSpace(string(src[j+2:k])) == "" {
		return 0, 0, false
	}
	return j, k, true
}

// MarkdownLinks lists every inline [label](href) link in src in exact
// rendering order, skipping fenced code blocks and inline code spans.
func MarkdownLinks(src string) []MarkdownLink {
	var out []MarkdownLink
	inCode := false
	for lineIdx, raw := range strings.Split(src, "\n") {
		trim := strings.TrimSpace(raw)
		if strings.HasPrefix(trim, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		runes := []rune(raw)
		for i := 0; i < len(runes); {
			if runes[i] == '`' {
				end := -1
				for j := i + 1; j < len(runes); j++ {
					if runes[j] == '`' {
						end = j
						break
					}
				}
				if end > i+1 {
					i = end + 1
					continue
				}
			}
			if j, k, ok := matchInlineLink(runes, i); ok {
				href := strings.TrimSpace(string(runes[j+2 : k]))
				if href != "" {
					out = append(out, MarkdownLink{
						Index: len(out),
						Label: string(runes[i+1 : j]),
						Href:  href,
						Line:  lineIdx,
					})
				}
				i = k + 1
				continue
			}
			i++
		}
	}
	return out
}

type mdRenderOpts struct {
	activeLink  int // 0-based active link index, or -1
	activeCheck int // 0-based active checkbox index, or -1
}

type mdRenderState struct {
	opts     mdRenderOpts
	linkIdx  int
	checkIdx int
}

func (c *ctx) markdownLines(rn *rnode, w int) []string {
	n := rn.n
	opts := mdRenderOpts{activeLink: -1, activeCheck: -1}
	if rn.onFocus {
		if v, ok := n.propInt("activeLink"); ok {
			opts.activeLink = v
		}
		if v, ok := n.propInt("activeCheck"); ok {
			opts.activeCheck = v
		}
	}
	return mdLinesWith(c.st, n.propString("markdown"), w, opts)
}

// mdLinesWith renders Markdown with optional active-link and active-checkbox
// focus highlights (using the theme's "selected" tone).
func mdLinesWith(st *styler, text string, w int, opts mdRenderOpts) []string {
	w = max(w, 1)
	state := &mdRenderState{opts: opts}
	var out []string
	var para []string
	blank := func() {
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
	}
	flush := func() {
		if len(para) > 0 {
			out = append(out, mdInlineWith(st, strings.Join(para, " "), w, "", "", state, false)...)
			para = nil
		}
	}
	rule := func() string { return st.t("border", strings.Repeat("─", w)) }
	inCode := false
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
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
				out = append(out, mdInlineWithBase(st, txt, w, "", "", state, hs)...)
				if lvl == 1 {
					out = append(out, rule())
				}
				continue
			}
			if strings.HasPrefix(trim, "> ") {
				flush()
				q := st.t("md.quote", "│ ")
				out = append(out, mdInlineWith(st, trim[2:], max(1, w-2), q, q, state, false)...)
				continue
			}
			if tbl, consumed, ok := parseMarkdownTable(lines, i); ok {
				flush()
				blank()
				out = append(out, renderMarkdownTable(st, tbl, w, state)...)
				blank()
				i += consumed - 1
				continue
			}
			if checked, body, _, ok := parseTaskMarker(trim); ok {
				flush()
				curCheck := state.checkIdx
				state.checkIdx++
				focused := opts.activeCheck >= 0 && curCheck == opts.activeCheck
				boxGlyph := "☐ "
				boxTone := "accent"
				if checked {
					boxGlyph = "☑ "
					boxTone = "success"
				}
				mw := strW(boxGlyph)
				if focused {
					selTS := st.tone("selected")
					if selTS == (ToneStyle{}) {
						selTS = mergeTone(st.tone("focus.fg"), st.tone("focus.bg"))
					}
					selFill := st.sgr(selTS)
					first := st.paint(spaces(indent)+"▸ "+boxGlyph, selTS)
					rest := st.paint(spaces(indent+2+mw), selTS)
					lines := mdInlineWith(st, body, max(1, w-indent-2-mw), first, rest, state, true)
					for _, ln := range lines {
						out = append(out, padLine(ln, w, selFill))
					}
				} else {
					first := spaces(indent) + st.t(boxTone, boxGlyph)
					rest := spaces(indent + mw)
					out = append(out, mdInlineWith(st, body, max(1, w-indent-mw), first, rest, state, false)...)
				}
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
				out = append(out, mdInlineWith(st, body, max(1, w-indent-mw), first, rest, state, false)...)
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

type mdParsedInline struct {
	rs   []rune
	base ToneStyle
	rsp  []rspan
}

func mdInlineWith(st *styler, text string, w int, first, rest string, state *mdRenderState, rowFocused bool) []string {
	base := st.tone("text")
	if rowFocused {
		if sel := st.tone("selected"); sel != (ToneStyle{}) {
			base = sel
		} else {
			base = mergeTone(st.tone("focus.fg"), st.tone("focus.bg"))
		}
	}
	return mdInlineWithBase(st, text, w, first, rest, state, base)
}

func mdInlineWithBase(st *styler, text string, w int, first, rest string, state *mdRenderState, base ToneStyle) []string {
	parsed := parseMdInline(st, text, base, state)
	var out []string
	for n, r := range wrapRanges(parsed.rs, w) {
		prefix := rest
		if n == 0 {
			prefix = first
		}
		out = append(out, prefix+st.runs(parsed.rs, r[0], r[1], parsed.base, parsed.rsp))
	}
	return out
}

// parseMdInline parses inline Markdown (`code`, **bold**, *em*, _em_, and
// [label](href)), supporting nested inline formatting inside bold/emphasis
// and preserving exact MarkdownLinks ordering via state.
func parseMdInline(st *styler, text string, base ToneStyle, state *mdRenderState) mdParsedInline {
	var rs []rune
	var rsp []rspan
	parseMdInlineInto(st, []rune(text), base, state, &rs, &rsp)
	return mdParsedInline{rs: rs, base: base, rsp: rsp}
}

func parseMdInlineInto(st *styler, src []rune, cur ToneStyle, state *mdRenderState, rs *[]rune, rsp *[]rspan) {
	for i := 0; i < len(src); {
		switch {
		case src[i] == '`':
			if end := findClosingBacktick(src, i+1); end > i+1 {
				from := len(*rs)
				*rs = append(*rs, src[i+1:end]...)
				*rsp = append(*rsp, rspan{from: from, to: len(*rs), ts: mergeTone(cur, st.tone("md.code"))})
				i = end + 1
				continue
			}
		case src[i] == '*' && i+1 < len(src) && src[i+1] == '*':
			if end := findClosingBold(src, i+2); end > i+2 {
				boldTS := mergeTone(cur, st.tone("md.bold"))
				from := len(*rs)
				spanIdx := len(*rsp)
				*rsp = append(*rsp, rspan{from: from, to: from, ts: boldTS})
				parseMdInlineInto(st, src[i+2:end], boldTS, state, rs, rsp)
				(*rsp)[spanIdx].to = len(*rs)
				i = end + 2
				continue
			}
		case (src[i] == '*' || src[i] == '_') && i+1 < len(src) && src[i+1] != ' ':
			if src[i] == '_' && i > 0 && isWordRune(src[i-1]) {
				break
			}
			if end := findClosingDelim(src, i+1, src[i]); end > i+1 {
				emTS := mergeTone(cur, st.tone("md.em"))
				from := len(*rs)
				spanIdx := len(*rsp)
				*rsp = append(*rsp, rspan{from: from, to: from, ts: emTS})
				parseMdInlineInto(st, src[i+1:end], emTS, state, rs, rsp)
				(*rsp)[spanIdx].to = len(*rs)
				i = end + 1
				continue
			}
		case src[i] == '[':
			if j, k, ok := matchInlineLink(src, i); ok {
				from := len(*rs)
				*rs = append(*rs, src[i+1:j]...)
				linkTS := mergeTone(cur, st.tone("link"))
				if state != nil {
					curIdx := state.linkIdx
					state.linkIdx++
					if state.opts.activeLink >= 0 && curIdx == state.opts.activeLink {
						sel := st.tone("selected")
						if sel == (ToneStyle{}) {
							sel = mergeTone(st.tone("focus.fg"), st.tone("focus.bg"))
						}
						sel.Underline = true
						sel.Bold = true
						linkTS = sel
					}
				}
				*rsp = append(*rsp, rspan{from: from, to: len(*rs), ts: linkTS})
				i = k + 1
				continue
			}
		}
		*rs = append(*rs, src[i])
		i++
	}
}

func findClosingBacktick(src []rune, from int) int {
	for j := from; j < len(src); j++ {
		if src[j] == '`' {
			return j
		}
	}
	return -1
}

func findClosingBold(src []rune, from int) int {
	for j := from; j < len(src); {
		if src[j] == '`' {
			if end := findClosingBacktick(src, j+1); end > j+1 {
				j = end + 1
				continue
			}
		}
		if src[j] == '[' {
			if _, k, ok := matchInlineLink(src, j); ok {
				j = k + 1
				continue
			}
		}
		if src[j] == '*' && j+1 < len(src) && src[j+1] == '*' {
			for j+2 < len(src) && src[j+2] == '*' {
				j++
			}
			return j
		}
		j++
	}
	return -1
}

func findClosingDelim(src []rune, from int, delim rune) int {
	for j := from; j < len(src); {
		if src[j] == '`' {
			if end := findClosingBacktick(src, j+1); end > j+1 {
				j = end + 1
				continue
			}
		}
		if src[j] == '[' {
			if _, k, ok := matchInlineLink(src, j); ok {
				j = k + 1
				continue
			}
		}
		if delim == '*' && src[j] == '*' && j+1 < len(src) && src[j+1] == '*' {
			if end := findClosingBold(src, j+2); end > j+2 {
				j = end + 2
				continue
			}
		}
		if src[j] == delim {
			if delim == '_' && j+1 < len(src) && isWordRune(src[j+1]) {
				j++
				continue
			}
			return j
		}
		j++
	}
	return -1
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
