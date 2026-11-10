package ochazuke

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Undo of the standard writes (standard verbs phase 1, lane S2b; plan
// 2.8). Every standard change entry (a row with an Undo shape: create,
// set, delete, archive, and the prop rows that run as set) records what it
// changed: one ThingChange per
// thing, with the changed props' values before and after, in the shape of
// the csv example's Patch (examples/csv/app.go): a create has no Before, a
// delete no After. A visual selection's write is one entry (Ctx.Each), so
// its changes are one group and undo as one.
//
// history-entry.undo reverts the newest entry not yet undone that holds changes,
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
// Undone marks a change history-entry.undo reverted.
type ThingChange struct {
	Noun   string         `json:"noun"`
	Key    string         `json:"key,omitempty"`
	Before map[string]any `json:"before,omitempty"`
	After  map[string]any `json:"after,omitempty"`
	Undone bool           `json:"undone,omitempty"`
	// Relation, when set, is a change to a stored relation (Before nil: it
	// was made, After nil: it was removed, both: a row rewritten); the
	// change has no Before or After props. Undo puts the row back.
	Relation *RelationChange `json:"relation,omitempty"`
}

// UndoGuard offers history-entry.undo when a newest change exists, including one
// the handler must explain it cannot undo; with none, a store declares Undo.
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

// runRecording runs a standard verb's handler and, when its row records a
// change (Undo), notes what it did to the thing, read with the store's Get
// around the run: the thing before (a verb that acts on an existing
// thing), and the thing after under the subject's key, or under the key
// prop of the thing it answers (a verb that Creates one: undo deletes it).
// A thing that was not there before has no Before, one that is gone after
// no After; when both are there, the props the request writes (the row's
// Writes) are kept, and every other writable prop whose value the write
// changed (sideChanges: what the store did on its own, as a move clearing
// a parent), so undo restores all of them. No verb is named: the shapes
// come from the row (Undo, Creates) and the store.
//
// A store that does not declare Undo (or has no Get) is not read around
// its writes, which would cost it two reads per write for nothing: its
// change is noted by noun and key alone, a mark with no values, so
// history-entry.undo explains why it cannot undo rather than skip to an older one.
func (r *Runtime) runRecording(row standardVerb, n *Noun, c Ctx) (any, error) {
	run := row.Run
	if row.Undo == undoNone {
		return run(r, n, c)
	}
	// The thing it acts on: its subject, or (Creates) the one it answers.
	subject := row.TakesSubject && !row.Creates
	if !n.Store.Undo || n.Store.Get == nil {
		key, _ := r.subjectKey(n, c)
		value, err := run(r, n, c)
		if err == nil {
			if !subject {
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
	if row.Writes != nil && subject {
		names, _ = row.Writes(n, c.Args)
	}
	key, keyed := "", false
	var before, whole map[string]any
	if subject {
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
	r.noteAfter(row, n, key, value, read, before, whole, names)
	return value, nil
}

// noteAfter notes the change a standard write made, once it has run: the
// thing after it is the source's own answer when the store is remote (no
// second call), else read back. A thing that cannot be read back is kept as
// a mark (noun and key), never as a delete.
func (r *Runtime) noteAfter(row standardVerb, n *Noun, key string, value any, read func(string) Thing, before, whole map[string]any, names []string) {
	var after map[string]any
	if before == nil {
		names = nil // a new thing: all of it
	}
	t, readFailed := Thing(nil), false
	if answer, ok := value.(map[string]any); ok && n.Store.Remote && len(answer) > 0 && row.Undo != undoByRecreate {
		t = Thing(answer) // the source's own answer after the write: no second call
	} else if t = read(key); t == nil && row.Undo != undoByRecreate {
		readFailed = true
	}
	if readFailed && before != nil {
		// The thing after could not be read: keep the change as a mark, as for
		// a store that does not undo, rather than lose it or record a delete.
		r.noteChange(ThingChange{Noun: n.Name, Key: key})
		return
	}
	if t != nil {
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
// file keep them, by the test that decides the line itself. The base times
// the framework stamps (created, updated) are not the writer's values and
// never make a write unsimple.
func (r *Runtime) simpleChanges(changes []ThingChange) bool {
	for _, c := range changes {
		if c.Relation != nil {
			return false
		}
		n := r.Nouns[c.Noun]
		for _, m := range []map[string]any{c.Before, c.After} {
			for name := range m {
				if p := n.prop(name); p == nil || !p.simple() && p.Role != RoleCreated && p.Role != RoleUpdated {
					return false
				}
			}
		}
	}
	return true
}

// undoTarget is the index in the ring of the entry history-entry.undo reverts:
// the newest entry with changes left to undo, from this session. -1 when
// there is none.
func (r *Runtime) undoTarget() int {
	entries := r.history.entries
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Restored || !e.Result.OK || (r.Actor().IsAgent() && !r.Actor().SameAgent(e.Context.Actor)) {
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
		if c.Relation != nil {
			if r.RelationStore() == nil {
				return errNoRelationStore
			}
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

// errNothingToUndo is history-entry.undo's answer when no change is left.
var errNothingToUndo = errors.New("nothing to undo")

// undoChange runs one change's inverse on its store.
func (r *Runtime) undoChange(c Ctx, ch ThingChange) error {
	if ch.Relation != nil {
		return r.undoRelation(*ch.Relation)
	}
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

// undoRun is what one run of an undo's body did: the changes it reverted
// (the entry's seq and the change's index in it) and the keys of their things.
type undoRun struct {
	done []undoneChange
	keys []string
}

// labels names each undone thing (keys, in order) for a person: its label
// when a reverted change holds it (a rename's before or after), else its
// key.
func (out *undoRun) labels(r *Runtime, keys []string) []string {
	names := slices.Clone(keys)
	for i, key := range keys {
		for _, d := range out.done {
			e := r.entryOf(d.seq)
			if e == nil {
				continue
			}
			ch := e.Changes[d.at]
			n := r.Noun(ch.Noun)
			if ch.Key != key || n == nil {
				continue
			}
			for _, side := range []map[string]any{ch.After, ch.Before} {
				if label := n.displayLabel(func(name string) any { return side[name] }); label != "" {
					names[i] = label
				}
			}
		}
	}
	return names
}

type undoneChange struct {
	seq uint64
	at  int
}

// undoVerb reverts the selected entry and the entries of its group
// (undoGroupOf): their changes' inverses in reverse order, each marked
// undone as it succeeds, so an undo that fails part way can be run again
// for the rest. One notice says what was undone. Over a remote store the
// inverses are store calls, so the undo runs as a job (remote.go) and marks
// and notices only when it ends.
func (r *Runtime) undoVerb(c Ctx) Result {
	a := c.Args
	i, err := r.undoEntry(a)
	if err != nil {
		return Failed(err)
	}
	if err := r.undoable(i); err != nil {
		if errors.Is(err, errNothingToUndo) {
			r.UI().Info(err.Error())
			return Done(nil)
		}
		return Failed(err)
	}
	var seqs []uint64
	remote := false
	for _, j := range r.undoGroupOf(i) {
		if err := r.undoable(j); err != nil {
			return Failed(err)
		}
		e := r.history.entries[j]
		seqs = append(seqs, e.Seq)
		for _, ch := range e.Changes {
			if n := r.Nouns[ch.Noun]; n != nil && n.Store != nil && n.Store.Remote {
				remote = true
			}
		}
	}
	seq := r.history.entries[i].Seq
	_, targeted := a["entry"]
	targeted = targeted || r.Actor().IsAgent()
	var out undoRun
	body := r.undoBody(seqs, targeted, &out)
	finish := r.undoFinish(seq, &out)
	if !remote {
		_, err := body(c)
		value, err := finish(nil, err)
		if err != nil {
			return Failed(err)
		}
		return Done(value)
	}
	return r.runSteppedBody(c, "ochazuke.history-entry.undo", body, finish)
}

// undoBody is the work of an undo over the entries seqs: the inverse of each
// change not yet undone, newest first. It notes what it reverted in out, and
// is repeatable (a remote store runs it again for each answer).
func (r *Runtime) undoBody(seqs []uint64, targeted bool, out *undoRun) func(Ctx) (any, error) {
	return func(ctx Ctx) (any, error) {
		*out = undoRun{}
		if targeted {
			for _, s := range seqs {
				if e := r.entryOf(s); e != nil {
					if err := r.undoCurrent(ctx, e.Changes); err != nil {
						return nil, err
					}
				}
			}
		}
		for _, s := range seqs {
			e := r.entryOf(s)
			if e == nil {
				continue
			}
			for k := len(e.Changes) - 1; k >= 0; k-- {
				ch := e.Changes[k]
				if ch.Undone {
					continue
				}
				if err := r.undoChange(ctx, ch); err != nil {
					return nil, fmt.Errorf("undo %s %s: %w", r.DisplayNoun(ch.Noun), ch.Key, err)
				}
				out.done = append(out.done, undoneChange{s, k})
				if !slices.Contains(out.keys, ch.Key) {
					out.keys = append(out.keys, ch.Key)
				}
			}
		}
		return nil, nil
	}
}

// undoFinish marks the changes an undo reverted and says so, once it ends.
func (r *Runtime) undoFinish(seq uint64, out *undoRun) func(any, error) (any, error) {
	return func(_ any, err error) (any, error) {
		for _, d := range out.done {
			if e := r.entryOf(d.seq); e != nil {
				e.Changes[d.at].Undone = true // each one that succeeded, even when a later one failed
			}
		}
		if err != nil {
			return nil, err
		}
		e := r.entryOf(seq)
		if e == nil {
			return nil, nil
		}
		keys := slices.Clone(out.keys)
		slices.Reverse(keys)
		name := r.DisplayKey(e.Request.Key())
		summary := fmt.Sprintf("undid %s on %s", name, strings.Join(out.labels(r, keys), ", "))
		r.UI().Info(summary)
		return map[string]any{"undone": e.Seq, "verb": name, "things": keys, "summary": summary}, nil
	}
}

// entryOf is the history entry with that seq, nil when the ring dropped it.
func (r *Runtime) entryOf(seq uint64) *HistoryEntry {
	for i := range r.history.entries {
		if r.history.entries[i].Seq == seq {
			return &r.history.entries[i]
		}
	}
	return nil
}

// undoGroupOf is the entries history-entry.undo reverts with the one at i,
// newest first: i alone, or every entry of its group (HistoryEntry.Group)
// with changes left to undo, the same actor's when an agent undoes.
func (r *Runtime) undoGroupOf(i int) []int {
	entries := r.history.entries
	group := entries[i].Group
	if group == "" {
		return []int{i}
	}
	var out []int
	for j := i; j >= 0; j-- {
		e := entries[j]
		if e.Group != group || e.Restored || !e.Result.OK || (r.Actor().IsAgent() && !r.Actor().SameAgent(e.Context.Actor)) {
			continue
		}
		if slices.ContainsFunc(e.Changes, func(c ThingChange) bool { return !c.Undone }) {
			out = append(out, j)
		}
	}
	return out
}

func registerUndo(r *Runtime) {
	r.DefineGuard(UndoGuard, func(r *Runtime, e KeyEvent) bool {
		// An unundoable newest change must explain its refusal, not hide
		// the action or pass over it for an older change.
		if _, targeted := e.Args["entry"]; targeted {
			return true
		}
		if i := r.undoTarget(); i >= 0 {
			return true // the handler explains an unundoable newest change
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
	r.Register(Operation{Key: "ochazuke.history-entry.undo", HistoryKind: HistoryChange,
		Description: "Undo a standard change by entry seq, or the newest change; agents undo only their own entries. Only on a store that declares Undo",
		Parameters:  map[string]Parameter{"entry": {Type: "integer", Description: "History entry seq; omitted, the newest change by this actor (owner: any actor)"}},
		Result:      "JSON value", Summary: "summary", AppHandler: r.undoVerb})
	r.SetAvailability("ochazuke.history-entry.undo", UndoGuard)
}

// undoEntry selects an explicit sequence, or the newest eligible change.
func (r *Runtime) undoEntry(a Arguments) (int, error) {
	if _, targeted := a["entry"]; !targeted {
		return r.undoTarget(), nil
	}
	seq := IntArg(a, "entry", 0)
	if seq <= 0 {
		return -1, errors.New("entry must be a positive history seq")
	}
	for i, e := range r.history.entries {
		if e.Seq != uint64(seq) {
			continue
		}
		if r.Actor().IsAgent() && !r.Actor().SameAgent(e.Context.Actor) {
			return -1, fmt.Errorf("an agent may undo only its own history entries; entry %d belongs to %s", seq, e.Context.Actor)
		}
		if e.Restored {
			return -1, fmt.Errorf("history entry %d is from an earlier session and cannot be undone", seq)
		}
		if !e.Result.OK || !slices.ContainsFunc(e.Changes, func(c ThingChange) bool { return !c.Undone }) {
			return -1, fmt.Errorf("history entry %d has no changes left to undo", seq)
		}
		return i, nil
	}
	return -1, fmt.Errorf("history entry %d is not in this session's change history", seq)
}

// undoCurrent checks every inverse before any write. Targeting an older entry
// must not overwrite subsequent edits (by the owner or another agent).
func (r *Runtime) undoCurrent(ctx Ctx, changes []ThingChange) error {
	for _, ch := range changes {
		if ch.Undone || ch.Relation != nil {
			continue
		}
		n := r.Nouns[ch.Noun]
		if n.Store.Get == nil {
			return fmt.Errorf("%s cannot check whether the thing changed since this entry", r.DisplayNoun(ch.Noun))
		}
		current, err := n.Store.Get(ctx, ch.Key)
		// Get commonly reports absence as an untyped error. If undo would
		// recreate a deleted key, an unfiltered listing can prove it is free.
		// A failed read alone must never be treated as proof of absence.
		if err != nil && ch.After == nil && n.Store.List != nil {
			rows, listErr := n.Store.List(ctx, ListQuery{})
			if listErr != nil {
				return fmt.Errorf("undo %s %s: %w", r.DisplayNoun(ch.Noun), ch.Key, listErr)
			}
			if slices.ContainsFunc(rows, func(t Thing) bool { return answerKey(n, t) == ch.Key }) {
				return fmt.Errorf("%s %s changed since this history entry; undo would overwrite later work", r.DisplayNoun(ch.Noun), ch.Key)
			}
			current, err = nil, nil
		}
		if err != nil {
			return fmt.Errorf("undo %s %s: %w", r.DisplayNoun(ch.Noun), ch.Key, err)
		}
		same := current == nil && ch.After == nil
		if current != nil && ch.After != nil {
			var names []string
			// Undoing a create deletes the entire thing, so also check
			// props that were absent at creation and added by later work.
			if ch.Before != nil {
				for name := range ch.After {
					names = append(names, name)
				}
			}
			after := changeProps(n, current, names)
			if after == nil {
				after = map[string]any{}
			}
			same = reflect.DeepEqual(after, ch.After)
		}
		if !same {
			return fmt.Errorf("%s %s changed since this history entry; undo would overwrite later work", r.DisplayNoun(ch.Noun), ch.Key)
		}
	}
	return nil
}
