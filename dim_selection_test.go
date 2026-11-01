package ochazuke

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// unfocus moves the focus off the fixture's list to a sibling on the page.
func unfocus(m *Model) {
	m.Runtime.Instance("other", Parent("main"))
	m.Runtime.Focus = "other"
}

// wantBar is the SGR the dimmed bar opens with at percent toward the
// background.
func wantBar(m *Model, percent int) string {
	s := m.Runtime.Settings().page.styles()
	var st ansi.Style
	return st.Reverse(true).ForegroundColor(fadeRamp(s.text, s.ground)[percent]).String()
}

func setInt(t *testing.T, m *Model, name string, v int) {
	t.Helper()
	if err := m.Runtime.Settings().Set(name, v); err != nil {
		t.Fatal(err)
	}
}

// A list without the focus draws its selected row's bar faded beyond the
// list (dim-unfocused plus dim-unfocused-selection), still reversed; the
// focused list's bar is the plain reverse bar.
func TestUnfocusedSelectionBarDims(t *testing.T) {
	m, l := toneFixture(t, "dark", nil, []ListItem{
		{ID: "a", Label: "alpha", Noun: "row"},
		{ID: "b", Label: "beta", Noun: "row"},
	})
	if line := strings.Split(l.View(20, 2), "\n")[0]; !strings.Contains(line, "\x1b[7malpha") {
		t.Fatalf("focused bar %q", line)
	}
	unfocus(m)
	want := wantBar(m, DefaultDimUnfocused+DefaultDimUnfocusedSelection)
	if line := strings.Split(l.View(20, 2), "\n")[0]; !strings.Contains(line, want+"alpha") || !strings.Contains(ansi.Strip(line), "alpha") {
		t.Fatalf("unfocused bar %q, want %q", line, want)
	}
	// The other row keeps the list's own fade, no bar.
	if line := strings.Split(l.View(20, 2), "\n")[1]; strings.Contains(line, "\x1b[7") {
		t.Fatalf("other row reversed %q", line)
	}
}

// dim-unfocused-selection 0 draws the bar as before the setting: the
// plain reverse bar under the list's fade.
func TestUnfocusedSelectionZeroIsTodaysBar(t *testing.T) {
	m, l := toneFixture(t, "dark", nil, []ListItem{{ID: "a", Label: "alpha", Noun: "row"}})
	setInt(t, m, DimUnfocusedSelectionSetting, 0)
	unfocus(m)
	line := l.View(20, 1)
	if !strings.Contains(line, "\x1b[7malpha") || strings.Contains(line, "\x1b[7;") {
		t.Fatalf("bar with the setting off %q", line)
	}
}

// The bar's fade adds to dim-unfocused and stops at MaxDim; the list
// fade 0 leaves the bar its own percent.
func TestUnfocusedSelectionPercentAdds(t *testing.T) {
	m, l := toneFixture(t, "dark", nil, []ListItem{{ID: "a", Label: "alpha", Noun: "row"}})
	unfocus(m)
	setInt(t, m, DimUnfocusedSetting, 0)
	setInt(t, m, DimUnfocusedSelectionSetting, 40)
	if line := l.View(20, 1); !strings.Contains(line, wantBar(m, 40)+"alpha") {
		t.Fatalf("bar at 0+40 %q", line)
	}
	setInt(t, m, DimUnfocusedSetting, 60)
	setInt(t, m, DimUnfocusedSelectionSetting, 60)
	if line := l.View(20, 1); !strings.Contains(line, wantBar(m, MaxDim)+"alpha") {
		t.Fatalf("bar clamped to MaxDim %q", line)
	}
}

// Mono keeps the faint reverse bar (the list's faint is its dim) and
// draws no colour.
func TestUnfocusedSelectionMonoKeepsFaintBar(t *testing.T) {
	m, l := toneFixture(t, "mono", nil, []ListItem{{ID: "a", Label: "alpha", Noun: "row"}})
	unfocus(m)
	line := l.View(20, 1)
	if !strings.Contains(line, "\x1b[2m") || !strings.Contains(line, "\x1b[7malpha") || strings.Contains(line, "38;") {
		t.Fatalf("mono unfocused bar %q", line)
	}
}

// A table's selected row with tones takes the dimmed bar too: its runs
// in the bar's colour, a tone's bold kept; a tone background kept on the
// row (tones.selected-background keep) fades with the bar.
func TestUnfocusedSelectionBarInTableWithTones(t *testing.T) {
	cols := []Column{{Title: "pane"}, {Title: "state"}}
	m, l := toneFixture(t, "dark", cols, []ListItem{
		{ID: "a", Label: "a", Noun: "row", Cells: []string{"work", "errored"}, Tones: []ToneSpan{CellTone(1, ToneError)}},
		{ID: "b", Label: "b", Noun: "row", Cells: []string{"lint", "idle"}},
	})
	unfocus(m)
	bar := wantBar(m, DefaultDimUnfocused+DefaultDimUnfocusedSelection)
	row := strings.Split(l.View(30, 3), "\n")[1]
	if !strings.Contains(row, strings.TrimSuffix(bar, "m")) || !strings.Contains(ansi.Strip(row), "work") {
		t.Fatalf("toned row without the dimmed bar %q (want %q)", row, bar)
	}
	setInt(t, m, DimUnfocusedSelectionSetting, 0)
	if row := strings.Split(l.View(30, 3), "\n")[1]; strings.Contains(row, "\x1b[7;38;") {
		t.Fatalf("setting 0 still dims the toned row %q", row)
	}
	if err := m.Runtime.Settings().Set(SelectedBackgroundSetting, SelectedBackgroundKeep); err != nil {
		t.Fatal(err)
	}
	setInt(t, m, DimUnfocusedSelectionSetting, 30)
	kept := strings.Split(l.View(30, 3), "\n")[1]
	setInt(t, m, DimUnfocusedSelectionSetting, 0)
	if base := strings.Split(l.View(30, 3), "\n")[1]; kept == base {
		t.Fatalf("a kept tone background does not fade with the bar: %q", kept)
	}
}

// A cell-mode table's current cell fades with the bar: its colours at
// the bar's percent, not the list's.
func TestUnfocusedSelectionFadesCellCursor(t *testing.T) {
	cols := []Column{{Title: "pane"}, {Title: "state"}}
	m, l := toneFixture(t, "dark", cols, []ListItem{
		{ID: "a", Label: "a", Noun: "row", Cells: []string{"one", "two"}},
	}, CellSelect())
	unfocus(m)
	s := m.Runtime.Settings().page.styles()
	look := s.moded(s.tones[ToneCellCursor])
	if look.bg == nil {
		t.Skip("dark theme has no list.cell.cursor background")
	}
	at := func(p int) string { return fg(hexOf(fadeRamp(look.bg, s.ground)[p])) }
	row := strings.Split(l.View(30, 2), "\n")[1]
	if !strings.Contains(row, at(DefaultDimUnfocused+DefaultDimUnfocusedSelection)) {
		t.Fatalf("cell cursor not at the bar's fade %q", row)
	}
	setInt(t, m, DimUnfocusedSelectionSetting, 0)
	if row := strings.Split(l.View(30, 2), "\n")[1]; !strings.Contains(row, at(DefaultDimUnfocused)) {
		t.Fatalf("cell cursor with the setting off %q", row)
	}
}

// Styles.selectionBar is nil (the plain reverse bar) for a focused list,
// with the setting 0 and in mono.
func TestSelectionBarStyles(t *testing.T) {
	dark := NewStyles(Appearance{Theme: "dark"})
	if dark.selectionBar(false) != nil {
		t.Fatal("focused list has a dimmed bar")
	}
	if dark.selectionBar(true) == nil {
		t.Fatal("unfocused list has no dimmed bar at the default")
	}
	if off := dark.WithSelectionDim(0); off.selectionBar(true) != nil {
		t.Fatal("setting 0 dims the bar")
	}
	if mono := NewStyles(Appearance{Theme: "mono"}); mono.selectionBar(true) != nil {
		t.Fatal("mono dims the bar with a colour")
	}
	var none *Styles
	if none.selectionBar(true) != nil {
		t.Fatal("nil styles dim the bar")
	}
}

// hexOf is a colour as "#RRGGBB".
func hexOf(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8)
}
