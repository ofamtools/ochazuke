package markdown

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

func (d *Doc) itemCursor() bool { return d.r.Settings().String(CursorSetting) != "line" }

// MoveCursor moves by the configured cursor unit, visiting inline links when
// link stops are enabled in item mode. The underlying line rows
// remain available to paging, scrolling and editing. A long element is read
// half a window at a time before the cursor crosses its far edge (R3.14).
func (d *Doc) MoveCursor(delta, count int) error {
	if err := d.Leave(); err != nil {
		return err
	}
	lineMove := func(delta, count int) error {
		q := ochazuke.Request{Noun: "ochazuke.list", Verb: "move", Args: ochazuke.Arguments{"id": d.ListID(), "delta": delta, "steps": count}}
		if !d.r.Available(q, d.r.Focus, d.r.Mode) {
			return nil // at an edge the list traps (list.no-spill): it stays
		}
		_, err := d.r.Execute(q)
		return err
	}
	if !d.itemCursor() {
		return lineMove(delta, count)
	}
	if delta == 0 {
		return nil
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	for n := 0; n < max(1, count); n++ {
		e, _ := d.cursor()
		if e < 0 {
			return lineMove(dir, 1)
		}
		from := d.cursorLine()
		if _, ok := d.FocusedLink(); ok && d.r.Settings().Bool(LinkStopsSetting) {
			next := d.linkIndex + dir
			if next >= 0 && next < len(d.links) && d.links[next].Element == e {
				_ = d.selectLink(next, false)
				continue
			}
		}
		next := e + dir
		for next >= 0 && next < len(d.elems) && (!d.drawn(next) || d.lay.spans[next][0] == d.lay.spans[e][0]) {
			next += dir
		}
		if next >= 0 && next < len(d.elems) {
			_ = d.SelectElement(next)
			if dir < 0 && d.r.Settings().Bool(LinkStopsSetting) {
				if i := d.firstLink(next); i >= 0 {
					for i+1 < len(d.links) && d.links[i+1].Element == next {
						i++
					}
					_ = d.selectLink(i, false)
				}
			}
			d.halfPage(from)
			continue
		}
		// Reach the last element's end before spilling, as a line list
		// does. A count arriving at the edge stops there on this press.
		if n > 0 {
			break
		}
		edge := d.lay.rows[0]
		if dir > 0 {
			edge = d.lay.rows[len(d.lay.rows)-1]
		}
		if from != edge {
			_ = d.selectLine(edge)
			d.halfPage(from)
			if d.cursorLine() != edge {
				break
			}
		}
		return lineMove(dir, 1) // list.move owns drawing-order spill and clamping
	}
	return nil
}

// cursorSpan is the selected unit's inclusive range of drawn lines.
func (d *Doc) cursorSpan(line int) (int, int) {
	if d.itemCursor() && line >= 0 && line < len(d.lay.owner) {
		if e := d.lay.owner[line]; e >= 0 {
			return d.lay.spans[e][0], d.lay.spans[e][1]
		}
	}
	return line, line
}

func (d *Doc) highlight(line string, width int, a ochazuke.Appearance) string {
	// The one selection painter: reverse video, kept marked and faded
	// without the focus (UX R5.1, unfocused-selection).
	styles := d.r.StylesFor(a)
	paint := styles.Selection(d.list.Focused()).Render
	plain := ansi.Truncate(ansi.Strip(line), width, "")
	if d.r.Settings().String(CursorWidthSetting) == "text" {
		body := strings.Trim(plain, " ")
		indent := len(plain) - len(strings.TrimLeft(plain, " "))
		if body == "" {
			body, indent = " ", min(indent, max(0, width-1))
		}
		return strings.Repeat(" ", indent) + paint(body)
	}
	plain += strings.Repeat(" ", max(0, width-ansi.StringWidth(plain)))
	return paint(plain)
}

// highlightLink keeps the element's reverse selection and adds the theme's
// accent and underline only to the focused inline link.
func (d *Doc) highlightLink(line string, row, width int, a ochazuke.Appearance) string {
	link, ok := d.FocusedLink()
	if !ok {
		return d.highlight(line, width, a)
	}
	out := d.highlight(line, width, a)
	// Focused, the link takes the accent inside the bar; without the focus
	// it is the faded bar underlined (Runtime.Selection).
	styles := d.r.StylesFor(a)
	st := styles.Selection(false)
	if st.GetReverse() {
		st = st.Underline(true)
	}
	if d.list.Focused() {
		st = styles.Accent.Underline(true).Inherit(styles.Selection(true))
	}
	paint := st.Render
	for _, run := range link.runs {
		if run.line != row {
			continue
		}
		start, end := min(run.start, width), min(run.end, width)
		if end <= start {
			continue
		}
		plain := ansi.Strip(out)
		out = ansi.Cut(out, 0, start) + paint(ansi.Cut(plain, start, end)) + ansi.Cut(out, end, ansi.StringWidth(plain))
	}
	return out
}
