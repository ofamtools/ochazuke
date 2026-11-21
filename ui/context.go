package ui

import "ofam.dev/ochazuke/core"

// ContextChain derives the modal context chain for the focused node: one frame
// per ancestor (innermost first) that has a Scope or a Ref, then the app frame.
//
// A frame's Scope is the node's Scope, or its Ref.Kind when only a Ref is set.
// Heterogeneous cards in one feed therefore yield different inner frames with
// no per-row registration. An unknown or empty focusID yields only the app frame.
func ContextChain(root ViewNode, focusID string, appFacts map[string]any) []core.ContextFrame {
	var chain []core.ContextFrame
	if focusID != "" {
		var path []*ViewNode
		if findPath(&root, focusID, &path) {
			for i := len(path) - 1; i >= 0; i-- {
				n := path[i]
				if n.Scope == "" && n.Ref == nil {
					continue
				}
				f := core.ContextFrame{Scope: n.Scope, NodeID: n.ID, Facts: n.Facts}
				if n.Ref != nil {
					ref := *n.Ref
					f.Subject = &ref
					if f.Scope == "" {
						f.Scope = ref.Kind
					}
				}
				chain = append(chain, f)
			}
		}
	}
	return append(chain, core.ContextFrame{Scope: "app", Facts: appFacts})
}
