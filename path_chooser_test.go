package ochazuke

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// pathTree makes root/{a.json, b.txt, .hidden, sub/{c.json, deep/}}.
func pathTree(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"sub/deep"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"a.json", "b.txt", ".hidden", "sub/c.json"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func pathFixture(t *testing.T, pick PathPick) (*Runtime, *Model, *Chooser, *[]string) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 100, 30
	m.Appearance.Theme = "mono"
	r.EnsureConfigSettings()
	var chosen []string
	c, err := r.OpenPathChooser(PathChooserSpec{Pick: pick, Choose: func(p string) Result {
		chosen = append(chosen, p)
		return Done(p)
	}})
	if err != nil {
		t.Fatal(err)
	}
	landLoads(m, r.PendingCommands())
	return r, m, c, &chosen
}

// Browse lists the start folder, folders first; enter on a folder browses
// into it, backspace on an empty query and alt+up go to the parent; enter
// on a file picks it.
func TestPathBrowseDescendsAndGoesUp(t *testing.T) {
	root := pathTree(t)
	_, m, c, chosen := pathFixture(t, PathPick{Name: "path", Parameter: Parameter{Type: "path", Base: root}})
	if c.Source() != PathSourceBrowse || !slices.Equal(labels(c.Matches()), []string{"sub/", "a.json", "b.txt"}) {
		t.Fatalf("source %q rows %v", c.Source(), labels(c.Matches()))
	}
	if !strings.Contains(c.Title(), "[browse "+displayPath(root)+"]") || !strings.Contains(c.Title(), "recent typed") {
		t.Fatalf("title %q", c.Title())
	}
	typeText(m, "su")
	pressLand(m, "enter")
	if c.Query() != "" || !slices.Equal(labels(c.Matches()), []string{"deep/", "c.json"}) {
		t.Fatalf("after enter on sub/: query %q rows %v", c.Query(), labels(c.Matches()))
	}
	pressLand(m, "backspace")
	if !slices.Equal(labels(c.Matches()), []string{"sub/", "a.json", "b.txt"}) {
		t.Fatalf("backspace on an empty query goes up: %v", labels(c.Matches()))
	}
	pressLand(m, "ctrl+l")
	pressLand(m, "alt+up")
	if !strings.Contains(c.Title(), "[browse "+displayPath(root)+"]") {
		t.Fatalf("alt+up goes up: %q", c.Title())
	}
	typeText(m, "b.t")
	pressLand(m, "enter")
	if !slices.Equal(*chosen, []string{filepath.Join(root, "b.txt")}) {
		t.Fatalf("chosen %v", *chosen)
	}
}

// A folder chooser lists folders only and ends with "use <folder>"; enter
// opens a folder, alt+enter picks it; chooser.enter=pick swaps them.
func TestPathDirChooserOpensAndPicks(t *testing.T) {
	root := pathTree(t)
	r, m, c, chosen := pathFixture(t, PathPick{Parameter: Parameter{Type: "path", Path: PathDir, Base: root}})
	if got := labels(c.Matches()); !slices.Equal(got, []string{"sub/", "use " + displayPath(root)}) {
		t.Fatalf("rows %v", got)
	}
	pressLand(m, "enter")
	if !strings.Contains(c.Title(), "[browse "+displayPath(filepath.Join(root, "sub"))+"]") {
		t.Fatalf("enter opens the folder: %q", c.Title())
	}
	pressLand(m, "alt+enter")
	if !slices.Equal(*chosen, []string{filepath.Join(root, "sub", "deep")}) {
		t.Fatalf("alt+enter picks the highlighted folder: %v", *chosen)
	}
	if err := r.Settings().Set(ChooserEnterSetting, "pick"); err != nil {
		t.Fatal(err)
	}
	c, err := r.OpenPathChooser(PathChooserSpec{Pick: PathPick{Parameter: Parameter{Type: "path", Path: PathDir, Base: root}}, Choose: func(p string) Result {
		*chosen = append(*chosen, p)
		return Done(p)
	}})
	if err != nil {
		t.Fatal(err)
	}
	landLoads(m, r.PendingCommands())
	if c.Source() != PathSourceBrowse {
		// remembered from the last open, which stayed on browse
		t.Fatalf("source %q", c.Source())
	}
	pressLand(m, "ctrl+l")
	pressLand(m, "enter")
	if got := (*chosen)[len(*chosen)-1]; got != filepath.Join(root, "sub", "deep") {
		t.Fatalf("with chooser.enter=pick enter picks: %v", *chosen)
	}
}

// The typed source completes the path being typed: the entries of its
// folder starting with its last part; enter on a folder types it with a
// trailing /, and a typed file is taken.
func TestPathTypedCompletes(t *testing.T) {
	root := pathTree(t)
	_, m, c, chosen := pathFixture(t, PathPick{Parameter: Parameter{Type: "path", Base: root}})
	pressLand(m, "tab", "tab")
	if c.Source() != PathSourceTyped {
		t.Fatalf("source %q", c.Source())
	}
	typeText(m, root+"/s")
	if got := labels(c.Matches()); !slices.Equal(got, []string{root + "/sub/"}) {
		t.Fatalf("rows %v", got)
	}
	pressLand(m, "enter")
	if c.Query() != root+"/sub/" {
		t.Fatalf("query %q", c.Query())
	}
	if got := labels(c.Matches()); !slices.Equal(got, []string{root + "/sub/deep/", root + "/sub/c.json"}) {
		t.Fatalf("rows %v", got)
	}
	typeText(m, "c.json")
	pressLand(m, "enter")
	if !slices.Equal(*chosen, []string{filepath.Join(root, "sub", "c.json")}) {
		t.Fatalf("chosen %v", *chosen)
	}
}

// Typed paths are relative to the start folder; with New a path not there
// yet whose folder is offers "new <path>"; hidden entries list once the
// part typed starts with a dot.
func TestPathTypedNewAndHidden(t *testing.T) {
	root := pathTree(t)
	_, m, c, chosen := pathFixture(t, PathPick{Parameter: Parameter{Type: "path", Base: root, New: true}})
	pressLand(m, "tab", "tab")
	typeText(m, ".h")
	if got := labels(c.Matches()); !slices.Equal(got, []string{".hidden", "new .h"}) {
		t.Fatalf("hidden rows %v", got)
	}
	c.SetQuery("")
	landLoads(m, c.r.PendingCommands())
	typeText(m, "out.csv")
	if got := labels(c.Matches()); !slices.Equal(got, []string{"new out.csv"}) {
		t.Fatalf("new rows %v", got)
	}
	pressLand(m, "enter")
	if !slices.Equal(*chosen, []string{filepath.Join(root, "out.csv")}) {
		t.Fatalf("chosen %v", *chosen)
	}
}

// Recent lists the application's paths, then the ones picked this run.
func TestPathRecentSource(t *testing.T) {
	root := pathTree(t)
	r := NewRuntime()
	m := NewModel(r)
	r.SetRecentPaths(func(PathPick) []string { return []string{"/tmp/app-recent.json"} })
	open := func() *Chooser {
		c, err := r.OpenPathChooser(PathChooserSpec{Pick: PathPick{Parameter: Parameter{Type: "path", Base: root}}})
		if err != nil {
			t.Fatal(err)
		}
		landLoads(m, r.PendingCommands())
		return c
	}
	c := open()
	typeText(m, "a.json")
	pressLand(m, "enter")
	if got := r.RecentPaths(); !slices.Equal(got, []string{filepath.Join(root, "a.json")}) {
		t.Fatalf("picked %v", got)
	}
	c = open()
	pressLand(m, "tab")
	if c.Source() != PathSourceRecent {
		t.Fatalf("source %q", c.Source())
	}
	if got := labels(c.Matches()); !slices.Equal(got, []string{"/tmp/app-recent.json", displayPath(filepath.Join(root, "a.json"))}) {
		t.Fatalf("recent rows %v", got)
	}
}

// Applications add sources after typed; the title lists them.
func TestPathAppSources(t *testing.T) {
	root := pathTree(t)
	r := NewRuntime()
	m := NewModel(r)
	r.AddPathSource("known", func(p PathPick) (ChooserSource, bool) {
		return ChooserSource{Load: func(string) Result {
			return Done([]ChooserRow{{ID: root, Label: "root", Data: root}})
		}}, p.Dir()
	})
	c, err := r.OpenPathChooser(PathChooserSpec{Pick: PathPick{Parameter: Parameter{Type: "path", Path: PathDir, Base: root}}})
	if err != nil {
		t.Fatal(err)
	}
	landLoads(m, r.PendingCommands())
	if got := c.Sources(); !slices.Equal(got, []string{"browse", "recent", "typed", "known"}) {
		t.Fatalf("sources %v", got)
	}
	file, err := r.OpenPathChooser(PathChooserSpec{Kind: "other", Pick: PathPick{Parameter: Parameter{Type: "path", Base: root}}})
	if err == nil && slices.Contains(file.Sources(), "known") {
		t.Fatal("a source whose builder says false is left out")
	}
}

// A folder under path.no-auto-list is not listed on its own: browse says
// why and lists nothing until it is opened into (or a path is typed). No
// test touches a real external volume: the prefix is the temp tree.
func TestPathNoAutoList(t *testing.T) {
	root := pathTree(t)
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 120, 30
	r.EnsureConfigSettings()
	if err := r.Settings().Set(PathNoAutoListSetting, []string{root + "/"}); err != nil {
		t.Fatal(err)
	}
	if r.autoListable(root) != true || r.autoListable(filepath.Join(root, "sub")) || !r.autoListable("/Volumes") || r.autoListable("/mnt-other") != true {
		t.Fatal("autoListable prefixes")
	}
	c, err := r.OpenPathChooser(PathChooserSpec{Pick: PathPick{Parameter: Parameter{Type: "path", Base: filepath.Join(root, "sub")}}})
	if err != nil {
		t.Fatal(err)
	}
	landLoads(m, r.PendingCommands())
	if len(c.Matches()) != 0 || !strings.Contains(screen(m), "not listed on its own") {
		t.Fatalf("rows %v:\n%s", labels(c.Matches()), screen(m))
	}
	pressLand(m, "alt+up")
	pressLand(m, "enter")
	if !slices.Equal(labels(c.Matches()), []string{"deep/", "c.json"}) {
		t.Fatalf("opened into, it lists: %v", labels(c.Matches()))
	}
}

// PathStart: the field's folder, else Base, else path.start (base: the
// application's folder, else the working directory; home; cwd).
func TestPathStart(t *testing.T) {
	r := NewRuntime()
	r.EnsureConfigSettings()
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	if got := r.PathStart(PathPick{Value: "/x/y/z.json"}); got != "/x/y" {
		t.Fatalf("value: %q", got)
	}
	if got := r.PathStart(PathPick{Value: "/x/y", Parameter: Parameter{Path: PathDir}}); got != "/x/y" {
		t.Fatalf("dir value: %q", got)
	}
	if got := r.PathStart(PathPick{Parameter: Parameter{Base: "~/b"}}); got != filepath.Join(home, "b") {
		t.Fatalf("base: %q", got)
	}
	if got := r.PathStart(PathPick{}); got != cwd {
		t.Fatalf("no app base: %q", got)
	}
	r.SetPathBase(func(PathPick) string { return "/open/file/folder" })
	if got := r.PathStart(PathPick{}); got != "/open/file/folder" {
		t.Fatalf("app base: %q", got)
	}
	for setting, want := range map[string]string{"home": home, "cwd": cwd} {
		if err := r.Settings().Set(PathStartSetting, setting); err != nil {
			t.Fatal(err)
		}
		if got := r.PathStart(PathPick{}); got != want {
			t.Fatalf("%s: %q", setting, got)
		}
	}
}

// A path field opens the path chooser: as a stop (path.field picker,
// enter opens it) or as a text field (path.field text, ctrl+space); the
// pick is written at once.
func TestPathFormField(t *testing.T) {
	root := pathTree(t)
	for _, mode := range []string{"picker", "text"} {
		r := NewRuntime()
		m := NewModel(r)
		m.Width, m.Height = 100, 30
		r.EnsureConfigSettings()
		if err := r.Settings().Set(PathFieldSetting, mode); err != nil {
			t.Fatal(err)
		}
		var ed *RecordEditor
		if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return ed.Block(w, h) }); err != nil {
			t.Fatal(err)
		}
		if err := r.OpenPage("main"); err != nil {
			t.Fatal(err)
		}
		var writes []string
		var err error
		ed, err = r.AddRecordEditor("ed", RecordSpec{
			Fields: []RecordField{{Name: "file", Parameter: Parameter{Type: "path", Base: root}}},
			Save:   func(w RecordWrite) Result { writes = append(writes, w.String()); return Done(nil) },
		}, Parent("main"))
		if err != nil {
			t.Fatal(err)
		}
		if err := ed.Focus(); err != nil {
			t.Fatal(err)
		}
		m.View()
		key := "enter"
		if mode == "text" {
			key = "ctrl+space"
		}
		pressLand(m, key)
		if r.TopModal() != PathChooserKind {
			t.Fatalf("%s: %s opened %q", mode, key, r.TopModal())
		}
		typeText(m, "a.js")
		pressLand(m, "enter")
		want := filepath.Join(root, "a.json")
		if ed.Values()["file"] != want || !slices.Equal(writes, []string{"write file=" + want}) {
			t.Fatalf("%s: values %v writes %v", mode, ed.Values(), writes)
		}
	}
}

// The args form holds a path parameter, and a missing one opens the
// chooser from the form.
func TestPathArgsForm(t *testing.T) {
	root := pathTree(t)
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 100, 30
	r.EnsureConfigSettings()
	var got string
	r.Define("doc").Verb("open", func(ctx Ctx) Result {
		got, _ = ctx.Args["path"].(string)
		return Done(nil)
	})
	op := r.Operations["doc.open"]
	op.Parameters = map[string]Parameter{"path": {Type: "path", Required: true, Base: root}}
	r.Operations["doc.open"] = op
	if _, err := r.OpenArgsForm(Request{Noun: "doc", Verb: "open"}, func(q Request) error {
		_, err := r.Execute(q)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	m.View()
	pressLand(m, "enter")
	if r.TopModal() != PathChooserKind {
		t.Fatalf("enter on the missing path opened %q", r.TopModal())
	}
	typeText(m, "b.t")
	pressLand(m, "enter")
	pressLand(m, "enter")
	if got != filepath.Join(root, "b.txt") {
		t.Fatalf("ran with %q (top %q)", got, r.TopModal())
	}
}

// Labels show a path under the home folder with ~, rows keep the absolute
// path: a tree under HOME browses, lists recent and picks the full path,
// and the shown form expands back to it. A sibling of HOME that only
// shares its prefix is not shortened.
func TestPathUnderHomeShowsTilde(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	root := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.json"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	r, m, c, chosen := pathFixture(t, PathPick{Parameter: Parameter{Type: "path", Base: root}})
	if !strings.Contains(c.Title(), "[browse ~/proj]") {
		t.Fatalf("title %q", c.Title())
	}
	typeText(m, "a.j")
	pressLand(m, "enter")
	want := filepath.Join(root, "a.json")
	if !slices.Equal(*chosen, []string{want}) {
		t.Fatalf("chosen %v, want the absolute path", *chosen)
	}
	c, err = r.OpenPathChooser(PathChooserSpec{Pick: PathPick{Parameter: Parameter{Type: "path", Base: root}}})
	if err != nil {
		t.Fatal(err)
	}
	landLoads(m, r.PendingCommands())
	pressLand(m, "tab")
	rows := c.Matches()
	if len(rows) != 1 || rows[0].Label != "~/proj/a.json" || rows[0].Data != want {
		t.Fatalf("recent rows %+v", rows)
	}
	for _, p := range []string{home, root, want} {
		if got := expandPath(displayPath(p), ""); got != p {
			t.Errorf("round trip %q -> %q -> %q", p, displayPath(p), got)
		}
	}
	if got := displayPath(home + "x/a"); got != home+"x/a" {
		t.Errorf("sibling of HOME shortened: %q", got)
	}

	_, _, dc, _ := pathFixture(t, PathPick{Parameter: Parameter{Type: "path", Path: PathDir, Base: root}})
	if got := labels(dc.Matches()); !slices.Equal(got, []string{"sub/", "use ~/proj"}) {
		t.Fatalf("dir rows %v", got)
	}
}
