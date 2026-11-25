package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
)

func sgrFg(th Theme, tone string) string {
	st := newStyler(th)
	s := st.sgr(ToneStyle{Fg: st.tone(tone).Fg})
	s = strings.TrimPrefix(s, "\x1b[")
	return strings.TrimSuffix(s, "m")
}

func TestCompileAndDecodeInlineRuns(t *testing.T) {
	ref := &core.Ref{Kind: "task", ID: "t-7"}
	runs := []InlineRun{
		{Text: "See "},
		{Kind: "link", Label: "PR #42", Href: "https://example.com/pr/42"},
		{Text: " · "},
		{Kind: "link", Label: "Task 7", Ref: ref, Command: "task.show", Args: map[string]any{"id": "t-7"}},
		{Text: " "},
		{Kind: "badge", Label: "P1", Tone: "badge.warning"},
	}

	plain, spans, links := CompileInlineRuns(runs, 1)
	wantPlain := "See PR #42 · Task 7 P1"
	if plain != wantPlain {
		t.Fatalf("plain = %q, want %q", plain, wantPlain)
	}
	if len(links) != 2 {
		t.Fatalf("links count = %d, want 2: %+v", len(links), links)
	}
	// Check exact rune offsets (note '·' is 1 rune, 2 UTF-8 bytes).
	rs := []rune(plain)
	if got := string(rs[links[0].From:links[0].To]); got != "PR #42" || links[0].Index != 0 || links[0].Href != "https://example.com/pr/42" {
		t.Errorf("link[0] = %+v, slice=%q", links[0], got)
	}
	if got := string(rs[links[1].From:links[1].To]); got != "Task 7" || links[1].Index != 1 || links[1].Ref == nil || *links[1].Ref != *ref || links[1].Command != "task.show" || links[1].Args["id"] != "t-7" {
		t.Errorf("link[1] = %+v, slice=%q", links[1], got)
	}

	// Verify activeLink=1 gave link 0 "link" and link 1 "link.focused".
	if len(spans) != 3 {
		t.Fatalf("spans count = %d, want 3: %+v", len(spans), spans)
	}
	if spans[0].Tone != "link" || spans[1].Tone != "link.focused" || spans[2].Tone != "badge.warning" {
		t.Errorf("unexpected span tones: %+v", spans)
	}

	// Mixed JSON array round-trip decoding.
	rawJSON := `["See ", {"kind":"link","label":"PR #42","href":"https://example.com/pr/42"}, " and ", {"kind":"link","label":"Doc","ref":{"kind":"document","id":"d-9"}}]`
	var mixed any
	if err := json.Unmarshal([]byte(rawJSON), &mixed); err != nil {
		t.Fatal(err)
	}
	decoded, ok := DecodeInlineRuns(mixed)
	if !ok || len(decoded) != 4 {
		t.Fatalf("DecodeInlineRuns(mixed) = (%+v, %v), want 4 runs", decoded, ok)
	}
	p2, _, l2 := CompileInlineRuns(decoded, -1)
	if p2 != "See PR #42 and Doc" || len(l2) != 2 || l2[1].Ref == nil || l2[1].Ref.Kind != "document" || l2[1].Ref.ID != "d-9" {
		t.Fatalf("compiled mixed JSON = (%q, %+v)", p2, l2)
	}

	// Plain strings and non-run maps must NOT match DecodeInlineRuns.
	if _, ok := DecodeInlineRuns("plain text"); ok {
		t.Error("plain string must not decode as inline runs")
	}
	if _, ok := DecodeInlineRuns(map[string]any{"id": "1", "name": "foo"}); ok {
		t.Error("arbitrary map must not decode as inline runs")
	}
	// Single link map decodes cleanly.
	if single, ok := DecodeInlineRuns(map[string]any{"kind": "link", "label": "Open", "href": "https://x.test"}); !ok || len(single) != 1 || single[0].Label != "Open" {
		t.Errorf("single link map = (%+v, %v)", single, ok)
	}
}

func TestKindTextInlineRunsAndActiveLink(t *testing.T) {
	th := DefaultDarkTheme()
	linkFg := sgrFg(th, "link")
	focusedBg := sgrBg(th, "link.focused")

	node := ViewNode{
		ID:        "txt",
		Kind:      KindText,
		Focusable: true,
		Props: map[string]any{
			"runs": []any{
				"Review ",
				map[string]any{"kind": "link", "label": "PR #42", "href": "https://example.com/42"},
				" or ",
				map[string]any{"kind": "link", "label": "Spec", "ref": map[string]any{"kind": "document", "id": "d1"}},
			},
			"activeLink": 1,
		},
	}

	// Wrap inside a VStack to confirm natural height measurement uses compiled runs.
	root := ViewNode{
		Kind:     KindVStack,
		Children: []ViewNode{node, {Kind: KindText, Props: map[string]any{"text": "Footer"}}},
	}
	rows, f, _ := drawTheme(root, th, "", 16, 6)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "PR #42") || !strings.Contains(joined, "Spec") || !strings.Contains(joined, "Footer") {
		t.Fatalf("KindText runs missing expected text/wrap height:\n%s", joined)
	}
	if !strings.Contains(f.ANSI, linkFg) {
		t.Errorf("KindText missing link tone fg %q", linkFg)
	}
	if !strings.Contains(f.ANSI, focusedBg) {
		t.Errorf("KindText missing activeLink link.focused bg %q", focusedBg)
	}

	links := NodeLinks(&node, -1)
	if len(links) != 2 || links[0].Label != "PR #42" || links[1].Label != "Spec" || links[1].Ref == nil || links[1].Ref.ID != "d1" {
		t.Fatalf("NodeLinks(KindText) = %+v", links)
	}
}

func TestKindTableInlineLinksAndCellSpans(t *testing.T) {
	th := DefaultDarkTheme()
	linkFg := sgrFg(th, "link")
	focusedBg := sgrBg(th, "link.focused")
	warnFg := sgrFg(th, "warning")

	row0 := TableRowSpec{
		Cells: []any{
			[]InlineRun{
				{Text: "Fix "},
				{Kind: "link", Label: "PR #1", Href: "https://example.com/1"},
			},
			[]any{
				map[string]any{"kind": "link", "label": "Spec", "href": "https://example.com/spec"},
				" · ",
				map[string]any{"kind": "link", "label": "Logs", "command": "logs.open"},
			},
		},
	}
	row1 := TableRowSpec{
		Cells:     []any{"Dissolving task", "ok"},
		CellIcons: []string{"●", ""},
		CellSpans: [][]ToneSpan{
			{{From: 0, To: 10, Tone: "row.dim"}, {From: 10, To: 15, Tone: "warning"}},
			nil,
		},
	}

	tbl := ViewNode{
		ID:        "tbl",
		Kind:      KindTable,
		Focusable: true,
		Props: map[string]any{
			"columns": []any{
				map[string]any{"key": "title", "title": "Title", "width": 20},
				map[string]any{"key": "refs", "title": "Refs", "width": 16},
			},
			"rows":       []any{row0, row1},
			"selected":   0,
			"activeLink": 2, // 3rd link across row 0 -> "Logs" in column 1
		},
	}

	rows, f, _ := drawTheme(tbl, th, "tbl", 42, 6)
	if !strings.Contains(rows[2], "Fix PR #1") || !strings.Contains(rows[2], "Spec · Logs") {
		t.Fatalf("table row 0 mismatch: %q", rows[2])
	}
	if !strings.Contains(rows[3], "● Dissolving task") {
		t.Fatalf("table row 1 with icon+spans mismatch: %q", rows[3])
	}

	ansiRows := strings.Split(f.ANSI, "\n")
	if !strings.Contains(ansiRows[2], linkFg) {
		t.Errorf("row 0 missing link fg %q: %q", linkFg, ansiRows[2])
	}
	if !strings.Contains(ansiRows[2], focusedBg) {
		t.Errorf("row 0 missing activeLink=2 focused bg %q: %q", focusedBg, ansiRows[2])
	}
	if strings.Contains(ansiRows[3], focusedBg) {
		t.Errorf("unselected row 1 must not carry link.focused bg: %q", ansiRows[3])
	}
	if !strings.Contains(ansiRows[3], warnFg) {
		t.Errorf("row 1 missing explicit CellSpans warning fg %q: %q", warnFg, ansiRows[3])
	}

	// TableRowLinks & NodeLinks extract all 3 links in column order without regex.
	rl := TableRowLinks(row0)
	nl := NodeLinks(&tbl, 0)
	nlDefault := NodeLinks(&tbl, -1) // defaults to selected=0
	if len(rl) != 3 || len(nl) != 3 || len(nlDefault) != 3 {
		t.Fatalf("links count mismatch: row=%d node=%d default=%d", len(rl), len(nl), len(nlDefault))
	}
	if nl[0].Index != 0 || nl[0].Label != "PR #1" || nl[1].Index != 1 || nl[1].Label != "Spec" || nl[2].Index != 2 || nl[2].Label != "Logs" || nl[2].Command != "logs.open" {
		t.Errorf("NodeLinks(KindTable, 0) = %+v", nl)
	}
	if got := NodeLinks(&tbl, 1); len(got) != 0 {
		t.Errorf("row 1 should have 0 links, got %+v", got)
	}
	if got := NodeLinks(&tbl, 99); got != nil {
		t.Errorf("out-of-bounds rowIdx should return nil, got %+v", got)
	}
}

func TestKindTreeLabelRunsAndLabelSpans(t *testing.T) {
	th := DefaultDarkTheme()
	linkFg := sgrFg(th, "link")
	focusedBg := sgrBg(th, "link.focused")
	warnFg := sgrFg(th, "warning")

	nodes := []TreeNode{
		{
			Icon: "●",
			LabelRuns: []InlineRun{
				{Text: "Ship "},
				{Kind: "link", Label: "PR #99", Href: "https://example.com/99"},
				{Text: " and "},
				{Kind: "link", Label: "Notes", Ref: &core.Ref{Kind: "document", ID: "n-1"}},
			},
			Meta: []TreeMeta{{Text: "P1", Tone: "muted"}},
		},
		{
			Label:      "Archiving old item cleanly",
			LabelSpans: []ToneSpan{{From: 0, To: 5, Tone: "row.dim"}, {From: 5, To: 26, Tone: "warning"}},
			Badges:     []Badge{{Text: "done", Tone: "success"}},
			Meta:       []TreeMeta{{Text: "2m", Tone: "muted"}},
		},
	}

	treeNode := ViewNode{
		ID:        "tr",
		Kind:      KindTree,
		Focusable: true,
		Props: map[string]any{
			"nodes":       nodes,
			"gapTopLevel": true,
			"wrap":        true,
			"selected":    0,
			"activeLink":  1,
		},
	}

	rows, f, _ := drawTheme(treeNode, th, "tr", 24, 8)
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"●", "Ship", "PR #99", "Notes", "P1", "Archiving", "done", "2m"} {
		if !strings.Contains(joined, want) {
			t.Errorf("wrapped tree missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(f.ANSI, linkFg) {
		t.Errorf("tree missing link fg %q", linkFg)
	}
	if !strings.Contains(f.ANSI, focusedBg) {
		t.Errorf("tree missing activeLink focused bg %q", focusedBg)
	}
	if !strings.Contains(f.ANSI, warnFg) {
		t.Errorf("tree missing LabelSpans warning fg %q", warnFg)
	}

	// Logical rowIdx ignores spacer rows added by gapTopLevel:true.
	links0 := NodeLinks(&treeNode, 0)
	if len(links0) != 2 || links0[0].Label != "PR #99" || links0[1].Label != "Notes" || links0[1].Ref == nil || links0[1].Ref.ID != "n-1" {
		t.Fatalf("NodeLinks(KindTree, 0) = %+v", links0)
	}
	if got := NodeLinks(&treeNode, -1); len(got) != 2 {
		t.Fatalf("NodeLinks(KindTree, -1) default selected = %+v", got)
	}
	if got := NodeLinks(&treeNode, 1); len(got) != 0 {
		t.Fatalf("NodeLinks(KindTree, 1) = %+v, want empty", got)
	}

	// Single-line truncation with LabelSpans + Meta preserves ellipsis and meta.
	treeNode.Props["wrap"] = false
	treeNode.Props["selected"] = 1
	shortRows, fShort, _ := drawTheme(treeNode, th, "tr", 24, 6)
	if !strings.Contains(strings.Join(shortRows, "\n"), "…") || !strings.Contains(fShort.ANSI, warnFg) {
		t.Errorf("truncated tree row with LabelSpans failed:\nrows:\n%s", strings.Join(shortRows, "\n"))
	}
}
