package ochazuke

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"slices"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
)

// Appearance is event-loop-owned configuration. Operation results are value snapshots.
// Dark is the terminal background reported by tea.BackgroundColorMsg; the
// adaptive theme assumes a dark background until that message arrives.
//
// Accent replaces the theme's accent colour when it is not nil: the title,
// panel and modal borders, the active tab, the badge and every highlight
// drawn in the accent take it, so an application can say "you are looking
// at everything" by colour alone (a pin state). Semantic colours (errors)
// and the muted text keep their meaning. The mono theme and NO_COLOR stay
// colourless. Applications set it in code; the appearance verbs leave it.
type Appearance struct {
	Theme   string      `json:"theme"`
	Density string      `json:"density"`
	Dark    bool        `json:"dark"`
	Accent  color.Color `json:"-"`
	// Schemes are the colour schemes beyond the built-in themes, by name:
	// the user's themes.json (Model.Init, themes.reload and config.reload set it) or an
	// application's own (NewSchemeSet). A pointer keeps Appearance
	// comparable.
	Schemes *SchemeSet `json:"-"`
}

// NewAppearance registers appearance operations once per runtime.
func NewAppearance(r *Runtime) *Appearance {
	var a *Appearance
	r.Builtin(func() { a = newAppearance(r) })
	return a
}

func newAppearance(r *Runtime) *Appearance {
	a := &Appearance{Theme: "adaptive", Density: "comfortable", Dark: true}
	c := r.Config()
	c.themeListeners = append(c.themeListeners, func() { a.Schemes = NewSchemeSet(r.Config().Schemes()) })
	r.Register(Operation{Verb: "get", Noun: "ochazuke.appearance", Description: "Get theme and density", Result: "appearance", Run: func(_ *Runtime, _ Context, _ Arguments) (any, error) {
		return *a, nil
	}})
	r.Register(Operation{Verb: "set", Noun: "ochazuke.appearance", Description: "Set theme and density atomically", Parameters: map[string]Parameter{
		"theme":   {Type: "string", Description: "A colour scheme: adaptive, dark, light, mono, or one from themes.json"},
		"density": {Type: "string", Description: "comfortable or compact"},
	}, Result: "appearance", Run: func(_ *Runtime, _ Context, args Arguments) (any, error) {
		next := *a
		if theme, ok := args["theme"].(string); ok {
			if !slices.Contains(a.ThemeNames(), theme) {
				return nil, fmt.Errorf("unknown theme %q: use %s", theme, strings.Join(a.ThemeNames(), ", "))
			}
			next.Theme = theme
		}
		if density, ok := args["density"].(string); ok {
			switch density {
			case "comfortable", "compact":
				next.Density = density
			default:
				return nil, fmt.Errorf("unknown density %q: use comfortable or compact", density)
			}
		}
		*a = next
		return next, nil
	}})
	r.Register(Operation{Verb: "list", Noun: "ochazuke.themes", Description: "List the colour schemes: the built-in themes, then those of themes.json", Result: "string[]", Run: func(_ *Runtime, _ Context, _ Arguments) (any, error) {
		return a.ThemeNames(), nil
	}})
	r.Register(Operation{Verb: "reload", Noun: "ochazuke.themes", Description: "Read themes.json again: a scheme that does not validate is skipped and reported", Result: "problem[]", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		problems := r.reloadThemes()
		if problems == nil {
			problems = []string{}
		}
		return problems, nil
	}})
	r.Register(Operation{Verb: "path", Noun: "ochazuke.themes", Description: "Print the user colour schemes file: themes.json beside settings.json", Result: "path", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		path := r.Config().Path(ThemesFile)
		if path == "" {
			return nil, errors.New("this application has no config directory")
		}
		return path, nil
	}})
	return a
}

// ThemeNames are the schemes Theme can name: the built-in themes, then the
// application's or user's (Schemes) by name.
func (a Appearance) ThemeNames() []string {
	names := slices.Clone(BuiltinThemes)
	return append(names, a.Schemes.Names()...)
}

// Background is the background the appearance draws on, "light" or "dark"
// (an adaptive scheme follows Dark), and whether it draws no colour at all
// (the mono theme or NO_COLOR). A renderer with its own palette, such as
// the markdown package's Glamour styles, picks from it.
func (a Appearance) Background() (background string, plain bool) {
	scheme, plain := a.scheme()
	plain = plain || os.Getenv("NO_COLOR") != ""
	switch scheme.Background {
	case "light", "dark":
		return scheme.Background, plain
	}
	if a.Dark {
		return "dark", plain
	}
	return "light", plain
}

// Styles is a render-time value; it does not retain mutable appearance state.
type Styles struct {
	Title        lipgloss.Style
	Subtitle     lipgloss.Style
	Muted        lipgloss.Style
	Accent       lipgloss.Style
	Error        lipgloss.Style
	Panel        lipgloss.Style
	FocusedPanel lipgloss.Style
	Tab          lipgloss.Style
	ActiveTab    lipgloss.Style
	Badge        lipgloss.Style
	Padding      int
	// Backdrop draws what a modal covers (modal-dim); Unfocused draws a page
	// list without the focus (dim-unfocused). Either is the empty style when
	// its dim is 0. WithDims sets both from percents.
	Backdrop  lipgloss.Style
	Unfocused lipgloss.Style

	text, ground   color.Color         // the scheme's text and surface, the ends of a fade
	accent, border color.Color         // the accent and border roles (CardBorders)
	unfocused      int                 // the dim-unfocused percent WithDims was given
	plain          bool                // mono or NO_COLOR: a fade is the faint attribute
	tones          map[string]toneLook // the scheme's tones resolved (Tone, tone.go)
	selectedBg     string              // tones.selected-background, read by the list that draws (listTones)
}

// Dim defaults, in percent toward the background (the settings modal-dim
// and dim-unfocused, Model.Styles).
const (
	DefaultModalDim     = 35 // about the scheme's muted colour, as the backdrop always was
	DefaultDimUnfocused = 20
	MaxDim              = 90
)

// Fade is a style that carries text percent of the way from the scheme's
// text colour toward its surface, blended in CIELAB (fadeRamp,
// lipgloss.Blend1D); with no colour (mono, NO_COLOR) any
// percent above 0 is the faint attribute. 0 is the empty style.
func (s Styles) Fade(percent int) lipgloss.Style {
	percent = min(percent, MaxDim)
	switch {
	case percent <= 0:
		return lipgloss.NewStyle()
	case s.plain || s.text == nil || s.ground == nil:
		return lipgloss.NewStyle().Faint(true)
	}
	return lipgloss.NewStyle().Foreground(fadeRamp(s.text, s.ground)[percent])
}

// WithDims sets Backdrop and Unfocused from percents: the backdrop is the
// fade plus the faint attribute (the look it always had at the default),
// an unfocused list the fade alone.
func (s Styles) WithDims(modal, unfocused int) Styles {
	s.Backdrop = lipgloss.NewStyle()
	if modal > 0 {
		s.Backdrop = s.Fade(modal).Faint(true)
	}
	s.Unfocused = s.Fade(unfocused)
	s.unfocused = unfocused
	return s
}

// CardBorders are the border styles of a card timeline's three states
// (the selection is the border, never reverse video): selected while the
// timeline has the focus, the accent in bold; selected without the focus,
// halfway from the accent to the border role; every other card, the
// border role faded toward the surface by dim-unfocused. With no colour
// (mono, NO_COLOR) they are bold, plain and faint; the timeline also
// draws the two selected states in heavy lines, so they read without
// any attribute.
func (s Styles) CardBorders() (focused, selected, other lipgloss.Style) {
	focused = lipgloss.NewStyle().Bold(true)
	selected, other = lipgloss.NewStyle(), lipgloss.NewStyle()
	if s.plain || s.accent == nil || s.border == nil || s.ground == nil {
		return focused, selected, other.Faint(true)
	}
	focused = focused.Foreground(s.accent)
	selected = selected.Foreground(lipgloss.Blend1D(3, s.accent, s.border)[1])
	other = other.Foreground(fadeRamp(s.border, s.ground)[min(max(s.unfocused, 0), MaxDim)])
	return focused, selected, other
}

// The dim settings: rows of settings/ochazuke.json.
const (
	ModalDimSetting     = "modal-dim"
	DimUnfocusedSetting = "dim-unfocused"
)

// dimSetting is a dim setting's value, def when it is not declared.
func (r *Runtime) dimSetting(name string, def int) int {
	v, ok := r.Settings().Value(name)
	n, isInt := v.(int)
	if !ok || !isInt {
		return def
	}
	return min(max(n, 0), MaxDim)
}

func validDim(v any) error {
	if n, _ := v.(int); n < 0 || n > MaxDim {
		return fmt.Errorf("must be 0 to %d", MaxDim)
	}
	return nil
}

// fadeSteps is the length of a fade ramp: index p is p percent of the way
// from text to ground (Blend1D's one segment puts step j at j/(steps-1)).
const fadeSteps = 101

type rampKey struct{ text, ground color.RGBA64 }

// fadeRamps caches a ramp per text and surface pair, so Model.Styles,
// which runs per frame and per page list, blends a scheme once. The pairs
// are the schemes in use, a handful.
var fadeRamps sync.Map // rampKey -> []color.Color

// fadeRamp is lipgloss.Blend1D from text to ground in fadeSteps (CIELAB,
// so a fade looks evenly spaced rather than muddy).
func fadeRamp(text, ground color.Color) []color.Color {
	key := rampKey{color.RGBA64Model.Convert(text).(color.RGBA64), color.RGBA64Model.Convert(ground).(color.RGBA64)}
	if ramp, ok := fadeRamps.Load(key); ok {
		return ramp.([]color.Color)
	}
	ramp := lipgloss.Blend1D(fadeSteps, text, ground)
	fadeRamps.Store(key, ramp)
	return ramp
}

// NewStyles keeps layout and focus cues usable without terminal color
// support. Theme names the scheme (a built-in theme or one of Schemes; an
// unknown name draws adaptive); mono and NO_COLOR draw no colour at all.
func NewStyles(a Appearance) Styles {
	scheme, plain := a.scheme()
	plain = plain || os.Getenv("NO_COLOR") != ""
	pick := func(c SchemeColor) color.Color {
		if plain {
			return lipgloss.NoColor{}
		}
		switch scheme.Background {
		case "light":
			return lipgloss.Color(c.Light)
		case "dark":
			return lipgloss.Color(c.Dark)
		default:
			return lipgloss.LightDark(a.Dark)(lipgloss.Color(c.Light), lipgloss.Color(c.Dark))
		}
	}
	text := pick(scheme.Text)
	muted := pick(scheme.Muted)
	accent := pick(scheme.Accent)
	border := pick(scheme.Border)
	title := text
	if a.Accent != nil && !plain {
		accent, border, title = a.Accent, a.Accent, a.Accent
	}
	surface := pick(scheme.Surface)
	errorColor := pick(scheme.Error)
	padding := 1
	if a.Density == "compact" {
		padding = 0
	}
	panel := lipgloss.NewStyle().Foreground(text).Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(padding, 1)
	focusedBorder := lipgloss.RoundedBorder()
	focusedBorder.Top, focusedBorder.Bottom = "━", "━"
	focusedBorder.Left, focusedBorder.Right = "┃", "┃"
	styles := Styles{
		Title:        lipgloss.NewStyle().Bold(true).Foreground(title),
		Subtitle:     lipgloss.NewStyle().Foreground(accent),
		Muted:        lipgloss.NewStyle().Foreground(muted).Faint(true),
		Accent:       lipgloss.NewStyle().Foreground(accent).Bold(true),
		Error:        lipgloss.NewStyle().Foreground(errorColor).Bold(true),
		Panel:        panel,
		FocusedPanel: panel.Border(focusedBorder).BorderForeground(accent),
		Tab:          lipgloss.NewStyle().Foreground(muted).Padding(0, 1),
		ActiveTab:    lipgloss.NewStyle().Foreground(accent).Background(surface).Bold(true).Padding(0, 1).Transform(func(s string) string { return "› " + s }),
		Badge:        lipgloss.NewStyle().Foreground(accent).Background(surface).Padding(0, 1),
		Padding:      padding,
		text:         text,
		ground:       surface,
		accent:       accent,
		border:       border,
		plain:        plain,
		tones:        resolveTones(scheme, plain, pick),
	}
	return styles.WithDims(DefaultModalDim, DefaultDimUnfocused)
}
