package ui

import (
	"strings"
	"testing"
)

func TestMarkdownGFMTableRenderingAndAlignment(t *testing.T) {
	src := strings.Join([]string{
		"Intro paragraph.",
		"",
		"| Flag | Formats | Link | Bytes |",
		"| :--- | :---: | :--- | ---: |",
		"| `--format` | `json|jsonl|jsonc` | [spec](https://example.com/spec) | 1280 |",
		"| **strict** | `json` | [docs](https://example.com/docs) | 64 |",
		"",
		"Outro paragraph.",
	}, "\n")

	th := DefaultDarkTheme()
	mdNode := ViewNode{
		ID:        "md",
		Kind:      KindMarkdown,
		Focusable: true,
		Props:     map[string]any{"markdown": src, "activeLink": 1},
	}

	rows, f, _ := drawTheme(mdNode, th, "md", 70, 12)
	joined := strings.Join(rows, "\n")

	// No raw markdown syntax leaks (no raw backticks, no raw **, no raw [label](url) pipes).
	if strings.Contains(joined, "`") || strings.Contains(joined, "**") || strings.Contains(joined, "https://") {
		t.Fatalf("raw markdown syntax leaked into rendered table:\n%s", joined)
	}

	// Table renders as aligned columns with crisp header rule (─ and ┼) rather than a pipe wall.
	wantLines := []string{
		"Intro paragraph.",
		"",
		"Flag     │     Formats      │ Link │ Bytes",
		"─────────┼──────────────────┼──────┼──────",
		"--format │ json|jsonl|jsonc │ spec │  1280",
		"strict   │       json       │ docs │    64",
		"",
		"Outro paragraph.",
	}
	wantRows(t, rows, wantLines...)

	// Active link index 1 ("docs" in row 1) matches MarkdownLinks and carries selected bg.
	links := MarkdownLinks(src)
	if len(links) != 2 || links[0].Label != "spec" || links[1].Label != "docs" {
		t.Fatalf("MarkdownLinks = %+v", links)
	}
	selBg := sgrBg(th, "selected")
	ansiRows := strings.Split(f.ANSI, "\n")
	if strings.Contains(ansiRows[4], selBg) {
		t.Errorf("row 0 link should not carry selected bg: %q", ansiRows[4])
	}
	if !strings.Contains(ansiRows[5], selBg) {
		t.Errorf("row 1 active link should carry selected bg %q: %q", selBg, ansiRows[5])
	}
}

func TestMarkdownGFMTableEscapedPipeAndNarrowViewport(t *testing.T) {
	src := strings.Join([]string{
		"Col A | Description | Count",
		"--- | --- | ---:",
		`a \| b | A fairly long description cell that must wrap cleanly inside narrow viewports | 42`,
	}, "\n")

	// Narrow width wraps long column without overflowing width w or panicking.
	for _, w := range []int{36, 20, 10, 4, 1} {
		rows, _, _ := draw(ViewNode{
			Kind:  KindMarkdown,
			Props: map[string]any{"markdown": src},
		}, "", w, 12)
		for i, r := range rows {
			if gotW := strW(r); gotW > w {
				t.Errorf("w=%d row %d overflowed width (%d > %d): %q", w, i, gotW, w, r)
			}
		}
	}

	rows36, _, _ := draw(ViewNode{
		Kind:  KindMarkdown,
		Props: map[string]any{"markdown": src},
	}, "", 36, 12)
	joined := strings.Join(rows36, "\n")
	if !strings.Contains(joined, "a | b") {
		t.Fatalf("escaped pipe should render as literal '|':\n%s", joined)
	}
	if !strings.Contains(joined, "42") || !strings.Contains(joined, "narrow") {
		t.Fatalf("wrapped narrow table lost content:\n%s", joined)
	}
}

func TestMarkdownNestedInlineInHeadingsBoldAndLists(t *testing.T) {
	src := strings.Join([]string{
		"# Deterministic AI Session Logs + `o-json` Declarative Modes",
		"",
		"## Heading with [link](https://example.com/h) and `flag`",
		"",
		"- **Explicit Format Support (`--format json|jsonl|jsonc`)**",
		"- **Read the [spec](https://example.com/spec) and *emphasized `code`* now**",
		"- Plain snake_case_ident stays intact",
	}, "\n")

	th := DefaultDarkTheme()
	mdNode := ViewNode{
		ID:        "md",
		Kind:      KindMarkdown,
		Focusable: true,
		Props:     map[string]any{"markdown": src, "activeLink": 1},
	}
	rows, f, _ := drawTheme(mdNode, th, "md", 80, 12)
	joined := strings.Join(rows, "\n")

	if strings.Contains(joined, "`") {
		t.Fatalf("raw backticks leaked into rendered markdown:\n%s", joined)
	}
	if strings.Contains(joined, "**") || strings.Contains(joined, "*emphasized") {
		t.Fatalf("raw emphasis markers leaked into rendered markdown:\n%s", joined)
	}
	for _, want := range []string{
		"Deterministic AI Session Logs + o-json Declarative Modes",
		"Heading with link and flag",
		"• Explicit Format Support (--format json|jsonl|jsonc)",
		"• Read the spec and emphasized code now",
		"• Plain snake_case_ident stays intact",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("rendered markdown missing %q:\n%s", want, joined)
		}
	}

	// Verify MarkdownLinks parity: link 0 is in heading, link 1 is inside **bold**.
	links := MarkdownLinks(src)
	if len(links) != 2 || links[0].Label != "link" || links[1].Label != "spec" {
		t.Fatalf("MarkdownLinks = %+v", links)
	}
	// Active link 1 ("spec") inside bold gets selected bg; heading link 0 does not.
	selBg := sgrBg(th, "selected")
	ansiRows := strings.Split(f.ANSI, "\n")
	headingRow := -1
	specRow := -1
	for i, r := range rows {
		if strings.Contains(r, "Heading with link") {
			headingRow = i
		}
		if strings.Contains(r, "Read the spec") {
			specRow = i
		}
	}
	if headingRow < 0 || specRow < 0 {
		t.Fatalf("could not locate heading/spec rows in:\n%s", joined)
	}
	if strings.Contains(ansiRows[headingRow], selBg) {
		t.Errorf("inactive heading link should not carry selected bg: %q", ansiRows[headingRow])
	}
	if !strings.Contains(ansiRows[specRow], selBg) {
		t.Errorf("active link inside bold should carry selected bg %q: %q", selBg, ansiRows[specRow])
	}

	// Inline code inside heading and bold carries md.code fg (#c099ff -> 38;2;192;153;255).
	if !strings.Contains(ansiRows[0], "38;2;192;153;255") {
		t.Errorf("heading inline code missing md.code fg: %q", ansiRows[0])
	}
}
