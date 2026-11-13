package ochazuke

import (
	"charm.land/lipgloss/v2"
	"hash/fnv"
	"image/color"
	"strconv"
	"strings"
)

// A hue is the colour an application gives a thing (a project's colour):
// a panel showing the thing draws its border and title in it, so the eye
// tells whose panel it is. A hue is a tone name (palette.3, which a
// theme recolours) or a colour literal ("#7FB685", an ANSI index, an
// ANSI name such as bright-blue).
//
// Applications register which nouns carry a hue (SetHueSource); a thing
// has the hue of itself, else of the thing its owner prop (RoleOwner)
// names. Setting panel.hue says where it draws (both, border, title,
// off); mono and NO_COLOR draw none, and a thing without a hue keeps its
// panel's usual look.

// PanelHueSetting is the setting panel.hue's name.
const PanelHueSetting = "panel.hue"

// PaletteSize is how many palette tones (palette.0 to palette.9) the
// built-in themes draw: the distinct hues PaletteTone gives.
const PaletteSize = 10

// PaletteTone is the palette tone key maps to: the same key gives the same
// tone every run and on every machine (a hash of the key, never a list
// position), so a new thing never recolours the others. Keys beyond
// PaletteSize share tones.
func PaletteTone(key string) string {
	h := fnv.New32a()
	h.Write([]byte(key))
	return "palette." + strconv.Itoa(int(h.Sum32()%PaletteSize))
}

// SetHueSource registers fn as the hue of noun's things: fn answers a
// thing's key with its hue, "" for none. It reads memory, never a store: a
// frame calls it.
func (r *Runtime) SetHueSource(noun string, fn func(id string) string) {
	if r.hueSources == nil {
		r.hueSources = map[string]func(string) string{}
	}
	r.hueSources[noun] = fn
}

// HueOf is the hue of thing, of noun: its own when noun has a hue
// source, else its owner's (the prop with RoleOwner whose noun has one).
func (r *Runtime) HueOf(noun string, thing Thing) string {
	n := r.Nouns[noun]
	if n == nil || len(r.hueSources) == 0 {
		return ""
	}
	if fn := r.hueSources[n.Name]; fn != nil {
		for _, p := range n.Props {
			if p.Key {
				if id, ok := thing[p.Name].(string); ok {
					return fn(id)
				}
			}
		}
		return ""
	}
	for _, p := range n.Props {
		if p.Role != RoleOwner {
			continue
		}
		if fn := r.hueSources[p.Noun]; fn != nil {
			if id, ok := thing[p.Name].(string); ok && id != "" {
				return fn(id)
			}
		}
	}
	return ""
}

// hueParts are the parts of a panel the setting panel.hue draws a hue on.
func (r *Runtime) hueParts() (border, title bool) {
	switch r.Settings().String(PanelHueSetting) {
	case "off":
		return false, false
	case "border":
		return true, false
	case "title":
		return false, true
	}
	return true, true
}

// WithHue is l whose next panel draws hue as panel.hue says.
func (r *Runtime) WithHue(l Layout, hue string) Layout {
	border, title := r.hueParts()
	return l.WithPanelHue(hue, border, title)
}

// WithDetailHue is l whose split's second panel (the detail, right or
// below) draws hue as panel.hue says.
func (r *Runtime) WithDetailHue(l Layout, hue string) Layout {
	border, title := r.hueParts()
	l.detailHue = panelHue{hue: hue, border: border, title: title}
	return l
}

// panelHue is a panel's hue and which of its parts draw it.
type panelHue struct {
	hue           string
	border, title bool
}

// WithPanelHue is the layout whose next panel (Panel, PanelBlock) draws
// hue on its border, its title, or both. The hue wins over a panel tone
// (WithPanelTone). Focus keeps its marks: heavy lines and bold, the hue at
// full brightness, where an unfocused panel's hue is faded toward the
// background (HueFadePercent).
func (l Layout) WithPanelHue(hue string, border, title bool) Layout {
	l.panelHue = panelHue{hue: hue, border: border, title: title}
	return l
}

// HueFadePercent is how far an unfocused panel's hue is carried toward the
// background.
const HueFadePercent = 25

var ansiNames = map[string]int{"black": 0, "red": 1, "green": 2, "yellow": 3, "blue": 4, "magenta": 5, "cyan": 6, "white": 7}

// hueColor resolves a hue: a tone's foreground, else a literal.
func (s Styles) hueColor(hue string) (color.Color, bool) {
	if s.plain || hue == "" {
		return nil, false
	}
	if look, ok := s.tones[hue]; ok {
		return look.fg, look.fg != nil
	}
	if validColor(hue) {
		return lipgloss.Color(hue), true
	}
	name, bright := strings.CutPrefix(strings.ToLower(hue), "bright-")
	if n, ok := ansiNames[name]; ok {
		if bright {
			n += 8
		}
		return lipgloss.Color(strconv.Itoa(n)), true
	}
	return nil, false
}

// hueStyles are the border and title styles of a panel with a hue: ok is
// false when the hue draws nothing (unknown, mono, or off).
func (s Styles) hueStyles(h panelHue, focused bool, edge, title lipgloss.Style) (lipgloss.Style, lipgloss.Style) {
	c, ok := s.hueColor(h.hue)
	if !ok {
		return edge, title
	}
	if !focused {
		c = s.fadeBy(c, HueFadePercent)
	}
	if h.border {
		edge = edge.Foreground(c).Bold(focused).Faint(false)
	}
	if h.title {
		title = title.Foreground(c).Bold(true).Faint(false)
	}
	return edge, title
}
