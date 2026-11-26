package diff

import (
	"testing"

	"ofam.dev/ochazuke/traits"
)

func TestMotionNodesDrawingOrder(t *testing.T) {
	m := sample()
	rows, nodes := m.Rows(), m.MotionNodes()
	if len(nodes) != len(rows) {
		t.Fatalf("nodes %d, rows %d", len(nodes), len(rows))
	}
	for i, n := range nodes {
		if n.Span.Y != i || (n.Object(traits.ObjectBlock) == "") != (rows[i].Kind == RowSection) || n.Object(traits.ObjectSection) == "" {
			t.Fatalf("node %d = %+v, row %+v", i, n, rows[i])
		}
	}
	// `{` from the second file's header lands on the first file's last hunk header.
	var lastHunk, second int
	for i, r := range rows {
		if r.Kind == RowHunk && r.Section == 0 {
			lastHunk = i
		}
		if r.Kind == RowSection && r.Section == 1 {
			second = i
		}
	}
	if lastHunk == 0 || second <= lastHunk {
		t.Fatalf("fixture: last hunk of file 0 at %d, file 1 header at %d", lastHunk, second)
	}
	m.Cursor = second
	m.HandleCommand(cmd("motion.block.prev", 0))
	if m.Cursor != lastHunk {
		t.Fatalf("prev from file header: %d want %d", m.Cursor, lastHunk)
	}
}
