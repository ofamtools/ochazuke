package ochazuke

import "slices"

// Keep the latest frame while Model coalesces composition. Own the lines:
// a stream may reuse its read buffer, and an identical-frame check must
// compare against the previous reading, not an already-mutated slice.
func (t *Terminal) acceptFrame(frame TerminalFrame) {
	if sameTerminalFrame(frame, t.frame) {
		return
	}
	t.frame = frame
	t.frame.Lines = slices.Clone(frame.Lines)
}

// sameTerminalFrame reports whether two readings are identical, the
// stream's Status included.
func sameTerminalFrame(a, b TerminalFrame) bool {
	return a.Cursor == b.Cursor && a.Shape == b.Shape && a.Hidden == b.Hidden && a.Status == b.Status && slices.Equal(a.Lines, b.Lines)
}
