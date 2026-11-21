// Package ui defines the single ViewNode schema shared by static views.json
// (Level 1), dynamic tool/agent UIs (Level 2) and native Go views (Level 3),
// derives the modal context chain from a ViewNode tree, and renders trees to
// terminal frames with a semantic theme and a subtree buffer cache.
//
// ui imports only the standard library, core, layout, buffer and lipgloss.
package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/layout"
)

// Supported ViewNode.Kind values.
const (
	KindVStack   = "vstack"
	KindHStack   = "hstack"
	KindSplit    = "split"
	KindGrid     = "grid"
	KindTabs     = "tabs"
	KindPanel    = "panel"
	KindScroll   = "scroll"
	KindOverlay  = "overlay"
	KindCard     = "card"
	KindText     = "text"
	KindMarkdown = "markdown"
	KindTable    = "table"
	KindTree     = "tree"
	KindInput    = "input"
	KindForm     = "form"
	KindPicker   = "picker"
	KindTerminal = "terminal"
	KindRaw      = "raw"
)

// ToneSpan styles a rune range of a text node with a semantic tone.
type ToneSpan struct {
	From int    `json:"from"`         // rune offset
	To   int    `json:"to,omitempty"` // 0 = end of string
	Tone string `json:"tone"`         // semantic tone name: "accent", "muted", "unread", "state.working", "error", ...
	Bg   string `json:"bg,omitempty"` // optional background tone
}

// Badge is a short labelled chip shown on cards, panels and tabs.
type Badge struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

// ActionHint is an explicit, selectable action attached to a node.
type ActionHint struct {
	Label   string         `json:"label"`
	Key     string         `json:"key,omitempty"`
	Command string         `json:"command"`
	Args    map[string]any `json:"args,omitempty"`
}

// ViewNode is plain JSON-serializable data describing one UI node. Domain
// records never appear in the tree; a node only carries an optional core.Ref.
type ViewNode struct {
	// Identity and modal context.
	ID        string         `json:"id,omitempty"`
	Kind      string         `json:"kind"`
	Scope     string         `json:"scope,omitempty"`
	Ref       *core.Ref      `json:"ref,omitempty"`
	Facts     map[string]any `json:"facts,omitempty"`
	Focusable bool           `json:"focusable,omitempty"`

	// Presentation metadata.
	Title    string  `json:"title,omitempty"`
	Tone     string  `json:"tone,omitempty"`
	Unread   bool    `json:"unread,omitempty"`
	Badges   []Badge `json:"badges,omitempty"`
	Border   bool    `json:"border,omitempty"`
	Padding  [4]int  `json:"padding,omitempty"` // top, right, bottom, left
	CacheKey uint64  `json:"cacheKey,omitempty"`

	// Layout properties for containers.
	Ratio   float64            `json:"ratio,omitempty"`
	Cols    int                `json:"cols,omitempty"`
	MinColW int                `json:"minColW,omitempty"`
	Gap     int                `json:"gap,omitempty"`
	Box     layout.Constraints `json:"box,omitempty"`

	// Kind-specific payload and children.
	Props    map[string]any `json:"props,omitempty"`
	Actions  []ActionHint   `json:"actions,omitempty"`
	Children []ViewNode     `json:"children,omitempty"`
}

type boxJSON struct {
	MinW   int `json:"minW,omitempty"`
	MaxW   int `json:"maxW,omitempty"`
	MinH   int `json:"minH,omitempty"`
	MaxH   int `json:"maxH,omitempty"`
	FixedW int `json:"fixedW,omitempty"`
	FixedH int `json:"fixedH,omitempty"`
	Flex   int `json:"flex,omitempty"`
}

// MarshalJSON omits zero padding and zero box constraints and writes the
// constraints with the same camelCase keys the schema documents.
func (n ViewNode) MarshalJSON() ([]byte, error) {
	type plain ViewNode
	aux := struct {
		plain
		Padding *[4]int  `json:"padding,omitempty"`
		Box     *boxJSON `json:"box,omitempty"`
	}{plain: plain(n)}
	if n.Padding != ([4]int{}) {
		p := n.Padding
		aux.Padding = &p
	}
	if n.Box != (layout.Constraints{}) {
		c := n.Box
		aux.Box = &boxJSON{c.MinW, c.MaxW, c.MinH, c.MaxH, c.FixedW, c.FixedH, c.Flex}
	}
	return json.Marshal(aux)
}

// Find returns the node with the given non-empty ID in the subtree, or nil.
func (n *ViewNode) Find(id string) *ViewNode {
	if id == "" {
		return nil
	}
	var path []*ViewNode
	if findPath(n, id, &path) {
		return path[len(path)-1]
	}
	return nil
}

// findPath appends the nodes from root to the node with the given ID (inclusive).
func findPath(n *ViewNode, id string, out *[]*ViewNode) bool {
	if n.ID == id {
		*out = append(*out, n)
		return true
	}
	for i := range n.Children {
		if findPath(&n.Children[i], id, out) {
			*out = append([]*ViewNode{n}, *out...)
			return true
		}
	}
	return false
}

// --- props access -----------------------------------------------------------

func (n *ViewNode) prop(key string) (any, bool) {
	if n.Props == nil {
		return nil, false
	}
	v, ok := n.Props[key]
	return v, ok
}

func (n *ViewNode) propString(key string) string {
	v, _ := n.prop(key)
	return anyString(v)
}

func (n *ViewNode) propInt(key string) (int, bool) {
	v, ok := n.prop(key)
	if !ok {
		return 0, false
	}
	return anyInt(v)
}

func (n *ViewNode) propBool(key string) bool {
	v, _ := n.prop(key)
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true"
	}
	return false
}

func anyString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case fmt.Stringer:
		return x.String()
	}
	return fmt.Sprint(v)
}

func anyInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		i, err := x.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(x)
		return i, err == nil
	}
	return 0, false
}

// decodeAny converts v to T by type assertion, falling back to a JSON round trip
// so props built in Go and props parsed from JSON behave the same.
func decodeAny[T any](v any) (T, bool) {
	var zero T
	if v == nil {
		return zero, false
	}
	if t, ok := v.(T); ok {
		return t, true
	}
	b, err := json.Marshal(v)
	if err != nil {
		return zero, false
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return zero, false
	}
	return out, true
}

func propDecode[T any](n *ViewNode, key string) (T, bool) {
	v, _ := n.prop(key)
	return decodeAny[T](v)
}

// toSlice converts any slice or array value to []any.
func toSlice(v any) []any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

func toStrings(v any) []string {
	var out []string
	for _, e := range toSlice(v) {
		out = append(out, anyString(e))
	}
	return out
}

// --- tree identity ----------------------------------------------------------

// Synthetic node IDs are "~" followed by a child-index path ("~/0/2") and an
// optional "#part" suffix. They exist only inside the layout solver; Render
// strips them from the returned layout.Solved (HitTest may still report one).

// IsSyntheticID reports whether id was generated by the renderer.
func IsSyntheticID(id string) bool {
	return id == "~" || strings.HasPrefix(id, "~/") || strings.HasPrefix(id, "~#")
}

func nodeID(n *ViewNode, path string) string {
	if n.ID != "" {
		return n.ID
	}
	return path
}

// effectiveRoot wraps an overlay root in a vstack so the overlay has a base.
func effectiveRoot(root ViewNode) ViewNode {
	if root.Kind == KindOverlay {
		return ViewNode{Kind: KindVStack, Children: []ViewNode{root}}
	}
	return root
}

// OwnerID maps any ID reported by layout.Solved.HitTest (real or synthetic) to
// the nearest real node ID at or above it in root. It returns "" if none.
func OwnerID(root ViewNode, id string) string {
	if !IsSyntheticID(id) {
		return id
	}
	if i := strings.IndexByte(id, '#'); i >= 0 {
		id = id[:i]
	}
	eff := effectiveRoot(root)
	cur := &eff
	owner := cur.ID
	for _, seg := range strings.Split(strings.TrimPrefix(strings.TrimPrefix(id, "~"), "/"), "/") {
		if seg == "" {
			continue
		}
		idx, err := strconv.Atoi(seg)
		if err != nil || idx < 0 || idx >= len(cur.Children) {
			break
		}
		cur = &cur.Children[idx]
		if cur.ID != "" {
			owner = cur.ID
		}
	}
	return owner
}
