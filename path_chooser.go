package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// A path parameter (Parameter.Type "path") names a file, or a folder when
// Parameter.Path is "dir". Its form field opens the path chooser: a
// chooser whose sources are browse (the entries of one folder; enter or
// ctrl+l browses into a folder, alt+up or backspace on an empty query goes
// to the parent), recent (the application's list, then the paths picked
// this run) and typed (completes the path being typed, as a shell does,
// and offers it when it exists, or when Parameter.New allows a path not
// there yet), then the sources the application adds (AddPathSource: mtm's
// known folders, a project's; AddWalkPathSource: every folder under the
// roots it gives, path_walk.go); path.first names the ones listed first. Browse starts at the field's folder, else
// the parameter's Base, else as the setting path.start says. It never
// lists a folder under path.no-auto-list (external volumes) on its own:
// only once opened into or typed.

// PathParameterType is the Parameter.Type of a path.
const PathParameterType = "path"

// Path variants (Parameter.Path).
const (
	PathFile = "file" // the default
	PathDir  = "dir"
)

// PathChooserKind is the path chooser's modal kind; the source last used
// is remembered under it.
const PathChooserKind = "path-chooser"

// Path settings: rows of settings/ochazuke.json.
const (
	PathStartSetting      = "path.start"
	PathFieldSetting      = "path.field"
	PathHiddenSetting     = "path.hidden"
	PathNoAutoListSetting = "path.no-auto-list"
	PathRecentDirsSetting = "path.recent-dirs"
)

// The path chooser's own sources, in order.
const (
	PathSourceBrowse = "browse"
	PathSourceRecent = "recent"
	PathSourceTyped  = "typed"
)

// PathPick describes what a path chooser is open for.
type PathPick struct {
	// Name is the parameter (or field) name; Parameter its description
	// (Path, Base, New).
	Name      string
	Parameter Parameter
	// Value is the field's value when the chooser opened.
	Value string
	// Start is the folder browse starts in (PathStart), filled in by
	// OpenPathChooser.
	Start string
}

// Dir reports whether the pick wants a folder.
func (p PathPick) Dir() bool { return p.Parameter.Path == PathDir }

// PathChooserSpec opens a path chooser. Choose takes the chosen path
// (absolute, cleaned).
type PathChooserSpec struct {
	Kind  string // default PathChooserKind
	Title string // default "Choose <name>"
	Pick  PathPick
	// Source names the source to open on (PathSourceBrowse, an
	// application's AddPathSource name); "" (or a source this pick leaves
	// out) opens on the one chooser.remember-source gives, else browse.
	Source string
	// Choose takes the path; nil returns it as the result.
	Choose func(path string) Result
}

type namedPathSource struct {
	name string
	fn   func(PathPick) (ChooserSource, bool)
}

// AddPathSource adds a source to every path chooser, after browse, recent
// and typed, in the order added; adding a name again replaces it. fn
// builds the source for one open (false leaves it out of that one). A
// row's Data is the path a pick takes (a string).
func (r *Runtime) AddPathSource(name string, fn func(PathPick) (ChooserSource, bool)) {
	s := r.chooserMemory()
	for i, one := range s.pathSources {
		if one.name == name {
			s.pathSources[i].fn = fn
			return
		}
	}
	s.pathSources = append(s.pathSources, namedPathSource{name, fn})
}

// SetPathBase gives the folder path.start=base starts in when the
// parameter names none (the open file's folder); "" falls back to the
// working directory.
func (r *Runtime) SetPathBase(fn func(PathPick) string) { r.chooserMemory().pathBase = fn }

// SetRecentPaths gives the recent source's paths, newest first; the paths
// picked this run follow them.
func (r *Runtime) SetRecentPaths(fn func(PathPick) []string) {
	r.chooserMemory().recentPaths = fn
}

// RecentPaths are the paths picked through a path chooser this run, newest
// first.
func (r *Runtime) RecentPaths() []string { return slices.Clone(r.chooserMemory().picked) }

// PathStart is the folder browse starts in for pick: the field's value's
// folder (the value itself for a folder; a relative value is under Base),
// else the parameter's Base, else what path.start names. It reads no file system.
func (r *Runtime) PathStart(pick PathPick) string {
	base := ""
	if b := strings.TrimSpace(pick.Parameter.Base); b != "" {
		base = expandPath(b, "")
	}
	if v := strings.TrimSpace(pick.Value); v != "" {
		// A relative value is under the base, as it was picked there.
		p := expandPath(v, base)
		if pick.Dir() || strings.HasSuffix(v, "/") {
			return p
		}
		return filepath.Dir(p)
	}
	if base != "" {
		return base
	}
	cwd, _ := os.Getwd()
	switch v, _ := r.settingValue(PathStartSetting); v {
	case "home":
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	case "cwd":
	default:
		if fn := r.chooserMemory().pathBase; fn != nil {
			if b := fn(pick); b != "" {
				return expandPath(b, "")
			}
		}
	}
	return cwd
}

// expandPath is an absolute, cleaned path: ~ is the home folder, a
// relative path is under base (the working directory when base is "").
func expandPath(p, base string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	if !filepath.IsAbs(p) {
		if base == "" {
			base, _ = os.Getwd()
		}
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

// displayPath shows a path with the home folder as ~.
func displayPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if strings.HasPrefix(p, home+string(filepath.Separator)) {
			return "~" + strings.TrimPrefix(p, home)
		}
	}
	return p
}

// autoListable reports whether a folder may be listed without the user
// asking for it: false under a path.no-auto-list prefix. It reads no file
// system, so an external volume is never touched.
func (r *Runtime) autoListable(dir string) bool {
	v, _ := r.settingValue(PathNoAutoListSetting)
	prefixes, _ := v.([]string)
	// The prefix itself (/Volumes) lists the mounts; a folder under it is
	// a volume.
	return autoListableUnder(prefixes, dir)
}

func (r *Runtime) showHidden() bool {
	v, _ := r.settingValue(PathHiddenSetting)
	return v == true
}

// OpenPathChooser opens the path chooser (browse, recent, typed, then the
// application's sources) for spec.Pick.
func (r *Runtime) OpenPathChooser(spec PathChooserSpec) (*Chooser, error) {
	pick := spec.Pick
	pick.Start = r.PathStart(pick)
	kind := spec.Kind
	if kind == "" {
		kind = PathChooserKind
	}
	title := spec.Title
	if title == "" {
		what := "file"
		if pick.Dir() {
			what = "folder"
		}
		if pick.Name != "" {
			what = pick.Name
		}
		title = "Choose " + what
	}
	use := func(path string) Result {
		path = filepath.Clean(path)
		mem := r.chooserMemory()
		mem.picked = slices.DeleteFunc(mem.picked, func(p string) bool { return p == path })
		mem.picked = append([]string{path}, mem.picked...)
		if len(mem.picked) > 50 {
			mem.picked = mem.picked[:50]
		}
		if spec.Choose == nil {
			return Done(path)
		}
		return spec.Choose(path)
	}
	sources := []ChooserSource{r.browseSource(pick, use), r.recentSource(pick), r.typedSource(pick, use)}
	for _, one := range r.chooserMemory().pathSources {
		if src, ok := one.fn(pick); ok {
			if src.Name == "" {
				src.Name = one.name
			}
			sources = append(sources, src)
		}
	}
	sources = orderPathSources(sources, r.settingList(PathFirstSetting))
	return r.OpenChooser(ChooserSpec{
		Kind:    kind,
		Title:   title,
		Sources: sources,
		Source:  spec.Source,
		Choose: func(row ChooserRow) Result {
			path, ok := row.Data.(string)
			if !ok || path == "" {
				return Failed(fmt.Errorf("%s is not a path", row.Label))
			}
			return use(path)
		},
		CreateRow: true,
	})
}

// orderPathSources puts the sources path.first names first, in its
// order; the rest keep theirs.
func orderPathSources(sources []ChooserSource, first []string) []ChooserSource {
	var head []ChooserSource
	for _, name := range first {
		if i := slices.IndexFunc(sources, func(s ChooserSource) bool { return s.Name == name }); i >= 0 {
			head = append(head, sources[i])
			sources = slices.Delete(sources, i, i+1)
		}
	}
	return append(head, sources...)
}

// listDir reads a folder's entries for a chooser: folders first (with a
// trailing /, Opens; NoPick when the pick wants a file), then files (left
// out when it wants a folder), each by name, hidden ones only when
// hidden; only names starting with prefix when it is set, compared as
// the search.case value caseMode says (CaseFor: smart ignores case unless
// the prefix holds a capital).
func listDir(dir, prefix string, pick PathPick, hidden bool, caseMode string) ([]ChooserRow, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs, files []ChooserRow
	cm := CaseFor(caseMode, prefix)
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !hidden && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if prefix != "" && !cm.HasPrefix(name, prefix) {
			continue
		}
		full := filepath.Join(dir, name)
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(full); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			dirs = append(dirs, ChooserRow{ID: full, Label: name + "/", Data: full, Opens: true, NoPick: !pick.Dir()})
		} else if !pick.Dir() {
			files = append(files, ChooserRow{ID: full, Label: name, Data: full})
		}
	}
	return append(dirs, files...), nil
}

// browseSource lists one folder; opening a folder row browses into it, up
// goes to the parent. A folder chooser has a fixed "use <folder>" row.
func (r *Runtime) browseSource(pick PathPick, use func(string) Result) ChooserSource {
	dir := pick.Start
	asked := false // the user opened or went up into dir: list it whatever it is
	src := ChooserSource{
		Name:  PathSourceBrowse,
		Label: func() string { return PathSourceBrowse + " " + displayPath(dir) },
	}
	src.Load = func(string) Result {
		at := dir
		if !asked && !r.autoListable(at) {
			return Failed(fmt.Errorf("not listed on its own (path.no-auto-list): open it or type a path; %s", displayPath(at)))
		}
		hidden, caseMode := r.showHidden(), r.SearchCase()
		return Pending(func(context.Context) (any, error) {
			rows, err := listDir(at, "", pick, hidden, caseMode)
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
		dir, asked = path, true
		return "", true
	}
	src.Up = func(string) (string, bool) {
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir, asked = parent, true
		return "", true
	}
	if pick.Parameter.New && !pick.Dir() {
		src.Create = func(query string) Result { return use(expandPath(query, dir)) }
		src.CreateLabel = func(query string) string { return "new " + displayPath(expandPath(query, dir)) }
	}
	return src
}

// recentSource lists the application's recent paths, then the ones picked
// this run. For a file pick, recent directories open into browseSource
// (Opens: true, NoPick: true) and cannot be submitted as files.
func (r *Runtime) recentSource(pick PathPick) ChooserSource {
	var (
		browser *ChooserSource
		depth   int
	)
	src := ChooserSource{
		Name:  PathSourceRecent,
		Empty: "no recent paths",
		Label: func() string {
			if browser != nil && browser.Label != nil {
				return strings.Replace(browser.Label(), PathSourceBrowse, PathSourceRecent, 1)
			}
			return PathSourceRecent
		},
	}
	src.Load = func(query string) Result {
		if browser != nil && browser.Load != nil {
			return browser.Load(query)
		}
		mem := r.chooserMemory()
		var paths []string
		if mem.recentPaths != nil {
			paths = append(paths, mem.recentPaths(pick)...)
		}
		paths = append(paths, mem.picked...)
		var rows []ChooserRow
		recentDirsOn := r.settingOn(PathRecentDirsSetting)
		seen := map[string]bool{}
		for _, p := range paths {
			p = expandPath(p, pick.Start)
			if seen[p] {
				continue
			}
			seen[p] = true
			if !pick.Dir() {
				if fi, err := os.Stat(p); err == nil && fi.IsDir() {
					if recentDirsOn {
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
		return Done(rows)
	}
	src.Open = func(row ChooserRow) (string, bool) {
		if browser != nil && browser.Open != nil {
			if q, ok := browser.Open(row); ok {
				depth++
				return q, true
			}
			return "", false
		}
		path, ok := row.Data.(string)
		if !ok {
			return "", false
		}
		b := r.browseSource(PathPick{Start: path, Parameter: pick.Parameter}, nil)
		q, ok := b.Open(ChooserRow{Data: path})
		if !ok {
			return "", false
		}
		browser = &b
		depth = 0
		return q, true
	}
	src.Up = func(query string) (string, bool) {
		if browser == nil {
			return "", false
		}
		if depth == 0 {
			browser = nil
			return "", true
		}
		if browser.Up != nil {
			if q, ok := browser.Up(query); ok {
				depth--
				return q, true
			}
		}
		browser = nil
		return "", true
	}
	return src
}

// typedSource completes the path being typed (relative to the start
// folder): the typed path itself when it exists, then the entries of its
// folder that start with its last part, then the path as new when it may be. Opening a folder
// row types it with a trailing /; up drops the last part.
func (r *Runtime) typedSource(pick PathPick, use func(string) Result) ChooserSource {
	return ChooserSource{
		Name:  PathSourceTyped,
		Live:  true,
		Empty: "type a path",
		Load: func(query string) Result {
			if strings.TrimSpace(query) == "" {
				return Done([]ChooserRow(nil))
			}
			hidden, caseMode := r.showHidden(), r.SearchCase()
			return Pending(func(context.Context) (any, error) {
				return typedRows(query, pick, hidden, caseMode), nil
			}).Label("complete " + query)
		},
		Open: func(row ChooserRow) (string, bool) {
			if _, ok := row.Data.(string); !ok {
				return "", false
			}
			return strings.TrimSuffix(row.Label, "/") + "/", true
		},
		Up: func(query string) (string, bool) {
			q := strings.TrimSuffix(query, "/")
			if q == "" {
				return "", false
			}
			i := strings.LastIndex(q, "/")
			if i < 0 {
				return "", true
			}
			return q[:i+1], true
		},
	}
}

// typedRows are the typed source's rows for query; caseMode is the
// search.case setting, applied to the name being completed (the part after
// the last /), so a capital in a folder already typed never makes the
// completion case-sensitive.
func typedRows(query string, pick PathPick, hidden bool, caseMode string) []ChooserRow {
	path := expandPath(query, pick.Start)
	dir, prefix := filepath.Dir(path), filepath.Base(path)
	// Rows read as typed: the query's folder part as written ("sub/",
	// "~/proj/", an absolute path), then the entry's name.
	typed := query[:strings.LastIndex(query, "/")+1]
	if strings.HasSuffix(query, "/") {
		dir, prefix = path, ""
	}
	var rows []ChooserRow
	var fresh []ChooserRow // the new path: after the completions, so enter takes an existing entry first
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		if pick.Dir() {
			rows = append(rows, ChooserRow{ID: path, Label: query, Data: path, Opens: true})
		}
	case err == nil:
		if !pick.Dir() {
			rows = append(rows, ChooserRow{ID: path, Label: query, Data: path})
		}
	case errors.Is(err, os.ErrNotExist) && pick.Parameter.New && !strings.HasSuffix(query, "/"):
		if parent, err := os.Stat(filepath.Dir(path)); err == nil && parent.IsDir() {
			fresh = append(fresh, ChooserRow{ID: path, Label: "new " + query, Data: path})
		}
	}
	more, _ := listDir(dir, prefix, pick, hidden, caseMode)
	for _, row := range more {
		if row.ID == path {
			continue
		}
		row.Label = typed + row.Label
		rows = append(rows, row)
		if len(rows) >= 200 {
			break
		}
	}
	rows = append(rows, fresh...)
	if len(rows) == 0 {
		// Nothing else stands for it: the typed path is still a row, and
		// one that cannot be used says why (R7.8).
		why := pathUnusable(path, pick)
		rows = append(rows, ChooserRow{ID: path, Label: query + " · " + why, Data: path, Refuse: why})
	}
	return rows
}

// pathUnusable says why path cannot be picked: the outermost folder on it
// that does not exist, a part that is not a folder, or a path of the wrong
// kind for the pick.
func pathUnusable(path string, pick PathPick) string {
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir() && !pick.Dir():
		return displayPath(path) + " is a folder"
	case err == nil && !info.IsDir() && pick.Dir():
		return displayPath(path) + " is not a folder"
	case err == nil:
		return displayPath(path) + " cannot be chosen here"
	case !errors.Is(err, os.ErrNotExist):
		return err.Error()
	}
	missing := path
	for dir := filepath.Dir(path); dir != missing; dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				return displayPath(dir) + " is not a folder"
			}
			break
		}
		missing = dir
	}
	return displayPath(missing) + " does not exist"
}
