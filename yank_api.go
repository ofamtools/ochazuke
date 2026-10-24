package ochazuke

// StoreYank puts a yank into the shared registers with the bookkeeping the
// editor's yank operations use: the named or active register, the unnamed
// register, "0" for a yank or "1".."9" and "-" for a deletion, and the yank
// history. A component outside the package (a grid) yanks shapes the editor
// does not know ("column"), which set.register refuses, so it stores through
// this call and the editor still sees the text.
func (r *Runtime) StoreYank(y Yank, register string, deletion bool) error {
	return r.storeYank(y, register, deletion)
}

// ReadYank reads a register ("" is the active register, else the unnamed one).
func (r *Runtime) ReadYank(register string) (Yank, error) { return r.readYank(register) }
