package markdown

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

func (d *Doc) itemCursor() bool { return d.r.Settings().String(CursorSetting) != "line" }

// MoveCursor moves by the configured cursor unit. The underlying line rows
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
		next := e + dir
		for next >= 0 && next < len(d.elems) && (!d.drawn(next) || d.lay.spans[next][0] == d.lay.spans[e][0]) {
			next += dir
		}
		if next >= 0 && next < len(d.elems) {
			_ = d.SelectElement(next)
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
	st := lipgloss.NewStyle().Reverse(true)
	paint := func(text string) string {
		out := st.Render(text)
		if !d.list.Focused() {
			out = ochazuke.NewStyles(a).Fade(d.r.Settings().Int(ochazuke.DimUnfocusedSetting)).Render(out)
		}
		return out
	}
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
