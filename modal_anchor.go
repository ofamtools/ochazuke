package ochazuke

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ModalAnchorSetting turns anchored modals on (the default) or off. A
// modal opened for something drawn on the page (Payload.Anchor: a form
// field's chooser) is drawn beside it, sized to its content, so the thing
// it is for and the lines around it stay visible; off, or when the screen
// leaves no room, it is centered as every other modal.
const ModalAnchorSetting = "modal.anchor"

// modalAnchorMin is the narrowest content an anchored modal is drawn at
// (room for its hint's first row), unless the modal bounds are narrower.
const modalAnchorMin = 24

// screenRect is a span of the screen: rows top to bottom (exclusive) and
// columns left to right (exclusive).
type screenRect struct{ left, right, top, bottom int }

// anchorRect is where instance id was drawn on screen this frame: the rows
// of its placement (Page.focusLines) and the columns its text fills on
// them. content is the page as rendered, before scrolling; toScreen maps a
// page position to the screen the way the page was composed. False when
// the page did not draw it or it is scrolled out of view.
func (m *Model) anchorRect(id string, content Block, toScreen func(Pos) (Pos, bool)) (screenRect, bool) {
	page := m.Runtime.PageOf(m.Runtime.Page)
	if page == nil || id == "" || m.Runtime.Instances[id] == nil {
		return screenRect{}, false
	}
	top, bottom, ok := page.focusLines(m.Runtime, id)
	if !ok {
		return screenRect{}, false
	}
	lines := strings.Split(content.Text, "\n")
	if top < 0 || bottom > len(lines) || top >= bottom {
		return screenRect{}, false
	}
	left, right := -1, 0
	for _, line := range lines[top:bottom] {
		text := strings.TrimRight(ansi.Strip(line), " ")
		if w := ansi.StringWidth(text); w > right {
			right = w
		}
		lead := ansi.StringWidth(text) - ansi.StringWidth(strings.TrimLeft(text, " "))
		if text != "" && (left < 0 || lead < left) {
			left = lead
		}
	}
	if left < 0 {
		left = 0
	}
	from, ok := toScreen(Pos{X: left, Y: top})
	if !ok {
		return screenRect{}, false
	}
	to, ok := toScreen(Pos{X: max(left, right-1), Y: bottom - 1})
	if !ok {
		return screenRect{}, false
	}
	return screenRect{left: from.X, right: to.X + 1, top: from.Y, bottom: to.Y + 1}, true
}

// anchoredPlace places a modal for an anchor on a width x height screen
// whose rows' text ends at the columns ends gives (the screen before the
// modal, trailing blanks dropped). The panel is contentWidth wide plus its
// border and padding, bodyRows tall plus its border, title and hint rows.
// It goes beside the anchor when it fits right of the text of every row it
// spans, one of them the anchor's (its body's first row level with the
// anchor's first row when it can be); else below the anchor; else above
// it, clear of the row above (a field's label). False when none fits: the
// modal is centered.
func anchoredPlace(a screenRect, ends []int, width, height, contentWidth, bodyRows int) (left, top int, ok bool) {
	pw, ph := contentWidth+4, bodyRows+4
	if ph > height {
		return 0, 0, false
	}
	// Tops that keep the anchor's first row beside the panel, the
	// preferred first, then nearer ones first.
	var tops []int
	for d := 0; d < ph; d++ {
		for _, t := range []int{a.top - 2 - d, a.top - 2 + d} {
			if t <= a.top && t+ph > a.top && t >= 0 && t+ph <= height && !slicesContainsInt(tops, t) {
				tops = append(tops, t)
			}
		}
	}
	for _, t := range tops {
		l := a.right
		for y := t; y < t+ph && y < len(ends); y++ {
			l = max(l, ends[y])
		}
		if l += 2; l+pw <= width-1 {
			return l, t, true
		}
	}
	l := clampSpan(a.left, pw, width)
	switch {
	case a.bottom+ph <= height:
		return l, a.bottom, true
	case a.top-1-ph >= 0:
		return l, a.top - 1 - ph, true
	}
	return 0, 0, false
}

// rowEnds is the column each row of a screen's text ends at, its trailing
// blanks dropped.
func rowEnds(text string) []int {
	lines := strings.Split(text, "\n")
	ends := make([]int, len(lines))
	for i, line := range lines {
		ends[i] = ansi.StringWidth(strings.TrimRight(ansi.Strip(line), " "))
	}
	return ends
}

func slicesContainsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// clampSpan moves a span of size n starting at at inside [0, total).
func clampSpan(at, n, total int) int { return max(0, min(at, total-n)) }

// fitWidth is the widest of a body's lines without their trailing blanks,
// and the title's: the width an anchored modal is drawn at.
func fitWidth(title string, body Block) int {
	w := ansi.StringWidth(title)
	for _, line := range strings.Split(body.Text, "\n") {
		w = max(w, ansi.StringWidth(strings.TrimRight(ansi.Strip(line), " ")))
	}
	return w
}
