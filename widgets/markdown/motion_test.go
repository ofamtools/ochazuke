package markdown

import (
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
)

func TestMotionNodesDrawingOrder(t *testing.T) {
	m := New("Doc", doc)
	m.PageSize = 5
	m.scrollTo(5) // inside "Install" (line 3)
	nodes := m.MotionNodes()
	var ids []string
	for i, n := range nodes {
		ids = append(ids, n.ID)
		if i > 0 && n.Span.Y <= nodes[i-1].Span.Y {
			t.Fatalf("node %d (%+v) not after node %d", i, n, i-1)
		}
	}
	if got := ids; len(got) != 5 || got[2] != ViewportNodeID || nodes[2].Object(traits.ObjectBlock) != "heading:1" ||
		nodes[3].Object(traits.ObjectSection) != "heading:2" || nodes[3].Object(traits.ObjectParent) != "heading:0" {
		t.Fatalf("nodes %+v", nodes)
	}

	// `{` from mid-section returns to the current heading first, as before.
	do(t, m, cmd("motion.block.prev", 0))
	if m.Offset != 3 || m.SelectedHeading != 1 {
		t.Fatalf("prev from mid-section: offset %d sel %d", m.Offset, m.SelectedHeading)
	}
	// motion.same.next walks sibling headings: Install -> Usage.
	do(t, m, core.Command{ID: traits.CmdMotionSameNext})
	if m.Offset != 9 {
		t.Fatalf("same.next from Install: offset %d", m.Offset)
	}
}
