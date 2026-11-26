package ui

import (
	"slices"
	"strings"

	"ofam.dev/ochazuke/core"
)

// Ancestors returns the path from the focused node up to n (innermost first),
// or nil when focusID is empty or not present in n's subtree.
func (n *ViewNode) Ancestors(focusID string) []*ViewNode {
	if n == nil || focusID == "" {
		return nil
	}
	var path []*ViewNode
	if !findPath(n, focusID, &path) {
		return nil
	}
	slices.Reverse(path)
	return path
}

// ContextFrames returns the context frames contributed by n alone: its own
// Scope/Ref frame (when set) followed by one deduped "trait.<name>" frame per
// declared trait.
func (n *ViewNode) ContextFrames() []core.ContextFrame {
	if n == nil {
		return nil
	}
	var frames []core.ContextFrame
	if n.Scope != "" || n.Ref != nil {
		frames = append(frames, n.contextFrame(n.ownScope()))
	}
	if len(n.Traits) == 0 {
		return frames
	}
	seen := make(map[string]bool, len(n.Traits))
	for _, tr := range n.Traits {
		scope := normalizeTraitScope(tr)
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		frames = append(frames, n.contextFrame(scope))
	}
	return frames
}

// ContextChain derives the modal context chain for the focused node: for each
// ancestor (innermost first), the node's own frame (when Scope or Ref is set)
// followed by one frame per declared trait ("trait.<name>"), then the app frame.
//
// A frame's Scope is the node's Scope, or its Ref.Kind when only a Ref is set.
// Heterogeneous cards in one feed therefore yield different inner frames with
// no per-row registration. An unknown or empty focusID yields only the app frame.
func ContextChain(root ViewNode, focusID string, appFacts map[string]any) []core.ContextFrame {
	var chain []core.ContextFrame
	for _, n := range root.Ancestors(focusID) {
		chain = append(chain, n.ContextFrames()...)
	}
	return append(chain, core.ContextFrame{Scope: "app", Facts: appFacts})
}

// ResolveFilterSpec walks root.Ancestors(focusID) innermost-first and returns
// the nearest declared FilterSpec together with its effective registry ID
// (spec.ID when non-empty, otherwise the declaring node's ID). Works uniformly
// across pages, trees, tables, show panels, and modals with zero ID switches.
func ResolveFilterSpec(root ViewNode, focusID string) (*FilterSpec, string) {
	for _, n := range root.Ancestors(focusID) {
		if n.Filter == nil {
			continue
		}
		id := n.Filter.ID
		if id == "" {
			id = n.ID
		}
		if id == "" {
			continue
		}
		cp := *n.Filter
		cp.ID = id
		return &cp, id
	}
	return nil, ""
}

func (n *ViewNode) ownScope() string {
	if n.Scope != "" {
		return n.Scope
	}
	if n.Ref != nil {
		return n.Ref.Kind
	}
	return ""
}

func (n *ViewNode) contextFrame(scope string) core.ContextFrame {
	var subject *core.Ref
	if n.Ref != nil {
		ref := *n.Ref
		subject = &ref
	}
	return core.ContextFrame{
		Scope:   scope,
		NodeID:  n.ID,
		Subject: subject,
		Facts:   n.Facts,
	}
}

func normalizeTraitScope(tr string) string {
	if tr == "" {
		return ""
	}
	if strings.HasPrefix(tr, "trait.") {
		return tr
	}
	return "trait." + tr
}
