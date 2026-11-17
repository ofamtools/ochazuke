package ochazuke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Where the overlay keeps its rows (run plan 5.4a, overlay plan OV2): one
// interface, OverlayBackend, chosen by the setting overlay.store. The
// framework fills two: app (overlay.json beside the program's
// settings.json, what a program run alone uses) and family (one
// overlay.json shared by every family program, in the ofam folder beside
// the programs' config folders). An application adds its own with
// SetOverlayBackend (mtm keeps the rows in its database, decision 54),
// which makes it the setting's default. A value naming no backend reads as
// app. The rows a process holds are the session's copy: a read takes
// copies under the read lock; a write goes to the backend, under its lock.
//
// The setting overlay.conflict says what a read shows when the source's
// value of a prop changed after the user wrote theirs (decision 55): keep-mine
// (the default) shows the user's value, and the row keeps the original as
// it was when they wrote it, so OverlayChanges can tell "the original
// changed" and give the new one; take-source shows the source's value.
// A read never writes (spine S6 rule 6), so take-source drops nothing: the
// user's value stays in their changes (ochazuke.overlay lists it, with
// its original), and keep-mine shows it again.

const (
	// OverlayStoreSetting names the overlay's backend.
	OverlayStoreSetting = "overlay.store"
	// OverlayStoreApp is the program's own file: overlay.json beside its
	// settings.json.
	OverlayStoreApp = "app"
	// OverlayStoreFamily is one file for every family program:
	// FamilyFolder/overlay.json beside the programs' config folders.
	OverlayStoreFamily = "family"
	// FamilyFolder is the config folder the family programs share.
	FamilyFolder = "ofam"
	// OverlayConflictSetting says which value a read shows when the source
	// changed a prop after the user wrote theirs.
	OverlayConflictSetting = "overlay.conflict"
	// OverlayKeepMine shows the user's value (the default).
	OverlayKeepMine = "keep-mine"
	// OverlayTakeSource shows the source's value.
	OverlayTakeSource = "take-source"
)

// OverlayBackend keeps the overlay's rows: by noun, then by the thing's
// key. FileOverlay is the framework's; an application gives its own with
// SetOverlayBackend.
type OverlayBackend interface {
	// Rows are every row the backend holds.
	Rows() (map[string]map[string]*OverlayRow, error)
	// Change changes one thing's row as the backend holds it now, under
	// the backend's lock, so processes sharing it keep each other's rows:
	// change gets that row (a new one when there is none) and answers
	// false to remove it. It answers the row after (nil when removed) and,
	// when it read every row again doing so, all of them (fresh: the
	// process then holds those); a nil fresh keeps what the process holds
	// of the other rows.
	Change(noun, key string, change func(*OverlayRow) bool) (row *OverlayRow, fresh map[string]map[string]*OverlayRow, err error)
}

// OverlayVersioner is a backend that tells cheaply whether its rows
// changed (another process wrote them): a read whose version differs from
// the one the process read the rows at reads them again; the process's own
// write moves it without a read.
type OverlayVersioner interface {
	Version() (string, error)
}

// OverlayVersionedChange is a versioned backend whose change also answers
// its version read inside the write's transaction, before and after the
// write: when before is the version the process read the rows at, the
// process keeps its rows (only its own write moved it); otherwise the next
// read pass reads them again. The overlay uses it in place of Change.
type OverlayVersionedChange interface {
	ChangeVersioned(noun, key string, change func(*OverlayRow) bool) (row *OverlayRow, before, after string, err error)
}

// PropChange is what the overlay keeps about one prop the user wrote:
// the source's value when they wrote it (the original as last seen; Seen
// is false when the source held no value of that prop then, or was not
// read), who wrote it and when.
type PropChange struct {
	Original any       `json:"original,omitempty"`
	Seen     bool      `json:"seen,omitempty"`
	By       *Actor    `json:"by,omitempty"`
	At       time.Time `json:"at,omitzero"`
}

// OverlayPropChange is one prop of a thing's overlay row as it stands:
// the user's value, the original as last seen, the source's value now
// (Current, CurrentKnown false when the source could not be read), whether
// the original changed since the user wrote theirs, and which value a read
// shows (Shown: yours, or source under overlay.conflict take-source).
type OverlayPropChange struct {
	Prop            string    `json:"prop"`
	Yours           any       `json:"yours"`
	Original        any       `json:"original,omitempty"`
	Seen            bool      `json:"seen,omitempty"`
	Current         any       `json:"current,omitempty"`
	CurrentKnown    bool      `json:"currentKnown,omitempty"`
	OriginalChanged bool      `json:"originalChanged,omitempty"`
	Shown           string    `json:"shown"`
	By              *Actor    `json:"by,omitempty"`
	At              time.Time `json:"at,omitzero"`
}

// The values OverlayPropChange.Shown takes.
const (
	OverlayShownYours  = "yours"
	OverlayShownSource = "source"
)

// FileOverlay is the backend of a JSON file of rows (overlay.json): written
// atomically under a file lock (Path.lock), read again under it before
// each write. A file that does not read is moved aside to
// Path.bad-<time> (the user's values kept, not overwritten by the next
// write) with a notice, and reads as no rows.
type FileOverlay struct {
	Path string
	// Notify, when set, tells the user about a file that did not read.
	Notify func(string)
}

// Rows reads the file; a missing file is no rows.
func (f FileOverlay) Rows() (map[string]map[string]*OverlayRow, error) {
	rows := map[string]map[string]*OverlayRow{}
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return rows, nil
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		bad := f.Path + ".bad-" + time.Now().UTC().Format("20060102-150405")
		if moved := os.Rename(f.Path, bad); moved != nil {
			bad = f.Path
		}
		if f.Notify != nil {
			f.Notify(fmt.Sprintf("%s: %v (kept as %s, not read)", filepath.Base(f.Path), err, filepath.Base(bad)))
		}
		return map[string]map[string]*OverlayRow{}, nil
	}
	return rows, nil
}

// Change reads the file again under its lock, changes that one row of
// what it holds now and writes it back; fresh is the whole file after.
func (f FileOverlay) Change(noun, key string, change func(*OverlayRow) bool) (*OverlayRow, map[string]map[string]*OverlayRow, error) {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return nil, nil, err
	}
	unlock, err := lockFile(f.Path + ".lock")
	if err != nil {
		return nil, nil, err
	}
	defer unlock()
	fresh, err := f.Rows()
	if err != nil {
		return nil, nil, err
	}
	row := ApplyOverlayChange(fresh, noun, key, change)
	data, err := json.MarshalIndent(fresh, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	if err := writeAtomic(f.Path, ".overlay-*", append(data, '\n')); err != nil {
		return nil, nil, err
	}
	return row, fresh, nil
}

// ApplyOverlayChange changes one row of rows in place, as a backend's
// Change does: change gets the row (a new one when there is none) and
// answers false to remove it. It answers the row, nil when removed.
func ApplyOverlayChange(rows map[string]map[string]*OverlayRow, noun, key string, change func(*OverlayRow) bool) *OverlayRow {
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

// SetOverlayBackend adds a backend under name and makes it overlay.store's
// default (it joins the setting's values): an application's own place for
// the user's changes. Call it before the first read of the overlay. A nil
// backend or an empty name adds nothing.
func (r *Runtime) SetOverlayBackend(name string, b OverlayBackend) error {
	if name == "" || b == nil {
		return nil
	}
	o := r.overlayHolder()
	if o.backends == nil {
		o.backends = map[string]OverlayBackend{}
	}
	o.backends[name] = b
	one, _ := frameworkSetting(OverlayStoreSetting)
	if !slices.Contains(one.Enum, name) {
		one.Enum = append(slices.Clone(one.Enum), name)
	}
	one.Default = name
	one.Description += "; " + name + " (this program's default): kept where the program keeps its own data"
	return r.DeclareSetting(one)
}

// overlayBackend is the backend overlay.store names, with what tells it
// apart from another (its name and file); nil, "" for the session's rows
// only (a file backend with no config directory: a sandbox, a test).
func (r *Runtime) overlayBackend() (OverlayBackend, string) {
	name, _ := r.settingValue(OverlayStoreSetting)
	s, _ := name.(string)
	if b := r.overlayHolder().backends[s]; b != nil {
		return b, s
	}
	c := r.Config()
	path := c.Path(OverlayFile)
	if s == OverlayStoreFamily {
		if dir := c.Directory(); dir != "" {
			path = filepath.Join(filepath.Dir(dir), FamilyFolder, OverlayFile)
		}
	} else {
		s = OverlayStoreApp
	}
	if path == "" {
		return nil, ""
	}
	return FileOverlay{Path: path, Notify: func(text string) { r.UI().Notify(text) }}, s + " " + path
}

// overlayTakesSource reports whether overlay.conflict is take-source.
func (r *Runtime) overlayTakesSource() bool {
	v, _ := r.settingValue(OverlayConflictSetting)
	return v == OverlayTakeSource
}

// originalChanged reports whether the source's value of prop name (in
// src, the source's thing) differs from the original the user saw when
// they wrote theirs. A prop with no original seen never changed.
func (row *OverlayRow) originalChanged(name string, src Thing) bool {
	ch := row.Changes[name]
	if ch == nil || !ch.Seen || src == nil {
		return false
	}
	now, ok := src[name]
	return !ok || !overlaySame(now, ch.Original)
}

// overlaySame reports whether two values encode alike (a value read back
// from JSON and the source's typed one compare equal).
func overlaySame(a, b any) bool {
	x, errX := json.Marshal(a)
	y, errY := json.Marshal(b)
	return errX == nil && errY == nil && bytes.Equal(x, y)
}

// mergeOver is a thing with its overlay row's props over the source's: a
// prop whose original changed since the user wrote it shows the source's
// value under overlay.conflict take-source, the user's otherwise.
func (r *Runtime) mergeOver(row *OverlayRow, t Thing) Thing {
	if row == nil || t == nil {
		return t
	}
	out := maps.Clone(t)
	take := r.overlayTakesSource()
	for name, v := range row.Props {
		if take && row.originalChanged(name, t) {
			continue
		}
		out[name] = v
	}
	return out
}

// noteChanges records, for each prop written (props: nil takes it out),
// who wrote it, when, and the source's value then (src, nil when it was
// not read: an earlier original is kept).
func (row *OverlayRow) noteChanges(props Thing, src Thing, by Actor, at time.Time) {
	for name, v := range props {
		if v == nil {
			delete(row.Changes, name)
			continue
		}
		ch := &PropChange{At: at}
		if by != (Actor{}) {
			who := by
			ch.By = &who
		}
		if before := row.Changes[name]; before != nil && src == nil {
			ch.Original, ch.Seen = before.Original, before.Seen
		}
		if orig, ok := src[name]; ok {
			ch.Original, ch.Seen = orig, true
		}
		if row.Changes == nil {
			row.Changes = map[string]*PropChange{}
		}
		row.Changes[name] = ch
	}
	if len(row.Changes) == 0 {
		row.Changes = nil
	}
}

// OverlayChanges is what the overlay keeps of one thing, prop by prop, and
// how the source stands now (one read of the source's thing; none for a
// thing of the user's own, or a noun the overlay does not fill): the data
// a view draws "yours over the original" from. A thing with no row has no
// changes.
func (r *Runtime) OverlayChanges(c Ctx, noun, key string) ([]OverlayPropChange, error) {
	row := r.overlayRow(noun, key)
	if row == nil {
		return nil, nil
	}
	var src Thing
	known := false
	if n := r.Noun(noun); n != nil && n.overlay != nil && n.overlay.srcGet != nil && !row.Created {
		of := n.overlay
		if c.Runtime == nil {
			c.Runtime = r
		}
		if got, err := of.srcGet(c, key); err == nil {
			src, known = got, true
		}
	}
	take := r.overlayTakesSource()
	out := make([]OverlayPropChange, 0, len(row.Props))
	for _, name := range slices.Sorted(maps.Keys(row.Props)) {
		one := OverlayPropChange{Prop: name, Yours: row.Props[name], Shown: OverlayShownYours}
		if ch := row.Changes[name]; ch != nil {
			one.Original, one.Seen, one.By, one.At = ch.Original, ch.Seen, ch.By, ch.At
		}
		if known {
			one.Current, one.CurrentKnown = src[name], true
			one.OriginalChanged = row.originalChanged(name, src)
			if take && one.OriginalChanged {
				one.Shown = OverlayShownSource
			}
		}
		out = append(out, one)
	}
	return out, nil
}

// OverlayRow is a copy of the row the overlay keeps beside one thing, nil
// when it keeps none: for an application that reads a change it records
// itself (ChangeOverlay).
func (r *Runtime) OverlayRow(noun, key string) *OverlayRow { return r.overlayRow(noun, key) }

// OverlayRows are copies of every row the overlay keeps for one noun, by
// the thing's key.
func (r *Runtime) OverlayRows(noun string) map[string]*OverlayRow { return r.overlayNounRows(noun) }

// ChangeOverlay changes one thing's row in the overlay's backend, as the
// overlay's own writes do (change gets the row, a new one when there is
// none, and answers false to remove it), so the process's rows and the
// change token follow: for an application that records a change of the
// user's beside a thing it does not own (mtm: the project of a tool's row).
// It answers the row after, nil when removed.
func (r *Runtime) ChangeOverlay(noun, key string, change func(*OverlayRow) bool) (*OverlayRow, error) {
	return r.changeOverlay(noun, key, change)
}

// ReloadOverlay drops the rows the process holds, so the next read takes
// them from the backend again: after an application changed the backend
// outside ChangeOverlay (a one-time move of rows into it, a project's
// delete rewriting the rows that name it).
func (r *Runtime) ReloadOverlay() {
	o := r.overlayHolder()
	o.mu.Lock()
	o.loaded, o.versions = false, nil
	o.mu.Unlock()
}
