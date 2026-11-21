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
	Fg        string `json:"fg,omitempty"`
	Bg        string `json:"bg,omitempty"`
	Bold      bool   `json:"bold,omitempty"`
	Dim       bool   `json:"dim,omitempty"`
	Underline bool   `json:"underline,omitempty"`
	Reverse   bool   `json:"reverse,omitempty"`
}

// Theme maps semantic roles and tones to concrete styles. Bump Version
// whenever Roles or Tones change so renderer caches are invalidated.
type Theme struct {
	Name    string               `json:"name"`
	Dark    bool                 `json:"dark"`
	Version uint64               `json:"version,omitempty"`
	Roles   map[string]string    `json:"roles"` // text, muted, accent, border, surface, error, ...
	Tones   map[string]ToneStyle `json:"tones"` // unread, state.working, state.needs-input, badge.*, ...
}

// DefaultDarkTheme returns the built-in dark theme.
func DefaultDarkTheme() Theme {
	return Theme{
		Name:    "ochazuke-dark",
		Dark:    true,
		Version: 1,
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
		Tones: map[string]ToneStyle{
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
			"selected":          {Fg: "text", Bg: "selection", Bold: true},
			"selected.inactive": {Fg: "text", Bg: "surface.focus"},
			"surface":           {Bg: "surface"},
			"input":             {Fg: "text", Bg: "surface"},
			"input.focus":       {Fg: "text", Bg: "surface.focus"},
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
			"md.code":           {Fg: "magenta", Bg: "surface"},
			"md.bold":           {Fg: "text", Bold: true},
			"md.em":             {Fg: "info"},
			"md.quote":          {Fg: "muted"},
			"md.bullet":         {Fg: "accent"},
		},
	}
}

// Tone resolves a tone name to a concrete style whose Fg/Bg are "#RRGGBB" or
// ANSI indexes. Unknown names fall back to a role name or a literal color as
// foreground, else the zero style.
func (t Theme) Tone(name string) ToneStyle {
	var ts ToneStyle
	if s, ok := t.Tones[name]; ok {
		ts = s
	} else if name != "" {
		if _, ok := t.Roles[name]; ok || colorOf(name) != nil {
			ts = ToneStyle{Fg: name}
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
	a.Underline = a.Underline || b.Underline
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
	st = st.Bold(ts.Bold).Faint(ts.Dim).Underline(ts.Underline).Reverse(ts.Reverse)
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
