package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Positions 0 to PaletteSize-1 are all distinct tones with a built-in
// colour; later ones repeat in rotation; ValidHue reads literals only.
func TestPaletteToneAtRotates(t *testing.T) {
	seen := map[string]bool{}
	for i := range PaletteSize {
		tone := PaletteToneAt(i)
		if _, ok := defaultTones[tone]; !ok || seen[tone] {
			t.Fatalf("position %d: %q is missing or repeated", i, tone)
		}
		seen[tone] = true
		if PaletteToneAt(i+PaletteSize) != tone {
			t.Fatalf("position %d does not wrap", i+PaletteSize)
		}
	}
	for hue, want := range map[string]bool{"#7FB685": true, "12": true, "bright-blue": true, "red": true, "palette.1": false, "bluish": false, "": false} {
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
		for i := range PaletteSize {
			c, ok := s.hueColor("palette." + string(rune('0'+i)))
			if !ok {
				t.Fatalf("%s: palette.%d draws nothing", theme, i)
			}
			r, g, b, _ := c.RGBA()
			distinct[strings.Join([]string{string(rune(r)), string(rune(g)), string(rune(b))}, ",")] = true
		}
		if len(distinct) != PaletteSize {
			t.Errorf("%s: %d distinct hues, want %d", theme, len(distinct), PaletteSize)
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
