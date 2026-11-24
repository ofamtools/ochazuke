// Package pathpicker is a directory and file path picker built on the picker
// widget. It works inline or inside a modal overlay: the host feeds it
// commands and renders View. All file system access goes through the FS
// interface so tests and hosts can substitute their own.
//
// Roots are named starting places (CycleRoot, JumpRoot); Recents holds
// picked paths, newest first. A typed path in the query ("~", "~/x", "/x",
// "./x", "../x") is resolved when Open or Select runs. In dir and any modes a
// pinned "[Use this folder]" row heads the list. CreateDir makes a folder.
//
// pathpicker imports only the standard library, core, ui and widgets/picker.
package pathpicker

import (
	"errors"
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
	"ofam.dev/ochazuke/traits"
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
	CmdRootCycle     = "pathpicker.root.cycle" // go to the next root, the first when none is active
	CmdMkdir         = "pathpicker.mkdir"      // create Args["name"] in Dir and highlight it
)

// useFolderLabel prefixes the pinned row that picks the current folder.
const useFolderLabel = "[Use this folder]"

// currentRowID is the picker item ID of the pinned current-folder row.
const currentRowID = "pathpicker.current"

var (
	errNoRoots          = errors.New("no roots to cycle")
	errMkdirUnsupported = errors.New("this file system cannot create folders")
	errMkdirName        = errors.New(CmdMkdir + " needs a folder name (args.name)")
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

// MkdirFS is an optional FS capability used by CreateDir.
type MkdirFS interface {
	Mkdir(path string) error
}

// Root is a named starting place. Path may start with "~" and may be
// relative to Dir.
type Root struct {
	Label string
	Path  string
}

// OSFS is the real file system. It implements FS and MkdirFS.
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

// Mkdir creates one directory; its parent must exist.
func (OSFS) Mkdir(path string) error { return os.Mkdir(path, 0o755) }

// Model is the path picker state.
type Model struct {
	FS          FS // nil means OSFS{}
	Dir         string
	Mode        PickMode
	ShowHidden  bool
	ShowPreview bool
	Query       string
	MatchMode   picker.MatchMode
	Err         string // error of the last failed action; "" after one succeeds

	Roots   []Root   // starting places for CycleRoot and JumpRoot
	Recents []string // picked paths, newest first, without duplicates

	list    picker.Model
	entries []DirEntry
	root    int // active root index + 1; 0 when none
}

// currentRow is the Data of the pinned "[Use this folder]" row. It is not a
// DirEntry, so Entries never lists it; Focused reports it as Current().
type currentRow struct{ DirEntry }

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
	m.resetSel()
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

// Filtering reports whether the path picker is in filter-edit mode.
func (m *Model) Filtering() bool { return m.list.Filtering }

// SetFiltering sets whether the path picker is in filter-edit mode.
func (m *Model) SetFiltering(v bool) { m.list.Filtering = v }

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

func (m *Model) pinCurrent() bool { return m.Mode != PickFile }

// rankEntry is the picker Rank: names equal to the query, then names that
// start with it, then names that contain it, then the rest (fuzzy or
// subtitle matches). Ties keep the listing order, so dirs stay first in
// each tier.
func rankEntry(it picker.Item, q string) int {
	q = strings.TrimSuffix(q, "/")
	n := strings.ToLower(strings.TrimSuffix(it.Title, "/"))
	switch {
	case n == q:
		return 0
	case strings.HasPrefix(n, q):
		return 1
	case strings.Contains(n, q):
		return 2
	}
	return 3
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
	m.list.Rank = rankEntry
	m.list.Pinned = nil
	if m.pinCurrent() {
		m.list.Pinned = &picker.Item{
			ID: currentRowID, Title: useFolderLabel + " " + m.Dir, Subtitle: "current",
			Badges: []ui.Badge{{Text: "use", Tone: "accent"}},
			Ref:    &core.Ref{Kind: "fs.path", ID: m.Dir}, Scope: "pathpicker.current",
			Data: currentRow{m.Current()},
		}
	}
	m.list.SetItems(items)
}

// resetSel highlights the first real row, or the pinned row when nothing
// else matches.
func (m *Model) resetSel() {
	m.list.SelectedIdx = 0
	if m.list.Pinned != nil && len(m.list.Filtered()) > 1 {
		m.list.SelectedIdx = 1
	}
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
	if err := m.Reload(); err != nil {
		m.Dir, m.entries, m.Query = old, oldEntries, oldQuery
		return err
	}
	m.resetSel()
	return nil
}

// SetQuery sets the filter query and highlights the best match.
func (m *Model) SetQuery(q string) { m.Query = q; m.rebuild(); m.resetSel() }

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
	m.MatchMode = traits.CycleMatchMode(m.MatchMode, traits.FilterConfig{})
	m.rebuild()
	m.resetSel()
}

// ToggleHidden shows or hides dotfiles.
func (m *Model) ToggleHidden() { m.ShowHidden = !m.ShowHidden; m.rebuild() }

// Focused returns the highlighted entry; on the pinned "[Use this folder]"
// row that is Current(), and FocusedCurrent reports true.
func (m *Model) Focused() (DirEntry, bool) {
	it, ok := m.list.Focused()
	if !ok {
		return DirEntry{}, false
	}
	switch d := it.Data.(type) {
	case DirEntry:
		return d, true
	case currentRow:
		return d.DirEntry, true
	}
	return DirEntry{}, false
}

// FocusedCurrent reports whether the pinned "[Use this folder]" row is
// highlighted.
func (m *Model) FocusedCurrent() bool {
	it, ok := m.list.Focused()
	return ok && it.ID == currentRowID
}

// Entries returns the visible entries after filtering, ranked under a
// query, dirs first otherwise. The pinned row is not an entry.
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
	m.highlight(from)
	return nil
}

// highlight moves the selection to the row with ID id, when listed.
func (m *Model) highlight(id string) {
	for i, it := range m.list.Filtered() {
		if it.ID == id {
			m.list.SelectedIdx = i
		}
	}
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

// Drill enters the highlighted directory; it reports whether it did. The
// pinned current-folder row is not drilled.
func (m *Model) Drill() bool {
	e, ok := m.Focused()
	if !ok || !e.IsDir || m.FocusedCurrent() {
		return false
	}
	return m.SetDir(e.Path) == nil
}

// Current returns Dir as an entry.
func (m *Model) Current() DirEntry {
	return DirEntry{Name: filepath.Base(m.Dir), Path: m.Dir, IsDir: true}
}

// fail stores err in Err and returns it.
func (m *Model) fail(err error) error { m.Err = err.Error(); return err }

// JumpRoot goes to Roots[idx]. The root becomes active even when its folder
// cannot be read, so CycleRoot moves past it; Dir, the listing and the query
// then stay as they were and Err is set.
func (m *Model) JumpRoot(idx int) error {
	if idx < 0 || idx >= len(m.Roots) {
		return m.fail(fmt.Errorf("root %d out of range (%d roots)", idx, len(m.Roots)))
	}
	m.root = idx + 1
	p, err := m.resolve(m.Roots[idx].Path)
	if err != nil {
		return m.fail(err)
	}
	return m.SetDir(p)
}

// CycleRoot goes to the root after the active one, wrapping, or to the
// first root when none is active.
func (m *Model) CycleRoot() error {
	if len(m.Roots) == 0 {
		return m.fail(errNoRoots)
	}
	return m.JumpRoot(m.root % len(m.Roots))
}

// RecordRecent puts path first in Recents and drops an older copy.
// Successful picks record themselves.
func (m *Model) RecordRecent(path string) {
	if path == "" {
		return
	}
	path = filepath.Clean(path)
	out := []string{path}
	for _, r := range m.Recents {
		if r != path {
			out = append(out, r)
		}
	}
	m.Recents = out
}

// isPathQuery reports whether q is a typed path: "~", "~/...", absolute,
// ".", "..", "./..." or "../...".
func isPathQuery(q string) bool {
	return q == "~" || strings.HasPrefix(q, "~/") || filepath.IsAbs(q) ||
		q == "." || q == ".." || strings.HasPrefix(q, "./") || strings.HasPrefix(q, "../")
}

// resolve makes p absolute: "~" uses FS.HomeDir, relative paths start at Dir.
func (m *Model) resolve(p string) (string, error) {
	switch {
	case p == "~" || strings.HasPrefix(p, "~/"):
		h, err := m.fs().HomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(h, p[1:]), nil
	case filepath.IsAbs(p):
		return filepath.Clean(p), nil
	}
	return filepath.Join(m.Dir, p), nil
}

// lookup describes path with FS.ReadDir alone (so any FS works): a readable
// directory, or a named entry of its parent, hidden or not.
func (m *Model) lookup(path string) (DirEntry, error) {
	_, dirErr := m.fs().ReadDir(path)
	if dirErr == nil {
		return DirEntry{Name: filepath.Base(path), Path: path, IsDir: true}, nil
	}
	if es, err := m.fs().ReadDir(filepath.Dir(path)); err == nil {
		for _, e := range es {
			if e.Name != filepath.Base(path) {
				continue
			}
			if e.IsDir { // listed but unreadable
				return DirEntry{}, dirErr
			}
			e.Path = path
			return e, nil
		}
	}
	return DirEntry{}, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
}

// openTyped acts on the typed path in the query: a folder is entered, a
// file is picked when the mode allows files. A failure sets Err and never
// falls back to the highlighted row.
func (m *Model) openTyped() *DirEntry {
	p, err := m.resolve(m.Query)
	if err != nil {
		m.fail(err)
		return nil
	}
	e, err := m.lookup(p)
	switch {
	case err != nil:
		m.fail(err)
	case e.IsDir:
		m.SetDir(p)
	case !m.pickable(e):
		m.fail(errors.New(p + ": not a folder"))
	default:
		return m.picked(e)
	}
	return nil
}

// CreateDir creates the child folder name in Dir (the FS must implement
// MkdirFS), clears the query, reloads and highlights it, showing hidden
// entries when name starts with ".". name must be one child name, not a
// path.
func (m *Model) CreateDir(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return m.fail(errors.New("folder name required"))
	case name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") ||
		strings.ContainsRune(name, filepath.Separator) || filepath.VolumeName(name) != "":
		return m.fail(fmt.Errorf("folder name %q must be one name, not a path", name))
	}
	mk, ok := m.fs().(MkdirFS)
	if !ok {
		return m.fail(errMkdirUnsupported)
	}
	p := filepath.Join(m.Dir, name)
	if err := mk.Mkdir(p); err != nil {
		return m.fail(err)
	}
	oldQuery, oldHidden := m.Query, m.ShowHidden
	m.Query = ""
	if strings.HasPrefix(name, ".") {
		m.ShowHidden = true
	}
	if err := m.Reload(); err != nil {
		// The folder exists; keep the listing, selection and filter as they were.
		m.Query, m.ShowHidden = oldQuery, oldHidden
		return m.fail(fmt.Errorf("created %s but could not reload %s: %w", p, m.Dir, err))
	}
	m.highlight(p)
	return nil
}

// picked records and returns e, or returns nil when the mode refuses it.
func (m *Model) picked(e DirEntry) *DirEntry {
	if !m.pickable(e) {
		return nil
	}
	m.Err = ""
	m.RecordRecent(e.Path)
	return &e
}

// HandleCommand applies a command. picked is non-nil when the user chose a
// path (it is also recorded in Recents); handled reports whether the
// command was understood.
//
// Open on the pinned "[Use this folder]" row picks the current folder, on
// a directory drills and on a file picks it when the mode allows. Open and
// Select resolve a typed path query instead of using the highlight.
func (m *Model) HandleCommand(cmd core.Command) (picked *DirEntry, handled bool) {
	switch cmd.ID {
	case CmdOpen, picker.CmdConfirm, "motion.right":
		if cmd.ID != "motion.right" && isPathQuery(m.Query) {
			return m.openTyped(), true
		}
		e, ok := m.Focused()
		switch {
		case !ok:
		case m.FocusedCurrent():
			if cmd.ID != "motion.right" {
				return m.picked(e), true
			}
		case e.IsDir:
			m.Drill()
		case cmd.ID != "motion.right":
			return m.picked(e), true
		}
		return nil, true
	case CmdSelect:
		if isPathQuery(m.Query) {
			return m.openTyped(), true
		}
		if e, ok := m.Focused(); ok {
			return m.picked(e), true
		}
		return m.picked(m.Current()), true
	case CmdSelectCurrent:
		return m.picked(m.Current()), true
	case CmdParent, "motion.left":
		m.Parent()
	case CmdHome:
		m.Home()
	case CmdRootCycle:
		m.CycleRoot()
	case CmdMkdir:
		if name := cmd.Arg("name"); name != "" {
			m.CreateDir(name)
		} else {
			m.fail(errMkdirName)
		}
	case CmdHiddenToggle:
		m.ToggleHidden()
	case CmdPreviewToggle:
		m.ShowPreview = !m.ShowPreview
		m.rebuild()
	case CmdModeCycle, picker.CmdModeCycle:
		m.CycleMatchMode()
	default:
		_, handled := m.list.HandleCommand(cmd)
		if handled && m.list.Query != m.Query {
			m.Query = m.list.Query
			m.rebuild()
			m.resetSel()
		} else if handled && cmd.ID == picker.CmdFilterClear {
			m.resetSel()
		}
		return nil, handled
	}
	return nil, true
}

func (m *Model) preview(it picker.Item) *ui.ViewNode {
	var e DirEntry
	switch d := it.Data.(type) {
	case DirEntry:
		e = d
	case currentRow:
		e = d.DirEntry
	default:
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
	root := ""
	if i := m.root - 1; i >= 0 && i < len(m.Roots) {
		if root = m.Roots[i].Label; root == "" {
			root = m.Roots[i].Path
		}
		badges = append(badges, ui.Badge{Text: root, Tone: "accent"})
	}
	m.list.Facts = map[string]any{
		"dir": m.Dir, "pickMode": string(m.Mode), "showHidden": m.ShowHidden,
		"showPreview": m.ShowPreview, "error": m.Err,
		"root": root, "focusedCurrent": m.FocusedCurrent(),
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
