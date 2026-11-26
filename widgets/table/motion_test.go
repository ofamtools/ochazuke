package table

import (
	"testing"

	"ofam.dev/ochazuke/traits"
)

func TestMotionNodesDrawingOrder(t *testing.T) {
	m := sample()
	m.GroupField = "kind"
	items, nodes := m.Items(), m.MotionNodes()
	if len(nodes) != len(items) {
		t.Fatalf("nodes %d, items %d", len(nodes), len(items))
	}
	for i, n := range nodes {
		if n.Span.Y != i || n.Object(traits.ObjectBlock) != "group:"+items[i].Group {
			t.Fatalf("node %d = %+v, item %+v", i, n, items[i])
		}
		if want := items[i].Row.ID; !items[i].Header && (n.ID != want || n.Object(traits.ObjectRow) != want) {
			t.Fatalf("row node %d = %+v, want id/row %q", i, n, want)
		}
	}
	m.GroupField = ""
	for _, n := range m.MotionNodes() {
		if n.Object(traits.ObjectBlock) != "" {
			t.Fatalf("ungrouped node has a block: %+v", n)
		}
	}
}
