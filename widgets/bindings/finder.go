// Package bindings provides a live keybinding editor, contextual help, and
// suggestions for unused key sequences. All operations are in memory.
package bindings

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
)

const letters = "abcdefghijklmnopqrstuvwxyz"
const symbols = ".,;:/?!@#$%&*+-_=~[]{}()<>|"

// DefaultReserved returns the default terminal/framework reservations.
func DefaultReserved() []string {
	keys := []string{"ctrl+c", "ctrl+g"}
	for i := 1; i <= 12; i++ {
		keys = append(keys, "f"+strconv.Itoa(i))
	}
	return keys
}

func reservedKey(key string, reserved []string) bool {
	for _, r := range reserved {
		// Printable key names preserve case. Named keys and modifiers
		// accept the display spelling F1 as well as the wire spelling f1.
		if r == key || utf8.RuneCountInString(key) > 1 && strings.EqualFold(r, key) {
			return true
		}
	}
	return false
}

// FindFreeBinding searches the focused scope, app and wildcard bindings. Pass
// nil for the default reservations, or a replacement list (including empty).
func FindFreeBinding(modes []core.Mode, bindings []core.Binding, scope, mode string, isInsert bool, reserved []string) ([]string, bool) {
	return FindFreeBindingInChain(modes, bindings, []core.ContextFrame{{Scope: scope}}, mode, isInsert, reserved)
}

// FindFreeBindingInChain also considers arbitrary parent scopes from a live
// context chain. Conditional bindings count as occupied in every context where
// their scope and mode apply. A candidate never consumes an existing longer
// sequence's prefix. A shorter exact command can still be extended: core holds
// that command as the prefix fallback. A layer trigger already opens another
// map, so its key is not offered as a chord prefix.
func FindFreeBindingInChain(modes []core.Mode, bindings []core.Binding, chain []core.ContextFrame, mode string, isInsert bool, reserved []string) ([]string, bool) {
	km, _ := core.NewKeymap(modes, nil)
	active := km.ModeChain(mode)
	scopes := map[string]bool{"": true, "app": true}
	for _, f := range chain {
		scopes[f.Scope] = true
	}
	if reserved == nil {
		reserved = DefaultReserved()
	}

	var used []core.Binding
	for _, b := range bindings {
		if scopes[b.Scope] && (b.Mode == "" || slices.Contains(active, b.Mode)) {
			used = append(used, b)
		}
	}
	free := func(keys []string) bool {
		for _, key := range keys {
			if reservedKey(key, reserved) {
				return false
			}
		}
		for _, b := range used {
			if b.Class != "" && core.ClassMatches(b.Class, keys[0]) || startsWith(b.Keys, keys) {
				return false
			}
			if b.Layer != nil && startsWith(keys, b.Keys) {
				return false
			}
		}
		return true
	}

	// Families are tried in order, letters in alphabetical order.
	type family struct{ prefix, modifier, chars string }
	var families []family
	if !isInsert {
		families = append(families, family{chars: letters}, family{chars: strings.ToUpper(letters)}, family{chars: symbols}, family{prefix: "g", chars: letters})
	}
	families = append(families, family{modifier: "ctrl+", chars: letters}, family{modifier: "alt+", chars: letters})
	if isInsert {
		families = append(families, family{prefix: "ctrl+a", chars: letters})
	}
	for _, f := range families {
		for _, r := range f.chars {
			keys := []string{f.modifier + string(r)}
			if f.prefix != "" {
				keys = []string{f.prefix, keys[0]}
			}
			if free(keys) {
				return keys, true
			}
		}
	}
	return nil, false
}

func startsWith(sequence, prefix []string) bool {
	return len(prefix) > 0 && len(sequence) >= len(prefix) && slices.Equal(sequence[:len(prefix)], prefix)
}
