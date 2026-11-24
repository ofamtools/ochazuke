package ui

import (
	"strings"

	"ofam.dev/ochazuke/core"
)

// ContextChain derives the modal context chain for the focused node: for each
// ancestor (innermost first), the node's own frame (when Scope or Ref is set)
// followed by one frame per declared trait ("trait.<name>"), then the app frame.
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
				if n.Scope == "" && n.Ref == nil && len(n.Traits) == 0 {
					continue
				}
				if n.Scope != "" || n.Ref != nil {
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
				if len(n.Traits) > 0 {
					seen := make(map[string]bool, len(n.Traits))
					for _, tr := range n.Traits {
						if tr == "" {
							continue
						}
						scope := tr
						if !strings.HasPrefix(scope, "trait.") {
							scope = "trait." + scope
						}
						if seen[scope] {
							continue
						}
						seen[scope] = true
						var subjectCopy *core.Ref
						if n.Ref != nil {
							ref := *n.Ref
							subjectCopy = &ref
						}
						chain = append(chain, core.ContextFrame{
							Scope:   scope,
							NodeID:  n.ID,
							Subject: subjectCopy,
							Facts:   n.Facts,
						})
					}
				}
			}
		}
	}
	return append(chain, core.ContextFrame{Scope: "app", Facts: appFacts})
}
