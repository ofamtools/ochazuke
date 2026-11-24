package modal

import (
	"slices"

	"ofam.dev/ochazuke/ui"
)

// ApplyEscapeGuardOverlay decorates an overlay ViewNode with a warning border
// tone and optional status badge when an escape guard is active, without
// mutating the caller's Badges slice.
func ApplyEscapeGuardOverlay(node ui.ViewNode, active bool, tone, badge string) ui.ViewNode {
	if !active {
		return node
	}
	if tone == "" {
		tone = "warning"
	}
	node.Tone = tone
	if badge != "" {
		badges := slices.Clone(node.Badges)
		badges = append(badges, ui.Badge{Text: badge, Tone: tone})
		node.Badges = badges
	}
	return node
}
