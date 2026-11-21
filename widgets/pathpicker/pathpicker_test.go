package pathpicker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
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
	if m.list.SelectedIdx != 4 {
		t.Fatalf("forwarded motion: %d", m.list.SelectedIdx)
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
