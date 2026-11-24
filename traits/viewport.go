// Package traits defines pure, domain-agnostic interaction traits shared across
// Ochazuke widgets and views: viewport motion/clamping, edge focus spilling,
// and multi-step dismiss resolution.
//
// traits performs no I/O, starts no goroutines, and imports only the Go
// standard library and ofam.dev/ochazuke/core.
package traits

import "ofam.dev/ochazuke/core"

// Viewport motion modes.
const (
	ViewportModeCursor = "cursor"
	ViewportModeOffset = "offset"
)

// ViewportConfig configures viewport clamping, paging, wrapping, and motion
// mode. Its zero value is cursor mode with zero scroll-off, no page overlap,
// and no wrapping.
type ViewportConfig struct {
	ScrollOff   int    `json:"scrollOff,omitempty"`
	PageOverlap int    `json:"pageOverlap,omitempty"`
	Wrap        bool   `json:"wrap,omitempty"`
	Mode        string `json:"mode,omitempty"` // "cursor" (default) or "offset"
	Overscroll  bool   `json:"overscroll,omitempty"`
}

// ViewportState captures the vertical and horizontal viewport geometry and
// position of a scrollable widget.
type ViewportState struct {
	Cursor       int
	Offset       int
	Total        int
	Height       int
	ScrollX      int
	ContentWidth int
	ViewWidth    int
}

func viewportMaxOffset(st ViewportState, cfg ViewportConfig) int {
	if st.Total <= 0 {
		return 0
	}
	if cfg.Mode == ViewportModeOffset && cfg.Overscroll {
		return max(0, st.Total-1)
	}
	if st.Height <= 0 {
		return 0
	}
	return max(0, st.Total-st.Height)
}

func viewportPad(height, scrollOff int) int {
	if height <= 0 {
		return 0
	}
	pad := max(scrollOff, 0)
	if maxPad := max(0, (height-1)/2); pad > maxPad {
		pad = maxPad
	}
	return pad
}

func wrapIndex(idx, n int) int {
	if n <= 0 {
		return 0
	}
	return ((idx % n) + n) % n
}

// ViewportClamp normalizes Cursor, Offset, and ScrollX within bounds for cfg.
func ViewportClamp(st ViewportState, cfg ViewportConfig) ViewportState {
	if st.ViewWidth <= 0 || st.ContentWidth <= st.ViewWidth {
		st.ScrollX = 0
	} else {
		st.ScrollX = min(max(st.ScrollX, 0), st.ContentWidth-st.ViewWidth)
	}
	if st.Total <= 0 {
		st.Cursor = 0
		st.Offset = 0
		return st
	}
	st.Cursor = min(max(st.Cursor, 0), st.Total-1)
	if cfg.Mode == ViewportModeOffset {
		st.Offset = min(max(st.Offset, 0), viewportMaxOffset(st, cfg))
		return st
	}
	if st.Height <= 0 {
		st.Offset = 0
		return st
	}
	st.Offset = core.ViewportClamp(st.Offset, st.Cursor, st.Total, st.Height, cfg.ScrollOff)
	return st
}

// ViewportReveal minimally adjusts Offset so targetRow (with ScrollOff margin)
// is visible inside the viewport.
func ViewportReveal(st ViewportState, cfg ViewportConfig, targetRow int) ViewportState {
	st = ViewportClamp(st, cfg)
	if st.Total <= 0 || st.Height <= 0 {
		return st
	}
	targetRow = min(max(targetRow, 0), st.Total-1)
	pad := viewportPad(st.Height, cfg.ScrollOff)
	if targetRow < st.Offset+pad {
		st.Offset = targetRow - pad
	} else if targetRow >= st.Offset+st.Height-pad {
		st.Offset = targetRow - st.Height + 1 + pad
	}
	st.Offset = min(max(st.Offset, 0), viewportMaxOffset(st, cfg))
	return st
}

// ViewportStep applies any of the 10 standard vertical motion commands
// (motion.down, motion.up, motion.top, motion.bottom, motion.half.down,
// motion.half.up, motion.page.down, motion.page.up, motion.scroll.down,
// motion.scroll.up) to st under cfg.
func ViewportStep(st ViewportState, cfg ViewportConfig, cmd core.Command) (next ViewportState, moved, handled bool) {
	switch cmd.ID {
	case "motion.down", "motion.up", "motion.top", "motion.bottom",
		"motion.half.down", "motion.half.up",
		"motion.page.down", "motion.page.up",
		"motion.scroll.down", "motion.scroll.up":
	default:
		return st, false, false
	}

	cur := ViewportClamp(st, cfg)
	if cur.Total <= 0 {
		return cur, false, true
	}

	n := cmd.Times()
	h := max(cur.Height, 1)
	half := max(h/2, 1)
	pageStep := max(h-max(cfg.PageOverlap, 0), 1)
	next = cur

	if cfg.Mode == ViewportModeOffset {
		maxOff := viewportMaxOffset(cur, cfg)
		switch cmd.ID {
		case "motion.down", "motion.scroll.down":
			if cfg.Wrap && maxOff > 0 {
				next.Offset = wrapIndex(cur.Offset+n, maxOff+1)
			} else {
				next.Offset = cur.Offset + n
			}
		case "motion.up", "motion.scroll.up":
			if cfg.Wrap && maxOff > 0 {
				next.Offset = wrapIndex(cur.Offset-n, maxOff+1)
			} else {
				next.Offset = cur.Offset - n
			}
		case "motion.top":
			next.Offset = 0
		case "motion.bottom":
			next.Offset = max(0, cur.Total-h)
		case "motion.half.down":
			next.Offset = cur.Offset + n*half
		case "motion.half.up":
			next.Offset = cur.Offset - n*half
		case "motion.page.down":
			next.Offset = cur.Offset + n*pageStep
		case "motion.page.up":
			next.Offset = cur.Offset - n*pageStep
		}
		next = ViewportClamp(next, cfg)
		return next, next.Offset != cur.Offset || next.Cursor != cur.Cursor, true
	}

	last := cur.Total - 1
	switch cmd.ID {
	case "motion.down":
		if cfg.Wrap {
			next.Cursor = wrapIndex(cur.Cursor+n, cur.Total)
		} else {
			next.Cursor = min(max(cur.Cursor+n, 0), last)
		}
		next = ViewportClamp(next, cfg)
	case "motion.up":
		if cfg.Wrap {
			next.Cursor = wrapIndex(cur.Cursor-n, cur.Total)
		} else {
			next.Cursor = min(max(cur.Cursor-n, 0), last)
		}
		next = ViewportClamp(next, cfg)
	case "motion.top":
		next.Cursor = 0
		next = ViewportClamp(next, cfg)
	case "motion.bottom":
		next.Cursor = last
		next = ViewportClamp(next, cfg)
	case "motion.half.down":
		next.Cursor = min(max(cur.Cursor+n*half, 0), last)
		next = ViewportClamp(next, cfg)
	case "motion.half.up":
		next.Cursor = min(max(cur.Cursor-n*half, 0), last)
		next = ViewportClamp(next, cfg)
	case "motion.page.down":
		next.Cursor = min(max(cur.Cursor+n*pageStep, 0), last)
		next = ViewportClamp(next, cfg)
	case "motion.page.up":
		next.Cursor = min(max(cur.Cursor-n*pageStep, 0), last)
		next = ViewportClamp(next, cfg)
	case "motion.scroll.down":
		next.Offset, next.Cursor = core.ViewportScroll(cur.Offset, cur.Cursor, cur.Total, h, cfg.ScrollOff, n)
	case "motion.scroll.up":
		next.Offset, next.Cursor = core.ViewportScroll(cur.Offset, cur.Cursor, cur.Total, h, cfg.ScrollOff, -n)
	}
	return next, next.Offset != cur.Offset || next.Cursor != cur.Cursor, true
}

// ViewportFacts derives truthful read-only viewport facts from st under cfg.
func ViewportFacts(st ViewportState, cfg ViewportConfig) map[string]any {
	st = ViewportClamp(st, cfg)
	empty := st.Total <= 0
	var atTop, atBottom bool
	if cfg.Mode == ViewportModeOffset {
		atTop = empty || st.Offset <= 0
		atBottom = empty || st.Offset >= viewportMaxOffset(st, cfg)
	} else {
		atTop = empty || (st.Cursor <= 0 && st.Offset <= 0)
		atBottom = empty || st.Cursor >= st.Total-1
	}
	atLeft := st.ScrollX <= 0
	atRight := st.ViewWidth <= 0 || st.ContentWidth <= st.ViewWidth || st.ScrollX >= st.ContentWidth-st.ViewWidth
	return map[string]any{
		"atTop":      atTop,
		"atBottom":   atBottom,
		"atLeft":     atLeft,
		"atRight":    atRight,
		"empty":      empty,
		"scrollable": st.Height > 0 && st.Total > st.Height,
		"offset":     st.Offset,
		"cursor":     st.Cursor,
		"total":      st.Total,
		"height":     st.Height,
	}
}

// MergeFacts copies all key-value pairs from src into dst, allocating dst if
// nil, and returns dst.
func MergeFacts(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = make(map[string]any, len(src))
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
