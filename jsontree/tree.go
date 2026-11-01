package jsontree

import (
	"ofam.dev/ochazuke"
	"strconv"
	"strings"
)

// TreeNodes adapts a document to read-only framework tree rows. IDs use
// prefix followed by the unique jq path; Data holds the corresponding Node.
// Columns are path, JSON type and child count. A nil root produces no rows.
func TreeNodes(root *Node, prefix string) []ochazuke.TreeNode {
	var rows []ochazuke.TreeNode
	for _, n := range Flatten(root) {
		label, parent, count := ".", "", ""
		if n.Parent != nil {
			label, parent = strings.TrimPrefix(step(n), "."), prefix+n.Parent.Path()
		}
		if n.IsContainer() {
			count = strconv.Itoa(len(n.Children))
		}
		rows = append(rows, ochazuke.TreeNode{ID: prefix + n.Path(), Parent: parent, Label: label, Data: n, Cells: []string{label, string(n.Kind), count}})
	}
	return rows
}
