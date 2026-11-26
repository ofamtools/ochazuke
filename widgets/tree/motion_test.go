package tree

import (
	"testing"

	"ofam.dev/ochazuke/traits"
)

func TestMotionNodesDrawingOrder(t *testing.T) {
	m := sampleTree()
	m.UnfoldAll()
	rows, nodes := m.Flatten(), m.MotionNodes()
	if len(nodes) != len(rows) {
		t.Fatalf("nodes %d, rows %d", len(nodes), len(rows))
	}
	root := ""
	for i, n := range nodes {
		r := rows[i]
		if r.IsRoot {
			root = r.ID
		}
		if n.ID != r.ID || n.Span.Y != r.VisualRow || n.Object(traits.ObjectBlock) != root || n.Object(traits.ObjectParent) != r.ParentID {
			t.Fatalf("node %d = %+v, row %+v (root %q)", i, n, r, root)
		}
		if i > 0 && n.Span.Y <= nodes[i-1].Span.Y {
			t.Fatalf("node %d not below node %d", i, i-1)
		}
	}
}
