package ochazuke

import (
	"strings"
	"testing"
)

// Styles.Selection is the one selection painter (UX R5.1): reverse video
// with the focus; without it the bar's colour faded dim-unfocused plus
// dim-unfocused-selection toward the surface, the faint reverse bar in
// mono or with no styles, and nothing under unfocused-selection hide.
func TestSelectionPainter(t *testing.T) {
	dark := NewStyles(Appearance{Theme: "dark", Dark: true})
	if got := dark.Selection(true).Render("x"); got != "\x1b[7mx\x1b[m" {
		t.Fatalf("focused: %q", got)
	}
	faded := dark.Selection(false)
	if !faded.GetReverse() || faded.GetFaint() {
		t.Fatalf("unfocused colour bar: reverse %v faint %v", faded.GetReverse(), faded.GetFaint())
	}
	if got, want := faded.GetForeground(), dark.fadeBy(dark.text, dark.barPercent()); got != want {
		t.Fatalf("unfocused bar colour %v, want %v", got, want)
	}
	mono := NewStyles(Appearance{Theme: "mono"})
	if st := mono.Selection(false); !st.GetReverse() || !st.GetFaint() {
		t.Fatalf("mono unfocused: %q", st.Render("x"))
	}
	if st := SelectionStyle(false); !st.GetReverse() || !st.GetFaint() {
		t.Fatalf("no styles unfocused: %q", st.Render("x"))
	}
	hidden := dark.WithUnfocusedSelection(false)
	if got := hidden.Selection(false).Render("x"); got != "x" {
		t.Fatalf("hide: %q", got)
	}
	if got := hidden.Selection(true).Render("x"); !strings.Contains(got, "\x1b[7m") {
		t.Fatalf("hide keeps the focused bar: %q", got)
	}
}

// Runtime.StylesFor reads the user's selection settings into a
// component's own appearance.
func TestStylesForReadsSelectionSettings(t *testing.T) {
	m, _ := toneFixture(t, "dark", nil, []ListItem{{ID: "a", Label: "alpha", Noun: "row"}})
	if err := m.Runtime.Settings().Set(UnfocusedSelectionSetting, "hide"); err != nil {
		t.Fatal(err)
	}
	st := m.Runtime.StylesFor(Appearance{Theme: "dark", Dark: true})
	if got := st.Selection(false).Render("x"); got != "x" {
		t.Fatalf("StylesFor ignores unfocused-selection hide: %q", got)
	}
	if got := m.Runtime.Selection(false).Render("x"); got != "x" {
		t.Fatalf("Runtime.Selection ignores unfocused-selection hide: %q", got)
	}
}
