package ui

import (
	"fmt"
	"slices"
	"testing"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/traits"
)

func largeWrapTreeNode(n int) ViewNode {
	nodes := make([]TreeNode, 0, n/5+1)
	id := 0
	for id < n {
		exp := true
		parent := TreeNode{
			Label:      fmt.Sprintf("Group %d: a moderately long parent heading that wraps when the terminal viewport is narrow", id),
			Expanded:   &exp,
			Icon:       "●",
			IconTone:   "state.working",
			Gutter:     "●",
			GutterTone: "state.working",
			Badges:     []Badge{{Text: "P1", Tone: "warn"}},
			Meta:       []TreeMeta{{Text: "active", Tone: "accent"}, {Text: "4/4", Tone: "muted"}},
		}
		id++
		for c := 0; c < 4 && id < n; c++ {
			lbl := fmt.Sprintf("Item %d: short label", id)
			if id%3 == 0 {
				lbl = fmt.Sprintf("Item %d: this is a much longer wrapped task description that spans multiple visual lines in a 90-column terminal pane", id)
			} else if id%7 == 0 {
				lbl = fmt.Sprintf("Item %d: multiline\nsecond line with details", id)
			} else if id%11 == 0 {
				lbl = fmt.Sprintf("Item %d: 日本語ラベル — wide runes & emoji 🚀 wrapping across lines cleanly", id)
			}
			parent.Children = append(parent.Children, TreeNode{
				Label:      lbl,
				Icon:       "○",
				IconTone:   "muted",
				Meta:       []TreeMeta{{Text: "P2", Tone: "muted"}},
				LabelSpans: []ToneSpan{{From: 0, To: 6, Tone: "accent"}},
			})
			id++
		}
		nodes = append(nodes, parent)
	}
	return ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props: map[string]any{
			"nodes":       nodes,
			"guides":      true,
			"gapTopLevel": true,
			"gutter":      true,
			"wrap":        true,
			"selected":    0,
			"offset":      0,
		},
	}
}

// referenceFullTreeLines mirrors the original unwindowed multi-line treeLines
// implementation (rendering all N rows before slicing) for pixel-identical testing.
func (c *ctx) referenceFullTreeLines(rn *rnode, w, h int) ([]string, *buffer.Cursor) {
	rows := treeRows(rn.n)
	selItem := selectedOf(rn.n)
	selRow := -1
	for i, r := range rows {
		if !r.spacer && r.itemIdx == selItem {
			selRow = i
			break
		}
	}
	vFrom, hasVFrom := rn.n.propInt("visualFrom")
	vTo, hasVTo := rn.n.propInt("visualTo")
	hasVisual := hasVFrom && hasVTo && vFrom <= vTo
	wrap := rn.n.propBool("wrap")

	var allLines []string
	var cur *buffer.Cursor
	rowStarts := make([]int, len(rows))
	rowEnds := make([]int, len(rows))
	for i, r := range rows {
		rowStarts[i] = len(allLines)
		inVis := hasVisual && !r.spacer && r.itemIdx >= vFrom && r.itemIdx <= vTo
		rLines, cx, cl, hasCur := c.renderTreeRowLines(rn, r, i == selRow, inVis, wrap, w)
		if hasCur {
			cur = c.cursorIf(rn, cx, rowStarts[i]+cl, buffer.CursorBar)
		}
		allLines = append(allLines, rLines...)
		rowEnds[i] = len(allLines) - 1
	}
	total := len(allLines)
	if h <= 0 || total <= h {
		if !IsSyntheticID(rn.id) {
			c.r.rememberScroll(rn.id, rn.n.propString("scrollKey"), 0)
		}
		return allLines, cur
	}
	seed := 0
	if o, ok := rn.n.propInt("offset"); ok && len(rows) > 0 {
		oi := clampInt(o, 0, len(rows)-1)
		seed = rowStarts[oi]
	}
	var visStart int
	if c.followStateless(rn) {
		visStart = min(max(seed, 0), max(0, total-h))
		if selRow >= 0 && selRow < len(rows) {
			selStart, selEnd := rowStarts[selRow], rowEnds[selRow]
			if selEnd-selStart+1 <= h {
				if selEnd >= visStart+h {
					visStart = selEnd - h + 1
				}
				if selStart < visStart {
					visStart = selStart
				}
			} else if cur == nil {
				if selStart >= visStart+h || selEnd < visStart {
					visStart = selStart
				}
			}
		}
		if cur != nil {
			if cur.Y >= visStart+h {
				visStart = cur.Y - h + 1
			}
			if cur.Y < visStart {
				visStart = cur.Y
			}
		}
		visStart = min(max(visStart, 0), max(0, total-h))
		if !IsSyntheticID(rn.id) {
			c.r.rememberScroll(rn.id, rn.n.propString("scrollKey"), visStart)
		}
	} else {
		var span traits.ItemSpan
		hasSpan := false
		if selRow >= 0 && selRow < len(rows) {
			selStart, selEnd := rowStarts[selRow], rowEnds[selRow]
			if cur != nil && selEnd-selStart+1 > h {
				span, hasSpan = traits.ItemSpan{Y: cur.Y, H: 1}, true
			} else {
				span, hasSpan = traits.ItemSpan{Y: selStart, H: selEnd - selStart + 1}, true
			}
		} else if cur != nil {
			span, hasSpan = traits.ItemSpan{Y: cur.Y, H: 1}, true
		}
		visStart = c.revealScroll(rn, total, h, seed, span, hasSpan)
	}
	win := allLines[visStart:min(total, visStart+h)]
	if cur != nil {
		cur.Y -= visStart
		if cur.Y < 0 || cur.Y >= len(win) {
			cur = nil
		}
	}
	return win, cur
}

func TestTreeWrapWindowPixelIdentical(t *testing.T) {
	th := DefaultDarkTheme()
	st := newStyler(th)

	cases := []struct {
		name             string
		w, h             int
		selected, offset int
		vFrom, vTo       int
		hasVisual        bool
		editChildIdx     int // -1 for none
		gutter           bool
	}{
		{"top_wide", 90, 40, 0, 0, 0, 0, false, -1, true},
		{"top_mid_wide", 90, 40, 25, 12, 0, 0, false, -1, true},
		{"mid_wide", 90, 40, 250, 235, 0, 0, false, -1, true},
		{"bottom_wide", 90, 40, 499, 470, 0, 0, false, -1, true},
		{"mid_narrow_heavy_wrap", 36, 20, 250, 240, 0, 0, false, -1, true},
		{"visual_range_scrolled", 60, 24, 120, 110, 112, 125, true, -1, true},
		{"no_gutter_short_viewport", 50, 5, 300, 298, 0, 0, false, -1, false},
		{"inline_edit_visible", 50, 12, 6, 3, 0, 0, false, 1, true},
		{"inline_edit_far_scroll", 50, 10, 250, 245, 0, 0, false, 1, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := largeWrapTreeNode(500)
			node.Props["selected"] = tc.selected
			node.Props["offset"] = tc.offset
			node.Props["gutter"] = tc.gutter
			if tc.hasVisual {
				node.Props["visualFrom"] = tc.vFrom
				node.Props["visualTo"] = tc.vTo
			}
			if tc.editChildIdx >= 0 {
				ns := node.Props["nodes"].([]TreeNode)
				ns[1].Children[0].Editing = true
				ns[1].Children[0].EditPrefix = "✎ "
				ns[1].Children[0].EditValue = "editing a long wrapped inline title\nwith a second line"
				ns[1].Children[0].EditCursor = 42
				node.Props["nodes"] = ns
			}

			rGot := NewRenderer(4)
			cGot := &ctx{r: rGot, st: st}
			rnGot := &rnode{id: "tr", n: &node, isFocus: true, onFocus: true}
			gotLines, gotCur := cGot.treeLines(rnGot, tc.w, tc.h)

			rRef := NewRenderer(4)
			cRef := &ctx{r: rRef, st: st}
			rnRef := &rnode{id: "tr", n: &node, isFocus: true, onFocus: true}
			refLines, refCur := cRef.referenceFullTreeLines(rnRef, tc.w, tc.h)

			if !slices.Equal(gotLines, refLines) {
				t.Fatalf("ANSI lines differ (got %d lines, ref %d lines)", len(gotLines), len(refLines))
			}
			if (gotCur == nil) != (refCur == nil) || (gotCur != nil && *gotCur != *refCur) {
				t.Fatalf("cursor mismatch: got=%+v ref=%+v", gotCur, refCur)
			}
		})
	}
}

func BenchmarkTreeWrapDraw500(b *testing.B) {
	th := DefaultDarkTheme()
	root := largeWrapTreeNode(500)
	r := NewRenderer(4)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sel := i % 500
		root.Props["selected"] = sel
		root.Props["offset"] = max(0, sel-10)
		root.CacheKey = uint64(i + 1)
		_, _ = r.Render(root, th, "tr", 90, 40)
	}
}
