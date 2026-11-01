package ochazuke

import (
	"errors"
	"fmt"
	"image/color"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	// AppTones are the application's own tone rows (SetModeTone, SetTone),
	// laid over the built-in tones and under every themes.json entry, so
	// a theme still recolours them. A pointer keeps Appearance comparable.
	AppTones *ToneRows `json:"-"`
}

// ToneRows are tone rows by name an application declares
// (Appearance.AppTones). The zero value and nil hold none.
type ToneRows struct {
	rows       map[string]ToneStyle
	generation uint64
}

// Rows are the rows, a copy.
func (t *ToneRows) Rows() map[string]ToneStyle {
	if t == nil {
		return nil
	}
	return maps.Clone(t.rows)
}

// SetTone declares (or replaces) the application's tone row name, laid
// over the built-in tones and under themes.json (Appearance.AppTones):
// an application's own cell tone, or a chrome row (chrome.rich.hint.key).
func (a *Appearance) SetTone(name string, style ToneStyle) {
	if a.AppTones == nil {
		a.AppTones = &ToneRows{}
	}
	if a.AppTones.rows == nil {
		a.AppTones.rows = map[string]ToneStyle{}
	}
	a.AppTones.rows[name] = style
	a.AppTones.generation++
}

// SetModeTone declares the badge hue of a mode: the tone row
// mode.<mode>, from which the rich and subtle chrome levels derive the
// badge (rich fills it with hue, subtle draws hue on the surface; off
// is unchanged). A themes.json theme's mode.<mode> row recolours it, and
// its chrome.<level>.mode.<mode> row replaces a level's badge whole. A
// mode with no hue row of its own takes the first row of its chain
// (mode.pending for a pending mode, the mode it extends, mode).
func (a *Appearance) SetModeTone(mode string, hue SchemeColor) {
	a.SetTone("mode."+mode, ToneStyle{Fg: hue})
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
	selectionDim   int                 // the dim-unfocused-selection percent (WithSelectionDim): the unfocused bar's fade beyond unfocused
	plain          bool                // mono or NO_COLOR: a fade is the faint attribute
	tones          map[string]toneLook // the scheme's tones resolved (Tone, tone.go)
	selectedBg     string              // tones.selected-background, read by the list that draws (listTones)
	intersect      bool                // list.intersect, read by the list that draws (listTones)
	cellCursor     ansi.Style          // list.cell-cursor's style for a table's current cell (Runtime.cellCursor); nil underlines it
	backdropPlain  bool                // modal-highlights plain: the backdrop drops reverse video and underline
	hideUnfocused  bool                // unfocused-selection hide: a selection without the focus is not drawn (WithUnfocusedSelection)
	chrome         string              // the chrome level (chrome.color), "" before WithChrome (chrome.go)
	appAccent      color.Color         // Appearance.Accent when it draws (not mono, not NO_COLOR)
	modeColor      color.Color         // the mode role: the current mode's badge colour (WithChrome)
}

// Dim defaults, in percent toward the background (the settings modal-dim
// and dim-unfocused, Model.Styles).
const (
	DefaultModalDim     = 35 // about the scheme's muted colour, as the backdrop always was
	DefaultDimUnfocused = 20
	// DefaultDimUnfocusedSelection is how much further than the list the
	// selected row's bar of a list without the focus fades (setting
	// dim-unfocused-selection): at the defaults the bar sits halfway
	// between the text and the background colour.
	DefaultDimUnfocusedSelection = 30
	MaxDim                       = 90
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

// WithSelectionDim sets how many percent further than the list
// (dim-unfocused) the selected row's bar of a list without the focus
// fades toward the background (dim-unfocused-selection). 0 fades the bar
// with the list, as before the setting.
func (s Styles) WithSelectionDim(percent int) Styles {
	s.selectionDim = min(max(percent, 0), MaxDim)
	return s
}

// barPercent is how far the selected row of a list without the focus
// fades toward the background: the list's dim-unfocused plus
// dim-unfocused-selection, at most MaxDim.
func (s *Styles) barPercent() int {
	return min(max(s.unfocused, 0)+s.selectionDim, MaxDim)
}

// fadeBy carries c percent of the way toward the scheme's surface; c
// itself for 0, or with no surface.
func (s *Styles) fadeBy(c color.Color, percent int) color.Color {
	if c == nil || s.ground == nil || percent <= 0 {
		return c
	}
	return fadeRamp(c, s.ground)[min(percent, MaxDim)]
}

// selectionBar is the SGR of the selected row's bar in a list: nil for
// the plain reverse bar (the focused list, dim-unfocused-selection 0, no
// styles, or mono and NO_COLOR, where the list's faint already dims the
// bar), else reverse video with the foreground, which the reverse makes
// the bar's colour, carried barPercent of the way from the scheme's text
// toward its surface. One rule for every list, table and tree (paintLine,
// List.bar).
func (s *Styles) selectionBar(faded bool) ansi.Style {
	if s == nil || !faded || s.selectionDim <= 0 || s.plain || s.text == nil || s.ground == nil {
		return nil
	}
	return selectionSGR().ForegroundColor(s.fadeBy(s.text, s.barPercent()))
}

// selectionSGR is the plain reverse-video SGR, the focused selection's bar
// for code that writes raw sequences (paintLine, highlightRow).
func selectionSGR() ansi.Style {
	var st ansi.Style
	return st.Reverse(true)
}

// Selection is the one selection painter: the style of the selected row,
// cell or item of any component (a list, table, chooser, picker, Doc,
// settings selector, grid, an app's own rows; UX R5.1). Focused, it is
// reverse video. Without the focus the selection stays marked, faded: the
// bar's colour (the foreground, which reverse video makes the bar) is
// carried dim-unfocused plus dim-unfocused-selection percent from the
// scheme's text toward its surface; in mono, NO_COLOR or with no styles
// (a nil receiver) it is the faint reverse bar; under unfocused-selection
// hide it is no style at all. Components and apps never
// call Reverse themselves. A component the page already fades as a whole
// (a page List under dim-unfocused) draws its bar through
// Styles.selectionBar, which leaves the list's fade to dim it when
// dim-unfocused-selection is 0.
func (s *Styles) Selection(focused bool) lipgloss.Style {
	st := lipgloss.NewStyle().Reverse(true)
	if focused {
		return st
	}
	if s != nil && s.hideUnfocused {
		return lipgloss.NewStyle()
	}
	if s == nil || s.plain || s.text == nil || s.ground == nil {
		return st.Faint(true)
	}
	if p := s.barPercent(); p > 0 {
		return st.Foreground(s.fadeBy(s.text, p))
	}
	return st
}

// SelectionStyle is Styles.Selection for code with no Styles at hand (a
// component's colourless defaults, an overlay mark's fallback, a text
// cursor block): reverse video focused, faint reverse without the focus.
func SelectionStyle(focused bool) lipgloss.Style {
	return (*Styles)(nil).Selection(focused)
}

// Selection is the Model's selection painter (Styles.Selection), the
// colourless one before a Model has set its styles. Components and apps
// draw every selection through it (or Styles.Selection), never Reverse.
func (r *Runtime) Selection(focused bool) lipgloss.Style {
	if r != nil {
		if styles := r.Settings().page.styles; styles != nil {
			s := styles()
			return s.Selection(focused)
		}
	}
	return SelectionStyle(focused)
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
	// DimUnfocusedSelectionSetting: percent the selected row's bar of a
	// list without the focus fades beyond dim-unfocused (0: with the list).
	DimUnfocusedSelectionSetting = "dim-unfocused-selection"
	// ModalBackdropHighlightsSetting: keep (the page's reverse video and
	// underline beneath a modal stay, dimmed) or plain (the backdrop is
	// drawn without them).
	ModalBackdropHighlightsSetting = "modal-highlights"
	// UnfocusedSelectionSetting: keep (a component without the focus keeps
	// its selection marked, faded; UX R5.1) or hide (only the focused
	// component draws its selection).
	UnfocusedSelectionSetting = "unfocused-selection"
)

// WithUnfocusedSelection says whether a selection without the focus is
// drawn (true, the default: unfocused-selection keep) or left plain
// (unfocused-selection hide).
func (s Styles) WithUnfocusedSelection(keep bool) Styles {
	s.hideUnfocused = !keep
	return s
}

// WithBackdropHighlights says whether a modal's backdrop keeps the page's
// reverse video and underline (true, the default) or drops them
// (modal-highlights plain).
func (s Styles) WithBackdropHighlights(keep bool) Styles {
	s.backdropPlain = !keep
	return s
}

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
	var appAccent color.Color
	if a.Accent != nil && !plain {
		accent, border, title = a.Accent, a.Accent, a.Accent
		appAccent = a.Accent
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
		tones:        resolveTones(scheme, plain, pick, appAccent),
		appAccent:    appAccent,
		modeColor:    accent,
	}
	return styles.WithDims(DefaultModalDim, DefaultDimUnfocused).WithSelectionDim(DefaultDimUnfocusedSelection)
}
