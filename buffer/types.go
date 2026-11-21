// Package buffer implements a 2D terminal cell grid with grapheme-safe
// display-width clipping, ANSI SGR and OSC 8 style preservation, and cursor
// composition.
package buffer

// CursorShape identifies the terminal hardware cursor presentation.
type CursorShape uint8

const (
	CursorHidden CursorShape = iota
	CursorBlock
	CursorBar
	CursorUnderline
)

// Cursor records a cursor position and shape in buffer-local coordinates.
type Cursor struct {
	X, Y  int
	Shape CursorShape
}

type cell struct {
	ch    string
	width uint8
	cont  bool
	style string // canonical SGR sequence: "" or "\x1b[...m"
	link  string // canonical OSC 8 opening sequence: "" or "\x1b]8;<params>;<uri>\x07"
}

// Buffer is a 2D grid of terminal cells with an optional cursor.
type Buffer struct {
	w, h   int
	cells  []cell
	Cursor Cursor
}

// New creates a w x h Buffer initialized with single-width spaces and a hidden
// cursor. Negative dimensions are clamped to 0.
func New(w, h int) *Buffer {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	b := &Buffer{
		w: w,
		h: h,
	}
	if w > 0 && h > 0 {
		b.cells = make([]cell, w*h)
		for i := range b.cells {
			b.cells[i] = cell{ch: " ", width: 1}
		}
	}
	return b
}

// Width returns the buffer width in terminal columns.
func (b *Buffer) Width() int {
	if b == nil {
		return 0
	}
	return b.w
}

// Height returns the buffer height in terminal rows.
func (b *Buffer) Height() int {
	if b == nil {
		return 0
	}
	return b.h
}
