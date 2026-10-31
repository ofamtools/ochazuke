package ochazuke

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// typingKeys are the keys a field taking text must insert: every printable
// ASCII character, each as the key name the terminal reports, and space.
func typingKeys() []MacroKey {
	var out []MacroKey
	for c := '!'; c <= '~'; c++ {
		out = append(out, MacroKey{Key: string(c)})
	}
	return append(out, MacroKey{Key: "space"})
}

// typingPlace is a focus and mode in which some level of the walk takes
// text: it answers the printable class with a row whose action is
// available and whose guard holds (the editor's insert, a list filter's
// query, a chooser's or the palette's line).
type typingPlace struct {
	action string // the typing row's action: a printable key must reach it
}

// typingPlaceIn finds the innermost bubbling level of a walk that types,
// if any.
func (r *Runtime) typingPlaceIn(levels []level, mode ModeState, ev KeyEvent) (typingPlace, bool) {
	exclusive := r.pendingModes[mode.Name]
	for _, lv := range levels {
		if lv.capture {
			continue
		}
		for _, rows := range r.levelTables(lv, mode.Name, exclusive) {
			for _, row := range rows {
				if row.Unbind || row.Capture || row.Pattern != Printable {
					continue
				}
				if !r.Available(row.Request, ev.Focus, mode) || !r.rowGuardHolds(row.When, ev) {
					continue
				}
				return typingPlace{action: row.Request.Key()}, true
			}
		}
	}
	return typingPlace{}, false
}

// levelTables are the tables a level reads in a mode, the way lookup reads
// them: the instance's rows, the noun's mode tables (the mode it extends
// after), then the noun's mode-less rows.
func (r *Runtime) levelTables(lv level, mode string, exclusive bool) [][]Binding {
	var tables [][]Binding
	if lv.inst != nil {
		tables = append(tables, rowsForMode(lv.inst.Bindings, mode, exclusive))
	}
	if lv.noun != nil {
		tables = append(tables, r.nounTables(lv.noun, mode)...)
		if !exclusive {
			tables = append(tables, lv.noun.Bindings[""])
		}
	}
	return tables
}

// levelLabel names a level for a report: its instance (and noun), else
// its noun.
func levelLabel(lv level) string {
	switch {
	case lv.inst != nil && lv.noun != nil && lv.noun.Name != lv.inst.ID:
		return lv.inst.ID + " (" + lv.noun.Name + ")"
	case lv.inst != nil:
		return lv.inst.ID
	default:
		return lv.name()
	}
}

// typingShadow is one printable key a typing place does not get: the row
// that answers it first, or the sequence that makes it wait.
type typingShadow struct {
	Focus, Mode, Key string
	Level            string
	Row              Binding
	Waits            bool
}

func (s typingShadow) String() string {
	what := fmt.Sprintf("%s %q → %s", tableLabel(s.Row), s.Row.Keys+s.Row.Pattern, s.Row.Request.Key())
	if s.Row.Capture {
		what += " (capture)"
	}
	how := "answers it"
	if s.Waits {
		how = "makes it wait for a sequence"
	}
	return fmt.Sprintf("focus %s mode %s: typing %q does not insert: %s at %s %s", s.Focus, s.Mode, s.Key, what, s.Level, how)
}

// typingShadowsAt resolves every typing key at one focus and mode the way
// a fresh key (no sequence pending) resolves in Dispatch, without running
// anything, and reports each key that would not reach the place's typing
// action: a capture row answering it, a row at an inner level or on the
// typing level itself answering it with another action, or a sequence at
// any level starting with it (the walk would wait instead of typing). A
// row with the place's own action answers rightly at any level (the
// editor fallback's space for a chooser's line).
func (r *Runtime) typingShadowsAt(focus string, mode ModeState) []typingShadow {
	if r.passthroughModes[mode.Name] {
		return nil // a terminal: every key is the pane's
	}
	levels := r.walkOf(r.stack(focus), false)
	ev := KeyEvent{Mode: mode, Focus: focus}
	place, ok := r.typingPlaceIn(levels, mode, ev)
	if !ok {
		return nil
	}
	exclusive := r.pendingModes[mode.Name]
	drop := r.dropsInheritedSequences()
	// A level none of whose rows names the key (as a key, the first of a
	// sequence, or a pattern it matches) neither answers nor waits: lookup,
	// which judges every row's availability, runs only where one does.
	mentions := make([]func(MacroKey) bool, len(levels))
	for i, lv := range levels {
		firsts, patterns := map[string]bool{}, []string{}
		for _, rows := range r.levelTables(lv, mode.Name, exclusive) {
			for _, row := range rows {
				if row.Unbind || row.Capture != lv.capture {
					continue
				}
				if row.Keys != "" {
					first, _, _ := strings.Cut(row.Keys, " ")
					firsts[first] = true
				} else if row.Pattern != "" {
					patterns = append(patterns, row.Pattern)
				}
			}
		}
		mentions[i] = func(key MacroKey) bool {
			return firsts[key.Key] || slices.ContainsFunc(patterns, func(p string) bool { ok, _ := matchesPattern(p, key); return ok })
		}
	}
	lookAt := func(i int, lv level, key MacroKey, ev KeyEvent) (*Binding, lookResult) {
		if !mentions[i](key) {
			return nil, lookResult{}
		}
		return r.lookup(lv, mode.Name, exclusive, "", key.Key, key, ev)
	}
	var out []typingShadow
	for _, key := range typingKeys() {
		ev := ev
		ev.Key = key
		shadow := func(lv level, row Binding, waits bool) {
			out = append(out, typingShadow{Focus: focus, Mode: mode.Name, Key: key.Key, Level: levelLabel(lv), Row: row, Waits: waits})
		}
		waitAt := func(lv level) {
			if row, ok := r.sequenceOn(lv, mode.Name, exclusive, key.Key, ev); ok {
				shadow(lv, row, true)
			}
		}
		// The capture pass: outer-first, the first level that answers or
		// waits wins.
		n, done := 0, false
		for ; n < len(levels) && levels[n].capture; n++ {
			lv := levels[n]
			hit, look := lookAt(n, lv, key, ev)
			if look.waits(look.ownExact, drop) {
				waitAt(lv)
				done = true
				break
			}
			if hit != nil {
				shadow(lv, *hit, false)
				done = true
				break
			}
		}
		if done {
			continue
		}
		// The bubbling pass: any level waiting on a sequence wins over
		// the innermost hit, scanned up to the first key-class hit.
		var (
			hitLv    level
			hit      *Binding
			looks    []lookResult
			lvs      []level
			ownExact bool
		)
		for i := n; i < len(levels); i++ {
			lv := levels[i]
			h, look := lookAt(i, lv, key, ev)
			looks, lvs = append(looks, look), append(lvs, lv)
			ownExact = ownExact || look.ownExact
			if hit == nil && h != nil {
				hit, hitLv = h, lv
				if h.Keys == "" {
					break
				}
			}
		}
		if i := slices.IndexFunc(looks, func(l lookResult) bool { return l.waits(ownExact, drop) }); i >= 0 {
			waitAt(lvs[i])
			continue
		}
		if hit != nil && hit.Request.Key() != place.action {
			shadow(hitLv, *hit, false)
		}
	}
	return out
}

// sequenceOn is the first row of a level whose keys extend key (a
// sequence on it that answers here).
func (r *Runtime) sequenceOn(lv level, mode string, exclusive bool, key string, ev KeyEvent) (Binding, bool) {
	for _, rows := range r.levelTables(lv, mode, exclusive) {
		for _, row := range rows {
			if row.Unbind || row.Capture != lv.capture || !strings.HasPrefix(row.Keys, key+" ") || isPrefixCancel(row) {
				continue
			}
			if r.Available(row.Request, ev.Focus, ev.Mode) && r.rowGuardHolds(row.When, ev) {
				return row, true
			}
		}
	}
	return Binding{}, false
}

// TypingShadows reports every printable key that a place taking text does
// not insert: for each instance as the focus and each base mode, where a
// level of the walk types (a row on the printable class, available, its
// guard holding), every printable character and space must reach that
// row's action. A row on a printable key that answers first (a capture
// row, an inner level's row, a mode-less row of a noun nearer the focus)
// or a sequence starting with one is reported: while a field takes text,
// only keys a person cannot type (ctrl, alt, esc, enter, tab, the arrows)
// may be bound. While a modal is open only the instances inside it are
// checked: the live focus is always there. Instances with no rows of
// their own under the same parents and of the same noun (a list's rows)
// are checked once. The check reads the runtime's present state (open modals,
// filters, guards) and runs nothing; the focus and mode are restored.
func (r *Runtime) TypingShadows() []string {
	saved, savedMode := r.Focus, r.Mode
	defer func() { r.Focus, r.Mode = saved, savedMode }()
	modes := slices.Sorted(maps.Keys(r.Modes))
	seen := map[string]bool{}
	var out []string
	top := r.TopModal()
	walked := map[string]bool{}
	for _, id := range slices.Sorted(maps.Keys(r.Instances)) {
		if top != "" && !slices.Contains(r.Ancestry(id), top) {
			continue // beneath the top modal: never focused while it is open
		}
		// Siblings with no rows of their own (a list's rows) walk alike:
		// one of them stands for the rest.
		inst, ancestry := r.Instances[id], r.Ancestry(id)
		sig := inst.Noun + "\x00" + strings.Join(ancestry[1:], "\x00")
		if len(inst.Bindings) > 0 || inst.inner != "" || r.Input(id) != nil {
			sig = id
		}
		if walked[sig] {
			continue
		}
		walked[sig] = true
		for _, mode := range modes {
			m := ModeState{Name: mode}
			r.Focus, r.Mode = id, m
			for _, s := range r.typingShadowsAt(id, m) {
				line := s.String()
				if !seen[line] {
					seen[line] = true
					out = append(out, line)
				}
			}
		}
	}
	return out
}
