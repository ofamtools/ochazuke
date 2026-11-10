package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// The path chooser's navigation: one place (browseNav) that knows the folder
// being shown, how to go into a folder, up to its parent (until the root,
// whatever the start was), to the home folder or the root, to the deepest
// place visited, and how to make a folder there. The browse source and the
// hybrid style's Browse section both stand on it, and every other source
// gets the same navigation through browsable, so no source is a wall: from
// any row or any start the user can go up one folder at a time to "/" and into
// any readable folder below it.

// pathState is what one open path chooser keeps for its sources: the hidden
// files switch (picker.toggle hidden), and whether the open is a style that
// draws the folder's parent and preview beside it or takes enter as a pick.
type pathState struct {
	hidden bool
	// columns: the columns style (the browse source supplies the side
	// column and the preview).
	columns bool
	// enterPicks: enter picks a folder row in a folder pick and l goes
	// into it (hybrid and columns), instead of enter going into it.
	enterPicks bool
}

// browseNav is the folder a source shows and where it can go.
type browseNav struct {
	r    *Runtime
	dir  string
	deep string // the deepest place visited on the way down from dir
	// asked: the user opened or went up into dir, so it is listed whatever
	// it is (path.no-auto-list holds only for a place the picker started in).
	asked bool
	// left is the folder just left by going up: the highlight returns to it.
	left string
}

func (r *Runtime) newBrowseNav(start string) *browseNav {
	return &browseNav{r: r, dir: start, deep: start}
}

// under reports whether p is base or inside it.
func under(p, base string) bool {
	return p == base || strings.HasPrefix(p, strings.TrimSuffix(base, string(filepath.Separator))+string(filepath.Separator))
}

// settle keeps deep a place at or below the folder shown: the deepest
// place visited on the way down, which last-col returns to.
func (n *browseNav) settle() {
	if !under(n.deep, n.dir) {
		n.deep = n.dir
	}
}

// open goes into path.
func (n *browseNav) open(path string) {
	n.dir, n.asked, n.left = filepath.Clean(path), true, ""
	n.settle()
}

// up goes to the parent folder; false at the root.
func (n *browseNav) up() bool { return n.upFrom(n.dir) }

// upFrom goes to the folder above from (a folder or a file's folder, the
// caller's call); false at the root. The highlight returns to from.
func (n *browseNav) upFrom(from string) bool {
	parent := filepath.Dir(from)
	if parent == from {
		return false
	}
	n.left = from
	n.dir, n.asked = parent, true
	n.settle()
	return true
}

// goTo goes to a named place: ~ or first-col (the home folder), / (the
// root), last-col (the deepest place visited going down from here).
func (n *browseNav) goTo(to string) bool {
	switch to {
	case "~", "first-col":
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return false
		}
		n.dir, n.asked, n.left = filepath.Clean(home), true, ""
		n.settle()
		return true
	case "/":
		n.dir, n.asked, n.left = string(filepath.Separator), true, ""
		n.settle()
		return true
	case "last-col":
		if n.deep != n.dir && under(n.deep, n.dir) {
			n.dir, n.asked, n.left = n.deep, true, ""
			return true
		}
	}
	return false
}

// landing is where the highlight goes after the last move.
func (n *browseNav) landing(section string) string {
	if n.left != "" {
		left := n.left
		n.left = ""
		return "row:" + left
	}
	return section
}

// make names a new folder under the folder shown (a/b makes both); on
// success the nav has moved into it.
func (n *browseNav) make(name string) Result {
	name = strings.Trim(strings.TrimSpace(name), "/")
	if name == "" {
		return Failed(errors.New("type a name first"))
	}
	target := filepath.Join(n.dir, name)
	return Pending(func(context.Context) (any, error) {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return nil, err
		}
		return target, nil
	}).Label("make " + displayPath(target)).Then(func(_ Ctx, _ any, err error) {
		if err == nil {
			n.open(target)
		}
	})
}

// browseListing is a folder's rows for the browse source and the hybrid
// style's Browse section: folders first, then files (none for a folder
// pick). It reads the file system: call it off the loop.
func browseListing(at string, pick PathPick, hidden bool, caseMode string, enterPicks bool) ([]ChooserRow, error) {
	rows, err := listDir(at, "", pick, hidden, caseMode)
	if err != nil {
		return nil, err
	}
	if enterPicks && pick.Dir() {
		for i := range rows {
			rows[i].Opens = false
		}
	}
	return rows, nil
}

// folderRow reports whether a row names a folder a pick may go into: a row
// of a folder pick, or one drawn with a trailing /.
func folderRow(pick PathPick, row ChooserRow) bool {
	if _, ok := row.Data.(string); !ok {
		return false
	}
	return row.Opens || pick.Dir() || strings.HasSuffix(row.Label, "/")
}

// browseSource lists one folder; going into a folder row browses into it,
// up goes to the parent, until the root. A folder chooser has a fixed "use
// <folder>" row.
func (r *Runtime) browseSource(pick PathPick, st *pathState, use func(string) Result) ChooserSource {
	nav := r.newBrowseNav(pick.Start)
	return r.browseSourceAt(pick, st, use, nav)
}

func (r *Runtime) browseSourceAt(pick PathPick, st *pathState, use func(string) Result, nav *browseNav) ChooserSource {
	src := ChooserSource{
		Name:  PathSourceBrowse,
		Label: func() string { return PathSourceBrowse + " " + displayPath(nav.dir) },
	}
	src.Load = func(string) Result {
		at, asked := nav.dir, nav.asked
		if !asked && !r.autoListable(at) {
			return Failed(fmt.Errorf("not listed on its own (path.no-auto-list): open it or type a path; %s", displayPath(at)))
		}
		hidden, caseMode, enterPicks := st.hidden, r.SearchCase(), st.enterPicks
		return Pending(func(context.Context) (any, error) {
			rows, err := browseListing(at, pick, hidden, caseMode, enterPicks)
			if err != nil {
				return nil, err
			}
			if pick.Dir() {
				rows = append(rows, ChooserRow{ID: at, Label: "use " + displayPath(at), Data: at, Fixed: true})
			}
			return rows, nil
		}).Label("browse " + displayPath(at))
	}
	src.Open = func(row ChooserRow) (string, bool) {
		path, ok := row.Data.(string)
		if !ok {
			return "", false
		}
		nav.open(path)
		return "", true
	}
	src.CanOpen = func(row ChooserRow) bool { return folderRow(pick, row) }
	src.Up = func(string) (string, bool) { return "", nav.up() }
	src.Goto = func(to string) (string, bool) { return "", nav.goTo(to) }
	src.Landing = func() string { return nav.landing("") }
	if pick.Dir() {
		src.Current = func() (ChooserRow, bool) {
			return ChooserRow{ID: nav.dir, Label: "use " + displayPath(nav.dir), Data: nav.dir}, true
		}
	}
	src.Make = nav.make
	src.MakeLabel = func() string { return "new folder in " + displayPath(nav.dir) }
	if pick.Parameter.New && !pick.Dir() {
		src.Create = func(query string) Result { return use(expandPath(query, nav.dir)) }
		src.CreateLabel = func(query string) string { return "new " + displayPath(expandPath(query, nav.dir)) }
	}
	if st.columns {
		src.Side = r.parentSide(nav, st)
		src.Preview = r.pathPreview(st)
	}
	return src
}

// parentSide is the side column of the columns style: the folder one level
// above the one shown, its entries listed, the folder shown marked.
func (r *Runtime) parentSide(nav *browseNav, st *pathState) func() (string, func() Result) {
	return func() (string, func() Result) {
		dir, hidden := nav.dir, st.hidden
		caseMode := r.SearchCase()
		prefixes := r.noAutoListPrefixes()
		return fmt.Sprintf("%s\x00%v", dir, hidden), func() Result {
			parent := filepath.Dir(dir)
			side := PickerSide{Title: displayPath(parent), At: filepath.Base(dir) + "/"}
			if parent == dir {
				side.Title, side.At = string(filepath.Separator), ""
				return Done(side)
			}
			if !autoListableUnder(prefixes, parent) {
				return Done(side)
			}
			return Pending(func(context.Context) (any, error) {
				rows, err := listDir(parent, "", PathPick{Parameter: Parameter{Path: PathAny}}, hidden, caseMode)
				if err != nil {
					side.Lines = []string{err.Error()}
					return side, nil
				}
				for _, row := range rows {
					side.Lines = append(side.Lines, row.Label)
					if len(side.Lines) >= 500 {
						break
					}
				}
				return side, nil
			}).Label("parent " + displayPath(parent))
		}
	}
}

// PathGotoOwn is picker.goto's place for a source's own rows again.
const PathGotoOwn = "own"

// pathPreviewLines is how many entries or lines a path preview reads.
const pathPreviewLines = 60

// pathPreview is the columns style's preview of the highlighted row: the
// entries of a folder, the first lines of a small text file. It reads the
// file system off the loop and never lists a folder under path.no-auto-list
// on its own.
func (r *Runtime) pathPreview(st *pathState) func(ChooserRow) Result {
	return func(row ChooserRow) Result {
		path, ok := row.Data.(string)
		if !ok || path == "" || row.ID == chooserCreateRow {
			return Done("")
		}
		hidden, caseMode := st.hidden, r.SearchCase()
		prefixes := r.noAutoListPrefixes()
		isDir := row.Fixed || strings.HasSuffix(row.Label, "/") || row.Opens
		return Pending(func(context.Context) (any, error) {
			if isDir {
				if !autoListableUnder(prefixes, path) {
					return "not listed on its own (path.no-auto-list)", nil
				}
				rows, err := listDir(path, "", PathPick{Parameter: Parameter{Path: PathAny}}, hidden, caseMode)
				if err != nil {
					return err.Error(), nil
				}
				if len(rows) == 0 {
					return "(empty)", nil
				}
				var b strings.Builder
				for i, r := range rows {
					if i == pathPreviewLines {
						fmt.Fprintf(&b, "… %d entries", len(rows))
						break
					}
					b.WriteString(r.Label + "\n")
				}
				return strings.TrimRight(b.String(), "\n"), nil
			}
			return filePreview(path), nil
		}).Label("preview " + displayPath(path))
	}
}

// filePreview is the head of a small text file, or what it is.
func filePreview(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return err.Error()
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err.Error()
	}
	head := fmt.Sprintf("%s · %s", filepath.Base(path), byteSize(info.Size()))
	buf := make([]byte, 2048)
	n, _ := io.ReadFull(f, buf)
	buf = buf[:n]
	if n == 0 {
		return head
	}
	if !utf8.Valid(buf) && !utf8.Valid(buf[:max(0, n-3)]) || strings.ContainsRune(string(buf), 0) {
		return head + "\n(binary)"
	}
	lines := strings.Split(string(buf), "\n")
	if len(lines) > pathPreviewLines {
		lines = lines[:pathPreviewLines]
	}
	return head + "\n\n" + strings.Join(lines, "\n")
}

func byteSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
}

// noAutoListPrefixes are the path.no-auto-list prefixes.
func (r *Runtime) noAutoListPrefixes() []string {
	v, _ := r.settingValue(PathNoAutoListSetting)
	prefixes, _ := v.([]string)
	return prefixes
}

// recentPathRows are the recent source's rows for paths: for a file pick a
// recent folder opens into the browse (a row that only opens), a file is
// picked. It stats each path, so it runs off the loop, and never touches
// a path under path.no-auto-list: that one is taken for a file.
func recentPathRows(paths []string, pick PathPick, recentDirs bool, prefixes []string, limit int) []ChooserRow {
	var rows []ChooserRow
	seen := map[string]bool{}
	for _, p := range paths {
		if limit > 0 && len(rows) >= limit {
			break
		}
		p = expandPath(p, pick.Start)
		if seen[p] {
			continue
		}
		seen[p] = true
		if pick.Files() && autoListableUnder(prefixes, p) {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				if recentDirs {
					label := displayPath(p)
					if !strings.HasSuffix(label, "/") {
						label += "/"
					}
					rows = append(rows, ChooserRow{ID: p, Label: label, Data: p, Opens: true, NoPick: true})
				}
				continue
			}
		}
		rows = append(rows, ChooserRow{ID: p, Label: displayPath(p), Data: p})
	}
	return rows
}

// recentList is the paths the recent source lists: the application's,
// ranked by use and age when it gives them (SetRecentUses), then the ones
// picked this run.
func (r *Runtime) recentList(pick PathPick) []string {
	mem := r.chooserMemory()
	var paths []string
	if mem.recentPaths != nil {
		paths = append(paths, mem.recentPaths(pick)...)
	}
	if mem.recentUses != nil {
		paths = frecentOrder(paths, mem.recentUses(pick), r.now())
	}
	return append(paths, mem.picked...)
}

// recentSource lists the application's recent paths, then the ones picked
// this run. For a file pick, recent directories open into the browse
// (Opens: true, NoPick: true) and cannot be submitted as files.
func (r *Runtime) recentSource(pick PathPick, st *pathState, use func(string) Result) ChooserSource {
	return ChooserSource{
		Name:  PathSourceRecent,
		Empty: "no recent paths",
		Load: func(string) Result {
			paths := r.recentList(pick)
			recentDirs := r.settingOn(PathRecentDirsSetting)
			prefixes := r.noAutoListPrefixes()
			if !pick.Files() || len(paths) == 0 {
				// No file system read decides these rows.
				return Done(recentPathRows(paths, pick, recentDirs, prefixes, 0))
			}
			return Pending(func(context.Context) (any, error) {
				return recentPathRows(paths, pick, recentDirs, prefixes, 0), nil
			}).Label("recent")
		},
	}
}

// browsable gives a source that cannot navigate (recent, walk, an
// application's known folders) the navigation every path source has: going
// up from a row browses the folder above it (and on up to the root), going
// into a folder row browses into it, and ~, / and the deepest place jump
// from anywhere. The source's own rows return with goto own.
func (r *Runtime) browsable(src ChooserSource, pick PathPick, st *pathState, use func(string) Result) ChooserSource {
	if src.Open != nil || src.Up != nil || src.UpFrom != nil {
		return src
	}
	b := &browsing{r: r, own: src, pick: pick, st: st, use: use}
	src.Label = b.label
	if b.own.Load != nil {
		src.Load = b.load
	}
	src.Open, src.CanOpen, src.UpFrom, src.Goto = b.open, b.canOpen, b.upFrom, b.goTo
	src.Landing, src.Current, src.Make, src.MakeLabel = b.landing, b.current, b.make, b.makeLabel
	if st.columns {
		src.Side, src.Preview = b.side, b.preview
	}
	return src
}

// browsing is a source that has gone into a folder: while browser is set
// the source's rows are that folder's.
type browsing struct {
	r       *Runtime
	own     ChooserSource
	pick    PathPick
	st      *pathState
	use     func(string) Result
	browser *ChooserSource
}

func (b *browsing) browseAt(path, left string) {
	n := b.r.newBrowseNav(path)
	n.open(path)
	n.left = left
	src := b.r.browseSourceAt(PathPick{Name: b.pick.Name, Start: path, Parameter: b.pick.Parameter}, b.st, b.use, n)
	b.browser = &src
}

func (b *browsing) label() string {
	if b.browser != nil {
		return strings.Replace(b.browser.Label(), PathSourceBrowse, b.own.Name, 1)
	}
	if b.own.Label != nil {
		return b.own.Label()
	}
	return b.own.Name
}

func (b *browsing) load(q string) Result {
	if b.browser != nil {
		return b.browser.Load(q)
	}
	return b.own.Load(q)
}

func (b *browsing) open(row ChooserRow) (string, bool) {
	if b.browser != nil {
		return b.browser.Open(row)
	}
	path, ok := row.Data.(string)
	if !ok {
		return "", false
	}
	b.browseAt(expandPath(path, b.pick.Start), "")
	return "", true
}

func (b *browsing) canOpen(row ChooserRow) bool {
	if b.browser != nil {
		return b.browser.CanOpen(row)
	}
	return folderRow(b.pick, row)
}

// upFrom goes up from the highlighted place: the folder above it; with
// none highlighted, the folder above where the pick started.
func (b *browsing) upFrom(row ChooserRow, q string) (string, bool) {
	if b.browser != nil {
		return b.browser.Up(q)
	}
	from := b.pick.Start
	if path, ok := row.Data.(string); ok && path != "" && row.ID != chooserCreateRow {
		from = expandPath(path, b.pick.Start)
	}
	parent := filepath.Dir(from)
	if parent == from {
		return "", false
	}
	b.browseAt(parent, from)
	return "", true
}

func (b *browsing) goTo(to string) (string, bool) {
	if b.browser != nil {
		if q, ok := b.browser.Goto(to); ok {
			return q, true
		}
		return b.leave(to)
	}
	n := b.r.newBrowseNav(b.pick.Start)
	if !n.goTo(to) {
		return "", false
	}
	b.browseAt(n.dir, "")
	return "", true
}

// leave ends the browsing when to asks for the source's own rows (goto
// own); the browser knows no such place.
func (b *browsing) leave(to string) (string, bool) {
	if to != PathGotoOwn {
		return "", false
	}
	b.browser = nil
	return "", true
}

func (b *browsing) landing() string {
	if b.browser != nil {
		return b.browser.Landing()
	}
	return ""
}

func (b *browsing) current() (ChooserRow, bool) {
	if b.browser != nil && b.browser.Current != nil {
		return b.browser.Current()
	}
	if b.own.Current != nil {
		return b.own.Current()
	}
	return ChooserRow{}, false
}

func (b *browsing) make(name string) Result {
	if b.browser == nil {
		return Failed(errors.New("open a folder first: nothing can be made here"))
	}
	return b.browser.Make(name)
}

func (b *browsing) makeLabel() string {
	if b.browser == nil {
		return ""
	}
	return b.browser.MakeLabel()
}

func (b *browsing) side() (string, func() Result) {
	if b.browser == nil {
		return "", func() Result { return Done(PickerSide{}) }
	}
	return b.browser.Side()
}

func (b *browsing) preview(row ChooserRow) Result {
	switch {
	case b.browser != nil:
		return b.browser.Preview(row)
	case b.own.Preview != nil:
		return b.own.Preview(row)
	}
	return b.r.pathPreview(b.st)(row)
}
