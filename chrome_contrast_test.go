package ochazuke

import (
	"image/color"
	"math"
	"slices"
	"strings"
	"testing"
)

// The contrast helpers are copied from tuios internal/overlay/contrast.go
// (ContrastRatio, relativeLuminance, linearize, xtermSlots): WCAG 2.1
// relative luminance and contrast ratio, the sixteen slots measured as
// xterm's defaults.

func contrastRatio(a, b color.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func relativeLuminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return 0.2126*linearize(float64(r)/65535) + 0.7152*linearize(float64(g)/65535) + 0.0722*linearize(float64(b)/65535)
}

func linearize(c float64) float64 {
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

var xtermSlots = [16]color.RGBA{
	{0x00, 0x00, 0x00, 0xff}, {0xcd, 0x00, 0x00, 0xff}, {0x00, 0xcd, 0x00, 0xff}, {0xcd, 0xcd, 0x00, 0xff},
	{0x00, 0x00, 0xee, 0xff}, {0xcd, 0x00, 0xcd, 0xff}, {0x00, 0xcd, 0xcd, 0xff}, {0xe5, 0xe5, 0xe5, 0xff},
	{0x7f, 0x7f, 0x7f, 0xff}, {0xff, 0x00, 0x00, 0xff}, {0x00, 0xff, 0x00, 0xff}, {0xff, 0xff, 0x00, 0xff},
	{0x5c, 0x5c, 0xff, 0xff}, {0xff, 0x00, 0xff, 0xff}, {0x00, 0xff, 0xff, 0xff}, {0xff, 0xff, 0xff, 0xff},
}

// nearest is the palette entry closest to c (squared RGB distance) and its
// index, as a terminal steps a colour down.
func nearest(c color.Color, palette []color.RGBA) (color.RGBA, int) {
	r, g, b, _ := c.RGBA()
	best, at := math.MaxFloat64, 0
	for i, p := range palette {
		dr, dg, db := float64(r>>8)-float64(p.R), float64(g>>8)-float64(p.G), float64(b>>8)-float64(p.B)
		if d := dr*dr + dg*dg + db*db; d < best {
			best, at = d, i
		}
	}
	return palette[at], at
}

// xterm256 is the 256-colour palette's cube and grey ramp (indexes 16-255).
func xterm256() []color.RGBA {
	steps := []uint8{0, 95, 135, 175, 215, 255}
	var out []color.RGBA
	for _, r := range steps {
		for _, g := range steps {
			for _, b := range steps {
				out = append(out, color.RGBA{r, g, b, 0xff})
			}
		}
	}
	for i := range 24 {
		v := uint8(8 + 10*i)
		out = append(out, color.RGBA{v, v, v, 0xff})
	}
	return out
}

// The floors: text (a badge's label, a hint key) 4.5:1, a border or a
// mark 3:1 (WCAG 1.4.3 and 1.4.11).
const (
	textFloor   = 4.5
	borderFloor = 3.0
)

// Every built-in chrome row reads: a row with a background (the badge)
// against it, a text row and a border row against the ground (the
// scheme's surface), on both sides, in true colour and stepped down to
// 256 colours. At 16 colours, where the terminal's palette decides the
// real colour, an ink never lands on its ground's slot (the tuios bug of
// light ink on a light slot).
func TestChromeRowsContrast(t *testing.T) {
	palette := xterm256()
	var slots []color.RGBA
	slots = append(slots, xtermSlots[:]...)
	for _, theme := range []string{"dark", "light"} {
		base := NewStyles(Appearance{Theme: theme})
		for _, name := range chromeRowNames() {
			level, role, _ := strings.Cut(strings.TrimPrefix(name, chromeTonePrefix), ".")
			s := base.WithChrome(level, nil)
			look, _ := s.chromeLook(role)
			fg, bg := look.fg, look.bg
			if fg == nil {
				continue
			}
			floor := textFloor
			if strings.HasPrefix(role, "border") {
				floor = borderFloor
			}
			if bg == nil {
				bg = s.ground
			}
			if got := contrastRatio(fg, bg); got < floor {
				t.Errorf("%s %s: %.2f:1 in true colour, want %.1f", theme, name, got, floor)
			}
			f256, _ := nearest(fg, palette)
			b256, _ := nearest(bg, palette)
			if got := contrastRatio(f256, b256); got < floor {
				t.Errorf("%s %s: %.2f:1 at 256 colours, want %.1f", theme, name, got, floor)
			}
			if _, fi := nearest(fg, slots); true {
				if _, bi := nearest(bg, slots); fi == bi {
					t.Errorf("%s %s: ink and ground both step down to slot %d at 16 colours", theme, name, fi)
				}
			}
		}
	}
}

// chromeRowNames are the built-in chrome rows, sorted.
func chromeRowNames() []string {
	var out []string
	for name := range defaultTones {
		if isChromeTone(name) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}
