package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A tree node's Search text is matched by the / filter as a list row's
// is, and never drawn.
func TestTreeFilterSearchText(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	tree, err := r.AddTree("tree", []TreeNode{
		{ID: "a", Label: "Alpha", Search: []string{"hidden"}},
		{ID: "a1", Parent: "a", Label: "Apple", Search: []string{"inside"}},
		{ID: "b", Label: "Beta"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ansi.Strip(tree.View(60, 10)), "hidden") {
		t.Fatal("Search text is drawn")
	}
	tree.list.SetFilter("hidden")
	if got := tree.list.Shown(); got != 1 {
		t.Fatalf("hidden: %d rows shown", got)
	}
	tree.list.SetFilter("inside")
	if frame := ansi.Strip(tree.View(60, 10)); !strings.Contains(frame, "Apple") || strings.Contains(frame, "Beta") {
		t.Fatalf("inside:\n%s", frame)
	}
}

// helpKeySearch spells a key every other way it is typed: the raw string
// and each sequence without its spaces, none the shown cell holds.
func TestHelpKeySearchSpellings(t *testing.T) {
	cases := []struct {
		shown string
		raw   []string
		want  []string
	}{
		{"g x", []string{"g x"}, []string{"gx"}},
		{"ctrl+d", []string{"ctrl+d"}, nil},
		{"ctrl+a o", []string{"ctrl+a o"}, []string{"ctrl+ao"}},
		{"0-9", []string{"<digit>"}, []string{"<digit>"}},
		{"ctrl+a 0-9", []string{"ctrl+a <digit>"}, []string{"ctrl+a0-9", "ctrl+a <digit>", "ctrl+a<digit>"}},
	}
	for _, c := range cases {
		if got := helpKeySearch(c.shown, c.raw...); !slices.Equal(got, c.want) {
			t.Errorf("helpKeySearch(%q, %q) = %q, want %q", c.shown, c.raw, got, c.want)
		}
	}
}

// Every help view's / filter finds a row by its key as it is shown and
// as it is typed: "z h" and "zh", "gx", "ctrl+d" (the owner's report:
// the filter did not search the key column's forms).
func TestHelpFilterFindsKeys(t *testing.T) {
	rows := []HelpRow{
		{Key: "g x", Prefix: "g", Final: "x", Operation: "demo.reset", Noun: "demo", Section: "demo"},
		{Key: "z h", Prefix: "z", Final: "h", Operation: "demo.open", Noun: "demo", Section: "demo"},
		{Key: "ctrl+d", Final: "ctrl+d", Operation: "demo.close", Noun: "demo", Section: "demo"},
		{Key: "q", Final: "q", Operation: "demo.quit", Description: "Leave the shadowed page", Noun: "demo", Section: "demo"},
	}
	m := NewMatcher(MatchSubstring, SearchCaseSmart, "")
	finds := func(item ListItem, query string) bool {
		return NewMatcher(MatchSubstring, SearchCaseSmart, query).Match(filterHay(item, m))
	}
	items := helpItems(rows)
	byKey := map[string]ListItem{}
	for _, item := range items {
		byKey[item.Data.(HelpRow).Key] = item
	}
	for key, queries := range map[string][]string{
		"g x":    {"g x", "gx"},
		"z h":    {"z h", "zh"},
		"ctrl+d": {"ctrl+d", "demo.close"},
	} {
		for _, q := range queries {
			if !finds(byKey[key], q) {
				t.Errorf("here/pick: %q does not find %s (search %q)", q, key, byKey[key].Search)
			}
		}
	}
	if finds(byKey["q"], "zh") || finds(byKey["q"], "gx") {
		t.Error("a joined key spelling matches another row")
	}
	nodes, _ := helpNodes(rows)
	for _, n := range nodes {
		if n.Data.(HelpRow).Key == "g x" && !slices.Contains(n.Search, "gx") {
			t.Errorf("tree node drops the key's search text: %+v", n)
		}
	}

	keys := []HelpKey{{Key: "g x", Modes: []HelpCell{{Mode: "normal", Rows: []HelpRow{rows[0]}}}}}
	table := helpTableItems("demo", []string{"normal"}, keys)
	for _, q := range []string{"g x", "gx", ".reset", "demo.reset"} {
		if !finds(table[0], q) {
			t.Errorf("table: %q does not find g x (cells %q, search %q)", q, table[0].Cells, table[0].Search)
		}
	}
}
