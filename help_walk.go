package ochazuke

import (
	"cmp"
	"maps"
	"slices"
	"strings"
)

// The binding help page's walk (help_page.go draws it): the levels a key
// press walks from a focus out to app, each level's tables in the order
// the resolver reads them, and every row on them with its status from that
// focus. It is a value: it answers questions about itself and draws and
// writes nothing. The live walk (stack, walkOf) and the hypothetical one
// (nounWalk) give its levels; nounTableChain gives a level's mode tables
// with their modes; declaredKeys and generatedAside decide which generated
// rows step aside, as the resolver does.

// RowStatus is what a binding row does from a focus: the one answer of
// HelpEntry.Status and HelpPageRow.Status.
type RowStatus string

const (
	// StatusAnswers: the first row on its key on the walk; it answers.
	StatusAnswers RowStatus = "answers"
	// StatusGuarded: the first row on its key, with a guard of its own: it
	// answers when the guard holds, else the walk goes on.
	StatusGuarded RowStatus = "answers if guard"
	// StatusMaybe: a nearer row on the same key has a guard; this one
	// answers when that guard is false.
	StatusMaybe RowStatus = "maybe"
	// StatusShadowed: a nearer unguarded row (or a nearer open pattern)
	// takes the key first.
	StatusShadowed RowStatus = "shadowed"
	// StatusStepsAside: a generated row a declared row on the walk binds
	// the keys of, or a prefix of them (noun_keys.go).
	StatusStepsAside RowStatus = "steps aside"
	// StatusOtherMode: a row of a table on the walk's nouns in a mode the
	// walk is not in.
	StatusOtherMode RowStatus = "other mode"
	// StatusOffWalk: a row of a table no level of the walk reads.
	StatusOffWalk RowStatus = "off walk"
	// StatusNoKey: a verb the focused noun has that no row of the walk
	// binds.
	StatusNoKey RowStatus = "no key"
)

// RowStatuses are the statuses in the order the help explains them.
var RowStatuses = []RowStatus{StatusAnswers, StatusGuarded, StatusMaybe, StatusShadowed, StatusStepsAside, StatusOtherMode, StatusOffWalk, StatusNoKey}

// Answering reports whether a row with the status answers from the focus,
// at once or when a guard holds.
func (s RowStatus) Answering() bool {
	return s == StatusAnswers || s == StatusGuarded || s == StatusMaybe
}

// Hidden reports whether a nearer row keeps the row from answering.
func (s RowStatus) Hidden() bool { return s == StatusShadowed || s == StatusStepsAside }

// OnWalk reports whether the status is one of a row the walk reads.
func (s RowStatus) OnWalk() bool { return s.Answering() || s.Hidden() }

// Kinds of a walk table (WalkTable.Kind), in the order a level reads them.
const (
	TableInstance = "instance"
	TableMode     = "mode"
	TableExtends  = "extends"
	TableAnyMode  = "any"
)

// Roles of a walk level (HelpLevel.Role).
const (
	LevelFocus     = "focus"
	LevelContainer = "container"
	LevelPage      = "page"
	LevelModal     = "modal"
	LevelApp       = "app"
	LevelFallback  = "fallback"
)

// WalkTable is one table a level reads: an instance's rows in a mode, a
// noun's mode table, a table that mode extends, or the noun's any-mode
// table. Rows are the stored rows with Noun, Instance and Mode filled.
type WalkTable struct {
	Kind     string    `json:"kind"`
	Noun     string    `json:"noun,omitempty"`
	Instance string    `json:"instance,omitempty"`
	Mode     string    `json:"mode"`
	Rows     []Binding `json:"rows"`
}

// Label names the table as the page's table: chip does: "list normal",
// "instance tree/list any mode".
func (t WalkTable) Label() string {
	owner := t.Noun
	if t.Instance != "" {
		owner = "instance " + t.Instance
	}
	return owner + " " + modeLabel(t.Mode)
}

// modeLabel is a mode as the page writes it: "any mode" for the any-mode
// table.
func modeLabel(mode string) string {
	if mode == "" {
		return "any mode"
	}
	return mode
}

// HelpCounts are a level's rows from the focus: every row on it, the ones
// that answer (at once or by a guard), the ones a nearer row hides, and
// the verbs of its noun no row binds.
type HelpCounts struct {
	Rows     int `json:"rows"`
	Answers  int `json:"answers"`
	Shadowed int `json:"shadowed"`
	NoKey    int `json:"noKey"`
}

// HelpLevel is one level of the walk, the focused thing first and app
// last.
type HelpLevel struct {
	Index    int         `json:"index"`
	Noun     string      `json:"noun"`
	Instance string      `json:"instance,omitempty"`
	Role     string      `json:"role"`
	Tables   []WalkTable `json:"tables"`
	Counts   HelpCounts  `json:"counts"`
}

// Label is the level's name as the rail and the level: chip write it: the
// noun.
func (l HelpLevel) Label() string { return l.Noun }

// HelpEntry is one row on the walk: the level and table that read it,
// whether it is read in the capture pass, its status and the entry that
// keeps it from answering (By, -1 for none).
type HelpEntry struct {
	Row     Binding   `json:"row"`
	Level   int       `json:"level"`
	Table   int       `json:"table"`
	Capture bool      `json:"capture,omitempty"`
	State   RowStatus `json:"status"`
	By      int       `json:"by"`
}

// Status is the row's status from the walk's focus.
func (e HelpEntry) Status() RowStatus { return e.State }

// HelpWalk is the walk from one focus in one mode: the place it stands for
// (the live focus's placement, or a chosen place), the live instance when
// there is one, the levels and every row on them in resolution order (the
// capture pass outer first, then the bubbling pass inner first).
type HelpWalk struct {
	Place   NounPlace   `json:"place"`
	Focus   string      `json:"focus,omitempty"`
	Mode    string      `json:"mode"`
	Levels  []HelpLevel `json:"levels"`
	Entries []HelpEntry `json:"entries"`
	// Unbound are the verbs of the focused noun (by help.unbound-scope, of
	// the chain) no row of the walk binds.
	Unbound []Operation `json:"-"`
}

// TableOf is the table an entry was read from.
func (w HelpWalk) TableOf(e HelpEntry) WalkTable { return w.Levels[e.Level].Tables[e.Table] }

// LevelLabel names level i, "not on this walk" for -1.
func (w HelpWalk) LevelLabel(i int) string {
	if i < 0 || i >= len(w.Levels) {
		return "not on this walk"
	}
	return w.Levels[i].Label()
}

// LevelOf is the walk level whose noun or instance owns a stored row's
// table, -1 when none does.
func (w HelpWalk) LevelOf(b Binding) int {
	for _, l := range w.Levels {
		if b.Instance != "" && b.Instance == l.Instance || b.Instance == "" && b.Noun == l.Noun {
			return l.Index
		}
	}
	return -1
}

// HelpWalkAt is the walk from a live instance in mode, the modals given
// bounding it as they bound the live walk: the instance tables of every
// level included.
func (r *Runtime) HelpWalkAt(focus string, mode ModeState, modals []string) HelpWalk {
	inst := r.Instances[focus]
	if inst == nil {
		return r.HelpWalkAtPlace(NounPlace{Noun: AppNoun}, mode)
	}
	levels := r.walkIn(queryResolution(focus, mode, modals))
	w := r.helpWalkOf(levels, mode, focus)
	w.Place, w.Focus = r.placementOf(focus, inst.Noun), focus
	w.Unbound = r.unboundOperations(r.focusChain(focus), levels, mode, "", nil)
	w.countUnbound()
	return w
}

// HelpWalkAtPlace is the walk a focus on place's noun would have in mode
// (nounWalk): noun tables only, no instance's rows.
func (r *Runtime) HelpWalkAtPlace(place NounPlace, mode ModeState) HelpWalk {
	place = r.canonicalPlace(place)
	levels := r.nounWalk(place, mode)
	w := r.helpWalkOf(levels, mode, "")
	w.Place = place
	w.Unbound = r.PlaceUnboundVerbs(place, mode)
	w.countUnbound()
	return w
}

// helpWalkOf turns resolver levels (a walk with its capture pass) into the
// page's walk: the bubbling levels are the page's levels; the capture
// pass, when there is one, reads their capture rows outer first.
func (r *Runtime) helpWalkOf(levels []level, mode ModeState, focus string) HelpWalk {
	base, _ := baseAndPrefix(mode)
	exclusive := r.modes[mode.Name].Pending || mode.Payload.Shadow != nil
	w := HelpWalk{Mode: base}
	var inner []level
	passthrough := true
	for _, lv := range levels {
		if lv.capture {
			passthrough = false
			continue
		}
		inner = append(inner, lv)
	}
	for i, lv := range inner {
		hl := HelpLevel{Index: i, Noun: lv.name(), Role: r.levelRole(lv, i)}
		if lv.inst != nil && len(lv.inst.Bindings) > 0 {
			hl.Instance = lv.inst.ID
			hl.Tables = append(hl.Tables, helpInstanceTable(lv.inst, base))
			if !exclusive {
				hl.Tables = append(hl.Tables, helpInstanceTable(lv.inst, ""))
			}
		}
		if lv.noun != nil {
			first := true
			r.eachNounTable(lv.noun, base, func(m string, rows []Binding) {
				kind := TableExtends
				if first {
					kind, first = TableMode, false
				}
				hl.Tables = append(hl.Tables, helpNounTable(kind, lv.noun.Name, m, rows))
			})
			if !exclusive {
				hl.Tables = append(hl.Tables, helpNounTable(TableAnyMode, lv.noun.Name, "", lv.noun.Bindings[""]))
			}
		}
		w.Levels = append(w.Levels, hl)
	}
	declared := r.declaredKeys(inner, mode, focus, exclusive, false)
	push := func(capture bool, order []int) {
		for _, i := range order {
			for t, table := range w.Levels[i].Tables {
				for _, b := range table.Rows {
					if b.Capture == capture {
						w.Entries = append(w.Entries, HelpEntry{Row: b, Level: i, Table: t, Capture: capture, By: -1})
					}
				}
			}
		}
	}
	// The capture pass reads outer first with the fallback table last
	// (walkOf); the bubbling pass inner first.
	var bubbling, capture, fallback []int
	for i := len(inner) - 1; i >= 0; i-- {
		if inner[i].fallback {
			fallback = append(fallback, i)
		} else {
			capture = append(capture, i)
		}
	}
	for i := range inner {
		bubbling = append(bubbling, i)
	}
	if !passthrough {
		push(true, append(capture, fallback...))
	}
	push(false, bubbling)
	w.judge(declared)
	return w
}

// levelRole names what a resolver level stands for on the page.
func (r *Runtime) levelRole(lv level, i int) string {
	switch {
	case lv.fallback:
		return LevelFallback
	case i == 0:
		return LevelFocus
	case lv.inst != nil && lv.inst.ID == AppNoun:
		return LevelApp
	case lv.noun != nil && lv.noun.Name == FrameworkNamespace+".modal":
		return LevelModal
	case lv.inst != nil && r.PageOf(lv.inst.ID) != nil:
		return LevelPage
	}
	return LevelContainer
}

// helpInstanceTable is an instance's rows of one mode ("" the any-mode
// ones), unbind rows left out.
func helpInstanceTable(inst *Instance, mode string) WalkTable {
	t := WalkTable{Kind: TableInstance, Instance: inst.ID, Mode: mode}
	for _, b := range inst.Bindings {
		if b.Mode == mode && !b.Unbind {
			b.Noun, b.Instance = "", inst.ID
			t.Rows = append(t.Rows, b)
		}
	}
	return t
}

// helpNounTable is a noun's table in mode, its rows with their table
// filled in, unbind rows left out.
func helpNounTable(kind, noun, mode string, rows []Binding) WalkTable {
	t := WalkTable{Kind: kind, Noun: noun, Mode: mode}
	for _, b := range rows {
		if !b.Unbind {
			b.Noun, b.Instance, b.Mode = noun, "", mode
			t.Rows = append(t.Rows, b)
		}
	}
	return t
}

// judge gives every entry its status (the shadowing rules of the
// resolver's lookup, read without judging guards: a guarded row nearer
// on a key makes the rows after it maybe) and every level its counts.
func (w *HelpWalk) judge(declared map[string]bool) {
	for i := range w.Entries {
		e := &w.Entries[i]
		e.State = StatusAnswers
		if generatedAside(e.Row, declared) {
			e.State = StatusStepsAside
			e.By = w.declaring(e.Row.Keys, i)
			continue
		}
		if e.Row.Keys == "" {
			if e.Row.When != "" {
				e.State = StatusGuarded
			}
			continue // an open pattern is never shadowed by a later row
		}
		guarded := -1
		for j := 0; j < i; j++ {
			o := w.Entries[j]
			if o.State == StatusStepsAside || !takesSameKey(o, *e) {
				continue
			}
			if o.Row.When == "" {
				e.State, e.By, guarded = StatusShadowed, j, -2
				break
			}
			if guarded == -1 {
				guarded = j
			}
		}
		switch {
		case guarded >= 0:
			e.State, e.By = StatusMaybe, guarded
		case guarded == -1 && e.Row.When != "":
			e.State = StatusGuarded
		}
	}
	for i := range w.Levels {
		c := HelpCounts{}
		for _, e := range w.Entries {
			if e.Level != i {
				continue
			}
			c.Rows++
			switch {
			case e.State.Answering():
				c.Answers++
			case e.State.Hidden():
				c.Shadowed++
			}
		}
		w.Levels[i].Counts = c
	}
}

// takesSameKey reports whether an earlier entry o takes the key of e first:
// the same keys, or an open pattern at a nearer level of the same pass
// that matches e's first key.
func takesSameKey(o, e HelpEntry) bool {
	if o.Row.Keys != "" {
		return o.Row.Keys == e.Row.Keys
	}
	if o.Capture != e.Capture || o.Level >= e.Level && !o.Capture {
		return false
	}
	first, _, _ := strings.Cut(e.Row.Keys, " ")
	ok, _ := matchesPattern(o.Row.Pattern, MacroKey{Key: first})
	return ok
}

// declaring is the entry of the declared row a generated row on keys steps
// aside for: the same keys or a prefix of them; -1 when none is on the
// walk (the declared row is on a related level).
func (w HelpWalk) declaring(keys string, self int) int {
	for j, o := range w.Entries {
		if j == self || o.Row.Generated || o.Row.Keys == "" {
			continue
		}
		if o.Row.Keys == keys || strings.HasPrefix(keys, o.Row.Keys+" ") {
			return j
		}
	}
	return -1
}

// countUnbound fills each level's NoKey: the unbound verbs of its noun.
func (w *HelpWalk) countUnbound() {
	for i := range w.Levels {
		n := 0
		for _, op := range w.Unbound {
			if op.Noun() == w.Levels[i].Noun {
				n++
			}
		}
		w.Levels[i].Counts.NoKey = n
	}
}

// ModeFor is the mode to walk a new focus on place in: mode when the
// place's noun has rows of its own in it, else normal (the resting mode)
// when it has, else the mode its noun has the most rows in, else mode.
func (r *Runtime) ModeFor(place NounPlace, mode string) string {
	n := r.Nouns[r.CanonicalNoun(place.Noun)]
	if n == nil {
		return mode
	}
	has := func(m string) bool {
		own := false
		r.eachNounTable(n, m, func(_ string, rows []Binding) { own = own || len(rows) > 0 })
		return own
	}
	if has(mode) {
		return mode
	}
	if resting := cmp.Or(firstOf(r.helpBaseModes()), "normal"); has(resting) {
		return resting
	}
	best, most := mode, 0
	for _, m := range slices.Sorted(maps.Keys(n.Bindings)) {
		if m != "" && len(n.Bindings[m]) > most {
			best, most = m, len(n.Bindings[m])
		}
	}
	return best
}

// firstOf is the first string of a list, "" for none.
func firstOf(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}
