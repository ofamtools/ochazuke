package modal

import (
	"testing"

	"ofam.dev/ochazuke/ui"
)

func TestApplyEscapeGuardOverlay(t *testing.T) {
	origBadges := make([]ui.Badge, 1, 4)
	origBadges[0] = ui.Badge{Text: "existing", Tone: "accent"}
	base := ui.ViewNode{
		ID:     "modal.edit",
		Kind:   ui.KindOverlay,
		Title:  "Edit",
		Tone:   "accent",
		Badges: origBadges,
	}

	// Inactive leaves node completely untouched.
	inactive := ApplyEscapeGuardOverlay(base, false, "warning", "esc locked")
	if inactive.Tone != "accent" || len(inactive.Badges) != 1 {
		t.Fatalf("inactive overlay mutated: tone=%q badges=%+v", inactive.Tone, inactive.Badges)
	}

	// Active applies tone and appends badge without mutating caller's backing slice.
	armed := ApplyEscapeGuardOverlay(base, true, "border.warn", "esc again to close")
	if armed.Tone != "border.warn" {
		t.Fatalf("armed Tone = %q, want border.warn", armed.Tone)
	}
	if len(armed.Badges) != 2 || armed.Badges[1] != (ui.Badge{Text: "esc again to close", Tone: "border.warn"}) {
		t.Fatalf("armed Badges = %+v", armed.Badges)
	}
	if len(base.Badges) != 1 || origBadges[:2][1] != (ui.Badge{}) {
		t.Fatalf("caller slice was mutated: len=%d backing[1]=%+v", len(base.Badges), origBadges[:2][1])
	}

	// Empty tone defaults to "warning".
	defTone := ApplyEscapeGuardOverlay(base, true, "", "esc locked")
	if defTone.Tone != "warning" || len(defTone.Badges) != 2 || defTone.Badges[1].Tone != "warning" {
		t.Fatalf("default tone overlay = tone:%q badges:%+v", defTone.Tone, defTone.Badges)
	}

	// Empty badge updates tone without appending an empty badge.
	noBadge := ApplyEscapeGuardOverlay(base, true, "warning", "")
	if noBadge.Tone != "warning" || len(noBadge.Badges) != 1 {
		t.Fatalf("empty badge overlay = tone:%q badges:%+v", noBadge.Tone, noBadge.Badges)
	}
}
