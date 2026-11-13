package ochazuke

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Positions 0 to PaletteSize-1 are all distinct tones with a built-in
// colour; later ones repeat in rotation; ValidHue reads literals only.
func TestPaletteToneAtRotates(t *testing.T) {
	seen := map[string]bool{}
	for i := range PaletteSize() {
		tone := PaletteToneAt(i)
		if _, ok := defaultTones[tone]; !ok || seen[tone] {
			t.Fatalf("position %d: %q is missing or repeated", i, tone)
		}
		seen[tone] = true
		if PaletteToneAt(i+PaletteSize()) != tone {
			t.Fatalf("position %d does not wrap", i+PaletteSize())
		}
	}
	for hue, want := range map[string]bool{"#7FB685": true, "12": true, "bright-blue": true, "red": true, "palette.1": true, "palette.99": false, "palette.01": false, "bluish": false, "": false} {
		if ValidHue(hue) != want {
			t.Errorf("ValidHue(%q) = %v, want %v", hue, !want, want)
		}
	}
}

// A built-in dark and light theme each draw every palette tone; mono none.
func TestPaletteTonesPerTheme(t *testing.T) {
	for _, theme := range []string{"dark", "light", "adaptive"} {
		s := NewStyles(Appearance{Theme: theme, Dark: true})
		distinct := map[string]bool{}
		for i := range PaletteSize() {
			c, ok := s.hueColor(PaletteToneAt(i))
			if !ok {
				t.Fatalf("%s: palette.%d draws nothing", theme, i)
			}
			r, g, b, _ := c.RGBA()
			distinct[strings.Join([]string{string(rune(r)), string(rune(g)), string(rune(b))}, ",")] = true
		}
		if len(distinct) != PaletteSize() {
			t.Errorf("%s: %d distinct hues, want %d", theme, len(distinct), PaletteSize())
		}
	}
	if _, ok := NewStyles(Appearance{Theme: "mono"}).hueColor("palette.0"); ok {
		t.Error("mono draws a hue")
	}
}

// The hue draws on the border, the title, both or neither as panel.hue
// says; no hue keeps today's panel; a focused panel still differs from an
// unfocused one, and the hue wins over a state tone.
func TestPanelHueParts(t *testing.T) {
	r := NewRuntime()
	s := NewStyles(Appearance{Theme: "dark"})
	plain := func(focused bool) string { return Layout{Styles: s}.Panel("pane", "body", 30, 5, focused) }
	hued := func(focused bool, hue string) string {
		return r.WithHue(Layout{Styles: s}, hue).Panel("pane", "body", 30, 5, focused)
	}
	top := func(text string) string { return strings.SplitN(text, "\n", 2)[0] }
	side := func(text string) string { return strings.Split(text, "\n")[1] }
	if err := r.Settings().Set(PanelHueSetting, "off"); err != nil {
		t.Fatal(err)
	}
	offTop := top(hued(true, "palette.2"))
	if offTop != top(plain(true)) {
		t.Fatal("off changed the panel")
	}
	for _, c := range []struct {
		value         string
		border, title bool
	}{{"both", true, true}, {"border", true, false}, {"title", false, true}, {"off", false, false}} {
		if err := r.Settings().Set(PanelHueSetting, c.value); err != nil {
			t.Fatal(err)
		}
		got := hued(true, "palette.2")
		if (side(got) != side(plain(true))) != c.border {
			t.Errorf("%s: border changed = %v, want %v", c.value, !c.border, c.border)
		}
		// The title is the top row less its border: strip the border's own
		// colour by comparing with the border-only row.
		if c.title && top(got) == top(hued2(r, s, "border")) {
			t.Errorf("%s: title not drawn in the hue", c.value)
		}
		if ansi.Strip(got) != ansi.Strip(plain(true)) {
			t.Errorf("%s: the hue changed the text", c.value)
		}
	}
	r.Settings().Set(PanelHueSetting, "both")
	if hued(true, "") != plain(true) || hued(false, "") != plain(false) {
		t.Error("no hue changes the panel")
	}
	if hued(true, "palette.2") == hued(false, "palette.2") {
		t.Error("focused and unfocused hued panels look the same")
	}
	if got := r.WithHue(Layout{Styles: s}.WithPanelTone(ToneNeedsInput), "palette.2").Panel("pane", "body", 30, 5, false); got != hued(false, "palette.2") {
		t.Error("the state tone beat the hue")
	}
}

// hued2 is a focused hued panel's top row with setting value.
func hued2(r *Runtime, s Styles, value string) string {
	prev := r.Settings().String(PanelHueSetting)
	defer r.Settings().Set(PanelHueSetting, prev)
	r.Settings().Set(PanelHueSetting, value)
	return strings.SplitN(r.WithHue(Layout{Styles: s}, "palette.2").Panel("pane", "body", 30, 5, true), "\n", 2)[0]
}

// A thing has its own noun's hue, else its owner's.
func TestHueOfThingAndOwner(t *testing.T) {
	r := NewRuntime()
	r.Nouns["project"] = &Noun{Name: "project", Props: []PropSpec{{Name: "id", Key: true}}}
	r.Nouns["pane"] = &Noun{Name: "pane", Props: []PropSpec{{Name: "id", Key: true}, {Name: "project", Role: RoleOwner, Parameter: Parameter{Noun: "project"}}}}
	r.Nouns["tool"] = &Noun{Name: "tool", Props: []PropSpec{{Name: "id", Key: true}}}
	r.SetHueSource("project", func(id string) string { return "hue-" + id })
	for _, c := range []struct {
		noun  string
		thing Thing
		want  string
	}{
		{"project", Thing{"id": "a"}, "hue-a"},
		{"pane", Thing{"id": "w", "project": "b"}, "hue-b"},
		{"pane", Thing{"id": "w", "project": ""}, ""},
		{"tool", Thing{"id": "t"}, ""},
	} {
		if got := r.HueOf(c.noun, c.thing); got != c.want {
			t.Errorf("HueOf(%s, %v) = %q, want %q", c.noun, c.thing, got, c.want)
		}
	}
}

// panel.hue-over-state: true (default) the hue wins the border over a state
// tone; false a state tone that draws keeps the border and the hue colours
// the title. A state with no border row never hides the hue.
func TestPanelHueOverState(t *testing.T) {
	r := NewRuntime()
	s := NewStyles(Appearance{Theme: "dark"}).WithChrome(ChromeRich, nil)
	draw := func(layout Layout, hue bool) string {
		if hue {
			layout = r.WithHue(layout, "palette.2")
		}
		return layout.Panel("pane", "body", 30, 5, true)
	}
	side := func(text string) string { return strings.Split(text, "\n")[1] }
	for _, c := range []struct {
		over bool
		tone string
		want string // whose border: "hue" or "state"
	}{{true, ToneNeedsInput, "hue"}, {false, ToneNeedsInput, "state"}, {false, ToneIdle, "hue"}} {
		if err := r.Settings().Set(PanelHueOverStateSetting, c.over); err != nil {
			t.Fatal(err)
		}
		base := Layout{Styles: s}.WithPanelTone(c.tone)
		got := side(draw(base, true))
		hueOnly := side(r.WithHue(Layout{Styles: s}, "palette.2").Panel("pane", "body", 30, 5, true))
		stateOnly := side(draw(base, false))
		if c.want == "hue" && got != hueOnly || c.want == "state" && got != stateOnly {
			t.Errorf("over=%v tone=%s: border is not the %s's", c.over, c.tone, c.want)
		}
		if top := strings.SplitN(draw(base, true), "\n", 2)[0]; top == strings.SplitN(draw(base, false), "\n", 2)[0] {
			t.Errorf("over=%v tone=%s: the title lost the hue", c.over, c.tone)
		}
	}
}

func hexRGB(h string) (r, g, b float64) {
	var v [3]int
	for i := range v {
		n, _ := strconv.ParseInt(h[1+2*i:3+2*i], 16, 32)
		v[i] = int(n)
	}
	lin := func(c int) float64 {
		x := float64(c) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return lin(v[0]), lin(v[1]), lin(v[2])
}

func relLum(h string) float64 {
	r, g, b := hexRGB(h)
	return 0.2126*r + 0.7152*g + 0.0722*b
}

func okLab(h string) [3]float64 {
	r, g, b := hexRGB(h)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return [3]float64{0.2104542553*l + 0.7936177850*m - 0.0040720468*s, 1.9779984951*l - 2.4285922050*m + 0.4505937099*s, 0.0259040371*l + 0.7827717662*m - 0.8086757660*s}
}

// The generated palette's bounds, per built-in side: every tone readable on
// the surface (3:1), any two at least 0.10 apart in OKLab, a tone as an
// unfocused panel draws it (faded by the real fade) at least 0.055 from any
// other tone at full strength, none within 0.06 of the error red or the
// needs-input amber.
func TestPaletteBounds(t *testing.T) {
	if PaletteSize() < 12 {
		t.Fatalf("palette has %d tones", PaletteSize())
	}
	for i, side := range []string{"light", "dark"} {
		surface := paletteSurface[i]
		s := NewStyles(Appearance{Theme: side})
		var cols []string
		var full, faded [][3]float64
		for _, r := range paletteRows {
			c := r.light
			if side == "dark" {
				c = r.dark
			}
			cols = append(cols, c)
			full = append(full, hexOKLab(c))
			fc, ok := s.hueColor(PaletteToneAt(len(cols) - 1))
			if !ok {
				t.Fatalf("%s: tone %d draws nothing", side, len(cols)-1)
			}
			r2, g2, b2, _ := s.fadeBy(fc, HueFadePercent).RGBA()
			faded = append(faded, hexOKLab(fmt.Sprintf("#%02X%02X%02X", r2>>8, g2>>8, b2>>8)))
		}
		minFull, minFade, minFadeFade, minRes := 9.0, 9.0, 9.0, 9.0
		for a, c := range cols {
			hi, lo := math.Max(relLum(c), relLum(surface)), math.Min(relLum(c), relLum(surface))
			if ratio := (hi + 0.05) / (lo + 0.05); ratio < 3 {
				t.Errorf("%s %s: contrast %.2f against %s", side, c, ratio, surface)
			}
			for _, r := range paletteReserved[i] {
				minRes = math.Min(minRes, labDistance(full[a], hexOKLab(r)))
			}
			for b := range cols {
				if a == b {
					continue
				}
				minFade = math.Min(minFade, labDistance(faded[a], full[b]))
				if b > a {
					minFull = math.Min(minFull, labDistance(full[a], full[b]))
					minFadeFade = math.Min(minFadeFade, labDistance(faded[a], faded[b]))
				}
			}
		}
		t.Logf("%s: %d tones, min full %.3f, faded vs other full %.3f, faded vs faded %.3f, reserved %.3f", side, len(cols), minFull, minFade, minFadeFade, minRes)
		if minFull < 0.10 || minFade < 0.055 || minRes < 0.06 {
			t.Errorf("%s: full %.3f faded %.3f reserved %.3f below bounds", side, minFull, minFade, minRes)
		}
	}
}
