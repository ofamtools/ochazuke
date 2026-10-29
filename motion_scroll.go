package ochazuke

import "errors"

// The half-page rule (UX-RULES R3.14): one motion key press (j, k, a step,
// a jump between items) never scrolls a view by more than half its height.
// When reaching the target would scroll further, the view scrolls half a
// page toward it and the cursor moves only once the target is in view.
// Explicit page keys (ctrl+d/ctrl+u, pgdown/pgup, J/K) and G/gg keep their
// own sizes. A component computes the scroll its plain motion would make
// (want, in lines, down when positive), scrolls by MotionScroll(want,
// page) and moves the cursor when SpanInView says the target shows.

// MotionScroll is the scroll a motion makes in a view page lines tall when
// reaching its target would scroll want lines (down when positive): want
// itself when it is at most half the page, else half the page (at least
// one line) toward it.
func MotionScroll(want, page int) int {
	half := max(1, page/2)
	switch {
	case want > half:
		return half
	case want < -half:
		return -half
	}
	return want
}

// SpanInView reports whether any of lines from through to (relative to the
// view's first line) is inside a view page lines tall.
func SpanInView(from, to, page int) bool {
	return to >= 0 && from < page && from <= to
}

// LineScroller is a component that windows its own lines and scrolls that
// window without a motion of the cursor (Vim's ctrl+e and ctrl+y, :help
// CTRL-E): the window moves, and the cursor stays where it is unless it
// would leave the window (with the page's scrolloff), when it moves to the
// nearest line still shown. A list (tables and trees through their list),
// a timeline and a markdown document implement it; view.scroll scrolls the
// innermost one on the focus path that can.
type LineScroller interface {
	// CanScrollLines reports whether the last frame windowed the lines (more
	// lines than the window shows), so a scroll would move it.
	CanScrollLines() bool
	// ScrollLines moves the window by lines, down when positive, clamped to
	// the first and last line.
	ScrollLines(lines int)
}

// viewScroller is the scrollable view.scroll moves for the focus: the
// innermost component on the focus path (an input excepted: its text is
// the editor's) that can scroll its lines, else the page's detail while it
// overflows (the J/K detail, which scrolls without the focus).
func (r *Runtime) viewScroller(focus string) func(lines int) {
	if r.Input(focus) == nil {
		path := r.Ancestry(focus)
		scroller := func(i int) LineScroller {
			if i < len(path) {
				if inst := r.Instances[path[i]]; inst != nil {
					if s, ok := inst.Component.(LineScroller); ok && s.CanScrollLines() {
						return s
					}
				}
			}
			return nil
		}
		for i := range path {
			if s := scroller(i); s != nil {
				// A scroller held by another one (a timeline's card list)
				// is part of its parent's window: the parent scrolls.
				if parent := scroller(i + 1); parent != nil {
					return parent.ScrollLines
				}
				return s.ScrollLines
			}
		}
	}
	if detailScrolls(r, KeyEvent{Focus: focus}) {
		if d := r.pageDetail(focus); d != nil {
			return func(lines int) { d.Scroll(lines) }
		}
	}
	return nil
}

// viewNoun is the focused scrollable view, the noun of view.scroll.
const viewNoun = "ochazuke.view"

func registerViewScroll(r *Runtime) {
	r.Define(viewNoun)
	r.DefineGuard("view-scrolls", func(r *Runtime, ev KeyEvent) bool { return r.viewScroller(ev.Focus) != nil })
	r.Register(Operation{Verb: "scroll", Noun: viewNoun, Motion: true,
		Description: "Scroll the focused view's window by lines times count, the cursor staying unless it would leave the window (Vim's ctrl+e/ctrl+y): a list, table, tree, timeline or document, else the page's detail",
		Parameters: map[string]Parameter{
			"lines": {Type: "integer", Description: "Lines to move, down when positive (default 1)"},
			"count": {Type: "integer", Description: "Repeat count (default 1)"},
		}, Result: "scrolled", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			scroll := r.viewScroller(focusOf(c))
			if scroll == nil {
				return nil, errors.New("nothing to scroll here")
			}
			scroll(IntArg(a, "lines", 1) * max(1, IntArg(a, "count", 1)))
			return true, nil
		}})
	r.SetAvailability(viewNoun+".scroll", "view-scrolls")
}

// CanScrollLines reports whether the list windowed its rows in the last
// frame (more rows than its window).
func (l *List) CanScrollLines() bool {
	return l.windowed && l.window > 0 && len(l.items) > l.window
}

// ScrollLines moves the list's window by lines rows, clamped, keeping the
// selection unless it would leave the window's scrolloff margin; then it
// moves to the nearest row inside (relaxed at the first and last row, as
// windowOffset is). It never spills.
func (l *List) ScrollLines(lines int) {
	if !l.CanScrollLines() || l.selected < 0 {
		return
	}
	total, visible := len(l.items), l.window
	l.offset = min(max(0, l.offset+lines), total-visible)
	margin := min(l.scrolloff(), max(0, (visible-1)/2))
	lo, hi := l.offset+margin, l.offset+visible-1-margin
	if l.offset == 0 {
		lo = 0
	}
	if l.offset+visible >= total {
		hi = total - 1
	}
	switch {
	case l.selected < lo:
		l.Move(lo - l.selected)
	case l.selected > hi:
		l.Move(hi - l.selected)
	}
}
