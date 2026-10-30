package ochazuke

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// fullTitleRowOf is the model's title row as plain text, trailing cells
// kept, so a padded badge's cells count.
func fullTitleRowOf(m *Model) string {
	return strings.Split(ansi.Strip(m.View().Content), "\n")[0]
}

// cellOf is the cell column where sub starts in row, -1 when it is absent.
func cellOf(row, sub string) int {
	i := strings.Index(row, sub)
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(row[:i])
}

// The badge is the right end of the title row and the focus is left of it:
// a short and a long path draw the badge at the same column (R2.10).
func TestBadgeHoldsItsColumnAsTheFocusChanges(t *testing.T) {
	r, m := focusDisplayApp(t)
	off := cellOf(fullTitleRowOf(m), "NORMAL")
	for _, mode := range []string{FocusDisplayNoun, FocusDisplayStack} {
		if err := r.Settings().Set(FocusDisplaySetting, mode); err != nil {
			t.Fatal(err)
		}
		for _, focus := range []string{"f1", "f2"} {
			if err := r.SetFocus(focus); err != nil {
				t.Fatal(err)
			}
			row := fullTitleRowOf(m)
			if got := cellOf(row, "NORMAL"); got != off || off < 0 {
				t.Fatalf("%s at %s: badge at %d, want %d: %q", mode, focus, got, off, row)
			}
			if ansi.StringWidth(row) != 80 {
				t.Fatalf("row is %d cells: %q", ansi.StringWidth(row), row)
			}
		}
	}
	// The path ends just left of the badge.
	row := fullTitleRowOf(m)
	if !strings.Contains(row, "› file  NORMAL") {
		t.Fatalf("the focus is not left of the badge: %q", row)
	}
}

// With mode-badge.width widest (the default) every base mode's label takes
// the widest one's cells (the default runtime's visual-block), so a mode change moves neither the badge's left
// edge nor the focus beside it; fit keeps only the right edge.
func TestBadgeWidthWidestHoldsBothEdges(t *testing.T) {
	r, m := focusDisplayApp(t)
	if err := r.Settings().Set(FocusDisplaySetting, FocusDisplayNoun); err != nil {
		t.Fatal(err)
	}
	if r.ModeBadgeWidth() != len("VISUAL-BLOCK") {
		t.Fatalf("ModeBadgeWidth = %d, want %d", r.ModeBadgeWidth(), len("VISUAL-BLOCK"))
	}
	normal := fullTitleRowOf(m)
	if err := r.SetMode("visual-block"); err != nil {
		t.Fatal(err)
	}
	visual := fullTitleRowOf(m)
	if a, b := cellOf(normal, "NORMAL"), cellOf(visual, "VISUAL-BLOCK"); a != b || a < 0 {
		t.Fatalf("badge's left edge moved %d -> %d:\n%q\n%q", a, b, normal, visual)
	}
	if a, b := cellOf(normal, " file "), cellOf(visual, " file "); a != b || a < 0 {
		t.Fatalf("focus moved %d -> %d:\n%q\n%q", a, b, normal, visual)
	}

	if err := r.Settings().Set(ModeBadgeWidthSetting, ModeBadgeWidthFit); err != nil {
		t.Fatal(err)
	}
	if r.ModeBadgeWidth() != 0 {
		t.Fatalf("fit: ModeBadgeWidth = %d", r.ModeBadgeWidth())
	}
	visual = strings.TrimRight(fullTitleRowOf(m), " ")
	if err := r.SetMode("normal"); err != nil {
		t.Fatal(err)
	}
	normal = strings.TrimRight(fullTitleRowOf(m), " ")
	if !strings.HasSuffix(visual, "file  VISUAL-BLOCK") || !strings.HasSuffix(normal, "file  NORMAL") {
		t.Fatalf("fit:\n%q\n%q", visual, normal)
	}
	if ansi.StringWidth(visual) != ansi.StringWidth(normal) {
		t.Fatalf("fit moved the right edge:\n%q\n%q", visual, normal)
	}
	if err := r.Settings().Set(ModeBadgeWidthSetting, "bogus"); err == nil {
		t.Fatal("mode-badge.width took a value outside its enum")
	}
}

// keys.echo badge draws the echo between the focus and the badge in cells
// it keeps; neither the echo's keys nor the path move the badge.
func TestBadgeHoldsItsColumnWithTheKeyEcho(t *testing.T) {
	r, m := focusDisplayApp(t)
	off := cellOf(fullTitleRowOf(m), "NORMAL")
	for _, kv := range [][2]string{{KeyEchoSetting, KeyEchoBadge}, {FocusDisplaySetting, FocusDisplayStack}} {
		if err := r.Settings().Set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	empty := fullTitleRowOf(m)
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	row := fullTitleRowOf(m)
	for _, got := range []string{empty, row} {
		if c := cellOf(got, "NORMAL"); c != off {
			t.Fatalf("badge at %d, want %d: %q", c, off, got)
		}
	}
	// Left to right: title, focus, echo, badge.
	f, j, b := cellOf(row, "Work ›"), cellOf(row, " j "), cellOf(row, "NORMAL")
	if !(f > 0 && f < j && j < b) {
		t.Fatalf("order focus %d, echo %d, badge %d: %q", f, j, b, row)
	}
}

// At narrow widths the focus is cut first, then the title; the badge is
// whole at the right end at every width it fits.
func TestBadgeSurvivesNarrowWidths(t *testing.T) {
	r, m := focusDisplayApp(t)
	if err := r.Settings().Set(FocusDisplaySetting, FocusDisplayStack); err != nil {
		t.Fatal(err)
	}
	for w := 80; w >= 14; w-- {
		m.Update(tea.WindowSizeMsg{Width: w, Height: 12})
		row := fullTitleRowOf(m)
		if ansi.StringWidth(row) != w {
			t.Fatalf("width %d: row is %d cells: %q", w, ansi.StringWidth(row), row)
		}
		trimmed := strings.TrimRight(row, " ")
		if !strings.HasSuffix(trimmed, " NORMAL") {
			t.Fatalf("width %d: badge cut: %q", w, row)
		}
		// The title is cut only once the focus is gone.
		if !strings.Contains(row, "Ochazuke") && strings.Contains(row, "notes") {
			t.Fatalf("width %d: title cut before the focus: %q", w, row)
		}
	}
}
