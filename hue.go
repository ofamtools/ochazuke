package ochazuke

import (
	"charm.land/lipgloss/v2"
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

// PanelHueSetting is the setting panel.hue's name; PanelHueOverStateSetting
// panel.hue-over-state: whether the hue beats a panel's state tone on the
// border (true, the default) or a state tone that draws keeps the border and
// the hue colours the title alone.
const (
	PanelHueSetting          = "panel.hue"
	PanelHueOverStateSetting = "panel.hue-over-state"
)

// PaletteSize is how many palette tones (palette.0 to palette.N-1) the
// built-in themes draw: the distinct hues PaletteToneAt gives. They come
// from a rule ((hue_palette.go)), not a hand-picked list.
func PaletteSize() int { return len(paletteRows) }

// PaletteToneAt is the palette tone of position n, wrapping round. An
// application that picks a colour for a thing chooses a position no other
// thing holds (and keeps it, writing the tone name); positions 0 to
// PaletteSize()-1 are all distinct.
func PaletteToneAt(n int) string {
	size := PaletteSize()
	return "palette." + strconv.Itoa(((n%size)+size)%size)
}

// ValidHue reports whether hue is one a panel can draw: a colour literal (a
// hex, an ANSI index, or an ANSI name such as bright-blue) or one of the
// palette tones. Another tone name is valid to the theme that defines it,
// not here.
func ValidHue(hue string) bool {
	if validColor(hue) {
		return true
	}
	if n, ok := strings.CutPrefix(hue, "palette."); ok {
		i, err := strconv.Atoi(n)
		return err == nil && i >= 0 && i < PaletteSize() && strconv.Itoa(i) == n
	}
	name, _ := strings.CutPrefix(strings.ToLower(hue), "bright-")
	_, ok := ansiNames[name]
	return ok
}

// SetHueSource registers fn as the hue of noun's things: fn answers a
// thing's key with its hue, "" for none. It reads memory, never a store: a
// frame calls it.
func (r *Runtime) SetHueSource(noun string, fn func(id string) string) {
	if n := r.Nouns[noun]; n != nil {
		n.Hue = fn
	}
}

// HueOf is the hue of thing, of noun: its own when noun has a hue
// source, else its owner's (the prop with RoleOwner whose noun has one).
func (r *Runtime) HueOf(noun string, thing Thing) string {
	n := r.Nouns[noun]
	if n == nil {
		return ""
	}
	if fn := n.Hue; fn != nil {
		if key := n.keyProp(); key != nil {
			if id, ok := thing[key.Name].(string); ok {
				return fn(id)
			}
		}
		return ""
	}
	if owner := n.roleProp(RoleOwner); owner != nil {
		if of := r.Nouns[owner.Noun]; of != nil && of.Hue != nil {
			if id, ok := thing[owner.Name].(string); ok && id != "" {
				return of.Hue(id)
			}
		}
	}
	return ""
}

// hueSettingParts are the parts of a panel each value of panel.hue draws.
var hueSettingParts = map[string]panelHue{
	"both":   {border: true, title: true},
	"border": {border: true},
	"title":  {title: true},
	"off":    {},
}

// hueParts are the parts of a panel the setting panel.hue draws a hue on.
func (r *Runtime) hueParts() (border, title bool) {
	parts, ok := hueSettingParts[r.Settings().String(PanelHueSetting)]
	if !ok {
		parts = hueSettingParts["both"]
	}
	return parts.border, parts.title
}

// hueOf is hue as a panelHue with the settings' parts.
func (r *Runtime) hueOf(hue string) panelHue {
	border, title := r.hueParts()
	return panelHue{hue: hue, border: border, title: title, overState: r.Settings().Bool(PanelHueOverStateSetting)}
}

// WithHue is l whose next panel draws hue as panel.hue says.
func (r *Runtime) WithHue(l Layout, hue string) Layout {
	l.panelHue = r.hueOf(hue)
	return l
}

// WithDetailHue is l whose split's second panel (the detail, right or
// below) draws hue as panel.hue says.
func (r *Runtime) WithDetailHue(l Layout, hue string) Layout {
	l.detailHue = r.hueOf(hue)
	return l
}

// panelHue is a panel's hue and which of its parts draw it.
type panelHue struct {
	hue           string
	border, title bool
	overState     bool // the hue beats a state tone on the border
}

// WithPanelHue is the layout whose next panel (Panel, PanelBlock) draws
// hue on its border, its title, or both. The hue wins over a panel tone
// (WithPanelTone). Focus keeps its marks: heavy lines and bold, the hue at
// full brightness, where an unfocused panel's hue is faded toward the
// background (HueFadePercent).
func (l Layout) WithPanelHue(hue string, border, title bool) Layout {
	l.panelHue = panelHue{hue: hue, border: border, title: title, overState: true}
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
func (s Styles) hueStyles(h panelHue, tone string, focused bool, edge, title lipgloss.Style) (lipgloss.Style, lipgloss.Style) {
	c, ok := s.hueColor(h.hue)
	if !ok {
		return edge, title
	}
	if !focused {
		c = s.fadeBy(c, HueFadePercent)
	}
	_, stateDrawn := s.chromeLook("border." + tone)
	if h.border && (h.overState || tone == "" || !stateDrawn) {
		edge = edge.Foreground(c).Bold(focused).Faint(false)
	}
	if h.title {
		title = title.Foreground(c).Bold(true).Faint(false)
	}
	return edge, title
}
