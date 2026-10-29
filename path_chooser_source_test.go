package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// PathChooserSpec.Source opens the path chooser on the named source (an
// application's known folders) whatever chooser.remember-source holds; a
// name this pick leaves out falls back to the remembered source. A source's
// Keep cuts its other cells first, so the kept path stays whole.
func TestPathChooserStartSourceAndKeep(t *testing.T) {
	root := pathTree(t)
	r := NewRuntime()
	m := NewModel(r)
	r.EnsureConfigSettings()
	path := "~/proj/a-rather-long-folder-name"
	r.AddPathSource("known", func(p PathPick) (ChooserSource, bool) {
		return ChooserSource{Keep: []int{1}, Load: func(string) Result {
			return Done([]ChooserRow{{ID: "k", Label: "k", Data: root, Cells: []string{"name", path, "feat/a-very-long-branch-name-here", "2d"}}})
		}}, p.Dir()
	})
	dir := PathPick{Parameter: Parameter{Type: "path", Path: PathDir, Base: root}}
	open := func(source string) *Chooser {
		t.Helper()
		c, err := r.OpenPathChooser(PathChooserSpec{Kind: "dirs", Pick: dir, Source: source})
		if err != nil {
			t.Fatal(err)
		}
		landLoads(m, r.PendingCommands())
		return c
	}
	c := open("known")
	if c.Source() != "known" {
		t.Fatalf("opened on %q, want known", c.Source())
	}
	if v := ansi.Strip(c.View(64, 6).Text); !strings.Contains(v, path) || strings.Contains(v, "a-very-long-branch-name-here") {
		t.Fatalf("the known source's Keep does not keep the path whole:\n%s", v)
	}
	// Recent is remembered for the kind; Source still opens on known.
	if err := c.cycle(-2); err != nil || c.Source() != "recent" {
		t.Fatalf("cycle to recent: %v %q", err, c.Source())
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if c := open("known"); c.Source() != "known" {
		t.Fatalf("Source over the remembered recent: %q", c.Source())
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	// No Source: the remembered one (recent, the last cycled to).
	if c := open(""); c.Source() != PathSourceRecent {
		t.Fatalf("no Source: %q, want the remembered recent", c.Source())
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	// A name this open does not have falls back to the remembered one.
	if c := open("nope"); c.Source() != PathSourceRecent {
		t.Fatalf("unknown Source: %q, want the remembered recent", c.Source())
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	// A file pick has no known source: the name falls back to the
	// remembered source (none for this kind), else browse.
	c, err := r.OpenPathChooser(PathChooserSpec{Kind: "files", Pick: PathPick{Parameter: Parameter{Type: "path", Base: root}}, Source: "known"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Source() != PathSourceBrowse {
		t.Fatalf("missing source: %q, want browse", c.Source())
	}
	// Browse has no Keep of its own: the spec's (none) applies.
	if k := c.keep(); k != nil {
		t.Fatalf("browse keep %v", k)
	}
}
