package ochazuke

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
