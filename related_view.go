package ochazuke

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// relatedViewer is a placed view that stands for a section's things
// (NounBuilder.RelatedView): a tree or a list.
type relatedViewer interface {
	View(width, height int) string
	Height() int
}

// relatedMargin is the lines the stack keeps around the focused line.
const relatedMargin = 1

// FocusTarget is where a jump to the stack lands: the first drawn
// section's selected row.
func (rel *Related) FocusTarget() string {
	rel.sync()
	if v := rel.visible(); len(v) > 0 {
		return v[0].list.FocusTarget()
	}
	return rel.id
}

// Focus focuses the first section's selected row.
func (rel *Related) Focus() error { return rel.r.SetFocus(rel.FocusTarget()) }

// View draws the sections one under the other in width by height lines. The
// stack keeps the focused row on screen (scrolling by lines, with a margin),
// and, scrolled, draws the heading of the section it is inside on its first
// line so you can tell where you are. Every drawn section is placed in
// drawing order, so j and k spill from one into the next.
func (rel *Related) View(width, height int) string {
	rel.sync()
	if width <= 0 || height <= 0 {
		return ""
	}
	var lines []string
	type span struct {
		s          *relatedSection
		start, end int
	}
	var spans []span
	focus := -1
	for _, s := range rel.visible() {
		text, at := s.draw(width)
		start := len(lines)
		lines = append(lines, strings.Split(text, "\n")...)
		spans = append(spans, span{s, start, len(lines)})
		if at >= 0 {
			focus = start + at
		}
	}
	if len(lines) == 0 {
		return strings.Join(layoutRows("", width, height), "\n")
	}
	if focus < 0 {
		focus = rel.lastLine
	}
	focus = min(focus, len(lines)-1)
	rel.lastLine = focus
	top := scrollToShow(rel.top, focus, focus+1, height, len(lines), relatedMargin)
	if top > 0 && focus == top {
		top-- // the first line is the breadcrumb: the focus stays under it
	}
	top = min(max(top, 0), max(0, len(lines)-height))
	rel.top = top
	window := slices.Clone(lines[top:min(len(lines), top+height)])
	if top > 0 {
		for i := len(spans) - 1; i >= 0; i-- {
			if spans[i].start <= top && top < spans[i].end {
				if spans[i].start < top { // the section's heading scrolled off
					window[0] = ansi.Truncate(lipgloss.NewStyle().Faint(true).Render("↑ "+strings.TrimLeft(spans[i].s.headLabel(), "▾▸ ")), width, "…")
				}
				break
			}
		}
	}
	return strings.Join(layoutRows(strings.Join(window, "\n"), width, height), "\n")
}

// headLabel is the heading's text.
func (s *relatedSection) headLabel() string { return s.heading().Label }

// draw is the section's lines at its natural height and the line of the
// focus in them, -1 when the focus is not in the section.
func (s *relatedSection) draw(width int) (string, int) {
	r := s.rel.r
	h := max(1, s.list.Height())
	if len(s.list.columns) > 0 {
		h++ // the header line
	}
	text := s.list.View(width, h)
	focus := -1
	if s.list.Focused() {
		focus = max(0, s.list.selLine)
	}
	if s.view == "" || s.collapsed() {
		return text, focus
	}
	inst := r.Instances[s.view]
	if inst == nil {
		return text, focus
	}
	v, ok := inst.Component.(relatedViewer)
	if !ok {
		return text, focus
	}
	above := strings.Count(text, "\n") + 1
	vt := v.View(width, max(1, v.Height()))
	if r.focusInside(s.view) {
		focus = above
		if l, ok := inst.Component.(interface{ List() *Collection }); ok && l.List().selLine >= 0 {
			focus = above + l.List().selLine
		}
	}
	return text + "\n" + vt, focus
}
