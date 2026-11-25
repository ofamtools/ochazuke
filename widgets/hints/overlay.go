package hints

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"unicode/utf8"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/layout"
	"ofam.dev/ochazuke/ui"
)

// OverlayStyle controls the ANSI SGR styling of hint badges painted by
// PaintOverlay and RenderOverlay.
type OverlayStyle struct {
	PrefixSGR string // SGR for already-typed prefix runes
	LabelSGR  string // SGR for remaining untyped label runes
	Bracketed bool   // wrap badge in [brackets] (default true in DefaultOverlayStyle)
}

// DefaultOverlayStyle returns a high-contrast terminal-portable badge style
// using standard ANSI bold/reverse SGR codes.
func DefaultOverlayStyle() OverlayStyle {
	return OverlayStyle{
		PrefixSGR: "\x1b[2;7m",
		LabelSGR:  "\x1b[1;7m",
		Bracketed: true,
	}
}

// OverlayStyleFromTheme derives an OverlayStyle from a ui.Theme without
// importing Lip Gloss.
func OverlayStyleFromTheme(th ui.Theme) OverlayStyle {
	st := DefaultOverlayStyle()
	if labelSGR := toneToSGR(th.Tone("badge.accent")); labelSGR != "" {
		st.LabelSGR = labelSGR
	}
	if prefixSGR := toneToSGR(th.Tone("badge")); prefixSGR != "" {
		st.PrefixSGR = prefixSGR
	}
	return st
}

// PaintOverlay composites active hint badges directly onto buf at each
// remaining target's (Rect.X, Rect.Y) coordinate.
func PaintOverlay(buf *buffer.Buffer, s *Session, style ...OverlayStyle) {
	if buf == nil || s == nil || !s.active || buf.Width() <= 0 || buf.Height() <= 0 {
		return
	}
	st := DefaultOverlayStyle()
	if len(style) > 0 {
		st = style[0]
	}
	w, h := buf.Width(), buf.Height()
	for _, p := range s.Placements() {
		if p.Y < 0 || p.Y >= h || p.X >= w {
			continue
		}
		var sb strings.Builder
		plainLen := 0
		if st.Bracketed {
			sb.WriteString(st.LabelSGR)
			sb.WriteByte('[')
			plainLen++
		}
		if p.MatchedPrefix != "" {
			sb.WriteString("\x1b[0m")
			sb.WriteString(st.PrefixSGR)
			sb.WriteString(p.MatchedPrefix)
			plainLen += utf8.RuneCountInString(p.MatchedPrefix)
		}
		if p.RemainingSuffix != "" {
			sb.WriteString("\x1b[0m")
			sb.WriteString(st.LabelSGR)
			sb.WriteString(p.RemainingSuffix)
			plainLen += utf8.RuneCountInString(p.RemainingSuffix)
		}
		if st.Bracketed {
			sb.WriteString("\x1b[0m")
			sb.WriteString(st.LabelSGR)
			sb.WriteByte(']')
			plainLen++
		}
		sb.WriteString("\x1b[0m")

		if plainLen <= 0 {
			continue
		}
		bx := max(0, p.X)
		if bx+plainLen > w {
			bx = max(0, w-plainLen)
		}
		bw := min(plainLen, w-bx)
		if bw <= 0 {
			continue
		}
		badge := buffer.New(bw, 1)
		badge.SetLine(0, sb.String())
		buf.Blit(badge, layout.Rect{X: bx, Y: p.Y, W: bw, H: 1})
	}
}

// RenderOverlay composites active hint badges onto an already-rendered ANSI
// frame of size w x h and returns the resulting ANSI string.
func RenderOverlay(baseANSI string, w, h int, s *Session, style ...OverlayStyle) string {
	if w <= 0 || h <= 0 || s == nil || !s.active {
		return baseANSI
	}
	buf := buffer.New(w, h)
	for y, line := range strings.Split(baseANSI, "\n") {
		if y >= h {
			break
		}
		buf.SetLine(y, line)
	}
	PaintOverlay(buf, s, style...)
	return buf.String()
}

// View returns a compact single-row ui.KindText status bar describing the
// active hint session (prefix, remaining count, and either the host-configured
// ActionLegend or the default modifier legend).
func (s *Session) View(id string) ui.ViewNode {
	if id == "" {
		id = "hints"
	}
	facts := s.Facts()
	if !s.Active() {
		return ui.ViewNode{
			ID:       id,
			Kind:     ui.KindText,
			Scope:    "hints",
			Box:      layout.Constraints{FixedH: 1},
			Props:    map[string]any{"text": ""},
			Facts:    facts,
			CacheKey: 1,
		}
	}

	rem := s.Remaining()
	active := s.effectiveAction()

	var b spanBuilder
	b.write("HINTS ", "badge.accent")
	b.write(s.prefix+"_", "match")
	b.write(fmt.Sprintf("  (%d)", len(rem)), "muted")

	if len(s.legend) > 0 {
		writeHostLegend(&b, s.legend, active)
	} else {
		if active != ActionDefault {
			b.write("  ", "muted")
			b.write("["+string(active)+"]", "badge.accent")
		}
		b.write("  [A-Z] secondary  [ctrl] alt  [esc] cancel", "muted")
	}

	return ui.ViewNode{
		ID:    id,
		Kind:  ui.KindText,
		Scope: "hints",
		Box:   layout.Constraints{FixedH: 1},
		Props: map[string]any{
			"text":  b.sb.String(),
			"spans": b.spans,
		},
		Facts:    facts,
		CacheKey: sessionCacheKey(s, rem),
	}
}

// ViewOverlay returns a floating ui.KindOverlay listing every remaining hint
// target and its label, suitable as a standalone hint legend or accessible
// overlay view.
func (s *Session) ViewOverlay(id string) ui.ViewNode {
	if id == "" {
		id = "hints.overlay"
	}
	if !s.Active() {
		return s.View(id)
	}
	rem := s.Remaining()
	kids := make([]ui.ViewNode, 0, len(rem))
	for _, p := range s.Placements() {
		badge := "[" + p.Target.Label + "]"
		desc := p.Target.Value
		if desc == "" {
			desc = p.Target.ID
		}
		if p.Target.Kind != "" {
			desc = p.Target.Kind + " · " + desc
		}
		line := badge + " " + desc
		bRunes := utf8.RuneCountInString(badge)
		kids = append(kids, ui.ViewNode{
			ID:   id + "." + p.Target.ID,
			Kind: ui.KindText,
			Props: map[string]any{
				"text": line,
				"spans": []ui.ToneSpan{
					{From: 0, To: bRunes, Tone: "match"},
					{From: bRunes, To: utf8.RuneCountInString(line), Tone: "text"},
				},
			},
		})
	}
	title := "Hints"
	if s.prefix != "" {
		title = "Hints: " + s.prefix
	}
	return ui.ViewNode{
		ID:       id,
		Kind:     ui.KindOverlay,
		Scope:    "hints",
		Title:    title,
		Badges:   []ui.Badge{{Text: strconv.Itoa(len(rem)), Tone: "accent"}},
		Children: kids,
		Facts:    s.Facts(),
		CacheKey: sessionCacheKey(s, rem),
	}
}

type spanBuilder struct {
	sb    strings.Builder
	runes int
	spans []ui.ToneSpan
}

func (b *spanBuilder) write(s, tone string) {
	if s == "" {
		return
	}
	n := utf8.RuneCountInString(s)
	if tone != "" {
		if len(b.spans) > 0 && b.spans[len(b.spans)-1].Tone == tone && b.spans[len(b.spans)-1].To == b.runes {
			b.spans[len(b.spans)-1].To += n
		} else {
			b.spans = append(b.spans, ui.ToneSpan{From: b.runes, To: b.runes + n, Tone: tone})
		}
	}
	b.sb.WriteString(s)
	b.runes += n
}

func writeHostLegend(b *spanBuilder, legend []ActionLegend, active Action) {
	matchedLegend := false
	for _, item := range legend {
		if item.Action != "" && item.Action == active {
			matchedLegend = true
			break
		}
	}
	if !matchedLegend && active != "" {
		b.write("  ", "muted")
		b.write("["+string(active)+"]", "badge.accent")
	}
	for _, item := range legend {
		seg := formatLegendItem(item)
		if seg == "" {
			continue
		}
		b.write("  ", "muted")
		if item.Action != "" && item.Action == active {
			b.write("● "+seg, "badge.accent")
		} else {
			b.write(seg, "muted")
		}
	}
}

func formatLegendItem(item ActionLegend) string {
	switch {
	case item.Key != "" && item.Label != "":
		return "[" + item.Key + "] " + item.Label
	case item.Key != "":
		return "[" + item.Key + "]"
	default:
		return item.Label
	}
}

func sessionCacheKey(s *Session, rem []Target) uint64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "%t|%s|%s|%t|", s.active, s.prefix, s.effectiveAction(), s.modifiers)
	for _, item := range s.legend {
		fmt.Fprintf(h, "%s=%s:%s;", item.Key, item.Label, item.Action)
	}
	for _, t := range rem {
		fmt.Fprintf(h, "%s:%s@%d,%d;", t.ID, t.Label, t.Rect.X, t.Rect.Y)
	}
	return h.Sum64() | 1
}

func toneToSGR(ts ui.ToneStyle) string {
	var parts []string
	if ts.Bold {
		parts = append(parts, "1")
	}
	if ts.Dim {
		parts = append(parts, "2")
	}
	if ts.Italic {
		parts = append(parts, "3")
	}
	if ts.Underline {
		parts = append(parts, "4")
	}
	if ts.Reverse {
		parts = append(parts, "7")
	}
	if ts.Strikethrough {
		parts = append(parts, "9")
	}
	if fg := colorSGR(ts.Fg, false); fg != "" {
		parts = append(parts, fg)
	}
	if bg := colorSGR(ts.Bg, true); bg != "" {
		parts = append(parts, bg)
	}
	if len(parts) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

func colorSGR(c string, bg bool) string {
	if c == "" {
		return ""
	}
	code := "38"
	if bg {
		code = "48"
	}
	if strings.HasPrefix(c, "#") && len(c) == 7 {
		r, err1 := strconv.ParseUint(c[1:3], 16, 8)
		g, err2 := strconv.ParseUint(c[3:5], 16, 8)
		b, err3 := strconv.ParseUint(c[5:7], 16, 8)
		if err1 == nil && err2 == nil && err3 == nil {
			return fmt.Sprintf("%s;2;%d;%d;%d", code, r, g, b)
		}
		return ""
	}
	if idx, err := strconv.Atoi(c); err == nil && idx >= 0 && idx <= 255 {
		return fmt.Sprintf("%s;5;%d", code, idx)
	}
	return ""
}
