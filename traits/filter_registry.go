package traits

// FieldFilterable is an optional extension of Filterable implemented by
// widgets that support narrowing a filter to a single field/column (such as
// tables), allowing callers to query or cycle filter fields without switching
// on concrete widget types or IDs.
type FieldFilterable interface {
	Filterable
	FilterFieldName() string
	CycleFilterField() string
}

// FilterSnapshot records a filterable's state at the moment an interactive
// filter edit began, so cancel can restore the previous filter while clear
// explicitly empties it.
type FilterSnapshot struct {
	Query      string
	Mode       MatchMode
	ReturnMode string
}

// FilterRegistry maps declarative ViewNode.Filter IDs to live Filterable
// instances and tracks pre-edit snapshots for clean cancel-vs-clear semantics.
type FilterRegistry struct {
	items     map[string]Filterable
	snapshots map[string]FilterSnapshot
}

// NewFilterRegistry returns an initialized FilterRegistry.
func NewFilterRegistry() *FilterRegistry {
	return &FilterRegistry{
		items:     map[string]Filterable{},
		snapshots: map[string]FilterSnapshot{},
	}
}

// Register binds id to f. Passing nil unregisters id.
func (r *FilterRegistry) Register(id string, f Filterable) {
	if r == nil || id == "" {
		return
	}
	if f == nil {
		delete(r.items, id)
		return
	}
	if r.items == nil {
		r.items = map[string]Filterable{}
	}
	r.items[id] = f
}

// Unregister removes id from the registry.
func (r *FilterRegistry) Unregister(id string) {
	if r == nil || id == "" {
		return
	}
	delete(r.items, id)
}

// Reset clears registered Filterable entries (e.g. before a view rebuild) while
// preserving any in-flight edit snapshots.
func (r *FilterRegistry) Reset() {
	if r == nil || r.items == nil {
		return
	}
	clear(r.items)
}

// Get returns the Filterable registered under id, or nil.
func (r *FilterRegistry) Get(id string) Filterable {
	if r == nil || id == "" {
		return nil
	}
	return r.items[id]
}

// BeginEdit snapshots id's current query, match mode, and returnMode so a
// subsequent CancelEdit restores the prior kept filter instead of wiping it.
func (r *FilterRegistry) BeginEdit(id, returnMode string) (Filterable, bool) {
	f := r.Get(id)
	if f == nil {
		return nil, false
	}
	if r.snapshots == nil {
		r.snapshots = map[string]FilterSnapshot{}
	}
	r.snapshots[id] = FilterSnapshot{
		Query:      f.FilterQuery(),
		Mode:       f.FilterMode(),
		ReturnMode: returnMode,
	}
	return f, true
}

// AcceptEdit commits the active filter edit, discards the snapshot, and
// returns the saved returnMode (or "" if none was recorded).
func (r *FilterRegistry) AcceptEdit(id string) string {
	if r == nil || id == "" {
		return ""
	}
	snap := r.snapshots[id]
	delete(r.snapshots, id)
	return snap.ReturnMode
}

// CancelEdit restores the pre-edit snapshot for id and returns the saved
// returnMode and whether a snapshot existed. If no snapshot existed, the
// filter is cleared cleanly.
func (r *FilterRegistry) CancelEdit(id string) (string, bool) {
	if r == nil || id == "" {
		return "", false
	}
	f := r.Get(id)
	snap, ok := r.snapshots[id]
	delete(r.snapshots, id)
	if f != nil {
		if ok {
			f.Filter(snap.Query, snap.Mode)
		} else {
			f.Filter("", f.FilterMode())
		}
	}
	return snap.ReturnMode, ok
}

// ClearFilter empties id's active filter query and discards any snapshot.
func (r *FilterRegistry) ClearFilter(id string) bool {
	if r == nil || id == "" {
		return false
	}
	delete(r.snapshots, id)
	f := r.Get(id)
	if f == nil {
		return false
	}
	return f.Filter("", f.FilterMode())
}

// Facts derives truthful read-only filter facts for id, including
// hasFilterFields, filterField, and filterPrefix so bindings.json guards and
// the filter bar render purely from data.
func (r *FilterRegistry) Facts(id, prefix string, active bool, cfg FilterConfig) map[string]any {
	f := r.Get(id)
	if f == nil {
		return map[string]any{
			"filtering":       false,
			"query":           "",
			"queryEmpty":      true,
			"hasFilter":       false,
			"hasFilterFields": false,
			"filterPrefix":    prefix,
		}
	}
	st := FilterState{
		Active: active,
		Query:  f.FilterQuery(),
		Mode:   f.FilterMode(),
		Err:    f.FilterError(),
	}
	facts := FilterFacts(st, cfg)
	facts["filterPrefix"] = prefix
	if ff, ok := f.(FieldFilterable); ok {
		facts["hasFilterFields"] = true
		facts["filterField"] = ff.FilterFieldName()
	} else {
		facts["hasFilterFields"] = false
		facts["filterField"] = ""
	}
	return facts
}

// ModeStack stores prior input modes when entering transient modes (such as
// ModeFilter or ModeHints) so exiting always restores the exact caller mode
// without per-modal boolean flags.
type ModeStack struct {
	modes []string
}

// Push records mode on top of the stack (ignoring empty strings).
func (s *ModeStack) Push(mode string) {
	if s == nil || mode == "" {
		return
	}
	s.modes = append(s.modes, mode)
}

// Pop removes and returns the top mode, or fallback when the stack is empty.
func (s *ModeStack) Pop(fallback string) string {
	if s == nil || len(s.modes) == 0 {
		return fallback
	}
	top := s.modes[len(s.modes)-1]
	s.modes = s.modes[:len(s.modes)-1]
	return top
}

// Peek returns the top mode without removing it, or "".
func (s *ModeStack) Peek() string {
	if s == nil || len(s.modes) == 0 {
		return ""
	}
	return s.modes[len(s.modes)-1]
}

// Len reports how many return modes are stacked.
func (s *ModeStack) Len() int {
	if s == nil {
		return 0
	}
	return len(s.modes)
}

// Clear empties the stack.
func (s *ModeStack) Clear() {
	if s == nil {
		return
	}
	s.modes = s.modes[:0]
}
