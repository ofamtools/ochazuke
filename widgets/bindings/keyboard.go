package bindings

import (
	"cmp"
	"slices"
	"strings"

	"ofam.dev/ochazuke/core"
)

// PrefixLayer describes the exact sequences that end at or extend a prefix.
// Counts are declaration count and distinct immediate sub-prefix count.
type PrefixLayer struct {
	Prefix         string
	Label          string
	LastKey        string
	Depth          int
	DirectCount    int
	SubPrefixCount int
}

// PhysicalKey identifies a key by its unshifted and shifted input tokens.
// Legend is the compact keycap label for named keys.
type PhysicalKey struct {
	Key, ShiftedKey, Legend string
}

// DefaultKeyboardRows returns the US QWERTY rows in physical order, followed
// by the named keys. The result belongs to the caller.
func DefaultKeyboardRows() [][]PhysicalKey {
	pairs := [][2]string{
		{"`1234567890-=", "~!@#$%^&*()_+"},
		{"qwertyuiop[]\\", "QWERTYUIOP{}|"},
		{"asdfghjkl;'", `ASDFGHJKL:"`},
		{"zxcvbnm,./", "ZXCVBNM<>?"},
	}
	rows := make([][]PhysicalKey, 0, len(pairs)+1)
	for _, pair := range pairs {
		row := make([]PhysicalKey, len(pair[0]))
		for i := range row {
			row[i] = PhysicalKey{Key: string(pair[0][i]), ShiftedKey: string(pair[1][i])}
		}
		rows = append(rows, row)
	}
	return append(rows, []PhysicalKey{
		{Key: "esc", Legend: "Esc"}, {Key: "tab", Legend: "Tab"}, {Key: "enter", Legend: "Ent"},
		{Key: "space", Legend: "␣"}, {Key: "backspace", Legend: "⌫"},
		{Key: "left", Legend: "←"}, {Key: "down", Legend: "↓"}, {Key: "up", Legend: "↑"}, {Key: "right", Legend: "→"},
	})
}

// KeyAction is a declared exact binding. Several declarations can share a
// stroke across scopes, modes and guards.
type KeyAction struct {
	Command string
	Hint    string
	Scope   string
	Mode    string
	When    string
	Unbind  bool
}

func (a KeyAction) label() string {
	if a.Unbind {
		return "unbound"
	}
	return cmp.Or(a.Hint, a.Command)
}

// Keycap describes a single stroke at the active prefix and Shift setting.
// ChildCount counts the distinct strokes available after entering NextPrefix.
type Keycap struct {
	PhysicalKey string
	Key         string
	Legend      string
	Tone        string
	Actions     []KeyAction
	NextPrefix  string
	ChildCount  int
	HasShift    bool
}

// KeyboardSpec is a snapshot of the bindings editor's keyboard presentation.
// OtherKeys contains strokes without a keycap in the US QWERTY rows.
type KeyboardSpec struct {
	ActivePrefix PrefixLayer
	Prefixes     []PrefixLayer
	Rows         [][]Keycap
	OtherKeys    []Keycap
	Shifted      bool
	Empty        bool
}

type prefixNode struct {
	prefix  PrefixLayer
	keys    map[string]*prefixNode
	actions []KeyAction
}

type keyboardIndex struct {
	nodes  map[string]*prefixNode
	layers []PrefixLayer
}

func newKeyboardIndex(entries []core.Binding) keyboardIndex {
	base := &prefixNode{prefix: PrefixLayer{Label: "Base"}}
	index := keyboardIndex{nodes: map[string]*prefixNode{"": base}}
	for _, b := range entries {
		if len(b.Keys) == 0 || slices.Contains(b.Keys, "") {
			continue
		}
		node := base
		for _, key := range b.Keys {
			node = node.addKey(key)
			index.nodes[node.prefix.Prefix] = node
		}
		node.actions = append(node.actions, KeyAction{Command: bindingTarget(b), Hint: b.Hint, Scope: b.Scope, Mode: b.Mode, When: b.When, Unbind: b.Unbind})
	}
	index.layers = []PrefixLayer{base.layer()}
	for prefix, node := range index.nodes {
		if prefix != "" && len(node.keys) > 0 {
			index.layers = append(index.layers, node.layer())
		}
	}
	slices.SortFunc(index.layers[1:], comparePrefixes)
	return index
}

func (n *prefixNode) addKey(key string) *prefixNode {
	if child := n.keys[key]; child != nil {
		return child
	}
	prefix := key
	if n.prefix.Depth > 0 {
		prefix = n.prefix.Prefix + " " + key
	}
	child := &prefixNode{prefix: PrefixLayer{Prefix: prefix, Label: prefix, LastKey: key, Depth: n.prefix.Depth + 1}}
	if n.keys == nil {
		n.keys = make(map[string]*prefixNode)
	}
	n.keys[key] = child
	return child
}

func (n *prefixNode) layer() PrefixLayer {
	layer := n.prefix
	layer.DirectCount = len(n.actions)
	for _, child := range n.keys {
		if len(child.keys) > 0 {
			layer.SubPrefixCount++
		}
	}
	return layer
}

func comparePrefixes(a, b PrefixLayer) int {
	return cmp.Or(
		cmp.Compare(strings.ToLower(a.LastKey), strings.ToLower(b.LastKey)),
		cmp.Compare(a.Depth, b.Depth),
		cmp.Compare(a.Prefix, b.Prefix),
	)
}

// ExtractPrefixes returns Base followed by every non-final sequence prefix,
// sorted by case-insensitive last key, depth and full prefix text.
func ExtractPrefixes(entries []core.Binding) []PrefixLayer {
	return newKeyboardIndex(entries).layers
}

// ProjectKeyboard views the declarations one stroke past activePrefix. An
// unavailable prefix selects Base. Key classes remain in the bindings table.
func ProjectKeyboard(entries []core.Binding, activePrefix string, shifted bool) KeyboardSpec {
	return newKeyboardIndex(entries).project(activePrefix, shifted)
}

func (index keyboardIndex) layerIndex(prefix string) int {
	return slices.IndexFunc(index.layers, func(layer PrefixLayer) bool { return layer.Prefix == prefix })
}

func (index keyboardIndex) project(prefix string, shifted bool) KeyboardSpec {
	layer := index.layers[max(0, index.layerIndex(prefix))]
	node := index.nodes[layer.Prefix]
	spec := KeyboardSpec{ActivePrefix: layer, Prefixes: slices.Clone(index.layers), Shifted: shifted, Empty: true}
	physical := map[string]bool{}
	for _, row := range DefaultKeyboardRows() {
		caps := make([]Keycap, 0, len(row))
		for _, key := range row {
			physical[key.Key], physical[key.ShiftedKey] = true, true
			cap := node.keycap(key, shifted)
			caps = append(caps, cap)
			spec.Empty = spec.Empty && !cap.declared()
		}
		spec.Rows = append(spec.Rows, caps)
	}
	for key := range node.keys {
		if !physical[key] {
			spec.OtherKeys = append(spec.OtherKeys, node.keycap(PhysicalKey{Key: key}, shifted))
		}
	}
	slices.SortFunc(spec.OtherKeys, func(a, b Keycap) int { return cmp.Compare(a.Key, b.Key) })
	spec.Empty = spec.Empty && len(spec.OtherKeys) == 0
	return spec
}

func (n *prefixNode) keycap(physical PhysicalKey, shifted bool) Keycap {
	key := physical.Key
	if shifted && physical.ShiftedKey != "" {
		key = physical.ShiftedKey
	}
	cap := Keycap{
		PhysicalKey: physical.Key, Key: key, Legend: cmp.Or(physical.Legend, key), Tone: "muted",
		HasShift: !shifted && n.keys[physical.ShiftedKey] != nil,
	}
	if stroke := n.keys[key]; stroke != nil {
		cap.Actions = slices.Clone(stroke.actions)
		cap.ChildCount = len(stroke.keys)
		if cap.ChildCount > 0 {
			cap.NextPrefix, cap.Tone = stroke.prefix.Prefix, "warning"
		} else if slices.ContainsFunc(cap.Actions, func(a KeyAction) bool { return !a.Unbind }) {
			cap.Tone = "accent"
		}
	}
	return cap
}

func (cap Keycap) declared() bool { return len(cap.Actions) > 0 || cap.ChildCount > 0 }

func (spec KeyboardSpec) keycap(physical string) (Keycap, bool) {
	for _, row := range spec.Rows {
		for _, cap := range row {
			if cap.PhysicalKey == physical || cap.Key == physical {
				return cap, true
			}
		}
	}
	for _, cap := range spec.OtherKeys {
		if cap.Key == physical {
			return cap, true
		}
	}
	return Keycap{}, false
}
