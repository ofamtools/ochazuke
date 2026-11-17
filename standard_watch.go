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
// It is told after every top-level request of the change kind, or that
// ran one (a palette line's set), once it has run, after the completion of a job of a change verb, or of
// a verb of any kind that declares the stores it writes (a fetch that
// loads a store's source), and when the
// application calls Runtime.Changed (its store changed some other way).
// A verb that declares the stores it writes (Operation.Writes) tells only
// their watches; one that declares none (terminal.send) tells none.
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
	// round counts refreshWatches passes: a reader fed by several watches
	// (an index tree's, one per noun) reads once per pass.
	round uint64
	// from are the watch and lists of each noun a From list shows (list_from.go).
	from map[string]*fromHub
	// changed is set when a request of the change kind ran inside the
	// top-level request still running (a palette line's verb); anyStore
	// when one of them may change any store (Operation.Writes nil), else
	// writes are the nouns they declared (none: nothing re-reads).
	changed  bool
	anyStore bool
	writes   []string
	// stale are the windowed lists whose view changed during the request
	// running (list_window.go): each reads its page once it has run.
	stale []string
	// hidden are the lists marked stale while nothing showed them
	// (list_hidden.go): each reads its store once something does.
	hidden []string
	// frames counts the frames a Model rendered: 0 while nothing draws
	// (the CLI, a test runtime), when stale lists read at once.
	frames uint64
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
	// batch, when set, runs after each batch of events reached fn (a
	// fallback watch's difference, a store's pushed batch).
	batch func()
	// idle, when set, reports that nothing needs the watch's snapshot now
	// (every list a From hub feeds is hidden and marked stale), so a
	// refresh takes none.
	idle func() bool
	// token is the store's ChangeToken taken just before the last snapshot;
	// hasToken says it is set.
	token    string
	hasToken bool
	// notify makes a fallback watch read nothing itself: each refresh
	// tells fn the noun may have changed, as for a remote store. A
	// windowed list's watch (list_window.go) is one: it reads its own page.
	notify bool
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
	return r.watch(Ctx{actor: r.Actor(), Runtime: r, Context: r.Context(), Args: args}, noun, fn)
}

func (r *Runtime) watch(c Ctx, noun string, fn func(WatchEvent)) (*Watch, error) {
	return r.watchAs(c, noun, fn, false)
}

// watchAs is watch; notify makes a fallback watch read nothing itself
// (Watch.notify).
func (r *Runtime) watchAs(c Ctx, noun string, fn func(WatchEvent), notify bool) (*Watch, error) {
	n := r.Nouns[r.CanonicalNoun(noun)]
	if n == nil {
		return nil, fmt.Errorf("%s has no store", noun)
	}
	keyed := false
	if key := n.keyProp(); key != nil {
		keyed = !emptyProp(c.Args[key.Name])
	}
	if n.Store == nil {
		if _, ok := r.Operation(n.Name + ".get"); !ok {
			return nil, fmt.Errorf("%s has no store or get operation", noun)
		}
	} else if !watchFits(*n.Store) && !(keyed && n.Store.Get != nil) {
		return nil, fmt.Errorf("%s cannot be watched: its store has no Watch, List or (for a singleton) Get", r.DisplayNoun(n.Name))
	}
	where, err := parseWhere(n, StringArg(c.Args, "where", ""))
	if err != nil {
		return nil, err
	}
	w := &Watch{r: r, noun: n.Name, where: where, ctx: c, fn: fn, notify: notify}
	if key := n.keyProp(); key != nil {
		if v, ok := c.Args[key.Name]; ok && !emptyProp(v) {
			w.key = fmt.Sprint(v)
		}
	}
	since := StringArg(c.Args, "since", "")
	if s := n.Store; s != nil && s.Watch != nil {
		w.push, w.signal, w.done = true, make(chan struct{}, 1), make(chan struct{})
		stop, err := s.Watch(c, WatchQuery{Key: w.key, Where: where, Since: since}, w.emit)
		if err != nil {
			return nil, err
		}
		w.stopStore = r.life.keepStop(w, stop) // Shutdown runs it too, once
		r.commands = append(r.commands, w.listen())
	} else if n.Store != nil {
		w.scope = watchScope(n.Name, w.key, where)
		w.known = map[string]watchKnown{}
		// A remote store is not listed on the loop: its watcher is told to
		// reload when something changed (refresh), and starts knowing nothing.
		if !n.Store.Remote && !notify {
			if err := w.start(since); err != nil {
				return nil, err
			}
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
	names := make([]string, len(nouns))
	for i, n := range nouns {
		names[i] = r.CanonicalNoun(n)
	}
	// A noun whose derived props read a changed noun changed too.
	if len(names) > 0 {
		names = r.withDependents(names)
	}
	// The open surfaces count the change too (surface.left, events.go).
	r.countChange(names)
	r.renderCache.changes++
	if r.watches == nil {
		return
	}
	r.refreshWatches(names)
}

// refreshWatches lists every fallback watch of nouns (all when none) again,
// except those whose store's ChangeToken says nothing changed. Each watch
// is snapshotted at most once per call; a request makes one call, however
// many change-kind verbs it ran. A custom verb may change any noun's store
// (cascades, side tables), so a request does not narrow nouns unless its
// verb declares what it writes (Operation.Writes): otherwise the token is
// what keeps an untouched store cheap.
//
// It reports, by noun, whether a watch it refreshed may answer
// differently (movedBy; a noun no watch was refreshed for is absent).
func (r *Runtime) refreshWatches(nouns []string) (movedBy map[string]bool) {
	h := r.watches
	if h == nil || h.refreshing || len(h.watches) == 0 || r.ShuttingDown() {
		return nil
	}
	h.refreshing = true
	h.round++
	r.readPass++
	defer func() { h.refreshing = false }()
	movedBy = map[string]bool{}
	for _, w := range slices.Clone(h.watches) {
		if !w.push && !w.ended && (len(nouns) == 0 || slices.Contains(nouns, w.noun)) {
			m := w.refresh()
			movedBy[w.noun] = movedBy[w.noun] || m
		}
	}
	return movedBy
}

// afterRequest refreshes the watches once a top-level request has run
// that is of the change kind or ran one (the palette's run of a set).
func (r *Runtime) watchAfterRequest(q Request, depth int, nothing bool) {
	change := !nothing && r.historyKind(q) == HistoryChange
	writes, anyStore := r.requestWrites(q)
	if change && !anyStore && len(writes) == 0 {
		change = false // it changes no store (Operation.Writes): nothing re-reads
	}
	if depth == 0 && change {
		defer recountOpenNounsPage(r)
	}
	h := r.watches
	if h == nil {
		return
	}
	if depth == 0 {
		r.readStaleWindows()
		r.syncIndexForm()
		defer r.refillShown() // a page or modal the request opened shows its stale lists
	}
	if change {
		h.changed = true
		h.anyStore = h.anyStore || anyStore
		h.writes = append(h.writes, writes...)
	}
	if depth == 0 && h.changed {
		anyStore, writes := h.anyStore, h.writes
		h.changed, h.anyStore, h.writes = false, false, nil
		r.refreshWritten(anyStore, writes)
		r.syncIndexForm() // the lists read again: a selection that moved gets its form
	}
}

// requestWrites is what a request's verb declares it writes
// (Operation.Writes): the nouns, or anyStore when it may change any store
// (nothing declared, a prop request, an unknown verb).
func (r *Runtime) requestWrites(q Request) (writes []string, anyStore bool) {
	if q.IsProp() {
		return nil, true
	}
	op, ok := r.Operations.Find(Key(q.Key()))
	if !ok || op.Writes == nil {
		return nil, true
	}
	return op.Writes, false
}

// refreshWritten tells the watches of what changed: every one when a verb
// may have changed any store, else those of the nouns written and the
// nouns derived from them, none when nothing was written.
//
// It reports whether what was written may answer differently now: for a
// written noun neither its store's ChangeToken (the same as at the last
// load) nor a local watch's snapshot says it is the same.
func (r *Runtime) refreshWritten(anyStore bool, writes []string) (moved bool) {
	if anyStore {
		r.refreshWatches(nil)
		return true
	}
	if len(writes) == 0 {
		return false
	}
	names := make([]string, len(writes))
	for i, n := range writes {
		names[i] = r.CanonicalNoun(n)
	}
	movedBy := r.refreshWatches(r.withDependents(names))
	for _, n := range names {
		if r.storeTokenSame(n) {
			continue // its ChangeToken is the one the last load saw
		}
		if m, watched := movedBy[n]; watched && !m {
			continue // a local snapshot says the listing is the same
		}
		moved = true
	}
	return moved
}

// storeTokenSame reports whether noun's store answers the ChangeToken it
// answered when last asked here, and keeps the token for the next ask; no
// token (or an error) is never the same.
func (r *Runtime) storeTokenSame(noun string) bool {
	n := r.Nouns[noun]
	if n == nil || n.Store == nil || n.Store.ChangeToken == nil {
		return false
	}
	token, err := n.Store.ChangeToken(Ctx{Runtime: r, actor: r.Actor()})
	if err != nil || token == "" {
		return false
	}
	c := &r.renderCache
	same := c.loadTokens[noun] == token
	if c.loadTokens == nil {
		c.loadTokens = map[string]string{}
	}
	c.loadTokens[noun] = token
	return same
}

// watchAfterJob refreshes the watches once a job of a change verb is done,
// and once a job of any other verb that declares the stores it writes
// (Operation.Writes) is done: a fetch that loads what a store answers (a
// daemon's listing read into the app's cache) tells that store's readers,
// as a change verb does. A verb of another kind that declares nothing
// tells nothing.
//
// The change counter the palette's memo compares (renderCache.changes)
// moves after a change verb that may write a store, and after such a fetch
// only when what it wrote may answer differently: a watch of a written
// noun saw its listing change, or a written noun has no local watch that
// could tell (then it moves, as nothing says the store is the same). A
// periodic refresh whose listing is unchanged keeps the memo.
func (r *Runtime) watchAfterJob(verb string) {
	op, ok := r.Operations.Find(Key(verb))
	switch {
	case !ok:
	case op.HistoryKindOf() == HistoryChange:
		if op.Writes != nil && len(op.Writes) == 0 {
			return // it changes no store (Operation.Writes)
		}
		r.renderCache.changes++ // what a guard or a store answers may differ now
		if r.watches == nil {
			return
		}
		r.refreshWritten(op.Writes == nil, op.Writes)
		recountOpenNounsPage(r)
	case len(op.Writes) > 0:
		if r.refreshWritten(false, op.Writes) {
			r.renderCache.changes++
		}
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

// changeToken asks the store for its change token; ok is false when the
// store has none, it failed, or it answered "" (unknown).
func (w *Watch) changeToken() (token string, ok bool) {
	n := w.r.Nouns[w.noun]
	if n == nil || n.Store == nil || n.Store.ChangeToken == nil {
		return "", false
	}
	t, err := n.Store.ChangeToken(w.ctx)
	if err != nil || t == "" {
		return "", false
	}
	return t, true
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
	case s.List != nil && (w.key == "" || s.Get == nil):
		list, err := s.List(w.ctx, ListQuery{Filters: whereFilters(w.where)})
		if err != nil {
			return watchSnapshot{}, err
		}
		things = list
	case s.Get != nil: // one thing's watch reads that thing, never the whole store
		t, err := s.Get(w.ctx, w.key)
		switch {
		case err != nil && s.List != nil:
			// The store lists too: a thing it cannot get is gone, as a
			// list without it would say.
		case err != nil:
			return watchSnapshot{}, err
		case t != nil:
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
	token, hasToken := w.changeToken() // before the list: a write during it moves the token on
	snap, err := w.snapshot()
	if err != nil {
		return err
	}
	w.token, w.hasToken = token, hasToken
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
// refresh reports whether the store may answer differently than at the
// last snapshot: false only when its token or a new snapshot says it is
// the same.
func (w *Watch) refresh() (moved bool) {
	if st := w.r.Nouns[w.noun].Store; st == nil || st.Remote || w.notify {
		// A proxy has no local snapshot. Existing change notifications
		// invalidate its readers, which reload through their own get proxy.
		w.deliver(WatchEvent{Type: WatchModified, ID: w.key})
		if w.batch != nil && !w.ended {
			w.batch()
		}
		return true
	}
	token, hasToken := w.changeToken()
	if hasToken && w.hasToken && token == w.token {
		return false // the store says nothing changed since the last snapshot
	}
	if w.idle != nil && w.idle() {
		return true // only hidden lists read it: they are marked stale instead
	}
	snap, err := w.snapshot()
	if err != nil {
		w.fail(err)
		return true
	}
	w.token, w.hasToken = token, hasToken
	return w.apply(snap, false)
}

// apply emits the difference between what the watch knows and snap, and
// keeps snap's version (always on start, so a later since finds it).
func (w *Watch) apply(snap watchSnapshot, first bool) (changed bool) {
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
	if len(events) > 0 && !first && w.batch != nil {
		w.batch()
	}
	return first || len(events) > 0
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
			case <-w.r.Lifetime().Done():
				return nil // Shutdown: the store's stop has run
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
	if w.batch != nil && !w.ended {
		w.batch()
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
		params["where"] = Parameter{Type: "string", Accept: AcceptKeep, Pairs: true, Description: "prop=value pairs, comma- or space-separated: watch the things whose props equal every value"}
	}
	return params
}

// runWatch starts the watch the request asks for; its answer is the
// *Watch (a stream for the CLI).
func runWatch(r *Runtime, n *Noun, c Ctx) (any, error) {
	return r.watch(c, n.Name, nil)
}
