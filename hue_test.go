package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The same key always draws the same palette tone, whatever else exists,
// and a handful of keys spread over several.
func TestPaletteToneStableAndSpread(t *testing.T) {
	ids := []string{"matomeru", "ochazuke", "oden", "nabe", "acme-web", "billing-api", "docs-site", "o-lane"}
	seen := map[string]bool{}
	for _, id := range ids {
		tone := PaletteTone(id)
		if tone != PaletteTone(id) || !strings.HasPrefix(tone, "palette.") {
			t.Fatalf("PaletteTone(%q) = %q, not stable", id, tone)
		}
		if _, ok := defaultTones[tone]; !ok {
			t.Fatalf("%q has no built-in tone", tone)
		}
		seen[tone] = true
	}
	if len(seen) < 5 {
		t.Fatalf("%d ids gave %d tones, want a spread", len(ids), len(seen))
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
