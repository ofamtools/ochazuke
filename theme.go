package ochazuke

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"slices"
	"strings"

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
	// the user's themes.json (Model.Init and themes.reload set it) or an
	// application's own (NewSchemeSet). A pointer keeps Appearance
	// comparable.
	Schemes *SchemeSet `json:"-"`
}

// NewAppearance registers appearance operations once per runtime.
func NewAppearance(r *Runtime) *Appearance {
	a := &Appearance{Theme: "adaptive", Density: "comfortable", Dark: true}
	r.Register(Operation{Verb: "get", Noun: "appearance", Description: "Get theme and density", Result: "appearance", Run: func(_ *Runtime, _ Context, _ Arguments) (any, error) {
		return *a, nil
	}})
	r.Register(Operation{Verb: "set", Noun: "appearance", Description: "Set theme and density atomically", Parameters: map[string]Parameter{
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
	r.Register(Operation{Verb: "list", Noun: "themes", Description: "List the colour schemes: the built-in themes, then those of themes.json", Result: "string[]", Run: func(_ *Runtime, _ Context, _ Arguments) (any, error) {
		return a.ThemeNames(), nil
	}})
	r.Register(Operation{Verb: "reload", Noun: "themes", Description: "Read themes.json again: a scheme that does not validate is skipped and reported", Result: "problem[]", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		problems := r.ReloadThemes()
		a.Schemes = NewSchemeSet(r.Config().Schemes())
		if problems == nil {
			problems = []string{}
		}
		return problems, nil
	}})
	r.Register(Operation{Verb: "path", Noun: "themes", Description: "Print the user colour schemes file: themes.json beside settings.json", Result: "path", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
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
	return Styles{
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
	}
}
