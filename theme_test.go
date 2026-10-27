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

// Fade blends the text colour toward the surface by percent, clamped at
// MaxDim; mono and NO_COLOR fade with the faint attribute; 0 is no style.
// WithDims adds faint to the backdrop only.
func TestStylesFadeAndWithDims(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	mono := NewStyles(Appearance{Theme: "mono"})
	if got := mono.Fade(0).Render("x"); got != "x" {
		t.Fatalf("Fade(0) styled: %q", got)
	}
	if !mono.Fade(20).GetFaint() || !mono.Fade(MaxDim+50).GetFaint() {
		t.Fatal("mono fade is not faint")
	}
	dark := NewStyles(Appearance{Theme: "dark", Dark: true})
	rgb := func(c color.Color) [3]int {
		r, g, b, _ := c.RGBA()
		return [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
	}
	text, ground := rgb(dark.text), rgb(dark.ground)
	if text == ground {
		t.Fatalf("dark scheme text and surface are the same: %v", text)
	}
	half := dark.Fade(50)
	if half.GetFaint() {
		t.Fatal("a coloured fade is faint")
	}
	mid := rgb(half.GetForeground())
	for i := range 3 {
		lo, hi := min(text[i], ground[i]), max(text[i], ground[i])
		if mid[i] < lo || mid[i] > hi {
			t.Fatalf("Fade(50) %v is not between text %v and surface %v", mid, text, ground)
		}
	}
	if mid == text || mid == ground {
		t.Fatalf("Fade(50) %v is an end, not a blend of %v and %v", mid, text, ground)
	}
	// The fade is the perceptual blend, step p of a 101-step Blend1D ramp:
	// its ends are text and surface, and each percent is further from text.
	if want := rgb(lipgloss.Blend1D(fadeSteps, dark.text, dark.ground)[50]); mid != want {
		t.Fatalf("Fade(50) %v, Blend1D gives %v", mid, want)
	}
	ramp := fadeRamp(dark.text, dark.ground)
	near := func(a, b [3]int) bool {
		for i := range 3 {
			if a[i]-b[i] > 1 || b[i]-a[i] > 1 {
				return false
			}
		}
		return true
	}
	if len(ramp) != fadeSteps || !near(rgb(ramp[0]), text) || !near(rgb(ramp[fadeSteps-1]), ground) {
		t.Fatalf("ramp of %d from %v to %v, want %v to %v", len(ramp), rgb(ramp[0]), rgb(ramp[len(ramp)-1]), text, ground)
	}
	dist := func(p int) int {
		c, d := rgb(dark.Fade(p).GetForeground()), 0
		for i := range 3 {
			d += (c[i] - text[i]) * (c[i] - text[i])
		}
		return d
	}
	if !(dist(10) < dist(35) && dist(35) < dist(MaxDim)) {
		t.Fatalf("fades do not move away from text: %d %d %d", dist(10), dist(35), dist(MaxDim))
	}
	if rgb(dark.Fade(MaxDim+10).GetForeground()) != rgb(dark.Fade(MaxDim).GetForeground()) {
		t.Fatal("Fade is not clamped at MaxDim")
	}
	if rgb(dark.Fade(MaxDim).GetForeground()) == ground {
		t.Fatal("Fade(MaxDim) reached the surface")
	}
	dims := dark.WithDims(35, 20)
	if !dims.Backdrop.GetFaint() || rgb(dims.Backdrop.GetForeground()) != rgb(dark.Fade(35).GetForeground()) {
		t.Fatal("Backdrop is not the fade plus faint")
	}
	if dims.Unfocused.GetFaint() || rgb(dims.Unfocused.GetForeground()) != rgb(dark.Fade(20).GetForeground()) {
		t.Fatal("Unfocused is not the fade alone")
	}
	off := dark.WithDims(0, 0)
	if off.Backdrop.Render("x") != "x" || off.Unfocused.Render("x") != "x" {
		t.Fatal("WithDims(0, 0) styles")
	}
}
