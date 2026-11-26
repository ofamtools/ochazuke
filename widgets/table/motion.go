package table

import (
	"strconv"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
)

// motionConfig is the table's object-motion configuration: blocks are
// groups, no wrap. motion.same.* needs an explicit "object" arg.
var motionConfig = traits.MotionConfig{DefaultObject: traits.ObjectBlock}

// motionKinds are the object kinds the table's MotionNodes carry.
var motionKinds = map[string]bool{traits.ObjectBlock: true, traits.ObjectRow: true}

// MotionNodes returns one traits.MotionNode per display item in drawing
// order. When grouped, every item (header and rows) carries
// block="group:<value>", so `}` lands on the next header and `{` on the
// current group's header first. Rows with an ID also carry row=<id>. Span.Y is
// the item index.
func (m *Model) MotionNodes() []traits.MotionNode {
	return itemMotionNodes(m.Items(), m.GroupField != "")
}

func itemMotionNodes(items []Item, grouped bool) []traits.MotionNode {
	out := make([]traits.MotionNode, len(items))
	for i, it := range items {
		n := traits.MotionNode{Span: traits.ItemSpan{Y: i, H: 1}, Objects: map[string]string{}}
		switch {
		case it.Header:
			n.ID = "group:" + it.Group
		case it.Row.ID != "":
			n.ID = it.Row.ID
			n.Objects[traits.ObjectRow] = it.Row.ID
		default:
			n.ID = "item:" + strconv.Itoa(i)
		}
		if grouped {
			n.Objects[traits.ObjectBlock] = "group:" + it.Group
		}
		n.Span.ID = n.ID
		out[i] = n
	}
	return out
}

// stepMotion runs the object motions (motion.block.*, motion.object.*,
// motion.same.*) through traits.MotionStep for a declared kind. A
// motion.object.select result is stored in MotionSelection, in MotionNodes
// indices; the cursor stays.
func (m *Model) stepMotion(items []Item, cmd core.Command) bool {
	kind, ok := traits.MotionObjectKind(cmd, motionConfig)
	if !ok || !motionKinds[kind] {
		return false
	}
	target, sel, _, _ := traits.MotionStep(itemMotionNodes(items, m.GroupField != ""), m.SelectedIdx, motionConfig, cmd)
	if cmd.ID == traits.CmdMotionObjectSelect {
		m.MotionSelection = sel
		return true
	}
	if len(items) > 0 {
		m.SelectedIdx = target
	}
	m.clamp(len(items))
	return true
}
