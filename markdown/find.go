package markdown

import (
	"slices"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

// FindText puts the cursor on the next drawn line the query matches
// (search.find, ochazuke.TextFinder): the Doc's cursor is a line, or an
// element of lines (markdown.cursor item), so a find down starts after
// the cursor's whole element and a find up before it. A find leaves the
// block editor first, as a motion does.
func (d *Doc) FindText(m ochazuke.Matcher, back bool) bool {
	if d.edit != nil {
		if err := d.Leave(); err != nil {
			return false
		}
	}
	rows := d.lay.rows
	from := -1
	if back {
		from = len(rows)
	}
	if line := d.cursorLine(); line >= 0 {
		first, last := d.cursorSpan(line)
		edge := last
		if back {
			edge = first
		}
		if i := slices.Index(rows, edge); i >= 0 {
			from = i
		} else if i := slices.Index(rows, line); i >= 0 {
			from = i
		}
	}
	at, ok := ochazuke.FindLine(len(rows), from, back, func(i int) bool {
		return m.Match(ansi.Strip(d.lay.lines[rows[i]]))
	})
	if !ok {
		return false
	}
	return d.selectLine(rows[at]) == nil
}
