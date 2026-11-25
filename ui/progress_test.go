package ui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProgressRenderFluidWidth(t *testing.T) {
	node := ViewNode{
		Kind: KindProgress,
		ID:   "p",
		Props: map[string]any{
			"label":  "Build",
			"done":   3,
			"active": 1,
			"total":  6,
			"suffix": "▶1",
		},
	}

	for _, w := range []int{40, 60, 100} {
		rows, _, solved := draw(node, "", w, 1)
		if r := solved.Rects["p"]; r.W != w || r.H != 1 {
			t.Fatalf("width %d: rect = %+v, want W=%d H=1", w, r, w)
		}
		row := rows[0]
		if got := strW(row); got != w {
			t.Errorf("width %d: rendered width = %d, want exact %d (%q)", w, got, w, row)
		}
		if strings.ContainsAny(row, "[]") {
			t.Errorf("width %d: progress bar must not have brackets: %q", w, row)
		}
		if !strings.HasPrefix(row, "Build  3/6  ") {
			t.Errorf("width %d: unexpected left section: %q", w, row)
		}
		if !strings.HasSuffix(row, "  50% ▶1") {
			t.Errorf("width %d: unexpected right section: %q", w, row)
		}
		if !strings.Contains(row, "█") || !strings.Contains(row, "▓") || !strings.Contains(row, "░") {
			t.Errorf("width %d: missing fill/active/track segments: %q", w, row)
		}
	}

	// Inside a bordered KindPanel, the bar uses the exact solved inner width (w - 4).
	panel := ViewNode{Kind: KindPanel, Title: "Status", Children: []ViewNode{node}}
	pRows, _, pSolved := draw(panel, "", 54, 4)
	if r := pSolved.Rects["p"]; r.W != 50 || r.H != 1 {
		t.Fatalf("panel child rect = %+v, want W=50 H=1", r)
	}
	if !strings.HasPrefix(pRows[1], "│ Build  3/6  ") || !strings.HasSuffix(pRows[1], "  50% ▶1 │") {
		t.Errorf("panel progress row = %q", pRows[1])
	}

	// Bar grows monotonically with container width (fluid expansion).
	r40, _, _ := draw(node, "", 40, 1)
	r80, _, _ := draw(node, "", 80, 1)
	barLen := func(s string) int {
		return strings.Count(s, "█") + strings.Count(s, "▓") + strings.Count(s, "░")
	}
	if barLen(r80[0])-barLen(r40[0]) != 40 {
		t.Errorf("bar did not expand by +40 cols: len40=%d len80=%d", barLen(r40[0]), barLen(r80[0]))
	}
}

func TestProgressSegmentsAndPercentMode(t *testing.T) {
	// value alias + no active + JSON roundtrip.
	raw := ViewNode{
		Kind: KindProgress,
		Props: map[string]any{
			"label": "Sync",
			"value": 2,
			"total": 4,
		},
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ViewNode
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	rows, _, _ := draw(decoded, "", 40, 1)
	if got := strW(rows[0]); got != 40 {
		t.Fatalf("roundtrip width = %d, want 40 (%q)", got, rows[0])
	}
	if !strings.HasPrefix(rows[0], "Sync  2/4  ") || !strings.HasSuffix(rows[0], "  50%") || strings.Contains(rows[0], "▓") {
		t.Errorf("unexpected roundtrip row: %q", rows[0])
	}

	// Percent mode (0..1 and 0..100) with total == 0 omits fraction by default.
	for _, tc := range []struct {
		name string
		pct  any
		want string
	}{
		{"unit-half", 0.5, "50%"},
		{"unit-full", 1.0, "100%"},
		{"hundred-scale", 75, "75%"},
		{"over-100", 140, "100%"},
		{"negative", -10, "0%"},
	} {
		n := ViewNode{Kind: KindProgress, Props: map[string]any{"percent": tc.pct}}
		r, _, _ := draw(n, "", 30, 1)
		if !strings.HasSuffix(r[0], "  "+tc.want) {
			t.Errorf("%s: got %q, want suffix %q", tc.name, r[0], tc.want)
		}
		if strings.Contains(r[0], "/") {
			t.Errorf("%s: total==0 should omit fraction: %q", tc.name, r[0])
		}
	}

	// Opt-out showFraction and showPercent + custom glyphs.
	custom := ViewNode{
		Kind: KindProgress,
		Props: map[string]any{
			"done":         1,
			"active":       1,
			"total":        4,
			"showFraction": false,
			"showPercent":  false,
			"fillChar":     "=",
			"activeChar":   ">",
			"trackChar":    "-",
		},
	}
	rCustom, _, _ := draw(custom, "", 20, 1)
	if want := "=====>>>>>----------"; rCustom[0] != want {
		t.Errorf("custom bar = %q, want %q", rCustom[0], want)
	}
}

func TestProgressThemeTokensAndOverrides(t *testing.T) {
	tokens := []string{"progress.fill", "progress.active", "progress.track"}
	for _, preset := range ThemePresets() {
		for _, tok := range tokens {
			if ts := preset.Tone(tok); ts.Fg == "" {
				t.Errorf("%s: tone %q did not resolve foreground", preset.Name, tok)
			}
		}
	}

	// Default 50% uses progress.fill (accent #82aaff -> 130;170;255), progress.active (warning #ffc777 -> 255;199;119),
	// and progress.track (border #4a5278 -> 74;82;120).
	mid := ViewNode{Kind: KindProgress, Props: map[string]any{"done": 2, "active": 1, "total": 4}}
	_, fMid, _ := draw(mid, "", 40, 1)
	for _, sgr := range []string{"38;2;130;170;255", "38;2;255;199;119", "38;2;74;82;120"} {
		if !strings.Contains(fMid.ANSI, sgr) {
			t.Errorf("mid progress missing SGR %q: %q", sgr, fMid.ANSI)
		}
	}

	// 100% done automatically switches fill to success (#c3e88d -> 195;232;141) unless fillTone is overridden.
	complete := ViewNode{Kind: KindProgress, Props: map[string]any{"done": 4, "total": 4}}
	_, fDone, _ := draw(complete, "", 40, 1)
	if !strings.Contains(fDone.ANSI, "38;2;195;232;141") {
		t.Errorf("100%% progress should default to success tone: %q", fDone.ANSI)
	}
	override := ViewNode{Kind: KindProgress, Props: map[string]any{"done": 4, "total": 4, "fillTone": "error"}}
	_, fOverride, _ := draw(override, "", 40, 1)
	if !strings.Contains(fOverride.ANSI, "38;2;255;117;127") {
		t.Errorf("explicit fillTone override should win at 100%%: %q", fOverride.ANSI)
	}

	// Custom theme tones override default progress tokens.
	customTh := DefaultDarkTheme()
	customTh.Tones["progress.fill"] = ToneStyle{Fg: "#112233"}
	customTh.Tones["progress.active"] = ToneStyle{Fg: "#445566"}
	customTh.Tones["progress.track"] = ToneStyle{Fg: "#778899"}
	fCustom, _ := Render(mid, customTh, "", 40, 1)
	for _, sgr := range []string{"38;2;17;34;51", "38;2;68;85;102", "38;2;119;136;153"} {
		if !strings.Contains(fCustom.ANSI, sgr) {
			t.Errorf("custom theme token missing SGR %q: %q", sgr, fCustom.ANSI)
		}
	}

	// Zero theme renders plain without ANSI sequences.
	fZero, _ := Render(mid, Theme{}, "", 30, 1)
	if strings.Contains(fZero.ANSI, "\x1b[") {
		t.Errorf("zero theme emitted ANSI: %q", fZero.ANSI)
	}
}

func TestProgressNarrowDegradationAndLayout(t *testing.T) {
	longLabel := "Very long descriptive progress label that must truncate"
	node := ViewNode{
		Kind: KindProgress,
		ID:   "p",
		Props: map[string]any{
			"label":  longLabel,
			"done":   3,
			"active": 1,
			"total":  6,
			"suffix": "▶1",
		},
	}

	// Wide screen caps label with ellipsis so bar stays prominent.
	rowsWide, _, _ := draw(node, "", 100, 1)
	if !strings.Contains(rowsWide[0], "…") {
		t.Errorf("long label should truncate with ellipsis: %q", rowsWide[0])
	}
	barWide := strings.Count(rowsWide[0], "█") + strings.Count(rowsWide[0], "▓") + strings.Count(rowsWide[0], "░")
	if barWide < 50 {
		t.Errorf("bar should remain prominent on 100-col viewport, got %d cols in %q", barWide, rowsWide[0])
	}

	// Narrow widths degrade gracefully without panicking or overflowing ir.W.
	for w := 0; w <= 40; w++ {
		rows, _, _ := draw(node, "", w, 1)
		if w == 0 {
			continue
		}
		if got := strW(rows[0]); got > w {
			t.Fatalf("width %d: overflowed to %d cols (%q)", w, got, rows[0])
		}
	}

	// Suffix drops before core counters on narrow viewports.
	rowsNarrow, _, _ := draw(node, "", 22, 1)
	if strings.Contains(rowsNarrow[0], "▶1") {
		t.Errorf("width 22 should drop suffix before crushing bar: %q", rowsNarrow[0])
	}
	if !strings.Contains(rowsNarrow[0], "50%") {
		t.Errorf("width 22 should keep percent: %q", rowsNarrow[0])
	}

	// Natural height is 1 inside a vstack (and 3 when bordered).
	stack := ViewNode{
		Kind: KindVStack,
		Children: []ViewNode{
			{Kind: KindProgress, ID: "p1", Props: map[string]any{"done": 1, "total": 2}},
			{Kind: KindProgress, ID: "p2", Border: true, Title: "Jobs", Props: map[string]any{"done": 2, "total": 2}},
		},
	}
	_, _, solved := draw(stack, "", 40, 10)
	if solved.Rects["p1"].H != 1 {
		t.Errorf("unbordered KindProgress height = %d, want 1", solved.Rects["p1"].H)
	}
	if solved.Rects["p2"].H != 3 {
		t.Errorf("bordered KindProgress height = %d, want 3", solved.Rects["p2"].H)
	}
}
