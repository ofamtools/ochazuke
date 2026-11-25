package ui

import (
	"math"
	"strconv"
	"strings"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/layout"
)

// drawProgress renders a KindProgress leaf node directly into buf at its exact
// solved inner rect ir, expanding the bar fluidly to fill all available width.
func drawProgress(buf *buffer.Buffer, ir layout.Rect, n *ViewNode, theme Theme) {
	if buf == nil || n == nil || ir.W <= 0 || ir.H <= 0 {
		return
	}
	line := progressLine(newStyler(theme), n, ir.W)
	tmp := buffer.New(ir.W, ir.H)
	tmp.SetLine(0, line)
	buf.Blit(tmp, ir)
}

func progressLine(st *styler, n *ViewNode, w int) string {
	if n == nil || w <= 0 {
		return ""
	}

	done, hasDone := n.propFloat("done")
	if !hasDone {
		done, _ = n.propFloat("value")
	}
	if done < 0 {
		done = 0
	}
	active, _ := n.propFloat("active")
	if active < 0 {
		active = 0
	}
	total, _ := n.propFloat("total")
	if total < 0 {
		total = 0
	}
	pctProp, hasPct := n.propFloat("percent")

	var doneRatio, activeRatio float64
	switch {
	case total > 0:
		doneRatio = clamp01(done / total)
		activeRatio = clamp01(active / total)
	case hasPct:
		doneRatio = normalizePercent(pctProp)
		if active > 0 {
			activeRatio = normalizePercent(active)
		}
	}
	if doneRatio+activeRatio > 1 {
		activeRatio = max(0, 1-doneRatio)
	}
	pctInt := int(math.Round(doneRatio * 100))

	label := sanitizeProgressText(n.propString("label"))
	labelTone := toneOr(n.propString("labelTone"), "title")
	showFrac := total > 0 && n.propBoolDefault("showFraction", true)
	showPct := n.propBoolDefault("showPercent", true)
	suffix := sanitizeProgressText(n.propString("suffix"))
	suffixTone := toneOr(n.propString("suffixTone"), "muted")

	fillChar := progressGlyph(n.propString("fillChar"), "█")
	activeChar := progressGlyph(n.propString("activeChar"), "▓")
	trackChar := progressGlyph(n.propString("trackChar"), "░")

	fillTone := n.propString("fillTone")
	if fillTone == "" {
		if doneRatio >= 1 {
			fillTone = "success"
		} else {
			fillTone = "progress.fill"
		}
	}
	activeTone := toneOr(n.propString("activeTone"), "progress.active")
	trackTone := toneOr(n.propString("trackTone"), "progress.track")

	minBarW := 10
	if v, ok := n.propInt("minBarWidth"); ok && v >= 0 {
		minBarW = v
	}

	doneStr, totalStr := formatProgressNum(done), formatProgressNum(total)
	fracW := 0
	if showFrac {
		fracW = strW(doneStr) + 1 + strW(totalStr)
	}
	pctStr := strconv.Itoa(pctInt) + "%"
	pctW := 0
	if showPct {
		pctW = strW(pctStr)
	}

	// Cap label at ~30% of available width (max 28 cols) so the bar stays prominent.
	if label != "" {
		labelCap := min(28, max(1, (w*30)/100))
		if strW(label) > labelCap {
			label = truncate(label, labelCap)
		}
	}

	gap := 2
	computeBarW := func() int {
		lw := 0
		if label != "" {
			lw = strW(label)
			if showFrac {
				lw += gap + fracW
			}
		} else if showFrac {
			lw = fracW
		}
		rw := 0
		if showPct {
			rw = pctW
			if suffix != "" {
				rw += 1 + strW(suffix)
			}
		} else if suffix != "" {
			rw = strW(suffix)
		}
		lg, rg := 0, 0
		if lw > 0 {
			lg = gap
		}
		if rw > 0 {
			rg = gap
		}
		return w - lw - lg - rg - rw
	}

	barW := computeBarW()

	// Graceful narrow-width degradation: shrink label -> drop suffix -> tighten gaps -> drop label -> shrink bar -> drop counters.
	if barW < minBarW && label != "" {
		if lw := strW(label); lw > 4 {
			label = truncate(label, max(4, lw-(minBarW-barW)))
			barW = computeBarW()
		}
	}
	if barW < minBarW && suffix != "" {
		suffix = ""
		barW = computeBarW()
	}
	if barW < minBarW && gap > 1 {
		gap = 1
		barW = computeBarW()
	}
	if barW < minBarW && label != "" {
		if rem := strW(label) - (minBarW - barW); rem >= 2 {
			label = truncate(label, rem)
		} else {
			label = ""
		}
		barW = computeBarW()
	}
	if barW < 1 && showFrac {
		showFrac = false
		barW = computeBarW()
	}
	if barW < 1 && showPct {
		showPct = false
		barW = computeBarW()
	}
	if barW < 0 {
		barW = 0
	}

	fillCells, activeCells, trackCells := progressSegments(barW, doneRatio, activeRatio)
	sep := spaces(gap)

	var sb strings.Builder
	hasLeft := false
	if label != "" {
		sb.WriteString(st.t(labelTone, label))
		hasLeft = true
	}
	if showFrac {
		if hasLeft {
			sb.WriteString(sep)
		}
		sb.WriteString(st.t("title", doneStr))
		sb.WriteString(st.t("muted", "/"+totalStr))
		hasLeft = true
	}
	if barW > 0 {
		if hasLeft {
			sb.WriteString(sep)
		}
		if fillCells > 0 {
			sb.WriteString(st.t(fillTone, repeatToWidth(fillChar, fillCells)))
		}
		if activeCells > 0 {
			sb.WriteString(st.t(activeTone, repeatToWidth(activeChar, activeCells)))
		}
		if trackCells > 0 {
			sb.WriteString(st.t(trackTone, repeatToWidth(trackChar, trackCells)))
		}
	}
	hasRight := showPct || suffix != ""
	if hasRight {
		if hasLeft || barW > 0 {
			sb.WriteString(sep)
		}
		if showPct {
			sb.WriteString(st.t("title", pctStr))
			if suffix != "" {
				sb.WriteByte(' ')
			}
		}
		if suffix != "" {
			sb.WriteString(st.t(suffixTone, suffix))
		}
	}
	return sb.String()
}

func progressSegments(barW int, doneRatio, activeRatio float64) (fill, active, track int) {
	if barW <= 0 {
		return 0, 0, 0
	}
	switch {
	case doneRatio >= 1:
		return barW, 0, 0
	case doneRatio > 0:
		fill = int(math.Round(doneRatio * float64(barW)))
		if fill >= barW && barW > 1 {
			fill = barW - 1
		}
	}
	if activeRatio > 0 && barW-fill > 0 {
		active = int(math.Round(activeRatio * float64(barW)))
		if active == 0 {
			active = 1
		}
		if fill+active > barW {
			active = barW - fill
		}
	}
	track = max(0, barW-fill-active)
	return fill, active, track
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func normalizePercent(p float64) float64 {
	if p <= 0 {
		return 0
	}
	if p <= 1 {
		return p
	}
	return clamp01(p / 100)
}

func formatProgressNum(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func progressGlyph(s, def string) string {
	if s == "" || strW(s) <= 0 {
		return def
	}
	return s
}

func repeatToWidth(glyph string, cols int) string {
	if cols <= 0 {
		return ""
	}
	gw := strW(glyph)
	if gw <= 0 {
		return spaces(cols)
	}
	if gw == 1 {
		return strings.Repeat(glyph, cols)
	}
	return strings.Repeat(glyph, cols/gw) + spaces(cols%gw)
}

func sanitizeProgressText(s string) string {
	if s == "" {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(cleaned)
}
