package ochazuke

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// walkTree makes a project-shaped tree: proj/matomeru with nested folders,
// a .git, a node_modules, a .worktrees checkout, a hidden folder and a
// deep chain, plus a sibling repo.
func walkTree(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{
		"proj/matomeru/cmd/mtm",
		"proj/matomeru/internal/work",
		"proj/matomeru/.git/objects",
		"proj/matomeru/node_modules/left-pad",
		"proj/matomeru/.worktrees/lane-a/cmd",
		"proj/matomeru/.secret/inner",
		"proj/matomeru/d1/d2/d3/d4",
		"proj/other/docs",
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "proj/matomeru/cmd/mtm/main.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// landAll delivers every load until none is queued (a walk streams in
// many batches).
func landAll(m *Model, cmd tea.Cmd) {
	for range 500 {
		if cmd == nil {
			return
		}
		var next []tea.Cmd
		for _, msg := range quickMsgs(cmd) {
			_, out := m.Update(msg)
			next = append(next, out)
		}
		cmd = tea.Batch(next...)
	}
}

func walkFixture(t *testing.T, roots []PathRoot, batch int, settings map[string]any) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 100, 30
	m.Appearance.Theme = "mono"
	r.EnsureConfigSettings()
	for name, v := range settings {
		if err := r.Settings().Set(name, v); err != nil {
			t.Fatal(err)
		}
	}
	r.AddWalkPathSource(func(PathPick) PathWalk { return PathWalk{Roots: roots, Batch: batch} })
	return r, m
}

func openWalk(t *testing.T, r *Runtime, pick PathPick) *Chooser {
	t.Helper()
	c, err := r.OpenPathChooser(PathChooserSpec{Kind: "walk-test", Pick: pick, Source: PathSourceWalk})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var dirPick = PathPick{Name: "folder", Parameter: Parameter{Type: "path", Path: PathDir}}

func rowIDs(rows []ChooserRow) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.ID
	}
	return out
}

// The walk lists every folder under its root, breadth first, skipping
// ignored and hidden names but walking the included .worktrees; a
// fragment of the path finds a deep folder ("mato cmd" for
// matomeru/cmd/mtm through the separator rule).
func TestPathWalkListsNestedFolders(t *testing.T) {
	tree := walkTree(t)
	repo := filepath.Join(tree, "proj/matomeru")
	r, m := walkFixture(t, []PathRoot{{Path: repo, Label: "matomeru"}}, 0, nil)
	c := openWalk(t, r, dirPick)
	landAll(m, r.PendingCommands())
	got := rowIDs(c.Matches())
	for _, want := range []string{repo, repo + "/cmd", repo + "/cmd/mtm", repo + "/internal/work", repo + "/.worktrees/lane-a/cmd", repo + "/d1/d2/d3/d4"} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	for _, lacks := range []string{".git", "node_modules", ".secret", "main.go"} {
		for _, id := range got {
			if strings.Contains(id, lacks) {
				t.Errorf("listed %s (%s)", id, lacks)
			}
		}
	}
	if got[0] != repo {
		t.Errorf("the root is not first: %v", got)
	}
	if c.Loading() || !strings.Contains(c.Title(), "[walk ") || strings.Contains(c.Title(), "…]") {
		t.Errorf("loading %v title %q", c.Loading(), c.Title())
	}
	if row := c.Matches()[1]; len(row.Cells) != 2 || row.Cells[1] != "matomeru" || !strings.HasSuffix(row.Cells[0], "/") {
		t.Errorf("row cells %q", row.Cells)
	}
	c.SetQuery("mato cmd")
	if top, ok := c.Highlighted(); !ok || top.ID != repo+"/cmd" {
		t.Fatalf("mato cmd: top %v %v of %v", top.ID, ok, rowIDs(c.Matches()))
	}
	if !slices.Contains(rowIDs(c.Matches()), repo+"/cmd/mtm") {
		t.Fatalf("mato cmd misses cmd/mtm: %v", rowIDs(c.Matches()))
	}
}

// path.walk-depth bounds the walk below each root; path.walk-hidden lists
// hidden folders; path.walk-ignore is the names never entered.
func TestPathWalkSettings(t *testing.T) {
	tree := walkTree(t)
	repo := filepath.Join(tree, "proj/matomeru")
	r, m := walkFixture(t, []PathRoot{{Path: repo}}, 0, map[string]any{
		PathWalkDepthSetting:   2,
		PathWalkHiddenSetting:  true,
		PathWalkIgnoreSetting:  []string{".git", "cmd"},
		PathWalkIncludeSetting: []string{},
	})
	c := openWalk(t, r, dirPick)
	landAll(m, r.PendingCommands())
	got := rowIDs(c.Matches())
	if !slices.Contains(got, repo+"/d1/d2") || slices.Contains(got, repo+"/d1/d2/d3") {
		t.Errorf("depth 2: %v", got)
	}
	if !slices.Contains(got, repo+"/.secret/inner") {
		t.Errorf("hidden on: %v", got)
	}
	for _, id := range got {
		if strings.HasSuffix(id, "/cmd") || strings.Contains(id, ".git") {
			t.Errorf("ignored listed: %s", id)
		}
	}
	// The limit stops the walk and the title says so.
	r2, m2 := walkFixture(t, []PathRoot{{Path: repo}}, 0, map[string]any{PathWalkLimitSetting: 3})
	c2 := openWalk(t, r2, dirPick)
	landAll(m2, r2.PendingCommands())
	if n := len(c2.Matches()); n != 3 || !strings.Contains(c2.Title(), "path.walk-limit") {
		t.Errorf("limit 3: %d rows, title %q", n, c2.Title())
	}
}

// The first rows land before the walk finishes: the roots at once, then a
// batch of one folder shows the root's children while the chooser still
// loads, and closing the chooser cancels the rest.
func TestPathWalkStreams(t *testing.T) {
	tree := walkTree(t)
	repo := filepath.Join(tree, "proj/matomeru")
	r, m := walkFixture(t, []PathRoot{{Path: repo}}, 1, nil)
	c := openWalk(t, r, dirPick)
	if got := rowIDs(c.Matches()); !slices.Equal(got, []string{repo}) || !c.Loading() {
		t.Fatalf("before any batch: loading %v rows %v", c.Loading(), got)
	}
	cmd := r.PendingCommands()
	for _, msg := range quickMsgs(cmd) {
		_, cmd = m.Update(msg)
	}
	first := rowIDs(c.Matches())
	if !c.Loading() || !slices.Contains(first, repo+"/cmd") || slices.Contains(first, repo+"/cmd/mtm") {
		t.Fatalf("after one batch: loading %v rows %v", c.Loading(), first)
	}
	if !strings.Contains(c.Title(), "…") {
		t.Errorf("title while walking %q", c.Title())
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	for _, msg := range quickMsgs(cmd) {
		m.Update(msg)
	}
	if r.Chooser("walk-test") != nil {
		t.Fatal("chooser still open")
	}
	for _, j := range r.Jobs() {
		if j.Verb == chooserLoadVerb && j.State == JobRunning {
			t.Fatalf("walk still running after close: %+v", j)
		}
	}
}

// A root inside one walked before it is listed once; a second open reuses
// the finished walk (the cache), so a folder made since is not listed
// until the cache ages out.
func TestPathWalkNestedRootsAndCache(t *testing.T) {
	tree := walkTree(t)
	repo := filepath.Join(tree, "proj/matomeru")
	roots := []PathRoot{{Path: repo, Label: "matomeru"}, {Path: filepath.Join(tree, "proj"), Label: "proj"}, {Path: repo + "/cmd"}}
	r, m := walkFixture(t, roots, 0, nil)
	c := openWalk(t, r, dirPick)
	landAll(m, r.PendingCommands())
	got := rowIDs(c.Matches())
	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("%s listed %d times", id, n)
		}
	}
	if !slices.Contains(got, filepath.Join(tree, "proj/other/docs")) {
		t.Errorf("the second root's folders: %v", got)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "fresh"), 0o755); err != nil {
		t.Fatal(err)
	}
	c = openWalk(t, r, dirPick)
	landAll(m, r.PendingCommands())
	if slices.Contains(rowIDs(c.Matches()), repo+"/fresh") || len(c.Matches()) != len(got) {
		t.Errorf("second open did not reuse the walk: %d rows, was %d", len(c.Matches()), len(got))
	}
}

// path.walk off leaves the source out; path.first puts it first; a file
// pick lists files; a root under path.no-auto-list is never walked.
func TestPathWalkOffFirstFilesAndVolumes(t *testing.T) {
	tree := walkTree(t)
	repo := filepath.Join(tree, "proj/matomeru")
	r, _ := walkFixture(t, []PathRoot{{Path: repo}}, 0, nil)
	c, err := r.OpenPathChooser(PathChooserSpec{Kind: "walk-test", Pick: dirPick})
	if err != nil {
		t.Fatal(err)
	}
	if srcs := c.Sources(); srcs[0] != PathSourceWalk || c.Source() != PathSourceWalk {
		t.Fatalf("path.first: sources %v on %q", srcs, c.Source())
	}
	_ = r.Pop()
	if err := r.Settings().Set(PathWalkSetting, false); err != nil {
		t.Fatal(err)
	}
	c, err = r.OpenPathChooser(PathChooserSpec{Kind: "walk-test-off", Pick: dirPick})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(c.Sources(), PathSourceWalk) || c.Source() != PathSourceBrowse {
		t.Fatalf("walk off: sources %v on %q", c.Sources(), c.Source())
	}
	_ = r.Pop()

	r, m := walkFixture(t, []PathRoot{{Path: repo}}, 0, nil)
	c = openWalk(t, r, PathPick{Name: "file", Parameter: Parameter{Type: "path"}})
	landAll(m, r.PendingCommands())
	if got := rowIDs(c.Matches()); !slices.Equal(got, []string{repo + "/cmd/mtm/main.go"}) {
		t.Fatalf("file pick: %v", got)
	}

	r, m = walkFixture(t, []PathRoot{{Path: repo}}, 0, map[string]any{PathNoAutoListSetting: []string{tree + "/proj/"}})
	c = openWalk(t, r, dirPick)
	landAll(m, r.PendingCommands())
	// The root itself is a row (naming it reads nothing); nothing under it.
	if got := rowIDs(c.Matches()); !slices.Equal(got, []string{repo}) {
		t.Fatalf("a root under path.no-auto-list was walked: %v", got)
	}
}

// A space in a fuzzy query matches a path separator.
func TestFuzzySpaceMatchesSeparator(t *testing.T) {
	m := NewMatcher(MatchFuzzy, SearchCaseSmart, "mato cmd")
	if !m.Match("~/proj/matomeru/cmd/mtm") || !m.Match("matomeru cmd") || m.Match("cmd/matomeru") {
		t.Fatal("space as separator")
	}
}
