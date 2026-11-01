// Copyright (c) 2025 Gaurav Gosain. MIT License (see LICENSE).
// Adapted from tuios/internal/diffview. No Crush code is copied.
package diffview

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

// View draws only visible rows, tokenising their text on first use. The
// token cache is bounded; colours are resolved from current styles each
// frame. Long lines scroll horizontally (z h/z l), never break split pairs.
func (v *View) View(s ochazuke.Styles, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	v.width = width
	v.height = max(0, height-1)
	v.selectLayout(width)
	if v.height > 0 && v.cursor >= v.top+v.height {
		v.top = v.cursor - v.height + 1
	}
	v.clamp()
	theme := newTheme(s)
	added, removed := v.added, v.removed
	path := v.file.NewPath
	if path == "" {
		path = v.file.OldPath
	}
	if v.file.OldPath != "" && v.file.NewPath != "" && v.file.OldPath != v.file.NewPath {
		path = v.file.OldPath + " → " + v.file.NewPath
	}
	label := fmt.Sprintf("%s %s  +%d -%d  %s", printable(v.file.Status), printable(path), added, removed, v.layout)
	if v.file.Truncated {
		label += "  truncated"
	}
	lines := []string{s.Title.Render(fit(label, width))}
	if v.file.Binary {
		size := func(n int64) string {
			if n < 0 {
				return "?"
			}
			return strconv.FormatInt(n, 10)
		}
		if height > 1 {
			lines = append(lines, fit("binary, "+size(v.file.OldSize)+" bytes → "+size(v.file.NewSize)+" bytes", width))
		}
	} else {
		digits := v.digits
		for i := v.top; i < len(v.rows) && len(lines) < height; i++ {
			r := v.rows[i]
			if r.left < 0 && r.right < 0 {
				h := v.file.Hunks[r.hunk]
				title := fmt.Sprintf("@@ -%d,%d +%d,%d @@ %s", h.OldStart, h.OldCount, h.NewStart, h.NewCount, printable(h.Header))
				st := s.Muted.Bold(true)
				if v.r.Focus == v.id && i == v.cursor {
					st = st.Inherit(v.r.Selection(true))
				}
				lines = append(lines, st.Render(fit(title, width)))
				continue
			}
			focused := v.r.Focus == v.id && i == v.cursor
			if v.layout == Split {
				left := (width - 1) / 2
				right := width - 1 - left
				a := v.side(theme, r.hunk, r.left, true, focused, left, digits)
				b := v.side(theme, r.hunk, r.right, false, focused, right, digits)
				lines = append(lines, a+s.Muted.Render("│")+b)
			} else {
				lines = append(lines, v.unifiedLine(theme, r.hunk, r.left, focused, width, digits))
			}
		}
		if len(v.rows) == 0 && height > 1 {
			lines = append(lines, s.Muted.Render(fit("no text changes", width)))
		}
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	out := strings.Join(lines, "\n")
	v.r.Placed(v.id)
	return v.r.PlacedAt(v.id, out, max(0, v.cursor-v.top)+1, 1)
}
func fit(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = ansi.Truncate(text, width, "…")
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}
func printable(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, strings.ReplaceAll(text, "\t", "    "))
}
func (v *View) line(hi, li int) (string, []Span, Range) {
	l := v.file.Hunks[hi].Lines[li]
	text := printable(l.Text)
	v.maxWidth = max(v.maxWidth, ansi.StringWidth(text))
	var spans []Span
	if v.Highlight {
		key := lineKey{hi, li}
		var ok bool
		spans, ok = v.spans[key]
		if !ok {
			path := v.file.NewPath
			if l.Kind == Delete || path == "" {
				path = v.file.OldPath
			}
			highlighted := Highlight(path, []string{text})
			if len(highlighted) > 0 {
				spans = highlighted[0]
			}
			if len(v.spans) >= 1024 {
				clear(v.spans)
			}
			v.spans[key] = spans
		}
	}
	changed := Range{}
	if mate := v.mates[hi][li]; mate >= 0 {
		other := printable(v.file.Hunks[hi].Lines[mate].Text)
		if l.Kind == Delete {
			changed, _ = Changed(text, other)
		} else {
			_, changed = Changed(other, text)
		}
	}
	if l.NoNewline {
		text += " ⏎"
	}
	return text, spans, changed
}
func (v *View) number(t renderTheme, n, digits int, k Kind, selected bool) string {
	text := ""
	if n > 0 {
		text = strconv.Itoa(n)
	}
	st := t.gutter[k]
	if selected {
		st = st.Inherit(v.r.Selection(true))
	}
	return st.Render(fmt.Sprintf("%*s ", digits, text))
}
func (v *View) sign(t renderTheme, k Kind) string {
	sign := " "
	if k == Add {
		sign = "+"
	}
	if k == Delete {
		sign = "-"
	}
	after := " "
	if v.x > 0 {
		after = "‹"
	}
	return t.gutter[k].Bold(true).Render(sign + after)
}
func (v *View) side(t renderTheme, hi, li int, old, selected bool, width, digits int) string {
	if width <= 0 {
		return ""
	}
	if li < 0 {
		return t.base[Missing].Render(strings.Repeat(" ", width))
	}
	l := v.file.Hunks[hi].Lines[li]
	n := l.New
	if old {
		n = l.Old
	}
	text, spans, changed := v.line(hi, li)
	gutter := v.number(t, n, digits, l.Kind, selected) + v.sign(t, l.Kind)
	return ansi.Truncate(gutter+code(t, text, spans, changed, l.Kind, v.x, max(0, width-digits-3)), width, "")
}
func (v *View) unifiedLine(t renderTheme, hi, li int, selected bool, width, digits int) string {
	l := v.file.Hunks[hi].Lines[li]
	text, spans, changed := v.line(hi, li)
	gutter := v.number(t, l.Old, digits, l.Kind, selected) + v.number(t, l.New, digits, l.Kind, selected) + v.sign(t, l.Kind)
	return ansi.Truncate(gutter+code(t, text, spans, changed, l.Kind, v.x, max(0, width-2*(digits+1)-2)), width, "")
}

// code adapts tuios's span/changed-range sweep and exact cell padding. Very
// long lines bypass token styling, so a minified line stays cheap.
func code(t renderTheme, text string, spans []Span, changed Range, kind Kind, x, width int) string {
	if width <= 0 {
		return ""
	}
	base := t.base[kind]
	if len(text) > maxHighlightLine {
		// Skip syntax, but retain changed-word marks in the visible slice. At
		// most three plain runs are measured; no off-screen text is styled.
		var visible strings.Builder
		offset := 0
		cuts := []int{0, len(text)}
		if !changed.Empty() {
			cuts = []int{0, changed.Start, changed.End, len(text)}
		}
		for i := 0; i+1 < len(cuts); i++ {
			part := text[cuts[i]:cuts[i+1]]
			cells := ansi.StringWidth(part)
			left, right := max(0, x-offset), min(cells, x+width+1-offset)
			if left < right {
				style := base
				if !changed.Empty() && cuts[i] == changed.Start && cuts[i+1] == changed.End {
					style = t.word[kind]
				}
				visible.WriteString(style.Render(ansi.Cut(part, left, right)))
			}
			offset += cells
		}
		styled := ansi.Truncate(visible.String(), width, "…")
		return styled + base.Render(strings.Repeat(" ", max(0, width-ansi.StringWidth(styled))))
	}
	var b strings.Builder
	si := 0
	for i := 0; i < len(text); {
		for si < len(spans) && spans[si].End <= i {
			si++
		}
		class, next := Plain, len(text)
		if si < len(spans) {
			if spans[si].Start <= i {
				class, next = spans[si].Class, spans[si].End
			} else {
				next = spans[si].Start
			}
		}
		word := false
		if !changed.Empty() {
			switch {
			case i < changed.Start:
				next = min(next, changed.Start)
			case i < changed.End:
				word = true
				next = min(next, changed.End)
			}
		}
		next = max(min(next, len(text)), i+1)
		st := base
		if word {
			st = t.word[kind]
		}
		syntax := t.syntax[class]
		if fg := syntax.GetForeground(); hasColor(fg) {
			st = st.Foreground(fg)
		}
		if syntax.GetBold() {
			st = st.Bold(true)
		}
		if syntax.GetFaint() {
			st = st.Faint(true)
		}
		b.WriteString(st.Render(text[i:next]))
		i = next
	}
	styled := b.String()
	if x > 0 {
		styled = ansi.TruncateLeft(styled, x, "")
	}
	if ansi.StringWidth(styled) > width {
		styled = ansi.Truncate(styled, width, "…")
	}
	return styled + base.Render(strings.Repeat(" ", max(0, width-ansi.StringWidth(styled))))
}
