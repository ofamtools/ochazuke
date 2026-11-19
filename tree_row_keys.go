package ochazuke

// A tree whose rows are other nouns' things (Tree.RowKeysLast) keeps its
// own keys first on them. The walk for a focus on such a row reads the
// row's level (its noun's key rows: task, project) after app, where the
// fallback table is read: the tree's rows (and its application's rows on
// it, a key for one kind of row guarded on the row) answer first, then
// what holds the tree (its modal, its page) and app, then the row's noun
// for the keys nobody on the way binds. Its verbs still act on the row from every key
// that names them, the palette and the CLI, since the row is the focused
// thing of its noun.

// TreeRowKeysSetting turns a RowKeysLast tree's row noun keys off: on (the
// default), a row's noun answers the keys the tree, what holds it and app
// leave free; off, only those answer on its rows.
const TreeRowKeysSetting = "tree.row-keys"

// rowKeysLast reports whether a focus is a row of a tree that reads its
// rows' keys last (RowKeysLast).
func (r *Runtime) rowKeysLast(focus string) bool {
	list := r.rows[focus]
	if list == "" {
		return false
	}
	inst := r.Instances[list]
	if inst == nil {
		return false
	}
	t := r.Tree(inst.Parent)
	return t != nil && t.RowKeysLast && t.list != nil && t.list == r.List(list)
}

// rowKeysOn is the tree.row-keys setting (default on).
func (r *Runtime) rowKeysOn() bool {
	v, ok := r.settingValue(TreeRowKeysSetting)
	on, isBool := v.(bool)
	return !ok || !isBool || on
}

// lastRowLevel is the level of the walk's focus (i 0) when it is a row of
// a tree that reads its rows' keys last: its noun's table, none while
// tree.row-keys is off. ok false for any other level.
func (r *Runtime) lastRowLevel(i int, inst *Instance) (level, bool) {
	if i != 0 || inst == nil || !r.rowKeysLast(inst.ID) {
		return level{}, false
	}
	lv := level{inst: inst}
	if r.rowKeysOn() {
		lv.noun = r.Nouns[inst.Noun]
	}
	return lv, true
}
