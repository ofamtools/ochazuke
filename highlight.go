package ochazuke

// moveHighlight shares the initial choice of pickers with a -1 (none)
// cursor. Their existing edge policy remains clamped or wrapping.
func (r *Runtime) moveHighlight(current, delta, count int, wrap bool) int {
	if count == 0 {
		return -1
	}
	if delta == 0 {
		return current
	}
	if current < 0 {
		if delta > 0 {
			return 0
		}
		if r.Settings().String("chooser.up-from-none") == "stay" {
			return -1
		}
		return count - 1
	}
	if wrap {
		return ((current+delta)%count + count) % count
	}
	return current + min(max(delta, -current), count-1-current)
}
