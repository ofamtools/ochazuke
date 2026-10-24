package ochazuke

// Pos is a cell position inside a rendered block: zero-based, counted from the
// block's top-left corner.
type Pos struct {
	X, Y int
}

// Block is the result of rendering. Text is trusted ANSI. Cursor is where the
// focused input's cursor sits inside Text, or nil when the block holds no
// focused input. Only the innermost focused component supplies a cursor;
// composers move it by where they placed the child and never invent one.
type Block struct {
	Text   string
	Cursor *Pos
}

// Text wraps rendered text that carries no cursor.
func Text(s string) Block { return Block{Text: s} }

// Offset returns the block with its cursor moved by dx, dy. Text is unchanged.
func (b Block) Offset(dx, dy int) Block {
	if b.Cursor == nil {
		return b
	}
	return Block{Text: b.Text, Cursor: &Pos{X: b.Cursor.X + dx, Y: b.Cursor.Y + dy}}
}

// Clip drops a cursor that fell outside a width x height cell area whose
// top-left corner is the block's origin, as when a composer clipped the child.
func (b Block) Clip(width, height int) Block {
	if b.Cursor != nil && (b.Cursor.X < 0 || b.Cursor.Y < 0 || b.Cursor.X >= width || b.Cursor.Y >= height) {
		return Block{Text: b.Text}
	}
	return b
}

// WithText replaces the text and keeps the cursor, for composers that rewrite
// the child's cells without moving them.
func (b Block) WithText(text string) Block { return Block{Text: text, Cursor: b.Cursor} }

// firstCursor returns the first block cursor among candidates, each already
// offset into the composer's coordinates.
func firstCursor(blocks ...Block) *Pos {
	for _, b := range blocks {
		if b.Cursor != nil {
			return b.Cursor
		}
	}
	return nil
}
