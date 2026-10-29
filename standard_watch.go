package ochazuke

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// The standard verb watch (standardVerbs' row): a stream of what happens
// to a noun's things, one WatchEvent per thing added, modified or deleted.
// Its subject is one thing (the key parameter, never filled from the
// focus) or the things where matches, and since resumes from a hash an
// earlier event carried.
//
// A store with a Watch handler answers the events itself: it pushes them,
// from any goroutine, and the runtime hands them to the subscriber on the
// loop (a command that waits for the next batch, re-armed after each, as
// a Bubble Tea subscription is). A store without one is watched through
// its List (a singleton's Get): the watch keeps each thing's record hash
// (RecordHash, the state delta protocol's) and, whenever the runtime is
// told its things may have changed, lists again and emits the difference.
// It is told after every request of the change kind (a top-level one, once
// it has run), after the completion of a job of a change verb, and when the
// application calls Runtime.Changed (its store changed some other way).
// There is no polling loop: nothing runs while nothing happens.
//
// Each list a fallback watch sees is a version (its things' ids, sorted,
// and record hashes) kept under the list hash; an event's Hash is the
// version after it, and since=HASH starts from that version, emitting
// what changed since. The versions live in memory, a few per watch
// (SetWatchVersions replaces where: the CLI keeps them in its state cache,
// so a later command resumes).
//
// In process the watch is a subscription (Runtime.Watch, with a callback
// called on the loop, so a list can patch its rows from it). The verb
// answers the *Watch: the CLI streams its events as JSON Lines until
// interrupted; any other caller that encodes the answer (the palette, a
// socket) gets the events so far as an array, and the watch ends.

// The types of a WatchEvent.
const (
	WatchAdded    = "added"
	WatchModified = "modified"
	WatchDeleted  = "deleted"
)

// WatchEvent is one change to a watched thing: its type, its key (id, ""
// for a singleton) and the thing as get answers it (every prop but the
// secret ones). A deleted thing carries its last record when the watch saw
// it, else none. Hash is the since a later watch resumes from: a fallback
// watch's list hash after the change, or what a store's Watch sets.
type WatchEvent struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Thing map[string]any `json:"thing,omitempty"`
	Hash  string         `json:"hash,omitempty"`
}

// WatchQuery is what a store's Watch handler is asked for: one thing (Key)
// or the things whose props equal Where (typed by their props; empty for
// every thing), from Since ("" for now: the store chooses whether to send
// the current things first).
type WatchQuery struct {
	Key   string
	Where map[string]any
	Since string
}

// WatchVersions keeps the versions a fallback watch resumes from (since):
// per scope (a noun and its query), a version's sorted ids and their record
// hashes under the list hash. A version that is gone answers ok false.
type WatchVersions interface {
	ReadVersion(scope, hash string) (ids, hashes []string, ok bool)
	WriteVersion(scope, hash string, ids, hashes []string) error
}

// RecordHash is a record's hash: SHA-256 (128 bits, hex) of its encoded
// line, surrounding space trimmed. The state delta protocol hashes row
// lines with it (cli.RecordHash) and watch hashes things.
func RecordHash(line []byte) string {
	sum := sha256.Sum256(bytes.TrimSpace(line))
	return hex.EncodeToString(sum[:16])
}

// ListHash is a list's content hash: SHA-256 (128 bits, hex) of its record
// hashes in order, one per line. An empty list hashes too.
func ListHash(records []string) string {
	h := sha256.New()
	for _, r := range records {
		h.Write([]byte(r))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// watchHub is the runtime's watches and where their versions live.
type watchHub struct {
	watches    []*Watch
	versions   WatchVersions
	refreshing bool
}

func (r *Runtime) watchHub() *watchHub {
	if r.watches == nil {
		r.watches = &watchHub{versions: newMemoryVersions()}
	}
	return r.watches
}

// SetWatchVersions sets where fallback watches keep their versions (nil:
// in memory, the default) and answers the previous keeper, to restore.
func (r *Runtime) SetWatchVersions(v WatchVersions) WatchVersions {
	h := r.watchHub()
	prev := h.versions
	if v == nil {
		v = newMemoryVersions()
	}
	h.versions = v
	return prev
}

// Watch is one watch of a noun's things (Runtime.Watch, the watch verb).
// Its methods run on the runtime's loop, like the runtime's own.
type Watch struct {
	r     *Runtime
	noun  string
	key   string
	where map[string]any
	scope string
	ctx   Ctx
	fn    func(WatchEvent)
	queue []WatchEvent
	known map[string]watchKnown
	hash  string
	err   error
	ended bool
	// A store's Watch: its events, queued by any goroutine; signal wakes
	// the waiting command; done ends it.
	push      bool
	mu        sync.Mutex
	pushed    []WatchEvent
	signal    chan struct{}
	done      chan struct{}
	stopStore func()
}

// watchKnown is what a fallback watch knows of a thing: its record hash
// and, once it has seen it, its record.
type watchKnown struct {
	hash   string
	record map[string]any
}

// Watch subscribes to noun's things: args are the watch verb's (the key
// prop for one thing, where, since). fn receives each event on the loop;
// nil queues them for Take. A store without a Watch handler is answered
// at once with the events since (every thing as added when since is
// empty). Stop ends it.
func (r *Runtime) Watch(noun string, args Arguments, fn func(WatchEvent)) (*Watch, error) {
	return r.watch(Ctx{Runtime: r, Context: r.Context(), Args: args}, noun, fn)
}

func (r *Runtime) watch(c Ctx, noun string, fn func(WatchEvent)) (*Watch, error) {
	n := r.Nouns[r.CanonicalNoun(noun)]
	if n == nil || n.Store == nil {
		return nil, fmt.Errorf("%s has no store", noun)
	}
	if !watchFits(*n.Store) {
		return nil, fmt.Errorf("%s cannot be watched: its store has no Watch, List or (for a singleton) Get", r.DisplayNoun(n.Name))
	}
	where, err := parseWhere(n, StringArg(c.Args, "where", ""))
	if err != nil {
		return nil, err
	}
	w := &Watch{r: r, noun: n.Name, where: where, ctx: c, fn: fn}
	if key := n.keyProp(); key != nil {
		if v, ok := c.Args[key.Name]; ok && !emptyProp(v) {
			w.key = fmt.Sprint(v)
		}
	}
	since := StringArg(c.Args, "since", "")
	if s := n.Store; s.Watch != nil {
		w.push, w.signal, w.done = true, make(chan struct{}, 1), make(chan struct{})
		stop, err := s.Watch(c, WatchQuery{Key: w.key, Where: where, Since: since}, w.emit)
		if err != nil {
			return nil, err
		}
		w.stopStore = stop
		r.commands = append(r.commands, w.listen())
	} else {
		w.scope = watchScope(n.Name, w.key, where)
		if err := w.start(since); err != nil {
			return nil, err
		}
	}
	h := r.watchHub()
	h.watches = append(h.watches, w)
	return w, nil
}

// watchFits reports whether a store can be watched: it has Watch, or List,
// or it is a singleton with Get (the thing it watches).
func watchFits(s Store) bool {
	return s.Watch != nil || s.List != nil || s.Kind == Singleton && s.Get != nil
}

// watchScope is the scope a fallback watch keeps its versions under: the
// noun and its query.
func watchScope(noun, key string, where map[string]any) string {
	data, _ := json.Marshal(where) // keys sorted
	return noun + ".watch\x00" + key + "\x00" + string(data)
}

// Changed tells the watches of nouns (every watch when none is named) that
// their things may have changed: each fallback watch lists again and
// emits the difference. An application calls it when its store changed
// other than through a verb of the change kind (an editor's write, a
// daemon's event delivered on the loop). Call it on the loop.
func (r *Runtime) Changed(nouns ...string) {
	if r.watches == nil {
		return
	}
	names := make([]string, len(nouns))
	for i, n := range nouns {
		names[i] = r.CanonicalNoun(n)
	}
	r.refreshWatches(names)
}

// refreshWatches lists every fallback watch of nouns (all when none) again.
func (r *Runtime) refreshWatches(nouns []string) {
	h := r.watches
	if h == nil || h.refreshing || len(h.watches) == 0 {
		return
	}
	h.refreshing = true
	defer func() { h.refreshing = false }()
	for _, w := range slices.Clone(h.watches) {
		if !w.push && !w.ended && (len(nouns) == 0 || slices.Contains(nouns, w.noun)) {
			w.refresh()
		}
	}
}

// afterRequest refreshes the watches once a top-level request of the
// change kind has run.
func (r *Runtime) watchAfterRequest(q Request, depth int) {
	if depth == 0 && r.watches != nil && r.historyKind(q) == HistoryChange {
		r.refreshWatches(nil)
	}
}

// watchAfterJob refreshes the watches once a job of a change verb is done.
func (r *Runtime) watchAfterJob(verb string) {
	if r.watches == nil {
		return
	}
	if op, ok := r.Operations[verb]; ok && op.HistoryKindOf() == HistoryChange {
		r.refreshWatches(nil)
	}
}

// watchSnapshot is a fallback watch's list: ids in the store's order, each
// one's record and record hash.
type watchSnapshot struct {
	ids     []string
	records map[string]map[string]any
	hashes  map[string]string
}

// version is the snapshot as a version: its ids sorted, their hashes, and
// the list hash.
func (s watchSnapshot) version() (ids, hashes []string, hash string) {
	ids = slices.Clone(s.ids)
	sort.Strings(ids)
	hashes = make([]string, len(ids))
	for i, id := range ids {
		hashes[i] = s.hashes[id]
	}
	return ids, hashes, ListHash(hashes)
}

// snapshot lists the watched things: the store's List (a singleton's Get),
// the ones where and the key keep, each as get answers it.
func (w *Watch) snapshot() (watchSnapshot, error) {
	n := w.r.Nouns[w.noun]
	if n == nil || n.Store == nil {
		return watchSnapshot{}, fmt.Errorf("%s has no store", w.noun)
	}
	var things []Thing
	switch s := n.Store; {
	case s.List != nil:
		list, err := s.List(w.ctx, ListQuery{Where: w.where})
		if err != nil {
			return watchSnapshot{}, err
		}
		things = list
	case s.Get != nil:
		t, err := s.Get(w.ctx, "")
		if err != nil {
			return watchSnapshot{}, err
		}
		if t != nil {
			things = []Thing{t}
		}
	}
	key := n.keyProp()
	snap := watchSnapshot{records: map[string]map[string]any{}, hashes: map[string]string{}}
	for _, t := range things {
		if !thingMatches(n, t, w.where) {
			continue
		}
		id := ""
		if key != nil {
			id = fmt.Sprint(t[key.Name])
		}
		if w.key != "" && id != w.key {
			continue
		}
		if _, twice := snap.records[id]; twice {
			continue
		}
		rec := thingRecord(n, t, nil)
		line, err := json.Marshal(rec)
		if err != nil {
			return watchSnapshot{}, fmt.Errorf("%s %s: %w", w.noun, id, err)
		}
		snap.ids = append(snap.ids, id)
		snap.records[id], snap.hashes[id] = rec, RecordHash(line)
	}
	return snap, nil
}

// start takes the first snapshot and emits what changed since (every
// thing as added for none).
func (w *Watch) start(since string) error {
	snap, err := w.snapshot()
	if err != nil {
		return err
	}
	ids, hashes, hash := snap.version()
	base := map[string]watchKnown{}
	switch {
	case since == "":
	case since == hash:
		for i, id := range ids {
			base[id] = watchKnown{hash: hashes[i]}
		}
	default:
		oldIDs, oldHashes, ok := w.r.watchHub().versions.ReadVersion(w.scope, since)
		if !ok {
			return &ArgumentError{fmt.Errorf("since %s is not a version this watch keeps; watch without since to start from every %s", since, w.r.DisplayNoun(w.noun))}
		}
		for i, id := range oldIDs {
			base[id] = watchKnown{hash: oldHashes[i]}
		}
	}
	w.known = base
	w.apply(snap, true)
	return nil
}

// refresh lists again and emits the difference; a failing list ends the
// watch with its error.
func (w *Watch) refresh() {
	snap, err := w.snapshot()
	if err != nil {
		w.fail(err)
		return
	}
	w.apply(snap, false)
}

// apply emits the difference between what the watch knows and snap, and
// keeps snap's version (always on start, so a later since finds it).
func (w *Watch) apply(snap watchSnapshot, first bool) {
	ids, hashes, hash := snap.version()
	var events []WatchEvent
	for _, id := range snap.ids {
		old, had := w.known[id]
		switch {
		case !had:
			events = append(events, WatchEvent{Type: WatchAdded, ID: id, Thing: snap.records[id]})
		case old.hash != snap.hashes[id]:
			events = append(events, WatchEvent{Type: WatchModified, ID: id, Thing: snap.records[id]})
		}
	}
	gone := make([]string, 0)
	for id := range w.known {
		if _, ok := snap.records[id]; !ok {
			gone = append(gone, id)
		}
	}
	sort.Strings(gone)
	for _, id := range gone {
		events = append(events, WatchEvent{Type: WatchDeleted, ID: id, Thing: w.known[id].record})
	}
	known := make(map[string]watchKnown, len(snap.ids))
	for _, id := range snap.ids {
		known[id] = watchKnown{hash: snap.hashes[id], record: snap.records[id]}
	}
	w.known = known
	if first || hash != w.hash {
		if err := w.r.watchHub().versions.WriteVersion(w.scope, hash, ids, hashes); err != nil {
			w.r.UI().Error(fmt.Sprintf("%s watch: keeping its version: %v", w.r.DisplayNoun(w.noun), err))
		}
	}
	w.hash = hash
	for _, e := range events {
		e.Hash = hash
		w.deliver(e)
	}
}

// deliver hands one event to the subscriber.
func (w *Watch) deliver(e WatchEvent) {
	if w.ended {
		return
	}
	if w.fn != nil {
		w.fn(e)
		return
	}
	w.queue = append(w.queue, e)
}

// emit is a store Watch's callback: it queues the event from any
// goroutine and wakes the waiting command; never blocks.
func (w *Watch) emit(e WatchEvent) {
	w.mu.Lock()
	select {
	case <-w.done:
		w.mu.Unlock()
		return
	default:
	}
	w.pushed = append(w.pushed, e)
	w.mu.Unlock()
	select {
	case w.signal <- struct{}{}:
	default:
	}
}

// watchMsg carries a store Watch's events to the loop.
type watchMsg struct {
	w      *Watch
	events []WatchEvent
}

// listen is the command that waits for the next batch a store pushed (nil
// once the watch ends).
func (w *Watch) listen() tea.Cmd {
	return func() tea.Msg {
		for {
			w.mu.Lock()
			events := w.pushed
			w.pushed = nil
			w.mu.Unlock()
			if len(events) > 0 {
				return watchMsg{w: w, events: events}
			}
			select {
			case <-w.signal:
			case <-w.done:
				return nil
			}
		}
	}
}

// deliverPushed hands a pushed batch to the subscriber, each thing read
// as get answers it and its id filled from the key prop, and waits for
// the next.
func (m watchMsg) deliver(r *Runtime) {
	w := m.w
	if w.ended {
		return
	}
	n := r.Nouns[w.noun]
	for _, e := range m.events {
		if e.Thing != nil && n != nil {
			if key := n.keyProp(); key != nil && e.ID == "" {
				e.ID = fmt.Sprint(e.Thing[key.Name])
			}
			e.Thing = thingRecord(n, e.Thing, nil)
		}
		w.deliver(e)
	}
	if !w.ended {
		r.commands = append(r.commands, w.listen())
	}
}

// fail ends the watch with err.
func (w *Watch) fail(err error) {
	if w.err == nil {
		w.err = err
	}
	w.Stop()
}

// Stop ends the watch (a store Watch's stop runs once); events after it
// are dropped.
func (w *Watch) Stop() {
	if w.ended {
		return
	}
	w.ended = true
	if w.done != nil {
		w.mu.Lock()
		close(w.done)
		w.mu.Unlock()
	}
	if w.stopStore != nil {
		w.stopStore()
	}
	if h := w.r.watches; h != nil {
		h.watches = slices.DeleteFunc(h.watches, func(o *Watch) bool { return o == w })
	}
}

// Take answers the queued events (a watch without a callback) and empties
// the queue.
func (w *Watch) Take() []WatchEvent {
	out := w.queue
	w.queue = nil
	return out
}

// Noun is the watched noun.
func (w *Watch) Noun() string { return w.noun }

// Hash is a fallback watch's list hash now (the since that resumes from
// here), "" for a store's Watch.
func (w *Watch) Hash() string { return w.hash }

// Stopped reports whether the watch ended (Stop, or a failing list).
func (w *Watch) Stopped() bool { return w.ended }

// Err is why the watch ended on its own, nil after Stop.
func (w *Watch) Err() error { return w.err }

// MarshalJSON is the watch as one value, for a caller that cannot stream:
// the events so far as an array; the watch ends.
func (w *Watch) MarshalJSON() ([]byte, error) {
	events := w.Take()
	w.Stop()
	if events == nil {
		events = []WatchEvent{}
	}
	return json.Marshal(events)
}

// memoryVersions keeps versions in memory: the newest few per scope, for
// the newest few scopes.
type memoryVersions struct {
	scopes []string
	by     map[string][]memoryVersion
}

type memoryVersion struct {
	hash        string
	ids, hashes []string
}

const (
	memoryVersionsKeep   = 4
	memoryVersionsScopes = 16
)

func newMemoryVersions() *memoryVersions { return &memoryVersions{by: map[string][]memoryVersion{}} }

func (m *memoryVersions) ReadVersion(scope, hash string) ([]string, []string, bool) {
	for _, v := range m.by[scope] {
		if v.hash == hash {
			return slices.Clone(v.ids), slices.Clone(v.hashes), true
		}
	}
	return nil, nil, false
}

func (m *memoryVersions) WriteVersion(scope, hash string, ids, hashes []string) error {
	if len(ids) != len(hashes) {
		return errors.New("a version needs one hash per id")
	}
	list := slices.DeleteFunc(m.by[scope], func(v memoryVersion) bool { return v.hash == hash })
	list = append([]memoryVersion{{hash: hash, ids: slices.Clone(ids), hashes: slices.Clone(hashes)}}, list...)
	if len(list) > memoryVersionsKeep {
		list = list[:memoryVersionsKeep]
	}
	m.by[scope] = list
	m.scopes = append([]string{scope}, slices.DeleteFunc(m.scopes, func(s string) bool { return s == scope })...)
	for len(m.scopes) > memoryVersionsScopes {
		delete(m.by, m.scopes[len(m.scopes)-1])
		m.scopes = m.scopes[:len(m.scopes)-1]
	}
	return nil
}

// watchParams are the watch verb's parameters: the key prop (one thing,
// never from the focus), where and since.
func watchParams(n *Noun) map[string]Parameter {
	params := map[string]Parameter{
		"since": {Type: "string", Description: "Start from this hash (an earlier event's hash): emit what changed since it instead of every thing as added"},
	}
	if n.Store.Kind != Singleton {
		params["where"] = Parameter{Type: "string", Description: "prop=value pairs, comma- or space-separated: watch the things whose props equal every value"}
	}
	return params
}

// runWatch starts the watch the request asks for; its answer is the
// *Watch (a stream for the CLI).
func runWatch(r *Runtime, n *Noun, c Ctx) (any, error) {
	return r.watch(c, n.Name, nil)
}
