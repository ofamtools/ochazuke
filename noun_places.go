package ochazuke

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// NounPlacesFile is the record of where each noun was seen, in the state
// directory: a JSON array of NounPlace, most recently seen first.
const NounPlacesFile = "noun-places.json"

// NounPlaceLimit bounds the placements kept, in memory and in the file:
// past it the least recently seen one is dropped.
const NounPlaceLimit = 2000

// NounPlaceWriteEvery is the least time between two writes of the
// placements file while the Model runs; Config.Flush (on quit) writes what
// is left.
const NounPlaceWriteEvery = 2 * time.Second

// maxPendingPlaces is how many registrations wait for a settle before
// notePlacement settles them itself.
const maxPendingPlaces = 256

// NounPlace is one place a noun was seen: an instance of Noun registered
// under instances of the Ancestors nouns (outermost first, app left out),
// on Page, or inside the open modal Modal. Inside a modal the ancestors
// start at the modal instance, where the stack walk stops; Page is then the
// page the modal opened over. Instance ids are never part of a placement
// (rows and generated instances differ per run), only nouns and the page id
// (page ids are stable). A list row counts once per SetItems for each noun
// its rows answer to, not once per row.
type NounPlace struct {
	Noun      string    `json:"noun"`
	Ancestors []string  `json:"ancestors"`
	Page      string    `json:"page,omitempty"`
	PageTitle string    `json:"pageTitle,omitempty"`
	Modal     string    `json:"modal,omitempty"`
	Count     int       `json:"count"`
	First     time.Time `json:"first"`
	Last      time.Time `json:"last"`
}

// Chain is the stack walk of the placement from outside in: the ancestors,
// then the noun itself.
func (p NounPlace) Chain() []string { return append(slices.Clone(p.Ancestors), p.Noun) }

func (p NounPlace) key() string {
	return p.Noun + "\x00" + strings.Join(p.Ancestors, "\x1f") + "\x00" + p.Page + "\x00" + p.Modal
}

// placements is the config's record: what was seen, what was registered
// since the last settle, and the file's writer once EnsureNounPlaces
// started it.
type placements struct {
	byKey   map[string]*NounPlace
	pending []placementNote
	started bool
	writer  *placementWriter
	dirty   bool
	sent    time.Time
}

// placementNote is one instance registered (id), a list's rows by noun
// (rows), or an existing instance opened as a modal with what it holds
// (subtree).
type placementNote struct {
	id      string
	rows    []string
	subtree bool
}

type placementWriter struct {
	path string
	ops  chan []NounPlace
	done chan struct{}

	mu       sync.Mutex
	err      error
	reported bool
}

// NounPlacesPath is the placements file, "" when placements are not
// persisted (no state directory: Config.Disable, a test binary).
func (c *Config) NounPlacesPath() string {
	dir := c.StateDirectory()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, NounPlacesFile)
}

// placementConfig is the config, nil before it is registered (the
// framework's first instances).
func (r *Runtime) placementConfig() *Config {
	inst := r.Instances[configID]
	if inst == nil {
		return nil
	}
	c, _ := inst.Component.(*Config)
	return c
}

func (c *Config) placementRecord() *placements {
	if c.places == nil {
		c.places = &placements{byKey: map[string]*NounPlace{}}
	}
	return c.places
}

// notePlacement queues an instance to record at the next settle; register
// calls it for every instance that is not a list row.
func (r *Runtime) notePlacement(note placementNote) {
	c := r.placementConfig()
	if c == nil {
		return
	}
	p := c.placementRecord()
	p.pending = append(p.pending, note)
	if len(p.pending) >= maxPendingPlaces {
		// A runtime without a Model (a headless host, the CLI) settles
		// as it goes, so the queue stays bounded.
		r.settlePlacements()
	}
}

// noteRows queues a list's rows: once per noun they answer to. nouns may
// hold repeats; most lists hold one noun, so only a change from the
// previous noun is looked up.
func (r *Runtime) noteRows(list string, items []ListItem) {
	if len(items) == 0 {
		return
	}
	var nouns []string
	last := ""
	for i := range items {
		if n := items[i].Noun; n != last {
			last = n
			if !slices.Contains(nouns, n) {
				nouns = append(nouns, n)
			}
		}
	}
	r.notePlacement(placementNote{id: list, rows: nouns})
}

// settlePlacements records every instance registered since the last call,
// at the place it stands now (so a page registered after its children, or a
// modal opened after its body was added, is part of the record). The Model
// calls it after every message; Placements, Pop and instance removal call
// it first, so an instance removed in the same update is still seen. With
// nothing pending it costs a lookup.
func (r *Runtime) settlePlacements() {
	c := r.placementConfig()
	if c == nil || c.places == nil {
		return
	}
	p := c.places
	if len(p.pending) > 0 {
		notes := p.pending
		p.pending = nil
		now := time.Now()
		seen := map[string]bool{}
		for _, note := range notes {
			switch {
			case note.rows != nil:
				if inst := r.Instances[note.id]; inst != nil {
					base := r.placementOf(note.id, inst.Noun)
					base.Ancestors = append(base.Ancestors, inst.Noun)
					for _, noun := range note.rows {
						one := base
						one.Noun = noun
						one.Ancestors = slices.Clone(base.Ancestors)
						p.record(one, now)
					}
				}
			case note.subtree:
				for _, id := range r.holders {
					if !seen[id] && slices.Contains(r.Ancestry(id), note.id) {
						seen[id] = true
						p.record(r.placementOf(id, r.Instances[id].Noun), now)
						if list := r.List(id); list != nil && len(list.all) > 0 {
							r.noteRows(id, list.all)
						}
					}
				}
			default:
				if inst := r.Instances[note.id]; inst != nil && !seen[note.id] {
					seen[note.id] = true
					p.record(r.placementOf(note.id, inst.Noun), now)
				}
			}
		}
		if len(p.pending) > 0 { // a subtree's lists queued their rows
			r.settlePlacements()
			return
		}
	}
	if p.dirty && p.writer != nil && time.Since(p.sent) >= NounPlaceWriteEvery {
		p.send(c, false)
	}
}

// placementOf is where id stands now, without counts: its ancestors'
// nouns outermost first, app left out; inside an open modal they start at
// the modal instance and Page is the page beneath it (else the current
// page).
func (r *Runtime) placementOf(id, noun string) NounPlace {
	out := NounPlace{Noun: noun}
	var ancestors []string
	walk := r.Ancestry(id)
	inModal := false
	for i, at := range walk {
		if page := r.PageOf(at); page != nil {
			out.Page, out.PageTitle = page.ID, page.Title
			break
		}
		if !inModal && slices.Contains(r.modals, at) {
			inModal = true
			out.Modal = r.Instances[at].Noun
			if i > 0 {
				ancestors = append(ancestors, r.Instances[at].Noun)
			}
			continue
		}
		if i == 0 || at == "app" || inModal {
			continue
		}
		ancestors = append(ancestors, r.Instances[at].Noun)
	}
	if !inModal && out.Page != "" {
		// The page's own noun is the outermost ancestor (unless the
		// instance is the page).
		if out.Page != id {
			ancestors = append(ancestors, r.Instances[out.Page].Noun)
		}
	}
	if inModal && out.Page == "" {
		if page := r.PageOf(r.Page); page != nil {
			out.Page, out.PageTitle = page.ID, page.Title
		}
	}
	slices.Reverse(ancestors)
	if ancestors == nil {
		ancestors = []string{}
	}
	out.Ancestors = ancestors
	return out
}

// record counts one sighting of at, dropping the least recently seen
// placement past NounPlaceLimit.
func (p *placements) record(at NounPlace, now time.Time) {
	key := at.key()
	if old := p.byKey[key]; old != nil {
		old.Count++
		old.Last = now
		if at.PageTitle != "" {
			old.PageTitle = at.PageTitle
		}
		p.dirty = true
		return
	}
	at.Count, at.First, at.Last = 1, now, now
	p.byKey[key] = &at
	p.dirty = true
	p.trim()
}

func (p *placements) trim() {
	for len(p.byKey) > NounPlaceLimit {
		var oldest string
		var at time.Time
		for key, one := range p.byKey {
			if oldest == "" || one.Last.Before(at) || one.Last.Equal(at) && key < oldest {
				oldest, at = key, one.Last
			}
		}
		delete(p.byKey, oldest)
	}
}

// merge adds placements read from the file to the record.
func (p *placements) merge(read []NounPlace) {
	for _, one := range read {
		if one.Noun == "" {
			continue
		}
		if one.Ancestors == nil {
			one.Ancestors = []string{}
		}
		key := one.key()
		if old := p.byKey[key]; old != nil {
			old.Count += one.Count
			if !one.First.IsZero() && one.First.Before(old.First) {
				old.First = one.First
			}
			if one.Last.After(old.Last) {
				old.Last = one.Last
			}
			if old.PageTitle == "" {
				old.PageTitle = one.PageTitle
			}
			continue
		}
		copied := one
		p.byKey[key] = &copied
	}
	p.trim()
}

// list is the record, most seen first, then most recently seen; noun ""
// is every noun.
func (p *placements) list(noun string) []NounPlace {
	out := []NounPlace{}
	for _, one := range p.byKey {
		if noun == "" || one.Noun == noun {
			out = append(out, *one)
		}
	}
	slices.SortFunc(out, func(a, b NounPlace) int {
		switch {
		case a.Count != b.Count:
			return b.Count - a.Count
		case !a.Last.Equal(b.Last):
			return b.Last.Compare(a.Last)
		}
		return strings.Compare(a.key(), b.key())
	})
	return out
}

// NounPlaces is where noun was seen (every noun when noun is ""): one entry
// per distinct place, most seen first, then most recently seen. It includes
// the placements file's when the Model read it (EnsureNounPlaces). A
// consumer resolving a key for a noun that is not on the focus path walks a
// placement's Chain (outermost first) from its Page, or from its Modal.
func (r *Runtime) NounPlaces(noun string) []NounPlace {
	c := r.Config()
	if c == nil {
		return []NounPlace{}
	}
	r.settlePlacements()
	return c.placementRecord().list(noun)
}

// EnsureNounPlaces persists the placements record, once: it reads the
// placements file into the record (a file that does not parse is one
// notice and is replaced at the next write) and starts the writer the
// record is saved through, off the loop, at most every
// NounPlaceWriteEvery and on Config.Flush. Nothing is read or written
// without a state directory (Config.Disable, a test binary). Model.Init
// calls it.
func (r *Runtime) EnsureNounPlaces() {
	c := r.Config()
	if c == nil {
		return
	}
	p := c.placementRecord()
	if p.started {
		return
	}
	p.started = true
	path := c.NounPlacesPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		c.report(NounPlacesFile + ": " + err.Error())
	default:
		var read []NounPlace
		if err := json.Unmarshal(data, &read); err != nil {
			c.report(NounPlacesFile + ": " + err.Error() + " (replaced)")
			p.dirty = true
		} else {
			p.merge(read)
		}
	}
	w := &placementWriter{path: path, ops: make(chan []NounPlace, 1), done: make(chan struct{})}
	p.writer = w
	go w.run()
}

// send hands the record to the writer: without waiting unless final (a
// writer still busy with the last one keeps the record dirty for the next
// settle). A write failure is reported once.
func (p *placements) send(c *Config, final bool) {
	w := p.writer
	if err := w.failure(); err != nil {
		w.mu.Lock()
		first := !w.reported
		w.reported = true
		w.mu.Unlock()
		if first {
			c.report(NounPlacesFile + ": " + err.Error() + " (not saved)")
		}
		p.dirty = false
		return
	}
	snapshot := make([]NounPlace, 0, len(p.byKey))
	for _, one := range p.byKey {
		snapshot = append(snapshot, *one)
	}
	slices.SortFunc(snapshot, func(a, b NounPlace) int {
		if c := b.Last.Compare(a.Last); c != 0 {
			return c
		}
		return strings.Compare(a.key(), b.key())
	})
	if final {
		w.ops <- snapshot
	} else {
		select {
		case w.ops <- snapshot:
		default:
			return
		}
	}
	p.dirty, p.sent = false, time.Now()
}

// flush writes what the record holds and stops the writer; Config.Flush
// calls it.
func (p *placements) flush(c *Config) error {
	w := p.writer
	if w == nil {
		return nil
	}
	if p.dirty {
		p.send(c, true)
	}
	p.writer = nil
	close(w.ops)
	<-w.done
	return w.failure()
}

func (w *placementWriter) run() {
	defer close(w.done)
	for snapshot := range w.ops {
		if w.failure() != nil {
			continue
		}
		data, err := json.MarshalIndent(snapshot, "", " ")
		if err == nil {
			err = writeAtomic(w.path, ".noun-places-*.json", append(data, '\n'))
		}
		if err != nil {
			w.mu.Lock()
			if w.err == nil {
				w.err = err
			}
			w.mu.Unlock()
		}
	}
}

func (w *placementWriter) failure() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func registerPlacements(r *Runtime) {
	r.Define("nouns")
	r.Register(Operation{Verb: "places", Noun: "nouns", Description: "List where a noun was seen (every noun without one): each distinct chain of ancestor nouns, outermost first, with its page or modal, how often and when last; kept across runs in " + NounPlacesFile, Parameters: map[string]Parameter{"noun": {Type: "string", Description: "The noun (default every noun)"}}, Positional: []string{"noun"}, HistoryKind: HistoryFetch, Result: "placement[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.NounPlaces(StringArg(a, "noun", "")), nil
	}})
	r.Register(Operation{Verb: "list", Noun: "nouns", Description: "List the defined nouns with their verbs and how many places each was seen in", HistoryKind: HistoryFetch, Result: "noun[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		places := map[string]int{}
		for _, one := range r.NounPlaces("") {
			places[one.Noun]++
		}
		names := make([]string, 0, len(r.Nouns))
		for name := range r.Nouns {
			names = append(names, name)
		}
		slices.Sort(names)
		out := make([]map[string]any, 0, len(names))
		for _, name := range names {
			out = append(out, map[string]any{"noun": name, "verbs": slices.Clone(r.Nouns[name].Verbs), "places": places[name]})
		}
		return out, nil
	}})
	r.Register(Operation{Verb: "places-path", Noun: "nouns", Description: "Print the placements file: $XDG_STATE_HOME/<app>/" + NounPlacesFile + ", else ~/.local/state/<app>/" + NounPlacesFile, HistoryKind: HistoryFetch, Result: "path", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		cfg := r.Config()
		if cfg == nil {
			return "", nil
		}
		return cfg.NounPlacesPath(), nil
	}})
}
