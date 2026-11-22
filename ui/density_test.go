package ui

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/buffer"
	"ofam.dev/ochazuke/layout"
)

func drawTheme(root ViewNode, th Theme, focus string, w, h int) ([]string, Frame, layout.Solved) {
	f, s := Render(root, th, focus, w, h)
	rows := strings.Split(stripANSI(f.ANSI), "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows, f, s
}

func withDensity(d string) Theme {
	th := DefaultDarkTheme()
	th.Density = d
	return th
}

func TestThemePresets(t *testing.T) {
	ps := ThemePresets()
	want := []struct {
		name string
		d    string
		b    string
		dark bool
	}{
		{"ochazuke-dark", DensityBalanced, BorderRounded, true},
		{"ochazuke-dark-compact", DensityCompact, BorderMinimal, true},
		{"ochazuke-dark-spacious", DensitySpacious, BorderRounded, true},
		{"ochazuke-light", DensityBalanced, BorderRounded, false},
		{"ochazuke-high-contrast-dark", DensityBalanced, BorderSharp, true},
	}
	if len(ps) != len(want) {
		t.Fatalf("got %d presets", len(ps))
	}
	for i, w := range want {
		p := ps[i]
		if p.Name != w.name || p.Density != w.d || p.BorderStyle != w.b || p.Dark != w.dark || p.Version == 0 {
			t.Errorf("preset %d = %s %s %s dark=%v v%d", i, p.Name, p.Density, p.BorderStyle, p.Dark, p.Version)
		}
		for _, tone := range []string{"text", "input.focus", "border.focus", "error", "selected"} {
			if ts := p.Tone(tone); ts.Fg == "" && ts.Bg == "" {
				t.Errorf("%s: tone %s does not resolve", p.Name, tone)
			}
		}
	}
	dark := DefaultDarkTheme()
	if dark.Roles["text"] != "#c8d3f5" || dark.Roles["accent"] != "#82aaff" || dark.Tones["error"] != (ToneStyle{Fg: "error", Bold: true}) {
		t.Error("default dark palette changed")
	}
	for _, p := range ps[1:3] {
		for k, v := range dark.Roles {
			if p.Roles[k] != v {
				t.Errorf("%s role %s = %s, want the dark palette", p.Name, k, p.Roles[k])
			}
		}
	}
	ps[1].Roles["text"], ps[1].Tones["text"] = "#000000", ToneStyle{}
	if ps[0].Roles["text"] == "#000000" || ps[2].Tones["text"].Fg == "" || ThemePresets()[1].Roles["text"] == "#000000" {
		t.Error("presets must not share maps")
	}
	var zero Theme
	if zero.EffectiveDensity() != DensityBalanced || zero.EffectiveBorderStyle() != BorderRounded || zero.Density != "" {
		t.Error("empty density/border must mean balanced/rounded without rewriting the theme")
	}
	odd := Theme{Density: "huge", BorderStyle: "dotted"}
	if odd.EffectiveDensity() != DensityBalanced || odd.EffectiveBorderStyle() != BorderRounded {
		t.Error("unknown values must fall back to the defaults")
	}
}

func TestFrameBorderStyles(t *testing.T) {
	panel := ViewNode{Kind: KindPanel, ID: "p", Title: "T", Children: []ViewNode{{Kind: KindText, ID: "x", Focusable: true, Props: map[string]any{"text": "hi"}}}}
	style := func(b string) Theme { th := DefaultDarkTheme(); th.BorderStyle = b; return th }
	var inner layout.Rect
	for _, tc := range []struct {
		b                    string
		top, mid             string
		focusTop, focusedMid string
	}{
		{BorderRounded, "╭─ T ────╮", "│ hi     │", "┏━ T ━━━━┓", "┃ hi     ┃"},
		{BorderSharp, "┌─ T ────┐", "│ hi     │", "┏━ T ━━━━┓", "┃ hi     ┃"},
		{BorderMinimal, "── T ─────", "  hi", "── T ─────", "┃ hi"},
		{BorderNone, "   T", "  hi", "   T", "  hi"},
	} {
		rows, _, s := drawTheme(panel, style(tc.b), "", 10, 3)
		wantRows(t, rows, tc.top, tc.mid)
		// Focus inside the panel (the same ancestor rule as the accent border tone).
		frows, _, _ := drawTheme(panel, style(tc.b), "x", 10, 3)
		wantRows(t, frows, tc.focusTop, tc.focusedMid)
		if inner == (layout.Rect{}) {
			inner = s.InnerRects["p"]
		} else if s.InnerRects["p"] != inner {
			t.Errorf("%s: inner rect %+v, want %+v (border style must not move layout)", tc.b, s.InnerRects["p"], inner)
		}
		if tc.b == BorderNone && strings.ContainsAny(strings.Join(rows, ""), "─│╭╮╰╯┃") {
			t.Errorf("none must draw no border glyphs:\n%s", strings.Join(rows, "\n"))
		}
	}
	// Focused cards and overlays use the heavy frame in rounded and sharp.
	card := ViewNode{Kind: KindCard, ID: "c", Focusable: true, Title: "Card"}
	ov := ViewNode{Kind: KindOverlay, ID: "o", Title: "O", Props: map[string]any{"widthPct": 100}, Children: []ViewNode{{Kind: KindText, ID: "ox", Focusable: true, Props: map[string]any{"text": "x"}}}}
	for _, b := range []string{BorderRounded, BorderSharp} {
		if rows, _, _ := drawTheme(ov, style(b), "ox", 12, 3); !strings.HasPrefix(rows[0], "┏━ O") {
			t.Errorf("%s focused overlay:\n%s", b, strings.Join(rows, "\n"))
		}
		if rows, _, _ := drawTheme(card, style(b), "c", 12, 3); !strings.HasPrefix(rows[0], "┏━") || !strings.HasPrefix(rows[2], "┗━") {
			t.Errorf("%s focused card:\n%s", b, strings.Join(rows, "\n"))
		}
	}
	if rows, _, _ := drawTheme(card, style(BorderSharp), "", 12, 3); rows[0] != "┌──────────┐" {
		t.Errorf("sharp card top = %q", rows[0])
	}
	// A long title truncates inside the edge and keeps both corners.
	long := ViewNode{Kind: KindPanel, Title: "A very long panel title", Badges: []Badge{{Text: "9"}}}
	if rows, _, _ := drawTheme(long, style(BorderSharp), "", 12, 2); rows[0] != "┌─ A ver… ─┐" {
		t.Errorf("long title = %q", rows[0])
	}
}

func cardWithBody() ViewNode {
	return ViewNode{Kind: KindCard, ID: "c", Title: "Head", Props: map[string]any{"summary": "sum"}, Children: []ViewNode{txt("body")},
		Actions: []ActionHint{{Key: "k", Label: "Keep"}}}
}

func TestCardDensity(t *testing.T) {
	card := cardWithBody()
	for _, tc := range []struct {
		d    string
		rows []string
	}{
		{DensityCompact, []string{"╭──────────────╮", "│   Head       │", "│   sum        │", "│ body         │", "│   k Keep     │", "╰──────────────╯"}},
		{DensityBalanced, []string{"╭──────────────╮", "│   Head       │", "│   sum        │", "│              │", "│ body         │", "│   k Keep     │", "╰──────────────╯"}},
		{DensitySpacious, []string{"╭──────────────╮", "│              │", "│    Head      │", "│    sum       │", "│              │", "│  body        │", "│    k Keep    │", "│              │", "╰──────────────╯"}},
	} {
		if got := measure(&card, 16, tc.d); got != len(tc.rows) {
			t.Errorf("%s: measured %d rows, want %d", tc.d, got, len(tc.rows))
		}
		root := ViewNode{Kind: KindVStack, Children: []ViewNode{card}}
		rows, _, _ := drawTheme(root, withDensity(tc.d), "", 16, len(tc.rows)+1)
		wantRows(t, rows, tc.rows...)
		if rows[len(tc.rows)] != "" {
			t.Errorf("%s: card taller than measured:\n%s", tc.d, strings.Join(rows, "\n"))
		}
	}
	// A head-only card gets no spacer at any density.
	bare := ViewNode{Kind: KindCard, Title: "only"}
	if measure(&bare, 16, DensityBalanced) != 3 || measure(&bare, 16, DensityCompact) != 3 {
		t.Error("head-only cards must not grow")
	}
}

func formNode(active int, errs map[string]any) ViewNode {
	return ViewNode{Kind: KindForm, ID: "f", Focusable: true, Props: map[string]any{
		"fields": []any{
			map[string]any{"name": "name", "type": "string", "required": true, "description": "who you are"},
			map[string]any{"name": "on", "type": "bool"},
			map[string]any{"name": "mode", "type": "string", "enum": []any{"a", "b"}},
		},
		"values": map[string]any{"name": "abc", "on": true, "mode": "b"},
		"active": active,
		"errors": errs,
	}}
}

func TestFormDensityRows(t *testing.T) {
	const w = 30
	for _, tc := range []struct {
		d      string
		rows   []string
		cursor buffer.Cursor
	}{
		{DensityCompact, []string{
			"▸ name*  abc",
			"  on    [x]",
			"  mode  ( ) a  (•) b",
			"  ⏎ submit",
		}, buffer.Cursor{X: 12, Y: 0, Shape: buffer.CursorBar}},
		{DensityBalanced, []string{
			"",
			"▸ name*  abc",
			"         who you are",
			"",
			"  on    [x]",
			"",
			"",
			"  mode  ( ) a  (•) b",
			"",
			"  ⏎ submit",
		}, buffer.Cursor{X: 12, Y: 1, Shape: buffer.CursorBar}},
		{DensitySpacious, []string{
			strings.Repeat("─", w),
			"",
			"▸ name*  abc",
			"         who you are",
			strings.Repeat("─", w),
			"",
			"  on    [x]",
			"",
		}, buffer.Cursor{X: 12, Y: 2, Shape: buffer.CursorBar}},
	} {
		n := formNode(0, nil)
		if got, want := measure(&n, w, tc.d), 3*formRows(tc.d)+1; got != want {
			t.Errorf("%s: measured %d, want %d", tc.d, got, want)
		}
		rows, f, s := drawTheme(ViewNode{Kind: KindVStack, Children: []ViewNode{n}}, withDensity(tc.d), "f", w, 20)
		wantRows(t, rows, tc.rows...)
		if s.Rects["f"].H != 3*formRows(tc.d)+1 {
			t.Errorf("%s: form rect %+v", tc.d, s.Rects["f"])
		}
		if f.Cursor != tc.cursor {
			t.Errorf("%s: cursor %+v, want %+v", tc.d, f.Cursor, tc.cursor)
		}
		// The active control row carries the input.focus background; the others the plain input one.
		ansi := strings.Split(f.ANSI, "\n")
		focusBg := sgrBg(DefaultDarkTheme(), "input.focus")
		if !strings.Contains(ansi[tc.cursor.Y], focusBg) {
			t.Errorf("%s: active control row lacks input.focus: %q", tc.d, ansi[tc.cursor.Y])
		}
		if _, f2, _ := drawTheme(n, withDensity(tc.d), "", w, 20); strings.Contains(f2.ANSI, focusBg) || f2.Cursor.Shape != buffer.CursorHidden {
			t.Errorf("%s: unfocused form must not look active", tc.d)
		}
	}
}

func sgrBg(th Theme, tone string) string {
	st := newStyler(th)
	return st.sgr(ToneStyle{Bg: st.tone(tone).Bg})[2:] // drop ESC[ so the match ignores attribute order
}

func TestFormErrorsAndEscaping(t *testing.T) {
	errs := map[string]any{"name": "required", "on": "nope"}
	rows, _, _ := drawTheme(formNode(1, errs), withDensity(DensityCompact), "f", 30, 6)
	if !strings.HasPrefix(rows[0], "  name*  abc") || !strings.HasSuffix(rows[0], "✗ required") || rows[1] != "▸ on    [x] ✗ nope" {
		t.Errorf("compact inline errors:\n%s", strings.Join(rows, "\n"))
	}
	rows, _, _ = drawTheme(formNode(1, errs), withDensity(DensityBalanced), "f", 30, 12)
	if rows[2] != "         ✗ required" || rows[5] != "         ✗ nope" {
		t.Errorf("balanced errors replace the description below:\n%s", strings.Join(rows, "\n"))
	}
	n := formNode(0, map[string]any{"name": "bad\x1b[2J"})
	n.Props["values"] = map[string]any{"name": "x\x1b[31my\nz"}
	_, f, _ := drawTheme(n, withDensity(DensityBalanced), "f", 30, 12)
	plain := stripANSI(f.ANSI)
	if strings.Contains(plain, "\x1b") || !strings.Contains(plain, "x�[31my↵z") || !strings.Contains(plain, "bad�[2J") {
		t.Errorf("field text must be escaped: %q", plain)
	}
}

func TestFormLongValueCursorAndLowHeight(t *testing.T) {
	n := formNode(0, nil)
	n.Props["values"] = map[string]any{"name": strings.Repeat("v", 40)}
	rows, f, _ := drawTheme(n, withDensity(DensityCompact), "f", 20, 4)
	// The value column starts at 8 and text at 9; the text scrolls so the cursor stays on screen.
	if f.Cursor.X != 19 || f.Cursor.Y != 0 || strW(rows[0]) > 20 {
		t.Errorf("long value cursor %+v row %q", f.Cursor, rows[0])
	}

	// Too short for every field: the active control stays visible and submit stays pinned.
	for _, d := range []string{DensityCompact, DensityBalanced, DensitySpacious} {
		low := ViewNode{Kind: KindVStack, Children: []ViewNode{formNode(2, nil)}}
		low.Children[0].Box.FixedH = 3
		rows, f, _ := drawTheme(low, withDensity(d), "f", 30, 3)
		if rows[2] != "  ⏎ submit" || f.Cursor.Shape != buffer.CursorHidden {
			t.Errorf("%s: submit row not pinned:\n%s", d, strings.Join(rows, "\n"))
		}
		found := false
		for _, r := range rows[:2] {
			found = found || strings.HasPrefix(r, "▸ mode")
		}
		if !found {
			t.Errorf("%s: active field scrolled away:\n%s", d, strings.Join(rows, "\n"))
		}
		one := ViewNode{Kind: KindVStack, Children: []ViewNode{formNode(0, nil)}}
		one.Children[0].Box.FixedH = 1
		_, f1, _ := drawTheme(one, withDensity(d), "f", 30, 1)
		if f1.Cursor != (buffer.Cursor{X: 12, Y: 0, Shape: buffer.CursorBar}) {
			t.Errorf("%s: one-row form cursor %+v", d, f1.Cursor)
		}
	}
}

// A caller that slices a tall form for one row per field (o-csv-next's
// formWindow) still sees the active field and an exact cursor once balanced
// rows make the form taller than its overlay.
func TestOverlayTallFormKeepsActiveField(t *testing.T) {
	var fields []any
	for i := 0; i < 20; i++ {
		fields = append(fields, map[string]any{"name": "f" + itoa(10+i)})
	}
	form := ViewNode{Kind: KindForm, ID: "form", Focusable: true, Props: map[string]any{"fields": fields, "active": 10, "values": map[string]any{"f20": "xy"}}}
	ov := ViewNode{Kind: KindOverlay, Title: "T", Props: map[string]any{"widthPct": 70}, Children: []ViewNode{form, txt("hint")}}
	rows, f, s := drawTheme(ViewNode{Kind: KindVStack, Children: []ViewNode{txt("bg"), ov}}, DefaultDarkTheme(), "form", 40, 20)
	fr := s.Rects["form"]
	ctl := f.Cursor.Y
	if fr.H > 18 || ctl < fr.Y || ctl >= fr.Y+fr.H || !strings.Contains(rows[ctl], "▸ f20") || !strings.Contains(rows[fr.Y+fr.H-1], "⏎ submit") {
		t.Fatalf("active field or submit hidden: cursor %+v rect %+v\n%s", f.Cursor, fr, strings.Join(rows, "\n"))
	}
	if want := fr.X + 7 + 1 + 2; f.Cursor.X != want { // value column 7 ("▸ f20  "), its leading space, "xy"
		t.Errorf("cursor X = %d, want %d", f.Cursor.X, want)
	}
}

func TestLightCanvas(t *testing.T) {
	root := ViewNode{Kind: KindPanel, Title: "T", Children: []ViewNode{txt("hi")}}
	light, _ := Render(root, DefaultLightTheme(), "", 10, 3)
	st := newStyler(DefaultLightTheme())
	canvas := st.sgr(ToneStyle{Fg: st.tone("text").Fg, Bg: st.tone("canvas").Bg})
	for i, row := range strings.Split(light.ANSI, "\n") {
		if !strings.HasPrefix(row, canvas) || strings.Count(row, reset) != strings.Count(row, reset+canvas)+1 {
			t.Errorf("light row %d is not laid on the canvas: %q", i, row)
		}
	}
	dark, _ := Render(root, DefaultDarkTheme(), "", 10, 3)
	if strings.Contains(dark.ANSI, "48;") {
		t.Errorf("default dark must not paint a background: %q", dark.ANSI)
	}
}

func TestRendererCacheKeysDensityAndBorder(t *testing.T) {
	r := NewRenderer(0)
	th := DefaultDarkTheme()
	r.Render(cachedTree(3), th, "", 30, 12)
	for _, step := range []struct {
		name   string
		th     Theme
		hits   int
		misses int
	}{
		{"same", th, 1, 0},
		{"empty density means balanced", func() Theme { x := th; x.Density, x.BorderStyle = "", ""; return x }(), 1, 0},
		{"compact", withDensity(DensityCompact), 0, 1},
		{"compact again", withDensity(DensityCompact), 1, 0},
		{"sharp", func() Theme { x := withDensity(DensityCompact); x.BorderStyle = BorderSharp; return x }(), 0, 1},
	} {
		r.Render(cachedTree(3), step.th, "", 30, 12)
		if r.Stats.CacheHits != step.hits || r.Stats.CacheMisses != step.misses {
			t.Errorf("%s: stats %+v", step.name, r.Stats)
		}
	}
	// A cached frame under one style never leaks into another.
	a, _ := NewRenderer(0).Render(cachedTree(3), th, "", 30, 12)
	sharp := th
	sharp.BorderStyle = BorderSharp
	r2 := NewRenderer(0)
	r2.Render(cachedTree(3), th, "", 30, 12)
	b, _ := r2.Render(cachedTree(3), sharp, "", 30, 12)
	if a.ANSI == b.ANSI || !strings.Contains(stripANSI(b.ANSI), "┌") {
		t.Error("border style change must re-render cached subtrees")
	}
}

func TestFormTextScrollWidthsAndNarrow(t *testing.T) {
	cursorFor := func(val string, w int) (buffer.Cursor, string) {
		n := formNode(0, nil)
		n.Props["values"] = map[string]any{"name": val}
		rows, f, _ := drawTheme(n, withDensity(DensityCompact), "f", w, 4)
		return f.Cursor, rows[0]
	}
	// Wide and combining text: the cursor lands right after the visible text.
	for _, tc := range []struct {
		val  string
		want int
	}{
		{"日本", 9 + 4},
		{"éé", 9 + 2},
		{strings.Repeat("日", 30), 19}, // scrolled: the cursor stays inside the 20-column row
	} {
		cur, row := cursorFor(tc.val, 20)
		if cur.X != tc.want || strW(row) > 20 {
			t.Errorf("%q: cursor X %d, want %d (row %q)", tc.val, cur.X, tc.want, row)
		}
	}
	// A long paste scrolls to its tail without rescanning the whole text.
	cur, row := cursorFor(strings.Repeat("x", 200000)+"END", 20)
	if cur.X != 19 || !strings.HasSuffix(row, "END") {
		t.Errorf("long paste: cursor %+v row %q", cur, row)
	}
	// Very narrow: nothing is drawn past the row and the cursor stays on it.
	for w := 8; w <= 10; w++ {
		cur, row := cursorFor("abc", w)
		if strW(row) > w || (cur.Shape != buffer.CursorHidden && cur.X >= w) {
			t.Errorf("w=%d: cursor %+v row %q", w, cur, row)
		}
	}
}

func TestFormActiveToggleHighlight(t *testing.T) {
	th := DefaultDarkTheme()
	st := newStyler(th)
	focusBg := st.tone("input.focus").Bg
	for _, active := range []int{1, 2} {
		_, f, _ := drawTheme(formNode(active, nil), withDensity(DensityCompact), "f", 30, 4)
		row := strings.Split(f.ANSI, "\n")[active]
		accentOn := st.sgr(mergeTone(mergeTone(st.tone("accent"), ToneStyle{Bold: active == 2}), ToneStyle{Bg: focusBg}))
		if !strings.Contains(row, accentOn[2:len(accentOn)-1]) {
			t.Errorf("active row %d: selected value must keep accent on input.focus: %q", active, row)
		}
		if active == 2 {
			mutedOn := st.sgr(mergeTone(st.tone("muted"), ToneStyle{Bg: focusBg}))
			if !strings.Contains(row, mutedOn[2:len(mutedOn)-1]) {
				t.Errorf("unselected option must stay muted on input.focus: %q", row)
			}
		}
		other := strings.Split(f.ANSI, "\n")[3-active]
		if strings.Contains(other, sgrBg(th, "input.focus")) {
			t.Errorf("inactive control must not be highlighted: %q", other)
		}
	}
}

// A bordered edit panel shorter than the balanced form (as in matomeru-next's
// edit view) keeps every control: fields fall back to compact rows, and the
// active field keeps its three-row block because the leftover rows allow it.
func TestShortPanelFormKeepsEveryControl(t *testing.T) {
	var fields []any
	names := []string{"title", "workStatus", "priority", "parent", "defaultDir", "due", "notes", "owner"}
	for _, name := range names {
		fields = append(fields, map[string]any{"name": name, "description": name + " help"})
	}
	form := ViewNode{Kind: KindForm, ID: "edit.form", Focusable: true, Props: map[string]any{
		"fields": fields, "values": map[string]any{"title": "Data layer"}, "active": 0, "errors": map[string]any{"due": "bad date"}}}
	panel := ViewNode{Kind: KindPanel, ID: "edit", Title: "Edit", Border: true, Children: []ViewNode{form}}
	rows, f, _ := draw(panel, "edit.form", 60, 16)
	plain := strings.Join(rows, "\n")
	for _, want := range append(names, "⏎ submit", "✗ bad date", "title help") {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q:\n%s", want, plain)
		}
	}
	// Border, blank breathing row, the active control and its note; then one
	// compact row per field (description as placeholder, error inline).
	if !strings.Contains(rows[2], "▸ title") || !strings.Contains(rows[3], "title help") || !strings.Contains(rows[4], "workStatus") || !strings.Contains(rows[5], "priority") {
		t.Errorf("active block or compact rows wrong:\n%s", plain)
	}
	if f.Cursor.Y != 2 || f.Cursor.X != 2+12+2+1+len("Data layer") { // border+pad, label column, value column, leading space
		t.Errorf("cursor = %+v", f.Cursor)
	}
	// Two rows shorter, the active block no longer fits: all compact, nothing lost.
	rows, f, _ = draw(panel, "edit.form", 60, 11)
	if plain := strings.Join(rows, "\n"); !strings.Contains(plain, "owner") || !strings.Contains(plain, "⏎ submit") || !strings.Contains(rows[1], "▸ title") || f.Cursor.Y != 1 {
		t.Errorf("compact fallback:\n%s\ncursor %+v", plain, f.Cursor)
	}
}
