package buffer

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"ofam.dev/ochazuke/layout"
)

const tabWidth = 4

type spanTransition struct {
	offset int
	style  string
	link   string
}

func satAdd64(a, b int64) int64 {
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}
	if b < 0 && a < math.MinInt64-b {
		return math.MinInt64
	}
	return a + b
}

// SetLine parses ansiText and writes it onto row y, clipping cleanly at display
// width without splitting wide graphemes or breaking combining marks. Out-of-bounds
// rows are ignored; shorter text clears the remainder of row y to unstyled spaces.
func (b *Buffer) SetLine(y int, ansiText string) {
	if b == nil || y < 0 || y >= b.h || b.w <= 0 {
		return
	}

	rowOff := y * b.w
	for x := 0; x < b.w; x++ {
		b.cells[rowOff+x] = cell{ch: " ", width: 1}
	}

	var sgr sgrState
	curStyle := ""
	curLink := ""
	x := 0

	var seg strings.Builder
	var transitions []spanTransition
	baseStyle := ""
	baseLink := ""

	recordTransition := func() {
		off := seg.Len()
		if len(transitions) > 0 && transitions[len(transitions)-1].offset == off {
			transitions[len(transitions)-1].style = curStyle
			transitions[len(transitions)-1].link = curLink
			return
		}
		transitions = append(transitions, spanTransition{
			offset: off,
			style:  curStyle,
			link:   curLink,
		})
	}

	flushSegment := func() bool {
		plain := seg.String()
		if len(plain) == 0 {
			transitions = transitions[:0]
			baseStyle = curStyle
			baseLink = curLink
			return false
		}
		state := -1
		tIdx := 0
		segStyle := baseStyle
		segLink := baseLink
		clusterOff := 0
		lastLeadX := -1
		clipped := false

		for len(plain) > 0 {
			for tIdx < len(transitions) && transitions[tIdx].offset <= clusterOff {
				segStyle = transitions[tIdx].style
				segLink = transitions[tIdx].link
				tIdx++
			}
			var cluster string
			var w int
			cluster, plain, w, state = uniseg.FirstGraphemeClusterInString(plain, state)
			clusterOff += len(cluster)

			if w == 0 {
				if lastLeadX >= 0 {
					candidate := b.cells[rowOff+lastLeadX].ch + cluster
					if uniseg.StringWidth(candidate) == int(b.cells[rowOff+lastLeadX].width) {
						b.cells[rowOff+lastLeadX].ch = strings.Clone(candidate)
					}
				}
				continue
			}
			if x+w > b.w {
				clipped = true
				break
			}
			b.cells[rowOff+x] = cell{
				ch:    strings.Clone(cluster),
				width: uint8(w),
				style: segStyle,
				link:  segLink,
			}
			for k := 1; k < w; k++ {
				b.cells[rowOff+x+k] = cell{
					width: 0,
					cont:  true,
					style: segStyle,
					link:  segLink,
				}
			}
			lastLeadX = x
			x += w
		}

		seg.Reset()
		transitions = transitions[:0]
		baseStyle = curStyle
		baseLink = curLink
		return clipped || x >= b.w
	}

	for i := 0; i < len(ansiText); {
		if ansiText[i] == '\x1b' {
			i = parseEscape(ansiText, i, &sgr, &curLink)
			curStyle = sgr.ansi()
			recordTransition()
			continue
		}
		if ansiText[i] == '\t' {
			if flushSegment() {
				return
			}
			n := tabWidth - (x % tabWidth)
			if x+n > b.w {
				n = b.w - x
			}
			for k := 0; k < n; k++ {
				b.cells[rowOff+x+k] = cell{ch: " ", width: 1, style: curStyle, link: curLink}
			}
			x += n
			if x >= b.w {
				return
			}
			i++
			continue
		}
		if ansiText[i] < 0x20 || ansiText[i] == 0x7F {
			i++
			continue
		}
		if ansiText[i] < 0x80 {
			seg.WriteByte(ansiText[i])
			i++
			continue
		}

		r, size := utf8.DecodeRuneInString(ansiText[i:])
		if r == utf8.RuneError && size == 1 {
			seg.WriteString("\uFFFD")
			i++
			continue
		}
		if (r >= 0x80 && r <= 0x9F) || r == '\u2028' || r == '\u2029' {
			i += size
			continue
		}
		seg.WriteString(ansiText[i : i+size])
		i += size
	}

	flushSegment()
}

// repairRange clears any wide-character cluster on row y that would be split
// at the left edge x0 or right edge x1 of a write range [x0, x1).
func (b *Buffer) repairRange(y, x0, x1 int) {
	rowOff := y * b.w
	if x0 > 0 && b.cells[rowOff+x0].cont {
		lx := x0 - 1
		for lx > 0 && b.cells[rowOff+lx].cont {
			lx--
		}
		for x := lx; x < x0; x++ {
			b.cells[rowOff+x] = cell{ch: " ", width: 1}
		}
	}
	if x1 < b.w && b.cells[rowOff+x1].cont {
		rx := x1
		for rx < b.w && b.cells[rowOff+rx].cont {
			b.cells[rowOff+rx] = cell{ch: " ", width: 1}
			rx++
		}
	}
}

// Blit copies src into b at rectangle at, clipping to src, at, and b bounds,
// repairing any split wide-character cells at boundaries, and translating
// src.Cursor when visible (dropping it if clipped out of bounds).
func (b *Buffer) Blit(src *Buffer, at layout.Rect) {
	if b == nil || src == nil || b.w <= 0 || b.h <= 0 || src.w <= 0 || src.h <= 0 {
		if b != nil && src != nil && src.Cursor.Shape != CursorHidden {
			b.Cursor = Cursor{Shape: CursorHidden}
		}
		return
	}

	visW := min(int64(src.w), int64(at.W))
	visH := min(int64(src.h), int64(at.H))
	if visW <= 0 || visH <= 0 {
		if src.Cursor.Shape != CursorHidden {
			b.Cursor = Cursor{Shape: CursorHidden}
		}
		return
	}

	dstX0 := max(int64(0), int64(at.X))
	dstY0 := max(int64(0), int64(at.Y))
	dstX1 := min(int64(b.w), satAdd64(int64(at.X), visW))
	dstY1 := min(int64(b.h), satAdd64(int64(at.Y), visH))

	if dstX0 < dstX1 && dstY0 < dstY1 {
		x0, y0, x1, y1 := int(dstX0), int(dstY0), int(dstX1), int(dstY1)

		srcCells := src.cells
		if src == b {
			srcCells = make([]cell, len(src.cells))
			copy(srcCells, src.cells)
		}

		for dy := y0; dy < y1; dy++ {
			b.repairRange(dy, x0, x1)
			sy := int(int64(dy) - int64(at.Y))
			sxStart := int(int64(x0) - int64(at.X))
			srcRowOff := sy * src.w
			dstRowOff := dy * b.w

			for dx := x0; dx < x1; {
				sx := sxStart + (dx - x0)
				sc := srcCells[srcRowOff+sx]
				if sc.cont {
					b.cells[dstRowOff+dx] = cell{ch: " ", width: 1}
					dx++
					continue
				}
				cw := int(sc.width)
				if cw > 1 {
					if dx+cw <= x1 && sx+cw <= src.w {
						for k := 0; k < cw; k++ {
							b.cells[dstRowOff+dx+k] = srcCells[srcRowOff+sx+k]
						}
						dx += cw
					} else {
						for ; dx < x1; dx++ {
							b.cells[dstRowOff+dx] = cell{ch: " ", width: 1}
						}
						break
					}
					continue
				}
				b.cells[dstRowOff+dx] = sc
				dx++
			}
		}
	}

	if src.Cursor.Shape != CursorHidden {
		srcX := int64(src.Cursor.X)
		srcY := int64(src.Cursor.Y)
		cx := satAdd64(int64(at.X), srcX)
		cy := satAdd64(int64(at.Y), srcY)
		if srcX >= 0 && srcX < visW && srcY >= 0 && srcY < visH &&
			cx >= dstX0 && cx < dstX1 && cy >= dstY0 && cy < dstY1 {
			b.Cursor = Cursor{
				X:     int(cx),
				Y:     int(cy),
				Shape: src.Cursor.Shape,
			}
		} else {
			b.Cursor = Cursor{Shape: CursorHidden}
		}
	}
}

// Fill fills rectangle r clipped to b with rune ch (defaulting 0/controls to ' ')
// styled by styleANSI (full SGR/OSC8 sequences or bare SGR parameter lists).
func (b *Buffer) Fill(r layout.Rect, ch rune, styleANSI string) {
	if b == nil || b.w <= 0 || b.h <= 0 || r.W <= 0 || r.H <= 0 {
		return
	}

	x0 := max(int64(0), int64(r.X))
	y0 := max(int64(0), int64(r.Y))
	x1 := min(int64(b.w), satAdd64(int64(r.X), int64(r.W)))
	y1 := min(int64(b.h), satAdd64(int64(r.Y), int64(r.H)))
	if x0 >= x1 || y0 >= y1 {
		return
	}
	ix0, iy0, ix1, iy1 := int(x0), int(y0), int(x1), int(y1)

	if ch == 0 || ch == '\t' || unicode.IsControl(ch) || ch == '\u2028' || ch == '\u2029' || !utf8.ValidRune(ch) {
		ch = ' '
	}
	chStr := string(ch)
	cw := uniseg.StringWidth(chStr)
	if cw <= 0 {
		chStr = " "
		cw = 1
	}

	fillStyle, fillLink := parseStyleANSI(styleANSI)

	for y := iy0; y < iy1; y++ {
		b.repairRange(y, ix0, ix1)
		rowOff := y * b.w
		x := ix0
		for x+cw <= ix1 {
			b.cells[rowOff+x] = cell{
				ch:    chStr,
				width: uint8(cw),
				style: fillStyle,
				link:  fillLink,
			}
			for k := 1; k < cw; k++ {
				b.cells[rowOff+x+k] = cell{
					width: 0,
					cont:  true,
					style: fillStyle,
					link:  fillLink,
				}
			}
			x += cw
		}
		for x < ix1 {
			b.cells[rowOff+x] = cell{
				ch:    " ",
				width: 1,
				style: fillStyle,
				link:  fillLink,
			}
			x++
		}
	}
}

// String renders the buffer rows joined by '\n' with no trailing newline,
// coalescing adjacent identical styles/links and closing active SGR/OSC 8 spans
// on changes and at end of line.
func (b *Buffer) String() string {
	if b == nil || b.w <= 0 || b.h <= 0 {
		return ""
	}

	var sb strings.Builder
	sb.Grow(b.w * b.h)

	for y := 0; y < b.h; y++ {
		if y > 0 {
			sb.WriteByte('\n')
		}
		curStyle := ""
		curLink := ""
		rowOff := y * b.w

		for x := 0; x < b.w; x++ {
			c := b.cells[rowOff+x]
			if c.cont {
				continue
			}
			if c.link != curLink {
				if curLink != "" {
					sb.WriteString("\x1b]8;;\x07")
				}
				if c.link != "" {
					sb.WriteString(c.link)
				}
				curLink = c.link
			}
			if c.style != curStyle {
				if curStyle != "" {
					sb.WriteString("\x1b[0m")
				}
				if c.style != "" {
					sb.WriteString(c.style)
				}
				curStyle = c.style
			}
			if c.ch == "" {
				sb.WriteByte(' ')
			} else {
				sb.WriteString(c.ch)
			}
		}

		if curLink != "" {
			sb.WriteString("\x1b]8;;\x07")
		}
		if curStyle != "" {
			sb.WriteString("\x1b[0m")
		}
	}

	return sb.String()
}
