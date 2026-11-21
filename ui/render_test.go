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
	rows, plain, _ := draw(card, "", 30, 7)
	box := func(c string) string { return "│ " + padPlain(c, 26) + " │" }
	wantRows(t, rows,
		"╭────────────────────────────╮",
		box("● Keep Saved Panes?"),
		box("   new"),
		box("  Decide today"),
		box("  alice · 2h"),
		box("  k Keep  d Drop"),
		"╰────────────────────────────╯")

	rowsF, focused, _ := draw(card, "c", 30, 7)
	if !strings.HasPrefix(rowsF[0], "┏━") || !strings.HasPrefix(rowsF[6], "┗━") {
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
	if f3.Cursor.Shape != buffer.CursorBar || f3.Cursor.X != 2+1+3 || f3.Cursor.Y != 2 {
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

func txtNode(s string) ViewNode { return txt(s) }
