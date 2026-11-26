package tree_test

import (
	"reflect"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
	"ofam.dev/ochazuke/widgets/tree"
)

func sampleFilterForest() []tree.Node {
	return []tree.Node{
		{
			ID:         "p1",
			Label:      "Auth Refactor",
			SearchText: traits.FormatFilterableText("title", "Auth Refactor", "status", "doing", "repo", "ochazuke"),
			Children: []tree.Node{
				{
					ID:         "c1",
					Label:      "Add OAuth PKCE",
					SearchText: traits.FormatFilterableText("title", "Add OAuth PKCE", "status", "todo", "repo", "ochazuke"),
					Children: []tree.Node{
						{
							ID:         "gc1",
							Label:      "Verify verifier hash",
							SearchText: traits.FormatFilterableText("title", "Verify verifier hash", "status", "todo"),
						},
					},
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
			Children: []tree.Node{
				{
					ID:         "b1",
					Label:      "Invoice export",
					SearchText: traits.FormatFilterableText("title", "Invoice export", "status", "done"),
				},
			},
		},
	}
}

func rowIDs(rows []tree.VisibleRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

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

	// 2. Table-driven recursive tree visibility cases.
	cases := []struct {
		name      string
		query     string
		mode      traits.MatchMode
		wantMatch bool
		wantIDs   []string
		wantErr   bool
	}{
		{
			name:      "parent match hides non-matching children",
			query:     "Billing",
			mode:      traits.MatchSubstring,
			wantMatch: true,
			wantIDs:   []string{"p2"},
		},
		{
			name:      "parent match retains only matching child",
			query:     "repo:ochazuke",
			mode:      traits.MatchSubstring,
			wantMatch: true,
			wantIDs:   []string{"p1", "c1"},
		},
		{
			name:      "child match preserves folded ancestor",
			query:     "status:todo pkce",
			mode:      traits.MatchSubstring,
			wantMatch: true,
			wantIDs:   []string{"p1", "c1"},
		},
		{
			name:      "grandchild match preserves full ancestor chain",
			query:     "verifier",
			mode:      traits.MatchSubstring,
			wantMatch: true,
			wantIDs:   []string{"p1", "c1", "gc1"},
		},
		{
			name:      "fuzzy match on parent hides non-matching child",
			query:     "blng",
			mode:      traits.MatchFuzzy,
			wantMatch: true,
			wantIDs:   []string{"p2"},
		},
		{
			name:      "no matches returns empty rows",
			query:     "nonexistent-term",
			mode:      traits.MatchSubstring,
			wantMatch: false,
			wantIDs:   []string{},
		},
		{
			name:      "invalid regex records error and falls back cleanly",
			query:     "[unclosed",
			mode:      traits.MatchRegex,
			wantMatch: false,
			wantIDs:   []string{},
			wantErr:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tm := tree.New(sampleFilterForest())
			tm.Folded = map[string]bool{"p1": true, "p2": true}
			var tf traits.Filterable = tm

			gotMatch := tf.Filter(tc.query, tc.mode)
			if gotMatch != tc.wantMatch {
				t.Fatalf("Filter(%q, %q) = %v, want %v", tc.query, tc.mode, gotMatch, tc.wantMatch)
			}
			if got := rowIDs(tm.Flatten()); !reflect.DeepEqual(got, tc.wantIDs) {
				t.Fatalf("Flatten IDs = %v, want %v", got, tc.wantIDs)
			}
			if tc.wantErr && tf.FilterError() == "" {
				t.Fatalf("expected FilterError for %q, got empty", tc.query)
			}
			if !tc.wantErr && tf.FilterError() != "" {
				t.Fatalf("unexpected FilterError %q for %q", tf.FilterError(), tc.query)
			}

			// Canonical Nodes remain untouched; clearing restores full tree & folds.
			if !tf.Filter("", traits.MatchSubstring) || tf.IsFiltered() || tf.FilterError() != "" {
				t.Fatalf("expected clear to restore visibility and clear error (err=%q)", tf.FilterError())
			}
			rows := tm.Flatten()
			if got := rowIDs(rows); !reflect.DeepEqual(got, []string{"p1", "p2"}) || !rows[0].Folded || !rows[1].Folded {
				t.Fatalf("after clear rows = %+v, want folded [p1, p2]", rows)
			}
			if len(tm.Nodes[0].Children) != 2 || len(tm.Nodes[1].Children) != 1 {
				t.Fatalf("canonical tree children mutated: p1=%d p2=%d", len(tm.Nodes[0].Children), len(tm.Nodes[1].Children))
			}
		})
	}

	// 3. Matched parent + matched child subtree remains navigable via HandleCommand.
	tm := tree.New(sampleFilterForest())
	tm.Folded = map[string]bool{"p1": true}
	if !tm.Filter("repo:ochazuke", traits.MatchSubstring) {
		t.Fatal("expected repo:ochazuke to match p1 and c1")
	}
	if row, ok := tm.Focused(); !ok || row.ID != "p1" {
		t.Fatalf("initial focused = %+v, want p1", row)
	}
	tm.HandleCommand(core.Command{ID: "motion.down"})
	if row, ok := tm.Focused(); !ok || row.ID != "c1" {
		t.Fatalf("after motion.down focused = %+v, want c1", row)
	}
	v := tm.View("tasks.tree")
	if v.Facts["hasFilter"] != true || v.Facts["query"] != "repo:ochazuke" {
		t.Fatalf("expected FilterFacts on tree ViewNode, got %+v", v.Facts)
	}
}

func TestTreeProjectionDoesNotMutate(t *testing.T) {
	tm := tree.New(sampleFilterForest())
	// Direct field assignment without calling Filter/Clamp simulates a caller
	// relying on pure projection before any explicit mutation.
	tm.SelectedID = "stale-id"
	tm.Offset = 99
	tm.Query = "[invalid-regex"
	tm.MatchMode = traits.MatchRegex
	tm.FilterErr = ""

	_ = tm.Flatten()
	_ = tm.View("tasks.tree")
	_ = tm.ContextFrame("", "tasks.tree")
	_, _ = tm.Focused()

	if tm.SelectedID != "stale-id" || tm.Offset != 99 || tm.FilterErr != "" {
		t.Fatalf("projection mutated Model: SelectedID=%q Offset=%d FilterErr=%q", tm.SelectedID, tm.Offset, tm.FilterErr)
	}
	// FilterError and View facts still report the truthful regex error purely.
	if tm.FilterError() == "" {
		t.Fatal("expected FilterError() to report invalid regex error even without mutating FilterErr")
	}
	v := tm.View("tasks.tree")
	if v.Facts["filterError"] == "" {
		t.Fatalf("expected View facts to include truthful filterError, got %+v", v.Facts)
	}

	// Explicit Filter mutation updates FilterErr, SelectedID, and Offset cleanly.
	tm.Filter("Billing", traits.MatchSubstring)
	if tm.SelectedID != "p2" || tm.Offset != 0 || tm.FilterErr != "" {
		t.Fatalf("after explicit Filter: SelectedID=%q Offset=%d FilterErr=%q", tm.SelectedID, tm.Offset, tm.FilterErr)
	}
}
