package core

// ViewportClamp computes the new top-row scroll offset for a list or tree of
// total rows viewed through a window of height rows, keeping selected visible
// with scrollOff rows of context padding above and below.
//
// Unlike naive clamping, moving the cursor away from an edge lets the cursor
// travel freely across the viewport without scrolling the document until it
// enters the opposite padding zone. Near the first or last row of the list,
// offset clamps to 0 or total-height so the cursor reaches the true edge.
func ViewportClamp(offset, selected, total, height, scrollOff int) int {
	if total <= 0 || height <= 0 {
		return 0
	}
	selected = min(max(selected, 0), total-1)
	maxOff := max(0, total-height)
	offset = min(max(offset, 0), maxOff)

	pad := max(scrollOff, 0)
	if maxPad := max(0, (height-1)/2); pad > maxPad {
		pad = maxPad
	}
	if selected < offset+pad {
		offset = selected - pad
	} else if selected >= offset+height-pad {
		offset = selected - height + 1 + pad
	}
	return min(max(offset, 0), maxOff)
}

// ViewportScroll shifts the viewport offset by delta rows (positive scrolls
// down like Vim's ctrl+e, negative scrolls up like ctrl+y) and nudges selected
// only when required to keep it inside the visible window with scrollOff
// padding when away from the list edges.
func ViewportScroll(offset, selected, total, height, scrollOff, delta int) (newOffset, newSelected int) {
	if total <= 0 || height <= 0 {
		return 0, 0
	}
	selected = min(max(selected, 0), total-1)
	maxOff := max(0, total-height)
	newOffset = min(max(offset+delta, 0), maxOff)

	pad := max(scrollOff, 0)
	if maxPad := max(0, (height-1)/2); pad > maxPad {
		pad = maxPad
	}
	lo := newOffset
	if newOffset > 0 {
		lo = newOffset + pad
	}
	hi := min(total-1, newOffset+height-1)
	if newOffset < maxOff {
		hi = min(total-1, newOffset+height-1-pad)
	}
	if lo > hi {
		lo = newOffset
		hi = min(total-1, newOffset+height-1)
	}
	newSelected = min(max(selected, lo), hi)
	return newOffset, newSelected
}
