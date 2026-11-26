package tree

import (
	"strconv"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
)

// motionConfig is the tree's object-motion configuration: blocks are
// top-level subtrees, motion.same.* walks siblings (same parent), no wrap.
var motionConfig = traits.MotionConfig{DefaultObject: traits.ObjectBlock, RelativeObject: traits.ObjectParent}

// motionKinds are the object kinds the tree's MotionNodes carry.
var motionKinds = map[string]bool{traits.ObjectBlock: true, traits.ObjectParent: true}

// MotionNodes returns one traits.MotionNode per visible row (Flatten order,
// which is drawing order). Every row carries block=<id of its top-level
// ancestor row> and, below the top level, parent=<parent id>; a draft row's
// ID is "draft:<index>". Span.Y is the visual row (GapTopLevel spacers
// counted).
func (m *Model) MotionNodes() []traits.MotionNode {
	return rowMotionNodes(m.Flatten())
}

func rowMotionNodes(rows []VisibleRow) []traits.MotionNode {
	out := make([]traits.MotionNode, len(rows))
	block := ""
	for i, r := range rows {
		id := r.ID
		if r.Draft || id == "" {
			id = "draft:" + strconv.Itoa(i)
		}
		if r.IsRoot {
			block = id
		}
		n := traits.MotionNode{ID: id, Span: traits.ItemSpan{ID: id, Y: r.VisualRow, H: 1}, Objects: map[string]string{}}
		if block != "" {
			n.Objects[traits.ObjectBlock] = block
		}
		if r.ParentID != "" {
			n.Objects[traits.ObjectParent] = r.ParentID
		}
		out[i] = n
	}
	return out
}

// stepMotion runs the object motions (motion.block.*, motion.object.*,
// motion.same.*) through traits.MotionStep for a declared kind. A
// motion.object.select result is stored in MotionSelection, in MotionNodes
// indices; the selection stays.
func (m *Model) stepMotion(rows []VisibleRow, curIdx int, moveToIdx func(int), cmd core.Command) bool {
	kind, ok := traits.MotionObjectKind(cmd, motionConfig)
	if !ok || !motionKinds[kind] {
		return false
	}
	target, sel, _, _ := traits.MotionStep(rowMotionNodes(rows), curIdx, motionConfig, cmd)
	if cmd.ID == traits.CmdMotionObjectSelect {
		m.MotionSelection = sel
		return true
	}
	moveToIdx(target)
	return true
}
