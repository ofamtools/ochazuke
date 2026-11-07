package ochazuke

import (
	"cmp"
	"container/list"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The find source is the path picker's fuzzy finder (it was the separate
// path finder modal, NewPicker, before v0.96.0). It indexes the folder it
// searches, recursively and with Git's ignore rules (picker_search.go),
// and ranks every path under it against the query in the picker's match
// mode: the query is a path relative to that folder, its longest existing
// folder the root the search runs under and the rest the fuzzy part
// ("src/ma" searches src for "ma"). The shallow listing shows first and
// the recursive one replaces it when it lands; the index runs once per
// root between keystrokes. Each row draws the path, its size and its
// modified time (statted off the loop for the first rows, cached), and
// the highlighted row is previewed (a file's first bytes, a folder's
// entries). A folder row opens (picker.into: the find searches it) and up
// goes to the parent; tab fills the highlighted path into the query; pick
// {current: true} takes the folder itself. picker.open with no name opens
// the path picker on it; the setting path.find off leaves it out of the
// other path pickers.

// PathSourceFind is the find source's name.
const PathSourceFind = "find"

// PathFindSetting offers the find source in every path picker (on), or
// only where it is asked for by name (picker.open with no name).
const PathFindSetting = "path.find"

// PathAny is a Parameter.Path that takes a file or a folder (picker.open
// with no name, filter any).
const PathAny = "any"

// findStatRows is how many of a load's first rows have their size and
// modified time statted (the rest draw blank cells).
const findStatRows = 256

// pickerStatLimit bounds the stat cache.
const pickerStatLimit = 4096

// pickerStat is one path's answer; ok is false when the stat failed.
type pickerStat struct {
	size     int64
	modified time.Time
	ok       bool
}

// pickerStats is the LRU of stat answers by absolute path. Loads run off
// the loop, so it has its own lock.
type pickerStats struct {
	mu    sync.Mutex
	order *list.List // front is the most recent; values are keys
	items map[string]*list.Element
	stats map[string]pickerStat
}

func newPickerStats() *pickerStats {
	return &pickerStats{order: list.New(), items: map[string]*list.Element{}, stats: map[string]pickerStat{}}
}

func (s *pickerStats) get(key string) (pickerStat, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[key]
	if !ok {
		return pickerStat{}, false
	}
	s.order.MoveToFront(e)
	return s.stats[key], true
}

func (s *pickerStats) put(key string, stat pickerStat) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.items[key]; ok {
		s.order.MoveToFront(e)
	} else {
		s.items[key] = s.order.PushFront(key)
	}
	s.stats[key] = stat
	for s.order.Len() > pickerStatLimit {
		old := s.order.Back()
		key := old.Value.(string)
		s.order.Remove(old)
		delete(s.items, key)
		delete(s.stats, key)
	}
}

func (s *pickerStats) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.order.Len()
}

// pickerSize is a byte count in at most six cells: 512, 1.5K, 34K, 2.0M.
func pickerSize(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10)
	}
	value := float64(n)
	for _, unit := range []string{"K", "M", "G", "T"} {
		value /= 1024
		if value < 1024 || unit == "T" {
			if value < 10 {
				return fmt.Sprintf("%.1f%s", value, unit)
			}
			return fmt.Sprintf("%.0f%s", value, unit)
		}
	}
	return ""
}

// findCells are a row's cells: the path (a folder with a trailing /), the
// size right-aligned in six cells (blank for a folder) and the modified
// time; size and modified are blank until the stat has landed.
func findCells(label string, dir bool, stat pickerStat) []string {
	size, modified := "", ""
	if stat.ok {
		if !dir {
			size = pickerSize(stat.size)
		}
		modified = stat.modified.Local().Format("2006-01-02 15:04")
	}
	return []string{label, fmt.Sprintf("%6s", size), modified}
}

// findIndex is one root's index: the shallow listing, then the recursive
// one, run once in the background however many loads wait on it.
type findIndex struct {
	root        string
	cancel      context.CancelFunc
	shallowDone chan struct{}
	fullDone    chan struct{}
	shallow     []PickerEntry
	full        []PickerEntry
	shallowErr  error
	fullErr     error
}

// done reports whether the recursive index finished (or failed).
func (x *findIndex) done() bool {
	select {
	case <-x.fullDone:
		return true
	default:
		return false
	}
}

// pathFind is one open's find source.
type pathFind struct {
	r        *Runtime
	filter   string // any, file or dir
	dir      string // the folder it searches
	resolved bool   // dir is canonical (its first load resolved it)
	root     string // the root of the last load (dir, or a folder under it the query names)
	stats    *pickerStats
	index    func(ctx context.Context, root string, recursive bool) ([]PickerEntry, error)
	stat     func(string) (fs.FileInfo, error)

	mu      sync.Mutex
	current *findIndex

	name string // the source's name (PathSourceFind)
	// Origin is the context picker.open opened it from (the finder
	// shim's field).
	Origin Context
}

// shows reports whether c shows this source (its current source is the
// one findSource made).
func (s *pathFind) shows(c *Chooser) bool {
	src := c.current()
	return src != nil && src.Close != nil && c.r.chooserMemory().find == s && src.Name == s.name
}

// indexError is why the current root's index is incomplete, "" when it
// is not (or still runs).
func (s *pathFind) indexError() string {
	s.mu.Lock()
	x := s.current
	s.mu.Unlock()
	if x == nil || x.root != s.root || !x.done() || x.fullErr == nil || x.shallowErr != nil {
		return ""
	}
	return "Index incomplete: " + x.fullErr.Error()
}

// findFilter is the find source's filter for a pick: its Parameter.Path
// (file, dir or any) names the filter searchPickerMatch takes.
func findFilter(pick PathPick) string { return cmp.Or(pick.Parameter.Path, PathFile) }

// findSource is the find source for pick, searching under its start
// folder; use takes a picked path.
func (r *Runtime) findSource(pick PathPick) ChooserSource {
	mem := r.chooserMemory()
	// Making it reads no file system: a path picker that never shows it
	// never touches its folder.
	s := &pathFind{r: r, filter: findFilter(pick), dir: pick.Start, root: pick.Start, stats: newPickerStats(), index: mem.findIndex, stat: mem.findStat}
	if s.index == nil {
		s.index = indexPickerEntries
	}
	if s.stat == nil {
		s.stat = os.Lstat
	}
	s.Origin, mem.findOrigin = mem.findOrigin, Context{}
	s.name = PathSourceFind
	mem.find = s
	return ChooserSource{
		Name:     s.name,
		Live:     true,
		Label:    s.label,
		Empty:    "no paths",
		LoadMode: s.load,
		Preview:  s.preview,
		Complete: s.complete,
		Current:  s.currentRow,
		Close:    s.stop,
		Open: func(row ChooserRow) (string, bool) {
			path, ok := row.Data.(string)
			if !ok || !row.Opens {
				return "", false
			}
			s.dir, s.root = path, path
			return "", true
		},
		Up: func(string) (string, bool) {
			parent := filepath.Dir(s.dir)
			if parent == s.dir {
				return "", false
			}
			s.dir, s.root = parent, parent
			return "", true
		},
	}
}

// label is the source's title: find and its folder, while the recursive
// index runs "indexing…", and when it failed why (the shallow rows stay).
func (s *pathFind) label() string {
	label := PathSourceFind + " " + displayPath(s.dir)
	s.mu.Lock()
	x := s.current
	s.mu.Unlock()
	if x != nil && x.root == s.root && !x.done() {
		return label + " · indexing…"
	}
	if why := s.indexError(); why != "" {
		return label + " · i" + strings.TrimPrefix(why, "I")
	}
	return label
}

// indexFor is root's index, started (and the previous root's cancelled)
// when root is a new one.
func (s *pathFind) indexFor(root string) *findIndex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil && s.current.root == root {
		return s.current
	}
	if s.current != nil {
		s.current.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	x := &findIndex{root: root, cancel: cancel, shallowDone: make(chan struct{}), fullDone: make(chan struct{})}
	s.current = x
	index := s.index
	go func() {
		defer cancel()
		x.shallow, x.shallowErr = index(ctx, root, false)
		close(x.shallowDone)
		if x.shallowErr != nil {
			x.fullErr = x.shallowErr
			close(x.fullDone)
			return
		}
		x.full, x.fullErr = index(ctx, root, true)
		close(x.fullDone)
	}()
	return x
}

// stop cancels the index (the picker closed).
func (s *pathFind) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		s.current.cancel()
		s.current = nil
	}
}

// load ranks the paths under the query's root in mode: the shallow
// listing's at once, the recursive index's when it lands.
func (s *pathFind) load(query, mode string) Result {
	if !s.resolved {
		// The folder as the index names it (a symlinked temp folder), so
		// rows are relative to it.
		if canonical, err := filepath.EvalSymlinks(s.dir); err == nil {
			s.dir = canonical
		}
		s.resolved = true
	}
	root, rest, err := pickerLocation(s.dir, query)
	if err != nil {
		return Failed(err)
	}
	s.root = root
	m := s.r.Matcher(mode, filepath.ToSlash(rest))
	blank := rest == ""
	x := s.indexFor(root)
	rows := func(ctx context.Context, entries []PickerEntry) ([]ChooserRow, error) {
		results, err := searchPickerMatch(ctx, entries, m, s.filter)
		if err != nil {
			return nil, err
		}
		if blank && s.filter != "file" {
			results = append([]PickerEntry{{Path: ".", Dir: true}}, results...)
		}
		return s.rows(ctx, root, results), nil
	}
	label := "find " + displayPath(root)
	full := func(ctx context.Context) (any, error) {
		select {
		case <-x.fullDone:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		entries := x.full
		if x.fullErr != nil {
			// A failed recursive index keeps the shallow rows: the label
			// says it is incomplete.
			entries = x.shallow
		}
		out, err := rows(ctx, entries)
		if err != nil {
			return nil, err
		}
		return ChooserBatch{Rows: out, Replace: true}, nil
	}
	return Pending(func(ctx context.Context) (any, error) {
		select {
		case <-x.shallowDone:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if x.shallowErr != nil {
			return nil, x.shallowErr
		}
		if x.done() {
			return full(ctx)
		}
		out, err := rows(ctx, x.shallow)
		if err != nil {
			return nil, err
		}
		return ChooserBatch{Rows: out, More: Pending(full).Label(label)}, nil
	}).Label(label)
}

// rows are the results as rows under root, the first findStatRows statted
// (from the cache, else now: this runs off the loop).
func (s *pathFind) rows(ctx context.Context, root string, results []PickerEntry) []ChooserRow {
	out := make([]ChooserRow, 0, len(results))
	for i, entry := range results {
		path := filepath.Join(root, filepath.FromSlash(entry.Path))
		label := s.labelOf(path)
		if entry.Path == "." {
			label = ". (current directory)"
		} else if entry.Dir {
			label += "/"
		}
		label = safeText(strings.ReplaceAll(label, "\n", "\\n"))
		var stat pickerStat
		if i < findStatRows && ctx.Err() == nil {
			var ok bool
			if stat, ok = s.stats.get(path); !ok {
				if info, err := s.stat(path); err == nil {
					stat = pickerStat{size: info.Size(), modified: info.ModTime(), ok: true}
				}
				s.stats.put(path, stat)
			}
		}
		row := ChooserRow{ID: path, Label: label, Data: path, Cells: findCells(label, entry.Dir, stat)}
		if entry.Dir && entry.Path != "." {
			row.Opens, row.NoPick = true, s.filter == "file"
		}
		if entry.Path == "." && s.filter == "file" {
			row.Refuse = "a file pick cannot take the folder"
		}
		out = append(out, row)
	}
	return out
}

// labelOf is path as a row shows it: relative to the folder the find
// searches, else whole (with ~ for the home folder).
func (s *pathFind) labelOf(path string) string {
	if rel, err := filepath.Rel(s.dir, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return displayPath(path)
}

// complete is the query that names row: its path (a folder's with a
// trailing /, so the find lists it).
func (s *pathFind) complete(row ChooserRow) (string, bool) {
	path, ok := row.Data.(string)
	if !ok || path == "" {
		return "", false
	}
	query := s.labelOf(path)
	if query == "." {
		query = ""
	}
	if row.Opens && query != "" {
		query += "/"
	}
	return query, true
}

// currentRow is the folder the find searches (the query's root).
func (s *pathFind) currentRow() (ChooserRow, bool) {
	row := ChooserRow{ID: s.root, Label: displayPath(s.root), Data: s.root}
	if s.filter == "file" {
		row.Refuse = "a file pick cannot take the folder"
	}
	return row, true
}

// preview reads the highlighted path off the loop: its name, what it is,
// then a file's first bytes or a folder's entries (readPickerPreview).
func (s *pathFind) preview(row ChooserRow) Result {
	path, ok := row.Data.(string)
	if !ok || path == "" {
		return Done("")
	}
	return Pending(func(ctx context.Context) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		metadata, text, diagnostic := readPickerPreview(path)
		lines := []string{filepath.Base(path)}
		if metadata != "" {
			lines = append(lines, metadata)
		}
		if diagnostic != "" {
			lines = append(lines, "Preview: "+diagnostic)
		} else {
			lines = append(lines, text)
		}
		return safeText(strings.Join(lines, "\n")), nil
	}).Label("preview " + displayPath(path))
}

// openFindPicker is picker.open with no name: the path picker on its find
// source for the input the focus is on (findOrigin). The path argument
// names where to start (its longest existing folder is searched, the rest
// typed as the query), query the text typed, filter any, file or dir.
// With no input in focus the pick is the verb's answer.
func (r *Runtime) openFindPicker(c Context, args Arguments) (any, error) {
	origin := r.findOrigin(c)
	filter := StringArg(args, "filter", origin.filter)
	if filter != "any" && filter != "file" && filter != "dir" {
		return nil, errors.New("filter must be any, file or dir")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	path := StringArg(args, "path", origin.path)
	if path == "" {
		path = cwd + string(filepath.Separator)
	}
	if strings.ContainsAny(path, "\r\n") {
		return nil, errors.New("picker path cannot contain newlines")
	}
	root, query, err := pickerLocation(cwd, path)
	if err != nil {
		return nil, err
	}
	if q, ok := args["query"].(string); ok && q != "" {
		query = q
	}
	// The filter names the pick's Parameter.Path (file, dir or any).
	r.chooserMemory().findOrigin = c.Clone()
	chooser, err := r.OpenPathChooser(PathChooserSpec{
		Pick:   PathPick{Name: "path", Parameter: Parameter{Type: PathParameterType, Path: filter}, Value: root + string(filepath.Separator)},
		Source: PathSourceFind,
		Query:  filepath.ToSlash(query),
		Choose: origin.choose(r),
	})
	if err != nil {
		return nil, err
	}
	return chooser.state(), nil
}

// findField is the input picker.open with no name fills: where a pick
// goes (start to end), the path it opens at and the filter it takes.
type findField struct {
	input      *Input
	at         Context
	text, kind string
	start, end int
	path       string
	filter     string
}

// findOrigin is the field in focus: a path field opens at its value and
// takes its filter, the pick replacing it whole; a path set at the line
// the cursor is on, the pick replacing that line; any other input at the
// working folder, the pick inserted at the cursor. A picker's own search
// line is no field.
func (r *Runtime) findOrigin(c Context) findField {
	f := findField{filter: PathAny}
	in := r.Input(c.InputID)
	if in == nil || r.chooserAt(r.Instances[in.ID]) != nil {
		return f
	}
	f.input, f.at, f.text, f.kind = in, c.Clone(), string(in.Text), in.Kind
	f.start, f.end = in.Cursor, in.Cursor
	switch in.Kind {
	case "path":
		f.start, f.end = 0, len(in.Text)
		f.path, f.filter = string(in.Text), cmp.Or(in.PathFilter, PathAny)
	case "paths":
		f.start, f.end = in.lineBounds(in.Cursor)
		f.path, f.filter = string(in.Text[f.start:f.end]), cmp.Or(in.PathFilter, PathAny)
	}
	return f
}

// choose puts a picked path in the field, unless the field changed while
// the picker was open or a path set would take a name with a newline;
// with no field it is nil (the pick is the answer).
func (f findField) choose(r *Runtime) func(string) Result {
	if f.input == nil {
		return nil
	}
	return func(path string) Result {
		if f.kind == "paths" && strings.ContainsAny(path, "\r\n") {
			return Failed(errors.New("paths fields cannot contain a filename with a newline"))
		}
		now := r.Input(f.at.InputID)
		if now != f.input || string(now.Text) != f.text || now.Kind != f.kind {
			return Failed(errors.New("originating input changed while the picker was open; open it again to choose safely"))
		}
		if _, err := r.ExecuteAt(f.at, Request{Verb: "replace", Noun: "ochazuke.input", Args: Arguments{"start": f.start, "end": f.end, "text": path}}); err != nil {
			return Failed(err)
		}
		return Done(path)
	}
}
