package ochazuke

import "slices"

// Blank lines above rows (ListItem.SpaceAbove). A list draws them in its
// three row layouts (plain, table, tall table); they are lines, never rows:
// the selection, motions, marks and the row counts of the window ignore
// them, and the window fits the lines they take. The first row shown never
// has one, nor does the first row of a scrolled window (nothing above it to
// space from).

// spacing reports whether any row draws a blank line above it: some shown
// row after the first has SpaceAbove and the list's setting allows it (a
// tree's tree.space-above).
func (l *List) spacing() bool {
	if len(l.items) < 2 || !slices.ContainsFunc(l.items[1:], func(it ListItem) bool { return it.SpaceAbove }) {
		return false
	}
	return l.spaceAllowed()
}

// spaceAllowed is false where the setting turns blank lines off.
func (l *List) spaceAllowed() bool {
	if t := l.tree(); t != nil {
		return t.spaceAboveEnabled()
	}
	return true
}

// spaceLines is the blank lines the list's rows (all of them, the filter's
// hidden ones too, so a host sizing the list by Height does not resize as
// the filter narrows it) would take.
func (l *List) spaceLines() int {
	if len(l.all) < 2 || !l.spaceAllowed() {
		return 0
	}
	n := 0
	for _, it := range l.all[1:] {
		if it.SpaceAbove {
			n++
		}
	}
	return n
}

// lineCount is the lines the shown rows take one each, plus a blank line
// above each SpaceAbove row after the first (spaced: spacing() was true).
func (l *List) lineCount(spaced bool) int {
	n := len(l.items)
	if spaced {
		for _, it := range l.items[1:] {
			if it.SpaceAbove {
				n++
			}
		}
	}
	return n
}

// span is the lines rows from..to (inclusive) take when from is the first
// row drawn: one each, a blank before each SpaceAbove row but from.
func (l *List) rowSpan(from, to int) int {
	n := to - from + 1
	for i := from + 1; i <= to; i++ {
		if l.items[i].SpaceAbove {
			n++
		}
	}
	return n
}

// spacedWindow is windowOffset for rows that take more than a line: the
// first row drawn and the one after the last, so the selected row keeps
// its scrolloff margin (shrunk in a short window, relaxed at the ends)
// inside visible lines.
func (l *List) spacedWindow(visible int) (offset, end int) {
	total := len(l.items)
	margin := min(l.scrolloff(), max(0, (visible-1)/2))
	offset = min(max(l.offset, 0), l.selected, l.fitFrom(visible))
	// Rows above the selection first: the margin above, then drop rows from
	// the top until the selection and its margin below fit.
	if lo := max(0, l.selected-margin); offset > lo {
		offset = lo
	}
	below := min(total-1, l.selected+margin)
	for offset < l.selected && l.rowSpan(offset, below) > visible {
		offset++
	}
	end = offset
	for end < total && l.rowSpan(offset, end) <= visible {
		end++
	}
	return offset, max(end, offset+1)
}

// fitFrom is the first row from which every row to the last fits visible
// lines, blank lines included.
func (l *List) fitFrom(visible int) int {
	total := len(l.items)
	first := total - 1
	for first > 0 && l.rowSpan(first-1, total-1) <= visible {
		first--
	}
	return max(first, 0)
}

// lastOffset is the greatest first row ScrollLines may scroll to: the
// window ends on the last row.
func (l *List) lastOffset() int {
	if l.budget <= 0 || !l.spacing() {
		return max(0, len(l.items)-l.window)
	}
	return l.fitFrom(l.budget)
}

// spaceAboveEnabled is the tree.space-above setting (default on).
func (t *Tree) spaceAboveEnabled() bool {
	v, _ := t.r.settingValue(TreeSpaceAboveSetting)
	on, _ := v.(bool)
	return on
}

// TreeSpaceAboveSetting turns off the blank lines a tree's rows ask for
// (TreeNode.SpaceAbove).
const TreeSpaceAboveSetting = "tree.space-above"
