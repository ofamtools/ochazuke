package ochazuke

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
)

// PathFinder was the path picker's fuzzy finder, a modal of its own;
// since v0.96.0 it is the path picker's find source (path_find.go) and
// picker.open with no name opens the path picker on it, whether or not
// NewPicker was called. What is left draws that picker for an
// application that still registers a modal view for the finder, and keeps
// the finder's fields readable: Refresh (and every method) fills them from
// the open path picker's find source.
//
// Deprecated: picker.open opens the path picker; NewPicker and PathFinder
// are kept for one release (v0.96.0, removed after v0.97.0).
type PathFinder struct {
	Runtime *Runtime
	// Origin is the context the find opened from (picker.open with no
	// name), Directory the folder it searches (the query's root), Filter
	// any, file or dir.
	Origin    Context
	Directory string
	Filter    string
	// Entries are the rows shown, relative to Directory; Selected the
	// highlighted one's index.
	Entries  []PickerEntry
	Selected int
	// Error is the load's error, or why the index is incomplete; Loading
	// whether a load runs.
	Error   string
	Loading bool
}

// Picker is the path finder's old name.
//
// Deprecated: use picker.open; Picker is kept for one release (v0.96.0).
type Picker = PathFinder

// NewPicker returns the path finder shim for r. picker.open with no name
// opens the path picker on its find source without it.
//
// Deprecated: kept for one release (v0.96.0); call picker.open.
func NewPicker(r *Runtime) *PathFinder { return &PathFinder{Runtime: r} }

// IsOpen reports whether the path picker is open.
func (p *PathFinder) IsOpen() bool {
	p.Refresh()
	return p.Runtime.Chooser(PathChooserKind) != nil
}

// MatchMode is the open path picker's match mode, else the picker.match
// setting.
func (p *PathFinder) MatchMode() string {
	p.Refresh()
	if c := p.Runtime.Chooser(PathChooserKind); c != nil {
		return c.MatchMode()
	}
	return p.Runtime.ChooserMatch()
}

// Refresh fills the finder's fields from the open path picker while its
// find source shows; closed (or on another source) they are cleared.
func (p *PathFinder) Refresh() {
	*p = PathFinder{Runtime: p.Runtime}
	c := p.Runtime.Chooser(PathChooserKind)
	f := p.Runtime.chooserMemory().find
	if c == nil || f == nil || !f.shows(c) {
		return
	}
	p.Origin, p.Directory, p.Filter = f.Origin, f.root, f.filter
	p.Loading, p.Error = c.Loading(), c.loadErr
	if p.Error == "" {
		p.Error = f.indexError()
	}
	highlighted, _ := c.Highlighted()
	for _, row := range c.Matches() {
		path, ok := row.Data.(string)
		if !ok {
			continue
		}
		rel, err := filepath.Rel(f.root, path)
		if err != nil {
			rel = path
		}
		if row.ID == highlighted.ID {
			p.Selected = len(p.Entries)
		}
		p.Entries = append(p.Entries, PickerEntry{Path: filepath.ToSlash(rel), Dir: row.Opens || rel == "."})
	}
}

// View draws the open path picker ("" when it is closed).
func (p *PathFinder) View(width, height int) string { return p.ViewBlock(width, height).Text }

// ViewBlock draws the open path picker; the model draws it on its own, so
// a modal view registered for the old finder's modal is never asked.
func (p *PathFinder) ViewBlock(width, height int) Block {
	p.Refresh()
	if c := p.Runtime.Chooser(PathChooserKind); c != nil && width > 0 && height > 0 {
		return c.View(width, height)
	}
	return Block{}
}

// PendingCommand, HandleMessage and Close are no-ops: the path picker's
// work runs as its own jobs.
func (p *PathFinder) PendingCommand() tea.Cmd        { p.Refresh(); return nil }
func (p *PathFinder) HandleMessage(msg tea.Msg) bool { return false }
func (p *PathFinder) Close()                         {}

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
