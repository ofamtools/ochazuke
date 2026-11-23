package pathpicker

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/picker"
)

type tfs struct {
	OSFS
	home string
}

func (f tfs) HomeDir() (string, error) { return f.home, nil }

func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "src", "deep"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "Docs"), 0o755))
	must(os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	must(os.WriteFile(filepath.Join(root, "readme.md"), []byte("hello preview\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644))
	must(os.WriteFile(filepath.Join(root, ".env"), []byte("X=1"), 0o644))
	must(os.WriteFile(filepath.Join(root, "blob.bin"), []byte{0, 1, 2, 3}, 0o644))
	must(os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("a"), 0o644))
	must(os.WriteFile(filepath.Join(root, "src", "b.go"), []byte("b"), 0o644))
	return root
}

func names(m *Model) string {
	var s []string
	for _, e := range m.Entries() {
		n := e.Name
		if e.IsDir {
			n += "/"
		}
		s = append(s, n)
	}
	return strings.Join(s, " ")
}

func cmd(m *Model, id string) *DirEntry {
	p, ok := m.HandleCommand(core.Command{ID: id})
	if !ok {
		panic("unhandled " + id)
	}
	return p
}

func TestListingAndHidden(t *testing.T) {
	root := tree(t)
	m, err := New(nil, root, PickAny)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(m); got != "Docs/ src/ blob.bin main.go readme.md" {
		t.Fatalf("listing: %s", got)
	}
	cmd(m, CmdHiddenToggle)
	if got := names(m); got != ".git/ Docs/ src/ .env blob.bin main.go readme.md" {
		t.Fatalf("hidden: %s", got)
	}
	cmd(m, CmdHiddenToggle)
	if names(m) != "Docs/ src/ blob.bin main.go readme.md" {
		t.Fatal("hide again")
	}
}

func TestNavigationAndModes(t *testing.T) {
	root := tree(t)
	home := filepath.Join(root, "Docs")
	m, _ := New(tfs{home: home}, root, PickFile)
	m.SetQuery("sr")
	cmd(m, "motion.right") // drill into src
	if m.Dir != filepath.Join(root, "src") || m.Query != "" {
		t.Fatalf("drill: %s %q", m.Dir, m.Query)
	}
	if got := names(m); got != "deep/ a.go b.go" {
		t.Fatalf("src: %s", got)
	}
	// PickFile: selecting a dir is refused, a file is accepted.
	if p := cmd(m, CmdSelect); p != nil {
		t.Fatalf("dir picked in file mode: %v", p)
	}
	cmd(m, "motion.down")
	if p := cmd(m, CmdOpen); p == nil || p.Name != "a.go" {
		t.Fatalf("file open: %v", p)
	}
	if p := cmd(m, "motion.right"); p != nil {
		t.Fatal("motion.right must not pick files")
	}
	cmd(m, "motion.left")
	if m.Dir != root || m.Entries()[m.list.SelectedIdx].Name != "src" {
		t.Fatalf("parent: %s sel %d", m.Dir, m.list.SelectedIdx)
	}
	cmd(m, CmdHome)
	if m.Dir != home {
		t.Fatalf("home: %s", m.Dir)
	}

	d, _ := New(nil, root, PickDir)
	if p := cmd(d, CmdSelect); p == nil || p.Name != "Docs" {
		t.Fatalf("select highlighted dir: %v", p)
	}
	if p := cmd(d, CmdSelectCurrent); p == nil || p.Path != root {
		t.Fatalf("select current: %v", p)
	}
	d.SetQuery("zzz")
	if p := cmd(d, CmdSelect); p == nil || p.Path != root {
		t.Fatalf("empty list selects current: %v", p)
	}
	d.SetQuery("")
	cmd(d, "motion.bottom")
	if p := cmd(d, CmdSelect); p != nil {
		t.Fatalf("file picked in dir mode: %v", p)
	}

	a, _ := New(nil, root, PickAny)
	if p := cmd(a, CmdSelect); p == nil || !p.IsDir {
		t.Fatalf("any/dir: %v", p)
	}
	cmd(a, "motion.bottom")
	if p := cmd(a, picker_confirm); p == nil || p.Name != "readme.md" {
		t.Fatalf("any/file: %v", p)
	}
}

const picker_confirm = "picker.confirm"

func TestErrorKeepsDir(t *testing.T) {
	root := tree(t)
	m, _ := New(nil, root, PickAny)
	if err := m.SetDir(filepath.Join(root, "missing")); err == nil || m.Err == "" || m.Dir != root {
		t.Fatalf("err handling: %v %q %s", err, m.Err, m.Dir)
	}
	if names(m) == "" {
		t.Fatal("entries lost")
	}
	if _, err := New(nil, filepath.Join(root, "missing"), PickAny); err == nil {
		t.Fatal("New should report error")
	}
}

func TestFilterModes(t *testing.T) {
	m, _ := New(nil, tree(t), PickAny)
	m.SetQuery("MAIN")
	if names(m) != "main.go" {
		t.Fatalf("substring: %s", names(m))
	}
	cmd(m, CmdModeCycle) // fuzzy
	m.SetQuery("rdm")
	if names(m) != "readme.md" {
		t.Fatalf("fuzzy: %s", names(m))
	}
	cmd(m, CmdModeCycle) // regex
	m.SetQuery(`\.go$`)
	if names(m) != "main.go" {
		t.Fatalf("regex: %s", names(m))
	}
	m.SetQuery("[")
	if m.List().FilterErr == "" || len(m.Entries()) != 5 {
		t.Fatalf("invalid regex: %q %d", m.List().FilterErr, len(m.Entries()))
	}
	m.SetQuery("")
	m.AppendQuery("x")
	m.BackspaceQuery()
	if m.Query != "" {
		t.Fatal("backspace")
	}
	m.HandleCommand(core.Command{ID: "motion.bottom"})
	// Row 0 is the pinned current-folder row in PickAny, so the last of the
	// five entries sits at index 5.
	if e, _ := m.Focused(); m.list.SelectedIdx != 5 || e.Name != "readme.md" {
		t.Fatalf("forwarded motion: %d %v", m.list.SelectedIdx, e)
	}
	if _, ok := m.HandleCommand(core.Command{ID: "bogus"}); ok {
		t.Fatal("bogus handled")
	}
}

func TestPreviewAndRender(t *testing.T) {
	root := tree(t)
	m, _ := New(nil, root, PickAny)
	cmd(m, CmdPreviewToggle)
	render := func() string {
		f, _ := ui.Render(m.View("pp"), ui.DefaultDarkTheme(), "pp", 90, 12)
		return f.ANSI
	}
	out := render()
	if !strings.Contains(out, "Docs/") || !strings.Contains(out, "0 entries") {
		t.Fatalf("dir preview: %q", out)
	}
	m.SetQuery("readme")
	if out := render(); !strings.Contains(out, "hello preview") {
		t.Fatalf("file preview: %q", out)
	}
	m.SetQuery("blob")
	if out := render(); !strings.Contains(out, "binary file") {
		t.Fatalf("binary preview: %q", out)
	}
	m.SetQuery("src")
	if out := render(); !strings.Contains(out, "3 entries") && !strings.Contains(out, "2 entries") {
		t.Fatalf("child count: %q", out)
	}

	v := m.View("pp")
	if v.Kind != ui.KindPanel || v.Title != root {
		t.Fatalf("panel: %+v", v)
	}
	if v.Find("pp") == nil || !v.Find("pp").Focusable || v.Find("pp").Scope != "pathpicker.dir" || v.Find("pp").Facts["dir"] != root {
		t.Fatalf("picker node: %+v", v.Find("pp"))
	}
	k1 := v.Find("pp").CacheKey
	m.ToggleHidden()
	hv := m.View("pp")
	if hv.Find("pp").CacheKey == k1 {
		t.Fatal("cache key must change with hidden")
	}
	m.ShowPreview = false
	m.rebuild()
	nv := m.View("pp")
	if nv.Find("pp.split") != nil {
		t.Fatal("no preview expected")
	}
	if HumanSize(1536) != "1.5 KB" || HumanSize(12) != "12 B" || HumanSize(3<<20) != "3.0 MB" {
		t.Fatal("HumanSize")
	}
}

// mapFS is a minimal FS without MkdirFS, like the mocks hosts already have.
type mapFS struct {
	home string
	dirs map[string][]DirEntry
}

func (f mapFS) ReadDir(dir string) ([]DirEntry, error) {
	if es, ok := f.dirs[dir]; ok {
		return es, nil
	}
	return nil, os.ErrNotExist
}
func (f mapFS) ReadPreview(string, int) (string, error) { return "", nil }
func (f mapFS) HomeDir() (string, error)                { return f.home, nil }

func TestRoots(t *testing.T) {
	root := tree(t)
	m, _ := New(tfs{home: root}, root, PickDir)
	if err := m.CycleRoot(); !errors.Is(err, errNoRoots) || m.Err == "" {
		t.Fatalf("no roots: %v %q", err, m.Err)
	}
	m.Roots = []Root{
		{Label: "docs", Path: filepath.Join(root, "Docs")},
		{Label: "gone", Path: filepath.Join(root, "missing")},
		{Label: "src", Path: "~/src"},
	}
	cmd(m, CmdRootCycle)
	if m.Dir != filepath.Join(root, "Docs") || m.root != 1 || m.Err != "" {
		t.Fatalf("first root: %s %d %q", m.Dir, m.root, m.Err)
	}
	m.SetQuery("x")
	if err := m.CycleRoot(); err == nil || m.Err == "" || m.Dir != filepath.Join(root, "Docs") || m.Query != "x" || m.root != 2 {
		t.Fatalf("broken root keeps state: %v %q %s %q", err, m.Err, m.Dir, m.Query)
	}
	if err := m.CycleRoot(); err != nil || m.Dir != filepath.Join(root, "src") || names(m) != "deep/ a.go b.go" || m.Err != "" {
		t.Fatalf("~ root: %v %s %s", err, m.Dir, names(m))
	}
	if v := m.View("pp"); v.Find("pp").Facts["root"] != "src" {
		t.Fatalf("root fact: %v", v.Find("pp").Facts["root"])
	}
	m.CycleRoot()
	if m.Dir != filepath.Join(root, "Docs") || m.root != 1 {
		t.Fatalf("wrap: %s %d", m.Dir, m.root)
	}
	if err := m.JumpRoot(3); err == nil {
		t.Fatal("out of range")
	}
}

func TestRecents(t *testing.T) {
	root := tree(t)
	m, _ := New(nil, root, PickAny)
	m.SetQuery("readme")
	cmd(m, CmdOpen)
	m.SetQuery("")
	cmd(m, "motion.top") // the pinned current-folder row
	if p := cmd(m, CmdOpen); p == nil || p.Path != root {
		t.Fatalf("pick current: %v", p)
	}
	m.SetQuery("readme")
	cmd(m, picker.CmdConfirm)
	if got := strings.Join(m.Recents, ","); got != filepath.Join(root, "readme.md")+","+root {
		t.Fatalf("recents: %s", got)
	}
	f, _ := New(nil, root, PickFile)
	cmd(f, CmdSelectCurrent) // refused: not recorded
	if len(f.Recents) != 0 {
		t.Fatalf("refused pick recorded: %v", f.Recents)
	}
}

func TestTypedPaths(t *testing.T) {
	root := tree(t)
	m, _ := New(tfs{home: root}, filepath.Join(root, "Docs"), PickFile)
	m.SetQuery("~/src")
	if p := cmd(m, CmdOpen); p != nil || m.Dir != filepath.Join(root, "src") || m.Query != "" {
		t.Fatalf("~ dir: %v %s %q", p, m.Dir, m.Query)
	}
	m.SetQuery("../")
	cmd(m, CmdSelect) // a folder drills even on Select
	if m.Dir != root {
		t.Fatalf("../: %s", m.Dir)
	}
	m.SetQuery("./nope")
	if p := cmd(m, CmdOpen); p != nil || m.Err == "" || m.Dir != root || m.Query != "./nope" {
		t.Fatalf("missing path must not fall back: %v %q %s", p, m.Err, m.Dir)
	}
	m.SetQuery("./.env") // hidden, not listed
	if p := cmd(m, CmdOpen); p == nil || p.Path != filepath.Join(root, ".env") || m.Err != "" {
		t.Fatalf("hidden file: %v %q", p, m.Err)
	}
	m.SetQuery(filepath.Join(root, "main.go"))
	if p := cmd(m, CmdSelect); p == nil || p.Name != "main.go" {
		t.Fatalf("abs file: %v", p)
	}
	d, _ := New(nil, root, PickDir)
	d.SetQuery("./readme.md")
	if p := cmd(d, CmdOpen); p != nil || d.Err == "" || d.Dir != root {
		t.Fatalf("file in dir mode: %v %q", p, d.Err)
	}
	// A plain mock FS resolves through ReadDir alone.
	mf := mapFS{home: "/h", dirs: map[string][]DirEntry{
		"/h":     {{Name: "sub", Path: "/h/sub", IsDir: true}, {Name: ".rc", Path: "/h/.rc"}},
		"/h/sub": {},
	}}
	a, _ := New(mf, "/h/sub", PickAny)
	a.SetQuery("~/.rc")
	if p := cmd(a, CmdOpen); p == nil || p.Path != "/h/.rc" {
		t.Fatalf("mock file: %v %q", p, a.Err)
	}
	a.SetQuery("..")
	if cmd(a, CmdOpen); a.Dir != "/h" {
		t.Fatalf("mock dir: %s %q", a.Dir, a.Err)
	}
}

func TestRanking(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"ago", "go", "gox"} {
		os.Mkdir(filepath.Join(root, d), 0o755)
	}
	for _, f := range []string{"a_g_o.txt", "cargo.toml", "gom.txt", "lingo"} {
		os.WriteFile(filepath.Join(root, f), nil, 0o644)
	}
	m, _ := New(nil, root, PickAny)
	m.SetQuery("go")
	if got := names(m); got != "go/ gox/ gom.txt ago/ cargo.toml lingo" {
		t.Fatalf("substring rank: %s", got)
	}
	if e, _ := m.Focused(); e.Name != "go" || m.FocusedCurrent() {
		t.Fatalf("first real result focused: %v", e)
	}
	if n := m.View("pp"); m.List().Query != "go" || len(m.List().Items) != 7 || n.Find("pp").Props["query"] != "go" {
		t.Fatalf("inner list keeps query and raw rows: %q %d", m.List().Query, len(m.List().Items))
	}
	cmd(m, CmdModeCycle) // fuzzy
	m.SetQuery("go")
	if got := names(m); got != "go/ gox/ gom.txt ago/ cargo.toml lingo a_g_o.txt" {
		t.Fatalf("fuzzy rank: %s", got)
	}
	cmd(m, CmdModeCycle) // regex keeps the listing order
	m.SetQuery("go")
	if got := names(m); got != "ago/ go/ gox/ cargo.toml gom.txt lingo" {
		t.Fatalf("regex order: %s", got)
	}
}

func TestPinnedCurrentRow(t *testing.T) {
	root := tree(t)
	m, _ := New(nil, root, PickDir)
	if e, _ := m.Focused(); e.Name != "Docs" || m.FocusedCurrent() {
		t.Fatalf("first real row highlighted: %v", e)
	}
	cmd(m, CmdModeCycle)
	cmd(m, CmdModeCycle) // regex
	m.SetQuery("^nomatch$")
	if !m.FocusedCurrent() || len(m.Entries()) != 0 {
		t.Fatal("pinned row survives the filter")
	}
	if p := cmd(m, "motion.right"); p != nil || m.Dir != root {
		t.Fatal("motion.right on the pinned row does nothing")
	}
	if p := cmd(m, CmdOpen); p == nil || p.Path != root || !p.IsDir {
		t.Fatalf("open on the pinned row picks the current folder: %v", p)
	}
	m.SetQuery("")
	cmd(m, CmdPreviewToggle)
	cmd(m, "motion.top")
	out, _ := ui.Render(m.View("pp"), ui.DefaultDarkTheme(), "pp", 90, 8)
	if !strings.Contains(out.ANSI, useFolderLabel) || !strings.Contains(out.ANSI, "7 entries") {
		t.Fatalf("render pinned row: %q", out.ANSI)
	}
	cmd(m, "motion.down")
	if p := cmd(m, CmdOpen); p != nil || m.Dir != filepath.Join(root, "Docs") {
		t.Fatalf("open on a dir drills: %v %s", p, m.Dir)
	}
	f, _ := New(nil, root, PickFile)
	if f.List().Pinned != nil {
		t.Fatal("no pinned row in file mode")
	}
}

func TestCreateDir(t *testing.T) {
	root := tree(t)
	m, _ := New(nil, root, PickDir)
	m.SetQuery("src")
	m.HandleCommand(core.Command{ID: CmdMkdir, Args: map[string]any{"name": "made"}})
	if e, _ := m.Focused(); e.Name != "made" || !e.IsDir || m.Query != "" || m.Err != "" {
		t.Fatalf("highlight new folder: %v %q %q", e, m.Query, m.Err)
	}
	for _, bad := range []string{"", " ", ".", "..", "../x", "a/b", "/abs"} {
		if err := m.CreateDir(bad); err == nil || m.Err == "" {
			t.Fatalf("accepted %q", bad)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "x")); err == nil {
		t.Fatal("traversal created a folder")
	}
	if cmd(m, CmdMkdir); m.Err != errMkdirName.Error() {
		t.Fatalf("missing name: %q", m.Err)
	}
	if err := m.CreateDir(".cache"); err != nil || !m.ShowHidden {
		t.Fatal(err)
	}
	if e, _ := m.Focused(); e.Name != ".cache" {
		t.Fatalf("hidden folder highlighted: %v", e)
	}
	m.SetQuery("+x") // a plain filter, never a mkdir
	cmd(m, CmdOpen)
	if _, err := os.Stat(filepath.Join(root, "x")); err == nil {
		t.Fatal("+query created a folder")
	}
	mf, _ := New(mapFS{dirs: map[string][]DirEntry{"/r": {}}}, "/r", PickAny)
	if err := mf.CreateDir("x"); !errors.Is(err, errMkdirUnsupported) {
		t.Fatalf("no MkdirFS: %v", err)
	}
	// Mkdir works but the reload fails: the filter and listing stay.
	bf := reloadFailFS{mapFS{dirs: map[string][]DirEntry{"/r": {{Name: "a", Path: "/r/a", IsDir: true}}}}}
	b, _ := New(bf, "/r", PickAny)
	b.SetQuery("a")
	if err := b.CreateDir(".n"); err == nil || !strings.Contains(b.Err, "created /r/.n") ||
		b.Query != "a" || b.ShowHidden || names(b) != "a/" || b.List().SelectedIdx != 1 {
		t.Fatalf("reload failure: %v %q %q %v %s", err, b.Err, b.Query, b.ShowHidden, names(b))
	}
}

// reloadFailFS creates folders by making its directories unreadable.
type reloadFailFS struct{ mapFS }

func (f reloadFailFS) Mkdir(string) error { clear(f.dirs); return nil }

func TestFilterModeAndSetDirPreservesFiltering(t *testing.T) {
	root := tree(t)
	m, err := New(nil, root, PickAny)
	if err != nil {
		t.Fatal(err)
	}
	if m.Filtering() {
		t.Fatal("default Filtering should be false")
	}

	// Normal mode stays normal across SetDir.
	if err := m.SetDir(filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}
	if m.Filtering() {
		t.Fatal("SetDir should keep Filtering false")
	}

	// SetFiltering and CmdFilterStart/Stop/Clear.
	m.SetFiltering(true)
	if !m.Filtering() || !m.List().Filtering {
		t.Fatal("SetFiltering(true) did not delegate")
	}
	if err := m.SetDir(root); err != nil {
		t.Fatal(err)
	}
	if !m.Filtering() {
		t.Fatal("SetDir must preserve Filtering=true")
	}

	m.SetQuery("readme")
	cmd(m, picker.CmdFilterStop)
	if m.Filtering() || m.Query != "readme" {
		t.Fatalf("CmdFilterStop: filtering=%v query=%q", m.Filtering(), m.Query)
	}

	cmd(m, picker.CmdFilterStart)
	if !m.Filtering() {
		t.Fatal("CmdFilterStart did not enable filtering")
	}

	cmd(m, picker.CmdFilterClear)
	if m.Filtering() || m.Query != "" || m.List().Query != "" {
		t.Fatalf("CmdFilterClear: filtering=%v query=%q listQuery=%q", m.Filtering(), m.Query, m.List().Query)
	}
	// PickAny has a pinned current-folder row at index 0; resetSel focuses first real row (Docs/).
	if e, ok := m.Focused(); !ok || e.Name != "Docs" || m.FocusedCurrent() {
		t.Fatalf("CmdFilterClear should resetSel to first real row, got %+v ok=%v current=%v", e, ok, m.FocusedCurrent())
	}
}
