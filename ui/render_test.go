package ui

import (
	"regexp"
	"strings"
	"testing"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/layout"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:]*m|\x1b\]8;[^\x07]*\x07`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// draw renders root and returns the plain rows, the frame and the geometry.
func draw(root ViewNode, focus string, w, h int) ([]string, Frame, layout.Solved) {
	f, s := Render(root, DefaultDarkTheme(), focus, w, h)
	rows := strings.Split(stripANSI(f.ANSI), "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows, f, s
}

func txt(s string) ViewNode { return ViewNode{Kind: KindText, Props: map[string]any{"text": s}} }

func wantRows(t *testing.T, got []string, want ...string) {
	t.Helper()
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Fatalf("row %d mismatch\n got: %q\nwant: %q\nfull:\n%s", i, at(got, i), w, strings.Join(got, "\n"))
		}
	}
}

func at(rows []string, i int) string {
	if i < len(rows) {
		return rows[i]
	}
	return "<missing>"
}

func TestRenderVStackHStack(t *testing.T) {
	rows, _, _ := draw(ViewNode{Kind: KindVStack, Gap: 1, Children: []ViewNode{txt("a"), txt("b"), txt("c")}}, "", 10, 5)
	wantRows(t, rows, "a", "", "b", "", "c")

	rows, _, _ = draw(ViewNode{Kind: KindHStack, Children: []ViewNode{txt("left"), txt("right")}}, "", 10, 2)
	wantRows(t, rows, "left right")
}

func TestRenderSplit(t *testing.T) {
	panel := func(title string) ViewNode {
		return ViewNode{Kind: KindPanel, Title: title, Children: []ViewNode{txt(title)}}
	}
	rows, _, _ := draw(ViewNode{Kind: KindSplit, Ratio: 0.5, Children: []ViewNode{panel("L"), panel("R")}}, "", 20, 4)
	wantRows(t, rows, "╭─ L ────╮╭─ R ────╮", "│ L      ││ R      │")

	rows, _, _ = draw(ViewNode{Kind: KindSplit, Ratio: 0.5, Props: map[string]any{"axis": "vertical"}, Children: []ViewNode{panel("T"), panel("B")}}, "", 8, 6)
	wantRows(t, rows, "╭─ T ──╮", "│ T    │", "╰──────╯", "╭─ B ──╮", "│ B    │", "╰──────╯")
}

func TestRenderGridResponsive(t *testing.T) {
	card := func(id string) ViewNode {
		return ViewNode{Kind: KindCard, ID: id, Title: id, Focusable: true}
	}
	grid := ViewNode{Kind: KindGrid, MinColW: 10, Gap: 1, Children: []ViewNode{card("c0"), card("c1"), card("c2"), card("c3")}}

	_, _, wide := draw(grid, "", 22, 12)
	if wide.Rects["c0"].X != 0 || wide.Rects["c1"].X != 12 || wide.Rects["c2"].Y != wide.Rects["c0"].Y+wide.Rects["c0"].H+1 {
		t.Errorf("22 cols should give 2 columns: %+v", wide.Rects)
	}
	_, _, narrow := draw(grid, "", 10, 20)
	if narrow.Rects["c1"].X != 0 || narrow.Rects["c1"].Y <= narrow.Rects["c0"].Y {
		t.Errorf("10 cols should give 1 column: %+v", narrow.Rects)
	}
	if got := strings.Join(wide.FocusOrder, ","); got != "c0,c1,c2,c3" {
		t.Errorf("FocusOrder = %s", got)
	}
}

func TestRenderPanelAndBadges(t *testing.T) {
	root := ViewNode{Kind: KindPanel, Title: "Files", Badges: []Badge{{Text: "3"}}, Children: []ViewNode{txt("hello")}}
	rows, _, _ := draw(root, "", 20, 3)
	if !strings.HasPrefix(rows[0], "╭─ Files ─") || !strings.HasSuffix(rows[0], " 3  ─╮") && !strings.HasSuffix(rows[0], " 3 ─╮") {
		t.Errorf("panel top border = %q", rows[0])
	}
	if rows[1] != "│ hello            │" || rows[2] != "╰──────────────────╯" {
		t.Errorf("panel body = %q / %q", rows[1], rows[2])
	}
}

func TestRenderCardUnreadAndFocused(t *testing.T) {
	card := ViewNode{Kind: KindCard, ID: "c", Focusable: true, Unread: true, Title: "Keep Saved Panes?",
		Badges:  []Badge{{Text: "new", Tone: "state.working"}},
		Props:   map[string]any{"summary": "Decide today", "meta": []any{"alice", "2h"}},
		Actions: []ActionHint{{Label: "Keep", Key: "k", Command: "x.keep"}, {Label: "Drop", Key: "d", Command: "x.drop"}}}
	rows, plain, _ := draw(card, "", 30, 8)
	box := func(c string) string { return "│ " + padPlain(c, 26) + " │" }
	wantRows(t, rows, // balanced: a spacer separates the head from the actions
		"╭────────────────────────────╮",
		box("● Keep Saved Panes?"),
		box("   new"),
		box("  Decide today"),
		box("  alice · 2h"),
		box(""),
		box("  k Keep  d Drop"),
		"╰────────────────────────────╯")

	rowsF, focused, _ := draw(card, "c", 30, 8)
	if !strings.HasPrefix(rowsF[0], "┏━") || !strings.HasPrefix(rowsF[7], "┗━") {
		t.Errorf("focused card should use the heavy border:\n%s", strings.Join(rowsF, "\n"))
	}
	if plain.ANSI == focused.ANSI {
		t.Error("focused and unfocused cards must differ in styling")
	}
	// The unread marker uses the unread tone (#ffc777) in both states.
	if !strings.Contains(plain.ANSI, "38;2;255;199;119") {
		t.Errorf("unread tone missing: %q", plain.ANSI)
	}
}

func TestRenderTextSpans(t *testing.T) {
	n := ViewNode{Kind: KindText, Props: map[string]any{
		"text":  "hello world",
		"spans": []any{map[string]any{"from": 0, "to": 5, "tone": "accent"}, map[string]any{"from": 6, "tone": "error", "bg": "selected"}},
	}}
	rows, f, _ := draw(n, "", 20, 1)
	if rows[0] != "hello world" {
		t.Fatalf("plain text = %q", rows[0])
	}
	if !strings.Contains(f.ANSI, "38;2;130;170;255mhello") { // accent
		t.Errorf("accent span missing: %q", f.ANSI)
	}
	if !strings.Contains(f.ANSI, "255;117;127") || !strings.Contains(f.ANSI, "48;2;59;79;135") { // error fg on selection bg
		t.Errorf("error span with bg missing: %q", f.ANSI)
	}
	// Wrapping keeps spans on the right runes.
	rows, _, _ = draw(ViewNode{Kind: KindText, Props: map[string]any{"text": "alpha beta gamma"}}, "", 10, 3)
	wantRows(t, rows, "alpha beta", "gamma")
}

func TestRenderTable(t *testing.T) {
	tbl := ViewNode{Kind: KindTable, ID: "t", Focusable: true, Props: map[string]any{
		"columns":  []any{"name", map[string]any{"title": "size", "align": "right"}},
		"rows":     []any{[]any{"render.go", 1200}, []any{"theme.go", 400}, map[string]any{"name": "keyed.go", "size": 7}},
		"selected": 1,
	}}
	rows, f, _ := draw(tbl, "t", 20, 6)
	wantRows(t, rows,
		"name       size",
		"────────────────────",
		"render.go  1200",
		"theme.go    400",
		"keyed.go      7")
	if !strings.Contains(f.ANSI, "48;2;59;79;135") {
		t.Error("selected row should carry the selection background")
	}
	_, unfocused, _ := draw(tbl, "", 20, 6)
	if unfocused.ANSI == f.ANSI || strings.Contains(unfocused.ANSI, "48;2;59;79;135") {
		t.Error("unfocused table should use the inactive selection style")
	}
	// Selection scrolls into view.
	many := make([]any, 50)
	for i := range many {
		many[i] = []any{"row", i}
	}
	tbl.Props["rows"], tbl.Props["selected"] = many, 40
	rows, _, _ = draw(tbl, "t", 20, 6)
	if !strings.Contains(strings.Join(rows, "\n"), "row     40") || !strings.Contains(rows[1], "41/50") {
		t.Errorf("selected row 40 not visible:\n%s", strings.Join(rows, "\n"))
	}
}

func TestRenderTableCellSelectAndVisual(t *testing.T) {
	base := ViewNode{Kind: KindTable, ID: "t", Focusable: true, Props: map[string]any{
		"columns":  []any{"name", map[string]any{"title": "size", "align": "right"}},
		"rows":     []any{[]any{"render.go", 1200}, []any{"theme.go", 400}, []any{"main.go", 90}},
		"selected": 1,
	}}
	_, wholeRow, _ := draw(base, "t", 20, 6)

	cell := ViewNode{Kind: KindTable, ID: "t", Focusable: true, Props: map[string]any{
		"columns":     []any{"name", map[string]any{"title": "size", "align": "right"}},
		"rows":        []any{[]any{"render.go", 1200}, []any{"theme.go", 400}, []any{"main.go", 90}},
		"selected":    1,
		"selectedCol": 1,
	}}
	rows, cellFrame, _ := draw(cell, "t", 20, 6)
	wantRows(t, rows,
		"name       size",
		"────────────────────",
		"render.go  1200",
		"theme.go    400",
		"main.go      90")
	if cellFrame.ANSI == wholeRow.ANSI {
		t.Fatal("selectedCol rendering should differ from whole-row selection")
	}
	// Selected cell uses accent fg (#82aaff -> 38;2;130;170;255) on selection bg,
	// while rest of row uses selected.inactive bg (#2d3352 -> 48;2;45;51;82).
	if !strings.Contains(cellFrame.ANSI, "38;2;130;170;255") {
		t.Errorf("selected cell missing accent fg:\n%q", cellFrame.ANSI)
	}
	if !strings.Contains(cellFrame.ANSI, "48;2;45;51;82") {
		t.Errorf("non-selected cells on selected row missing selected.inactive bg:\n%q", cellFrame.ANSI)
	}

	// Visual range highlights non-selected rows in [visualFrom, visualTo].
	vis := ViewNode{Kind: KindTable, ID: "t", Focusable: true, Props: map[string]any{
		"columns":    []any{"name", map[string]any{"title": "size", "align": "right"}},
		"rows":       []any{[]any{"render.go", 1200}, []any{"theme.go", 400}, []any{"main.go", 90}},
		"selected":   2,
		"visualFrom": 0,
		"visualTo":   2,
	}}
	_, visFrame, _ := draw(vis, "t", 20, 6)
	if !strings.Contains(visFrame.ANSI, "48;2;45;51;82") {
		t.Errorf("visual range rows missing inactive selection highlight:\n%q", visFrame.ANSI)
	}
}

func TestRenderOverlay(t *testing.T) {
	root := ViewNode{Kind: KindVStack, Children: []ViewNode{
		txt("base text"),
		{Kind: KindOverlay, ID: "dlg", Title: "Dlg", Children: []ViewNode{txt("Really?")}},
	}}
	rows, _, s := draw(root, "", 30, 9)
	if rows[0] != "base text" {
		t.Errorf("base row = %q", rows[0])
	}
	r, ok := s.Rects["dlg"]
	if !ok || r.W < 10 || r.H != 3 {
		t.Fatalf("overlay rect = %+v ok=%v", r, ok)
	}
	if !strings.Contains(rows[r.Y], "╭─ Dlg ─") || !strings.Contains(rows[r.Y+1], "│ Really?") {
		t.Errorf("overlay not painted at %+v:\n%s", r, strings.Join(rows, "\n"))
	}
	if id, _, _ := s.HitTest(r.X+2, r.Y+1); OwnerID(root, id) != "dlg" {
		t.Errorf("HitTest inside overlay = %q", id)
	}
	// An overlay as the root gets a base to float over.
	rows, _, _ = draw(ViewNode{Kind: KindOverlay, Title: "Solo", Children: []ViewNode{txt("x")}}, "", 20, 7)
	if !strings.Contains(strings.Join(rows, "\n"), "╭─ Solo ") {
		t.Errorf("root overlay missing:\n%s", strings.Join(rows, "\n"))
	}
}

func TestRenderRawCursorComposition(t *testing.T) {
	raw := ViewNode{Kind: KindRaw, ID: "r", Focusable: true, Props: map[string]any{
		"ansi":   "\x1b[31mred\x1b[0m\nline2",
		"cursor": map[string]any{"x": 2, "y": 1, "shape": "bar"},
	}}
	root := ViewNode{Kind: KindVStack, Children: []ViewNode{txt("header"), raw}}
	rows, f, _ := draw(root, "r", 12, 4)
	wantRows(t, rows, "header", "red", "line2")
	if want := (buffer.Cursor{X: 2, Y: 2, Shape: buffer.CursorBar}); f.Cursor != want {
		t.Errorf("cursor = %+v, want %+v", f.Cursor, want)
	}
	if !strings.Contains(f.ANSI, "\x1b[31mred") {
		t.Errorf("raw ANSI styling lost: %q", f.ANSI)
	}
	if _, f2, _ := draw(root, "", 12, 4); f2.Cursor.Shape != buffer.CursorHidden {
		t.Errorf("unfocused raw must hide the cursor, got %+v", f2.Cursor)
	}

	// Cursor of an input nested in a card composes through the card's chrome.
	in := ViewNode{Kind: KindInput, ID: "in", Focusable: true, Props: map[string]any{"value": "abc"}}
	_, f3, _ := draw(ViewNode{Kind: KindCard, Title: "Q", Children: []ViewNode{in}}, "in", 20, 6)
	if f3.Cursor.Shape != buffer.CursorBar || f3.Cursor.X != 2+1+3 || f3.Cursor.Y != 3 { // border, title, spacer
		t.Errorf("input cursor = %+v", f3.Cursor)
	}
}

func TestRenderScrollWindowAndFocusReveal(t *testing.T) {
	var kids []ViewNode
	for _, id := range []string{"l0", "l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9"} {
		n := txt(id)
		n.ID, n.Focusable = id, true
		kids = append(kids, n)
	}
	sc := ViewNode{Kind: KindScroll, ID: "sc", Children: kids, Props: map[string]any{}}

	rows, _, s := draw(sc, "", 8, 3)
	wantRows(t, []string{rows[0][:2], rows[1][:2], rows[2][:2]}, "l0", "l1", "l2")
	if strings.Join(s.FocusOrder, ",") != "l0,l1,l2" || len(s.Rects) != 4 {
		t.Errorf("visible geometry wrong: focus=%v rects=%v", s.FocusOrder, s.Rects)
	}
	if !strings.Contains(stripANSI(strings.Join(rows, "\n")), "┃") {
		t.Error("overflowing scroll should draw a scrollbar thumb")
	}

	sc.Props["offset"] = 5
	rows, _, _ = draw(sc, "", 8, 3)
	wantRows(t, []string{rows[0][:2], rows[1][:2], rows[2][:2]}, "l5", "l6", "l7")

	delete(sc.Props, "offset")
	rows, _, s = draw(sc, "l9", 8, 3)
	wantRows(t, []string{rows[0][:2], rows[1][:2], rows[2][:2]}, "l7", "l8", "l9")
	if strings.Join(s.FocusOrder, ",") != "l7,l8,l9" {
		t.Errorf("FocusOrder after reveal = %v", s.FocusOrder)
	}

	sc.Props["followTail"] = true
	rows, _, _ = draw(sc, "", 8, 3)
	if rows[2][:2] != "l9" {
		t.Errorf("followTail should show the last line, got %q", rows[2])
	}
}

func TestRenderContentKinds(t *testing.T) {
	root := ViewNode{Kind: KindVStack, Children: []ViewNode{
		{Kind: KindMarkdown, Props: map[string]any{"markdown": "# Title\nA **bold** `code` [link](u).\n\n- one\n- two\n\n```\nraw\n```"}},
		{Kind: KindInput, ID: "in", Props: map[string]any{"value": "", "placeholder": "type here"}},
		{Kind: KindForm, Props: map[string]any{"fields": []any{map[string]any{"name": "ok", "type": "bool"}, map[string]any{"name": "pick", "type": "string", "enum": []any{"a", "b"}}}, "values": map[string]any{"pick": "b"}}},
		{Kind: KindPicker, ID: "p", Props: map[string]any{"query": "ar", "matchMode": "substring", "selected": 0, "items": []any{"parent", "child", map[string]any{"label": "bar", "desc": "d"}}}},
		{Kind: KindTree, Props: map[string]any{"nodes": []any{map[string]any{"label": "root", "children": []any{map[string]any{"label": "leaf"}}}}}},
		{Kind: KindTerminal, Props: map[string]any{"streamId": "pty-1"}},
		{Kind: "mystery"},
	}}
	out := strings.Join(func() []string { r, _, _ := draw(root, "p", 40, 30); return r }(), "\n")
	for _, want := range []string{"Title", "A bold code link.", "• one", "raw", "type here", "ok    [ ]", "( ) a", "(•) b", "❯ ar", "parent", "▾ root", "└─ · leaf", "terminal  pty-1", "‹mystery›"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if h := MarkdownHeadings("# A\n```\n# no\n```\n## B"); len(h) != 2 || h[1] != (Heading{2, "B", 4}) {
		t.Errorf("headings = %+v", h)
	}
}

func TestRenderTabs(t *testing.T) {
	tabs := ViewNode{Kind: KindTabs, Props: map[string]any{"active": "two"}, Children: []ViewNode{
		{Kind: KindText, Title: "One", Props: map[string]any{"text": "first"}},
		{Kind: KindText, ID: "two", Title: "Two", Unread: true, Props: map[string]any{"text": "second"}},
	}}
	rows, _, _ := draw(tabs, "", 20, 4)
	if rows[0] != " One   ● Two" || !strings.Contains(rows[1], "━") || rows[2] != "second" {
		t.Errorf("tabs:\n%s", strings.Join(rows, "\n"))
	}
}

func TestRenderTinyViewportsDoNotPanic(t *testing.T) {
	root := feedTree()
	root.Children = append(root.Children,
		ViewNode{Kind: KindScroll, Children: []ViewNode{txt("x"), txt("y")}},
		ViewNode{Kind: KindOverlay, Title: "T", Children: []ViewNode{txt("x")}},
		ViewNode{Kind: KindTable, Props: map[string]any{"columns": []any{"a"}, "rows": []any{[]any{"1"}}}},
		ViewNode{Kind: KindTabs, Children: []ViewNode{txt("x")}},
		ViewNode{Kind: KindForm, Props: map[string]any{"fields": []any{map[string]any{"name": "n"}}}},
		ViewNode{Kind: KindPicker, Props: map[string]any{"items": []any{"a"}}},
	)
	for w := 0; w <= 4; w++ {
		for h := 0; h <= 4; h++ {
			f, _ := Render(root, DefaultDarkTheme(), "b1", w, h)
			if got := strings.Count(f.ANSI, "\n"); w > 0 && h > 0 && got != h-1 {
				t.Fatalf("%dx%d: frame has %d rows", w, h, got+1)
			}
		}
	}
	// A zero theme renders unstyled without escape codes.
	f, _ := Render(txtNode("plain"), Theme{}, "", 8, 1)
	if f.ANSI != "plain   " {
		t.Errorf("zero theme frame = %q", f.ANSI)
	}
}

func TestRenderFormSubmitRow(t *testing.T) {
	node := ViewNode{
		Kind:      KindForm,
		ID:        "frm",
		Focusable: true,
		Props: map[string]any{
			"fields": []any{
				map[string]any{"name": "a", "type": "string"},
				map[string]any{"name": "b", "type": "string"},
			},
			"values":      map[string]any{"a": "one", "b": "two"},
			"active":      2,
			"submitLabel": "Create",
			"submitRow":   true,
		},
	}

	rowsF, focused, _ := draw(node, "frm", 30, 8)
	joinedF := strings.Join(rowsF, "\n")
	if !strings.Contains(joinedF, "▸ [ ⏎ Create ]") {
		t.Fatalf("focused submit row missing highlight:\n%s", joinedF)
	}
	// Selected bg (#3b4f87 -> 48;2;59;79;135) and accent fg (#82aaff -> 38;2;130;170;255).
	if !strings.Contains(focused.ANSI, "48;2;59;79;135") || !strings.Contains(focused.ANSI, "38;2;130;170;255") {
		t.Errorf("focused submit row missing selected/accent styling: %q", focused.ANSI)
	}

	// Unfocused keeps subtle lowercase submit row.
	rowsU, unfocused, _ := draw(node, "", 30, 8)
	joinedU := strings.Join(rowsU, "\n")
	if !strings.Contains(joinedU, "  ⏎ create") || strings.Contains(joinedU, "[ ⏎ Create ]") {
		t.Fatalf("unfocused submit row should stay subtle:\n%s", joinedU)
	}
	if focused.ANSI == unfocused.ANSI {
		t.Error("focused and unfocused submit row should differ")
	}

	// Short viewport scrolls to anchor the highlighted submit row at the bottom.
	node.Props["fields"] = []any{
		map[string]any{"name": "f1", "type": "string"},
		map[string]any{"name": "f2", "type": "string"},
		map[string]any{"name": "f3", "type": "string"},
		map[string]any{"name": "f4", "type": "string"},
	}
	node.Props["active"] = 4
	rowsShort, _, _ := draw(node, "frm", 30, 3)
	if !strings.Contains(rowsShort[len(rowsShort)-1], "▸ [ ⏎ Create ]") {
		t.Fatalf("short viewport should pin highlighted submit row at bottom:\n%s", strings.Join(rowsShort, "\n"))
	}
}

func TestRenderTreeGuides(t *testing.T) {
	collapsed := false
	nodes := []any{
		map[string]any{
			"label": "root",
			"children": []any{
				map[string]any{
					"label": "branchA",
					"children": []any{
						map[string]any{"label": "a1"},
						map[string]any{"label": "a2", "badge": "2"},
					},
				},
				map[string]any{
					"label":    "branchB",
					"expanded": collapsed,
					"children": []any{
						map[string]any{"label": "hidden"},
					},
				},
				map[string]any{
					"label": "branchC",
					"children": []any{
						map[string]any{"label": "c1"},
					},
				},
			},
		},
	}

	// Default (guides unset) renders box-drawing tree guide lines.
	rows, _, _ := draw(ViewNode{
		Kind:  KindTree,
		ID:    "tr",
		Props: map[string]any{"nodes": nodes},
	}, "", 30, 8)
	wantRows(t, rows,
		"▾ root",
		"├─ ▾ branchA",
		"│  ├─ · a1",
		"│  └─ · a2 2",
		"├─ ▸ branchB",
		"└─ ▾ branchC",
		"   └─ · c1",
	)

	// Opt-out with guides: false falls back to plain 2-space indentation.
	rowsNoGuides, _, _ := draw(ViewNode{
		Kind:  KindTree,
		ID:    "tr",
		Props: map[string]any{"nodes": nodes, "guides": false},
	}, "", 30, 8)
	wantRows(t, rowsNoGuides,
		"▾ root",
		"  ▾ branchA",
		"    · a1",
		"    · a2 2",
		"  ▸ branchB",
		"  ▾ branchC",
		"    · c1",
	)

	// String "false" opt-out also disables guides.
	rowsStrFalse, _, _ := draw(ViewNode{
		Kind:  KindTree,
		ID:    "tr",
		Props: map[string]any{"nodes": nodes, "guides": "false"},
	}, "", 30, 8)
	wantRows(t, rowsStrFalse,
		"▾ root",
		"  ▾ branchA",
		"    · a1",
		"    · a2 2",
		"  ▸ branchB",
		"  ▾ branchC",
		"    · c1",
	)

	// Selected row preserves guide prefix and applies selection highlight.
	rowsSel, fSel, _ := draw(ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props:     map[string]any{"nodes": nodes, "selected": 2},
	}, "tr", 30, 8)
	if rowsSel[2] != "│  ├─ · a1" {
		t.Fatalf("selected row text = %q, want %q", rowsSel[2], "│  ├─ · a1")
	}
	if !strings.Contains(fSel.ANSI, "48;2;59;79;135") {
		t.Errorf("selected tree row missing selection bg: %q", fSel.ANSI)
	}
}

func TestRenderRichTableRowSpec(t *testing.T) {
	tbl := ViewNode{
		Kind:      KindTable,
		ID:        "t",
		Focusable: true,
		Props: map[string]any{
			"columns": []any{"status", "title", map[string]any{"title": "prio", "align": "right"}},
			"rows": []any{
				TableRowSpec{
					Cells:     []any{"working", "Implement phase 1", "P1"},
					Tone:      "row.active",
					CellTones: []string{"state.working", "", "state.blocked"},
					CellIcons: []string{"●", "", ""},
					IconTones: []string{"state.working", "", ""},
				},
				TableRowSpec{
					Cells:     []any{"cancelled", "Drop old shim", "P3"},
					Tone:      "row.cancelled",
					CellIcons: []string{"✕", "", ""},
					IconTones: []string{"state.cancelled", "", ""},
				},
				map[string]any{
					"cells":     []any{"done", "Ship binary", "P2"},
					"tone":      "row.dim",
					"cellTones": []any{"state.done.dim", "", ""},
					"cellIcons": []any{"✓", "", ""},
					"iconTones": []any{"state.done", "", ""},
				},
			},
			"selected": 0,
		},
	}

	rows, f, _ := draw(tbl, "t", 50, 6)
	if !strings.Contains(rows[2], "● working") || !strings.Contains(rows[3], "✕ cancelled") || !strings.Contains(rows[4], "✓ done") {
		t.Fatalf("rich table icons/text missing:\n%s", strings.Join(rows, "\n"))
	}

	ansiRows := strings.Split(f.ANSI, "\n")
	// Selected row 0 (ansiRows[2]) preserves status icon fg (#86e1fc -> 38;2;134;225;252) on selection bg (48;2;59;79;135).
	if !strings.Contains(ansiRows[2], "38;2;134;225;252") || !strings.Contains(ansiRows[2], "48;2;59;79;135") {
		t.Errorf("selected row should preserve icon fg over selection bg: %q", ansiRows[2])
	}
	// Unselected cancelled row (ansiRows[3]) has strikethrough on text, NOT on column gaps ("  ") and NOT on icon ("✕").
	if !strings.Contains(ansiRows[3], ";9") && !strings.Contains(ansiRows[3], "[9") {
		t.Errorf("cancelled row missing strikethrough SGR 9: %q", ansiRows[3])
	}
	if strings.Contains(ansiRows[3], "48;") {
		t.Errorf("unselected row without Bg must not emit background fill: %q", ansiRows[3])
	}
	// Icon "✕" must not be painted with strikethrough.
	if idx := strings.Index(ansiRows[3], "✕"); idx >= 0 {
		prefix := ansiRows[3][:idx]
		lastEsc := strings.LastIndex(prefix, "\x1b[")
		if lastEsc >= 0 && strings.Contains(prefix[lastEsc:], ";9") {
			t.Errorf("icon should not carry strikethrough: %q", prefix[lastEsc:])
		}
	}
	// When cancelled row is selected, text keeps strikethrough while icon does not.
	tbl.Props["selected"] = 1
	_, fSelCancel, _ := draw(tbl, "t", 50, 6)
	selCancelRow := strings.Split(fSelCancel.ANSI, "\n")[3]
	if !strings.Contains(selCancelRow, "48;2;59;79;135") {
		t.Errorf("selected cancelled row missing selection bg: %q", selCancelRow)
	}
	if !strings.Contains(selCancelRow, ";9") && !strings.Contains(selCancelRow, "[9") {
		t.Errorf("selected cancelled row should preserve strikethrough on text: %q", selCancelRow)
	}
}

func TestRenderRichTreeNode(t *testing.T) {
	nodes := []TreeNode{
		{
			Label:    "Alpha release",
			Tone:     "row.active",
			Icon:     "●",
			IconTone: "state.working",
			Badges:   []Badge{{Text: "P1", Tone: "error"}},
			Meta:     []TreeMeta{{Text: "2/5", Tone: "muted"}},
			Children: []TreeNode{
				{
					Label:    "Very long child task name that should truncate cleanly",
					Tone:     "row.cancelled",
					Icon:     "✕",
					IconTone: "state.blocked",
					Meta:     []TreeMeta{{Text: "(+3)", Tone: "muted"}},
				},
			},
		},
	}

	root := ViewNode{
		Kind:      KindTree,
		ID:        "tr",
		Focusable: true,
		Props: map[string]any{
			"nodes":    nodes,
			"guides":   true,
			"selected": 0,
		},
	}

	rows, f, _ := draw(root, "tr", 44, 4)
	if !strings.Contains(rows[0], "▾ ● Alpha release") || !strings.Contains(rows[0], "P1") || !strings.HasSuffix(rows[0], "2/5") {
		t.Fatalf("rich selected root row mismatch: %q", rows[0])
	}
	if !strings.Contains(rows[1], "└─ · ✕ ") || !strings.Contains(rows[1], "…") || !strings.HasSuffix(rows[1], "(+3)") {
		t.Fatalf("rich child row truncation/right-align mismatch: %q", rows[1])
	}

	ansiRows := strings.Split(f.ANSI, "\n")
	// Selected root preserves state.working icon fg (#86e1fc -> 38;2;134;225;252) on selection bg (48;2;59;79;135).
	if !strings.Contains(ansiRows[0], "38;2;134;225;252") || !strings.Contains(ansiRows[0], "48;2;59;79;135") {
		t.Errorf("selected tree row should preserve icon fg over selection bg: %q", ansiRows[0])
	}
	// Unselected child has no trailing bg fill and has strikethrough on label.
	if strings.Contains(ansiRows[1], "48;") {
		t.Errorf("unselected tree row without Bg must not emit background fill: %q", ansiRows[1])
	}
	if !strings.Contains(ansiRows[1], ";9") && !strings.Contains(ansiRows[1], "[9") {
		t.Errorf("cancelled child row missing strikethrough SGR 9: %q", ansiRows[1])
	}

	// Narrow viewport drops Meta gracefully without panicking or crushing the label.
	narrowRows, _, _ := draw(root, "tr", 12, 4)
	if strings.Contains(narrowRows[1], "(+3)") {
		t.Errorf("narrow viewport should drop Meta gracefully: %q", narrowRows[1])
	}
}

func TestRenderPickerFilterModes(t *testing.T) {
	items := []any{"alpha", "beta", "gamma"}

	// Normal mode (filtering: false) with empty query: no cursor, unfocused input bar, muted hint.
	normalEmpty := ViewNode{
		Kind:      KindPicker,
		ID:        "p",
		Focusable: true,
		Props: map[string]any{
			"items":     items,
			"query":     "",
			"selected":  0,
			"filtering": false,
		},
	}
	rowsNE, fNE, _ := draw(normalEmpty, "p", 40, 4)
	if fNE.Cursor.Shape != buffer.CursorHidden {
		t.Errorf("normal mode should hide cursor, got %+v", fNE.Cursor)
	}
	if !strings.Contains(rowsNE[0], "[/ or i: filter] 3") {
		t.Errorf("normal mode empty query header = %q, want hint \"[/ or i: filter] 3\"", rowsNE[0])
	}

	// Normal mode (filtering: false) with non-empty query: no cursor, edit hint.
	normalQuery := ViewNode{
		Kind:      KindPicker,
		ID:        "p",
		Focusable: true,
		Props: map[string]any{
			"items":     items,
			"query":     "al",
			"selected":  0,
			"filtering": false,
		},
	}
	rowsNQ, fNQ, _ := draw(normalQuery, "p", 40, 4)
	if fNQ.Cursor.Shape != buffer.CursorHidden {
		t.Errorf("normal mode with query should hide cursor, got %+v", fNQ.Cursor)
	}
	if !strings.Contains(rowsNQ[0], "❯ al") || !strings.Contains(rowsNQ[0], "[/ or i: edit] 3") {
		t.Errorf("normal mode with query header = %q, want query and \"[/ or i: edit] 3\"", rowsNQ[0])
	}

	// Filter insert mode (filtering: true): active bar cursor, focused input bar, no normal-mode hint.
	filtering := ViewNode{
		Kind:      KindPicker,
		ID:        "p",
		Focusable: true,
		Props: map[string]any{
			"items":     items,
			"query":     "al",
			"cursor":    2,
			"selected":  0,
			"filtering": true,
		},
	}
	rowsF, fF, _ := draw(filtering, "p", 40, 4)
	if want := (buffer.Cursor{X: 5, Y: 0, Shape: buffer.CursorBar}); fF.Cursor != want {
		t.Errorf("filter mode cursor = %+v, want %+v", fF.Cursor, want)
	}
	if strings.Contains(rowsF[0], "[/ or i:") {
		t.Errorf("filter mode should not show normal-mode hint: %q", rowsF[0])
	}
	if !strings.HasSuffix(rowsF[0], "3") {
		t.Errorf("filter mode header should end with count: %q", rowsF[0])
	}
	if strings.Split(fF.ANSI, "\n")[0] == strings.Split(fNQ.ANSI, "\n")[0] {
		t.Error("filter mode and normal mode header styling should differ")
	}

	// Bare KindPicker without "filtering" prop defaults to filtering=true for compatibility.
	bare := ViewNode{
		Kind:      KindPicker,
		ID:        "p",
		Focusable: true,
		Props: map[string]any{
			"items":    items,
			"query":    "a",
			"selected": 0,
		},
	}
	_, fBare, _ := draw(bare, "p", 40, 4)
	if want := (buffer.Cursor{X: 4, Y: 0, Shape: buffer.CursorBar}); fBare.Cursor != want {
		t.Errorf("bare picker default cursor = %+v, want %+v", fBare.Cursor, want)
	}
}

func TestScrollSolvedGeometry(t *testing.T) {
	var kids []ViewNode
	for i := range 20 {
		kids = append(kids, ViewNode{
			ID:        "row." + string(rune('a'+i)),
			Kind:      KindText,
			Focusable: i == 19,
			Box:       layout.Constraints{FixedH: 2},
			Props:     map[string]any{"text": "line"},
		})
	}
	root := ViewNode{
		ID:        "doc.scroll",
		Kind:      KindScroll,
		Focusable: true,
		Props:     map[string]any{"offset": 0},
		Children:  []ViewNode{{Kind: KindVStack, Children: kids}},
	}
	_, solved := Render(root, DefaultDarkTheme(), "doc.scroll", 40, 10)
	info, ok := solved.Scrolls["doc.scroll"]
	if !ok {
		t.Fatalf("missing Scrolls[doc.scroll] in %+v", solved.Scrolls)
	}
	if info.ContentH != 40 || info.ViewH != 10 || info.Offset != 0 || info.MaxOff != 30 {
		t.Fatalf("unexpected scroll info: %+v", info)
	}
	if info.ChildY["row.t"] != 38 {
		t.Fatalf("expected bottom child row.t at Y=38, got %d", info.ChildY["row.t"])
	}
}

func txtNode(s string) ViewNode { return txt(s) }
