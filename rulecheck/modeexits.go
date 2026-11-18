package rulecheck

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

const modeExitsHeader = `rulecheck modeexits baseline: place (noun chain outside in, ">" joined; "@modal" when in a modal), mode, kind, count, reason (tab separated).
A row says: MODE can be entered at PLACE (a row on the walk enters or switches to it, the noun pins it, or the noun's own
table binds rows in it) and no key there leaves it: no row switches to another mode, no row's verb declares that it
leaves the mode or closes the modal (Operation.Exits), a pending mode has no row that ends it, and no leader key reached
from the mode leads out. kind "no-exit": nothing on the walk could leave it; kind "exit-not-declared": rows answer there
whose verbs may change the mode in Go with nothing declared: declare the one that leaves (Operation.Exits), or bind a
key to a verb that does. Fix it by adding a way out; a row with a reason is a reviewed exception, one without is debt
that only shrinks (RULECHECK_UPDATE=1).`

// ModeExitsWidth is the number of key fields of a modeexits row.
const ModeExitsWidth = 3

// Kinds of a modeexits finding (its third field).
const (
	// ModeNoExit: nothing on the mode's walk at the place could leave it.
	ModeNoExit = "no-exit"
	// ModeExitNotDeclared: no row leaves the mode by data, but rows answer
	// there whose verbs declare nothing (Operation.Exits) and may change
	// the mode in Go.
	ModeExitNotDeclared = "exit-not-declared"
)

// ModeExitsCoverage is what the modeexits walk saw.
type ModeExitsCoverage struct {
	// Places and Walks are the places walked and the (place, mode) walks
	// read; Entered the (place, mode) pairs found enterable, Exits those
	// with a way out found.
	Places, Walks, Entered, Exits int
}

func (c ModeExitsCoverage) String() string {
	return fmt.Sprintf("places %d; walks %d; modes entered at a place %d; with a way out %d", c.Places, c.Walks, c.Entered, c.Exits)
}

// ModeExits reports every mode that can be entered at a place and has no
// key there that leaves it.
//
// The invariant: no mode may be entered that cannot be left. For every
// place and every mode that can be entered there, some key on the walk in
// that mode at that place leaves it: back to the mode it was entered from
// or to the resting mode, or out of the modal or view.
//
// A mode is entered at a place when (all read from data, never from a
// mode's name):
//   - a row on the walk, in a mode already entered there (the resting
//     mode first), takes a mode as a literal argument of a parameter whose
//     noun is the mode noun (mode.enter, mode.switch): the mode it names;
//   - the place's noun pins a mode (Noun.Pin);
//   - the place's own noun binds rows in the mode's own table (the noun
//     declares behaviour in the mode, so a verb of it enters it in Go).
//
// The resting mode (ModeSpec.CursorOnChar: normal) needs no way out,
// except in a modal, where it needs a key that closes the modal. A key
// leaves mode M when its answering row (at once or under a guard):
//   - names, as its mode argument, the resting mode or a mode the mode
//     was entered from (a switch back; a switch on to a third mode is
//     not a way out);
//   - runs a verb that declares it leaves (Operation.Exits): ExitsClose
//     always, ExitsMode in any mode but a modal's resting mode,
//     ExitsPending in a pending mode;
//   - in a pending mode, runs any verb that is not Staying (the runtime
//     returns from a pending mode before such a verb runs); a catch-all
//     row counts;
//   - names a pending mode L (a leader) in which a row leaves M as above:
//     the way out of a passthrough mode (the terminal) under its leader.
//
// A pending mode entered with a held key sequence (mode.enter with a
// prefix) is left by any key it does not bind. Key-sequence prefixes
// ("normal g") are not modes here: each one's cancel row leaves it.
//
// The places are those of HiddenKeys: every place r has, the extra places
// given, and every noun on its own. Findings have fields place, mode and
// kind (ModeNoExit, ModeExitNotDeclared); the message names how the mode
// is entered and the rows that answer in it. work counts one unit per walk
// and stops the walk when it returns false.
func ModeExits(r *ochazuke.Runtime, places []ochazuke.NounPlace, work func(int) bool) ([]Finding, ModeExitsCoverage) {
	var cov ModeExitsCoverage
	walked := append(r.AllPlaces(), places...)
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		walked = append(walked, ochazuke.NounPlace{Noun: name})
	}
	inner := containers(r, walked)
	found := map[string]Finding{}
	seenPlace := map[string]bool{}
	for _, p := range walked {
		if r.Nouns[r.CanonicalNoun(p.Noun)] == nil {
			continue
		}
		label := placeField(p)
		if seenPlace[label] {
			continue
		}
		seenPlace[label] = true
		cov.Places++
		mp := modePlace{r: r, place: p, container: inner[r.CanonicalNoun(p.Noun)+"\x00"+strings.Join(p.Chain(), ">")] || len(p.Ancestors) == 0 && inner[r.CanonicalNoun(p.Noun)], walks: map[string]ochazuke.HelpWalk{}, work: work}
		entered, ok := mp.entered()
		if !ok {
			cov.Walks += mp.walked
			return sortedFindings(found), cov
		}
		for _, m := range slices.Sorted(maps.Keys(entered)) {
			if mp.resting(m) && p.Modal == "" {
				continue
			}
			cov.Entered++
			if !work(1) {
				return sortedFindings(found), cov
			}
			if _, ok := mp.exit(m, entered[m]); ok {
				cov.Exits++
				continue
			}
			f := mp.finding(m, entered[m])
			if _, dup := found[f.Key()]; !dup {
				found[f.Key()] = f
			}
		}
		cov.Walks += mp.walked
	}
	return sortedFindings(found), cov
}

// containers are the places another walked place stands inside, keyed
// by noun and chain (noun NUL chain), and the nouns that stand as an
// ancestor of some walked place or as the root level every walk ends at
// (by noun alone, for a noun walked on its own).
func containers(r *ochazuke.Runtime, walked []ochazuke.NounPlace) map[string]bool {
	out := map[string]bool{}
	// The root: the outermost level of the first walk with more than one
	// level (every walk ends at it).
	for _, p := range walked {
		if r.Nouns[r.CanonicalNoun(p.Noun)] == nil {
			continue
		}
		var resting string
		for _, m := range r.HelpModes(p, false) {
			if s, _ := r.ModeTraits(m.Name); s.CursorOnChar {
				resting = m.Name
				break
			}
		}
		levels := r.HelpWalkAtPlace(p, ochazuke.ModeState{Name: resting}).Levels
		if len(levels) < 2 {
			continue
		}
		for i, l := range levels {
			if i > 0 && l.Role == ochazuke.LevelApp {
				out[r.CanonicalNoun(l.Noun)] = true
			}
		}
		break
	}
	for _, q := range walked {
		chain := q.Chain()
		for i := range q.Ancestors {
			a := r.CanonicalNoun(chain[i])
			out[a] = true
			out[a+"\x00"+strings.Join(chain[:i+1], ">")] = true
		}
	}
	return out
}

// placeField is a place as a finding's first field: the noun chain
// outside in, ">" joined, and "@modal" when it stands in a modal.
func placeField(p ochazuke.NounPlace) string {
	s := strings.Join(p.Chain(), ">")
	if p.Modal != "" {
		s += "@" + p.Modal
	}
	return s
}

// modePlace is one place's walks by mode, read once each.
type modePlace struct {
	r     *ochazuke.Runtime
	place ochazuke.NounPlace
	// container: another place walked stands inside this one (its chain
	// goes on past this place's noun), so this noun's own mode tables
	// serve that inner focus and say nothing about modes entered here.
	container bool
	walks     map[string]ochazuke.HelpWalk
	work      func(int) bool
	walked    int
}

// entry is how a mode was entered at a place: the reasons (rows, a pin,
// own rows) and whether every entering row held a key sequence.
type entry struct {
	how  []string
	held bool // entered only by rows that hold a key sequence (a prefix)
	rows int  // entering rows seen
	// from are the modes it is entered from: a switch back to one of them
	// (or to the resting mode) leaves it.
	from map[string]bool
}

func (mp *modePlace) walk(mode string) (ochazuke.HelpWalk, bool) {
	if w, ok := mp.walks[mode]; ok {
		return w, true
	}
	if !mp.work(1) {
		return ochazuke.HelpWalk{}, false
	}
	mp.walked++
	w := mp.r.HelpWalkAtPlace(mp.place, ochazuke.ModeState{Name: mode})
	mp.walks[mode] = w
	return w, true
}

func (mp *modePlace) spec(mode string) ochazuke.ModeSpec {
	s, _ := mp.r.ModeTraits(mode)
	return s
}

func (mp *modePlace) resting(mode string) bool { return mp.spec(mode).CursorOnChar }

// declared is the mode the place's noun declares: its pin, else the
// resting mode the walk names first.
func (mp *modePlace) declared() string {
	if n := mp.r.Nouns[mp.r.CanonicalNoun(mp.place.Noun)]; n != nil && n.Pin != "" {
		return n.Pin
	}
	for _, m := range mp.modes() {
		if mp.resting(m) {
			return m
		}
	}
	return ""
}

// modes are the defined modes the help offers at the place, base and
// pending, without key-sequence prefixes.
func (mp *modePlace) modes() []string {
	var out []string
	for _, m := range mp.r.HelpModes(mp.place, true) {
		if m.Payload.Prefix == "" {
			out = append(out, m.Name)
		}
	}
	return out
}

// entered are the modes that can be entered at the place, with how; false
// when the work cap stopped the walk.
func (mp *modePlace) entered() (map[string]*entry, bool) {
	out := map[string]*entry{}
	add := func(m, how string) {
		e := out[m]
		if e == nil {
			e = &entry{held: true, from: map[string]bool{}}
			out[m] = e
		}
		if len(e.how) < 4 && !slices.Contains(e.how, how) {
			e.how = append(e.how, how)
		}
	}
	defined := map[string]bool{}
	var queue []string
	for _, m := range mp.modes() {
		defined[m] = true
		if mp.resting(m) {
			add(m, "the resting mode")
			out[m].held = false
			queue = append(queue, m)
		}
	}
	if pin := mp.declared(); pin != "" && defined[pin] && out[pin] == nil {
		add(pin, "pinned by "+mp.place.Noun)
		out[pin].held = false
		queue = append(queue, pin)
	}
	for _, m := range mp.modes() {
		if out[m] != nil {
			continue
		}
		w, ok := mp.walk(m)
		if !ok {
			return out, false
		}
		if mp.ownRows(w, m) {
			add(m, mp.place.Noun+" binds rows in it")
			out[m].held = false
			queue = append(queue, m)
		}
	}
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		w, ok := mp.walk(m)
		if !ok {
			return out, false
		}
		for _, e := range w.Entries {
			if !e.State.Answering() {
				continue
			}
			target, ok := mp.target(e.Row)
			if !ok || target == m || !defined[target] {
				continue
			}
			fresh := out[target] == nil
			add(target, fmt.Sprintf("%s %s in %s", rowKeys(e.Row), operationText(e.Row), m))
			t := out[target]
			t.rows++
			if mp.spec(m).Pending {
				maps.Copy(t.from, out[m].from) // a pending mode returns to the mode it shadowed
			} else {
				t.from[m] = true
			}
			if !mp.spec(target).Pending || !holdsSequence(e.Row) {
				t.held = false
			}
			if fresh {
				queue = append(queue, target)
			}
		}
	}
	return out, true
}

// ownRows reports whether the walk's focused level binds a row in mode's
// own table that does something in it: not a prefix's cancel row, and not
// a row that only enters another mode (a register or count prefix bound
// in every mode says nothing about where the mode itself is used).
func (mp *modePlace) ownRows(w ochazuke.HelpWalk, mode string) bool {
	if mp.container || len(w.Levels) == 0 {
		return false
	}
	for _, e := range w.Entries {
		if e.Level != 0 || !e.State.OnWalk() || isPrefixCancel(e.Row) {
			continue
		}
		if _, enters := mp.target(e.Row); enters {
			continue
		}
		if t := w.TableOf(e); t.Mode == mode && t.Kind == ochazuke.TableMode {
			return true
		}
	}
	return false
}

// target is the mode a row names as a literal argument of a parameter
// whose noun is the mode noun (mode.enter, mode.switch), the declared
// mode resolved.
func (mp *modePlace) target(b ochazuke.Binding) (string, bool) {
	op, ok := mp.r.Operation(b.Request.Key())
	if !ok {
		return "", false
	}
	for name, p := range op.Parameters {
		if p.Noun != ochazuke.ModeNoun {
			continue
		}
		if _, filled := b.From[name]; filled {
			continue
		}
		v, ok := b.Request.Args[name].(string)
		if !ok || v == "" {
			continue
		}
		if v == ochazuke.ModeDeclared {
			v = mp.declared()
		}
		return v, true
	}
	return "", false
}

// holdsSequence reports whether a row entering a pending mode gives it a
// held key sequence (a prefix argument): any key it does not bind ends it.
func holdsSequence(b ochazuke.Binding) bool {
	v, _ := b.Request.Args["prefix"].(string)
	_, from := b.From["prefix"]
	return v != "" || from
}

// exit is the first way out of mode at the place, as text.
func (mp *modePlace) exit(mode string, e *entry) (string, bool) {
	if mp.spec(mode).Pending && e.held && e.rows > 0 {
		return "any key it does not bind (it holds a key sequence)", true
	}
	w, ok := mp.walk(mode)
	if !ok {
		return "", false
	}
	for _, en := range w.Entries {
		if !en.State.Answering() {
			continue
		}
		if mp.leaves(mode, mode, e.from, en.Row) {
			return rowKeys(en.Row) + " " + operationText(en.Row), true
		}
		target, ok := mp.target(en.Row)
		if !ok || target == mode || !mp.spec(target).Pending {
			continue
		}
		lw, ok := mp.walk(target)
		if !ok {
			return "", false
		}
		for _, le := range lw.Entries {
			if le.State.Answering() && mp.leaves(mode, target, e.from, le.Row) && !mp.returnsOnly(target, le.Row) {
				return fmt.Sprintf("%s then %s %s (under the leader %s)", rowKeys(en.Row), rowKeys(le.Row), operationText(le.Row), target), true
			}
		}
	}
	return "", false
}

// leaves reports whether row, answering in mode in (mode itself, or a
// leader entered from it), leaves mode.
func (mp *modePlace) leaves(mode, in string, from map[string]bool, b ochazuke.Binding) bool {
	closeOnly := mp.resting(mode) && mp.place.Modal != ""
	if target, ok := mp.target(b); ok {
		if closeOnly || target == mode || target == in || mp.spec(target).Pending {
			return false
		}
		return mp.resting(target) || from[target]
	}
	op, _ := mp.r.Operation(b.Request.Key())
	switch op.Exits {
	case ochazuke.ExitsClose:
		return true
	case ochazuke.ExitsMode:
		return !closeOnly
	case ochazuke.ExitsPending:
		return in == mode && mp.spec(mode).Pending
	}
	return in == mode && mp.spec(mode).Pending && !op.Staying && !b.Request.Empty()
}

// returnsOnly reports whether a row under a leader only ends the leader
// (back to the mode it was entered from) without leaving that mode: the
// one-shot return every non-staying row makes.
func (mp *modePlace) returnsOnly(leader string, b ochazuke.Binding) bool {
	if _, ok := mp.target(b); ok {
		return false
	}
	op, _ := mp.r.Operation(b.Request.Key())
	return op.Exits == "" || op.Exits == ochazuke.ExitsPending
}

// finding is the report for a mode with no way out at the place.
func (mp *modePlace) finding(mode string, e *entry) Finding {
	w, _ := mp.walk(mode)
	var undeclared []string
	for _, en := range w.Entries {
		if !en.State.Answering() || en.Row.Request.Empty() {
			continue
		}
		op, _ := mp.r.Operation(en.Row.Request.Key())
		if op.Motion || op.Exits != "" || op.Staying && !mp.spec(mode).Pending {
			continue
		}
		if _, ok := mp.target(en.Row); ok {
			continue
		}
		text := rowKeys(en.Row) + " " + operationText(en.Row)
		if !slices.Contains(undeclared, text) {
			undeclared = append(undeclared, text)
		}
	}
	kind := ModeNoExit
	what := "nothing on the walk could leave it"
	if len(undeclared) > 0 {
		kind = ModeExitNotDeclared
		shown := undeclared
		if len(shown) > 8 {
			shown = append(slices.Clone(shown[:8]), fmt.Sprintf("and %d more", len(undeclared)-8))
		}
		what = "rows that answer there declare no exit: " + strings.Join(shown, "; ") + ". If one of these verbs leaves the mode, declare it (Operation.Exits); else bind a key to a verb that leaves"
	}
	if mp.resting(mode) {
		what = "the modal's resting mode has no key that closes the modal (a verb with Operation.Exits close); " + what
	}
	place := strings.Join(mp.place.Chain(), " > ")
	if mp.place.Modal != "" {
		place += " (modal " + mp.place.Modal + ")"
	}
	return Finding{
		Fields:  []string{placeField(mp.place), mode, kind},
		Message: fmt.Sprintf("modeexits: %s: mode %s (entered: %s) has no way out: %s", place, mode, strings.Join(e.how, "; "), what),
	}
}

// rowKeys is a row's keys, or its pattern as <pattern>.
func rowKeys(b ochazuke.Binding) string {
	if b.Keys != "" {
		return b.Keys
	}
	return "<" + b.Pattern + ">"
}

func runModeExits(t *testing.T, c *Context) {
	path := c.BaselinePath("modeexits")
	var found []Finding
	for _, r := range c.AllRuntimes() {
		f, cov := ModeExits(r, c.Places, c.Work)
		c.Over(t)
		found = append(found, f...)
		t.Log("modeexits: " + cov.String())
	}
	CheckFindings(t, "modeexits", path, ModeExitsWidth, modeExitsHeader, dedupe(found))
}
