package ochazuke

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The columns presentation of a picker (ChooserSpec.Columns): three columns
// side by side, the folder the rows are in one level up (the source's Side,
// with the place the rows are in marked), the rows themselves, and the
// highlighted row's preview. The rows are the picker's own, so filtering,
// marks, motions and every key work as in the list; h and l are picker.up
// and picker.into, the same verbs, and the highlight is the picker's full-width
// bar in the middle column.

// ColumnsMin is the width from which a picker with Columns draws them;
// narrower, the rows take the whole width.
const ColumnsMin = 72

// chooserSideVerb names a source's side-column job.
const chooserSideVerb = "ochazuke.picker.side"

// columnsOn reports whether the picker draws columns at width.
func (c *Chooser) columnsOn(width int) bool {
	src := c.current()
	return (c.spec.Columns || src != nil && src.Columns) && width >= ColumnsMin && src != nil && src.Side != nil
}

// queueSide loads the current source's side column when its key changed
// (the source went to another folder), as a replacing job of the picker.
func (c *Chooser) queueSide() {
	src := c.current()
	if !(c.spec.Columns || src != nil && src.Columns) || src == nil || src.Side == nil {
		return
	}
	key, load := src.Side()
	if key == c.side.key && (c.side.loading || c.side.value.Title != "" || c.side.value.Lines != nil) {
		return
	}
	gen := c.side.gen + 1
	c.side = pickerSide{key: key, gen: gen}
	res := load()
	if res.work == nil {
		if v, ok := res.value.(PickerSide); ok {
			c.side.value = v
		}
		return
	}
	c.side.loading = true
	res = res.Replacing("side").also(func(_ Ctx, value any, err error) {
		if c.r.Chooser(c.spec.Kind) != c || c.side.gen != gen {
			return
		}
		c.side.loading = false
		if v, ok := value.(PickerSide); ok && err == nil {
			c.side.value = v
		}
	})
	if _, err := c.r.Launch(c.spec.Kind, chooserSideVerb, res, Ctx{actor: c.r.Actor(), Runtime: c.r, Context: c.r.contextOf(c.spec.Kind), Instance: c.r.Instances[c.spec.Kind]}); err != nil {
		c.side.loading = false
	}
}

// Side is the side column as last loaded.
func (c *Chooser) Side() PickerSide { return c.side.value }

// columnsView is View's three columns: parent, rows, preview.
func (c *Chooser) columnsView(width, height int) Block {
	const gap = " │ "
	gapW := ansi.StringWidth(gap)
	count := 3
	if src := c.current(); src != nil && src.ColumnCount != nil {
		count = min(3, max(1, src.ColumnCount()))
	}
	if count == 1 {
		return c.rowsView(width, height)
	}
	if count == 2 {
		leftW := max(14, width/3)
		midW := width - leftW - gapW
		mid := c.rowsView(midW, height)
		lines := strings.Split(mid.Text, "\n")
		left := c.sideColumn(leftW, len(lines))
		for i, line := range lines {
			l := ""
			if i < len(left) {
				l = left[i]
			}
			lines[i] = l + strings.Repeat(" ", max(0, leftW-ansi.StringWidth(l))) + gap + line
		}
		return mid.Offset(leftW+gapW, 0).WithText(strings.Join(lines, "\n"))
	}
	leftW := max(14, width*26/98)
	rightW := max(20, width*36/98)
	midW := width - leftW - rightW - 2*gapW
	if midW < 24 {
		return c.rowsView(width, height)
	}
	mid := c.rowsView(midW, height)
	midLines := strings.Split(mid.Text, "\n")
	left := c.sideColumn(leftW, len(midLines))
	text := ""
	if row, ok := c.Highlighted(); ok {
		text = c.previewText(row)
	}
	right := previewLines(text, rightW, max(1, len(midLines)-1))
	out := make([]string, len(midLines))
	for i, line := range midLines {
		l, rt := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i >= 1 && i-1 < len(right) {
			rt = right[i-1]
		}
		out[i] = l + strings.Repeat(" ", max(0, leftW-ansi.StringWidth(l))) + gap + line + strings.Repeat(" ", max(0, midW-ansi.StringWidth(line))) + gap + rt
	}
	return mid.Offset(leftW+gapW, 0).WithText(strings.Join(out, "\n"))
}

// sideColumn is the side column's lines, n of them: its title on the
// search line's row, then the lines with the place the rows are in drawn in
// the picker's unfocused selection, scrolled to keep it in view.
func (c *Chooser) sideColumn(width, n int) []string {
	side := c.side.value
	title := side.Title
	if c.side.loading && title == "" {
		title = LoadingText("parent")
	}
	lines := []string{ansi.Truncate(title, width, "…")}
	at := side.AtRow - 1
	if side.AtRow == 0 {
		for i, l := range side.Lines {
			if l == side.At {
				at = i
			}
		}
	}
	room := max(1, n-1)
	offset := 0
	if at >= room {
		offset = at - room/2
	}
	toned := c.toneStyles()
	for i := offset; i < len(side.Lines) && len(lines) < n; i++ {
		line := "  " + ansi.Truncate(side.Lines[i], max(1, width-2), "…")
		if i == at {
			line = ansi.Truncate(line+strings.Repeat(" ", width), width, "")
			if toned != nil {
				line = toned.Selection(false).Render(line)
			}
		}
		lines = append(lines, line)
	}
	return lines
}
