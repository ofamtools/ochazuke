package ochazuke

import (
	"maps"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
)

// Chrome colour: how much colour the chrome (borders, the mode badge, the
// hint lines) draws is the setting chrome.color, a level: off, subtle or
// rich. A level is a family of tone rows, chrome.<level>.<role> (the role
// border.focused at rich is the tone chrome.rich.border.focused), so every
// colour is data: the built-in rows are in chromeTones, and a themes.json
// entry overrides any of them as it does any tone. A role with no row at
// the level draws as it did before the levels existed, so off, which has
// no rows, is that look exactly.
//
// A chrome row's colours may name the palette roles a tone names, plus
// mode: the colour of the current mode's badge (its row's background,
// else its foreground). An application's accent (Appearance.Accent)
// replaces the accent, border and mode roles in chrome rows, as it
// replaces the accent everywhere else.
//
// Roles (the fallback is the look before the levels):
//
//	border                      an unfocused panel's border
//	border.focused              a focused panel's border (heavy lines stay)
//	border.<tone>               a panel given a tone (Layout.WithPanelTone), a card's (Card.Tone)
//	border.modal                a modal's border
//	border.completion           the completion popup's border
//	border.split                the path picker's split rule
//	panel.title                 an unfocused panel's title
//	panel.title.focused         a focused panel's title
//	title.mark, title           the title row's ◆ and the title
//	mode.<name>, mode.pending,  the mode badge: the first row that exists of
//	mode.<extends>, mode        the mode's own, pending (a pending mode),
//	                            the mode it extends, then mode
//	hint.key, hint.text,        the footer's and every modal's hint line:
//	hint.sep                    a row's key, its action, the " · " between
const (
	// ChromeColorSetting is the chrome colour level, a row of
	// settings/ochazuke.json (appearance).
	ChromeColorSetting = "chrome.color"
	ChromeOff          = "off"
	ChromeSubtle       = "subtle"
	ChromeRich         = "rich"
)

// ChromeLevels are chrome.color's values.
var ChromeLevels = []string{ChromeOff, ChromeSubtle, ChromeRich}

// ChromeLevel is the chrome.color value, rich (its default) when it holds
// anything else.
func (r *Runtime) ChromeLevel() string {
	if v := r.Settings().String(ChromeColorSetting); slices.Contains(ChromeLevels, v) {
		return v
	}
	return ChromeRich
}

// modeRoles are the badge's role chain for the current mode: mode.<name>
// (held keys stripped, as the badge names it), mode.pending for a pending
// mode, mode.<extends> for each mode it extends, then mode.
func (r *Runtime) modeRoles() []string {
	name, keys := r.Mode.Name, r.Mode.Payload.Prefix
	if keys != "" && strings.HasSuffix(name, " "+keys) {
		name = strings.TrimSuffix(name, " "+keys)
	}
	roles := []string{"mode." + name}
	if r.pendingModes[r.Mode.Name] || r.pendingModes[name] {
		roles = append(roles, "mode.pending")
	}
	seen := map[string]bool{name: true}
	for parent := r.modeExtends[name]; parent != "" && !seen[parent]; parent = r.modeExtends[parent] {
		seen[parent] = true
		roles = append(roles, "mode."+parent)
	}
	return append(roles, "mode")
}

// The built-in chrome rows' colours, {light, dark}.
var (
	chromeTeal   = SchemeColor{"#5A9483", "#4F8F7A"}
	chromeInsert = SchemeColor{"#1F6FB2", "#7CB7E8"}
	chromeVisual = SchemeColor{"#6F42C1", "#C3A6E8"}
	chromeAmber  = SchemeColor{"#9A6700", "#E3B341"}
	chromeOrange = SchemeColor{"#984410", "#F0A060"}
	// The badge's pending and recording hues: on the light side darker
	// than the amber and error they come from, so the surface ink on them
	// clears 4.5:1 at 256 colours too (TestChromeRowsContrast).
	chromePending = SchemeColor{"#8A5C00", "#E3B341"}
	chromeRec     = SchemeColor{"#9B3530", "error"}
	chromeHint    = SchemeColor{"#5B6A66", "muted"}
	chromeGreen   = SchemeColor{"#2E7D32", "#8BC48A"}
	chromeError   = SchemeColor{"error", "error"}
	chromeMode    = SchemeColor{"mode", "mode"}
	chromeAccent  = SchemeColor{"accent", "accent"}
	chromeSurf    = SchemeColor{"surface", "surface"}
	chromeText    = SchemeColor{"text", "text"}
)

// chromeModes are the badge hues by mode role, shared by both levels:
// rich fills the badge with the hue, subtle draws the hue on the surface.
var chromeModes = map[string]SchemeColor{
	"mode":                 chromeAccent,
	"mode.normal":          chromeAccent,
	"mode.insert":          chromeInsert,
	"mode.visual":          chromeVisual,
	"mode.visual-line":     chromeVisual,
	"mode.visual-block":    chromeVisual,
	"mode.pending":         chromePending,
	"mode.command":         chromeOrange,
	"mode.terminal":        chromeGreen,
	"mode.list_filter":     chromeInsert,
	"mode.macro_recording": chromeRec,
}

// chromeTones are the built-in rows of the subtle and rich levels (off has
// none). rich colours every border (a toned panel by its state, the
// focused one by the mode), fills the badge and lifts the hint keys;
// subtle tints foregrounds only and colours only the alert states.
func chromeTones() map[string]ToneStyle {
	rows := map[string]ToneStyle{
		"chrome.rich.border":                   {Fg: chromeTeal},
		"chrome.rich.border.focused":           {Fg: chromeMode, Bold: true},
		"chrome.rich.border.modal":             {Fg: chromeMode},
		"chrome.rich.border.completion":        {Fg: chromeMode},
		"chrome.rich.border.split":             {Fg: chromeTeal},
		"chrome.rich.border.state.working":     {Fg: chromeInsert},
		"chrome.rich.border.state.needs-input": {Fg: chromeAmber, Bold: true},
		"chrome.rich.border.state.errored":     {Fg: chromeError, Bold: true},
		"chrome.rich.border.state.done":        {Fg: chromeGreen},
		"chrome.rich.panel.title.focused":      {Fg: chromeMode, Bold: true},
		"chrome.rich.hint.key":                 {Fg: chromeAccent, Bold: true},
		"chrome.rich.hint.text":                {Fg: chromeHint},

		"chrome.subtle.border.state.needs-input": {Fg: chromeAmber},
		"chrome.subtle.border.state.errored":     {Fg: chromeError},
		"chrome.subtle.hint.key":                 {Fg: chromeText},
	}
	for role, hue := range chromeModes {
		rows["chrome.rich."+role] = ToneStyle{Fg: chromeSurf, Bg: hue, Bold: true}
		rows["chrome.subtle."+role] = ToneStyle{Fg: hue, Bg: chromeSurf, Bold: true}
	}
	return rows
}

func init() { maps.Copy(defaultTones, chromeTones()) }

// chromeTonePrefix is the namespace of the chrome rows.
const chromeTonePrefix = "chrome."

// isChromeTone reports whether a tone is a chrome row (chrome.<level>.<role>),
// whose accent, border and mode roles an application's accent replaces.
func isChromeTone(name string) bool { return strings.HasPrefix(name, chromeTonePrefix) }

// WithChrome sets the chrome level (chrome.color) and the current mode's
// badge role chain (Runtime.modeRoles): the badge takes the first row of
// the chain that exists, the mode role its colour, and the panel styles
// their level's border rows. Level off, or a level with no rows, leaves
// every style as it was.
func (s Styles) WithChrome(level string, modeRoles []string) Styles {
	s.chrome = level
	if s.appAccent != nil {
		s.modeColor = s.appAccent
	} else {
		s.modeColor = s.accent
	}
	for _, role := range modeRoles {
		look, ok := s.chromeLook(role)
		if !ok {
			continue
		}
		s.Badge = look.over(s.Badge)
		if s.appAccent == nil {
			switch {
			case look.bg != nil && !look.bgMode:
				s.modeColor = look.bg
			case look.fg != nil && !look.fgMode:
				s.modeColor = look.fg
			}
		}
		break
	}
	if s.appAccent == nil {
		if look, ok := s.chromeLook("border"); ok && look.fg != nil {
			s.Panel = s.Panel.BorderForeground(look.fg)
			s.FocusedPanel = s.FocusedPanel.BorderForeground(look.fg)
		}
		if look, ok := s.chromeLook("border.focused"); ok && look.fg != nil {
			s.FocusedPanel = s.FocusedPanel.BorderForeground(look.fg)
		}
	}
	return s
}

// ChromeLevel is the level WithChrome set ("" before it).
func (s Styles) ChromeLevel() string { return s.chrome }

// Chrome is the style of a chrome role at the current level: fallback with
// the colours the row chrome.<level>.<role> names laid over it and the
// row's attributes (bold, dim), or fallback itself when the scheme has no
// such row. fallback keeps everything a row does not set (padding,
// borders, a colour the row leaves empty).
func (s Styles) Chrome(role string, fallback lipgloss.Style) lipgloss.Style {
	look, ok := s.chromeLook(role)
	if !ok {
		return fallback
	}
	return look.over(fallback)
}

// HasChrome reports whether any of the roles has a row at the current
// level.
func (s Styles) HasChrome(roles ...string) bool {
	for _, role := range roles {
		if _, ok := s.chromeLook(role); ok {
			return true
		}
	}
	return false
}

// chromeLook is the row of role at the current level, its mode role
// resolved.
func (s Styles) chromeLook(role string) (toneLook, bool) {
	if s.chrome == "" {
		return toneLook{}, false
	}
	look, ok := s.tones[chromeTonePrefix+s.chrome+"."+role]
	if !ok {
		return toneLook{}, false
	}
	return s.moded(look), true
}

// moded is look with its mode role resolved to the current mode's colour.
func (s Styles) moded(look toneLook) toneLook {
	if look.fgMode {
		look.fg = s.modeColor
	}
	if look.bgMode {
		look.bg = s.modeColor
	}
	return look
}

// over lays the look on st: its colours where it has them, its attributes.
func (l toneLook) over(st lipgloss.Style) lipgloss.Style {
	if l.fg != nil {
		st = st.Foreground(l.fg)
	}
	if l.bg != nil {
		st = st.Background(l.bg)
	}
	return st.Bold(l.bold).Faint(l.dim)
}

// panelEdge is the style a panel's border lines draw in, edge being the
// look before the levels. The first that exists wins:
//
//  1. the row border.<tone> (a panel's state; on a focused panel bold);
//  2. the application's accent (Appearance.Accent): edge, which carries it;
//  3. for a focused panel, the row border.focused (the mode tint);
//  4. the row border;
//  5. edge.
//
// Focus is also marked by shape: a focused panel keeps its heavy lines.
func (s Styles) panelEdge(tone string, focused bool, edge lipgloss.Style) lipgloss.Style {
	if tone != "" {
		if look, ok := s.chromeLook("border." + tone); ok {
			st := look.over(edge)
			if focused {
				st = st.Bold(true)
			}
			return st
		}
	}
	if s.appAccent != nil {
		return edge
	}
	if focused {
		if look, ok := s.chromeLook("border.focused"); ok {
			return look.over(edge)
		}
	}
	if look, ok := s.chromeLook("border"); ok {
		return look.over(edge)
	}
	return edge
}

// chromeRender draws text in role's row, or returns it as it is when the
// level has no such row (the look before the levels: unstyled).
func (s Styles) chromeRender(role, text string) string {
	look, ok := s.chromeLook(role)
	if !ok {
		return text
	}
	return look.over(lipgloss.NewStyle()).Render(text)
}
