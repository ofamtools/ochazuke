package ui

import (
	"strings"
	"testing"
)

func TestMarkdownLinksAndCheckboxesParsingAndToggle(t *testing.T) {
	src := strings.Join([]string{
		"# Checklist",
		"",
		"- [ ] First task with [source](https://example.com/src) and [edit](https://example.com/edit)",
		"- [x] Done task",
		"```md",
		"- [ ] Ignored in fence [fake](https://ignored.test)",
		"```",
		"- [ ] Third task `code [not-a-link](https://code.test)` and [live](https://example.com/live)",
	}, "\n")

	links := MarkdownLinks(src)
	if len(links) != 3 {
		t.Fatalf("MarkdownLinks count = %d, want 3: %+v", len(links), links)
	}
	if links[0].Label != "source" || links[0].Href != "https://example.com/src" || links[0].Line != 2 {
		t.Errorf("link[0] = %+v", links[0])
	}
	if links[1].Label != "edit" || links[1].Href != "https://example.com/edit" || links[1].Line != 2 {
		t.Errorf("link[1] = %+v", links[1])
	}
	if links[2].Label != "live" || links[2].Href != "https://example.com/live" || links[2].Line != 7 {
		t.Errorf("link[2] = %+v", links[2])
	}

	checks := MarkdownCheckboxes(src)
	if len(checks) != 3 {
		t.Fatalf("MarkdownCheckboxes count = %d, want 3: %+v", len(checks), checks)
	}
	if checks[0].Checked || !checks[1].Checked || checks[2].Checked {
		t.Errorf("unexpected initial checkbox states: %+v", checks)
	}

	updated, checked, ok := ToggleMarkdownCheckbox(src, 0)
	if !ok || !checked {
		t.Fatalf("ToggleMarkdownCheckbox(0) = (%v, %v), want (true, true)", checked, ok)
	}
	if !strings.Contains(updated, "- [x] First task") {
		t.Fatalf("expected checkbox 0 checked:\n%s", updated)
	}
	reverted, checked2, ok2 := ToggleMarkdownCheckbox(updated, 0)
	if !ok2 || checked2 || reverted != src {
		t.Fatalf("round-trip toggle did not restore exact source: ok=%v checked=%v", ok2, checked2)
	}
}

func TestMarkdownActiveLinkAndActiveCheckboxHighlight(t *testing.T) {
	src := "- [ ] Check item\n- Links: [source](https://a.test) · [live](https://b.test)"
	th := DefaultDarkTheme()
	selBg := sgrBg(th, "selected")

	// Active checkbox highlights checkbox row with ▸ and selected background.
	mdNode := ViewNode{
		ID:        "md",
		Kind:      KindMarkdown,
		Focusable: true,
		Props:     map[string]any{"markdown": src, "activeCheck": 0, "activeLink": -1},
	}
	rows, f, _ := drawTheme(mdNode, th, "md", 60, 6)
	if !strings.Contains(rows[0], "▸ ☐ Check item") {
		t.Fatalf("active checkbox missing ▸ ☐ marker: %q", rows[0])
	}
	ansiRows := strings.Split(f.ANSI, "\n")
	if !strings.Contains(ansiRows[0], selBg) {
		t.Fatalf("active checkbox row missing selected bg %q: %q", selBg, ansiRows[0])
	}
	if strings.Contains(ansiRows[1], selBg) {
		t.Fatalf("non-active row must not carry selected bg: %q", ansiRows[1])
	}

	// Active link highlights only the active link pill with selected bg.
	mdNode.Props = map[string]any{"markdown": src, "activeCheck": -1, "activeLink": 1}
	rows2, f2, _ := drawTheme(mdNode, th, "md", 60, 6)
	if !strings.Contains(rows2[0], "☐ Check item") || strings.Contains(rows2[0], "▸") {
		t.Fatalf("unfocused checkbox should show ☐ without ▸: %q", rows2[0])
	}
	ansiRows2 := strings.Split(f2.ANSI, "\n")
	if strings.Contains(ansiRows2[0], selBg) {
		t.Fatalf("row 0 must not carry selected bg when link on row 1 is active: %q", ansiRows2[0])
	}
	if !strings.Contains(ansiRows2[1], selBg) {
		t.Fatalf("row 1 active link missing selected bg %q: %q", selBg, ansiRows2[1])
	}

	// When node is not focused, neither activeLink nor activeCheck paints selection bg.
	_, fUnfocused, _ := drawTheme(mdNode, th, "other", 60, 6)
	if strings.Contains(fUnfocused.ANSI, selBg) {
		t.Fatalf("unfocused KindMarkdown must not paint selected bg: %q", fUnfocused.ANSI)
	}
}
