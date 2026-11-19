package ochazuke

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The overlay (no-refusals D1, overlay-every-store OV1, OV3): what a
// store's source cannot do is done by the framework beside the thing, keyed
// by noun, key and prop, merged into get, list, the paged list (Query) and
// the change token, and marked as the user's. It fills every store that
// reads things (Get or List) and cannot write them (no Patch), a system
// fact or not, local or remote (a mounted tool's proxy, ListFacts), and
// every system fact: props the source cannot hold (every prop of a store
// with no Patch, the Overlaid ones of a fact with a Patch), a create (a
// thing of the user's own), a delete and an archive prop. A delete on a
// local store archives; on a remote one it hides the thing (the prop
// hidden: left out of every list, page and count, still answered by get,
// undoable), since the thing goes on existing in the tool's source; a thing
// of the user's own is removed. A singleton's create is its set, and its
// delete resets the user's changes (the source's values show again). A
// store whose every write is the overlay's undoes them.
//
// A remote store's calls run off the loop as the work of a job; the verb's
// body (its plan and record runs, remote.go) runs on the loop, and so does
// everything the overlay does inside it: the merge reads copies of the rows
// under the read lock, and the writes (changeOverlay) are the body's, as on
// a local store. A paged list stays paged: one source call per page, one
// row lookup per thing on it; nothing reads the source whole to lay the
// overlay over it.
//
// It leaves alone a store that writes its own props (a Patch, not a
// fact: its author decides its create, delete and archive), a store whose
// author keeps a missing write missing
// (Store.NoOverlay, deprecated: nothing is exempt), a copy (it keeps
// the user's values itself, CopyStore) and an instances store (its things
// are live instances; create does not fit them).
//
// The rows live where overlay.store says (overlay_backend.go): overlay.json
// beside settings.json (Config.Path, the default), the family's shared
// file, or an application's own place (mtm's database); a mutex keeps the
// session's rows; with no config directory (a sandbox, a test) and no
// application backend they live for the session.
// Setting facts.overlay off, or overlay.stores facts for a store that is not
// a system fact, answers a write the source cannot hold with a ChoiceAnswer
// naming the verbs that do change the thing.

const (
	// OverlayFile holds the overlay rows beside settings.json.
	OverlayFile = "overlay.json"
	// FactsOverlaySetting turns the overlay's writes on or off.
	FactsOverlaySetting = "facts.overlay"
	// OverlayStoresSetting is which stores the overlay writes for:
	// OverlayStoresAll (every store it fills) or NounKindFacts (system
	// facts only, the scope before OV1).
	OverlayStoresSetting = "overlay.stores"
	// OverlayStoresAll is overlay.stores' default: every store it fills.
	OverlayStoresAll = "all"
	// OverlayArchiveProp is the archive-role prop the overlay gives a
	// collection that has none, so archive and restore run on it.
	OverlayArchiveProp = "archived"
	// OverlayHiddenProp is the prop the overlay gives a remote collection
	// whose source cannot delete: a delete sets it, and the thing is left
	// out of every list, page and count (filter hidden=true lists them).
	OverlayHiddenProp = "hidden"
	// OverlayNoun lists the overlay's rows, so the user can see and clear
	// the values the framework keeps for them.
	OverlayNoun = "ochazuke.overlay"
)

// OverlayRow is one thing's overlay: the props the user wrote that the
// source cannot hold, what is known of each (Changes: the original as last
// seen, who, when), and whether the thing is the user's own (created
// here, unknown to the source).
type OverlayRow struct {
	Props    map[string]any         `json:"props,omitempty"`
	Changes  map[string]*PropChange `json:"changes,omitempty"`
	Created  bool                   `json:"created,omitempty"`
	Archived bool                   `json:"archived,omitempty"`
}

// overlayRows are every noun's rows, read from the backend (overlay.store,
// OverlayBackend) on first use and again when the setting or the config
// directory changes. session names the nouns whose keys hold only in this
// process (Store.SessionKeys): their rows live here and never reach the
// backend. mu guards loaded, ident, backend and rows: a read takes copies
// under it, so a merge made off the loop never races a write. backends are
// the applications' (SetOverlayBackend).
type overlayRows struct {
	mu       sync.RWMutex
	loaded   bool
	ident    string
	backend  OverlayBackend
	rows     map[string]map[string]*OverlayRow
	session  map[string]bool
	backends map[string]OverlayBackend
	// version is the backend's version the rows were read at
	// (OverlayVersioner); a read whose backend answers another reads the
	// rows again.
	version string
	// checked is the read pass the version was last read in.
	checked uint64
	// versions caches each noun's overlay version (overlayVersion); a
	// write clears it.
	versions map[string]string
}

// overlayHolder is the overlay's state, not read yet.
func (r *Runtime) overlayHolder() *overlayRows {
	c := r.Config()
	if c.overlay == nil {
		c.overlay = &overlayRows{session: map[string]bool{}}
	}
	return c.overlay
}

// overlayState is the overlay's state, its rows read from the backend when
// the backend (overlay.store, the config directory) changed or they were
// never read.
func (r *Runtime) overlayState() *overlayRows {
	o := r.overlayHolder()
	backend, ident := r.overlayBackend()
	o.mu.Lock()
	defer o.mu.Unlock()
	version := o.version
	if v, ok := backend.(OverlayVersioner); ok && (!o.loaded || o.ident != ident || o.checked != r.readPass) {
		version, _ = v.Version() // once per read pass (ReadPass), never per row
		o.checked = r.readPass
	}
	if !o.loaded || o.ident != ident || version != o.version {
		held := o.rows
		if o.ident != ident {
			held = nil // another backend: the session's rows were of the other one's things too
		}
		o.loaded, o.ident, o.backend, o.rows, o.versions, o.version = true, ident, backend, map[string]map[string]*OverlayRow{}, nil, version
		if backend != nil {
			rows, err := backend.Rows()
			if err != nil {
				r.UI().Notify("your changes (" + OverlayStoreSetting + "): " + err.Error())
			}
			if rows != nil {
				o.dropSession(rows)
				o.rows = rows
			}
		}
		for noun := range o.session {
			if rows, ok := held[noun]; ok {
				o.rows[noun] = rows // read again (ReloadOverlay): the session's rows stay
			}
		}
	}
	return o
}

// clone is a copy of the row that shares nothing with it; nil for nil.
func (row *OverlayRow) clone() *OverlayRow {
	if row == nil {
		return nil
	}
	out := *row
	out.Props = maps.Clone(row.Props)
	if row.Changes != nil {
		out.Changes = make(map[string]*PropChange, len(row.Changes))
		for name, ch := range row.Changes {
			one := *ch
			out.Changes[name] = &one
		}
	}
	return &out
}

// dropSession takes out of rows read from the backend the nouns whose rows
// are the session's (a row of an earlier run would wear another thing).
func (o *overlayRows) dropSession(rows map[string]map[string]*OverlayRow) {
	for noun := range o.session {
		delete(rows, noun)
	}
}

// overlayRow is a copy of the overlay of one thing, nil when it has none.
func (r *Runtime) overlayRow(noun, key string) *OverlayRow {
	o := r.overlayState()
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.rows[noun][key].clone()
}

// overlayNounRows are copies of every row of one noun, by key.
func (r *Runtime) overlayNounRows(noun string) map[string]*OverlayRow {
	o := r.overlayState()
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make(map[string]*OverlayRow, len(o.rows[noun]))
	for key, row := range o.rows[noun] {
		out[key] = row.clone()
	}
	return out
}

// overlayAllRows are copies of every noun's rows.
func (r *Runtime) overlayAllRows() map[string]map[string]*OverlayRow {
	o := r.overlayState()
	o.mu.RLock()
	nouns := slices.Collect(maps.Keys(o.rows))
	o.mu.RUnlock()
	out := make(map[string]map[string]*OverlayRow, len(nouns))
	for _, noun := range nouns {
		out[noun] = r.overlayNounRows(noun)
	}
	return out
}

// changeOverlay changes one thing's overlay row: change gets the row (a
// new one when there is none) and answers false to remove it. With a
// backend it changes the row as the backend holds it now (Change, under
// the backend's lock), so processes sharing it (the TUI, mcp serve, a
// one-shot CLI) keep each other's rows; the session then holds what the
// backend answered. A noun of session keys, or no backend, changes the
// session's rows only. It answers the row, nil when removed.
func (r *Runtime) changeOverlay(noun, key string, change func(*OverlayRow) bool) (*OverlayRow, error) {
	o := r.overlayState()
	o.mu.Lock()
	defer o.mu.Unlock()
	o.versions = nil
	if o.backend == nil || o.session[noun] {
		return ApplyOverlayChange(o.rows, noun, key, change).clone(), nil
	}
	var row *OverlayRow
	var fresh map[string]map[string]*OverlayRow
	var err error
	if vc, ok := o.backend.(OverlayVersionedChange); ok {
		var before, after string
		row, before, after, err = vc.ChangeVersioned(noun, key, change)
		if err == nil && before == o.version {
			o.version = after // this write alone moved it (read inside its transaction): the rows held stay right
		}
	} else {
		row, fresh, err = o.backend.Change(noun, key, change)
	}
	if err != nil {
		return nil, err
	}
	if fresh == nil {
		ApplyOverlayChange(o.rows, noun, key, func(held *OverlayRow) bool {
			if row != nil {
				*held = *row.clone()
			}
			return row != nil
		})
		return row.clone(), nil
	}
	o.dropSession(fresh)
	for session := range o.session {
		if rows, ok := o.rows[session]; ok {
			fresh[session] = rows
		}
	}
	o.rows = fresh
	return row.clone(), nil
}

// overlayVersion is a short hash of a noun's overlay rows as this process
// holds them, "" when it has none: it moves on every write that changes
// them (and on rows another process wrote, read again with it), so a
// change token joined with it moves too, without a call to the source.
func (r *Runtime) overlayVersion(noun string) string {
	o := r.overlayState()
	o.mu.Lock()
	defer o.mu.Unlock()
	if v, ok := o.versions[noun]; ok {
		return v
	}
	v := ""
	if rows := o.rows[noun]; len(rows) > 0 {
		data, _ := json.Marshal(rows) // map keys sorted: the same rows, the same text
		h := fnv.New64a()
		h.Write(data)
		v = strconv.FormatUint(h.Sum64(), 36)
	}
	if o.versions == nil {
		o.versions = map[string]string{}
	}
	o.versions[noun] = v
	return v
}

// putOverlay merges props into a thing's overlay row and writes it. A nil
// value takes the prop out of the row (the source's value shows again:
// what undo writes back for a prop the user had not changed), and a row
// left with nothing of the user's is removed. src is the source's thing as
// it is now (nil when it was not read): each prop written keeps its value
// there as the original, with who wrote it (by) and when.
func (r *Runtime) putOverlay(noun, key string, props Thing, created bool, src Thing, by Actor) error {
	at := time.Now().UTC()
	_, err := r.changeOverlay(noun, key, func(row *OverlayRow) bool {
		if row.Props == nil {
			row.Props = map[string]any{}
		}
		for name, v := range props {
			if v == nil {
				delete(row.Props, name)
			} else {
				row.Props[name] = v
			}
		}
		row.noteChanges(props, src, by, at)
		row.Created = row.Created || created
		return row.Created || row.Archived || len(row.Props) > 0
	})
	return err
}

// dropOverlay removes a thing's overlay row.
func (r *Runtime) dropOverlay(noun, key string) error {
	_, err := r.changeOverlay(noun, key, func(*OverlayRow) bool { return false })
	return err
}

// overlayStop is what keeps the overlay from writing for a store ("" when
// nothing does): facts.overlay off (default on), or overlay.stores facts
// (default all) for a store that is not a system fact. Both are read at
// each write, so a change of either holds at once.
func (r *Runtime) overlayStop(facts bool) string {
	s := r.Settings()
	switch {
	case s.Has(FactsOverlaySetting) && !s.Bool(FactsOverlaySetting):
		return FactsOverlaySetting + " is off"
	case !facts && s.Has(OverlayStoresSetting) && s.String(OverlayStoresSetting) == NounKindFacts:
		return OverlayStoresSetting + " is " + NounKindFacts
	}
	return ""
}

// overlayOff is the answer to a write the source cannot hold while a
// setting keeps it from the overlay (stop, overlayStop): the verbs of the
// noun that change its things (its application verbs), each with the
// command that runs it.
func (r *Runtime) overlayOff(noun, key, stop string, props []string) error {
	shown := r.DisplayNoun(noun)
	answer := &ChoiceAnswer{Needs: "verb", Reason: fmt.Sprintf("%s %s cannot hold %s (%s); a verb that changes it:", shown, key, strings.Join(props, ", "), stop)}
	for _, op := range r.Operations.List() {
		if op.Noun() != noun || op.Standard != "" || op.Unavailable != "" || op.Kind != KindApplication {
			continue
		}
		answer.Choices = append(answer.Choices, ChoiceRow{ID: op.Key.Short(), Label: op.Description, Command: shown + " " + op.Name() + " " + key})
	}
	return answer
}

// rowFits reports whether the standard row of verb is for a store of
// that kind (its Kinds, the table's data).
func rowFits(verb string, s Store) bool {
	row := standardRow(verb)
	return row != nil && slices.Contains(row.Kinds, s.Kind)
}

// overlayOf is what the overlay fills for one noun's store.
type overlayOf struct {
	r              *Runtime
	n              *Noun
	facts          bool
	keyName        string
	srcGet         func(Ctx, string) (Thing, error)
	srcList        func(Ctx, ListQuery) ([]Thing, error)
	srcQuery       func(Ctx, ListQuery) (ListPage, error)
	srcPatch       func(Ctx, string, Thing) (Thing, error)
	create, delete bool
	// hiddenName is the prop a delete sets on a remote store's thing
	// (hidden, or hidden-by-you), "" where a delete archives; added are
	// the props the overlay gave the noun (the source knows nothing of
	// them, so a paged list keeps their conditions from it).
	hiddenName string
	added      []string
	// remote is a store whose calls leave the process: a write takes the
	// originals of the props it writes from the source's thing the same
	// verb's get answered before it (tape.seen), never a call more.
	remote bool
}

// withOverlay fills what a store's source lacks with the overlay (see the
// top of this file): a List for a singleton (its one thing), a Patch that
// keeps the props the source cannot hold, a Create and a Delete where the
// store has none (a singleton's create is its set, its delete a reset), an
// archive prop where archive fits and the noun has no archive role, a hidden
// prop where a remote store's delete hides, and reads (Get, List, Query,
// ChangeToken) that merge the overlay rows in (fill). A store with
// NoOverlay, a copy, one that reads no thing and an instances store
// (create does not fit its live instances) are left as they are, and so
// are one that writes its own props (not a fact) and one that writes
// everything itself. A remote store is filled as a local one is.
func (r *Runtime) withOverlay(n *Noun, s Store) Store {
	if s.NoOverlay || s.copy != nil || s.Get == nil && s.List == nil {
		return s
	}
	hadList := s.List != nil
	s = withSingletonList(n, s)
	if !hadList && s.List != nil {
		delete(s.Missing, "list") // a mounted singleton lists its one thing
	}
	if !rowFits("create", s) {
		return s // an instances store: its things are live instances
	}
	if s.Patch != nil && !s.Facts {
		// A store that writes its own props keeps its shape: its author
		// decides its create, delete and archive (OV1 fills the stores
		// that cannot write their things, and system facts).
		return s
	}
	if s.SessionKeys {
		r.overlayHolder().session[n.Name] = true
	}
	o := &overlayOf{r: r, n: n, facts: s.Facts, srcGet: s.Get, srcList: s.List, srcQuery: s.Query, srcPatch: s.Patch,
		create: s.Create == nil, delete: rowFits("delete", s) && s.Delete == nil, remote: s.Remote}
	archiveName := overlayArchiveName(n)
	archive := rowFits("archive", s) && n.roleProp(RoleArchive) == nil && n.prop(archiveName) == nil
	overlaid := slices.ContainsFunc(n.Props, func(p PropSpec) bool { return p.Overlaid })
	if s.Patch != nil && !o.create && !o.delete && !archive && !overlaid {
		return s // it writes everything itself
	}
	if o.delete && s.Remote && n.keyProp() != nil {
		o.hiddenName = overlayHiddenName(n) // the tool's source keeps the thing: a delete hides it
	}
	n.overlay = o
	o.markProps(archive, archiveName)
	if archive {
		s.Overlaid = append(s.Overlaid, "archive")
	}
	return o.fill(s)
}

// fill is s with the overlay's reads and writes in place of what it
// lacks. A store whose every write is then the overlay's (it had no
// Patch, Create or Delete) undoes (Store.Undo); one with writes of its own
// keeps its author's Undo, which covers those writes too. A create it
// fills keeps the key it is given (Store.AcceptsKey). A paged store's
// Query merges each page (query), and its ChangeToken carries the
// overlay's version (token). Store.Overlaid names the parts it supplied
// where the source had none, and Store.Missing (a mounted tool's reasons
// for the calls it lacks) no longer names them.
func (o *overlayOf) fill(s Store) Store {
	own := s.Patch != nil || s.Create != nil || s.Delete != nil
	if s.Get != nil {
		s.Get = o.get
	}
	if s.List != nil {
		s.List = o.list
	}
	if s.Query != nil {
		s.Query = o.query
	}
	if src := s.ChangeToken; src != nil {
		s.ChangeToken = func(c Ctx) (string, error) { return o.token(c, src) }
	}
	if s.Patch == nil {
		s.Overlaid = append(s.Overlaid, "patch")
		delete(s.Missing, "set")
	}
	s.Patch, s.Undo = o.patch, s.Undo || !own
	if o.create {
		s.AcceptsKey, s.Create = true, o.createThing
		s.Overlaid = append(s.Overlaid, "create")
		delete(s.Missing, "create")
	}
	if o.delete {
		s.Delete = o.deleteThing
		s.DeleteKeeps = o.deleteKeeps
		s.Overlaid = append(s.Overlaid, "delete")
		delete(s.Missing, "delete")
	}
	return s
}

// markProps gives the noun the archive prop the overlay holds (when it
// has no archive role), makes the key the prop create writes (when the
// overlay creates), gives the overlay a facts store's read-only props, and
// lets the user's value of a computed prop the overlay holds show
// (yoursOver). A derived prop that holds no value of its own
// (derivedOnly) stays read-only: the derivations wrap the filled store and
// strip it from every write; one that keeps its own stored value (a first
// derivation's, ownsValue) is a prop the overlay can hold like any other.
func (o *overlayOf) markProps(archive bool, archiveName string) {
	n := o.n
	o.r.InvalidateBindings() // the props a standard row's availability reads
	if archive {
		n.Props = append(n.Props, PropSpec{Name: archiveName, Role: RoleArchive, Overlaid: true,
			Parameter: Parameter{Type: SettingBoolean, Description: "Archived by you: the thing is hidden from lists (the framework keeps the flag beside it)"}})
		o.added = append(o.added, archiveName)
	}
	if o.hiddenName != "" {
		n.Props = append(n.Props, PropSpec{Name: o.hiddenName, Overlaid: true,
			Parameter: Parameter{Type: SettingBoolean, Description: "Hidden by you (a delete here): the thing still exists in its source and is left out of every list and count; a list with the condition " + o.hiddenName + "=true shows them, and undo or set " + o.hiddenName + "=false shows one again"}})
		o.added = append(o.added, o.hiddenName)
	}
	for i := range n.Props {
		p := &n.Props[i]
		switch {
		case p.derivedOnly():
			continue // derived with no value of its own: the framework computes it, nothing holds it
		case p.Key:
			o.keyName = p.Name
			if o.create {
				p.ReadOnly, p.CreateOnly = false, true
			}
		case o.facts && p.ReadOnly:
			p.ReadOnly, p.Overlaid = false, true
		}
		if p.Overlaid && p.Read != nil {
			o.yoursOver(p)
		}
	}
}

// yoursOver makes a computed prop the overlay holds (PropSpec.Read) read
// the user's value where the thing's row has one, else compute it as
// before: a value written over what the system reports shows as written
// (the system itself goes on using its own).
func (o *overlayOf) yoursOver(p *PropSpec) {
	read, name := p.Read, p.Name
	p.Read = func(c Ctx, t Thing) (any, error) {
		if row := o.r.overlayRow(o.n.Name, o.keyOf(t)); row != nil {
			if v, ok := row.Props[name]; ok {
				return v, nil
			}
		}
		return read(c, t)
	}
}

// keyOf is a thing's key: its key prop's value, "" for a singleton's one
// thing (it has no key prop).
func (o *overlayOf) keyOf(t Thing) string {
	if o.keyName == "" || t == nil {
		return ""
	}
	return fmt.Sprint(t[o.keyName])
}

// overlayArchiveName is the name of the archive prop the overlay gives a
// noun: archived, unless the noun's own prop holds the word.
func overlayArchiveName(n *Noun) string {
	if n.prop(OverlayArchiveProp) != nil {
		return "archived-by-you"
	}
	return OverlayArchiveProp
}

// overlayHiddenName is the name of the hidden prop the overlay gives a
// noun: hidden, unless the noun's own prop holds the word.
func overlayHiddenName(n *Noun) string {
	if n.prop(OverlayHiddenProp) != nil {
		return "hidden-by-you"
	}
	return OverlayHiddenProp
}

// hides reports whether a thing is hidden by the user (its hidden prop
// true) and the request q does not ask for hidden things (a condition on
// the prop).
func (o *overlayOf) hides(t Thing, q ListQuery) bool {
	if o.hiddenName == "" || t[o.hiddenName] != true {
		return false
	}
	return !slices.ContainsFunc(q.Filters, func(f PropFilter) bool { return f.Prop == o.hiddenName })
}

// archiveProp is the name of the noun's archive prop: its own (role
// archive), else the one the overlay gave it.
func (o *overlayOf) archiveProp() string {
	if p := o.n.roleProp(RoleArchive); p != nil {
		return p.Name
	}
	return OverlayArchiveProp
}

// withSingletonList gives a store that holds one thing (a singleton: no
// key prop, where list fits) a list of that thing.
func withSingletonList(n *Noun, s Store) Store {
	if rowFits("list", s) && n.keyProp() == nil && s.List == nil && s.Get != nil {
		s.List = singletonList(s.Get)
	}
	return s
}

// singletonList lists a singleton as its one thing.
func singletonList(get func(Ctx, string) (Thing, error)) func(Ctx, ListQuery) ([]Thing, error) {
	return func(c Ctx, _ ListQuery) ([]Thing, error) {
		t, err := get(c, "")
		if err != nil {
			return nil, err
		}
		return []Thing{t}, nil
	}
}

// merge is a thing with its overlay row's props over the source's.
func (o *overlayOf) merge(key string, t Thing) Thing {
	return o.r.mergeOver(o.r.overlayRow(o.n.Name, key), t)
}

func (o *overlayOf) get(c Ctx, key string) (Thing, error) {
	t, err := o.srcGet(c, key)
	if err == nil && o.remote && c.tape != nil {
		c.tape.keepSeen(o.n.Name, key, t)
	}
	if err != nil {
		if row := o.r.overlayRow(o.n.Name, key); row != nil && row.Created {
			return createdThing(o.keyName, key, row), nil
		}
		return nil, err
	}
	return o.merge(key, t), nil
}

func (o *overlayOf) list(c Ctx, q ListQuery) ([]Thing, error) {
	things, err := o.srcList(c, q)
	if err != nil {
		return nil, err
	}
	rows := o.r.overlayNounRows(o.n.Name)
	seen := map[string]bool{}
	out := make([]Thing, 0, len(things))
	for _, t := range things {
		key := o.keyOf(t)
		seen[key] = true
		if t = o.r.mergeOver(rows[key], t); !o.hides(t, q) {
			out = append(out, t)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(rows)) {
		if rows[key].Created && !seen[key] {
			out = append(out, createdThing(o.keyName, key, rows[key]))
		}
	}
	return out, nil
}

// held reports whether the overlay holds a prop of the thing whose row is
// row: every prop of a store with no Patch and of a thing of the user's
// own (the source does not have it), else the Overlaid ones.
func (o *overlayOf) held(row *OverlayRow, name string) bool {
	p := o.n.prop(name)
	return o.srcPatch == nil || row != nil && row.Created || p != nil && p.Overlaid
}

func (o *overlayOf) patch(c Ctx, key string, t Thing) (Thing, error) {
	row := o.r.overlayRow(o.n.Name, key)
	src, mine := Thing{}, Thing{}
	for name, v := range t {
		if o.held(row, name) {
			mine[name] = v
		} else {
			src[name] = v
		}
	}
	if stop := o.r.overlayStop(o.facts); len(mine) > 0 && stop != "" {
		return nil, o.r.overlayOff(o.n.Name, key, stop, slices.Sorted(maps.Keys(mine)))
	}
	var seen Thing // the source's thing before the write: the originals of the props written
	switch {
	case row != nil && row.Created:
	case o.remote:
		seen = c.tape.takeSeen(o.n.Name, key) // this verb read it already: no call more
	case o.srcGet != nil:
		if got, err := o.srcGet(c, key); err == nil {
			seen = got
		}
	}
	var after Thing
	if len(src) > 0 {
		var err error
		if after, err = o.srcPatch(c, key, src); err != nil {
			return nil, err
		}
	}
	if len(mine) == 0 {
		return o.merge(key, after), nil // the source wrote it all: its answer
	}
	if err := o.r.putOverlay(o.n.Name, key, mine, false, seen, c.Actor()); err != nil {
		return nil, err
	}
	if o.n.Store != nil && o.n.Store.Get != nil {
		if got, err := o.n.Store.Get(c, key); err == nil {
			return got, nil
		}
	}
	return o.merge(key, after), nil
}

// createThing records a thing of the user's own under the key given (else
// the one the overlay assigns, yoursKey). A singleton's create is its set:
// the props the request names are written as set writes them. It makes no
// call to the source, so a remote verb's runs (replayed on the loop) write
// it once, in the run that finishes.
func (o *overlayOf) createThing(c Ctx, t Thing) (Thing, error) {
	if o.keyName == "" {
		return o.patch(c, "", givenProps(o.n, c.Args, t))
	}
	if stop := o.r.overlayStop(o.facts); stop != "" {
		return nil, o.r.overlayOff(o.n.Name, "", stop, slices.Sorted(maps.Keys(t)))
	}
	key := fmt.Sprint(t[o.keyName])
	if t[o.keyName] == nil || key == "" {
		key = o.yoursKey(time.Now())
	}
	props := maps.Clone(t)
	delete(props, o.keyName)
	if err := o.r.putOverlay(o.n.Name, key, props, true, nil, c.Actor()); err != nil {
		return nil, err
	}
	return createdThing(o.keyName, key, o.r.overlayRow(o.n.Name, key)), nil
}

// yoursKey is the key the overlay gives a thing of the user's own made
// with none: yours-<UTC time to the millisecond>, with -2, -3 ... after it
// while the overlay already holds that key. The yours- word keeps it apart
// from the keys a source makes (a tool's row ids, ListFacts' noun/ ids).
func (o *overlayOf) yoursKey(at time.Time) string {
	base := "yours-" + at.UTC().Format("20060102-150405.000")
	rows := o.r.overlayNounRows(o.n.Name)
	key := base
	for i := 2; rows[key] != nil; i++ {
		key = base + "-" + strconv.Itoa(i)
	}
	return key
}

// givenProps are the props of t the request a names (createWrites): the
// defaults create fills in for the others are left out, so a singleton's
// create changes what set would.
func givenProps(n *Noun, a Arguments, t Thing) Thing {
	names, err := createWrites(n, a)
	if err != nil {
		return t
	}
	out := Thing{}
	for _, name := range names {
		if v, ok := t[name]; ok {
			out[name] = v
		}
	}
	return out
}

// deleteThing removes a thing of the user's own, and archives any other
// thing through set (the source keeps it; nothing is destroyed). A
// singleton's delete resets it: its row of the user's changes is dropped,
// so the source's values show again; with no changes it changes nothing.
func (o *overlayOf) deleteThing(c Ctx, key string) error {
	row := o.r.overlayRow(o.n.Name, key)
	if o.keyName != "" && (row == nil || !row.Created) {
		prop, hid := o.archiveProp(), o.hiddenName != ""
		if hid {
			prop = o.hiddenName
		}
		if _, err := o.r.setVia(o.n, c, prop, true); err != nil {
			return err
		}
		return &archivedInstead{noun: o.r.DisplayNoun(o.n.Name), key: key, how: prop, hid: hid}
	}
	if row == nil {
		return NothingErr(CauseUnchanged, o.r.DisplayNoun(o.n.Name)+" has no changes of yours to reset")
	}
	if stop := o.r.overlayStop(o.facts); stop != "" {
		return o.r.overlayOff(o.n.Name, key, stop, slices.Sorted(maps.Keys(row.Props)))
	}
	return o.r.dropOverlay(o.n.Name, key)
}

// deleteKeeps is the prompt for what deleteThing will do for key, decided
// by the same test: hide, archive, or "" for a thing of the user's own
// (deleted).
func (o *overlayOf) deleteKeeps(_ Ctx, key string) string {
	row := o.r.overlayRow(o.n.Name, key)
	if o.keyName == "" || row != nil && row.Created {
		return ""
	}
	if o.hiddenName != "" {
		return DefaultHideConfirm
	}
	return DefaultKeepArchiveConfirm
}

// archivedInstead is what the overlay's Delete answers when it archived or
// hid the thing (its source cannot delete it): the delete ran, and the
// thing is still there, so runDelete leaves its relations as they are. how
// is the prop the delete set (the archive prop, or the hidden prop of a
// remote source's thing: hid).
type archivedInstead struct {
	noun, key, how string
	hid            bool
}

func (a *archivedInstead) Error() string {
	if a.hid {
		return "hid " + a.noun + " " + a.key + ": it still exists in its source (the tool that lists " + a.noun + "); it is left out of every list here, and undo or set " + a.how + "=false shows it again"
	}
	return "archived " + a.noun + " " + a.key + ": its source keeps it (restore shows it again)"
}

// createdThing is a thing of the user's own (created through the overlay):
// its key and the props it was given.
func createdThing(keyName, key string, row *OverlayRow) Thing {
	t := Thing{}
	if row != nil {
		maps.Copy(t, row.Props)
	}
	if keyName != "" {
		t[keyName] = key
	}
	return t
}

// registerOverlayNoun declares ochazuke.overlay: one thing per overlay row
// (key NOUN/KEY), so the user can list, read, change and clear the values
// the framework keeps for them. delete removes the row (the values have no
// other copy: it asks first, as every delete does).
func registerOverlayNoun(r *Runtime) {
	rowThing := func(noun, key string, row *OverlayRow) Thing {
		t := Thing{"id": noun + "/" + key, "noun": noun, "thing": key, "props": maps.Clone(row.Props), "created": row.Created, "archived": row.Archived}
		originals := map[string]any{}
		for name, ch := range row.Changes {
			if ch.Seen {
				originals[name] = ch.Original
			}
		}
		if len(originals) > 0 {
			t["originals"] = originals
		}
		return t
	}
	// A noun name has no slash; a thing's key may (a path, branch/x).
	split := func(id string) (string, string) {
		noun, key, _ := strings.Cut(id, "/")
		return noun, key
	}
	r.Define(OverlayNoun).Props(
		PropSpec{Name: "id", Key: true, Parameter: Parameter{Description: "NOUN/KEY: the thing the row is beside"}},
		PropSpec{Name: "noun", ReadOnly: true, Parameter: Parameter{Description: "The thing's noun"}},
		PropSpec{Name: "thing", Role: RoleLabel, ReadOnly: true, Parameter: Parameter{Description: "The thing's key"}},
		PropSpec{Name: "props", Parameter: Parameter{Type: PropObject, Description: "The props the framework keeps for you"}},
		PropSpec{Name: "originals", ReadOnly: true, Parameter: Parameter{Type: PropObject, Description: "The source's value of each prop when you changed it (the original as last seen)"}},
		PropSpec{Name: "created", ReadOnly: true, Parameter: Parameter{Type: SettingBoolean, Description: "The thing is your own (its source does not have it)"}},
		PropSpec{Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean, Description: "Hidden from the list (restore shows it again)"}},
	).Store(Store{
		List: func(Ctx, ListQuery) ([]Thing, error) {
			var out []Thing
			rows := r.overlayAllRows()
			for _, noun := range slices.Sorted(maps.Keys(rows)) {
				for _, key := range slices.Sorted(maps.Keys(rows[noun])) {
					out = append(out, rowThing(noun, key, rows[noun][key]))
				}
			}
			return out, nil
		},
		Get: func(c Ctx, id string) (Thing, error) {
			noun, key := split(id)
			row := r.overlayRow(noun, key)
			if row == nil {
				return nil, r.NotFound(OverlayNoun, id, nil)
			}
			return rowThing(noun, key, row), nil
		},
		Patch: func(c Ctx, id string, t Thing) (Thing, error) {
			noun, key := split(id)
			if r.overlayRow(noun, key) == nil {
				return nil, r.NotFound(OverlayNoun, id, nil)
			}
			row, err := r.changeOverlay(noun, key, func(row *OverlayRow) bool {
				if props, ok := t["props"].(map[string]any); ok {
					row.Props = maps.Clone(props)
				}
				if archived, ok := t["archived"].(bool); ok {
					row.Archived = archived
				}
				return true
			})
			if err != nil {
				return nil, err
			}
			return rowThing(noun, key, row), nil
		},
		Create: func(c Ctx, t Thing) (Thing, error) {
			noun, key := split(fmt.Sprint(t["id"]))
			props, _ := t["props"].(map[string]any)
			if err := r.putOverlay(noun, key, props, false, nil, c.Actor()); err != nil {
				return nil, err
			}
			return rowThing(noun, key, r.overlayRow(noun, key)), nil
		},
		AcceptsKey: true,
		Delete: func(c Ctx, id string) error {
			noun, key := split(id)
			return r.dropOverlay(noun, key)
		},
	})
}
