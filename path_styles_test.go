package ochazuke

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke/keytest"
)

// The directory picker's styles (setting pickers.directory.style): list (the
// picker as it was), hybrid and columns. Every behaviour a path picker
// has must hold in each of them.

var allPathStyles = []string{PathStyleList, PathStyleHybrid, PathStyleColumns}

// upKey is the key that goes up in each style: the list style types in its
// search line, so it uses alt+up; the browsing styles use h.
func upKey(style string) string {
	if style == PathStyleList {
		return "alt+up"
	}
	return "h"
}

// intoKey is the key that goes into the highlighted folder.
func intoKey(style string) string {
	if style == PathStyleList {
		return "ctrl+l"
	}
	return "l"
}

// filterKeys opens the search line in a style that rests in browse mode.
func filterKeys(style string) []string {
	if style == PathStyleList {
		return nil
	}
	return []string{"/"}
}

func styleFixture(t *testing.T, style string, pick PathPick, setup func(*Runtime), spec func(*PathChooserSpec)) (*Runtime, *Model, *Chooser, *[]string) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 110, 30
	m.Appearance.Theme = "mono"
	r.EnsureConfigSettings()
	if err := r.Settings().Set(PathStyleSetting, style); err != nil {
		t.Fatal(err)
	}
	if setup != nil {
		setup(r)
	}
	var chosen []string
	ps := PathChooserSpec{Pick: pick, Choose: func(p string) Result {
		chosen = append(chosen, p)
		return Done(p)
	}}
	if spec != nil {
		spec(&ps)
	}
	c, err := r.OpenPathChooser(ps)
	if err != nil {
		t.Fatal(err)
	}
	landLoads(m, r.PendingCommands())
	return r, m, c, &chosen
}

func styleDirPick(base string) PathPick {
	return PathPick{Name: "dir", Parameter: Parameter{Type: "path", Path: PathDir, Base: base}}
}

func styleFilePick(base string) PathPick {
	return PathPick{Name: "file", Parameter: Parameter{Type: "path", Base: base}}
}

// shownDir is the folder a style shows, as its title says it.
func shownDir(c *Chooser) string {
	title := c.Title()
	for _, name := range []string{PathSourceBrowse, PathSourceHybrid, PathSourceRecent, PathSourceWalk, "known"} {
		if i := strings.Index(title, "["+name+" "); i >= 0 {
			rest := title[i+len(name)+2:]
			return rest[:strings.Index(rest, "]")]
		}
	}
	return title
}

func rowLabels(c *Chooser) []string {
	var out []string
	for _, row := range c.Matches() {
		out = append(out, row.Label)
	}
	return out
}

// Never confined: from any start, in every style, going up one folder at a
// time reaches / and stops there; any readable folder below can be entered.
func TestPathStylesGoUpToRoot(t *testing.T) {
	root := pathTree(t)
	start := filepath.Join(root, "sub", "deep")
	for _, style := range allPathStyles {
		t.Run(style, func(t *testing.T) {
			_, m, c, _ := styleFixture(t, style, styleDirPick(start), nil, nil)
			want := start
			for want != "/" {
				if got := shownDir(c); got != displayPath(want) {
					t.Fatalf("showing %q, want %q", got, displayPath(want))
				}
				pressLand(m, upKey(style))
				want = filepath.Dir(want)
			}
			if got := shownDir(c); got != "/" {
				t.Fatalf("at the end showing %q, want /", got)
			}
			pressLand(m, upKey(style))
			if got := shownDir(c); got != "/" {
				t.Fatalf("up from / moved to %q", got)
			}
		})
	}
}

// A source is a starting place, never a wall: from a row of the recent
// list, of the walk, of an application's known folders, going up reaches
// the folder above it and on to /.
func TestPathSourcesAreNotWalls(t *testing.T) {
	root := pathTree(t)
	deep := filepath.Join(root, "sub", "deep")
	setup := func(r *Runtime) {
		r.SetRecentPaths(func(PathPick) []string { return []string{deep} })
		r.AddWalkPathSource(func(PathPick) PathWalk { return PathWalk{Roots: []PathRoot{{Path: filepath.Join(root, "sub")}}} })
		r.AddPathSource("known", func(PathPick) (ChooserSource, bool) {
			return ChooserSource{Name: "known", Section: "Known", Load: func(string) Result {
				return Done([]ChooserRow{{ID: deep, Label: displayPath(deep), Data: deep}})
			}}, true
		})
	}
	for _, style := range allPathStyles {
		for _, source := range []string{PathSourceRecent, PathSourceWalk, "known"} {
			t.Run(style+"/"+source, func(t *testing.T) {
				_, m, c, _ := styleFixture(t, style, styleDirPick(root), setup, func(ps *PathChooserSpec) { ps.Source = source })
				if style == PathStyleHybrid {
					// the sections are one list: highlight the row of the source's section
					pressLand(m, "]")
				}
				landAll(m, nil)
				if c.Source() != source && style != PathStyleHybrid {
					t.Fatalf("source %q, want %q", c.Source(), source)
				}
				// The first row is the deep folder (walk lists the root first: its row is sub itself).
				pressLand(m, upKey(style))
				first := shownDir(c)
				if first == "" || first == c.Title() {
					t.Fatalf("up from a %s row did not browse: %q", source, c.Title())
				}
				for i := 0; i < 40 && shownDir(c) != "/"; i++ {
					pressLand(m, upKey(style))
				}
				if got := shownDir(c); got != "/" {
					t.Fatalf("never reached / from %s: at %q", source, got)
				}
			})
		}
	}
}

// A typed or pasted absolute path always works, including one outside every
// source.
func TestPathStylesTakeAnyTypedPath(t *testing.T) {
	root := pathTree(t)
	outside := t.TempDir()
	outside, _ = filepath.EvalSymlinks(outside)
	for _, style := range allPathStyles {
		t.Run(style, func(t *testing.T) {
			_, m, c, chosen := styleFixture(t, style, styleDirPick(filepath.Join(root, "sub")), func(r *Runtime) {
				r.SetRecentPaths(func(PathPick) []string { return []string{root} })
			}, nil)
			pressLand(m, filterKeys(style)...)
			typeText(m, outside)
			if c.Source() != PathSourceTyped {
				t.Fatalf("source %q after typing a path", c.Source())
			}
			pressLand(m, "enter")
			if !slices.Equal(*chosen, []string{outside}) {
				t.Fatalf("chosen %v, want %v", *chosen, outside)
			}
		})
	}
}

// Descending is not picking: in a folder pick l (ctrl+l in the list) goes into the
// highlighted folder, u (alt+u) uses the folder shown, and enter in the new
// styles picks the highlighted folder (the list style's enter goes into it).
func TestPathStylesUseThisFolderIsNotDescend(t *testing.T) {
	root := pathTree(t)
	for _, style := range allPathStyles {
		t.Run(style, func(t *testing.T) {
			_, m, c, chosen := styleFixture(t, style, styleDirPick(root), nil, nil)
			pressLand(m, intoKey(style)) // into sub/
			if got := shownDir(c); got != displayPath(filepath.Join(root, "sub")) {
				t.Fatalf("after into: %q", got)
			}
			if len(*chosen) != 0 {
				t.Fatalf("into picked %v", *chosen)
			}
			if style == PathStyleList {
				pressLand(m, "alt+u")
			} else {
				pressLand(m, "u")
			}
			if !slices.Equal(*chosen, []string{filepath.Join(root, "sub")}) {
				t.Fatalf("use this folder chose %v", *chosen)
			}
		})
	}
	t.Run("enter picks the highlighted folder in the new styles", func(t *testing.T) {
		for _, style := range []string{PathStyleHybrid, PathStyleColumns} {
			_, m, _, chosen := styleFixture(t, style, styleDirPick(root), nil, nil)
			pressLand(m, "enter")
			if !slices.Equal(*chosen, []string{filepath.Join(root, "sub")}) {
				t.Fatalf("%s: enter chose %v", style, *chosen)
			}
		}
	})
}

// File mode: enter on a file picks it, on a folder goes into it; u takes the
// highlighted file and refuses a folder.
func TestPathStylesFileMode(t *testing.T) {
	root := pathTree(t)
	for _, style := range allPathStyles {
		t.Run(style, func(t *testing.T) {
			_, m, c, chosen := styleFixture(t, style, styleFilePick(root), nil, nil)
			pressLand(m, "enter") // sub/ opens
			if got := shownDir(c); got != displayPath(filepath.Join(root, "sub")) {
				t.Fatalf("enter on a folder in a file pick: %q", got)
			}
			pressLand(m, "down", "enter") // c.json
			if !slices.Equal(*chosen, []string{filepath.Join(root, "sub", "c.json")}) {
				t.Fatalf("chosen %v", *chosen)
			}
		})
	}
}

// Filtering narrows the rows; hidden files show with the toggle.
func TestPathStylesFilterAndHidden(t *testing.T) {
	root := pathTree(t)
	for _, style := range allPathStyles {
		t.Run(style, func(t *testing.T) {
			_, m, c, _ := styleFixture(t, style, styleFilePick(root), nil, nil)
			if slices.Contains(rowLabels(c), ".hidden") {
				t.Fatal("hidden file shown before the toggle")
			}
			if style == PathStyleList {
				pressLand(m, "alt+.")
			} else {
				pressLand(m, ".")
			}
			if !slices.Contains(rowLabels(c), ".hidden") {
				t.Fatalf("toggle did not show hidden files: %v", rowLabels(c))
			}
			pressLand(m, filterKeys(style)...)
			typeText(m, "b.t")
			var plain []string
			for _, l := range rowLabels(c) {
				if !strings.HasPrefix(l, "Browse") {
					plain = append(plain, l)
				}
			}
			if !slices.Equal(plain, []string{"b.txt"}) {
				t.Fatalf("filter b.t kept %v", plain)
			}
		})
	}
}

// A new folder is made where the picker is and the picker goes into it.
func TestPathStylesMakeFolder(t *testing.T) {
	root := pathTree(t)
	for _, style := range allPathStyles {
		t.Run(style, func(t *testing.T) {
			_, m, c, _ := styleFixture(t, style, styleDirPick(root), nil, nil)
			if style == PathStyleList {
				pressLand(m, "alt+n")
			} else {
				pressLand(m, "n")
			}
			typeText(m, "fresh/inner")
			pressLand(m, "enter")
			made := filepath.Join(root, "fresh", "inner")
			if fi, err := os.Stat(made); err != nil || !fi.IsDir() {
				t.Fatalf("folder not made: %v", err)
			}
			if got := shownDir(c); got != displayPath(made) {
				t.Fatalf("showing %q, want the new folder %q", got, displayPath(made))
			}
			if c.Query() != "" {
				t.Fatalf("query %q after making", c.Query())
			}
		})
	}
}

// Several paths are marked when the parameter is a list; enter takes the marks.
func TestPathStylesMarkSeveral(t *testing.T) {
	root := pathTree(t)
	if err := os.MkdirAll(filepath.Join(root, "second"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, style := range allPathStyles {
		t.Run(style, func(t *testing.T) {
			var many []string
			_, m, _, chosen := styleFixture(t, style, styleDirPick(root), nil, func(ps *PathChooserSpec) {
				ps.Many = true
				ps.ChooseMany = func(paths []string) Result { many = paths; return Done(len(paths)) }
			})
			mark, markAll := "m", "M"
			if style == PathStyleList {
				mark, markAll = "alt+m", "alt+a"
			}
			pressLand(m, mark, "down", mark)
			pressLand(m, "enter")
			if len(*chosen) != 0 {
				t.Fatalf("Choose ran with marks: %v", *chosen)
			}
			want := []string{filepath.Join(root, "second"), filepath.Join(root, "sub")}
			if !slices.Equal(many, want) {
				t.Fatalf("marked %v, want %v", many, want)
			}
			_ = markAll
		})
	}
}

// Home and the root are one key from anywhere.
func TestPathStylesGoHomeAndRoot(t *testing.T) {
	root := pathTree(t)
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", home)
	for _, style := range allPathStyles {
		t.Run(style, func(t *testing.T) {
			_, m, c, _ := styleFixture(t, style, styleDirPick(root), nil, nil)
			homeKey, rootKeys := "~", []string{"g", "/"}
			if style == PathStyleList {
				homeKey, rootKeys = "alt+h", []string{"alt+/"}
			}
			pressLand(m, homeKey)
			if got := shownDir(c); got != displayPath(home) {
				t.Fatalf("home: %q, want %q", got, displayPath(home))
			}
			pressLand(m, rootKeys...)
			if got := shownDir(c); got != "/" {
				t.Fatalf("root: %q", got)
			}
		})
	}
}

// The hybrid style lists Recent, an application's sections and Browse together;
// ] and [ jump between them and a heading never holds the highlight.
func TestPathHybridSections(t *testing.T) {
	root := pathTree(t)
	deep := filepath.Join(root, "sub", "deep")
	setup := func(r *Runtime) {
		r.SetRecentPaths(func(PathPick) []string { return []string{deep, root} })
		r.AddPathSource("project", func(PathPick) (ChooserSource, bool) {
			return ChooserSource{Name: "project", Section: "Project", Load: func(string) Result {
				return Done([]ChooserRow{{ID: "p", Label: "~/proj/x", Data: filepath.Join(root, "sub")}})
			}}, true
		})
	}
	_, m, c, chosen := styleFixture(t, PathStyleHybrid, styleDirPick(root), setup, nil)
	var heads []string
	for _, row := range c.Matches() {
		if row.Header {
			heads = append(heads, row.Label)
		}
	}
	if len(heads) != 3 || heads[0] != "Recent" || heads[1] != "Project" || !strings.HasPrefix(heads[2], "Browse ") {
		t.Fatalf("headings %v", heads)
	}
	section := func() string { row, _ := c.Highlighted(); return row.Section }
	if section() != "recent" {
		t.Fatalf("starts in %q", section())
	}
	pressLand(m, "]")
	if section() != "app:project" {
		t.Fatalf("] moved to %q", section())
	}
	pressLand(m, "]")
	if section() != "browse" {
		t.Fatalf("] moved to %q", section())
	}
	pressLand(m, "]")
	if section() != "browse" {
		t.Fatalf("] past the last section moved to %q", section())
	}
	pressLand(m, "[", "[")
	if section() != "recent" {
		t.Fatalf("[ [ moved to %q", section())
	}
	// j never rests on a heading
	for i := 0; i < 12; i++ {
		pressLand(m, "j")
		if row, ok := c.Highlighted(); !ok || row.Header {
			t.Fatalf("j %d: the highlight is on a heading", i)
		}
	}
	pressLand(m, "g", "g")
	if row, _ := c.Highlighted(); row.Header || row.Section != "recent" {
		t.Fatalf("gg landed on %+v", row)
	}
	pressLand(m, "G")
	if row, _ := c.Highlighted(); row.Header || !row.Fixed {
		t.Fatalf("G landed on %+v, want the fixed use row", row)
	}
	// going into a Recent folder browses it and lands in Browse
	pressLand(m, "g", "g", "l")
	if got := shownDir(c); got != displayPath(deep) {
		t.Fatalf("l on the first recent row: %q", got)
	}
	if len(*chosen) != 0 {
		t.Fatalf("chosen %v", *chosen)
	}
}

// Sections filter together: a heading is shown only over a row that matched.
func TestPathHybridFilterAcrossSections(t *testing.T) {
	root := pathTree(t)
	setup := func(r *Runtime) {
		r.SetRecentPaths(func(PathPick) []string { return []string{filepath.Join(root, "sub", "deep")} })
	}
	_, m, c, _ := styleFixture(t, PathStyleHybrid, styleDirPick(root), setup, nil)
	pressLand(m, "/")
	typeText(m, "deep")
	var heads []string
	for _, row := range c.Matches() {
		if row.Header {
			heads = append(heads, row.Label)
		}
	}
	if !slices.Equal(heads, []string{"Recent"}) {
		t.Fatalf("headings after filtering: %v", heads)
	}
}

// The columns style draws the folder above, the rows and a preview of the
// highlighted entry.
func TestPathColumns(t *testing.T) {
	root := pathTree(t)
	_, m, c, _ := styleFixture(t, PathStyleColumns, styleFilePick(filepath.Join(root, "sub")), nil, nil)
	view := c.View(110, 14).Text
	for _, want := range []string{"sub/", "a.json", "deep/", "c.json", " │ "} {
		if !strings.Contains(view, want) {
			t.Fatalf("columns lack %q:\n%s", want, view)
		}
	}
	// h moves a column left: the folder above becomes the rows.
	pressLand(m, "h")
	view = c.View(110, 14).Text
	if !strings.Contains(view, "a.json") || !strings.Contains(view, "deep/") {
		t.Fatalf("after h:\n%s", view)
	}
	// the preview of a file shows its head
	pressLand(m, "G") // the last row is "use <folder>"? it is fixed
	if c.Side().Title == "" {
		t.Fatalf("no side column title")
	}
}

// A path pasted into a picker resting in browse mode is not lost: it opens the
// search line with the text, and the typed source answers it.
func TestPathBrowsePasteOpensTheSearchLine(t *testing.T) {
	root := pathTree(t)
	for _, style := range []string{PathStyleHybrid, PathStyleColumns} {
		t.Run(style, func(t *testing.T) {
			_, m, c, chosen := styleFixture(t, style, styleDirPick(root), nil, nil)
			landLoads(m, keytest.Paste(m, filepath.Join(root, "sub")))
			if c.Query() != filepath.Join(root, "sub") || c.Source() != PathSourceTyped {
				t.Fatalf("query %q source %q", c.Query(), c.Source())
			}
			pressLand(m, "enter")
			if !slices.Equal(*chosen, []string{filepath.Join(root, "sub")}) {
				t.Fatalf("chosen %v", *chosen)
			}
		})
	}
}

// The four path verbs on the highlighted row: open, open with, reveal and
// copy path, through the opener and the register; no application starts in a test.
func TestPathPickerVerbsOnTheHighlightedPath(t *testing.T) {
	root := pathTree(t)
	for _, style := range allPathStyles {
		t.Run(style, func(t *testing.T) {
			r, m, _, _ := styleFixture(t, style, styleFilePick(root), nil, nil)
			var ran [][]string
			r.SetOpener(Opener{GOOS: "darwin", Start: func(name string, args ...string) error {
				ran = append(ran, append([]string{name}, args...))
				return nil
			}})
			want := filepath.Join(root, "sub")
			for _, verb := range []struct {
				name string
				args Arguments
			}{{"open-with", nil}, {"open-with", Arguments{"with": "Preview"}}, {"reveal", nil}} {
				if _, err := r.Execute(Request{Noun: "ochazuke.picker", Verb: verb.name, Args: verb.args}); err != nil {
					t.Fatalf("%s: %v", verb.name, err)
				}
			}
			landLoads(m, nil)
			if len(ran) != 3 || ran[0][len(ran[0])-1] != want || ran[1][1] != "-a" || ran[2][1] != "-R" {
				t.Fatalf("ran %v", ran)
			}
			if _, err := r.Execute(Request{Noun: "ochazuke.picker", Verb: "yank-path"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Recent places rank by use and age when the application says how often and when.
func TestPathRecentRanksByFrecency(t *testing.T) {
	root := pathTree(t)
	now := time.Now()
	a, b, c := filepath.Join(root, "sub"), filepath.Join(root, "sub", "deep"), root
	setup := func(r *Runtime) {
		r.SetRecentPaths(func(PathPick) []string { return []string{a, b, c} })
		r.SetRecentUses(func(PathPick) []PathUse {
			return []PathUse{{Path: a, Uses: 1, LastUsed: now.Add(-30 * 24 * time.Hour)}, {Path: c, Uses: 3, LastUsed: now.Add(-time.Minute)}}
		})
	}
	_, _, ch, _ := styleFixture(t, PathStyleList, styleDirPick(root), setup, func(ps *PathChooserSpec) { ps.Source = PathSourceRecent })
	got := rowLabels(ch)
	want := []string{displayPath(c), displayPath(a), displayPath(b)}
	if len(got) < 3 || !slices.Equal(got[:3], want) {
		t.Fatalf("recent order %v, want %v first", got, want)
	}
	if PathBoost(100, now, now) != PathMaxBoost || PathBoost(0, now, now) != 0 {
		t.Fatal("boost is capped and zero without use")
	}
}

// The style is read at each open, so switching it is live.
func TestPathStyleSwitchesLive(t *testing.T) {
	root := pathTree(t)
	r, _, c, _ := styleFixture(t, PathStyleList, styleDirPick(root), nil, nil)
	if c.Source() != PathSourceBrowse {
		t.Fatalf("list opened on %q", c.Source())
	}
	_ = c.close()
	for style, source := range map[string]string{PathStyleHybrid: PathSourceHybrid, PathStyleColumns: PathSourceBrowse} {
		if err := r.Settings().Set(PathStyleSetting, style); err != nil {
			t.Fatal(err)
		}
		c, err := r.OpenPathChooser(PathChooserSpec{Pick: styleDirPick(root)})
		if err != nil {
			t.Fatal(err)
		}
		if c.Source() != source || !strings.Contains(c.Title(), source) {
			t.Fatalf("%s opened on %q", style, c.Source())
		}
		_ = c.close()
	}
}

// No file system read on the loop: the sources answer their rows as
// work, never as a value read while the key is handled.
func TestPathSourcesReadTheFileSystemOffTheLoop(t *testing.T) {
	root := pathTree(t)
	r := NewRuntime()
	NewModel(r)
	r.EnsureConfigSettings()
	pick := PathPick{Start: root, Parameter: Parameter{Type: "path", Base: root}}
	st := &pathState{}
	use := func(string) Result { return Done(nil) }
	r.SetRecentPaths(func(PathPick) []string { return []string{root} })
	for name, res := range map[string]Result{
		"browse": r.browseSource(pick, st, use).Load(""),
		"recent": r.recentSource(pick, st, use).Load(""),
		"typed":  r.typedSource(pick, st, use).Load("~/x"),
	} {
		if res.work == nil {
			t.Errorf("%s read the file system on the loop", name)
		}
	}
	if res := r.hybridSource(pick, st, use, nil).Load(""); res.work == nil {
		t.Error("hybrid read the file system on the loop")
	}
}

// Escape while a folder is being named drops the name; the picker stays on
// the folder it was on and the search line is what it was.
func TestPathStylesNamingFolderEscapeDropsTheName(t *testing.T) {
	root := pathTree(t)
	for _, style := range allPathStyles {
		t.Run(style, func(t *testing.T) {
			r, m, c, _ := styleFixture(t, style, styleDirPick(root), nil, nil)
			newKey := "n"
			if style == PathStyleList {
				newKey = "alt+n"
			}
			pressLand(m, newKey)
			typeText(m, "nope")
			pressLand(m, "esc")
			if r.Chooser(PathChooserKind) != c {
				t.Fatal("escape closed the picker while a name was being typed")
			}
			if c.Query() != "" || shownDir(c) != displayPath(root) {
				t.Fatalf("query %q showing %q", c.Query(), shownDir(c))
			}
			if _, err := os.Stat(filepath.Join(root, "nope")); err == nil {
				t.Fatal("the folder was made")
			}
		})
	}
}

// In a file pick u takes the highlighted file; on a folder it says the folder
// only opens.
func TestPathStylesUseTheHighlightedFile(t *testing.T) {
	root := pathTree(t)
	for _, style := range allPathStyles {
		t.Run(style, func(t *testing.T) {
			_, m, _, chosen := styleFixture(t, style, styleFilePick(root), nil, nil)
			use := "u"
			if style == PathStyleList {
				use = "alt+u"
			}
			pressLand(m, use) // sub/ is highlighted first: refused
			if len(*chosen) != 0 {
				t.Fatalf("use picked a folder in a file pick: %v", *chosen)
			}
			pressLand(m, "down", use)
			if !slices.Equal(*chosen, []string{filepath.Join(root, "a.json")}) {
				t.Fatalf("chosen %v", *chosen)
			}
		})
	}
}
