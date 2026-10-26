package ochazuke

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
)

// An accent override replaces the theme's accent on the title, borders,
// active tab and accent highlights; errors and muted text keep their
// colours, and mono stays colourless.
func TestAppearanceAccentOverride(t *testing.T) {
	orange := lipgloss.Color("#E8833A")
	base := NewStyles(Appearance{Theme: "dark", Dark: true})
	over := NewStyles(Appearance{Theme: "dark", Dark: true, Accent: orange})
	same := func(a, b color.Color) bool {
		r1, g1, b1, a1 := a.RGBA()
		r2, g2, b2, a2 := b.RGBA()
		return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
	}
	for name, c := range map[string]color.Color{
		"title":         over.Title.GetForeground(),
		"subtitle":      over.Subtitle.GetForeground(),
		"accent":        over.Accent.GetForeground(),
		"active tab":    over.ActiveTab.GetForeground(),
		"badge":         over.Badge.GetForeground(),
		"panel border":  over.Panel.GetBorderTopForeground(),
		"focused panel": over.FocusedPanel.GetBorderTopForeground(),
	} {
		if !same(c, orange) {
			t.Errorf("%s is %v, want the override", name, c)
		}
	}
	if !same(over.Error.GetForeground(), base.Error.GetForeground()) || !same(over.Muted.GetForeground(), base.Muted.GetForeground()) {
		t.Error("the override changed a semantic colour")
	}
	if same(base.ActiveTab.GetForeground(), orange) {
		t.Error("the theme's accent is the override's colour")
	}
	mono := NewStyles(Appearance{Theme: "mono", Accent: orange})
	if _, ok := mono.ActiveTab.GetForeground().(lipgloss.NoColor); !ok {
		t.Errorf("mono active tab is %v, want no colour", mono.ActiveTab.GetForeground())
	}
}

// Background resolves the scheme's background (adaptive follows Dark) and
// reports plain for mono and NO_COLOR.
func TestAppearanceBackground(t *testing.T) {
	for _, tc := range []struct {
		a     Appearance
		bg    string
		plain bool
	}{
		{Appearance{Theme: "adaptive", Dark: true}, "dark", false},
		{Appearance{Theme: "adaptive", Dark: false}, "light", false},
		{Appearance{Theme: "light", Dark: true}, "light", false},
		{Appearance{Theme: "dark", Dark: false}, "dark", false},
		{Appearance{Theme: "mono", Dark: true}, "dark", true},
	} {
		if bg, plain := tc.a.Background(); bg != tc.bg || plain != tc.plain {
			t.Errorf("%s dark=%v: got %s plain=%v, want %s plain=%v", tc.a.Theme, tc.a.Dark, bg, plain, tc.bg, tc.plain)
		}
	}
	t.Setenv("NO_COLOR", "1")
	if _, plain := (Appearance{Theme: "dark"}).Background(); !plain {
		t.Error("NO_COLOR must be plain")
	}
}
