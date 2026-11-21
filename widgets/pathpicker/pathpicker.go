// Package pathpicker is a directory and file path picker built on the picker
// widget. It works inline or inside a modal overlay: the host feeds it
// commands and renders View. All file system access goes through the FS
// interface so tests and hosts can substitute their own.
//
// pathpicker imports only the standard library, core, ui and widgets/picker.
package pathpicker

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/picker"
)

// PickMode says what a selection may be.
type PickMode string

// Pick modes.
const (
	PickFile PickMode = "file"
	PickDir  PickMode = "dir"
	PickAny  PickMode = "any"
)

// Command IDs handled by Model.HandleCommand besides motions and the
// picker.* commands it forwards.
const (
	CmdOpen          = "pathpicker.open"   // drill into a dir; select a file when files are pickable
	CmdSelect        = "pathpicker.select" // select the highlighted entry (or the current dir when empty)
	CmdSelectCurrent = "pathpicker.select.current"
	CmdParent        = "pathpicker.parent"
	CmdHome          = "pathpicker.home"
	CmdHiddenToggle  = "pathpicker.hidden.toggle"
	CmdPreviewToggle = "pathpicker.preview.toggle"
	CmdModeCycle     = "pathpicker.mode.cycle"
)

// PreviewBytes is how much of a file the preview reads.
const PreviewBytes = 4096

// MaxPreviewSize is the largest file the preview shows as text.
const MaxPreviewSize = 1 << 20

// DirEntry describes one directory entry.
type DirEntry struct {
	Name    string
	Path    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

// FS is the file system the picker browses.
type FS interface {
	ReadDir(dir string) ([]DirEntry, error)
	ReadPreview(path string, maxBytes int) (string, error)
	HomeDir() (string, error)
}

// OSFS is the real file system.
type OSFS struct{}

// ReadDir lists dir; symlinks to directories count as directories.
func (OSFS) ReadDir(dir string) ([]DirEntry, error) {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]DirEntry, 0, len(es))
	for _, e := range es {
		p := filepath.Join(dir, e.Name())
		info, err := os.Stat(p) // follows symlinks; dangling links fall back to Lstat
		if err != nil {
			if info, err = e.Info(); err != nil {
				continue
			}
		}
		out = append(out, DirEntry{Name: e.Name(), Path: p, IsDir: info.IsDir(), Size: info.Size(), ModTime: info.ModTime()})
	}
	return out, nil
}

// ReadPreview returns up to maxBytes of the file.
func (OSFS) ReadPreview(path string, maxBytes int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)))
	return string(b), err
}

// HomeDir returns the user's home directory.
func (OSFS) HomeDir() (string, error) { return os.UserHomeDir() }

// Model is the path picker state.
type Model struct {
	FS          FS // nil means OSFS{}
	Dir         string
	Mode        PickMode
	ShowHidden  bool
	ShowPreview bool
	Query       string
	MatchMode   picker.MatchMode
	Err         string // last file system error; "" when none

	list    picker.Model
	entries []DirEntry
}

// New returns a Model at dir (made absolute) and loads it. A load error is
// stored in Err and returned.
func New(fs FS, dir string, mode PickMode) (*Model, error) {
	if mode == "" {
		mode = PickAny
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	m := &Model{FS: fs, Dir: dir, Mode: mode, MatchMode: picker.MatchSubstring}
	err := m.Reload()
	return m, err
}

func (m *Model) fs() FS {
	if m.FS == nil {
		return OSFS{}
	}
	return m.FS
}

// List exposes the inner picker (read-only use: selection, filtered items).
func (m *Model) List() *picker.Model { return &m.list }

// Reload re-reads Dir and rebuilds the list, keeping the query.
func (m *Model) Reload() error {
	es, err := m.fs().ReadDir(m.Dir)
	if err != nil {
		m.Err = err.Error()
		return err
	}
	m.Err = ""
	m.entries = es
	m.rebuild()
	return nil
}

func (m *Model) visible() []DirEntry {
	var out []DirEntry
	for _, e := range m.entries {
		if !m.ShowHidden && strings.HasPrefix(e.Name, ".") {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func (m *Model) pickable(e DirEntry) bool {
	switch m.Mode {
	case PickFile:
		return !e.IsDir
	case PickDir:
		return e.IsDir
	}
	return true
}

func (m *Model) rebuild() {
	es := m.visible()
	items := make([]picker.Item, len(es))
	for i, e := range es {
		it := picker.Item{ID: e.Path, Title: e.Name, Ref: &core.Ref{Kind: "fs.path", ID: e.Path}, Data: e}
		if e.IsDir {
			it.Title += "/"
			it.Subtitle = "dir"
			it.Badges = []ui.Badge{{Text: "dir", Tone: "accent"}}
			it.Scope = "pathpicker.dir"
		} else {
			it.Subtitle = HumanSize(e.Size)
			it.Scope = "pathpicker.file"
		}
		items[i] = it
	}
	m.list.Title = m.Dir
	m.list.Scope = "pathpicker"
	m.list.Placeholder = "filter " + m.Dir
	m.list.PreviewFn = nil
	if m.ShowPreview {
		m.list.PreviewFn = m.preview
	}
	m.list.MatchMode = m.MatchMode
	m.list.Query = m.Query
	m.list.SetItems(items)
}

// HumanSize formats a byte count as B, KB, MB, ...
func HumanSize(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	v, unit := float64(n), ""
	for _, u := range []string{"KB", "MB", "GB", "TB"} {
		v /= 1024
		unit = u
		if v < 1024 {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", v, unit)
}

// SetDir changes directory, clearing the query. On error Dir is unchanged
// and Err is set.
func (m *Model) SetDir(dir string) error {
	old, oldEntries, oldQuery := m.Dir, m.entries, m.Query
	m.Dir, m.Query = dir, ""
	m.list.SelectedIdx = 0
	if err := m.Reload(); err != nil {
		m.Dir, m.entries, m.Query = old, oldEntries, oldQuery
		return err
	}
	return nil
}

// SetQuery sets the filter query.
func (m *Model) SetQuery(q string) { m.Query = q; m.rebuild(); m.list.SelectedIdx = 0 }

// AppendQuery appends to the query.
func (m *Model) AppendQuery(s string) { m.SetQuery(m.Query + s) }

// BackspaceQuery removes the last rune of the query.
func (m *Model) BackspaceQuery() {
	if rs := []rune(m.Query); len(rs) > 0 {
		m.SetQuery(string(rs[:len(rs)-1]))
	}
}

// CycleMatchMode advances substring -> fuzzy -> regex.
func (m *Model) CycleMatchMode() {
	l := picker.Model{MatchMode: m.MatchMode}
	l.CycleMatchMode()
	m.MatchMode = l.MatchMode
	m.rebuild()
	m.list.SelectedIdx = 0
}

// ToggleHidden shows or hides dotfiles.
func (m *Model) ToggleHidden() { m.ShowHidden = !m.ShowHidden; m.rebuild() }

// Focused returns the highlighted entry.
func (m *Model) Focused() (DirEntry, bool) {
	it, ok := m.list.Focused()
	if !ok {
		return DirEntry{}, false
	}
	e, ok := it.Data.(DirEntry)
	return e, ok
}

// Entries returns the visible entries after filtering, dirs first.
func (m *Model) Entries() []DirEntry {
	var out []DirEntry
	for _, it := range m.list.Filtered() {
		if e, ok := it.Data.(DirEntry); ok {
			out = append(out, e)
		}
	}
	return out
}

// Parent goes to the parent directory and highlights the directory left.
func (m *Model) Parent() error {
	parent := filepath.Dir(m.Dir)
	if parent == m.Dir {
		return nil
	}
	from := m.Dir
	if err := m.SetDir(parent); err != nil {
		return err
	}
	for i, e := range m.list.Filtered() {
		if e.ID == from {
			m.list.SelectedIdx = i
		}
	}
	return nil
}

// Home jumps to the home directory.
func (m *Model) Home() error {
	h, err := m.fs().HomeDir()
	if err != nil {
		m.Err = err.Error()
		return err
	}
	return m.SetDir(h)
}

// Drill enters the highlighted directory; it reports whether it did.
func (m *Model) Drill() bool {
	e, ok := m.Focused()
	if !ok || !e.IsDir {
		return false
	}
	return m.SetDir(e.Path) == nil
}

// Current returns Dir as an entry.
func (m *Model) Current() DirEntry {
	return DirEntry{Name: filepath.Base(m.Dir), Path: m.Dir, IsDir: true}
}

// HandleCommand applies a command. picked is non-nil when the user chose a
// path; handled reports whether the command was understood.
func (m *Model) HandleCommand(cmd core.Command) (picked *DirEntry, handled bool) {
	pick := func(e DirEntry) *DirEntry {
		if m.pickable(e) {
			return &e
		}
		return nil
	}
	switch cmd.ID {
	case CmdOpen, picker.CmdConfirm, "motion.right":
		e, ok := m.Focused()
		switch {
		case !ok:
		case e.IsDir:
			m.Drill()
		case cmd.ID != "motion.right":
			return pick(e), true
		}
		return nil, true
	case CmdSelect:
		if e, ok := m.Focused(); ok {
			return pick(e), true
		}
		return pick(m.Current()), true
	case CmdSelectCurrent:
		return pick(m.Current()), true
	case CmdParent, "motion.left":
		m.Parent()
	case CmdHome:
		m.Home()
	case CmdHiddenToggle:
		m.ToggleHidden()
	case CmdPreviewToggle:
		m.ShowPreview = !m.ShowPreview
		m.rebuild()
	case CmdModeCycle, picker.CmdModeCycle:
		m.CycleMatchMode()
	default:
		_, handled := m.list.HandleCommand(cmd)
		return nil, handled
	}
	return nil, true
}

func (m *Model) preview(it picker.Item) *ui.ViewNode {
	e, ok := it.Data.(DirEntry)
	if !ok {
		return nil
	}
	text := ""
	switch {
	case e.IsDir:
		es, err := m.fs().ReadDir(e.Path)
		if err != nil {
			text = "cannot read directory: " + err.Error()
		} else {
			text = strconv.Itoa(len(es)) + " entries"
			if len(es) == 1 {
				text = "1 entry"
			}
		}
	case e.Size > MaxPreviewSize:
		text = "file too large to preview (" + HumanSize(e.Size) + ")"
	default:
		s, err := m.fs().ReadPreview(e.Path, PreviewBytes)
		switch {
		case err != nil:
			text = "cannot read file: " + err.Error()
		case strings.ContainsRune(s, 0) || !utf8.ValidString(trimPartial(s)):
			text = "binary file (" + HumanSize(e.Size) + ")"
		case s == "":
			text = "empty file"
		default:
			text = s
		}
	}
	return &ui.ViewNode{Kind: ui.KindText, Props: map[string]any{"text": text}}
}

// trimPartial drops a trailing incomplete rune cut by the preview limit.
func trimPartial(s string) string {
	for i := 0; i < utf8.UTFMax && len(s) > 0; i++ {
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size != 1 {
			return s
		}
		s = s[:len(s)-1]
	}
	return s
}

// View renders a panel titled with Dir holding the picker (with a preview
// split when ShowPreview). The picker node has ID id and is the focus target.
func (m *Model) View(id string) ui.ViewNode {
	badges := []ui.Badge{{Text: string(m.Mode), Tone: "muted"}}
	if m.ShowHidden {
		badges = append(badges, ui.Badge{Text: "hidden", Tone: "warn"})
	}
	if m.Err != "" {
		badges = append(badges, ui.Badge{Text: "error", Tone: "error"})
	}
	m.list.Facts = map[string]any{
		"dir": m.Dir, "pickMode": string(m.Mode), "showHidden": m.ShowHidden,
		"showPreview": m.ShowPreview, "error": m.Err,
	}
	m.list.Badges = badges
	inner := m.list.View(id)
	children := []ui.ViewNode{inner}
	if m.Err != "" {
		children = append(children, ui.ViewNode{Kind: ui.KindText, Tone: "error", Props: map[string]any{"text": m.Err}})
	}
	return ui.ViewNode{
		ID: id + ".panel", Kind: ui.KindPanel, Title: m.Dir, Badges: badges,
		Children: children,
	}
}
