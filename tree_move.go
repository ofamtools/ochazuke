package ochazuke

// TreeMoveSpillSetting says whether moving an outline row (J, K) past its
// first or last sibling goes on out of its parent, as the rows are drawn.
const TreeMoveSpillSetting = "tree.move-spill"

// TreeMoveSpill is the tree.move-spill setting (default off): at its last
// sibling a row moved down follows its parent, at its first sibling a row
// moved up precedes it. Off, the row stops at its siblings' edge.
func (r *Runtime) TreeMoveSpill() bool {
	v, _ := r.settingValue(TreeMoveSpillSetting)
	on, _ := v.(bool)
	return on
}
