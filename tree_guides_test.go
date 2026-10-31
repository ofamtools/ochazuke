package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/keytest"
)

func guideFixture(t *testing.T, opts ...InstanceOption) (*Runtime, *Tree) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	r.Define("note").Props(PropSpec{Name: "by"})
	nodes := []TreeNode{
		{ID: "root", Label: "Root", Noun: "note"},
		{ID: "a", Parent: "root", Label: "Alpha", Noun: "note"},
		{ID: "a1", Parent: "a", Label: "Apple", Noun: "note", Data: Thing{"by": "owner"}, Tones: []ToneSpan{CellTone(0, ToneDone)}},
		{ID: "a2", Parent: "a", Label: "Apricot", Noun: "note"},
		{ID: "b", Parent: "root", Label: "Beta", Noun: "note"},
		{ID: "b1", Parent: "b", Label: "Berry", Noun: "note"},
		{ID: "other", Label: "Other", Noun: "note"},
	}
	tree, err := r.AddTree("tree", nodes, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return r, tree
}

func guideContains(t *testing.T, tree *Tree, want ...string) string {
	t.Helper()
	frame := ansi.Strip(tree.View(60, 20))
	for _, text := range want {
		if !strings.Contains(frame, text) {
			t.Fatalf("missing %q:\n%s", text, frame)
		}
	}
	return frame
}

func TestTreeGuides(t *testing.T) {
	r, tree := guideFixture(t, Guides())
	guideContains(t, tree, "▾ Root", "├─ ▾ Alpha", "│  ├─   Apple", "│  └─   Apricot", "└─ ▾ Beta", "   └─   Berry", "  Other")
	row := tree.list.items[2]
	if !slices.Contains(row.Tones, SpanTone(0, 0, 6, ToneTreeGuide)) || !slices.Contains(row.Tones, SpanTone(0, 8, 0, ToneDone)) {
		t.Fatalf("guides and label tones: %+v", row.Tones)
	}
	if err := r.Settings().Set(TreeGuidesSetting, "off"); err != nil {
		t.Fatal(err)
	}
	if frame := tree.List().View(60, 20); strings.ContainsAny(frame, "├└│") {
		t.Fatalf("off still shows guides through the child list:\n%s", frame)
	}
	frame := guideContains(t, tree, "      Apple")
	if strings.ContainsAny(frame, "├└│") {
		t.Fatalf("off still shows guides:\n%s", frame)
	}
	if err := r.Settings().Set(TreeGuidesSetting, "app"); err != nil {
		t.Fatal(err)
	}
	guideContains(t, tree, "│  ├─   Apple")
	_, plain := guideFixture(t)
	if frame := plain.View(60, 20); strings.ContainsAny(frame, "├└│") {
		t.Fatalf("default tree gained guides:\n%s", frame)
	}
}

func TestTreeGuidesFilteredFoldedZoomed(t *testing.T) {
	_, tree := guideFixture(t, Guides())
	if err := tree.SetCollapsed("a", true); err != nil {
		t.Fatal(err)
	}
	frame := guideContains(t, tree, "├─ ▸ Alpha", "   └─   Berry")
	if strings.Contains(frame, "Apple") {
		t.Fatal(frame)
	}
	if err := tree.list.SetPropFilters([]PropFilter{{Prop: "by", Op: "=", Value: "owner"}}); err != nil {
		t.Fatal(err)
	}
	frame = guideContains(t, tree, "└─ ▾ Alpha", "   └─   Apple")
	if strings.ContainsAny(frame, "│├") {
		t.Fatalf("hidden siblings leave guides:\n%s", frame)
	}
	tree.list.SetFilter("App")
	guideContains(t, tree, "   └─   Apple")
	_ = tree.list.SetPropFilters(nil)
	_ = tree.SetCollapsed("a", false)
	tree.list.SetFilter("Berry")
	frame = guideContains(t, tree, "   └─   Berry")
	if strings.ContainsAny(frame, "│├") {
		t.Fatal(frame)
	}
	tree.list.SetFilter("│")
	if tree.list.Shown() != 0 {
		t.Fatal("guide glyph is searchable")
	}
	tree.list.SetFilter("")
	if err := tree.Zoom("a"); err != nil {
		t.Fatal(err)
	}
	frame = guideContains(t, tree, "Apple", "Apricot")
	if strings.ContainsAny(frame, "│├└") {
		t.Fatalf("zoom top rows keep hidden ancestor guides:\n%s", frame)
	}
	if err := tree.r.Settings().Set(TreeZoomRootSetting, TreeZoomRootRow); err != nil {
		t.Fatal(err)
	}
	_ = tree.Zoom("a")
	guideContains(t, tree, "├─   Apple", "└─   Apricot")
}

func TestTreeGuidesTableNarrowAndScroll(t *testing.T) {
	_, tree := guideFixture(t, Guides(), Columns(Column{Title: "Name", Min: 16}, Column{Title: "Extra", Min: 15}, Column{Title: "More", Min: 15}))
	nodes := tree.Nodes()
	for i := range nodes {
		nodes[i].Cells = []string{nodes[i].Label, "a long second column", "third column"}
	}
	if err := tree.SetNodes(nodes); err != nil {
		t.Fatal(err)
	}
	for width := 1; width <= 45; width++ {
		frame := tree.View(width, 20)
		for _, line := range strings.Split(frame, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
		}
	}
	tree.View(40, 20)
	tree.list.scrollColumns("last", 0)
	frame := ansi.Strip(tree.View(40, 20))
	if tree.list.ColumnOffset() == 0 || !strings.Contains(frame, "More") || !strings.Contains(frame, "│  ├─   Apple") {
		t.Fatalf("scrolled columns lost guides (offset %d):\n%s", tree.list.ColumnOffset(), frame)
	}
}

func TestTreeGuideToneBuiltins(t *testing.T) {
	for _, name := range (Appearance{}).ThemeNames() {
		s := NewStyles(Appearance{Theme: name, Dark: true})
		if !s.Tone(ToneTreeGuide).GetFaint() {
			t.Errorf("%s tree.guide is not dim", name)
		}
	}
}

func TestTreeGuidesEditingNarrow(t *testing.T) {
	r, tree := guideFixture(t, Guides())
	m := NewModel(r)
	tree.OnChange = func(TreeChange) error { return nil }
	if err := tree.Select("a1"); err != nil {
		t.Fatal(err)
	}
	if err := tree.Focus(); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "i")
	if tree.Editing() != "a1" {
		t.Fatal("inline edit did not open")
	}
	for width := 1; width <= 20; width++ {
		block := tree.Block(width, 20)
		for _, line := range strings.Split(block.Text, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d: %q", width, line)
			}
		}
		if block.Cursor != nil && block.Cursor.X >= width {
			t.Fatalf("width %d cursor %+v", width, block.Cursor)
		}
	}
}
