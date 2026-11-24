package ui

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/layout"
)

func TestFocusChromeTonesAndRoleAliases(t *testing.T) {
	tokens := []string{
		"focus.border", "focus.bg", "focus.fg", "focus.title",
		"unfocused.border", "unfocused.fg",
	}
	for _, p := range ThemePresets() {
		for _, tok := range tokens {
			ts := p.Tone(tok)
			if ts.Fg == "" && ts.Bg == "" {
				t.Errorf("%s: tone %q did not resolve", p.Name, tok)
			}
		}
		if p.Tone("focus.bg").Bg == "" {
			t.Errorf("%s: focus.bg must resolve a background color", p.Name)
		}
		if !p.Tone("focus.border").Bold || !p.Tone("focus.title").Bold {
			t.Errorf("%s: focus.border and focus.title must be bold", p.Name)
		}
	}

	// Custom camelCase role overrides in themes.json flow cleanly into the semantic tones.
	custom := DefaultDarkTheme()
	custom.Roles["focusBorder"] = "#00ffcc"
	custom.Roles["focusBg"] = "#112233"
	custom.Roles["focusFg"] = "#eeeeee"
	custom.Roles["focusTitleFg"] = "#ffaa00"
	custom.Roles["unfocusedBorder"] = "#334455"
	custom.Roles["unfocusedFg"] = "#667788"

	checks := []struct {
		tone   string
		wantFg string
		wantBg string
		bold   bool
		dim    bool
	}{
		{"focus.border", "#00ffcc", "", true, false},
		{"focus.bg", "", "#112233", false, false},
		{"focus.fg", "#eeeeee", "", true, false},
		{"focus.title", "#ffaa00", "", true, false},
		{"unfocused.border", "#334455", "", false, true},
		{"unfocused.fg", "#667788", "", false, false},
	}
	for _, tc := range checks {
		got := custom.Tone(tc.tone)
		if got.Fg != tc.wantFg || got.Bg != tc.wantBg || got.Bold != tc.bold || got.Dim != tc.dim {
			t.Errorf("custom %s = %+v, want fg=%q bg=%q bold=%v dim=%v", tc.tone, got, tc.wantFg, tc.wantBg, tc.bold, tc.dim)
		}
	}

	// Explicit Tones override takes precedence over role alias.
	custom.Tones["focus.border"] = ToneStyle{Fg: "#ff0055", Bold: true}
	if got := custom.Tone("focus.border"); got.Fg != "#ff0055" {
		t.Errorf("explicit tone override lost to role alias: %+v", got)
	}

	// Zero Theme stays unstyled and defaults to CardBorderAuto.
	var zero Theme
	if zero.EffectiveCardBorderStyle() != CardBorderAuto {
		t.Errorf("zero EffectiveCardBorderStyle = %q, want auto", zero.EffectiveCardBorderStyle())
	}
	if zero.Tone("focus.border") != (ToneStyle{}) || zero.Tone("unfocused.border") != (ToneStyle{}) {
		t.Errorf("zero Theme must not emit styles")
	}
	if (Theme{CardBorderStyle: "unknown"}).EffectiveCardBorderStyle() != CardBorderAuto {
		t.Errorf("unknown CardBorderStyle must fall back to auto")
	}
}

func TestPanelFocusContrast(t *testing.T) {
	th := DefaultDarkTheme()
	th.Roles["focusBorder"] = "#00ffcc"
	th.Roles["focusTitleFg"] = "#ffaa00"
	th.Roles["unfocusedBorder"] = "#334455"
	th.Roles["unfocusedFg"] = "#667788"

	root := ViewNode{
		Kind: KindHStack,
		Children: []ViewNode{
			{Kind: KindPanel, ID: "p1", Title: "Left", Children: []ViewNode{{Kind: KindText, ID: "t1", Focusable: true, Props: map[string]any{"text": "a"}}}},
			{Kind: KindPanel, ID: "p2", Title: "Right", Children: []ViewNode{{Kind: KindText, ID: "t2", Focusable: true, Props: map[string]any{"text": "b"}}}},
		},
	}

	rows, f, _ := drawTheme(root, th, "t1", 32, 4)
	if !strings.Contains(rows[0], "┏━ Left ") || !strings.Contains(rows[0], "╭─ Right ") {
		t.Fatalf("panel focus glyphs mismatch:\n%s", strings.Join(rows, "\n"))
	}
	// Focus border (#00ffcc -> 0;255;204), focus title (#ffaa00 -> 255;170;0),
	// unfocused border (#334455 -> 51;68;85), unfocused fg (#667788 -> 102;119;136).
	for _, want := range []string{"38;2;0;255;204", "38;2;255;170;0", "38;2;51;68;85", "38;2;102;119;136"} {
		if !strings.Contains(f.ANSI, want) {
			t.Errorf("panel frame missing expected SGR %q:\n%q", want, f.ANSI)
		}
	}
}

func TestCardBorderSubtleEliminatesNestedBoxes(t *testing.T) {
	th := DefaultDarkTheme()
	th.CardBorderStyle = CardBorderSubtle

	root := ViewNode{
		Kind:  KindPanel,
		ID:    "feed",
		Title: "Feed",
		Children: []ViewNode{
			{Kind: KindCard, ID: "c1", Focusable: true, Title: "Active Card", Props: map[string]any{"summary": "focused detail"}},
			{Kind: KindCard, ID: "c2", Focusable: true, Title: "Quiet Card", Props: map[string]any{"summary": "idle detail"}},
		},
	}

	rows, f, s := drawTheme(root, th, "c1", 36, 12)
	c1r := s.Rects["c1"]
	c2r := s.Rects["c2"]
	if c1r.H < 4 || c2r.H < 4 {
		t.Fatalf("unexpected card rects: c1=%+v c2=%+v", c1r, c2r)
	}

	// Inspect only the card interiors (inside the outer panel frame).
	sliceRect := func(r []string, y0, h, x0, w int) []string {
		out := make([]string, 0, h)
		for y := y0; y < y0+h && y < len(r); y++ {
			rs := []rune(r[y])
			if x0 < len(rs) {
				out = append(out, string(rs[x0:min(len(rs), x0+w)]))
			}
		}
		return out
	}

	c1Rows := sliceRect(rows, c1r.Y, c1r.H, c1r.X, c1r.W)
	c2Rows := sliceRect(rows, c2r.Y, c2r.H, c2r.X, c2r.W)
	joinedCards := strings.Join(append(c1Rows, c2Rows...), "\n")
	if strings.ContainsAny(joinedCards, "╭╮╰╯┏┓┗┛┌┐└┘") {
		t.Errorf("subtle card border must eliminate 4-sided box corners:\n%s", joinedCards)
	}
	if !strings.Contains(c1Rows[1], "┃") || !strings.Contains(c1Rows[1], "▸ Active Card") {
		t.Errorf("focused subtle card missing ┃ rail or ▸ marker:\n%s", strings.Join(c1Rows, "\n"))
	}
	if !strings.Contains(c2Rows[1], "│") || strings.Contains(c2Rows[1], "▸") || !strings.Contains(c2Rows[1], "Quiet Card") {
		t.Errorf("unfocused subtle card missing quiet │ rail or leaked focus marker:\n%s", strings.Join(c2Rows, "\n"))
	}

	// Focused card row carries focus.bg ANSI tint (#2d3352 -> 48;2;45;51;82), unfocused card does not.
	ansiRows := strings.Split(f.ANSI, "\n")
	focusBg := sgrBg(th, "focus.bg")
	if !strings.Contains(ansiRows[c1r.Y+1], focusBg) {
		t.Errorf("focused card title row missing focus.bg %q: %q", focusBg, ansiRows[c1r.Y+1])
	}
	if !strings.Contains(ansiRows[c1r.Y+2], focusBg) {
		t.Errorf("focused card summary row missing focus.bg %q: %q", focusBg, ansiRows[c1r.Y+2])
	}
	if strings.Contains(ansiRows[c2r.Y+1], focusBg) {
		t.Errorf("unfocused card must not carry focus.bg: %q", ansiRows[c2r.Y+1])
	}
}

func TestCardBorderModesAndCacheInvalidation(t *testing.T) {
	card := ViewNode{
		Kind:      KindCard,
		ID:        "c",
		Focusable: true,
		CacheKey:  42,
		Title:     "Item",
		Props:     map[string]any{"summary": "body"},
	}

	// CardBorderNone: zero border glyphs, focus.bg tint + ▸ marker on focus.
	noneTh := DefaultDarkTheme()
	noneTh.CardBorderStyle = CardBorderNone
	rowsNone, fNone, sNone := drawTheme(card, noneTh, "c", 24, 5)
	if strings.ContainsAny(strings.Join(rowsNone, ""), "─│╭╮╰╯┏┓┗┛┃") {
		t.Errorf("CardBorderNone must draw no border glyphs:\n%s", strings.Join(rowsNone, "\n"))
	}
	if !strings.Contains(rowsNone[1], "▸ Item") || !strings.Contains(fNone.ANSI, sgrBg(noneTh, "focus.bg")) {
		t.Errorf("CardBorderNone focused card missing ▸ marker or focus.bg:\n%s", strings.Join(rowsNone, "\n"))
	}

	// CardBorderMinimal: top/bottom hairlines + ┃ on focus.
	minTh := DefaultDarkTheme()
	minTh.CardBorderStyle = CardBorderMinimal
	rowsMin, _, sMin := drawTheme(card, minTh, "c", 24, 5)
	if !strings.HasPrefix(rowsMin[0], "──") || !strings.HasPrefix(rowsMin[1], "┃") || !strings.HasPrefix(rowsMin[4], "──") {
		t.Errorf("CardBorderMinimal focused card mismatch:\n%s", strings.Join(rowsMin, "\n"))
	}

	// CardBorderAuto (default): matches Theme.BorderStyle (rounded heavy frame on focus).
	autoTh := DefaultDarkTheme()
	rowsAuto, _, sAuto := drawTheme(card, autoTh, "c", 24, 5)
	if !strings.HasPrefix(rowsAuto[0], "┏━") || !strings.HasPrefix(rowsAuto[4], "┗━") {
		t.Errorf("CardBorderAuto focused card mismatch:\n%s", strings.Join(rowsAuto, "\n"))
	}

	// Layout geometry is identical across all card border styles.
	if sNone.InnerRects["c"] != sAuto.InnerRects["c"] || sMin.InnerRects["c"] != sAuto.InnerRects["c"] {
		t.Errorf("card border style must not alter layout geometry: auto=%+v min=%+v none=%+v",
			sAuto.InnerRects["c"], sMin.InnerRects["c"], sNone.InnerRects["c"])
	}

	// Per-node props["cardBorder"] overrides theme CardBorderStyle.
	perNode := card
	perNode.Props = map[string]any{"summary": "body", "cardBorder": CardBorderSubtle}
	rowsOverride, _, _ := drawTheme(perNode, autoTh, "c", 24, 5)
	if strings.ContainsAny(strings.Join(rowsOverride, ""), "┏┓┗┛╭╮╰╯") || !strings.HasPrefix(rowsOverride[1], "┃") {
		t.Errorf("per-node cardBorder override failed:\n%s", strings.Join(rowsOverride, "\n"))
	}

	// Renderer subtree cache invalidates when Theme.CardBorderStyle changes.
	r := NewRenderer(0)
	f1, _ := r.Render(cachedTree(9), autoTh, "c1", 30, 12)
	if r.Stats.CacheMisses != 1 {
		t.Fatalf("first render should miss: %+v", r.Stats)
	}
	r.Render(cachedTree(9), autoTh, "c1", 30, 12)
	if r.Stats.CacheHits != 1 {
		t.Fatalf("repeat render should hit: %+v", r.Stats)
	}
	subTh := autoTh
	subTh.CardBorderStyle = CardBorderSubtle
	f2, _ := r.Render(cachedTree(9), subTh, "c1", 30, 12)
	if r.Stats.CacheMisses != 1 || f1.ANSI == f2.ANSI {
		t.Fatalf("CardBorderStyle change must invalidate subtree cache: stats=%+v", r.Stats)
	}
	r.Render(cachedTree(9), subTh, "c1", 30, 12)
	if r.Stats.CacheHits != 1 {
		t.Fatalf("repeat subtle render should hit: %+v", r.Stats)
	}
}

func TestFocusableTextRendersSelectionHighlightAndFocusedText(t *testing.T) {
	th := DefaultDarkTheme()
	root := ViewNode{
		Kind:  KindPanel,
		ID:    "toc",
		Title: "Pages",
		Children: []ViewNode{
			{
				Kind: KindScroll,
				ID:   "toc.scroll",
				Children: []ViewNode{
					{
						Kind: KindVStack,
						Children: []ViewNode{
							{
								ID:        "row.1",
								Kind:      KindText,
								Focusable: true,
								Tone:      "accent",
								Box:       layout.Constraints{FixedH: 1},
								Props:     map[string]any{"text": "◆ ▣ Screen", "focusedText": "▸ ▣ Screen"},
							},
							{
								ID:        "row.2",
								Kind:      KindText,
								Focusable: true,
								Tone:      "warning",
								Box:       layout.Constraints{FixedH: 1},
								Props:     map[string]any{"text": "● ▪ ALDI issues", "focusedText": "▸ ▪ ALDI issues"},
							},
							{
								ID:        "row.3",
								Kind:      KindText,
								Focusable: false,
								Tone:      "muted",
								Box:       layout.Constraints{FixedH: 1},
								Props:     map[string]any{"text": "static label", "focusedText": "should not show"},
							},
						},
					},
				},
			},
		},
	}

	rows, f, s := drawTheme(root, th, "row.2", 34, 8)
	r1 := s.Rects["row.1"]
	r2 := s.Rects["row.2"]
	if r1.H != 1 || r2.H != 1 {
		t.Fatalf("unexpected text row rects: r1=%+v r2=%+v", r1, r2)
	}
	if !strings.Contains(rows[r1.Y], "◆ ▣ Screen") || strings.Contains(rows[r1.Y], "▸ ▣ Screen") {
		t.Errorf("unfocused row.1 should show normal text, got %q", rows[r1.Y])
	}
	if !strings.Contains(rows[r2.Y], "▸ ▪ ALDI issues") {
		t.Errorf("focused row.2 should show focusedText, got %q", rows[r2.Y])
	}

	ansiRows := strings.Split(f.ANSI, "\n")
	selBg := sgrBg(th, "selected")
	if selBg == "" {
		t.Fatal("DefaultDarkTheme selected tone must resolve a background")
	}
	if !strings.Contains(ansiRows[r2.Y], selBg) {
		t.Errorf("focused KindText row.2 missing selected background %q: %q", selBg, ansiRows[r2.Y])
	}
	if strings.Contains(ansiRows[r1.Y], selBg) {
		t.Errorf("unfocused KindText row.1 must not carry selected background: %q", ansiRows[r1.Y])
	}

	// Non-focusable KindText ignores focus even if its ID matches focusID.
	rows3, f3, s3 := drawTheme(root, th, "row.3", 34, 8)
	r3 := s3.Rects["row.3"]
	if strings.Contains(rows3[r3.Y], "should not show") || strings.Contains(strings.Split(f3.ANSI, "\n")[r3.Y], selBg) {
		t.Errorf("non-focusable KindText must not highlight or swap focusedText: %q", rows3[r3.Y])
	}

	// Zero Theme stays unstyled and still swaps focusedText cleanly.
	rowsZero, fZero, sZero := drawTheme(root, Theme{}, "row.2", 34, 8)
	if !strings.Contains(rowsZero[sZero.Rects["row.2"].Y], "▸ ▪ ALDI issues") {
		t.Errorf("zero theme focusedText missing: %q", rowsZero[sZero.Rects["row.2"].Y])
	}
	if strings.Contains(fZero.ANSI, "\x1b[") {
		t.Errorf("zero theme must not emit ANSI escapes: %q", fZero.ANSI)
	}
}
