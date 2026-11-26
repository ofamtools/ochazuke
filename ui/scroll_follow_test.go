package ui

import (
	"fmt"
	"strings"
	"testing"

	"ofam.dev/ochazuke/layout"
)

// cardColumn is a KindScroll of n focusable cards, each h rows tall.
func cardColumn(n, h int, props map[string]any) ViewNode {
	var kids []ViewNode
	for i := range n {
		kids = append(kids, ViewNode{ID: fmt.Sprintf("c%d", i), Kind: KindText, Focusable: true,
			Box: layout.Constraints{FixedH: h}, Props: map[string]any{"text": fmt.Sprintf("card %d", i)}})
	}
	if props == nil {
		props = map[string]any{}
	}
	return ViewNode{ID: "col", Kind: KindScroll, Children: kids, Props: props}
}

func scrollOff(t *testing.T, r *Renderer, root ViewNode, focus string, h int) int {
	t.Helper()
	_, s := r.Render(root, DefaultDarkTheme(), focus, 30, h)
	info, ok := s.Scrolls["col"]
	if !ok {
		t.Fatalf("no ScrollInfo for col")
	}
	return info.Offset
}

// statelessOff is the original offset for a focused card [top, top+h).
func statelessOff(top, h, vh, maxOff int) int {
	off := 0
	if top+h > vh {
		off = top + h - vh
	}
	return min(max(min(off, top), 0), maxOff)
}

func TestScrollFollowPaddedAndStateful(t *testing.T) {
	const cards, ch, vh, pad = 20, 3, 12, 2
	maxOff := cards*ch - vh
	r := NewRenderer(0)
	r.ScrollOff = pad
	col := cardColumn(cards, ch, nil)

	off := 0
	for i := range cards {
		off = scrollOff(t, r, col, fmt.Sprintf("c%d", i), vh)
		bottom := i*ch + ch
		if want := min(max(0, bottom+pad-vh), maxOff); off != want {
			t.Fatalf("down to c%d: offset %d, want %d", i, off, want)
		}
		if off > 0 && off < maxOff && off+vh-bottom != pad {
			t.Fatalf("down to c%d: bottom gap %d, want %d", i, off+vh-bottom, pad)
		}
	}
	for i := cards - 1; i >= 10; i-- {
		prev := off
		off = scrollOff(t, r, col, fmt.Sprintf("c%d", i), vh)
		top := i * ch
		if top >= prev+pad {
			if off != prev {
				t.Fatalf("up to c%d inside the padded window: offset moved %d -> %d", i, prev, off)
			}
			continue
		}
		if top-off != pad {
			t.Fatalf("up to c%d: top gap %d, want %d", i, top-off, pad)
		}
	}

	// ScrollOff 0 moving down matches the original flush-bottom offsets.
	r0 := NewRenderer(0)
	for i := range cards {
		if got, want := scrollOff(t, r0, col, fmt.Sprintf("c%d", i), vh), statelessOff(i*ch, ch, vh, maxOff); got != want {
			t.Fatalf("pad 0 down to c%d: offset %d, want %d", i, got, want)
		}
	}
}

func TestScrollFollowExplicitOffsetThenDropped(t *testing.T) {
	r := NewRenderer(0)
	if got := scrollOff(t, r, cardColumn(20, 3, map[string]any{"offset": 20}), "c9", 12); got != 20 {
		t.Fatalf("explicit offset drawn as %d", got)
	}
	if got := scrollOff(t, r, cardColumn(20, 3, nil), "c9", 12); got != 20 {
		t.Fatalf("after dropping offset: %d, want 20 (no snap)", got)
	}
}

func TestScrollFollowStatelessOptOut(t *testing.T) {
	const cards, ch, vh = 20, 3, 12
	maxOff := cards*ch - vh
	field := NewRenderer(0)
	field.ScrollOff, field.StatelessScrollFollow = 2, true
	prop := NewRenderer(0)
	prop.ScrollOff = 2
	propCol := cardColumn(cards, ch, map[string]any{"followStateless": true})
	seq := []int{0, 5, 19, 17, 15, 8, 9, 2}
	for _, i := range seq {
		id := fmt.Sprintf("c%d", i)
		want := statelessOff(i*ch, ch, vh, maxOff)
		if got := scrollOff(t, field, cardColumn(cards, ch, nil), id, vh); got != want {
			t.Errorf("Renderer opt-out at %s: %d, want %d", id, got, want)
		}
		if got := scrollOff(t, prop, propCol, id, vh); got != want {
			t.Errorf("node opt-out at %s: %d, want %d", id, got, want)
		}
	}
}

func TestScrollFollowScrollKeyDropsMemory(t *testing.T) {
	r := NewRenderer(0)
	scrollOff(t, r, cardColumn(20, 3, map[string]any{"offset": 30, "scrollKey": "rec-a"}), "", 12)
	if got := scrollOff(t, r, cardColumn(20, 3, map[string]any{"scrollKey": "rec-a"}), "", 12); got != 30 {
		t.Fatalf("same key: %d, want 30", got)
	}
	// A new key drops the memory before the reveal in the same frame.
	if got := scrollOff(t, r, cardColumn(20, 3, map[string]any{"scrollKey": "rec-b"}), "c4", 12); got != 3 {
		t.Fatalf("new key revealing c4: %d, want 3 (from 0)", got)
	}
	r.Reset()
	if r.ScrollMemoLen() != 0 {
		t.Fatal("Reset should forget scroll offsets")
	}
}

func TestScrollFollowNestedRevealsVisibleFocus(t *testing.T) {
	inner := cardColumn(10, 3, nil)
	inner.ID, inner.Box = "inner", layout.Constraints{FixedH: 6}
	filler := func(id string) ViewNode {
		return ViewNode{ID: id, Kind: KindText, Box: layout.Constraints{FixedH: 10}, Props: map[string]any{"text": id}}
	}
	outer := ViewNode{ID: "outer", Kind: KindScroll, Children: []ViewNode{filler("head"), inner, filler("foot")}}
	r := NewRenderer(0)
	for _, tc := range []struct {
		focus            string
		outerOff, innOff int
	}{
		{"c0", 5, 0},  // inner rows 10..16; c0 visible at 10..13 -> outer 13-8
		{"c5", 8, 12}, // c5 at inner 15..18, inner offset 12 -> visible 13..16 -> outer 16-8
		{"c1", 8, 3},  // c1 at inner 3..6, inner offset 3 -> visible 10..13: inside outer window
	} {
		_, s := r.Render(outer, DefaultDarkTheme(), tc.focus, 30, 8)
		if got := s.Scrolls["outer"].Offset; got != tc.outerOff {
			t.Errorf("focus %s: outer offset %d, want %d", tc.focus, got, tc.outerOff)
		}
		if got := s.Scrolls["inner"].Offset; got != tc.innOff {
			t.Errorf("focus %s: inner offset %d, want %d", tc.focus, got, tc.innOff)
		}
		if fr := s.Rects[tc.focus]; fr.H != 3 {
			t.Errorf("focus %s: visible rect %+v, want fully visible", tc.focus, fr)
		}
	}
}

func renderPlainRows(r *Renderer, root ViewNode, focus string, w, h int) []string {
	f, _ := r.Render(root, DefaultDarkTheme(), focus, w, h)
	raw := strings.Split(f.ANSI, "\n")
	out := make([]string, len(raw))
	for i, ln := range raw {
		out[i] = strings.TrimRight(stripANSI(ln), " ")
	}
	return out
}

func lineIndexContaining(rows []string, sub string) int {
	for i, ln := range rows {
		if strings.Contains(ln, sub) {
			return i
		}
	}
	return -1
}

func TestTreeWrapScrollFollowPaddedStatefulAndCached(t *testing.T) {
	// Row 0 wraps across many screen lines (like a long task above short tasks),
	// while tree.Model passes logical offset=0. Viewport h=10, ScrollOff=3.
	nodes := []TreeNode{
		{Label: strings.Repeat("long wrapped task text ", 8)},
	}
	for i := 1; i <= 20; i++ {
		nodes = append(nodes, TreeNode{Label: fmt.Sprintf("task-%02d", i)})
	}
	makeTree := func(sel int, key string, extra map[string]any) ViewNode {
		props := map[string]any{
			"nodes":    nodes,
			"wrap":     true,
			"selected": sel,
			"offset":   0, // logical offset from tree.Model
		}
		if key != "" {
			props["scrollKey"] = key
		}
		for k, v := range extra {
			props[k] = v
		}
		return ViewNode{
			ID:        "proj.tasks.tree",
			Kind:      KindTree,
			Focusable: true,
			CacheKey:  uint64(sel + 100),
			Props:     props,
		}
	}

	const w, h, pad = 32, 10, 3
	r := NewRenderer(0)
	r.ScrollOff = pad

	// Moving down past the tall wrapped row keeps `pad` rows visible below selection.
	rows := renderPlainRows(r, makeTree(10, "proj-a", nil), "proj.tasks.tree", w, h)
	y := lineIndexContaining(rows, "task-10")
	if y < 0 {
		t.Fatalf("task-10 not visible:\n%s", strings.Join(rows, "\n"))
	}
	if gapBelow := (h - 1) - y; gapBelow != pad {
		t.Fatalf("down to task-10: y=%d gapBelow=%d, want %d\n%s", y, gapBelow, pad, strings.Join(rows, "\n"))
	}

	// Idle re-render hits the subtree buffer cache while preserving scroll memo.
	_ = renderPlainRows(r, makeTree(10, "proj-a", nil), "proj.tasks.tree", w, h)
	if r.Stats.CacheHits != 1 {
		t.Fatalf("idle re-render CacheHits=%d, want 1", r.Stats.CacheHits)
	}

	// Moving up inside the padded window keeps the viewport still (stateful!).
	prevTop := rows[0]
	upRows := renderPlainRows(r, makeTree(8, "proj-a", nil), "proj.tasks.tree", w, h)
	if upRows[0] != prevTop {
		t.Fatalf("up inside padded window moved viewport: %q -> %q", prevTop, upRows[0])
	}
	// Continuing up past top pad scrolls while keeping `pad` rows above selection.
	upFar := renderPlainRows(r, makeTree(5, "proj-a", nil), "proj.tasks.tree", w, h)
	y5 := lineIndexContaining(upFar, "task-05")
	if y5 != pad {
		t.Fatalf("up to task-05: y=%d, want %d (top pad)\n%s", y5, pad, strings.Join(upFar, "\n"))
	}

	// Changing scrollKey drops the remembered offset cleanly.
	rKey := renderPlainRows(r, makeTree(0, "proj-b", nil), "proj.tasks.tree", w, h)
	if !strings.Contains(rKey[0], "long wrapped") {
		t.Fatalf("scrollKey change did not reset to top:\n%s", strings.Join(rKey, "\n"))
	}

	// Stateless opt-out restores original flush-to-bottom clamping.
	rStateless := NewRenderer(0)
	rStateless.ScrollOff = pad
	rStateless.StatelessScrollFollow = true
	sRows := renderPlainRows(rStateless, makeTree(10, "proj-a", nil), "proj.tasks.tree", w, h)
	if ys := lineIndexContaining(sRows, "task-10"); ys != h-1 {
		t.Fatalf("StatelessScrollFollow=true: task-10 y=%d, want flush bottom %d\n%s", ys, h-1, strings.Join(sRows, "\n"))
	}
}

func TestTableScrollFollowWithoutExplicitOffset(t *testing.T) {
	rows := make([]any, 20)
	for i := range rows {
		rows[i] = []any{fmt.Sprintf("row-%02d", i)}
	}
	makeTable := func(sel int) ViewNode {
		return ViewNode{
			ID:        "dyn.table",
			Kind:      KindTable,
			Focusable: true,
			Props: map[string]any{
				"columns":  []any{"Name"},
				"rows":     rows,
				"selected": sel,
			},
		}
	}
	// h=10 -> 2 header/rule lines + 8 data rows. ScrollOff=2 keeps 2 rows below.
	r := NewRenderer(0)
	r.ScrollOff = 2
	out := renderPlainRows(r, makeTable(10), "dyn.table", 24, 10)
	y := lineIndexContaining(out, "row-10")
	if wantY := 2 + (8 - 1 - 2); y != wantY {
		t.Fatalf("dynamic KindTable row-10 y=%d, want %d (pad=2)\n%s", y, wantY, strings.Join(out, "\n"))
	}
	// Moving up inside padded window holds the table viewport still.
	prevFirst := out[2]
	outUp := renderPlainRows(r, makeTable(8), "dyn.table", 24, 10)
	if outUp[2] != prevFirst {
		t.Fatalf("dynamic KindTable moved viewport on small upward move: %q -> %q", prevFirst, outUp[2])
	}
}
