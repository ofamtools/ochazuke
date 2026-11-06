package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"
)

// Copy stores: malleable things (DESIGN-001 "Malleable copies", owner
// 2026-11-05). CopyStore(source, local) is a Store whose things are copies
// held by the application (LocalStore) of the things a source has:
//
//   - the first read of a thing (get, or a list that meets it) copies it
//     from the source once, the backfill; after that the copy IS the thing:
//     no background sync, no fingerprints, no remembered originals;
//   - create and archive are the copy's alone, and so is set, except the
//     props the noun declares Through (PropSpec.Through), which set sends on
//     to the source's Patch too;
//   - delete removes the copy and keeps a tombstone, so the next list does
//     not copy the thing in again; copies.delete source deletes it at the
//     source as well. A source's real actions (deleting a git branch) stay
//     explicit verbs of the noun (Store.CopySource);
//   - the copy stamps the base times (created, updated) itself: the
//     source's own times are props like any other;
//   - refresh (copy_refresh.go) compares the copies with the source on
//     demand and shows a plain diff; differences never block anything.
//
// A copy over a remote source (Store.Remote) is remote too: its verbs run
// as jobs whenever the source is asked, and every run of a plan makes the
// same source calls (tape.missing and tape.filled), whatever an earlier run
// wrote to the copy. Writes to the source come before the copy's own write,
// so a plan's later runs read the copy as it was.

// LocalStore is where a copy store keeps its copies: get, put, list and
// delete of things by key. Things are stored whole; List answers every one
// (tombstones included, CopyTombstone), in the order they were first put.
// NewMemoryLocal is one in memory; an application keeps them in its own
// database.
type LocalStore interface {
	Get(key string) (Thing, bool, error)
	Put(key string, t Thing) error
	List() ([]Thing, error)
	Delete(key string) error
}

// CopyTombstone is the field a deleted copy's tombstone holds (true),
// beside its key: the thing is gone here and is never copied in again. No
// prop can take the name (a prop name is one word).
const CopyTombstone = "copy.deleted"

// The copy store settings.
const (
	// CopiesDeleteSetting is what the standard delete of a copied thing
	// does: CopiesDeleteLocal (the default) deletes the copy alone and
	// keeps a tombstone; CopiesDeleteSource deletes it at the source too.
	CopiesDeleteSetting = "copies.delete"
	CopiesDeleteLocal   = "local"
	CopiesDeleteSource  = "source"
	// CopiesMarksSetting marks a list row whose copy differs from what the
	// last refresh read at the source (copyMarkText). Default on; off
	// shows no mark.
	CopiesMarksSetting = "copies.marks"
)

// MemoryLocal is a LocalStore in memory, for tests and demos.
type MemoryLocal struct {
	mu     sync.Mutex
	order  []string
	things map[string]Thing
}

// NewMemoryLocal is an empty MemoryLocal.
func NewMemoryLocal() *MemoryLocal { return &MemoryLocal{things: map[string]Thing{}} }

func (m *MemoryLocal) Get(key string) (Thing, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.things[key]
	return maps.Clone(t), ok, nil
}

func (m *MemoryLocal) Put(key string, t Thing) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.things[key]; !ok {
		m.order = append(m.order, key)
	}
	m.things[key] = maps.Clone(t)
	return nil
}

func (m *MemoryLocal) List() ([]Thing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Thing, 0, len(m.order))
	for _, k := range m.order {
		out = append(out, maps.Clone(m.things[k]))
	}
	return out, nil
}

func (m *MemoryLocal) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.things, key)
	m.order = slices.DeleteFunc(m.order, func(k string) bool { return k == key })
	return nil
}

// copyStore is a copy store's state: its source, its copies, and the noun
// it is bound to (NounBuilder.Store), whose props it reads.
type copyStore struct {
	noun   string
	source Store
	local  LocalStore
}

// CopyStore is a store of copies of source's things kept in local (see the
// file comment). It is a collection store; declare it on a noun with
// NounBuilder.Store, which binds it to the noun (its key and Through
// props). The source's other fields (Confirm, When, ListParams) are not
// carried over: set them on the answer.
func CopyStore(source Store, local LocalStore) Store {
	cs := &copyStore{source: source, local: local}
	return cs.fill(Store{Kind: Collection})
}

// CopySource is the source a copy store copies from; false for any other
// store. A noun's explicit source verbs (a real delete) call it.
func (s Store) CopySource() (Store, bool) {
	if s.copy == nil {
		return Store{}, false
	}
	return s.copy.source, true
}

// bind is the store s (a CopyStore answer, maybe with fields the
// application set) bound to noun.
func (cs *copyStore) bind(noun string, s Store) Store {
	bound := &copyStore{noun: noun, source: cs.source, local: cs.local}
	return bound.fill(s)
}

// fill gives s the copy's calls.
func (cs *copyStore) fill(s Store) Store {
	s.Kind = Collection
	s.copy = cs
	s.Remote = cs.source.Remote
	s.Undo, s.AcceptsKey, s.Stamps, s.Facts = true, true, false, false
	s.Get, s.List, s.Create, s.Patch, s.Delete = cs.get, cs.list, cs.create, cs.patch, cs.delete
	s.Query, s.Watch = nil, nil
	return s
}

// nounOf is the noun the copy is bound to and its key prop.
func (cs *copyStore) nounOf(c Ctx) (*Noun, *PropSpec, error) {
	n := c.Runtime.Nouns[cs.noun]
	if cs.noun == "" || n == nil {
		return nil, nil, errors.New("copy store: declare it on a noun (NounBuilder.Store)")
	}
	kp := n.keyProp()
	if kp == nil {
		return nil, nil, fmt.Errorf("copy store %s: it needs a key prop", cs.noun)
	}
	return n, kp, nil
}

func (cs *copyStore) unknown(c Ctx, key string) error {
	return fmt.Errorf("unknown %s %q", c.Runtime.DisplayNoun(cs.noun), key)
}

func tombstoned(t Thing) bool { b, _ := t[CopyTombstone].(bool); return b }

// thingKey is a thing's key as text, "" when it has none.
func thingKey(kp *PropSpec, t Thing) string {
	if emptyProp(t[kp.Name]) {
		return ""
	}
	return fmt.Sprint(t[kp.Name])
}

// ask reports whether a read of key must copy it in from the source: the
// copy lacks it (had is false). In a remote plan the answer is the one the
// plan's first read of it gave, until the current run has filled it, so a
// copy an earlier run wrote does not change the calls a run makes.
func (cs *copyStore) ask(c Ctx, key string, had bool) bool {
	t := c.tape
	if t == nil {
		return !had
	}
	id := cs.noun + "\x00" + key
	if t.missing == nil {
		t.missing = map[string]bool{}
	}
	missing, seen := t.missing[id]
	if !seen {
		missing = !had
		t.missing[id] = missing
	}
	return missing && !t.filled[id]
}

// filled notes that the current run copied key in.
func (cs *copyStore) filled(c Ctx, key string) {
	if t := c.tape; t != nil {
		if t.filled == nil {
			t.filled = map[string]bool{}
		}
		t.filled[cs.noun+"\x00"+key] = true
	}
}

// backfill copies a source thing in: the copy stamps its own base times.
func (cs *copyStore) backfill(c Ctx, n *Noun, key string, src Thing) (Thing, error) {
	t := maps.Clone(src)
	if t == nil {
		t = Thing{}
	}
	delete(t, CopyTombstone)
	n.stamp(t, RoleCreated, RoleUpdated)
	if err := cs.local.Put(key, t); err != nil {
		return nil, err
	}
	cs.filled(c, key)
	return maps.Clone(t), nil
}

func (cs *copyStore) get(c Ctx, key string) (Thing, error) {
	n, _, err := cs.nounOf(c)
	if err != nil {
		return nil, err
	}
	t, had, err := cs.local.Get(key)
	if err != nil {
		return nil, err
	}
	if had && tombstoned(t) {
		return nil, cs.unknown(c, key)
	}
	if !cs.ask(c, key, had) {
		if !had {
			return nil, cs.unknown(c, key)
		}
		return t, nil
	}
	if cs.source.Get == nil {
		return nil, cs.unknown(c, key)
	}
	src, err := cs.source.Get(c, key)
	if err != nil {
		return nil, err
	}
	return cs.backfill(c, n, key, src)
}

// sourceList is every thing the source lists for q: its List, else its
// Query's first page; nil when it lists nothing.
func (cs *copyStore) sourceList(c Ctx, q ListQuery) ([]Thing, error) {
	switch {
	case cs.source.List != nil:
		return cs.source.List(c, q)
	case cs.source.Query != nil:
		page, err := cs.source.Query(c, ListQuery{Filters: q.Filters, Archived: true, Args: q.Args})
		return page.Items, err
	}
	return nil, nil
}

// list is the copies, the source's order first: a thing the source lists
// that has no copy (and no tombstone) is copied in, and the copies the
// source no longer lists (or never did: created here) follow.
func (cs *copyStore) list(c Ctx, q ListQuery) ([]Thing, error) {
	n, kp, err := cs.nounOf(c)
	if err != nil {
		return nil, err
	}
	srcs, err := cs.sourceList(c, q)
	if err != nil {
		return nil, err
	}
	locals, err := cs.local.List()
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]Thing, len(locals))
	for _, t := range locals {
		if k := thingKey(kp, t); k != "" {
			byKey[k] = t
		}
	}
	out := make([]Thing, 0, len(locals)+len(srcs))
	seen := map[string]bool{}
	for _, src := range srcs {
		k := thingKey(kp, src)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		if t, ok := byKey[k]; ok {
			if !tombstoned(t) {
				out = append(out, t)
			}
			continue
		}
		t, err := cs.backfill(c, n, k, src)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	for _, t := range locals {
		k := thingKey(kp, t)
		if k == "" || seen[k] || tombstoned(t) {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	return out, nil
}

// create makes a thing in the copy alone: under the key the values give
// (an undone delete's), else a new one.
func (cs *copyStore) create(c Ctx, values Thing) (Thing, error) {
	_, kp, err := cs.nounOf(c)
	if err != nil {
		return nil, err
	}
	t := maps.Clone(values)
	if t == nil {
		t = Thing{}
	}
	key := thingKey(kp, t)
	if key == "" {
		if key, err = cs.newKey(kp); err != nil {
			return nil, err
		}
		t[kp.Name] = key
		if kp.Type == SettingInteger {
			t[kp.Name], _ = strconv.Atoi(key)
		}
	} else if old, ok, err := cs.local.Get(key); err != nil {
		return nil, err
	} else if ok && !tombstoned(old) {
		return nil, fmt.Errorf("%s %q is already there", c.Runtime.DisplayNoun(cs.noun), key)
	}
	if err := cs.local.Put(key, t); err != nil {
		return nil, err
	}
	return maps.Clone(t), nil
}

// newKey is a key no copy holds: the next number for an integer key,
// else local-N.
func (cs *copyStore) newKey(kp *PropSpec) (string, error) {
	locals, err := cs.local.List()
	if err != nil {
		return "", err
	}
	taken := map[string]bool{}
	top := 0
	for _, t := range locals {
		k := thingKey(kp, t)
		taken[k] = true
		if i, err := strconv.Atoi(k); err == nil {
			top = max(top, i)
		}
	}
	if kp.Type == SettingInteger {
		return strconv.Itoa(top + 1), nil
	}
	for i := len(locals) + 1; ; i++ {
		if k := "local-" + strconv.Itoa(i); !taken[k] {
			return k, nil
		}
	}
}

// patch writes changes to the copy; the props the noun declares Through
// go to the source's Patch first. A source that refuses them does not lose
// the write: the copy keeps it and an error notice says the source did not
// take it (record first, never refuse).
func (cs *copyStore) patch(c Ctx, key string, changes Thing) (Thing, error) {
	n, _, err := cs.nounOf(c)
	if err != nil {
		return nil, err
	}
	t, err := cs.get(c, key)
	if err != nil {
		return nil, err
	}
	through := Thing{}
	for name, v := range changes {
		if p := n.prop(name); p != nil && p.Through {
			through[p.Name] = v
		}
	}
	var srcErr error
	if len(through) > 0 {
		if cs.source.Patch == nil {
			srcErr = errors.New("the source writes no props")
		} else {
			_, srcErr = cs.source.Patch(c, key, through)
		}
	}
	maps.Copy(t, changes)
	if err := cs.local.Put(key, t); err != nil {
		return nil, err
	}
	if srcErr != nil {
		c.Error(fmt.Sprintf("%s %s: kept here, not written to the source: %v", c.Runtime.DisplayNoun(cs.noun), key, srcErr))
	}
	return maps.Clone(t), nil
}

// delete removes the copy and keeps its tombstone; with copies.delete
// source the source deletes the thing first.
func (cs *copyStore) delete(c Ctx, key string) error {
	_, kp, err := cs.nounOf(c)
	if err != nil {
		return err
	}
	if _, err := cs.get(c, key); err != nil {
		return err
	}
	if c.Runtime.Settings().String(CopiesDeleteSetting) == CopiesDeleteSource {
		if cs.source.Delete == nil {
			return errors.New("the source deletes no things (copies.delete is source)")
		}
		if err := cs.source.Delete(c, key); err != nil {
			return err
		}
	}
	stone := Thing{kp.Name: key, CopyTombstone: true}
	if kp.Type == SettingInteger {
		if i, err := strconv.Atoi(key); err == nil {
			stone[kp.Name] = i
		}
	}
	return cs.local.Put(key, stone)
}
