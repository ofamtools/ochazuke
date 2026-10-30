package ochazuke

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Undo of the standard writes (standard verbs phase 1, lane S2b; plan
// 2.8). Every standard change entry (create, set, delete, and the prop
// rows that run as set) records what it changed: one ThingChange per
// thing, with the changed props' values before and after, in the shape of
// the csv example's Patch (examples/csv/app.go): a create has no Before, a
// delete no After. A visual selection's write is one entry (Ctx.Each), so
// its changes are one group and undo as one.
//
// history.undo reverts the newest entry not yet undone that holds changes,
// when every one of its things' stores declares Undo (Store.Undo) and can
// run the inverse:
//
//   - a set writes Before back (Patch);
//   - a create is undone by deleting the thing (Delete);
//   - a delete is undone by creating the thing again under its old key
//     (Create), which only a store that accepts a given key can do
//     (Store.AcceptsKey).
//
// The inverse is another write, not a discard (mtm's "undo, not discard"):
// it calls the store as the standard verb would, and is not itself
// recorded as a change to undo, so the next undo goes one step further
// back. Entries restored from the history file are never undone, as
// dot-repeat never repeats them.

// ThingChange is what one standard write did to one thing: its noun and
// key, and the props it changed with their values before and after, in
// their file form. Before is nil for a create, After nil for a delete.
// Undone marks a change history.undo reverted.
type ThingChange struct {
	Noun   string         `json:"noun"`
	Key    string         `json:"key,omitempty"`
	Before map[string]any `json:"before,omitempty"`
	After  map[string]any `json:"after,omitempty"`
	Undone bool           `json:"undone,omitempty"`
}

// UndoGuard is the guard history.undo is available under: the newest
// entry holding changes can be undone (undoable), or, with none, some
// noun's store declares Undo.
const UndoGuard = "history-undoable"

// noteChange adds a change to the entry the running request records:
// the standard handler runs one level inside its ExecuteAt (depth + 1), so
// its entry is the frame one below the current depth. A request it runs
// itself records its own changes in its own frame.
func (r *Runtime) noteChange(c ThingChange) {
	h := &r.history
	if h.changes == nil {
		h.changes = map[int][]ThingChange{}
	}
	d := max(h.depth-1, 0)
	h.changes[d] = append(h.changes[d], c)
}

// takeChanges are the changes noted for the entry at depth, cleared.
func (r *Runtime) takeChanges(depth int) []ThingChange {
	out := r.history.changes[depth]
	delete(r.history.changes, depth)
	return out
}

// runRecording runs a standard verb's handler and, when its row is a
// change (HistoryKind), notes what it did to the thing, read with the
// store's Get around the run: the thing before (a verb with a subject),
// and the thing after under the subject's key, or under the key prop of
// the thing it answers (create). A thing that was not there before has no
// Before, one that is gone after no After; when both are there, the props
// the request writes (the row's Writes) are kept, and every other writable
// prop whose value the write changed (sideChanges: what the store did on
// its own, as a move clearing a parent), so undo restores all of them. No verb is named:
// the shapes come from the table and the store.
//
// A store that does not declare Undo (or has no Get) is not read around
// its writes, which would cost it two reads per write for nothing: its
// change is noted by noun and key alone, a mark with no values, so
// history.undo stops at it (unavailable) rather than skip to an older one.
func (r *Runtime) runRecording(verb string, n *Noun, c Ctx, run func(*Runtime, *Noun, Ctx) (any, error)) (any, error) {
	row := standardRow(verb)
	if row == nil || row.HistoryKind != HistoryChange {
		return run(r, n, c)
	}
	if !n.Store.Undo || n.Store.Get == nil {
		key, _ := r.subjectKey(n, c)
		value, err := run(r, n, c)
		if err == nil {
			if !row.Subject {
				key = answerKey(n, value)
			}
			r.noteChange(ThingChange{Noun: n.Name, Key: key})
		}
		return value, err
	}
	read := func(key string) Thing {
		if t, err := n.Store.Get(c, key); err == nil && t != nil {
			return t
		}
		return nil
	}
	var names []string
	if row.Writes != nil && row.Subject {
		names, _ = row.Writes(n, c.Args)
	}
	key, keyed := "", false
	var before, whole map[string]any
	if row.Subject {
		if k, err := r.subjectKey(n, c); err == nil {
			key, keyed = k, true
			if t := read(k); t != nil {
				before = changeProps(n, t, names)
				if before == nil {
					before = map[string]any{}
				}
				if len(names) > 0 {
					whole = changeProps(n, t, nil)
				}
			}
		}
	}
	value, err := run(r, n, c)
	if err != nil {
		return value, err
	}
	if !keyed {
		if key = answerKey(n, value); key == "" {
			return value, nil
		}
	}
	var after map[string]any
	if before == nil {
		names = nil // a new thing: all of it
	}
	if t := read(key); t != nil {
		if whole != nil {
			// The store may change more than the request wrote (a move
			// that clears the parent): every writable prop that changed
			// is recorded too, so undo writes it back.
			for _, extra := range sideChanges(n, whole, changeProps(n, t, nil), names) {
				names = append(names, extra)
				before[extra] = whole[extra]
			}
		}
		after = changeProps(n, t, names)
		if after == nil {
			after = map[string]any{}
		}
	}
	if before != nil || after != nil {
		r.noteChange(ThingChange{Noun: n.Name, Key: key, Before: before, After: after})
	}
	return value, nil
}

// sideChanges are the writable props, beside the written ones, whose
// values differ between the whole thing before a write and after it, in
// prop order: what the store changed on its own (a read-only prop it
// derives is left out; undo could not write it back).
func sideChanges(n *Noun, before, after map[string]any, written []string) []string {
	var out []string
	for _, p := range writableProps(n) {
		if p.Key || slices.Contains(written, p.Name) {
			continue
		}
		if !reflect.DeepEqual(before[p.Name], after[p.Name]) {
			out = append(out, p.Name)
		}
	}
	return out
}

// answerKey is the key of the thing a verb answered (create's new thing),
// "" when it answered none.
func answerKey(n *Noun, value any) string {
	answer, _ := value.(map[string]any)
	kp := n.keyProp()
	if answer == nil || kp == nil || emptyProp(answer[kp.Name]) {
		return ""
	}
	return fmt.Sprint(answer[kp.Name])
}

// changeProps are a thing's props a change records: those named (every
// prop but the key when none is), each in its file form; a named prop the
// thing does not hold is nil.
func changeProps(n *Noun, t Thing, names []string) map[string]any {
	out := map[string]any{}
	for _, p := range n.Props {
		if p.Key || len(names) > 0 && !slices.Contains(names, p.Name) {
			continue
		}
		if v, ok := t[p.Name]; ok {
			out[p.Name] = fileForm(v)
		} else if len(names) > 0 {
			out[p.Name] = nil // written, unset before: undo unsets it
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// fileChanges are the changes a history line keeps: those with values
// (a mark holds none), and only when every value is simple
// (simpleChanges); nil otherwise.
func (r *Runtime) fileChanges(changes []ThingChange) []ThingChange {
	var out []ThingChange
	for _, c := range changes {
		if c.Before != nil || c.After != nil {
			out = append(out, c)
		}
	}
	if !r.simpleChanges(out) {
		return nil
	}
	return out
}

// simpleChanges reports whether every prop a set of changes records is
// simple (PropSpec.simple, owner decision 12): only then does the history
// file keep them, by the test that decides the line itself.
func (r *Runtime) simpleChanges(changes []ThingChange) bool {
	for _, c := range changes {
		n := r.Nouns[c.Noun]
		for _, m := range []map[string]any{c.Before, c.After} {
			for name := range m {
				if p := n.prop(name); p == nil || !p.simple() {
					return false
				}
			}
		}
	}
	return true
}

// undoTarget is the index in the ring of the entry history.undo reverts:
// the newest entry with changes left to undo, from this session. -1 when
// there is none.
func (r *Runtime) undoTarget() int {
	entries := r.history.entries
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Restored || !e.Result.OK {
			continue
		}
		if slices.ContainsFunc(e.Changes, func(c ThingChange) bool { return !c.Undone }) {
			return i
		}
	}
	return -1
}

// undoable reports why the entry at i cannot be undone, nil when it can:
// every change left to undo is on a store that declares Undo and has the
// inverse's handler.
func (r *Runtime) undoable(i int) error {
	if i < 0 {
		return errNothingToUndo
	}
	for _, c := range r.history.entries[i].Changes {
		if c.Undone {
			continue
		}
		n := r.Nouns[c.Noun]
		if n == nil || n.Store == nil || !n.Store.Undo {
			return fmt.Errorf("%s does not undo its changes", r.DisplayNoun(c.Noun))
		}
		switch {
		case c.Before == nil && c.After == nil:
			return fmt.Errorf("%s cannot undo: its store does not read things", r.DisplayNoun(c.Noun))
		case c.Before == nil && n.Store.Delete == nil:
			return fmt.Errorf("%s cannot undo a create: its store does not delete", r.DisplayNoun(c.Noun))
		case c.After == nil && (n.Store.Create == nil || !n.Store.AcceptsKey):
			return fmt.Errorf("%s cannot undo a delete: its store does not create under a given key", r.DisplayNoun(c.Noun))
		case c.Before != nil && c.After != nil && n.Store.Patch == nil:
			return fmt.Errorf("%s cannot undo a write: its store does not write", r.DisplayNoun(c.Noun))
		}
	}
	return nil
}

// errNothingToUndo is history.undo's answer when no change is left.
var errNothingToUndo = errors.New("nothing to undo")

// undoChange runs one change's inverse on its store.
func (r *Runtime) undoChange(c Ctx, ch ThingChange) error {
	n := r.Nouns[ch.Noun]
	typed := func(m map[string]any) Thing {
		out := Thing{}
		for name, v := range m {
			if p := n.prop(name); p != nil {
				if tv, err := propValue(*p, v); err == nil {
					v = tv
				}
			}
			out[name] = v
		}
		return out
	}
	switch {
	case ch.Before == nil:
		return n.Store.Delete(c, ch.Key)
	case ch.After == nil:
		values := typed(ch.Before)
		if key := n.keyProp(); key != nil {
			values[key.Name] = ch.Key
		}
		_, err := n.Store.Create(c, values)
		return err
	default:
		_, err := n.Store.Patch(c, ch.Key, typed(ch.Before))
		return err
	}
}

// undo reverts the newest change left (undoTarget): its changes' inverses
// in reverse order, each marked undone as it succeeds, so an undo that
// fails part way can be run again for the rest. One notice says what was
// undone.
func (r *Runtime) undo(c Context) (any, error) {
	i := r.undoTarget()
	if err := r.undoable(i); err != nil {
		if errors.Is(err, errNothingToUndo) {
			r.UI().Info(err.Error())
			return nil, nil
		}
		return nil, err
	}
	e := &r.history.entries[i]
	ctx := Ctx{Runtime: r, Context: c, Args: Arguments{}, Instance: r.Instances[focusOf(c)]}
	var keys []string
	for j := len(e.Changes) - 1; j >= 0; j-- {
		ch := &e.Changes[j]
		if ch.Undone {
			continue
		}
		if err := r.undoChange(ctx, *ch); err != nil {
			return nil, fmt.Errorf("undo %s %s: %w", r.DisplayNoun(ch.Noun), ch.Key, err)
		}
		ch.Undone = true
		keys = append(keys, ch.Key)
	}
	slices.Reverse(keys)
	name := r.DisplayKey(e.Request.Key())
	r.UI().Info(fmt.Sprintf("undid %s on %s", name, strings.Join(keys, ", ")))
	return map[string]any{"undone": e.Seq, "verb": name, "things": keys}, nil
}

func registerUndo(r *Runtime) {
	r.DefineGuard(UndoGuard, func(r *Runtime, _ KeyEvent) bool {
		if i := r.undoTarget(); i >= 0 {
			return r.undoable(i) == nil
		}
		// Nothing to undo: offered (it says so) only where some store
		// undoes, so an app with none never shows the verb.
		for _, n := range r.Nouns {
			if n.Store != nil && n.Store.Undo {
				return true
			}
		}
		return false
	})
	r.Register(Operation{Verb: "undo", Noun: "ochazuke.history", HistoryKind: HistoryChange,
		Description: "Undo the last standard change (create, set, delete; a visual selection's write as one): write the values before it back, delete what it created, create again what it deleted. Only on a store that declares Undo",
		Result:      "JSON value", Run: func(r *Runtime, c Context, a Arguments) (any, error) { return r.undo(c) }})
	r.SetAvailability("ochazuke.history.undo", UndoGuard)
}
