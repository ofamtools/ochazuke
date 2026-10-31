package ochazuke

import "slices"

// Keep the latest frame while Model coalesces composition. Own the lines:
// a stream may reuse its read buffer, and an identical-frame check must
// compare against the previous reading, not an already-mutated slice.
func (t *Terminal) acceptFrame(frame TerminalFrame) {
	if frame.Cursor == t.frame.Cursor && frame.Shape == t.frame.Shape && frame.Hidden == t.frame.Hidden && slices.Equal(frame.Lines, t.frame.Lines) {
		return
	}
	t.frame = frame
	t.frame.Lines = slices.Clone(frame.Lines)
}
