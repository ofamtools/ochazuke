package ochazuke

import (
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Soft wrap (Input.Wrap): InputBlock draws a line wider than its block on
// further rows, broken after the last space that fits, and never changes
// the text.
//
// Compared with Vim: 'wrap' with 'linebreak' (:help 'linebreak'), where
// 'breakat' is a space alone; a word wider than the row breaks at the
// row's edge, as Vim does. Spaces at a break hang past the edge (drawn
// off the row) rather than starting the next one, as with 'linebreak'.
// Continuation rows are indented like 'breakindent' (the line's leading
// spaces, or Input.WrapIndent), keeping at least 20 cells (half a narrow
// block) for text as 'breakindentopt' min:20 does. Up and down keep
// moving by text line, as Vim's j, k and insert mode's arrows do with
// 'wrap' set; there is no gj or gk. With the cursor at the end of a line
// that fills its row, the cursor's cell starts a new row, as in Vim.

// wrapMinRoom is the least room continuation rows keep for text after
// their indent ('breakindentopt' min:20).
const wrapMinRoom = 20

// wrapBreaks is where InputBlock starts a new row inside a line when the
// input wraps at width: rune index to the row's indent in cells. ok is
// false when the input does not wrap (Wrap off, or no room to wrap in).
// cursor is the rune index of the cursor: at the end of a line, it needs
// one more cell on that line's last row.
func (in *Input) wrapBreaks(width, cursor int) (breaks map[int]int, ok bool) {
	if in == nil || !in.Wrap || width < 2 {
		return nil, false
	}
	breaks = map[int]int{}
	at := 0
	for _, line := range strings.Split(safeText(string(in.Text)), "\n") {
		n := utf8.RuneCountInString(line)
		in.wrapLine(breaks, line, at, width, cursor == at+n)
		at += n + 1
	}
	return breaks, true
}

// wrapItem is one grapheme of a wrapped line (or the cursor's cell after
// its end): its rune index, runes, cells, and whether it is a space.
type wrapItem struct {
	at, runes, cells int
	space, cursor    bool
}

// wrapLine adds the breaks of one line starting at rune index at.
func (in *Input) wrapLine(breaks map[int]int, line string, at, width int, cursorAtEnd bool) {
	if cellWidth(line) < width {
		return // fits, with a cell to spare for the cursor
	}
	indent := in.wrapIndentOf(line, width)
	var items []wrapItem
	g := uniseg.NewGraphemes(line)
	for i := at; g.Next(); {
		s := g.Str()
		n := utf8.RuneCountInString(s)
		items = append(items, wrapItem{at: i, runes: n, cells: cellWidth(s), space: s == " "})
		i += n
	}
	if cursorAtEnd {
		items = append(items, wrapItem{at: at + utf8.RuneCountInString(line), cells: 1, cursor: true})
	}
	room, rowStart, col, lastBreak := width, at, 0, -1
next:
	for k, it := range items {
		for col > 0 && col+it.cells > room {
			brk := it.at
			switch {
			case it.space:
				// A space at the edge hangs off the row; the next word
				// breaks after it.
				col += it.cells
				lastBreak = it.at + it.runes
				continue next
			case !it.cursor && lastBreak > rowStart:
				brk = lastBreak
			}
			breaks[brk] = indent
			room, rowStart, lastBreak, col = width-indent, brk, -1, 0
			for _, before := range items[:k] {
				if before.at >= brk {
					col += before.cells
				}
			}
		}
		col += it.cells
		if it.space {
			lastBreak = it.at + it.runes
		}
	}
}

// wrapIndentOf is a line's continuation indent at width: WrapIndent's, or
// its leading spaces, cut so the rows keep wrapMinRoom cells (or half the
// width when narrower) for text.
func (in *Input) wrapIndentOf(line string, width int) int {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if in.WrapIndent != nil {
		indent = in.WrapIndent(line)
	}
	least := min(wrapMinRoom, (width+1)/2)
	return max(0, min(indent, width-least))
}

// InputRows is how many rows InputBlock draws all of in at width: its
// lines, plus a row for each soft wrap when in.Wrap is set. A caller
// sizes the block with it so every row shows.
func InputRows(in *Input, width int) int {
	if in == nil {
		return 1
	}
	rows := strings.Count(string(in.Text), "\n") + 1
	breaks, _ := in.wrapBreaks(max(1, width), min(max(0, in.Cursor), len(in.Text)))
	return rows + len(breaks)
}
