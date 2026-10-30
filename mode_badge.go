package ochazuke

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ModeBadgeKeyCaseSetting is the framework setting that sets the case of
// the keys a pending mode holds in the mode badge (R2.10): upper (the
// default, NORMAL G for both g and G) or as-typed (NORMAL g, NORMAL G,
// LEADER PENDING ctrl+a). The mode's name is upper case either way. A row
// of settings/ochazuke.json, read at every frame.
const ModeBadgeKeyCaseSetting = "mode-badge.key-case"

// The mode-badge.key-case values.
const (
	ModeBadgeKeysUpper   = "upper"
	ModeBadgeKeysAsTyped = "as-typed"
)

// ModeBadgeKeyCases are mode-badge.key-case's values.
var ModeBadgeKeyCases = []string{ModeBadgeKeysUpper, ModeBadgeKeysAsTyped}

// ModeBadgeKeyCase is the mode-badge.key-case value, upper when it is
// undeclared or holds anything else.
func (r *Runtime) ModeBadgeKeyCase() string {
	if v := r.Settings().String(ModeBadgeKeyCaseSetting); slices.Contains(ModeBadgeKeyCases, v) {
		return v
	}
	return ModeBadgeKeysUpper
}

// ModeBadgeWidthSetting is the framework setting that sets the cells the
// mode badge reserves (R2.10): widest (the default) pads every label to the
// widest base mode's name, so the badge keeps both edges as the mode
// changes; fit draws each label in its own cells, so only its right edge
// stays. Either way the badge is the right end of the title row. A row of
// settings/ochazuke.json, read at every frame.
const ModeBadgeWidthSetting = "mode-badge.width"

// The mode-badge.width values.
const (
	ModeBadgeWidthWidest = "widest"
	ModeBadgeWidthFit    = "fit"
)

// ModeBadgeWidths are mode-badge.width's values.
var ModeBadgeWidths = []string{ModeBadgeWidthWidest, ModeBadgeWidthFit}

// ModeBadgeWidth is the cells the mode badge's label reserves
// (Header.BadgeWidth): with mode-badge.width widest (the default, or any
// value it does not know), the widest label of a registered base mode
// (Runtime.Modes, as the badge names it); 0 with fit. Pending modes are left
// out: they show the keys they hold, so no width fits them, and a longer
// label grows the badge leftward from the same right edge.
func (r *Runtime) ModeBadgeWidth() int {
	if r.Settings().String(ModeBadgeWidthSetting) == ModeBadgeWidthFit {
		return 0
	}
	w := 0
	for name := range r.Modes {
		w = max(w, ansi.StringWidth(strings.ToUpper(modeLabelOf(name))))
	}
	return w
}
