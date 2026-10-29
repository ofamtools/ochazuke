package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// zoomFixture is an editable outline r0 › a › b › c › d › e with a leaf
// a2 beside b and a root s beside r0, focused on a, on an 80x24 model.
func zoomFixture(t *testing.T, settings map[string]any) (*Runtime, *Model, *Tree) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 80, 24
	r.EnsureConfigSettings()
	for name, v := range settings {
		if err := r.Settings().Set(name, v); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return r.Tree("t").Block(w, h) }); err != nil {
		t.Fatal(err)
	}
	nodes := []TreeNode{
		{ID: "r0", Label: "root"},
		{ID: "a", Parent: "r0", Label: "alpha"},
		{ID: "b", Parent: "a", Label: "beta"},
		{ID: "c", Parent: "b", Label: "gamma"},
		{ID: "d", Parent: "c", Label: "delta"},
		{ID: "e", Parent: "d", Label: "eps"},
		{ID: "a2", Parent: "a", Label: "alpha2"},
		{ID: "s", Label: "side"},
	}
	tree, err := r.AddTree("t", nodes, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	tree.OnChange = func(TreeChange) error { return nil }
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	if err := tree.Select("a"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("a"); err != nil {
		t.Fatal(err)
	}
	return r, m, tree
}

// lines is the tree drawn at width, plain, trailing blanks trimmed.
func zoomLines(tree *Tree, width int) []string {
	var out []string
	for _, line := range strings.Split(ansi.Strip(tree.View(width, 20)), "\n") {
		if line = strings.TrimRight(line, " "); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func wantLines(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("drawn\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// z + zooms on the node: its children are the top rows, the breadcrumb
// line names its ancestors and ends with it, the first child is focused.
// z - steps out a level, z 0 shows every level.
func TestTreeZoomInOutReset(t *testing.T) {
	r, m, tree := zoomFixture(t, nil)
	keytest.Press(m, "z")
	keytest.Press(m, "+")
	if tree.Zoomed() != "a" || r.Focus != "b" {
		t.Fatalf("zoom %q focus %q, want a and b", tree.Zoomed(), r.Focus)
	}
	wantLines(t, zoomLines(tree, 40), "root › alpha", "▾ beta", "  ▾ gamma", "    ▾ delta", "        eps", "  alpha2")
	keytest.Press(m, "z")
	keytest.Press(m, "-")
	if tree.Zoomed() != "r0" || r.Focus != "b" {
		t.Fatalf("after z -: zoom %q focus %q", tree.Zoomed(), r.Focus)
	}
	wantLines(t, zoomLines(tree, 40)[:2], "root", "▾ alpha")
	keytest.Press(m, "z")
	keytest.Press(m, "0")
	if tree.Zoomed() != "" || r.Focus != "b" {
		t.Fatalf("after z 0: zoom %q focus %q", tree.Zoomed(), r.Focus)
	}
	wantLines(t, zoomLines(tree, 40)[:2], "▾ root", "  ▾ alpha")
}

// h at the zoom's top steps out one level onto the parent, as h steps
// onto the parent anywhere else.
func TestTreeZoomHStepsOut(t *testing.T) {
	r, m, tree := zoomFixture(t, nil)
	if err := tree.Zoom("a"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("a2"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "h")
	if tree.Zoomed() != "r0" || r.Focus != "a" {
		t.Fatalf("h at the top: zoom %q focus %q, want r0 and a", tree.Zoomed(), r.Focus)
	}
	keytest.Press(m, "h") // a is open: h closes it
	keytest.Press(m, "h")
	if tree.Zoomed() != "" || r.Focus != "r0" {
		t.Fatalf("h out of the last level: zoom %q focus %q", tree.Zoomed(), r.Focus)
	}
}

// The breadcrumb names the nearest tree.breadcrumb.ancestors ancestors
// and folds the farther ones into …; 0 names them all.
func TestTreeBreadcrumbAncestors(t *testing.T) {
	_, _, tree := zoomFixture(t, nil)
	if err := tree.Zoom("d"); err != nil {
		t.Fatal(err)
	}
	if got := tree.Breadcrumb(); !slices.Equal(got, []string{"…", "beta", "gamma", "delta"}) {
		t.Fatalf("breadcrumb %q", got)
	}
	wantLines(t, zoomLines(tree, 40), "… › beta › gamma › delta", "  eps")
	_ = tree.r.Settings().Set(TreeBreadcrumbAncestorsSetting, 0)
	if got := tree.Breadcrumb(); !slices.Equal(got, []string{"root", "alpha", "beta", "gamma", "delta"}) {
		t.Fatalf("breadcrumb with 0 %q", got)
	}
}

// The ancestors are dimmed (Styles.Muted); the zoomed node is not.
func TestTreeBreadcrumbDimsAncestors(t *testing.T) {
	r, _, tree := zoomFixture(t, nil)
	if err := tree.Zoom("b"); err != nil {
		t.Fatal(err)
	}
	line := strings.Split(tree.View(40, 20), "\n")[0]
	muted := r.Settings().page.styles().Muted.Render("root › alpha › ")
	if !strings.HasPrefix(line, muted) || !strings.Contains(line, "beta") {
		t.Fatalf("breadcrumb %q, want it to start with %q", line, muted)
	}
}

// tree.zoom.root row keeps the zoomed node as the top row; the line names
// only its ancestors.
func TestTreeZoomRootRow(t *testing.T) {
	r, m, tree := zoomFixture(t, map[string]any{TreeZoomRootSetting: TreeZoomRootRow})
	keytest.Press(m, "z")
	keytest.Press(m, "+")
	if tree.Zoomed() != "a" || r.Focus != "a" {
		t.Fatalf("zoom %q focus %q, want a and a", tree.Zoomed(), r.Focus)
	}
	wantLines(t, zoomLines(tree, 40)[:3], "root", "▾ alpha", "  ▾ beta")
	keytest.Press(m, "h") // closes a
	keytest.Press(m, "h")
	if tree.Zoomed() != "r0" || r.Focus != "r0" {
		t.Fatalf("h on the top row: zoom %q focus %q, want r0 and r0", tree.Zoomed(), r.Focus)
	}
}

// tree.breadcrumb title draws no line: the header shows the breadcrumb
// beside the mode while the tree has the focus; off shows it nowhere.
func TestTreeBreadcrumbTitleAndOff(t *testing.T) {
	r, _, tree := zoomFixture(t, map[string]any{TreeBreadcrumbSetting: TreeBreadcrumbTitle})
	if err := tree.Zoom("a"); err != nil {
		t.Fatal(err)
	}
	if got := zoomLines(tree, 40)[0]; got != "▾ beta" {
		t.Fatalf("first line %q, want the first row", got)
	}
	if got := r.focusDisplayLevels(); !slices.Equal(got, []string{"root", "alpha"}) {
		t.Fatalf("header levels %q", got)
	}
	_ = r.Settings().Set(FocusDisplaySetting, FocusDisplayStack)
	if got := r.focusDisplayLevels(); len(got) < 3 || !slices.Equal(got[len(got)-3:], []string{"root", "alpha", "beta"}) {
		t.Fatalf("stack levels %q, want the breadcrumb before the row", got)
	}
	_ = r.Settings().Set(TreeBreadcrumbSetting, TreeBreadcrumbOff)
	_ = r.Settings().Set(FocusDisplaySetting, FocusDisplayOff)
	if got := r.focusDisplayLevels(); got != nil {
		t.Fatalf("off: header levels %q", got)
	}
	if got := zoomLines(tree, 40)[0]; got != "▾ beta" {
		t.Fatalf("off: first line %q", got)
	}
}

// tree.zoom auto zooms when the selected row's indent takes over half the
// width, keeping two levels above it in view.
func TestTreeZoomAuto(t *testing.T) {
	r, _, tree := zoomFixture(t, map[string]any{TreeZoomSetting: TreeZoomAuto})
	if err := tree.Select("e"); err != nil {
		t.Fatal(err)
	}
	_ = r.SetFocus("e")
	tree.View(40, 20)
	if tree.Zoomed() != "" {
		t.Fatalf("zoomed %q at 40 cells", tree.Zoomed())
	}
	wantLines(t, zoomLines(tree, 20), "root › alpha › beta", "▾ gamma", "  ▾ delta", "      eps")
	if tree.Zoomed() != "b" || r.Focus != "e" {
		t.Fatalf("auto zoom %q focus %q, want b and e", tree.Zoomed(), r.Focus)
	}
	_ = tree.r.Settings().Set(TreeZoomSetting, TreeZoomManual)
	if err := tree.Zoom(""); err != nil {
		t.Fatal(err)
	}
	tree.View(20, 20)
	if tree.Zoomed() != "" {
		t.Fatal("manual zoomed by itself")
	}
}

// tree.zoom off: the zoom verbs are unavailable and a zoom in place is
// dropped at the next frame.
func TestTreeZoomOff(t *testing.T) {
	r, m, tree := zoomFixture(t, nil)
	_ = tree.Zoom("a")
	_ = r.Settings().Set(TreeZoomSetting, TreeZoomOff)
	tree.View(40, 20)
	if tree.Zoomed() != "" {
		t.Fatal("off kept the zoom")
	}
	keytest.Press(m, "z")
	keytest.Press(m, "+")
	if tree.Zoomed() != "" {
		t.Fatal("off: z + zoomed")
	}
}

// Zoom is view state: it never reaches the undo stack, and an edit that
// moves the selected row out of the zoom steps the zoom out to show it.
func TestTreeZoomUndoNeutralAndEdits(t *testing.T) {
	r, m, tree := zoomFixture(t, nil)
	before := len(tree.undo)
	keytest.Press(m, "z")
	keytest.Press(m, "+")
	keytest.Press(m, "z")
	keytest.Press(m, "-")
	if len(tree.undo) != before {
		t.Fatal("zoom went on the undo stack")
	}
	if err := tree.Zoom("a"); err != nil {
		t.Fatal(err)
	}
	_ = r.SetFocus("b")
	keytest.Press(m, "shift+tab") // b out of a: now under r0
	if tree.Zoomed() != "r0" || r.Focus != "b" {
		t.Fatalf("outdent: zoom %q focus %q, want r0 and b", tree.Zoomed(), r.Focus)
	}
	keytest.Press(m, "u")
	if tree.Zoomed() != "r0" {
		t.Fatalf("undo changed the zoom to %q", tree.Zoomed())
	}
}
