package table_test

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/table"
)

func TestTableWidgetInlineRunsAndCellSpans(t *testing.T) {
	runs := []ui.InlineRun{
		{Text: "Fix "},
		{Kind: "link", Label: "PR #7", Href: "https://example.com/7"},
	}
	m := table.New(
		[]table.Column{
			{Key: "title", Title: "Title", Width: 22},
			{Key: "status", Title: "Status", Width: 10},
		},
		[]table.Row{
			{
				ID: "r1",
				Cells: map[string]any{
					"title":  runs,
					"status": "open",
				},
			},
			{
				ID: "r2",
				Cells: map[string]any{
					"title":  "Dissolving row",
					"status": "archived",
				},
				CellSpans: map[string][]ui.ToneSpan{
					"title": {{From: 0, To: 10, Tone: "row.dim"}, {From: 10, To: 14, Tone: "warning"}},
				},
			},
		},
	)

	// Text() extracts display text from inline runs for filtering & sorting.
	if got := table.Text(runs); got != "Fix PR #7" {
		t.Fatalf("table.Text(runs) = %q, want %q", got, "Fix PR #7")
	}
	m.Query = "PR #7"
	if got := m.Filtered(); len(got) != 1 || got[0].ID != "r1" {
		t.Fatalf("filtering over inline runs failed: %+v", got)
	}
	m.Query = ""

	// Marked rows shift CellSpans and prepend mark cleanly onto inline runs without panicking.
	m.Marked = map[string]bool{"r1": true, "r2": true}
	v := m.View("t")
	links := ui.NodeLinks(&v, 0)
	if len(links) != 1 || links[0].Label != "PR #7" || links[0].Href != "https://example.com/7" {
		t.Fatalf("NodeLinks on table.Model.View = %+v", links)
	}

	th := ui.DefaultDarkTheme()
	f, _ := ui.Render(v, th, "t", 36, 6)
	if !strings.Contains(f.ANSI, "● ") || !strings.Contains(f.ANSI, "PR #7") || !strings.Contains(f.ANSI, "Dissolving") {
		t.Fatalf("rendered table missing expected content:\n%s", f.ANSI)
	}
}
