package markdown

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// layout is a Doc drawn at one width: its lines, per element the first
// and last line it covers ({-1, -1} when it is not drawn), per line the
// element it belongs to (-1 for none: blank lines, a note box's borders)
// and the lines the cursor can sit on, in drawing order.
type layout struct {
	lines []string
	spans [][2]int
	owner []int
	rows  []int
}

// markedSource is the source Glamour draws: each element's text between
// its start and end marker, and each blockquote of notes replaced by one
// placeholder quote line holding its first note's start marker (the Doc
// draws notes itself).
func markedSource(src string, els []Element) string {
	var b strings.Builder
	at := 0
	for i := 0; i < len(els) && i < maxMarkers; i++ {
		e := els[i]
		if e.Kind == KindNote {
			if i > 0 && els[i-1].Kind == KindNote && els[i-1].Group == e.Group {
				continue
			}
			last := i
			for last+1 < len(els) && els[last+1].Kind == KindNote && els[last+1].Group == e.Group {
				last++
			}
			if e.Start < at {
				continue
			}
			b.WriteString(src[at:e.Start])
			b.WriteString(strings.Repeat(" ", e.Indent) + "> ")
			b.WriteRune(startMarker + rune(i))
			at = els[last].End
			continue
		}
		if e.ms < 0 || e.ms < at || e.me < e.ms {
			continue
		}
		b.WriteString(src[at:e.ms])
		b.WriteRune(startMarker + rune(i))
		b.WriteString(src[e.ms:e.me])
		b.WriteRune(endMarker + rune(i))
		at = e.me
	}
	b.WriteString(src[at:])
	return b.String()
}

// noteStyle draws notes: a rounded box in the note colour (none in the
// plain style), the author bold, the date faint.
type noteStyle struct {
	border, author, date, fold lipgloss.Style
}

func noteStyleOf(style string) noteStyle {
	var c color.Color
	switch style {
	case StyleDark:
		c = lipgloss.Color("#d7af5f")
	case StyleLight:
		c = lipgloss.Color("#875f00")
	}
	s := noteStyle{
		border: lipgloss.NewStyle(),
		author: lipgloss.NewStyle().Bold(true),
		date:   lipgloss.NewStyle().Faint(true),
		fold:   lipgloss.NewStyle().Faint(true),
	}
	if c != nil {
		s.border = s.border.Foreground(c)
		s.author = s.author.Foreground(c)
		s.fold = s.fold.Foreground(c)
	}
	return s
}

// noteBox draws a blockquote's notes as a box indent cells in, width
// wide in all: per note its lines (header first) and the box lines.
func noteBox(notes []Element, indent, width int, st noteStyle) (lines []string, spans [][2]int) {
	inner := max(4, width-indent-4)
	pad := strings.Repeat(" ", indent)
	lines = append(lines, pad+st.border.Render("╭"+strings.Repeat("─", inner+2)+"╮"))
	for _, n := range notes {
		head := ""
		if n.Author != "" {
			head = st.author.Render(n.Author)
			if n.Date != "" {
				head += " " + st.date.Render(n.Date)
			}
			if n.Resolved {
				head += " " + st.date.Render("✓")
			}
			if n.Sent {
				head += " " + st.date.Render("· sent")
			}
		}
		text := n.Text
		first, rest, _ := strings.Cut(text, "\n")
		if n.Resolved {
			first = st.date.Render(first)
		}
		body := first
		if head != "" {
			body = head + "  " + first
		}
		var content []string
		for _, l := range append([]string{body}, splitLines(rest)...) {
			if n.Resolved && l != body {
				l = st.date.Render(l)
			}
			content = append(content, strings.Split(ansi.Wrap(l, inner, ""), "\n")...)
		}
		top := len(lines)
		for _, l := range content {
			l = ansi.Truncate(l, inner, "")
			l += strings.Repeat(" ", max(0, inner-ansi.StringWidth(l)))
			lines = append(lines, pad+st.border.Render("│")+" "+l+" "+st.border.Render("│"))
		}
		spans = append(spans, [2]int{top, len(lines) - 1})
	}
	lines = append(lines, pad+st.border.Render("╰"+strings.Repeat("─", inner+2)+"╯"))
	return lines, spans
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// foldLine is a folded item's notes: its open notes counted, "· 2
// notes", and the resolved ones after them.
func foldLine(open, resolved, indent int, st noteStyle) string {
	word := "notes"
	if open == 1 {
		word = "note"
	}
	text := "· " + strconv.Itoa(open) + " " + word
	if resolved > 0 {
		text += ", " + strconv.Itoa(resolved) + " resolved"
	}
	return strings.Repeat(" ", indent) + st.fold.Render(text)
}

// layoutOf draws src (its elements els) width cells wide: Glamour for
// everything but notes, the Doc's own boxes for notes (folded: one count
// line under the item, per folded(item)).
func layoutOf(src string, els []Element, width int, style string, opts options, folded func(item int) bool) layout {
	raw := render(markedSource(src, els), 0, width, style, opts).lines
	n := min(len(els), maxMarkers)
	lay := layout{spans: make([][2]int, len(els))}
	for i := range lay.spans {
		lay.spans[i] = [2]int{-1, -1}
	}
	st := noteStyleOf(style)
	for _, line := range raw {
		note := -1
		for _, r := range line {
			if r >= startMarker && r < startMarker+rune(n) && els[r-startMarker].Kind == KindNote {
				note = int(r - startMarker)
			}
		}
		if note >= 0 {
			group := []int{}
			for k := note; k < len(els) && els[k].Kind == KindNote && els[k].Group == els[note].Group; k++ {
				group = append(group, k)
			}
			plain := ansi.Strip(line)
			indent := len(plain) - len(strings.TrimLeft(plain, " "))
			if item := els[note].Item; item >= 0 && folded(item) {
				at := len(lay.lines)
				open := 0
				for _, k := range group {
					if !els[k].Resolved {
						open++
					}
				}
				lay.lines = append(lay.lines, foldLine(open, len(group)-open, indent, st))
				lay.spans[note] = [2]int{at, at}
				continue
			}
			notes := make([]Element, len(group))
			for j, k := range group {
				notes[j] = els[k]
			}
			box, spans := noteBox(notes, indent, width, st)
			base := len(lay.lines)
			lay.lines = append(lay.lines, box...)
			for j, k := range group {
				lay.spans[k] = [2]int{base + spans[j][0], base + spans[j][1]}
			}
			continue
		}
		at := len(lay.lines)
		if !strings.ContainsFunc(line, func(r rune) bool { return isMarker(r, n) }) {
			lay.lines = append(lay.lines, line)
			continue
		}
		var b strings.Builder
		stripped := 0
		for _, r := range line {
			if !isMarker(r, n) {
				b.WriteRune(r)
				continue
			}
			stripped++
			if r >= endMarker {
				lay.spans[r-endMarker][1] = at
			} else if k := int(r - startMarker); lay.spans[k][0] < 0 {
				lay.spans[k][0] = at
			}
		}
		lay.lines = append(lay.lines, b.String()+strings.Repeat(" ", stripped))
	}
	for i := range lay.spans {
		switch {
		case lay.spans[i][0] < 0:
			lay.spans[i] = [2]int{-1, -1}
		case lay.spans[i][1] < lay.spans[i][0]:
			lay.spans[i][1] = lay.spans[i][0]
		}
	}
	lay.fillGaps()
	lay.owner = make([]int, len(lay.lines))
	for i := range lay.owner {
		lay.owner[i] = -1
	}
	for e, s := range lay.spans {
		for l := s[0]; l >= 0 && l <= s[1] && l < len(lay.owner); l++ {
			if lay.owner[l] < 0 {
				lay.owner[l] = e
			}
		}
	}
	for l, e := range lay.owner {
		if e >= 0 {
			lay.rows = append(lay.rows, l)
		}
	}
	return lay
}

// fillGaps gives the elements Glamour drew without their markers (a
// rule, an empty code block or row) lines from the gap between their
// neighbours: nonblank lines first, one each, in order.
func (lay *layout) fillGaps() {
	for i := 0; i < len(lay.spans); i++ {
		if lay.spans[i][0] >= 0 {
			continue
		}
		j := i
		for j < len(lay.spans) && lay.spans[j][0] < 0 {
			j++
		}
		lo := -1
		for k := i - 1; k >= 0; k-- {
			if lay.spans[k][1] >= 0 {
				lo = lay.spans[k][1]
				break
			}
		}
		hi := len(lay.lines)
		if j < len(lay.spans) {
			hi = lay.spans[j][0]
		}
		var free, blank []int
		for l := lo + 1; l < hi; l++ {
			if strings.TrimSpace(ansi.Strip(lay.lines[l])) != "" {
				free = append(free, l)
			} else {
				blank = append(blank, l)
			}
		}
		for k := i; k < j; k++ {
			switch {
			case len(free) > 0:
				lay.spans[k] = [2]int{free[0], free[0]}
				free = free[1:]
			case len(blank) > 0:
				lay.spans[k] = [2]int{blank[0], blank[0]}
				blank = blank[1:]
			}
		}
		i = j
	}
}
