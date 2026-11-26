package tree_test

import (
	"testing"

	"ofam.dev/ochazuke/traits"
	"ofam.dev/ochazuke/widgets/tree"
)

func TestTreeFilterableAndRecursiveVisibility(t *testing.T) {
	// 1. Plain string value object (traits.TextValue).
	tv := traits.NewTextValue(traits.FormatFilterableText("repo", "ochazuke", "branch", "feat/filter", "title", "Universal Filter"))
	var f traits.Filterable = tv
	if !f.Filter("repo:ochazuke filter", traits.MatchSubstring) || !tv.Visible || !f.IsFiltered() {
		t.Fatalf("expected multi-term key:value match on TextValue, visible=%v filtered=%v", tv.Visible, f.IsFiltered())
	}
	if f.Filter("repo:matomeru", traits.MatchSubstring) || tv.Visible {
		t.Fatalf("expected non-matching query to hide TextValue")
	}
	if !f.Filter("", traits.MatchSubstring) || !tv.Visible || f.IsFiltered() {
		t.Fatalf("expected clearing query to restore TextValue visibility")
	}

	// 2. Recursive tree filtering preserves parent branches & auto-expands folded matches.
	tm := tree.New([]tree.Node{
		{
			ID:         "p1",
			Label:      "Auth Refactor",
			SearchText: traits.FormatFilterableText("title", "Auth Refactor", "status", "doing", "repo", "ochazuke"),
			Children: []tree.Node{
				{
					ID:         "c1",
					Label:      "Add OAuth PKCE",
					SearchText: traits.FormatFilterableText("title", "Add OAuth PKCE", "status", "todo", "repo", "ochazuke"),
				},
				{
					ID:         "c2",
					Label:      "Cleanup sessions",
					SearchText: traits.FormatFilterableText("title", "Cleanup sessions", "status", "done"),
				},
			},
		},
		{
			ID:         "p2",
			Label:      "Billing Cleanup",
			SearchText: traits.FormatFilterableText("title", "Billing Cleanup", "status", "todo"),
		},
	})
	tm.Folded = map[string]bool{"p1": true}
	var tf traits.Filterable = tm

	// Child match ("pkce") keeps folded parent p1 visible and reveals only c1.
	if !tf.Filter("status:todo pkce", traits.MatchSubstring) {
		t.Fatal("expected tree filter to match descendant c1")
	}
	rows := tm.Flatten()
	if len(rows) != 2 || rows[0].ID != "p1" || rows[1].ID != "c1" {
		t.Fatalf("filtered tree rows = %+v, want [p1, c1]", rows)
	}
	v := tm.View("tasks.tree")
	if v.Facts["hasFilter"] != true || v.Facts["query"] != "status:todo pkce" {
		t.Fatalf("expected FilterFacts on tree ViewNode, got %+v", v.Facts)
	}

	// Fuzzy mode works on tree too.
	if !tf.Filter("blng", traits.MatchFuzzy) {
		t.Fatal("expected fuzzy tree match on Billing Cleanup")
	}
	rows = tm.Flatten()
	if len(rows) != 1 || rows[0].ID != "p2" {
		t.Fatalf("fuzzy rows = %+v, want [p2]", rows)
	}

	// Clearing restores original nodes and fold state.
	if !tf.Filter("", traits.MatchSubstring) || tf.IsFiltered() {
		t.Fatal("expected clearing tree filter to restore visibility")
	}
	rows = tm.Flatten()
	if len(rows) != 2 || rows[0].ID != "p1" || rows[1].ID != "p2" || !rows[0].Folded {
		t.Fatalf("after clear rows = %+v, want folded [p1, p2]", rows)
	}
}
