package traits

import "ofam.dev/ochazuke/core"

// Long-jump scroll policies for ScrollStepToward.
const (
	// LongJumpHalfPage implements Rule 1B: if a target span is completely
	// off-screen, scroll by a half-page toward it while keeping focus on the
	// current item until any part of the target becomes visible.
	LongJumpHalfPage = "half-page"

	// LongJumpDirect opts out of Rule 1B, immediately revealing and focusing
	// the target span even when it is completely off-screen.
	LongJumpDirect = "jump"
)

// ScrollConfig configures pure scroll window clamping, stepping, overscroll,
// and off-screen long-jump behavior.
type ScrollConfig struct {
	ScrollOff      int    `json:"scrollOff,omitempty"`
	PageOverlap    int    `json:"pageOverlap,omitempty"`
	HalfPageStep   int    `json:"halfPageStep,omitempty"` // 0 = max(1, ViewH/2)
	Overscroll     bool   `json:"overscroll,omitempty"`
	LongJumpPolicy string `json:"longJumpPolicy,omitempty"` // "" or "half-page" (default Rule 1B) vs "jump"
}

// ScrollState captures the scroll window geometry and offsets independently of
// focus or cursor selection.
type ScrollState struct {
	OffsetY  int `json:"offsetY"`
	OffsetX  int `json:"offsetX,omitempty"`
	ContentH int `json:"contentH"`
	ViewH    int `json:"viewH"`
	ContentW int `json:"contentW,omitempty"`
	ViewW    int `json:"viewW,omitempty"`
}

// ItemSpan describes an item's vertical extent [Y, Y+max(1,H)) in content
// coordinates.
type ItemSpan struct {
	ID string `json:"id,omitempty"`
	Y  int    `json:"y"`
	H  int    `json:"h"` // <= 0 treated as 1
}

// ScrollJumpResult reports the outcome of ScrollStepToward.
type ScrollJumpResult struct {
	State         ScrollState `json:"state"`
	Scrolled      bool        `json:"scrolled"`
	FocusTarget   bool        `json:"focusTarget"`   // true = target is visible (or viewport hit bound), caller moves focus to target; false = stay on A
	TargetVisible bool        `json:"targetVisible"` // whether any part of target is visible in resulting State
	HalfStepped   bool        `json:"halfStepped"`   // true when Rule 1B performed an intermediate half-page scroll
}

// ScrollMaxOffsetY returns the maximum valid vertical scroll offset for st
// under cfg.
func ScrollMaxOffsetY(st ScrollState, cfg ScrollConfig) int {
	if st.ContentH <= 0 {
		return 0
	}
	if cfg.Overscroll {
		return max(0, st.ContentH-1)
	}
	if st.ViewH <= 0 {
		return 0
	}
	return max(0, st.ContentH-st.ViewH)
}

func scrollMaxOffsetX(st ScrollState) int {
	if st.ViewW <= 0 || st.ContentW <= st.ViewW {
		return 0
	}
	return st.ContentW - st.ViewW
}

// ScrollClamp normalizes OffsetY and OffsetX within valid bounds for cfg.
func ScrollClamp(st ScrollState, cfg ScrollConfig) ScrollState {
	st.OffsetY = min(max(st.OffsetY, 0), ScrollMaxOffsetY(st, cfg))
	st.OffsetX = min(max(st.OffsetX, 0), scrollMaxOffsetX(st))
	return st
}

// ScrollHalfStep returns the vertical half-page step size for st under cfg.
func ScrollHalfStep(st ScrollState, cfg ScrollConfig) int {
	if cfg.HalfPageStep > 0 {
		return cfg.HalfPageStep
	}
	return max(1, st.ViewH/2)
}

// ScrollSpanVisible reports whether any part of [span.Y, span.Y+max(1,span.H))
// overlaps the visible window [st.OffsetY, st.OffsetY+st.ViewH).
func ScrollSpanVisible(st ScrollState, span ItemSpan) bool {
	if st.ViewH <= 0 {
		return false
	}
	h := max(1, span.H)
	return span.Y+h > st.OffsetY && span.Y < st.OffsetY+st.ViewH
}

// ScrollReveal minimally adjusts OffsetY so span is visible within st,
// respecting cfg.ScrollOff. If span is at least as tall as the viewport, it
// aligns the top of span so the top of a tall item is never cut off.
func ScrollReveal(st ScrollState, cfg ScrollConfig, span ItemSpan) ScrollState {
	st = ScrollClamp(st, cfg)
	if st.ContentH <= 0 || st.ViewH <= 0 {
		return st
	}
	h := max(1, span.H)
	if h >= st.ViewH {
		st.OffsetY = span.Y
		return ScrollClamp(st, cfg)
	}
	pad := max(cfg.ScrollOff, 0)
	if maxPad := max(0, (st.ViewH-h)/2); pad > maxPad {
		pad = maxPad
	}
	spanEnd := span.Y + h
	if span.Y < st.OffsetY+pad {
		st.OffsetY = span.Y - pad
	} else if spanEnd > st.OffsetY+st.ViewH-pad {
		st.OffsetY = spanEnd - st.ViewH + pad
	}
	return ScrollClamp(st, cfg)
}

// ScrollStep applies pure scroll motions (motion.scroll.down, motion.scroll.up,
// motion.half.down, motion.half.up, motion.page.down, motion.page.up,
// motion.top, motion.bottom, motion.scroll.left, motion.scroll.right, plus
// motion.down/motion.up when delegated directly) to st under cfg.
func ScrollStep(st ScrollState, cfg ScrollConfig, cmd core.Command) (next ScrollState, moved, handled bool) {
	switch cmd.ID {
	case "motion.scroll.down", "motion.scroll.up",
		"motion.down", "motion.up",
		"motion.half.down", "motion.half.up",
		"motion.page.down", "motion.page.up",
		"motion.top", "motion.bottom",
		"motion.scroll.left", "motion.scroll.right":
	default:
		return st, false, false
	}

	cur := ScrollClamp(st, cfg)
	n := cmd.Times()
	h := max(cur.ViewH, 1)
	half := ScrollHalfStep(cur, cfg)
	pageStep := max(h-max(cfg.PageOverlap, 0), 1)
	next = cur

	switch cmd.ID {
	case "motion.scroll.down", "motion.down":
		next.OffsetY = cur.OffsetY + n
	case "motion.scroll.up", "motion.up":
		next.OffsetY = cur.OffsetY - n
	case "motion.half.down":
		next.OffsetY = cur.OffsetY + n*half
	case "motion.half.up":
		next.OffsetY = cur.OffsetY - n*half
	case "motion.page.down":
		next.OffsetY = cur.OffsetY + n*pageStep
	case "motion.page.up":
		next.OffsetY = cur.OffsetY - n*pageStep
	case "motion.top":
		next.OffsetY = 0
	case "motion.bottom":
		next.OffsetY = max(0, cur.ContentH-h)
	case "motion.scroll.left":
		next.OffsetX = cur.OffsetX - n
	case "motion.scroll.right":
		next.OffsetX = cur.OffsetX + n
	}

	next = ScrollClamp(next, cfg)
	return next, next.OffsetY != cur.OffsetY || next.OffsetX != cur.OffsetX, true
}

// ScrollStepToward steps the scroll window toward target according to
// cfg.LongJumpPolicy (defaulting to LongJumpHalfPage / Rule 1B).
//
// Under LongJumpHalfPage, if target is completely off-screen, ScrollStepToward
// scrolls by one half-page toward target and returns FocusTarget=false so the
// caller stays on the current item until any part of target peeks into view.
// Once any part of target is visible (or if the viewport is pinned at a bound,
// or under LongJumpDirect), it reveals target and returns FocusTarget=true.
func ScrollStepToward(st ScrollState, cfg ScrollConfig, target ItemSpan) ScrollJumpResult {
	cur := ScrollClamp(st, cfg)
	target.H = max(1, target.H)
	if cur.ViewH <= 0 || cur.ContentH <= cur.ViewH {
		return ScrollJumpResult{
			State:         cur,
			Scrolled:      false,
			FocusTarget:   true,
			TargetVisible: true,
			HalfStepped:   false,
		}
	}

	vis := ScrollSpanVisible(cur, target)
	policy := cfg.LongJumpPolicy
	if policy == "" {
		policy = LongJumpHalfPage
	}

	if policy == LongJumpHalfPage && !vis {
		half := ScrollHalfStep(cur, cfg)
		next := cur
		if target.Y >= cur.OffsetY+cur.ViewH {
			next.OffsetY = cur.OffsetY + half
		} else {
			next.OffsetY = cur.OffsetY - half
		}
		next = ScrollClamp(next, cfg)
		if next.OffsetY == cur.OffsetY {
			return ScrollJumpResult{
				State:         next,
				Scrolled:      false,
				FocusTarget:   true,
				TargetVisible: ScrollSpanVisible(next, target),
				HalfStepped:   false,
			}
		}
		return ScrollJumpResult{
			State:         next,
			Scrolled:      true,
			FocusTarget:   false,
			TargetVisible: ScrollSpanVisible(next, target),
			HalfStepped:   true,
		}
	}

	next := ScrollReveal(cur, cfg, target)
	return ScrollJumpResult{
		State:         next,
		Scrolled:      next.OffsetY != cur.OffsetY,
		FocusTarget:   true,
		TargetVisible: ScrollSpanVisible(next, target),
		HalfStepped:   false,
	}
}

// ScrollFacts derives truthful read-only scroll facts from st under cfg.
func ScrollFacts(st ScrollState, cfg ScrollConfig) map[string]any {
	st = ScrollClamp(st, cfg)
	maxY := ScrollMaxOffsetY(st, cfg)
	maxX := scrollMaxOffsetX(st)
	empty := st.ContentH <= 0
	return map[string]any{
		"atTop":      empty || st.OffsetY <= 0,
		"atBottom":   empty || st.OffsetY >= maxY,
		"atLeft":     st.OffsetX <= 0,
		"atRight":    maxX <= 0 || st.OffsetX >= maxX,
		"scrollable": st.ViewH > 0 && st.ContentH > st.ViewH,
		"offsetY":    st.OffsetY,
		"offsetX":    st.OffsetX,
		"contentH":   st.ContentH,
		"viewH":      st.ViewH,
		"maxOffsetY": maxY,
		"halfStep":   ScrollHalfStep(st, cfg),
	}
}
