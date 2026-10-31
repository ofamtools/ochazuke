package cliview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/cli"
	"ofam.dev/ochazuke/keytest"
)

func docsCatalog() cli.CatalogDoc {
	return cli.CatalogDoc{
		Schema: cli.Schema, Name: "md",
		Nouns: []cli.CatalogNoun{{Name: "doc", Verbs: []cli.CatalogVerb{{Name: "list", Rows: "doc"}}}},
		Pages: []cli.CatalogPage{{ID: "docs", Title: "Docs", Layout: cli.LayoutSplit,
			Lists:  []cli.CatalogList{{ID: "docs/docs", Noun: "doc"}},
			Detail: &cli.CatalogDetail{Kind: Kind, PathField: "path"}}},
	}
}

func mount(t *testing.T, opts ...Option) (*ochazuke.Model, *cli.PageView) {
	t.Helper()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	m := ochazuke.NewModel(r)
	m.Width, m.Height = 110, 24
	m.Appearance.Theme = "mono"
	cfg := cli.PageConfig{DetailKinds: map[string]cli.DetailFactory{Kind: Factory(opts...)}}
	v, err := cli.MountPage(m, docsCatalog(), docsCatalog().Pages[0], cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("docs"); err != nil {
		t.Fatal(err)
	}
	return m, v
}

func frame(m *ochazuke.Model) string { return ansi.Strip(m.View().Content) }

func press(m *ochazuke.Model, keys ...string) {
	for _, k := range keys {
		m.Update(keytest.Msg(k, false, false))
	}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestMarkdownDetailEditsTheSelectedFile: the detail opens the selected
// row's file as an editable Doc; a toggle and a new line are written to
// the file at once; selecting another row opens its file; a row without
// a path, or a missing file, says why.
func TestMarkdownDetailEditsTheSelectedFile(t *testing.T) {
	dir := t.TempDir()
	one, two := filepath.Join(dir, "one.md"), filepath.Join(dir, "two.md")
	write(t, one, "# One\n\n- [ ] first task\n")
	write(t, two, "# Two\n\nplain paragraph\n")
	m, v := mount(t)
	rows := []cli.Row{
		{ID: one, Label: "One", Data: map[string]any{"path": one}},
		{ID: two, Label: "Two", Data: map[string]any{"path": two}},
		{ID: "nopath", Label: "No path", Data: map[string]any{}},
		{ID: "gone", Label: "Gone", Data: map[string]any{"path": filepath.Join(dir, "gone.md")}},
	}
	if err := v.SetRows("docs/docs", rows); err != nil {
		t.Fatal(err)
	}
	f := frame(m)
	if !strings.Contains(f, "first task") || !strings.Contains(f, "One") {
		t.Fatalf("first file not shown:\n%s", f)
	}
	doc := Of(v)
	if doc == nil || !doc.Editable() {
		t.Fatal("no editable Doc")
	}
	if err := m.Runtime.SetFocus(doc.FocusTarget()); err != nil {
		t.Fatal(err)
	}
	press(m, "]", "x", "x")
	if got := read(t, one); !strings.Contains(got, "- [x] first task") {
		t.Fatalf("toggle not written:\n%s", got)
	}
	press(m, "o")
	for _, r := range "added line" {
		press(m, string(r))
	}
	press(m, "esc")
	if got := read(t, one); !strings.Contains(got, "added line") {
		t.Fatalf("new line not written:\n%s", got)
	}
	frame(m)
	if !v.DetailView().Focused() {
		t.Fatal("the editor does not report the focus")
	}
	if err := m.Runtime.SetFocus("docs/docs/" + two); err != nil {
		t.Fatal(err)
	}
	if f := frame(m); !strings.Contains(f, "plain paragraph") || strings.Contains(f, "first task") {
		t.Fatalf("second file not shown:\n%s", f)
	}
	press(m, "j")
	if f := frame(m); !strings.Contains(f, "No path has no path") {
		t.Fatalf("row without a path:\n%s", f)
	}
	press(m, "j")
	if f := frame(m); !strings.Contains(f, "gone.md") || Of(v) != nil {
		t.Fatalf("missing file:\n%s", f)
	}
}

// TestMarkdownDetailReloadsAChangedFile: a reload of the same row with a
// changed record reads the file again.
func TestMarkdownDetailReloadsAChangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.md")
	write(t, path, "# Doc\n\nbefore\n")
	m, v := mount(t, Author("tester"))
	set := func(mod string) {
		if err := v.SetRows("docs/docs", []cli.Row{{ID: path, Label: "Doc", Data: map[string]any{"path": path, "modified": mod}}}); err != nil {
			t.Fatal(err)
		}
	}
	set("1")
	if f := frame(m); !strings.Contains(f, "before") {
		t.Fatalf("frame:\n%s", f)
	}
	write(t, path, "# Doc\n\nafter\n")
	set("2")
	if f := frame(m); !strings.Contains(f, "after") || strings.Contains(f, "before") {
		t.Fatalf("not reloaded:\n%s", f)
	}
	if Of(v).Author != "tester" {
		t.Fatal("Author option not applied")
	}
}

// TestFactoryNeedsAPathField: a markdown detail without pathField fails
// the mount.
func TestFactoryNeedsAPathField(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	m := ochazuke.NewModel(r)
	doc := docsCatalog()
	doc.Pages[0].Detail.PathField = ""
	if _, err := cli.MountPage(m, doc, doc.Pages[0], cli.PageConfig{DetailKinds: map[string]cli.DetailFactory{Kind: Factory()}}); err == nil {
		t.Fatal("mounted without pathField")
	}
}
