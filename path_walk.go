package ochazuke

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The walk path source lists every folder (and, for a file pick, every
// file) under a set of roots an application gives, so a pick can reach any
// folder of a project by typing a fragment of its path ("mato cmd" finds
// ~/proj/matomeru/cmd/mtm). The walk runs off the loop in batches, breadth
// first, so the shallow folders show at once and the deep ones stream in
// (ChooserBatch); closing the chooser or loading again cancels it. It is
// bounded by the settings path.walk-depth, path.walk-limit,
// path.walk-ignore, path.walk-hidden and path.walk-include, never enters a
// folder under path.no-auto-list (an external volume), and keeps each
// root's listing for a short while (pathWalkTTL), so cycling back to it or
// reopening the chooser does not walk again. A row shows the path relative
// to its root and the root's label; the filter reads the whole path.
// path.walk off leaves the source out; path.first puts it first.

// PathSourceWalk is the walk source's name.
const PathSourceWalk = "walk"

// Walk settings: rows of settings/ochazuke.json.
const (
	PathWalkSetting        = "path.walk"
	PathWalkDepthSetting   = "path.walk-depth"
	PathWalkLimitSetting   = "path.walk-limit"
	PathWalkIgnoreSetting  = "path.walk-ignore"
	PathWalkHiddenSetting  = "path.walk-hidden"
	PathWalkIncludeSetting = "path.walk-include"
	// PathFirstSetting names the path chooser's sources listed first, in
	// order (the rest keep theirs); a chooser opened on no named source
	// and with none remembered opens on the first of them it has.
	PathFirstSetting = "path.first"
)

// pathWalkTTL is how long a root's finished walk is reused.
const pathWalkTTL = 30 * time.Second

// pathWalkBatch is the folders one batch reads by default; a batch also
// ends after pathWalkSlice, so rows land at least that often.
const (
	pathWalkBatch = 64
	pathWalkSlice = 50 * time.Millisecond
)

// PathRoot is one folder a walk lists under. Label names it in the rows;
// "" shows its path (with ~ for the home folder).
type PathRoot struct {
	Path  string
	Label string
}

// PathWalk is what one open walks: its roots in order (a root inside one
// walked before it is listed once, under the first), and Batch, the
// folders read per batch (0 is the default, 64).
type PathWalk struct {
	Roots []PathRoot
	Batch int
}

// AddWalkPathSource adds the walk source (PathSourceWalk) to every path
// chooser: walk gives the roots for one open. No roots, or the setting
// path.walk off, leaves the source out of that open.
func (r *Runtime) AddWalkPathSource(walk func(PathPick) PathWalk) {
	r.AddPathSource(PathSourceWalk, func(pick PathPick) (ChooserSource, bool) {
		if !r.PathWalkOn() {
			return ChooserSource{}, false
		}
		w := walk(pick)
		if len(w.Roots) == 0 {
			return ChooserSource{}, false
		}
		return r.WalkSource(pick, w), true
	})
}

// PathWalkOn reports the setting path.walk (on when not declared).
func (r *Runtime) PathWalkOn() bool { return r.settingOn(PathWalkSetting) }

// walkOptions are the settings a walk reads, taken on the loop when it
// starts.
type walkOptions struct {
	dirs    bool // the pick wants a folder: list folders only
	depth   int
	limit   int
	hidden  bool
	ignore  []string
	include []string
	noAuto  []string
}

// key identifies the options a cached walk was made with.
func (o walkOptions) key() string {
	return fmt.Sprintf("%t|%d|%d|%t|%q|%q|%q", o.dirs, o.depth, o.limit, o.hidden, o.ignore, o.include, o.noAuto)
}

func (r *Runtime) walkOptions(pick PathPick) walkOptions {
	o := walkOptions{
		dirs:    pick.Dir(),
		depth:   r.settingInt(PathWalkDepthSetting, 8),
		limit:   r.settingInt(PathWalkLimitSetting, 20000),
		hidden:  r.settingBool(PathWalkHiddenSetting),
		ignore:  r.settingList(PathWalkIgnoreSetting),
		include: r.settingList(PathWalkIncludeSetting),
		noAuto:  r.settingList(PathNoAutoListSetting),
	}
	if o.depth < 1 {
		o.depth = 1
	}
	if o.limit < 1 {
		o.limit = 1
	}
	return o
}

// settingInt is an integer setting's value, def when it is not one.
func (r *Runtime) settingInt(name string, def int) int {
	v, _ := r.settingValue(name)
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return def
}

func (r *Runtime) settingBool(name string) bool {
	v, _ := r.settingValue(name)
	return v == true
}

func (r *Runtime) settingList(name string) []string {
	v, _ := r.settingValue(name)
	switch l := v.(type) {
	case []string:
		return slices.Clone(l)
	case []any:
		out := make([]string, 0, len(l))
		for _, one := range l {
			if s, ok := one.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// walkEntry is one path a walk found, relative to its root.
type walkEntry struct {
	rel string
	dir bool
}

// walkCache keeps finished root walks for pathWalkTTL. Batches run off
// the loop, so it has its own lock.
type walkCache struct {
	mu sync.Mutex
	m  map[string]walkCached
}

type walkCached struct {
	entries []walkEntry
	at      time.Time
}

func (c *walkCache) get(key string, now time.Time) ([]walkEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	one, ok := c.m[key]
	if !ok || now.Sub(one.at) > pathWalkTTL {
		delete(c.m, key)
		return nil, false
	}
	return one.entries, true
}

func (c *walkCache) put(key string, entries []walkEntry, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]walkCached{}
	}
	for k, one := range c.m {
		if now.Sub(one.at) > pathWalkTTL {
			delete(c.m, k)
		}
	}
	c.m[key] = walkCached{entries: entries, at: now}
}

// walker is one open's walk, resumed batch by batch off the loop (one
// batch at a time: each runs after the last one landed).
type walker struct {
	opts  walkOptions
	roots []PathRoot
	batch int
	cache *walkCache
	now   func() time.Time

	ri      int         // the root being walked
	queue   []walkQueue // its folders still to read, breadth first
	entries []walkEntry // what it found so far (cached when it finishes)
	exclude []string    // the earlier roots inside it: not walked again
	started bool        // the current root was begun
	seen    map[string]bool

	found   atomic.Int64
	done    atomic.Bool
	limited atomic.Bool
}

type walkQueue struct {
	path  string
	depth int
}

// WalkSource is the walk source for pick over w's roots, for an
// application that adds it under its own name (AddPathSource);
// AddWalkPathSource adds it as walk.
func (r *Runtime) WalkSource(pick PathPick, w PathWalk) ChooserSource {
	mem := r.chooserMemory()
	if mem.walks == nil {
		mem.walks = &walkCache{}
	}
	cache := mem.walks
	roots := make([]PathRoot, 0, len(w.Roots))
	for _, root := range w.Roots {
		if strings.TrimSpace(root.Path) == "" {
			continue
		}
		root.Path = expandPath(root.Path, "")
		if slices.ContainsFunc(roots, func(one PathRoot) bool { return one.Path == root.Path }) {
			continue
		}
		roots = append(roots, root)
	}
	batch := w.Batch
	if batch <= 0 {
		batch = pathWalkBatch
	}
	var current *walker
	label := func() string {
		what := fmt.Sprintf("%d roots", len(roots))
		if len(roots) == 1 {
			what = roots[0].Label
			if what == "" {
				what = displayPath(roots[0].Path)
			}
		}
		if current == nil {
			return PathSourceWalk + " " + what
		}
		n := current.found.Load()
		switch {
		case current.limited.Load():
			return fmt.Sprintf("%s %s · %d (path.walk-limit)", PathSourceWalk, what, n)
		case current.done.Load():
			return fmt.Sprintf("%s %s · %d", PathSourceWalk, what, n)
		}
		return fmt.Sprintf("%s %s · %d…", PathSourceWalk, what, n)
	}
	return ChooserSource{
		Name:  PathSourceWalk,
		Label: label,
		Empty: "no folders under the roots",
		Load: func(string) Result {
			wk := &walker{opts: r.walkOptions(pick), roots: roots, batch: batch, cache: cache, now: time.Now, seen: map[string]bool{}}
			current = wk
			return wk.result()
		},
	}
}

// result is the pending work for the walk's next batch.
func (w *walker) result() Result {
	return Pending(func(ctx context.Context) (any, error) {
		rows, err := w.next(ctx)
		if err != nil {
			return nil, err
		}
		if w.done.Load() {
			return ChooserBatch{Rows: rows}, nil
		}
		return ChooserBatch{Rows: rows, More: w.result()}, nil
	}).Label("walk")
}

// next walks one batch: up to batch folders read, or pathWalkSlice, and
// returns the rows found.
func (w *walker) next(ctx context.Context) ([]ChooserRow, error) {
	var rows []ChooserRow
	deadline := time.Now().Add(pathWalkSlice)
	reads := 0
	for w.ri < len(w.roots) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if w.limited.Load() {
			break
		}
		root := w.roots[w.ri]
		if !w.started {
			w.started = true
			if !w.begin(root, &rows) {
				w.advance()
				continue
			}
		}
		if len(w.queue) == 0 {
			w.cache.put(w.cacheKey(root), w.entries, w.now())
			w.advance()
			continue
		}
		if reads >= w.batch || time.Now().After(deadline) {
			return rows, nil
		}
		dir := w.queue[0]
		w.queue = w.queue[1:]
		reads++
		w.read(root, dir, &rows)
	}
	w.done.Store(true)
	return rows, nil
}

// begin starts a root: its cached walk when fresh (the whole root at
// once), else its own row and a queue holding it. False skips the root (it
// is gone, under path.no-auto-list, or already listed under an earlier
// root).
func (w *walker) begin(root PathRoot, rows *[]ChooserRow) bool {
	w.queue, w.entries = nil, nil
	if w.seen[root.Path] || !autoListableUnder(w.opts.noAuto, root.Path) {
		return false
	}
	info, err := os.Stat(root.Path)
	if err != nil || !info.IsDir() {
		return false
	}
	w.exclude = nil
	for _, earlier := range w.roots[:w.ri] {
		if strings.HasPrefix(earlier.Path, root.Path+string(filepath.Separator)) {
			w.exclude = append(w.exclude, earlier.Path)
		}
	}
	if cached, ok := w.cache.get(w.cacheKey(root), w.now()); ok {
		for _, e := range cached {
			w.emit(root, e, rows)
		}
		return false // nothing left to read
	}
	w.entries = []walkEntry{{rel: ".", dir: true}}
	w.emit(root, w.entries[0], rows)
	w.queue = []walkQueue{{path: root.Path, depth: 0}}
	return true
}

func (w *walker) advance() {
	w.ri++
	w.started = false
	w.queue, w.entries, w.exclude = nil, nil, nil
}

func (w *walker) cacheKey(root PathRoot) string {
	return root.Path + "|" + strings.Join(w.exclude, ":") + "|" + w.opts.key()
}

// read lists one folder: its entries become rows, its subfolders join the
// queue while above the depth limit.
func (w *walker) read(root PathRoot, dir walkQueue, rows *[]ChooserRow) {
	entries, err := os.ReadDir(dir.path)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if w.skipped(name) {
			continue
		}
		full := filepath.Join(dir.path, name)
		if slices.Contains(w.exclude, full) {
			continue
		}
		isDir, descend := e.IsDir(), e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			// A linked folder is listed but never entered: no loops.
			if info, err := os.Stat(full); err == nil {
				isDir = info.IsDir()
			}
			descend = false
		}
		if descend && !autoListableUnder(w.opts.noAuto, full) {
			descend = false
		}
		rel, _ := filepath.Rel(root.Path, full)
		entry := walkEntry{rel: rel, dir: isDir}
		w.entries = append(w.entries, entry)
		w.emit(root, entry, rows)
		if w.limited.Load() {
			return
		}
		if descend && dir.depth+1 < w.opts.depth {
			w.queue = append(w.queue, walkQueue{path: full, depth: dir.depth + 1})
		}
	}
}

// skipped reports whether a walk leaves a name out: an ignored name
// (path.walk-ignore, globs), or a hidden one while path.walk-hidden is off
// unless path.walk-include names it.
func (w *walker) skipped(name string) bool {
	for _, pattern := range w.opts.ignore {
		if ok, _ := filepath.Match(pattern, name); ok {
			return true
		}
	}
	if strings.HasPrefix(name, ".") && !w.opts.hidden {
		for _, pattern := range w.opts.include {
			if ok, _ := filepath.Match(pattern, name); ok {
				return false
			}
		}
		return true
	}
	return false
}

// emit adds an entry's row unless the pick leaves it out (a file for a
// folder pick, a folder for a file pick) or an earlier root listed it.
func (w *walker) emit(root PathRoot, e walkEntry, rows *[]ChooserRow) {
	if e.dir != w.opts.dirs {
		return
	}
	full := filepath.Join(root.Path, e.rel)
	if w.seen[full] {
		return
	}
	w.seen[full] = true
	if int(w.found.Load()) >= w.opts.limit {
		w.limited.Store(true)
		return
	}
	w.found.Add(1)
	rel := e.rel
	if e.dir {
		rel = strings.TrimSuffix(rel, "/") + "/"
		if e.rel == "." {
			rel = "./"
		}
	}
	label := root.Label
	if label == "" {
		label = displayPath(root.Path)
	}
	*rows = append(*rows, ChooserRow{ID: full, Label: displayPath(full), Data: full, Cells: []string{rel, label}})
}

// autoListableUnder is autoListable over the path.no-auto-list prefixes,
// for work off the loop.
func autoListableUnder(prefixes []string, dir string) bool {
	dir = filepath.Clean(dir) + string(filepath.Separator)
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		if strings.HasPrefix(dir, prefix) && dir != prefix {
			return false
		}
	}
	return true
}
