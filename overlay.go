package ochazuke

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// The overlay (no-refusals D1, overlay-every-store OV1): what a local
// store's source cannot do is done by the framework beside the thing, keyed
// by noun, key and prop, merged into get and list and marked as the
// user's. It fills every store that reads things (Get or List) and lacks a
// write, a system fact or not: props the source cannot hold (every prop of
// a store with no Patch, the Overlaid ones of one with a Patch), a create
// (a thing of the user's own), a delete (it archives; nothing is
// destroyed; a thing of the user's own is removed) and an archive prop. A
// singleton's create is its set, and its delete resets the user's changes
// (the source's values show again). A store whose every write is the
// overlay's undoes them.
//
// It leaves alone a store whose author keeps a missing write missing
// (Store.NoOverlay, the catalog's noOverlay), a remote store (a mounted
// tool's proxy, ListFacts: their calls run off the loop), a copy (it keeps
// the user's values itself, CopyStore) and an instances store (its things
// are live instances; create does not fit them).
//
// The rows live in overlay.json beside settings.json (Config.Path), written
// atomically under a file lock, and a mutex keeps the session's rows; with
// no config directory (a sandbox, a test) they live for the session.
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
	// OverlayNoun lists the overlay's rows, so the user can see and clear
	// the values the framework keeps for them.
	OverlayNoun = "ochazuke.overlay"
)

// OverlayRow is one thing's overlay: the props the user wrote that the
// source cannot hold, and whether the thing is the user's own (created
// here, unknown to the source).
type OverlayRow struct {
	Props    map[string]any `json:"props,omitempty"`
	Created  bool           `json:"created,omitempty"`
	Archived bool           `json:"archived,omitempty"`
}

// overlayRows are every noun's rows, read from the file on first use.
// session names the nouns whose keys hold only in this process
// (Store.SessionKeys): their rows live here and never reach the file. mu
// guards loaded, path and rows: a read takes copies under it, so a merge
// made off the loop never races a write.
type overlayRows struct {
	mu      sync.RWMutex
	loaded  bool
	path    string
	rows    map[string]map[string]*OverlayRow
	session map[string]bool
}

// overlayHolder is the overlay's state, not read yet.
func (r *Runtime) overlayHolder() *overlayRows {
	c := r.Config()
	if c.overlay == nil {
		c.overlay = &overlayRows{session: map[string]bool{}}
	}
	return c.overlay
}

// overlayState is the overlay's state, its rows read from the file when
// the config directory changed or they were never read.
func (r *Runtime) overlayState() *overlayRows {
	c := r.Config()
	o := r.overlayHolder()
	path := c.Path(OverlayFile)
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.loaded || o.path != path {
		o.loaded, o.path, o.rows = true, path, map[string]map[string]*OverlayRow{}
		if path != "" {
			o.rows = r.readOverlay(path)
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
	return &out
}

// dropSession takes out of rows read from the file the nouns whose rows
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

// readOverlay reads the rows of the file at path, the session's nouns left
// out; a missing file is no rows. A file that does not read is moved aside
// to OverlayFile.bad-<time> (the user's values kept, not overwritten by
// the next write) with a notice, and reads as no rows.
func (r *Runtime) readOverlay(path string) map[string]map[string]*OverlayRow {
	rows := map[string]map[string]*OverlayRow{}
	data, err := os.ReadFile(path)
	if err != nil {
		return rows
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		bad := path + ".bad-" + time.Now().UTC().Format("20060102-150405")
		if moved := os.Rename(path, bad); moved != nil {
			bad = path
		}
		r.UI().Notify(fmt.Sprintf("%s: %v (kept as %s, not read)", OverlayFile, err, filepath.Base(bad)))
		rows = map[string]map[string]*OverlayRow{}
	}
	r.overlayHolder().dropSession(rows)
	return rows
}

// changeOverlay changes one thing's overlay row: change gets the row (a
// new one when there is none) and answers false to remove it. With a
// file, under its lock (lockFile), it reads the file again, changes that
// one row of what the file holds now and writes it atomically, so
// processes sharing the config directory (the TUI, mcp serve, a one-shot
// CLI) keep each other's rows; the session then holds the file's rows. A
// noun of session keys, or no file, changes the session's rows only. It
// answers the row, nil when removed.
func (r *Runtime) changeOverlay(noun, key string, change func(*OverlayRow) bool) (*OverlayRow, error) {
	o := r.overlayState()
	o.mu.Lock()
	defer o.mu.Unlock()
	apply := func(rows map[string]map[string]*OverlayRow) *OverlayRow {
		row := rows[noun][key]
		if row == nil {
			row = &OverlayRow{}
		}
		if !change(row) {
			delete(rows[noun], key)
			if len(rows[noun]) == 0 {
				delete(rows, noun)
			}
			return nil
		}
		if rows[noun] == nil {
			rows[noun] = map[string]*OverlayRow{}
		}
		rows[noun][key] = row
		return row
	}
	if o.path == "" || o.session[noun] {
		return apply(o.rows).clone(), nil
	}
	unlock, err := lockFile(o.path + ".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()
	fresh := r.readOverlay(o.path)
	row := apply(fresh)
	data, err := json.MarshalIndent(fresh, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(o.path, ".overlay-*", append(data, '\n')); err != nil {
		return nil, err
	}
	for session := range o.session {
		if rows, ok := o.rows[session]; ok {
			fresh[session] = rows
		}
	}
	o.rows = fresh
	return row.clone(), nil
}

// putOverlay merges props into a thing's overlay row and writes the file.
// A nil value takes the prop out of the row (the source's value shows
// again: what undo writes back for a prop the user had not changed), and
// a row left with nothing of the user's is removed.
func (r *Runtime) putOverlay(noun, key string, props Thing, created bool) error {
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
	srcPatch       func(Ctx, string, Thing) (Thing, error)
	create, delete bool
}

// withOverlay fills what a store's source lacks with the overlay (see the
// top of this file): a List for a singleton (its one thing), a Patch that
// keeps the props the source cannot hold, a Create and a Delete where the
// store has none (a singleton's create is its set, its delete a reset), an
// archive prop where archive fits and the noun has no archive role, and
// reads (Get, List) that merge the overlay rows in (fill). A store with NoOverlay, a remote one, a copy, one
// that reads no thing and an instances store (create does not fit its
// live instances) are left as they are, and so is one that writes
// everything itself.
func (r *Runtime) withOverlay(n *Noun, s Store) Store {
	if s.NoOverlay || s.Remote || s.copy != nil || s.Get == nil && s.List == nil {
		return s
	}
	s = withSingletonList(n, s)
	if !rowFits("create", s) {
		return s // an instances store: its things are live instances
	}
	if s.SessionKeys {
		r.overlayHolder().session[n.Name] = true
	}
	o := &overlayOf{r: r, n: n, facts: s.Facts, srcGet: s.Get, srcList: s.List, srcPatch: s.Patch,
		create: s.Create == nil, delete: rowFits("delete", s) && s.Delete == nil}
	archiveName := overlayArchiveName(n)
	archive := rowFits("archive", s) && n.roleProp(RoleArchive) == nil && n.prop(archiveName) == nil
	overlaid := slices.ContainsFunc(n.Props, func(p PropSpec) bool { return p.Overlaid })
	if s.Patch != nil && !o.create && !o.delete && !archive && !overlaid {
		return s // it writes everything itself
	}
	o.markProps(archive, archiveName)
	return o.fill(s)
}

// fill is s with the overlay's reads and writes in place of what it
// lacks. A store whose every write is then the overlay's (it had no
// Patch, Create or Delete) undoes (Store.Undo); one with writes of its own
// keeps its author's Undo, which covers those writes too. A create it
// fills keeps the key it is given (Store.AcceptsKey).
func (o *overlayOf) fill(s Store) Store {
	own := s.Patch != nil || s.Create != nil || s.Delete != nil
	if s.Get != nil {
		s.Get = o.get
	}
	if s.List != nil {
		s.List = o.list
	}
	s.Patch, s.Undo = o.patch, s.Undo || !own
	if o.create {
		s.AcceptsKey, s.Create = true, o.createThing
	}
	if o.delete {
		s.Delete = o.deleteThing
	}
	return s
}

// markProps gives the noun the archive prop the overlay holds (when it
// has no archive role), makes the key the prop create writes (when the
// overlay creates), gives the overlay a facts store's read-only props, and
// lets the user's value of a computed prop the overlay holds show
// (yoursOver). A derived prop (PropSpec.Derivation) stays read-only: the
// derivations wrap the filled store and strip it from every write.
func (o *overlayOf) markProps(archive bool, archiveName string) {
	n := o.n
	if archive {
		n.Props = append(n.Props, PropSpec{Name: archiveName, Role: RoleArchive, Overlaid: true,
			Parameter: Parameter{Type: SettingBoolean, Description: "Archived by you: the thing is hidden from lists (the framework keeps the flag beside it)"}})
	}
	for i := range n.Props {
		p := &n.Props[i]
		switch {
		case p.Derivation != nil:
			continue // derived: the framework computes it, nothing holds it
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

// mergeRow is a thing with its overlay row's props over the source's.
func mergeRow(row *OverlayRow, t Thing) Thing {
	if row == nil || t == nil {
		return t
	}
	out := maps.Clone(t)
	maps.Copy(out, row.Props)
	return out
}

// merge is a thing with its overlay row's props over the source's.
func (o *overlayOf) merge(key string, t Thing) Thing {
	return mergeRow(o.r.overlayRow(o.n.Name, key), t)
}

func (o *overlayOf) get(c Ctx, key string) (Thing, error) {
	t, err := o.srcGet(c, key)
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
	for i, t := range things {
		key := o.keyOf(t)
		seen[key] = true
		things[i] = mergeRow(rows[key], t)
	}
	for _, key := range slices.Sorted(maps.Keys(rows)) {
		if rows[key].Created && !seen[key] {
			things = append(things, createdThing(o.keyName, key, rows[key]))
		}
	}
	return things, nil
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
	var after Thing
	if len(src) > 0 {
		var err error
		if after, err = o.srcPatch(c, key, src); err != nil {
			return nil, err
		}
	}
	if len(mine) > 0 {
		if err := o.r.putOverlay(o.n.Name, key, mine, false); err != nil {
			return nil, err
		}
	}
	if o.n.Store != nil && o.n.Store.Get != nil {
		if got, err := o.n.Store.Get(c, key); err == nil {
			return got, nil
		}
	}
	return o.merge(key, after), nil
}

// createThing records a thing of the user's own under the key given (else
// untitled-<time>). A singleton's create is its set: the props the
// request names are written as set writes them.
func (o *overlayOf) createThing(c Ctx, t Thing) (Thing, error) {
	if o.keyName == "" {
		return o.patch(c, "", givenProps(o.n, c.Args, t))
	}
	if stop := o.r.overlayStop(o.facts); stop != "" {
		return nil, o.r.overlayOff(o.n.Name, "", stop, slices.Sorted(maps.Keys(t)))
	}
	key := fmt.Sprint(t[o.keyName])
	if t[o.keyName] == nil || key == "" {
		key = "untitled-" + time.Now().UTC().Format("20060102-150405.000")
	}
	props := maps.Clone(t)
	delete(props, o.keyName)
	if err := o.r.putOverlay(o.n.Name, key, props, true); err != nil {
		return nil, err
	}
	return createdThing(o.keyName, key, o.r.overlayRow(o.n.Name, key)), nil
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
		if _, err := o.r.setVia(o.n, c, o.archiveProp(), true); err != nil {
			return err
		}
		return &archivedInstead{noun: o.r.DisplayNoun(o.n.Name), key: key}
	}
	if row == nil {
		return NothingErr(CauseUnchanged, o.r.DisplayNoun(o.n.Name)+" has no changes of yours to reset")
	}
	if stop := o.r.overlayStop(o.facts); stop != "" {
		return o.r.overlayOff(o.n.Name, key, stop, slices.Sorted(maps.Keys(row.Props)))
	}
	return o.r.dropOverlay(o.n.Name, key)
}

// archivedInstead is what the overlay's Delete answers when it archived
// the thing (its source cannot delete it): the delete ran, and the thing is
// still there, so runDelete leaves its relations as they are.
type archivedInstead struct{ noun, key string }

func (a *archivedInstead) Error() string {
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
		return Thing{"id": noun + "/" + key, "noun": noun, "thing": key, "props": maps.Clone(row.Props), "created": row.Created, "archived": row.Archived}
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
			if err := r.putOverlay(noun, key, props, false); err != nil {
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
