package ochazuke

import "slices"

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
