package ochazuke

import (
	"fmt"
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
)

// Appearance is event-loop-owned configuration. Operation results are value snapshots.
// Dark is the terminal background reported by tea.BackgroundColorMsg; the
// adaptive theme assumes a dark background until that message arrives.
type Appearance struct {
	Theme   string `json:"theme"`
	Density string `json:"density"`
	Dark    bool   `json:"dark"`
}

// NewAppearance registers appearance operations once per runtime.
func NewAppearance(r *Runtime) *Appearance {
	a := &Appearance{Theme: "adaptive", Density: "comfortable", Dark: true}
	r.Register(Operation{Verb: "get", Noun: "appearance", Description: "Get theme and density", Result: "appearance", Run: func(_ *Runtime, _ Context, _ Arguments) (any, error) {
		return *a, nil
	}})
	r.Register(Operation{Verb: "set", Noun: "appearance", Description: "Set theme and density atomically", Parameters: map[string]Parameter{
		"theme":   {Type: "string", Description: "adaptive, dark, light, or mono"},
		"density": {Type: "string", Description: "comfortable or compact"},
	}, Result: "appearance", Run: func(_ *Runtime, _ Context, args Arguments) (any, error) {
		next := *a
		if theme, ok := args["theme"].(string); ok {
			switch theme {
			case "adaptive", "dark", "light", "mono":
				next.Theme = theme
			default:
				return nil, fmt.Errorf("unknown theme %q: use adaptive, dark, light, or mono", theme)
			}
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
	r.Register(Operation{Verb: "list", Noun: "themes", Description: "List available themes", Result: "string[]", Run: func(_ *Runtime, _ Context, _ Arguments) (any, error) {
		return []string{"adaptive", "dark", "light", "mono"}, nil
	}})
	return a
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

// NewStyles keeps layout and focus cues usable without terminal color support.
func NewStyles(a Appearance) Styles {
	plain := a.Theme == "mono" || os.Getenv("NO_COLOR") != ""
	color := func(light, dark string) color.Color {
		if plain {
			return lipgloss.NoColor{}
		}
		switch a.Theme {
		case "light":
			return lipgloss.Color(light)
		case "dark":
			return lipgloss.Color(dark)
		default:
			return lipgloss.LightDark(a.Dark)(lipgloss.Color(light), lipgloss.Color(dark))
		}
	}
	text := color("#253D39", "#DCE9E3")
	muted := color("#657672", "#93A7A1")
	accent := color("#17695E", "#78BFA5")
	border := color("#A7BAB2", "#465D55")
	surface := color("#EEF3EF", "#172720")
	padding := 1
	if a.Density == "compact" {
		padding = 0
	}
	panel := lipgloss.NewStyle().Foreground(text).Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(padding, 1)
	focusedBorder := lipgloss.RoundedBorder()
	focusedBorder.Top, focusedBorder.Bottom = "━", "━"
	focusedBorder.Left, focusedBorder.Right = "┃", "┃"
	return Styles{
		Title:        lipgloss.NewStyle().Bold(true).Foreground(text),
		Subtitle:     lipgloss.NewStyle().Foreground(accent),
		Muted:        lipgloss.NewStyle().Foreground(muted).Faint(true),
		Accent:       lipgloss.NewStyle().Foreground(accent).Bold(true),
		Error:        lipgloss.NewStyle().Foreground(color("#AB3D36", "#E99B8D")).Bold(true),
		Panel:        panel,
		FocusedPanel: panel.Border(focusedBorder).BorderForeground(accent),
		Tab:          lipgloss.NewStyle().Foreground(muted).Padding(0, 1),
		ActiveTab:    lipgloss.NewStyle().Foreground(accent).Background(surface).Bold(true).Padding(0, 1).Transform(func(s string) string { return "› " + s }),
		Badge:        lipgloss.NewStyle().Foreground(accent).Background(surface).Padding(0, 1),
		Padding:      padding,
	}
}
