package diff

import (
	"fmt"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
)

// motionConfig is the diff's object-motion configuration: blocks are hunks,
// sections are files, no wrap. motion.same.* needs an explicit "object" arg.
var motionConfig = traits.MotionConfig{DefaultObject: traits.ObjectBlock}

// motionKinds are the object kinds the diff's MotionNodes carry.
var motionKinds = map[string]bool{traits.ObjectBlock: true, traits.ObjectSection: true}

// MotionNodes returns one traits.MotionNode per visible row (Rows order,
// which is drawing order). Every row carries section="file:<section index>";
// hunk header and line rows also carry block="hunk:<section>:<hunk>", file
// header rows carry no block, so `}` / `{` stop on hunk headers only. Span.Y
// is the row index.
func (m *Model) MotionNodes() []traits.MotionNode {
	return rowMotionNodes(m.Rows())
}

func rowMotionNodes(rows []Row) []traits.MotionNode {
	out := make([]traits.MotionNode, len(rows))
	for i, r := range rows {
		id := fmt.Sprintf("row:%d", i)
		objs := map[string]string{traits.ObjectSection: fmt.Sprintf("file:%d", r.Section)}
		switch r.Kind {
		case RowSection:
			id = fmt.Sprintf("file:%d", r.Section)
		case RowHunk:
			id = fmt.Sprintf("hunk:%d:%d", r.Section, r.Hunk)
		}
		if r.Kind != RowSection {
			objs[traits.ObjectBlock] = fmt.Sprintf("hunk:%d:%d", r.Section, r.Hunk)
		}
		out[i] = traits.MotionNode{ID: id, Span: traits.ItemSpan{ID: id, Y: i, H: 1}, Objects: objs}
	}
	return out
}

// stepMotion runs the object motions (motion.block.*, motion.object.*,
// motion.same.*) through traits.MotionStep for a declared kind. A
// motion.object.select result is stored in MotionSelection, in MotionNodes
// indices; the cursor stays.
func (m *Model) stepMotion(rows []Row, cmd core.Command) bool {
	kind, ok := traits.MotionObjectKind(cmd, motionConfig)
	if !ok || !motionKinds[kind] {
		return false
	}
	target, sel, _, _ := traits.MotionStep(rowMotionNodes(rows), m.Cursor, motionConfig, cmd)
	if cmd.ID == traits.CmdMotionObjectSelect {
		m.MotionSelection = sel
		return true
	}
	if len(rows) > 0 {
		m.Cursor = target
	}
	m.clamp(len(rows))
	return true
}
