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
// Parameter.Path is "dir" (either with PathAny). Its form field opens the path chooser: a
// chooser whose sources are browse (the entries of one folder; enter or
// ctrl+l browses into a folder, alt+up or backspace on an empty query goes
// to the parent), recent (the application's list, then the paths picked
// this run), typed (completes the path being typed, as a shell does,
// and offers it when it exists, or when Parameter.New allows a path not
// there yet) and find (every path under the start folder, fuzzy-ranked:
// path_find.go; path.find off leaves it out), then the sources the
// application adds (AddPathSource: mtm's
// known folders, a project's; AddWalkPathSource: every folder under the
// roots it gives, path_walk.go); path.first names the ones listed first. Browse starts at the field's folder, else
// the parameter's Base, else as the setting path.start says. It never
// lists a folder under path.no-auto-list (external volumes) on its own:
// only once opened into or typed.

// PathChooserHint is what every path chooser's empty and no-match lines
// add (ChooserSpec.Hint), whichever source shows.
const PathChooserHint = "type a path to pick one"

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

// PathStyleSetting is the directory picker's style, whichever place opens
// it: list (the sources one at a time, the default), hybrid (sections on
// show together, path_hybrid.go) or columns (parent, folder and preview,
// chooser_columns.go). It is read at each open, so the Settings page
// switches it live.
const PathStyleSetting = "pickers.directory.style"

// The path picker's styles.
const (
	PathStyleList    = "list"
	PathStyleHybrid  = "hybrid"
	PathStyleColumns = "columns"
)

// pathStyles are the non-list styles the setting accepts; an unknown value is list.
var pathStyles = []string{PathStyleHybrid, PathStyleColumns}

// pathStyle is the style the setting names; an unknown value is list.
func (r *Runtime) pathStyle() string {
	v, _ := r.settingValue(PathStyleSetting)
	if s, ok := v.(string); ok && slices.Contains(pathStyles, s) {
		return s
	}
	return PathStyleList
}

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

// Files reports whether the pick wants a file only: a folder row opens
// but is not picked (PathFile, the default; PathAny takes either).
func (p PathPick) Files() bool { return !p.Dir() && p.Parameter.Path != PathAny }

// PathChooserSpec opens a path chooser. Choose takes the chosen path
// (absolute, cleaned).
type PathChooserSpec struct {
	Kind  string // default PathChooserKind
	Title string // default "Choose <name>"
	Pick  PathPick
	// Source names the source to open on (PathSourceBrowse, an
	// application's AddPathSource name); "" opens on the parameter's Start;
	// with neither (or a name this pick leaves out) it opens on the one
	// chooser.remember-source gives, else browse.
	Source string
	// Choose takes the path; nil returns it as the result.
	Choose func(path string) Result
	// Query is the text typed into the search line when it opens.
	Query string
	// Many lets the user mark several paths (the parameter is a list):
	// ChooseMany takes them, else Choose takes each in turn.
	Many       bool
	ChooseMany func(paths []string) Result
	// whereFrom adds relation graph scopes for a parameter ask, leaving
	// standalone directory browsing and its remembered source unchanged.
	whereFrom bool
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

// PathAutoListable reports whether a folder may be listed without the user
// asking for it: false under a path.no-auto-list prefix. It reads no file
// system. An application scanning folders of its own (a known-folder scan)
// asks it before it lists a base on an external volume.
func (r *Runtime) PathAutoListable(dir string) bool { return r.autoListable(dir) }

func (r *Runtime) showHidden() bool {
	v, _ := r.settingValue(PathHiddenSetting)
	return v == true
}

// PathStartSource is the source a pick opens on: spec's, else the one the
// parameter declares (Parameter.Start); "" leaves it to
// chooser.remember-source. A name no source answers to falls back to that
// quietly (the chooser ignores a source it does not have).
func (r *Runtime) PathStartSource(pick PathPick, source string) string {
	if source != "" {
		return source
	}
	return strings.TrimSpace(pick.Parameter.Start)
}

// pathStyleDef is what a style of the directory picker is, as data: the
// whether it opens in normal mode (else the search line only), whether it draws
// columns, whether enter picks a folder row (and l goes into it), whether
// one sectioned source stands for the browse and recent sources, and the
// source it opens on when nothing names one.
type pathStyleDef struct {
	Name string
	// StartNormal: the style opens in normal mode, its browse keys at once.
	StartNormal bool
	Columns     bool
	EnterPicks  bool
	Sections    bool
	Start       string
}

var pathStyleDefs = []pathStyleDef{
	{Name: PathStyleList},
	{Name: PathStyleHybrid, StartNormal: true, EnterPicks: true, Sections: true, Start: PathSourceHybrid},
	{Name: PathStyleColumns, StartNormal: true, EnterPicks: true, Columns: true, Start: PathSourceBrowse},
}

// pathStyleOf is the style definition named name; an unknown name is the
// first (list).
func pathStyleOf(name string) pathStyleDef {
	i := slices.IndexFunc(pathStyleDefs, func(d pathStyleDef) bool { return d.Name == name })
	return pathStyleDefs[max(i, 0)]
}

// pathTitle is the chooser's title for a pick.
func pathTitle(pick PathPick) string {
	what := "file"
	if pick.Dir() {
		what = "folder"
	} else if !pick.Files() {
		what = "path"
	}
	if pick.Name != "" {
		what = pick.Name
	}
	return "Choose " + what
}

// rememberPicked puts path first among the paths picked this run.
func (r *Runtime) rememberPicked(path string) string {
	path = filepath.Clean(path)
	mem := r.chooserMemory()
	mem.picked = slices.DeleteFunc(mem.picked, func(p string) bool { return p == path })
	mem.picked = append([]string{path}, mem.picked...)
	if len(mem.picked) > 50 {
		mem.picked = mem.picked[:50]
	}
	return path
}

// pathSourceSet is the sources of one open: browse, recent and typed, find
// while path.find is on (or asked for), then the application's; each made
// able to go up and into folders (browsable); the hybrid style swaps browse
// and recent for its one sectioned source.
func (r *Runtime) pathSourceSet(spec PathChooserSpec, pick PathPick, st *pathState, def pathStyleDef, use func(string) Result) []ChooserSource {
	sources := []ChooserSource{r.browseSource(pick, st, use), r.recentSource(pick, st, use), r.typedSource(pick, st, use)}
	if find := r.findSource(pick); r.settingOn(PathFindSetting) || find.Name == spec.Source {
		sources = append(sources, find)
	}
	for _, one := range r.chooserMemory().pathSources {
		if src, ok := one.fn(pick); ok {
			if src.Name == "" {
				src.Name = one.name
			}
			sources = append(sources, src)
		}
	}
	var sections []ChooserSource
	for i := range sources {
		if sources[i].Section != "" {
			sections = append(sections, sources[i])
		}
		sources[i] = r.browsable(sources[i], pick, st, use)
	}
	sources = orderPathSources(sources, r.settingList(PathFirstSetting))
	if def.Sections {
		one := r.hybridSource(pick, st, use, sections)
		sources = slices.DeleteFunc(sources, func(s ChooserSource) bool { return slices.Contains(one.Stands, s.Name) })
		sources = append([]ChooserSource{one}, sources...)
	}
	return sources
}

// pathToggles are the options picker.toggle switches on a path picker.
func pathToggles(st *pathState) []PickerToggle {
	return []PickerToggle{{Name: "hidden", Flip: func() string {
		st.hidden = !st.hidden
		if st.hidden {
			return "hidden files shown"
		}
		return "hidden files hidden"
	}}}
}

// chooseMany takes the marked rows of a path picker as paths.
func (r *Runtime) chooseMany(spec PathChooserSpec) func([]ChooserRow) Result {
	return func(rows []ChooserRow) Result {
		var paths []string
		for _, row := range rows {
			if path, ok := row.Data.(string); ok && path != "" {
				paths = append(paths, r.rememberPicked(path))
			}
		}
		if spec.ChooseMany != nil {
			return spec.ChooseMany(paths)
		}
		if spec.Choose == nil {
			return Done(paths)
		}
		res := Done(nil)
		for _, path := range paths {
			if res = spec.Choose(path); res.err != nil {
				return res
			}
		}
		return res
	}
}

// OpenPathChooser opens the path chooser (browse, recent, typed, then the
// application's sources) for spec.Pick, in the style pickers.directory.style
// names.
func (r *Runtime) OpenPathChooser(spec PathChooserSpec) (*Chooser, error) {
	pick := spec.Pick
	pick.Start = r.PathStart(pick)
	kind := spec.Kind
	if kind == "" {
		kind = PathChooserKind
	}
	title := spec.Title
	if title == "" {
		title = pathTitle(pick)
	}
	def := pathStyleOf(r.pathStyle())
	st := &pathState{hidden: r.showHidden(), columns: def.Columns, enterPicks: def.EnterPicks}
	use := func(path string) Result {
		path = r.rememberPicked(path)
		if spec.Choose == nil {
			return Done(path)
		}
		return spec.Choose(path)
	}
	start := r.PathStartSource(pick, spec.Source)
	if start == "" {
		start = def.Start
	}
	chooser := ChooserSpec{
		Kind:    kind,
		Title:   title,
		Sources: r.pathSourceSet(spec, pick, st, def, use),
		Source:  start,
		Hint:    PathChooserHint,
		Choose: func(row ChooserRow) Result {
			path, ok := row.Data.(string)
			if !ok || path == "" {
				return Failed(fmt.Errorf("%s is not a path", row.Label))
			}
			return use(path)
		},
		CreateRow:   true,
		Many:        spec.Many,
		Columns:     def.Columns,
		StartNormal: def.StartNormal,
		Toggles:     pathToggles(st),
	}
	if spec.whereFrom && r.subjectAsks() && r.settingOn(PickerScopesSetting) {
		chooser.Sources, chooser.Source = r.pathSubjectScopes(chooser.Sources, pick, r.contextOf(r.BaseFocus()), chooser.Source)
		chooser.Scoped = true
		chooser.NormalMode = true
	}
	if spec.Many {
		chooser.ChooseMany = r.chooseMany(spec)
	}
	c, err := r.OpenChooser(chooser)
	if err == nil && spec.Query != "" {
		c.SetQuery(spec.Query)
	}
	return c, err
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
			dirs = append(dirs, ChooserRow{ID: full, Label: name + "/", Data: full, Opens: true, NoPick: pick.Files()})
		} else if !pick.Dir() {
			files = append(files, ChooserRow{ID: full, Label: name, Data: full})
		}
	}
	return append(dirs, files...), nil
}

// typedSource completes the path being typed (relative to the start
// folder): the typed path itself when it exists, then the entries of its
// folder that start with its last part, then the path as new when it may be. Opening a folder
// row types it with a trailing /; up drops the last part.
func (r *Runtime) typedSource(pick PathPick, st *pathState, use func(string) Result) ChooserSource {
	return ChooserSource{
		Name:   PathSourceTyped,
		Live:   true,
		Empty:  "type a path",
		Claims: pathQuery,
		Load: func(query string) Result {
			if strings.TrimSpace(query) == "" {
				return Done([]ChooserRow(nil))
			}
			hidden, caseMode := st.hidden, r.SearchCase()
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
			// The folder above what is typed, to the root: the last part
			// goes; a lone part (~, a name under the start) goes up to the
			// absolute folder above it.
			q := strings.TrimSuffix(query, "/")
			switch {
			case query == "":
				return typedParent(pick.Start)
			case q == "":
				return "", false
			case q == "~" || q == ".":
				return typedParent(expandPath(q, pick.Start))
			case q == "..":
				return typedParent(expandPath(q, pick.Start))
			}
			i := strings.LastIndex(q, "/")
			if i < 0 {
				return "", true
			}
			return q[:i+1], true
		},
	}
}

// typedParent is the typed query for the folder above dir (an absolute
// path with a trailing /); false at the root.
func typedParent(dir string) (string, bool) {
	parent := filepath.Dir(dir)
	if parent == dir {
		return "", false
	}
	return strings.TrimSuffix(parent, "/") + "/", true
}

// pathQuery reports whether query is written as a path ("/", "~", "./",
// "../"), so the typed source answers it from any source; a plain word
// is not, and keeps filtering the list.
func pathQuery(query string) bool {
	q := query
	return strings.HasPrefix(q, "/") || q == "~" || strings.HasPrefix(q, "~/") || strings.HasPrefix(q, "./") || strings.HasPrefix(q, "../")
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
		if !pick.Files() {
			// The typed folder itself is picked by enter; the folders
			// under it below it open.
			rows = append(rows, ChooserRow{ID: path, Label: query, Data: path})
		}
	case err == nil:
		if !pick.Dir() {
			rows = append(rows, ChooserRow{ID: path, Label: query, Data: path})
		}
	case errors.Is(err, os.ErrNotExist) && pick.Parameter.New && (pick.Dir() || !strings.HasSuffix(query, "/")):
		// New offers the path as new: a folder pick whatever its parent
		// (a missing folder is recorded, not refused), a file pick when
		// the folder to write it in is there.
		if pick.Dir() {
			fresh = append(fresh, ChooserRow{ID: path, Label: "new " + query, Data: path})
		} else if parent, err := os.Stat(filepath.Dir(path)); err == nil && parent.IsDir() {
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
	case err == nil && info.IsDir() && pick.Files():
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
