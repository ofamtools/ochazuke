package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Picker owns one modal and one ordinary editor. Background commands only read
// immutable snapshots; HandleMessage is the sole result-application boundary.
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
	indexID      uint64
	searchID     uint64
	indexFailed  bool
	cancelIndex  context.CancelFunc
	cancelSearch context.CancelFunc
	pending      []tea.Cmd
}

type pickerIndexMsg struct {
	picker  *Picker
	id      uint64
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
	layer := &Layer{ID: "picker", Noun: "picker", Actions: map[string]Handler{}}
	r.Layers["picker"] = layer
	p.input = NewInput("picker-path", "", false)
	if err := r.AddInput(p.input, []string{"app", "picker"}); err != nil {
		panic(err)
	}
	for _, binding := range []struct {
		mode, key, verb string
		args            Arguments
	}{
		{"normal", "j", "move", Arguments{"delta": 1}}, {"normal", "down", "move", Arguments{"delta": 1}},
		{"normal", "k", "move", Arguments{"delta": -1}}, {"normal", "up", "move", Arguments{"delta": -1}},
		{"normal", "h", "parent", nil}, {"normal", "left", "parent", nil},
		{"normal", "l", "descend", nil}, {"normal", "right", "descend", nil},
		{"insert", "ctrl+n", "move", Arguments{"delta": 1}}, {"insert", "ctrl+p", "move", Arguments{"delta": -1}},
		{"", "tab", "complete", nil},
		{"", "enter", "enter", nil}, {"", "ctrl+s", "choose", nil}, {"normal", "esc", "close", nil},
	} {
		layer.Bindings = append(layer.Bindings, Binding{Mode: binding.mode, Keys: binding.key, Request: Request{Verb: binding.verb, Noun: "picker", Args: binding.args}})
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
		if _, err := p.Runtime.ExecuteAt(p.frame.Context, Request{Verb: "replace", Noun: "input", Args: Arguments{"start": 0, "end": len(p.input.Text), "text": path}}); err != nil {
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

	// CLI and keyboard edits share the same dispatch path. Keep ancestor action
	// semantics and delegate the normal operation, then invalidate search once.
	for key, operation := range r.Operations {
		if operation.Noun != "input" {
			continue
		}
		layer.Actions[key] = func(r *Runtime, c Context, args Arguments) (any, error) {
			before := string(p.input.Text)
			run := operation.Run
			for i := len(c.Path) - 2; i >= 0; i-- {
				if action := r.Layers[c.Path[i]].Actions[operation.Key()]; action != nil {
					run = action
					break
				}
			}
			result, err := run(r, c, args)
			if p.IsOpen() && before != string(p.input.Text) {
				p.pathChanged()
			}
			return result, err
		}
	}
	return p
}

func (p *Picker) open(c Context, args Arguments) (any, error) {
	if p.IsOpen() {
		return nil, errors.New("picker is already open")
	}
	origin := p.Runtime.Inputs[c.InputID]
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
	p.stop()
	p.Origin, p.originInput = c.Clone(), origin
	p.originText, p.originKind = string(origin.Text), origin.Kind
	p.start, p.end, p.Filter, p.base = start, end, filter, cwd
	// A new modal session gets fresh editor history while retaining its registered ID.
	p.input = NewInput(p.input.ID, path, false)
	p.Runtime.Inputs[p.input.ID] = p.input
	p.input.SetMode("insert")
	p.input.Cursor = len(p.input.Text)
	p.frame = &Frame{ID: "picker", Context: Context{Path: []string{"app", "picker"}, InputID: p.input.ID}, State: map[string]any{}}
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
	if !p.IsOpen() || p.Runtime.Frames[len(p.Runtime.Frames)-1] != p.frame {
		return errors.New("picker is not the active modal")
	}
	return nil
}

func (p *Picker) selected() (PickerEntry, error) {
	if p.Loading {
		return PickerEntry{}, errors.New("picker search is still running")
	}
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
	if _, err := p.Runtime.ExecuteAt(p.frame.Context, Request{Verb: "replace", Noun: "input", Args: Arguments{"start": 0, "end": len(p.input.Text), "text": path + string(filepath.Separator)}}); err != nil {
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
	origin := p.Runtime.Inputs[p.Origin.InputID]
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

// pickerLocation uses the longest existing directory prefix as the root and the
// remaining relative full path as the fuzzy query. It never trims path spaces.
func pickerLocation(base, value string) (string, string, error) {
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
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

func (p *Picker) pathChanged() {
	p.Error, p.Entries, p.Selected = "", nil, 0
	if p.cancelSearch != nil {
		p.cancelSearch()
	}
	p.searchID++
	root, query, err := pickerLocation(p.base, string(p.input.Text))
	if err != nil {
		p.Error, p.Loading = err.Error(), false
		if p.cancelIndex != nil {
			p.cancelIndex()
		}
		p.indexID++
		p.indexReady = false
		p.Directory = ""
		p.sync()
		return
	}
	p.query, p.Loading = query, true
	if root != p.Directory || p.indexFailed {
		if p.cancelIndex != nil {
			p.cancelIndex()
		}
		p.indexID++
		p.Directory, p.indexed, p.indexReady = root, nil, false
		p.indexFailed = false
		ctx, cancel := context.WithCancel(context.Background())
		p.cancelIndex = cancel
		id := p.indexID
		p.pending = append(p.pending, func() tea.Msg {
			entries, err := indexPicker(ctx, root)
			return pickerIndexMsg{picker: p, id: id, entries: entries, err: err}
		})
	} else if p.indexReady {
		p.queueSearch()
	}
	p.sync()
}

func (p *Picker) queueSearch() {
	if p.cancelSearch != nil {
		p.cancelSearch()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancelSearch = cancel
	p.searchID++
	id, entries, query, filter := p.searchID, p.indexed, p.query, p.Filter
	p.Loading = true
	p.pending = append(p.pending, func() tea.Msg {
		results, err := searchPicker(ctx, entries, query, filter)
		if err == nil && query == "" && filter != "file" {
			results = append([]PickerEntry{{Path: ".", Dir: true}}, results...)
		}
		return pickerSearchMsg{picker: p, id: id, entries: results, err: err}
	})
}

// PendingCommand must be drained by the Bubble Tea adapter after every event,
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

// HandleMessage applies only this picker's current generation on the UI loop.
func (p *Picker) HandleMessage(msg tea.Msg) bool {
	switch result := msg.(type) {
	case pickerIndexMsg:
		if result.picker != p {
			return false
		}
		if !p.IsOpen() || result.id != p.indexID {
			return true
		}
		if result.err != nil {
			p.Error, p.Loading = "Index incomplete: "+result.err.Error(), false
			p.indexFailed = true
		} else {
			p.Error = ""
			p.indexed, p.indexReady = result.entries, true
			p.queueSearch()
		}
	case pickerSearchMsg:
		if result.picker != p {
			return false
		}
		if !p.IsOpen() || result.id != p.searchID {
			return true
		}
		p.Loading = false
		if result.err != nil {
			p.Error = result.err.Error()
		} else {
			p.Error = ""
			p.Entries, p.Selected = result.entries, 0
		}
	default:
		return false
	}
	p.sync()
	return true
}

func (p *Picker) stop() {
	if p.cancelIndex != nil {
		p.cancelIndex()
	}
	if p.cancelSearch != nil {
		p.cancelSearch()
	}
	p.indexID++
	p.searchID++
	p.pending = nil
	p.Loading = false
}

func (p *Picker) state() map[string]any {
	return map[string]any{"directory": p.Directory, "filter": p.Filter, "query": p.query, "selected": p.Selected, "entries": p.Entries, "loading": p.Loading, "error": p.Error, "input": p.input.ID}
}

func (p *Picker) sync() {
	if p.IsOpen() {
		p.frame.State = p.state()
	}
}

func (p *Picker) View(width, height int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Path [%s]: %s\n", p.Filter, InputView(p.input, max(1, width-13), 1))
	b.WriteString("Enter open/select · Ctrl-S confirm · Esc normal/cancel\n")
	b.WriteString("Ctrl-N/P results · normal hjkl/←↓↑→ browse\n")
	if p.Loading {
		b.WriteString("Indexing/searching…\n")
	}
	if p.Error != "" {
		fmt.Fprintf(&b, "Error: %s\n", safeText(p.Error))
	}
	if !p.Loading && p.Error == "" && len(p.Entries) == 0 {
		b.WriteString("(no matching paths)\n")
	}
	limit := max(1, height-4)
	start := max(0, p.Selected-limit+1)
	end := min(len(p.Entries), start+limit)
	for i := start; i < end; i++ {
		mark, suffix := "  ", ""
		if i == p.Selected {
			mark = "> "
		}
		if p.Entries[i].Dir {
			suffix = "/"
		}
		name := strings.ReplaceAll(p.Entries[i].Path, "\n", "\\n")
		if name == "." {
			name = ". (current directory)"
		}
		fmt.Fprintf(&b, "%s\n", ansi.Truncate(mark+safeText(name)+suffix, max(1, width), "…"))
	}
	return b.String()
}
