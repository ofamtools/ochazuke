package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
)

// pickerID is the picker's instance, the owner of its index, search and
// preview jobs.
const pickerID = "picker"

// PathFinder is the path picker's fuzzy finder (NewPicker). It owns one
// modal and one ordinary editor. Background commands only read immutable
// snapshots; HandleMessage is the sole result-application boundary.
// Its work runs as four replacing jobs on the picker instance (the "index",
// "search", "preview" and "stat" slots), so the runtime's jobs list decides which
// result is current: a newer job in a slot drops the older, and Close
// cancels them all.
type PathFinder struct {
	Runtime      *Runtime
	Origin       Context
	Directory    string
	Filter       string
	Entries      []PickerEntry
	Selected     int
	Error        string
	Loading      bool
	input        *Input
	originInput  *Input
	originText   string
	originKind   string
	start, end   int
	base         string
	query        string
	indexed      []PickerEntry
	indexReady   bool
	indexRunning bool
	fullIndex    func() // the recursive index, queued once shallow results are published
	indexID      uint64 // the current job in the index slot
	searchID     uint64 // the current job in the search slot
	indexFailed  bool
	preview      pickerPreview
	previewID    uint64 // the current job in the preview slot
	pending      []tea.Cmd
	// The results table's lazy stats (picker_stats.go): the cache, the
	// paths of the stat job in flight, the rows the last frame drew and
	// the stat function (os.Lstat; tests count calls).
	stats     *pickerStats
	statting  map[string]bool
	drawnRows int
	statFunc  func(string) (fs.FileInfo, error)
	// match is the match mode search.cycle-match or search.set-match
	// chose while open; "" is the chooser.match setting (search_match.go).
	match string
}

type pickerIndexMsg struct {
	picker  *PathFinder
	id      uint64
	shallow bool
	entries []PickerEntry
	err     error
}

type pickerSearchMsg struct {
	picker  *PathFinder
	id      uint64
	entries []PickerEntry
	err     error
}

// Picker is the path finder's old name.
//
// Deprecated: use PathFinder; Picker is kept for one release (v0.96.0).
type Picker = PathFinder

// NewPicker returns the path finder for r (picker.open with no name opens
// it).
func NewPicker(r *Runtime) *PathFinder {
	var p *PathFinder
	r.Builtin(func() { p = newPicker(r) })
	return p
}

func newPicker(r *Runtime) *PathFinder {
	p := &PathFinder{Runtime: r}
	r.Instance(pickerID, Kind("ochazuke.picker")).Component = p
	p.input = NewInput("picker-path", "", false)
	if err := r.AddInput(p.input, Parent(pickerID), Kind("ochazuke.prompt")); err != nil {
		panic(err)
	}
	// open is the picker registry's ochazuke.picker.open with no name
	// (picker_registry.go): one verb opens a named picker or this finder.
	// The finder's other verbs are the picker noun's shared ones
	// (picker_verbs.go: pick, cancel, move, into, up, enter, complete),
	// and its keys are rows of bindings/picker.json.
	r.setPickerFinder(func(c Context, args Arguments) (any, error) {
		result, err := p.open(c, args)
		if err != nil {
			p.Error = err.Error()
		}
		p.sync()
		return result, err
	})
	picker := r.Define("ochazuke.picker")
	// CLI and keyboard edits share the same dispatch path. Keep ancestor action
	// semantics and delegate the normal operation, then invalidate search once.
	for key, operation := range r.Operations {
		if operation.Noun != "ochazuke.input" {
			continue
		}
		picker.Action(key, func(r *Runtime, c Context, args Arguments) (any, error) {
			before := string(p.input.Text)
			run := operation.Run
			if action := r.ActionFor("app", operation.Key()); action != nil {
				run = action
			}
			result, err := run(r, c, args)
			if p.IsOpen() && before != string(p.input.Text) {
				p.pathChanged()
			}
			return result, err
		})
	}
	return p
}

func (p *PathFinder) open(c Context, args Arguments) (any, error) {
	if p.IsOpen() {
		return nil, errors.New("picker is already open")
	}
	origin := p.Runtime.Input(c.InputID)
	if origin == nil || origin == p.input {
		return nil, errors.New("picker needs an originating input")
	}
	filter := "any"
	if origin.Kind == "path" || origin.Kind == "paths" {
		filter = origin.PathFilter
		if filter == "" {
			filter = "any"
		}
	}
	filter = StringArg(args, "filter", filter)
	if filter != "any" && filter != "file" && filter != "dir" {
		return nil, errors.New("filter must be any, file or dir")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	start, end := origin.Cursor, origin.Cursor
	path := ""
	switch origin.Kind {
	case "path":
		start, end = 0, len(origin.Text)
		path = string(origin.Text)
	case "paths":
		start, end = origin.lineBounds(origin.Cursor)
		path = string(origin.Text[start:end])
	}
	path = StringArg(args, "path", path)
	if path == "" {
		path = cwd + string(filepath.Separator)
	}
	if strings.ContainsAny(path, "\r\n") {
		return nil, errors.New("picker path input cannot contain newlines")
	}
	path, err = pickerAbsolutePath(cwd, path)
	if err != nil {
		return nil, err
	}
	p.stop()
	p.match = "" // each open starts in chooser.match
	p.Origin, p.originInput = c.Clone(), origin
	p.originText, p.originKind = string(origin.Text), origin.Kind
	p.start, p.end, p.Filter, p.base = start, end, filter, cwd
	// A new modal session gets fresh editor history while retaining its registered ID.
	inst := p.Runtime.Instances[p.input.ID]
	p.input = NewInput(p.input.ID, path, false)
	p.input.pin, p.input.r = "insert", p.Runtime
	inst.Component = p.input
	p.input.SetMode("insert")
	p.input.Cursor = len(p.input.Text)
	if err := p.Runtime.Push(pickerID, p.input.ID); err != nil {
		return nil, err
	}
	if err := p.Runtime.SetModalClose(pickerID, Request{Noun: "ochazuke.picker", Verb: "cancel"}); err != nil {
		return nil, err
	}
	p.Directory = ""
	p.pathChanged()
	return p.state(), nil
}

func (p *PathFinder) IsOpen() bool { return slices.Contains(p.Runtime.modals, pickerID) }

func (p *PathFinder) require() error {
	if p.Runtime.TopModal() != pickerID {
		return errors.New("picker is not the active modal")
	}
	return nil
}

func (p *PathFinder) selected() (PickerEntry, error) {
	if p.Selected < 0 || p.Selected >= len(p.Entries) {
		if p.Error != "" {
			return PickerEntry{}, errors.New(p.Error)
		}
		return PickerEntry{}, errors.New("no matching path selected")
	}
	return p.Entries[p.Selected], nil
}

func (p *PathFinder) enter(directoryOnly bool) (any, error) {
	if err := p.require(); err != nil {
		return nil, err
	}
	entry, err := p.selected()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(p.Directory, filepath.FromSlash(entry.Path))
	if entry.Dir {
		return p.browse(path)
	}
	if directoryOnly {
		return nil, errors.New("highlight a directory to enter it")
	}
	return p.choose(path)
}

func (p *PathFinder) browse(path string) (any, error) {
	if _, err := p.Runtime.Execute(Request{Verb: "replace", Noun: "ochazuke.input", Target: p.input.ID, Args: Arguments{"start": 0, "end": len(p.input.Text), "text": path + string(filepath.Separator)}}); err != nil {
		return nil, err
	}
	return p.state(), nil
}

func (p *PathFinder) choose(path string) (any, error) {
	if p.Filter == "file" && path == p.Directory {
		return nil, errors.New("file filter cannot choose the current directory")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if p.Filter == "file" && info.IsDir() || p.Filter == "dir" && !info.IsDir() {
		return nil, fmt.Errorf("selected path does not satisfy %s filter", p.Filter)
	}
	if p.originKind == "paths" && strings.ContainsAny(path, "\r\n") {
		return nil, errors.New("paths fields cannot contain a filename with a newline")
	}
	origin := p.Runtime.Input(p.Origin.InputID)
	if origin != p.originInput || string(origin.Text) != p.originText || origin.Kind != p.originKind {
		return nil, errors.New("originating input changed while picker was open; cancel and reopen to choose safely")
	}
	if _, err := p.Runtime.ExecuteAt(p.Origin, Request{Verb: "replace", Noun: "ochazuke.input", Args: Arguments{"start": p.start, "end": p.end, "text": path}}); err != nil {
		return nil, err
	}
	p.stop()
	if err := p.Runtime.Pop(); err != nil {
		return nil, err
	}
	return path, nil
}

func pickerAbsolutePath(base, value string) (string, error) {
	path := value
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	if strings.HasSuffix(value, string(filepath.Separator)) && !strings.HasSuffix(path, string(filepath.Separator)) {
		path += string(filepath.Separator)
	}
	return path, nil
}

// pickerLocation uses the longest existing directory prefix as the root and the
// remaining relative full path as the fuzzy query. It never trims path spaces.
func pickerLocation(base, value string) (string, string, error) {
	path, err := pickerAbsolutePath(base, value)
	if err != nil {
		return "", "", err
	}
	path = filepath.Clean(path)
	root := path
	for {
		info, err := os.Stat(root)
		if err == nil && info.IsDir() {
			query, err := filepath.Rel(root, path)
			if err != nil {
				return "", "", err
			}
			if query == "." {
				query = ""
			}
			canonical, err := filepath.EvalSymlinks(root)
			return canonical, query, err
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return "", "", fmt.Errorf("open search root %q: %w", root, err)
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", "", fmt.Errorf("no accessible search root for %q", value)
		}
		root = parent
	}
}

// issue starts a replacing job in one of the picker's slots and returns its
// id and context; the older job in the slot, if any, is cancelled.
func (p *PathFinder) issue(slot string) (uint64, context.Context) {
	ticket, ctx, err := p.Runtime.Issue(pickerID, slot, slot, Ctx{})
	if err != nil {
		panic(err)
	}
	return ticket.Job, ctx
}

// cancel drops the running job in one of the picker's slots.
func (p *PathFinder) cancel(slot string) {
	if j := p.Runtime.slotJob(pickerID, slot); j != nil {
		p.Runtime.finish(j, JobCancelled, nil)
	}
}

// accept settles a result for one of the picker's slots and reports whether
// it was the slot's current job; a stale result is dropped.
func (p *PathFinder) accept(slot string, id uint64, err error) bool {
	return p.IsOpen() && p.Runtime.Settle(Ticket{Owner: pickerID, Job: id, Slot: slot}, err)
}

func (p *PathFinder) pathChanged() {
	p.Entries, p.Selected = nil, 0
	if !p.indexFailed {
		p.Error = ""
	}
	p.cancel("search")
	root, query, err := pickerLocation(p.base, string(p.input.Text))
	if err != nil {
		p.Error, p.Loading = err.Error(), false
		p.cancel("index")
		p.indexReady = false
		p.indexRunning, p.fullIndex = false, nil
		p.Directory = ""
		p.sync()
		return
	}
	p.query, p.Loading = query, true
	if root != p.Directory {
		p.Directory, p.indexed, p.indexReady = root, nil, false
		p.indexFailed = false
		p.Error, p.indexRunning = "", true
		p.fullIndex = func() { p.queueIndex(root, false) }
		p.queueIndex(root, true)
	} else if p.indexReady {
		p.queueSearch()
	} else {
		p.Loading = p.indexRunning
	}
	p.sync()
}

// queueIndex starts an index of root in the index slot: the shallow listing
// first, the recursive walk once shallow results have been published.
func (p *PathFinder) queueIndex(root string, shallow bool) {
	if shallow {
		p.resetStats()
	}
	id, ctx := p.issue("index")
	p.indexID = id
	p.pending = append(p.pending, func() tea.Msg {
		var entries []PickerEntry
		var err error
		if shallow {
			entries, err = indexPickerEntries(ctx, root, false)
		} else {
			entries, err = indexPicker(ctx, root)
		}
		return pickerIndexMsg{picker: p, id: id, shallow: shallow, entries: entries, err: err}
	})
}

func (p *PathFinder) queueSearch() {
	id, ctx := p.issue("search")
	p.searchID = id
	entries, query, filter := p.indexed, p.query, p.Filter
	m := p.matcher()
	p.Loading = true
	p.pending = append(p.pending, func() tea.Msg {
		results, err := searchPickerMatch(ctx, entries, m, filter)
		if err == nil && query == "" && filter != "file" {
			results = append([]PickerEntry{{Path: ".", Dir: true}}, results...)
		}
		return pickerSearchMsg{picker: p, id: id, entries: results, err: err}
	})
}

// MatchMode is the picker's match mode: the one search.cycle-match or
// search.set-match chose while it is open, else the chooser.match
// setting. It decides how the query (the path after its directory)
// picks entries.
func (p *PathFinder) MatchMode() string {
	if p.match != "" {
		return p.match
	}
	return p.Runtime.ChooserMatch()
}

// matcher is the query compiled in the picker's match mode.
func (p *PathFinder) matcher() Matcher {
	return p.Runtime.Matcher(p.MatchMode(), filepath.ToSlash(p.query))
}

// The matchSearcher contract (search_match.go).
func (p *PathFinder) matchMode() string  { return p.MatchMode() }
func (p *PathFinder) matchApplies() bool { return p.IsOpen() }
func (p *PathFinder) setMatchMode(mode string) {
	p.match = mode
	p.pathChanged()
}

// Close cancels indexing, searching and previews; the picker instance stays
// registered for the next open.
func (p *PathFinder) Close() { p.stop() }

// PendingCommand is drained by Runtime.PendingCommands after every event,
// including CallMsg. It schedules work, never waits on filesystem or search.
func (p *PathFinder) PendingCommand() tea.Cmd {
	if !p.IsOpen() {
		p.stop()
		return nil
	}
	p.queueStats()
	commands := p.pending
	p.pending = nil
	return tea.Batch(commands...)
}

// HandleMessage applies only the current job of each slot (index, search,
// preview) on the UI loop; the runtime's jobs list says which that is.
func (p *PathFinder) HandleMessage(msg tea.Msg) bool {
	switch result := msg.(type) {
	case pickerPreviewMsg:
		if result.picker != p {
			return false
		}
		if !p.accept("preview", result.id, nil) {
			return true
		}
		p.preview = result.preview
	case pickerStatMsg:
		if result.picker != p {
			return false
		}
		p.applyStats(result)
		return true
	case pickerIndexMsg:
		if result.picker != p {
			return false
		}
		if !p.accept("index", result.id, result.err) {
			return true
		}
		if result.shallow && p.indexReady {
			return true
		}
		if result.err != nil {
			p.Error = "Index incomplete: " + result.err.Error()
			p.indexFailed, p.indexRunning, p.fullIndex = true, false, nil
			// Keep the independently completed shallow index and any search
			// against it; a recursive failure must not erase usable paths.
			p.Loading = p.Runtime.slotJob(pickerID, "search") != nil
		} else {
			p.Error = ""
			p.indexed, p.indexReady = result.entries, true
			p.indexRunning = result.shallow
			p.queueSearch()
		}
	case pickerSearchMsg:
		if result.picker != p {
			return false
		}
		if !p.accept("search", result.id, result.err) {
			return true
		}
		p.Loading = p.indexRunning
		if result.err != nil {
			p.Error = result.err.Error()
		} else {
			if !p.indexFailed {
				p.Error = ""
			}
			var selected string
			if p.Selected >= 0 && p.Selected < len(p.Entries) {
				selected = p.Entries[p.Selected].Path
			}
			p.Entries, p.Selected = result.entries, 0
			for i, entry := range p.Entries {
				if entry.Path == selected {
					p.Selected = i
					break
				}
			}
			// Start recursion only after shallow results have been published.
			if p.fullIndex != nil {
				p.fullIndex()
				p.fullIndex = nil
			}
		}
	default:
		return false
	}
	p.sync()
	return true
}

// stop cancels the picker's jobs in every slot and drops the queued
// commands; a late result of a cancelled job is stale.
func (p *PathFinder) stop() {
	p.Runtime.cancelJobs(pickerID)
	p.preview = pickerPreview{}
	p.statting = nil
	p.pending = nil
	p.fullIndex, p.indexRunning = nil, false
	p.Loading = false
}

func (p *PathFinder) state() map[string]any {
	return map[string]any{"directory": p.Directory, "filter": p.Filter, "query": p.query, "selected": p.Selected, "entries": p.Entries, "loading": p.Loading, "error": p.Error, "input": p.input.ID}
}

func (p *PathFinder) sync() {
	if p.IsOpen() {
		p.queuePreview()
	}
}
