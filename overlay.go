package ochazuke

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The overlay (no-refusals D1): props a store's source cannot hold (a
// system fact, a store without Patch) are kept by the framework beside the
// thing, keyed by noun, key and prop, merged into get and list and marked
// as the user's. A create on a store that cannot create records a thing of
// the user's own; a delete on one that cannot delete archives (nothing is
// destroyed). So every listed verb of every thing noun runs: set, create,
// archive and delete never answer "a system fact" or "no Store.Patch".
//
// The rows live in overlay.json beside settings.json (Config.Path), written
// atomically; with no config directory (a sandbox, a test) they live for
// the session. Setting facts.overlay off answers a write the source cannot
// hold with a ChoiceAnswer naming the verbs that do change the thing.

const (
	// OverlayFile holds the overlay rows beside settings.json.
	OverlayFile = "overlay.json"
	// FactsOverlaySetting turns the overlay's writes on or off.
	FactsOverlaySetting = "facts.overlay"
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
// (Store.SessionKeys): their rows live here and never reach the file.
type overlayRows struct {
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

func (r *Runtime) overlayState() *overlayRows {
	c := r.Config()
	o := r.overlayHolder()
	if path := c.Path(OverlayFile); !o.loaded || o.path != path {
		o.loaded, o.path, o.rows = true, path, map[string]map[string]*OverlayRow{}
		if path != "" {
			o.rows = r.readOverlay(path)
		}
	}
	return o
}

// dropSession takes out of rows read from the file the nouns whose rows
// are the session's (a row of an earlier run would wear another thing).
func (o *overlayRows) dropSession(rows map[string]map[string]*OverlayRow) {
	for noun := range o.session {
		delete(rows, noun)
	}
}

// overlayRow is the overlay of one thing, nil when it has none.
func (r *Runtime) overlayRow(noun, key string) *OverlayRow {
	return r.overlayState().rows[noun][key]
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
		return apply(o.rows), nil
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
	return row, nil
}

// putOverlay merges props into a thing's overlay row and writes the file.
func (r *Runtime) putOverlay(noun, key string, props Thing, created bool) error {
	_, err := r.changeOverlay(noun, key, func(row *OverlayRow) bool {
		if row.Props == nil {
			row.Props = map[string]any{}
		}
		maps.Copy(row.Props, props)
		row.Created = row.Created || created
		return true
	})
	return err
}

// dropOverlay removes a thing's overlay row.
func (r *Runtime) dropOverlay(noun, key string) error {
	_, err := r.changeOverlay(noun, key, func(*OverlayRow) bool { return false })
	return err
}

// overlayOn reports whether writes the source cannot hold go to the
// overlay (facts.overlay, default true).
func (r *Runtime) overlayOn() bool {
	s := r.Settings()
	return !s.Has(FactsOverlaySetting) || s.Bool(FactsOverlaySetting)
}

// overlayOff is the answer to a write the source cannot hold while
// facts.overlay is off: the verbs of the noun that change its things
// (its application verbs), each with the command that runs it.
func (r *Runtime) overlayOff(noun, key string, props []string) error {
	shown := r.DisplayNoun(noun)
	answer := &ChoiceAnswer{Needs: "verb", Reason: fmt.Sprintf("%s %s cannot hold %s (facts.overlay is off); a verb that changes it:", shown, key, strings.Join(props, ", "))}
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
	keyName        string
	srcGet         func(Ctx, string) (Thing, error)
	srcList        func(Ctx, ListQuery) ([]Thing, error)
	srcPatch       func(Ctx, string, Thing) (Thing, error)
	create, delete bool
}

// withOverlay fills what a store's source lacks with the overlay: a List
// for a singleton (its one thing), a Patch that keeps the props the source
// cannot hold (every prop when it has no Patch, the Overlaid ones of a
// facts store), a Create and a Delete where the rows for them fit the
// store's kind and it has none, an archive prop where archive fits and the
// noun has no archive role, and reads (Get, List) that merge the overlay
// rows in. A system fact (Store.Facts) is filled, and any store that sets
// Store.Overlay (a read-only source the user may annotate); a store that
// can read no thing, a remote one and a copy are left as they are.
func (r *Runtime) withOverlay(n *Noun, s Store) Store {
	if s.Remote || s.copy != nil || s.Get == nil && s.List == nil {
		return s
	}
	s = withSingletonList(s)
	if !s.Facts && !s.Overlay || !rowFits("create", s) {
		// Any other store keeps its own shape unless it asks for the
		// overlay (Store.Overlay); a singleton fact keeps its own too, so
		// set NOUN still reads the :set line where the noun has none
		// (shadowsNoun).
		return s
	}
	if s.SessionKeys {
		r.overlayHolder().session[n.Name] = true
	}
	o := &overlayOf{r: r, n: n, srcGet: s.Get, srcList: s.List, srcPatch: s.Patch,
		create: rowFits("create", s) && s.Create == nil, delete: rowFits("delete", s) && s.Delete == nil}
	archiveName := overlayArchiveName(n)
	archive := rowFits("archive", s) && n.roleProp(RoleArchive) == nil && n.prop(archiveName) == nil
	overlaid := slices.ContainsFunc(n.Props, func(p PropSpec) bool { return p.Overlaid })
	if !rowFits("set", s) || s.Patch != nil && !o.create && !o.delete && !archive && !overlaid {
		return s
	}
	o.markProps(archive, archiveName, s.Facts)
	if s.Get != nil {
		s.Get = o.get
	}
	if s.List != nil {
		s.List = o.list
	}
	s.Patch = o.patch
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
// overlay creates), and gives the overlay a facts store's read-only props.
func (o *overlayOf) markProps(archive bool, archiveName string, facts bool) {
	n := o.n
	if archive {
		n.Props = append(n.Props, PropSpec{Name: archiveName, Role: RoleArchive, Overlaid: true,
			Parameter: Parameter{Type: SettingBoolean, Description: "Archived by you: the thing is hidden from lists (the framework keeps the flag beside it)"}})
	}
	for i := range n.Props {
		p := &n.Props[i]
		switch {
		case p.Key:
			o.keyName = p.Name
			if o.create {
				p.ReadOnly, p.CreateOnly = false, true
			}
		case facts && p.ReadOnly:
			p.ReadOnly, p.Overlaid = false, true
		}
	}
}

// overlayArchiveName is the name of the archive prop the overlay gives a
// noun: archived, unless the noun's own prop holds the word.
func overlayArchiveName(n *Noun) string {
	if n.prop(OverlayArchiveProp) != nil {
		return "archived-by-you"
	}
	return OverlayArchiveProp
}

// withSingletonList gives a store that holds one thing (where list fits
// and create does not) a list of that thing.
func withSingletonList(s Store) Store {
	if rowFits("list", s) && !rowFits("create", s) && s.List == nil && s.Get != nil {
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
	row := o.r.overlayRow(o.n.Name, key)
	if row == nil || t == nil {
		return t
	}
	out := maps.Clone(t)
	maps.Copy(out, row.Props)
	return out
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
	seen := map[string]bool{}
	for i, t := range things {
		key := fmt.Sprint(t[o.keyName])
		seen[key] = true
		things[i] = o.merge(key, t)
	}
	rows := o.r.overlayState().rows[o.n.Name]
	for _, key := range slices.Sorted(maps.Keys(rows)) {
		if rows[key].Created && !seen[key] {
			things = append(things, createdThing(o.keyName, key, rows[key]))
		}
	}
	return things, nil
}

// held reports whether the overlay holds a prop: every prop of a store
// with no Patch, else the Overlaid ones.
func (o *overlayOf) held(name string) bool {
	p := o.n.prop(name)
	return o.srcPatch == nil || p != nil && p.Overlaid
}

func (o *overlayOf) patch(c Ctx, key string, t Thing) (Thing, error) {
	src, mine := Thing{}, Thing{}
	for name, v := range t {
		if o.held(name) {
			mine[name] = v
		} else {
			src[name] = v
		}
	}
	if len(mine) > 0 && !o.r.overlayOn() {
		return nil, o.r.overlayOff(o.n.Name, key, slices.Sorted(maps.Keys(mine)))
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

func (o *overlayOf) createThing(c Ctx, t Thing) (Thing, error) {
	if !o.r.overlayOn() {
		return nil, o.r.overlayOff(o.n.Name, "", slices.Sorted(maps.Keys(t)))
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

// deleteThing removes a thing of the user's own, and archives any other
// (the source keeps it; nothing is destroyed).
func (o *overlayOf) deleteThing(c Ctx, key string) error {
	if row := o.r.overlayRow(o.n.Name, key); row != nil && row.Created {
		return o.r.dropOverlay(o.n.Name, key)
	}
	if !o.r.overlayOn() {
		return o.r.overlayOff(o.n.Name, key, []string{OverlayArchiveProp})
	}
	archive := OverlayArchiveProp
	if p := o.n.roleProp(RoleArchive); p != nil {
		archive = p.Name
	}
	return o.r.putOverlay(o.n.Name, key, Thing{archive: true}, false)
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
			rows := r.overlayState().rows
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
