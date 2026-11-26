package markdown

import (
	"maps"
	"strconv"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
)

// ViewportNodeID is the ID of the MotionNodes entry that stands for the
// viewport top when it is not on a heading line.
const ViewportNodeID = "viewport"

// motionConfig is the reader's object-motion configuration: blocks and
// sections are headings, motion.same.* walks sibling headings (same parent
// heading), no wrap.
var motionConfig = traits.MotionConfig{DefaultObject: traits.ObjectBlock, RelativeObject: traits.ObjectParent}

// motionKinds are the object kinds the reader's MotionNodes carry.
var motionKinds = map[string]bool{traits.ObjectSection: true, traits.ObjectBlock: true, traits.ObjectParent: true}

// MotionNodes returns the reader's motion nodes in document order: one per
// heading (ID "heading:<index>", section=block=<that ID>, parent=<ID of the
// nearest preceding heading of a lower level> when there is one, Span the
// heading's source lines up to the next heading), plus one ViewportNodeID
// node at Offset when the viewport top is not on a heading line. That node
// carries the memberships of the heading above it (none before the first
// heading), so `{` first returns to the current heading and `}` goes to the
// next one, as a cursor would.
func (m *Model) MotionNodes() []traits.MotionNode {
	nodes, _ := m.motionNodes()
	return nodes
}

// motionNodes returns MotionNodes and the index of the node for the
// viewport top (the heading on Offset, or the viewport node).
func (m *Model) motionNodes() ([]traits.MotionNode, int) {
	total := m.total()
	nodes := make([]traits.MotionNode, 0, len(m.Headings)+1)
	cur := -1
	var prev map[string]string
	viewport := func() {
		objs := map[string]string{}
		maps.Copy(objs, prev)
		nodes = append(nodes, traits.MotionNode{ID: ViewportNodeID, Span: traits.ItemSpan{ID: ViewportNodeID, Y: m.Offset, H: 1}, Objects: objs})
		cur = len(nodes) - 1
	}
	type open struct {
		level int
		id    string
	}
	var stack []open
	for i, h := range m.Headings {
		if cur < 0 && h.Line > m.Offset {
			viewport()
		}
		id := "heading:" + strconv.Itoa(i)
		for len(stack) > 0 && stack[len(stack)-1].level >= h.Level {
			stack = stack[:len(stack)-1]
		}
		objs := map[string]string{traits.ObjectSection: id, traits.ObjectBlock: id}
		if len(stack) > 0 {
			objs[traits.ObjectParent] = stack[len(stack)-1].id
		}
		stack = append(stack, open{h.Level, id})
		end := total
		if i+1 < len(m.Headings) {
			end = m.Headings[i+1].Line
		}
		nodes = append(nodes, traits.MotionNode{ID: id, Span: traits.ItemSpan{ID: id, Y: h.Line, H: max(end-h.Line, 1)}, Objects: objs})
		if h.Line == m.Offset {
			cur = len(nodes) - 1
		}
		prev = objs
	}
	if cur < 0 {
		viewport()
	}
	return nodes, cur
}

// stepMotion runs the object motions (motion.block.*, motion.object.*,
// motion.same.*) through traits.MotionStep for a declared kind and scrolls to
// the heading it lands on. A motion.object.select result is stored in
// MotionSelection, in MotionNodes indices; the viewport stays.
func (m *Model) stepMotion(cmd core.Command) bool {
	kind, ok := traits.MotionObjectKind(cmd, motionConfig)
	if !ok || !motionKinds[kind] {
		return false
	}
	nodes, cur := m.motionNodes()
	target, sel, moved, _ := traits.MotionStep(nodes, cur, motionConfig, cmd)
	if cmd.ID == traits.CmdMotionObjectSelect {
		m.MotionSelection = sel
		return true
	}
	if moved && nodes[target].ID != ViewportNodeID {
		m.scrollTo(nodes[target].Span.Y)
	}
	return true
}
