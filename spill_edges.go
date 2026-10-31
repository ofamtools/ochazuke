package ochazuke

import (
	"slices"
	"strings"
)

// SpillEdge is one edge a list-like component's motions can spill past
// (move the focus to the neighbouring stop): the rows' first and last, and
// a table's first and last column. Every component that spills (a list, a
// table, a tree's list, a timeline's cards and steps) names its edges with
// this one set, so list.no-spill traps each of the four independently and
// no component misses a direction.
type SpillEdge string

const (
	// EdgeFirst is before the first row: k, up (a count from the edge).
	EdgeFirst SpillEdge = "first"
	// EdgeLast is past the last row: j, down; a timeline's last step.
	EdgeLast SpillEdge = "last"
	// EdgeFirstColumn is before a table's first column (h, left).
	EdgeFirstColumn SpillEdge = "first-col"
	// EdgeLastColumn is past a table's last column (l, right).
	EdgeLastColumn SpillEdge = "last-col"
)

// SpillEdges is every edge, the suffixes a list.no-spill entry takes
// (ID:last, ID:first-col, ...). An entry without one names all four. A
// component with no horizontal motion (a plain list, a timeline) accepts
// the column edges as a no-op: it never spills sideways.
var SpillEdges = []SpillEdge{EdgeFirst, EdgeLast, EdgeFirstColumn, EdgeLastColumn}

// rowEdge is the edge a vertical motion of delta rows leaves by.
func rowEdge(delta int) SpillEdge {
	if delta < 0 {
		return EdgeFirst
	}
	return EdgeLast
}

// columnEdge is the edge a horizontal motion in direction step leaves by.
func columnEdge(step int) SpillEdge {
	if step < 0 {
		return EdgeFirstColumn
	}
	return EdgeLastColumn
}

// ParseNoSpillEntry splits a list.no-spill entry into the id (or prefix
// ending in *) it names and the edge it traps: "panes/working:last" is
// panes/working at its last row; "panes/*:first-col" a prefix at its first
// column. An entry without a known edge suffix names every edge (edge
// ""); an unknown suffix stays part of the id, so it names nothing that
// exists rather than a wrong edge.
func ParseNoSpillEntry(entry string) (name string, edge SpillEdge) {
	entry = strings.TrimSpace(entry)
	if i := strings.LastIndex(entry, ":"); i >= 0 {
		if e := SpillEdge(strings.TrimSpace(entry[i+1:])); slices.Contains(SpillEdges, e) {
			return strings.TrimSpace(entry[:i]), e
		}
	}
	return entry, ""
}

// noSpillMatches reports whether a list.no-spill name (an id, or a prefix
// ending in *) names id.
func noSpillMatches(name, id string) bool {
	if prefix, ok := strings.CutSuffix(name, "*"); ok {
		return strings.HasPrefix(id, prefix)
	}
	return name == id
}
