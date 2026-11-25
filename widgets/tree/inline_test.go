package tree_test

import (
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/tree"
)

func TestTreeWidgetLabelRunsLabelSpansAndCacheKey(t *testing.T) {
	m := tree.New([]tree.Node{
		{
			ID: "n1",
			LabelRuns: []ui.InlineRun{
				{Text: "Open "},
				{Kind: "link", Label: "Doc", Ref: &core.Ref{Kind: "document", ID: "d-1"}},
			},
		},
		{
			ID:         "n2",
			Label:      "Fading",
			LabelSpans: []ui.ToneSpan{{From: 0, To: 6, Tone: "accent"}},
		},
	})

	v1 := m.View("tr")
	links := ui.NodeLinks(&v1, 0)
	if len(links) != 1 || links[0].Label != "Doc" || links[0].Ref == nil || links[0].Ref.ID != "d-1" {
		t.Fatalf("NodeLinks on tree.Model.View = %+v", links)
	}

	// Updating LabelSpans (e.g. next dissolve frame) must invalidate CacheKey.
	m.Nodes[1].LabelSpans = []ui.ToneSpan{{From: 0, To: 5, Tone: "row.dim"}, {From: 5, To: 6, Tone: "accent"}}
	v2 := m.View("tr")
	if v1.CacheKey == v2.CacheKey {
		t.Fatalf("CacheKey did not change after LabelSpans update: %d", v1.CacheKey)
	}
}
