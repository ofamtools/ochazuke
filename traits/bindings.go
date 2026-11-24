package traits

import (
	"strings"

	"ofam.dev/ochazuke/core"
)

// Canonical trait names declared on ui.ViewNode.Traits.
const (
	TraitScroll  = "scroll"
	TraitNav     = "nav"
	TraitFilter  = "filter"
	TraitFold    = "fold"
	TraitSpill   = "spill"
	TraitDismiss = "dismiss"
)

// TraitScope returns the canonical scope name for a trait ("trait.<name>").
// It is idempotent if name already carries the "trait." prefix, and returns ""
// when name is empty.
func TraitScope(name string) string {
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "trait.") {
		return name
	}
	return "trait." + name
}

// DefaultTraitBindings returns the default zero-g-prefix bindings for the
// named trait in mode, scoped to TraitScope(traitName). Unknown trait names
// return nil.
func DefaultTraitBindings(mode, traitName string) []core.Binding {
	name := strings.TrimPrefix(traitName, "trait.")
	scope := TraitScope(name)
	if scope == "" {
		return nil
	}
	bind := func(keys, cmd, hint string, hidden bool) core.Binding {
		return core.Binding{
			Scope:   scope,
			Mode:    mode,
			Keys:    strings.Fields(keys),
			Command: cmd,
			Hint:    hint,
			Hidden:  hidden,
		}
	}
	switch name {
	case TraitScroll:
		return []core.Binding{
			bind("ctrl+e", "motion.scroll.down", "Scroll down", false),
			bind("ctrl+y", "motion.scroll.up", "Scroll up", false),
			bind("ctrl+d", "motion.half.down", "Half page down", false),
			bind("ctrl+u", "motion.half.up", "Half page up", false),
			bind("ctrl+f", "motion.page.down", "Page down", false),
			bind("ctrl+b", "motion.page.up", "Page up", false),
		}
	case TraitNav:
		return []core.Binding{
			bind("j", "motion.down", "Down", false),
			bind("down", "motion.down", "Down", true),
			bind("k", "motion.up", "Up", false),
			bind("up", "motion.up", "Up", true),
			bind("T", "motion.top", "Top", false),
			bind("home", "motion.top", "Top", true),
			bind("G", "motion.bottom", "Bottom", false),
			bind("end", "motion.bottom", "Bottom", true),
		}
	case TraitFold:
		return []core.Binding{
			bind("space", "tree.fold.toggle", "Toggle fold", false),
			bind("z a", "tree.fold.toggle", "Toggle fold", false),
			bind("z o", "tree.fold.open", "Open fold", false),
			bind("z c", "tree.fold.close", "Close fold", false),
			bind("z M", "tree.fold.close.all", "Close all folds", false),
			bind("z R", "tree.fold.open.all", "Open all folds", false),
		}
	case TraitFilter:
		return []core.Binding{
			bind("/", "filter.start", "Filter", false),
		}
	default:
		return nil
	}
}
