package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func runeW(r rune) int {
	if r >= 0x20 && r < 0x7f {
		return 1
	}
	if r < 0x20 || r == 0x7f {
		return 0
	}
	return lipgloss.Width(string(r))
}

// strW is the display width of s, ignoring ANSI sequences.
func strW(s string) int { return lipgloss.Width(s) }

func runesW(rs []rune) int {
	w := 0
	for _, r := range rs {
		w += runeW(r)
	}
	return w
}

// wrapRanges word-wraps rs to width w and returns [start,end) rune ranges, one
// per line. Spaces at wrap points are dropped; explicit newlines split lines.
func wrapRanges(rs []rune, w int) [][2]int {
	w = max(w, 1)
	var out [][2]int
	ps := 0
	for i := 0; i <= len(rs); i++ {
		if i == len(rs) || rs[i] == '\n' {
			out = append(out, wrapPara(rs, ps, i, w)...)
			ps = i + 1
		}
	}
	return out
}

func wrapPara(rs []rune, s, e, w int) [][2]int {
	if s >= e {
		return [][2]int{{s, s}}
	}
	var out [][2]int
	ls, cur, lastSp := s, 0, -1
	for i := s; i < e; {
		rw := runeW(rs[i])
		if cur+rw > w && i > ls {
			switch {
			case rs[i] == ' ':
				out = append(out, [2]int{ls, i})
				ls = i + 1
			case lastSp > ls:
				out = append(out, [2]int{ls, lastSp})
				ls = lastSp + 1
			default:
				out = append(out, [2]int{ls, i})
				ls = i
			}
			i, cur, lastSp = ls, 0, -1
			continue
		}
		if rs[i] == ' ' {
			lastSp = i
		}
		cur += rw
		i++
	}
	return append(out, [2]int{ls, e})
}

// wrapPlain wraps text to width w.
func wrapPlain(text string, w int) []string {
	rs := []rune(text)
	rngs := wrapRanges(rs, w)
	out := make([]string, len(rngs))
	for i, r := range rngs {
		out[i] = string(rs[r[0]:r[1]])
	}
	return out
}

// truncate shortens plain text to display width w, ending with "…" when cut.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if strW(s) <= w {
		return s
	}
	var sb strings.Builder
	cur := 0
	for _, r := range s {
		rw := runeW(r)
		if cur+rw > w-1 {
			break
		}
		sb.WriteRune(r)
		cur += rw
	}
	sb.WriteString("…")
	return sb.String()
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// padPlain right-pads plain text with spaces to width w.
func padPlain(s string, w int) string { return s + spaces(w-strW(s)) }

// padLine right-pads an ANSI line to width w; fill is an SGR prefix for the pad.
func padLine(s string, w int, fill string) string {
	gap := w - strW(s)
	if gap <= 0 {
		return s
	}
	if fill == "" {
		return s + spaces(gap)
	}
	return s + fill + spaces(gap) + reset
}

type rspan struct {
	from, to int
	ts       ToneStyle
}

// resolveSpans merges each span's tones over base.
func (s *styler) resolveSpans(spans []ToneSpan, base ToneStyle) []rspan {
	out := make([]rspan, len(spans))
	for i, sp := range spans {
		ts := mergeTone(base, s.tone(sp.Tone))
		if sp.Bg != "" {
			if bg := s.bgOf(sp.Bg); bg != "" {
				ts.Bg = bg
			}
		}
		to := sp.To
		if to == 0 {
			to = 1 << 30
		}
		out[i] = rspan{sp.From, to, ts}
	}
	return out
}

// runs paints rs[a:b] with base, overridden by the last span covering each rune.
func (s *styler) runs(rs []rune, a, b int, base ToneStyle, spans []rspan) string {
	if a >= b {
		return ""
	}
	var sb strings.Builder
	start := a
	curTS := s.styleAt(a, base, spans)
	for i := a + 1; i <= b; i++ {
		var ts ToneStyle
		if i < b {
			ts = s.styleAt(i, base, spans)
			if ts == curTS {
				continue
			}
		}
		sb.WriteString(s.paint(string(rs[start:i]), curTS))
		start, curTS = i, ts
	}
	return sb.String()
}

func (s *styler) styleAt(i int, base ToneStyle, spans []rspan) ToneStyle {
	ts := base
	for k := len(spans) - 1; k >= 0; k-- {
		if i >= spans[k].from && i < spans[k].to {
			return spans[k].ts
		}
	}
	return ts
}

// badgeStyle returns the chip style for a badge tone.
func (s *styler) badgeStyle(tone string) ToneStyle {
	if s == nil {
		return ToneStyle{}
	}
	if tone == "" {
		return s.tone("badge")
	}
	if _, ok := s.th.Tones["badge."+tone]; ok {
		return s.tone("badge." + tone)
	}
	ts := s.tone("badge")
	if fg := s.tone(tone).Fg; fg != "" {
		ts.Fg = fg
	}
	return ts
}

func (s *styler) badgeText(b Badge) string {
	return s.paint(" "+b.Text+" ", s.badgeStyle(b.Tone))
}
