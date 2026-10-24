package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
)

// pickerID is the picker's instance, the owner of its index, search and
// preview jobs.
const pickerID = "picker"

// Picker owns one modal and one ordinary editor. Background commands only read
// immutable snapshots; HandleMessage is the sole result-application boundary.
// Its work runs as three replacing jobs on the picker instance (the "index",
// "search" and "preview" slots), so the runtime's jobs list decides which
// result is current: a newer job in a slot drops the older, and Close
// cancels them all.
type Picker struct {
	Runtime      *Runtime
	Origin       Context
	Directory    string
	Filter       string
	Entries      []PickerEntry
	Selected     int
	Error        string
	Loading      bool
	frame        *Frame
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
}

type pickerIndexMsg struct {
	picker  *Picker
	id      uint64
	shallow bool
	entries []PickerEntry
	err     error
}

type pickerSearchMsg struct {
	picker  *Picker
	id      uint64
	entries []PickerEntry
	err     error
}

func NewPicker(r *Runtime) *Picker {
	p := &Picker{Runtime: r}
	r.Instance(pickerID, Kind("picker")).Component = p
	p.input = NewInput("picker-path", "", false)
	if err := r.AddInput(p.input, Parent(pickerID), Kind("prompt")); err != nil {
		panic(err)
	}
	register := func(verb, description string, params map[string]Parameter, fn func(Context, Arguments) (any, error)) {
		r.Register(Operation{Verb: verb, Noun: "picker", Description: description, Parameters: params, Result: "picker state", Run: func(_ *Runtime, c Context, args Arguments) (any, error) {
			result, err := fn(c, args)
			if err != nil {
				p.Error = err.Error()
			}
			p.sync()
			return result, err
		}})
	}
	register("open", "Open the shared fuzzy path picker in insert mode", map[string]Parameter{
		"filter": {Type: "string", Description: "any, file or dir; defaults to the originating path field's filter"},
		"path":   {Type: "string", Description: "Initial path/query; otherwise path value, current paths line, or cwd"},
	}, p.open)
	register("move", "Move the highlighted result without editing the path", map[string]Parameter{"delta": {Type: "integer", Required: true}}, func(_ Context, args Arguments) (any, error) {
		if err := p.require(); err != nil {
			return nil, err
		}
		p.Selected = min(max(0, p.Selected+IntArg(args, "delta", 0)), max(0, len(p.Entries)-1))
		return p.state(), nil
	})
	register("parent", "Browse the parent directory", nil, func(_ Context, _ Arguments) (any, error) {
		if err := p.require(); err != nil {
			return nil, err
		}
		return p.browse(filepath.Dir(p.Directory))
	})
	register("descend", "Enter the highlighted directory without selecting files", nil, func(_ Context, _ Arguments) (any, error) {
		return p.enter(true)
	})
	register("enter", "Enter the highlighted directory or select a file", nil, func(_ Context, _ Arguments) (any, error) {
		return p.enter(false)
	})
	register("complete", "Complete the highlighted path without confirming it", nil, func(_ Context, _ Arguments) (any, error) {
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
		if _, err := p.Runtime.Execute(Request{Verb: "replace", Noun: "input", Target: p.input.ID, Args: Arguments{"start": 0, "end": len(p.input.Text), "text": path}}); err != nil {
			return nil, err
		}
		return p.state(), nil
	})
	register("choose", "Confirm highlighted path, or explicitly choose current root", map[string]Parameter{"current": {Type: "boolean", Description: "Choose current root instead of the highlight (any/dir only)"}}, func(_ Context, args Arguments) (any, error) {
		if err := p.require(); err != nil {
			return nil, err
		}
		if current, _ := args["current"].(bool); current {
			return p.choose(p.Directory)
		}
		entry, err := p.selected()
		if err != nil {
			return nil, err
		}
		return p.choose(filepath.Join(p.Directory, filepath.FromSlash(entry.Path)))
	})
	register("close", "Cancel the picker without changing its originating input", nil, func(_ Context, _ Arguments) (any, error) {
		if err := p.require(); err != nil {
			return nil, err
		}
		p.stop()
		if err := r.Pop(); err != nil {
			return nil, err
		}
		return r.Context(), nil
	})

	picker := r.Define("picker")
	picker.Mode("insert").
		Bind("ctrl+n", Do("move.picker", Arguments{"delta": 1})).
		Bind("ctrl+p", Do("move.picker", Arguments{"delta": -1})).
		Bind("alt+up", Do("parent.picker")).
		Bind("tab", Do("complete.picker")).
		Bind("enter", Do("enter.picker")).
		Bind("ctrl+s", Do("choose.picker")).
		Bind("esc", Do("close.picker"))
	// CLI and keyboard edits share the same dispatch path. Keep ancestor action
	// semantics and delegate the normal operation, then invalidate search once.
	for key, operation := range r.Operations {
		if operation.Noun != "input" {
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

func (p *Picker) open(c Context, args Arguments) (any, error) {
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
	p.Origin, p.originInput = c.Clone(), origin
	p.originText, p.originKind = string(origin.Text), origin.Kind
	p.start, p.end, p.Filter, p.base = start, end, filter, cwd
	// A new modal session gets fresh editor history while retaining its registered ID.
	inst := p.Runtime.Instances[p.input.ID]
	p.input = NewInput(p.input.ID, path, false)
	p.input.pin, p.input.inst = "insert", inst
	inst.Component = p.input
	p.input.SetMode("insert")
	inst.State = "insert"
	p.input.Cursor = len(p.input.Text)
	p.frame = &Frame{ID: "picker", Focus: p.input.ID}
	if err := p.Runtime.Push(p.frame); err != nil {
		return nil, err
	}
	p.Directory = ""
	p.pathChanged()
	return p.state(), nil
}

func (p *Picker) IsOpen() bool {
	if p.frame == nil {
		return false
	}
	for _, frame := range p.Runtime.Frames {
		if frame == p.frame {
			return true
		}
	}
	return false
}

func (p *Picker) require() error {
	if !p.IsOpen() || p.Runtime.top() != p.frame {
		return errors.New("picker is not the active modal")
	}
	return nil
}

func (p *Picker) selected() (PickerEntry, error) {
	if p.Selected < 0 || p.Selected >= len(p.Entries) {
		if p.Error != "" {
			return PickerEntry{}, errors.New(p.Error)
		}
		return PickerEntry{}, errors.New("no matching path selected")
	}
	return p.Entries[p.Selected], nil
}

func (p *Picker) enter(directoryOnly bool) (any, error) {
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

func (p *Picker) browse(path string) (any, error) {
	if _, err := p.Runtime.Execute(Request{Verb: "replace", Noun: "input", Target: p.input.ID, Args: Arguments{"start": 0, "end": len(p.input.Text), "text": path + string(filepath.Separator)}}); err != nil {
		return nil, err
	}
	return p.state(), nil
}

func (p *Picker) choose(path string) (any, error) {
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
	if _, err := p.Runtime.ExecuteAt(p.Origin, Request{Verb: "replace", Noun: "input", Args: Arguments{"start": p.start, "end": p.end, "text": path}}); err != nil {
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
func (p *Picker) issue(slot string) (uint64, context.Context) {
	ticket, ctx, err := p.Runtime.Issue(pickerID, slot, slot, Ctx{})
	if err != nil {
		panic(err)
	}
	return ticket.Job, ctx
}

// cancel drops the running job in one of the picker's slots.
func (p *Picker) cancel(slot string) {
	if j := p.Runtime.slotJob(pickerID, slot); j != nil {
		p.Runtime.finish(j, JobCancelled, nil)
	}
}

// accept settles a result for one of the picker's slots and reports whether
// it was the slot's current job; a stale result is dropped.
func (p *Picker) accept(slot string, id uint64, err error) bool {
	return p.IsOpen() && p.Runtime.Settle(Ticket{Owner: pickerID, Job: id, Slot: slot}, err)
}

func (p *Picker) pathChanged() {
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
func (p *Picker) queueIndex(root string, shallow bool) {
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

func (p *Picker) queueSearch() {
	id, ctx := p.issue("search")
	p.searchID = id
	entries, query, filter := p.indexed, p.query, p.Filter
	p.Loading = true
	p.pending = append(p.pending, func() tea.Msg {
		results, err := searchPicker(ctx, entries, query, filter)
		if err == nil && query == "" && filter != "file" {
			results = append([]PickerEntry{{Path: ".", Dir: true}}, results...)
		}
		return pickerSearchMsg{picker: p, id: id, entries: results, err: err}
	})
}

// Close cancels indexing, searching and previews; the picker instance stays
// registered for the next open.
func (p *Picker) Close() { p.stop() }

// PendingCommand is drained by Runtime.PendingCommands after every event,
// including CallMsg. It schedules work, never waits on filesystem or search.
func (p *Picker) PendingCommand() tea.Cmd {
	if !p.IsOpen() {
		p.stop()
		return nil
	}
	commands := p.pending
	p.pending = nil
	return tea.Batch(commands...)
}

// HandleMessage applies only the current job of each slot (index, search,
// preview) on the UI loop; the runtime's jobs list says which that is.
func (p *Picker) HandleMessage(msg tea.Msg) bool {
	switch result := msg.(type) {
	case pickerPreviewMsg:
		if result.picker != p {
			return false
		}
		if !p.accept("preview", result.id, nil) {
			return true
		}
		p.preview = result.preview
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
func (p *Picker) stop() {
	p.Runtime.cancelJobs(pickerID)
	p.preview = pickerPreview{}
	p.pending = nil
	p.fullIndex, p.indexRunning = nil, false
	p.Loading = false
}

func (p *Picker) state() map[string]any {
	return map[string]any{"directory": p.Directory, "filter": p.Filter, "query": p.query, "selected": p.Selected, "entries": p.Entries, "loading": p.Loading, "error": p.Error, "input": p.input.ID}
}

func (p *Picker) sync() {
	if p.IsOpen() {
		p.queuePreview()
	}
}
