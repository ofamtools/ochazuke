package ui

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// ToneStyle is one semantic style. Fg and Bg may be "#RRGGBB", an ANSI index
// ("0".."255"), or a role name defined in Theme.Roles.
type ToneStyle struct {
	Fg            string `json:"fg,omitempty"`
	Bg            string `json:"bg,omitempty"`
	Bold          bool   `json:"bold,omitempty"`
	Dim           bool   `json:"dim,omitempty"`
	Italic        bool   `json:"italic,omitempty"`
	Underline     bool   `json:"underline,omitempty"`
	Strikethrough bool   `json:"strikethrough,omitempty"`
	Reverse       bool   `json:"reverse,omitempty"`
}

// Theme densities: how much vertical space forms and cards use. The empty
// Density means DensityBalanced; unknown values also render as balanced.
const (
	DensityCompact  = "compact"  // one row per form field, no card spacing
	DensityBalanced = "balanced" // three rows per form field, a spacer under card heads
	DensitySpacious = "spacious" // four rows per form field, padded cards
)

// Theme border styles: the glyphs frames are drawn with. The empty
// BorderStyle means BorderRounded; unknown values also render as rounded.
// Every style keeps the same one-cell frame geometry, so layout never
// depends on it.
const (
	BorderRounded = "rounded" // ╭─╮ corners, heavy ┏━┓ frame on focus
	BorderSharp   = "sharp"   // ┌─┐ corners, heavy ┏━┓ frame on focus
	BorderMinimal = "minimal" // top and bottom hairlines, open sides, ┃ on the left on focus
	BorderNone    = "none"    // no border glyphs; titles and spacing only
)

// Card border styles: how KindCard draws its frame chrome. The empty
// CardBorderStyle (CardBorderAuto) follows Theme.BorderStyle so existing
// themes keep their default bordered card behavior. Every style keeps the same
// one-cell frame ring geometry, so layout never depends on it.
const (
	CardBorderAuto    = ""        // follow Theme.BorderStyle (default)
	CardBorderSubtle  = "subtle"  // quiet left rail (┃ on focus, │ unfocused), no 4-sided box
	CardBorderMinimal = "minimal" // top and bottom hairlines, ┃ on the left on focus
	CardBorderRounded = "rounded" // traditional 4-sided rounded box border
	CardBorderNone    = "none"    // no border glyphs; whitespace and focus background tint only
)

// Theme maps semantic roles and tones to concrete styles. Bump Version
// whenever Roles or Tones change so renderer caches are invalidated; Name,
// Version, Density, BorderStyle and CardBorderStyle together key the renderer
// caches. A "canvas" tone with a background, when present, paints every cell
// that has no background of its own; the light preset uses it so its dark text
// stays legible on a dark terminal.
type Theme struct {
	Name            string               `json:"name"`
	Dark            bool                 `json:"dark"`
	Version         uint64               `json:"version,omitempty"`
	Density         string               `json:"density,omitempty"`         // DensityCompact, DensityBalanced (default), DensitySpacious
	BorderStyle     string               `json:"borderStyle,omitempty"`     // BorderRounded (default), BorderSharp, BorderMinimal, BorderNone
	CardBorderStyle string               `json:"cardBorderStyle,omitempty"` // CardBorderAuto (default), CardBorderSubtle, CardBorderMinimal, CardBorderRounded, CardBorderNone
	Roles           map[string]string    `json:"roles"`                     // text, muted, accent, border, surface, error, ...
	Tones           map[string]ToneStyle `json:"tones"`                     // unread, state.working, state.needs-input, badge.*, ...
}

// EffectiveDensity returns the density the renderer uses: Density when it is
// a known value, else DensityBalanced. The theme itself is not changed.
func (t Theme) EffectiveDensity() string {
	switch t.Density {
	case DensityCompact, DensitySpacious:
		return t.Density
	}
	return DensityBalanced
}

// EffectiveBorderStyle returns the border style the renderer uses:
// BorderStyle when it is a known value, else BorderRounded.
func (t Theme) EffectiveBorderStyle() string {
	switch t.BorderStyle {
	case BorderSharp, BorderMinimal, BorderNone:
		return t.BorderStyle
	}
	return BorderRounded
}

// EffectiveCardBorderStyle returns the card border style the renderer uses:
// CardBorderStyle when it is a known value, else CardBorderAuto.
func (t Theme) EffectiveCardBorderStyle() string {
	switch t.CardBorderStyle {
	case CardBorderSubtle, CardBorderMinimal, CardBorderRounded, CardBorderNone:
		return t.CardBorderStyle
	}
	return CardBorderAuto
}

// DefaultDarkTheme returns the built-in dark theme: balanced density and
// rounded borders.
func DefaultDarkTheme() Theme {
	return Theme{
		Name:        "ochazuke-dark",
		Dark:        true,
		Version:     1,
		Density:     DensityBalanced,
		BorderStyle: BorderRounded,
		Roles: map[string]string{
			"text":          "#c8d3f5",
			"muted":         "#8a93b8",
			"accent":        "#82aaff",
			"border":        "#4a5278",
			"surface":       "#1e2030",
			"surface.focus": "#2d3352",
			"selection":     "#3b4f87",
			"error":         "#ff757f",
			"success":       "#c3e88d",
			"warning":       "#ffc777",
			"info":          "#86e1fc",
			"magenta":       "#c099ff",
		},
		Tones: defaultTones(),
	}
}

// defaultTones returns a fresh copy of the tone table every built-in theme
// shares; tones name roles, so each theme's palette gives them their colors.
func defaultTones() map[string]ToneStyle {
	return map[string]ToneStyle{
		"text":              {Fg: "text"},
		"muted":             {Fg: "muted"},
		"dim":               {Fg: "muted", Dim: true},
		"accent":            {Fg: "accent"},
		"error":             {Fg: "error", Bold: true},
		"success":           {Fg: "success"},
		"warning":           {Fg: "warning"},
		"info":              {Fg: "info"},
		"unread":            {Fg: "warning", Bold: true},
		"state.working":     {Fg: "info"},
		"state.needs-input": {Fg: "warning", Bold: true},
		"state.done":        {Fg: "success"},
		"state.errored":     {Fg: "error", Bold: true},
		"title":             {Fg: "text", Bold: true},
		"border":            {Fg: "border"},
		"border.focus":      {Fg: "accent", Bold: true},
		"border.warn":       {Fg: "warning", Bold: true},
		"focus.border":      {Fg: "accent", Bold: true},
		"focus.bg":          {Bg: "surface.focus"},
		"focus.fg":          {Fg: "text", Bold: true},
		"focus.title":       {Fg: "accent", Bold: true, Reverse: true},
		"unfocused.border":  {Fg: "border", Dim: true},
		"unfocused.fg":      {Fg: "muted"},
		"selected":          {Fg: "text", Bg: "selection", Bold: true},
		"selected.inactive": {Fg: "text", Bg: "surface.focus"},
		"surface":           {Bg: "surface"},
		"input":             {Fg: "text", Bg: "surface"},
		"input.focus":       {Fg: "text", Bg: "surface.focus"},
		"input.edit":        {Fg: "text", Bg: "selection", Bold: true},
		"placeholder":       {Fg: "muted", Bg: "surface"},
		"badge":             {Fg: "text", Bg: "surface.focus"},
		"badge.accent":      {Fg: "surface", Bg: "accent", Bold: true},
		"badge.error":       {Fg: "surface", Bg: "error", Bold: true},
		"badge.success":     {Fg: "surface", Bg: "success", Bold: true},
		"badge.warning":     {Fg: "surface", Bg: "warning", Bold: true},
		"table.header":      {Fg: "accent", Bold: true},
		"key":               {Fg: "accent", Bold: true},
		"match":             {Fg: "warning", Bold: true},
		"heading.1":         {Fg: "accent", Bold: true},
		"heading.2":         {Fg: "info", Bold: true},
		"heading.3":         {Fg: "success", Bold: true},
		"link":              {Fg: "accent", Underline: true},
		"link.focused":      {Fg: "surface", Bg: "accent", Bold: true, Underline: true},
		"md.code":           {Fg: "magenta", Bg: "surface"},
		"md.bold":           {Fg: "text", Bold: true},
		"md.em":             {Fg: "info"},
		"md.quote":          {Fg: "muted"},
		"md.bullet":         {Fg: "accent"},
		"row.active":        {Fg: "text", Bold: true},
		"row.attention":     {Fg: "magenta"},
		"row.muted":         {Fg: "muted"},
		"row.dim":           {Fg: "muted", Dim: true},
		"row.cancelled":     {Fg: "muted", Dim: true, Strikethrough: true},
		"row.error":         {Fg: "error"},
		"state.review":      {Fg: "magenta", Bold: true},
		"state.blocked":     {Fg: "error", Bold: true},
		"state.planned":     {Fg: "muted"},
		"state.cancelled":   {Fg: "muted", Dim: true, Strikethrough: true},
		"state.done.dim":    {Fg: "success", Dim: true},
		"icon.star":         {Fg: "warning", Bold: true},
		"icon.unread":       {Fg: "warning", Bold: true},
		"table.group":       {Fg: "accent", Bold: true},
	}
}

// CompactDarkTheme returns the dark palette with compact density and minimal
// borders, for dense dashboards and small terminals.
func CompactDarkTheme() Theme {
	t := DefaultDarkTheme()
	t.Name = "ochazuke-dark-compact"
	t.Density, t.BorderStyle = DensityCompact, BorderMinimal
	return t
}

// SpaciousDarkTheme returns the dark palette with spacious density and
// rounded borders.
func SpaciousDarkTheme() Theme {
	t := DefaultDarkTheme()
	t.Name = "ochazuke-dark-spacious"
	t.Density, t.BorderStyle = DensitySpacious, BorderRounded
	return t
}

// DefaultLightTheme returns a high-contrast light theme: dark text on a white
// canvas, balanced density and rounded borders. Its "canvas" tone paints the
// whole frame white, so it reads the same on dark and light terminals.
func DefaultLightTheme() Theme {
	t := Theme{
		Name:        "ochazuke-light",
		Dark:        false,
		Version:     1,
		Density:     DensityBalanced,
		BorderStyle: BorderRounded,
		Roles: map[string]string{
			"text":          "#14161f",
			"muted":         "#464c63",
			"accent":        "#0846b8",
			"border":        "#5d6582",
			"surface":       "#ffffff",
			"surface.input": "#eceff6",
			"surface.focus": "#d4def4",
			"selection":     "#b7cbf4",
			"error":         "#ad0019",
			"success":       "#1b6510",
			"warning":       "#7d4a00",
			"info":          "#00577d",
			"magenta":       "#72249f",
		},
		Tones: defaultTones(),
	}
	t.Tones["canvas"] = ToneStyle{Fg: "text", Bg: "surface"}
	// Fields sit on a tint so they stand out from the white canvas.
	t.Tones["input"] = ToneStyle{Fg: "text", Bg: "surface.input"}
	t.Tones["placeholder"] = ToneStyle{Fg: "muted", Bg: "surface.input"}
	t.Tones["md.code"] = ToneStyle{Fg: "magenta", Bg: "surface.input"}
	return t
}

// HighContrastDarkTheme returns a crisp dark theme: white text on black,
// bright accents, balanced density and sharp borders.
func HighContrastDarkTheme() Theme {
	t := Theme{
		Name:        "ochazuke-high-contrast-dark",
		Dark:        true,
		Version:     1,
		Density:     DensityBalanced,
		BorderStyle: BorderSharp,
		Roles: map[string]string{
			"text":          "#ffffff",
			"muted":         "#c6cad6",
			"accent":        "#5cc8ff",
			"border":        "#a3abc6",
			"surface":       "#000000",
			"surface.focus": "#1d2333",
			"selection":     "#004f7a",
			"error":         "#ff5c6c",
			"success":       "#7dff8a",
			"warning":       "#ffd34d",
			"info":          "#6be4ff",
			"magenta":       "#e3a6ff",
		},
		Tones: defaultTones(),
	}
	t.Tones["dim"] = ToneStyle{Fg: "muted"} // faint text loses contrast; muted is enough
	t.Tones["row.dim"] = ToneStyle{Fg: "muted"}
	t.Tones["row.cancelled"] = ToneStyle{Fg: "muted", Strikethrough: true}
	t.Tones["state.cancelled"] = ToneStyle{Fg: "muted", Strikethrough: true}
	t.Tones["state.done.dim"] = ToneStyle{Fg: "success"}
	t.Tones["unfocused.border"] = ToneStyle{Fg: "border"}
	return t
}

type focusRoleAlias struct {
	role string
	bg   bool
	def  ToneStyle
}

var focusRoleAliases = map[string]focusRoleAlias{
	"focus.border":     {role: "focusBorder", def: ToneStyle{Fg: "accent", Bold: true}},
	"focus.bg":         {role: "focusBg", bg: true, def: ToneStyle{Bg: "surface.focus"}},
	"focus.fg":         {role: "focusFg", def: ToneStyle{Fg: "text", Bold: true}},
	"focus.title":      {role: "focusTitleFg", def: ToneStyle{Fg: "accent", Bold: true, Reverse: true}},
	"unfocused.border": {role: "unfocusedBorder", def: ToneStyle{Fg: "border", Dim: true}},
	"unfocused.fg":     {role: "unfocusedFg", def: ToneStyle{Fg: "muted"}},
}

// ThemePresets returns the built-in themes in display order: default dark,
// compact dark, spacious dark, light and high-contrast dark. Names are unique
// and every theme owns its maps, so callers may edit them freely.
func ThemePresets() []Theme {
	return []Theme{DefaultDarkTheme(), CompactDarkTheme(), SpaciousDarkTheme(), DefaultLightTheme(), HighContrastDarkTheme()}
}

// Tone resolves a tone name to a concrete style whose Fg/Bg are "#RRGGBB" or
// ANSI indexes. Unknown names fall back to a role name or a literal color as
// foreground, else the zero style. Focus and unfocused chrome tones also honor
// user-friendly camelCase role aliases (focusBorder, focusBg, focusFg,
// focusTitleFg, unfocusedBorder, unfocusedFg) when defined in Theme.Roles.
func (t Theme) Tone(name string) ToneStyle {
	var ts ToneStyle
	s, hasTone := t.Tones[name]
	if hasTone {
		ts = s
	} else if name != "" {
		if _, ok := t.Roles[name]; ok || colorOf(name) != nil {
			ts = ToneStyle{Fg: name}
		}
	}
	if alias, ok := focusRoleAliases[name]; ok {
		if _, hasRole := t.Roles[alias.role]; hasRole {
			if !hasTone {
				ts = alias.def
			}
			if alias.bg {
				if ts.Bg == "" || ts.Bg == alias.def.Bg {
					ts.Bg = alias.role
				}
			} else {
				if ts.Fg == "" || ts.Fg == alias.def.Fg {
					ts.Fg = alias.role
				}
			}
		}
	}
	ts.Fg = t.resolveColor(ts.Fg)
	ts.Bg = t.resolveColor(ts.Bg)
	return ts
}

// Color resolves a role name, hex string or ANSI index to a concrete color string.
func (t Theme) Color(c string) string { return t.resolveColor(c) }

func (t Theme) resolveColor(c string) string {
	for i := 0; i < 4 && c != ""; i++ {
		r, ok := t.Roles[c]
		if !ok {
			break
		}
		c = r
	}
	if colorOf(c) == nil {
		return ""
	}
	return c
}

func colorOf(s string) color.Color {
	if s == "" {
		return nil
	}
	if s[0] == '#' {
		if len(s) != 7 {
			return nil
		}
		if _, err := strconv.ParseUint(s[1:], 16, 32); err != nil {
			return nil
		}
		return lipgloss.Color(s)
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 255 {
		return lipgloss.Color(s)
	}
	return nil
}

// mergeTone overlays b on a: non-empty colors and set flags in b win.
func mergeTone(a, b ToneStyle) ToneStyle {
	if b.Fg != "" {
		a.Fg = b.Fg
	}
	if b.Bg != "" {
		a.Bg = b.Bg
	}
	a.Bold = a.Bold || b.Bold
	a.Dim = a.Dim || b.Dim
	a.Italic = a.Italic || b.Italic
	a.Underline = a.Underline || b.Underline
	a.Strikethrough = a.Strikethrough || b.Strikethrough
	a.Reverse = a.Reverse || b.Reverse
	return a
}

const reset = "\x1b[0m"

// styler turns tones into ANSI. A nil *styler produces unstyled text, which the
// measuring code uses to count lines without allocating a theme.
type styler struct {
	th   Theme
	sgrs map[ToneStyle]string
}

func newStyler(th Theme) *styler {
	return &styler{th: th, sgrs: map[ToneStyle]string{}}
}

func (s *styler) tone(name string) ToneStyle {
	if s == nil {
		return ToneStyle{}
	}
	return s.th.Tone(name)
}

// bgOf returns the background color a tone name denotes: its Bg, else its Fg.
func (s *styler) bgOf(name string) string {
	ts := s.tone(name)
	if ts.Bg != "" {
		return ts.Bg
	}
	return ts.Fg
}

func (s *styler) sgr(ts ToneStyle) string {
	if s == nil || ts == (ToneStyle{}) {
		return ""
	}
	if p, ok := s.sgrs[ts]; ok {
		return p
	}
	st := lipgloss.NewStyle()
	if c := colorOf(ts.Fg); c != nil {
		st = st.Foreground(c)
	}
	if c := colorOf(ts.Bg); c != nil {
		st = st.Background(c)
	}
	st = st.Bold(ts.Bold).Faint(ts.Dim).Italic(ts.Italic).Underline(ts.Underline).Strikethrough(ts.Strikethrough).Reverse(ts.Reverse)
	out := st.Render("X")
	prefix := ""
	if i := strings.Index(out, "X"); i > 0 {
		prefix = out[:i]
	}
	s.sgrs[ts] = prefix
	return prefix
}

func (s *styler) paint(text string, ts ToneStyle) string {
	p := s.sgr(ts)
	if p == "" || text == "" {
		return text
	}
	return p + text + reset
}

// t paints text with the named tone.
func (s *styler) t(name, text string) string { return s.paint(text, s.tone(name)) }
