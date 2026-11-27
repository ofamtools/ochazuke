package tree

import (
	"fmt"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

func largeTree(n int) *Model {
	roots := make([]Node, 0, n/5+1)
	id := 0
	for id < n {
		pID := fmt.Sprintf("t-%d", id)
		parent := Node{
			ID:         pID,
			Label:      fmt.Sprintf("Parent task %d: implement end-to-end caching and layout improvements", id),
			Ref:        &core.Ref{Kind: "item", ID: pID},
			Icon:       "●",
			IconTone:   "state.working",
			Gutter:     "●",
			GutterTone: "state.working",
			Badges:     []ui.Badge{{Text: "P1", Tone: "warn"}},
			Meta:       []ui.TreeMeta{{Text: "doing", Tone: "accent"}, {Text: "3/5", Tone: "muted"}},
		}
		id++
		for c := 0; c < 4 && id < n; c++ {
			cID := fmt.Sprintf("t-%d", id)
			parent.Children = append(parent.Children, Node{
				ID:         cID,
				Label:      fmt.Sprintf("Subtask %d: verify scroll window and cache invalidation across wide and narrow viewports", id),
				Ref:        &core.Ref{Kind: "item", ID: cID},
				Icon:       "○",
				IconTone:   "muted",
				Meta:       []ui.TreeMeta{{Text: "todo", Tone: "muted"}},
				LabelSpans: []ui.ToneSpan{{From: 0, To: 7, Tone: "accent"}},
			})
			id++
		}
		roots = append(roots, parent)
	}
	m := New(roots)
	m.PageSize = 36
	m.Wrap = true
	m.ShowGutter = true
	return m
}

func TestTreeViewCacheReuseAndInvalidation(t *testing.T) {
	m := largeTree(100)
	v0 := m.View("tr")
	nodes0 := v0.Props["nodes"].([]ui.TreeNode)

	// Pure cursor move reuses uiNodes slice while updating CacheKey.
	m.HandleCommand(core.Command{ID: "motion.down"})
	v1 := m.View("tr")
	nodes1 := v1.Props["nodes"].([]ui.TreeNode)
	if v1.CacheKey == v0.CacheKey {
		t.Fatal("cursor move did not update CacheKey")
	}
	if &nodes0[0] != &nodes1[0] {
		t.Fatal("pure cursor move rebuilt uiNodes instead of reusing cached content")
	}

	// In-place node mutation invalidates cached uiNodes and CacheKey.
	m.Nodes[0].Label = "Renamed parent"
	v2 := m.View("tr")
	nodes2 := v2.Props["nodes"].([]ui.TreeNode)
	if v2.CacheKey == v1.CacheKey || nodes2[0].Label != "Renamed parent" {
		t.Fatalf("in-place label mutation not reflected: keySame=%v label=%q", v2.CacheKey == v1.CacheKey, nodes2[0].Label)
	}

	// Fold toggle invalidates cached uiNodes and CacheKey.
	m.HandleCommand(core.Command{ID: CmdFoldToggle})
	v3 := m.View("tr")
	if v3.CacheKey == v2.CacheKey {
		t.Fatal("fold toggle did not invalidate CacheKey")
	}

	// Inline rename invalidates cached uiNodes and CacheKey.
	m.StartRename("✎ ")
	v4 := m.View("tr")
	if v4.CacheKey == v3.CacheKey {
		t.Fatal("StartRename did not invalidate CacheKey")
	}

	// Copy() does not share viewCache.
	cp := m.Copy()
	cp.CancelEdit()
	if cp.View("tr").CacheKey == v4.CacheKey {
		t.Fatal("Copy() CancelEdit did not produce distinct CacheKey")
	}
}

func BenchmarkTreeView500(b *testing.B) {
	m := largeTree(500)
	_ = m.View("tr")
	down := core.Command{ID: "motion.down"}
	up := core.Command{ID: "motion.up"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			m.HandleCommand(down)
		} else {
			m.HandleCommand(up)
		}
		_ = m.View("tr")
	}
}
