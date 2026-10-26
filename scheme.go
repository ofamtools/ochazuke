package ochazuke

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ThemesFile is the user colour schemes file in the config directory.
const ThemesFile = "themes.json"

// ThemeSetting is the declared setting that selects the colour scheme.
const ThemeSetting = "theme"

// BuiltinThemes are the schemes every application has: adaptive follows
// the terminal's background, dark and light fix it, mono draws no colour.
var BuiltinThemes = []string{"adaptive", "dark", "light", "mono"}

// SchemeColor is one palette role: Light is drawn on a light background,
// Dark on a dark one. Each is "#RRGGBB", "#RGB" or an ANSI index "0" to
// "255". In themes.json a role is one colour for both, or
// {"light": "...", "dark": "..."}.
type SchemeColor struct {
	Light string `json:"light"`
	Dark  string `json:"dark"`
}

// UnmarshalJSON reads "#hex" (both sides) or {"light", "dark"} (a missing
// side takes the other).
func (c *SchemeColor) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*c = SchemeColor{one, one}
		return nil
	}
	var pair struct {
		Light *string `json:"light"`
		Dark  *string `json:"dark"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pair); err != nil || (pair.Light == nil && pair.Dark == nil) {
		return errors.New(`a colour is "#RRGGBB" or {"light": "#RRGGBB", "dark": "#RRGGBB"}`)
	}
	if pair.Light == nil {
		pair.Light = pair.Dark
	}
	if pair.Dark == nil {
		pair.Dark = pair.Light
	}
	*c = SchemeColor{*pair.Light, *pair.Dark}
	return nil
}

// Scheme is a named colour scheme: the palette roles NewStyles draws
// pages, panels, inputs and modals with. Background is which side of each
// role it draws: "adaptive" (the terminal's, the default), "light" or
// "dark". Text is body text and the title; Muted hints and inactive tabs;
// Accent the active tab, badge, subtitle and focused borders; Border
// unfocused panel borders; Surface the active tab's and badge's
// background; Error errors, kept apart from Accent and Muted. Selection
// stays reverse video in every scheme, so the selected row reads without
// colour.
type Scheme struct {
	Background string      `json:"background,omitempty"`
	Text       SchemeColor `json:"text"`
	Muted      SchemeColor `json:"muted"`
	Accent     SchemeColor `json:"accent"`
	Border     SchemeColor `json:"border"`
	Surface    SchemeColor `json:"surface"`
	Error      SchemeColor `json:"error"`
}

// defaultScheme is the built-in palette; a user scheme's missing role
// takes its colour.
var defaultScheme = Scheme{
	Background: "adaptive",
	Text:       SchemeColor{"#253D39", "#DCE9E3"},
	Muted:      SchemeColor{"#657672", "#93A7A1"},
	Accent:     SchemeColor{"#17695E", "#78BFA5"},
	Border:     SchemeColor{"#A7BAB2", "#465D55"},
	Surface:    SchemeColor{"#EEF3EF", "#172720"},
	Error:      SchemeColor{"#AB3D36", "#E99B8D"},
}

// scheme resolves Theme: plain for mono, a built-in, one of Schemes, else
// adaptive.
func (a Appearance) scheme() (Scheme, bool) {
	switch a.Theme {
	case "mono":
		return defaultScheme, true
	case "dark", "light":
		s := defaultScheme
		s.Background = a.Theme
		return s, false
	}
	if s, ok := a.Schemes.Get(a.Theme); ok {
		return s, false
	}
	return defaultScheme, false
}

// SchemeSet is a read-only set of named schemes; a nil set is empty.
type SchemeSet struct{ byName map[string]Scheme }

// NewSchemeSet holds a copy of schemes.
func NewSchemeSet(schemes map[string]Scheme) *SchemeSet {
	return &SchemeSet{maps.Clone(schemes)}
}

// Get is the scheme of that name.
func (s *SchemeSet) Get(name string) (Scheme, bool) {
	if s == nil {
		return Scheme{}, false
	}
	one, ok := s.byName[name]
	return one, ok
}

// Names are the scheme names, sorted.
func (s *SchemeSet) Names() []string {
	if s == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(s.byName))
}

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

func validColor(c string) bool {
	if hexColor.MatchString(c) {
		return true
	}
	n, err := strconv.Atoi(c)
	return err == nil && n >= 0 && n <= 255 && strconv.Itoa(n) == c
}

// ParseSchemes reads colour schemes in the themes.json shape, an object of
// schemes by name:
//
//	{"kombu": {"background": "dark", "accent": "#C9A227",
//	           "error": {"light": "#B3261E", "dark": "#FF8A80"}}}
//
// A role left out takes the built-in palette's. A scheme that does not
// validate (an unknown role, a bad colour or background, a built-in's
// name, an error colour equal to the accent or muted one) is skipped and
// reported; a file that is not a JSON object yields no schemes.
func ParseSchemes(data []byte) (map[string]Scheme, []string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		if err == nil {
			err = errors.New("null")
		}
		return nil, []string{ThemesFile + ": not a JSON object of schemes, using the built-in themes: " + err.Error()}
	}
	out := map[string]Scheme{}
	var problems []string
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		s, err := parseScheme(name, raw[name])
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %s: %v (skipped)", ThemesFile, name, err))
			continue
		}
		out[name] = s
	}
	return out, problems
}

func parseScheme(name string, data json.RawMessage) (Scheme, error) {
	if name == "" || strings.ContainsAny(name, " \t\n") {
		return Scheme{}, errors.New("a scheme name is one word")
	}
	if slices.Contains(BuiltinThemes, name) {
		return Scheme{}, errors.New("is a built-in theme")
	}
	var roles map[string]json.RawMessage
	if err := json.Unmarshal(data, &roles); err != nil || roles == nil {
		return Scheme{}, errors.New("a scheme is an object of roles")
	}
	s := defaultScheme
	fields := map[string]*SchemeColor{"text": &s.Text, "muted": &s.Muted, "accent": &s.Accent, "border": &s.Border, "surface": &s.Surface, "error": &s.Error}
	for _, role := range slices.Sorted(maps.Keys(roles)) {
		value := roles[role]
		if role == "background" {
			if err := json.Unmarshal(value, &s.Background); err != nil || !slices.Contains([]string{"adaptive", "light", "dark"}, s.Background) {
				return Scheme{}, errors.New("background is adaptive, light or dark")
			}
			continue
		}
		field, ok := fields[role]
		if !ok {
			return Scheme{}, fmt.Errorf("unknown role %q: use background, text, muted, accent, border, surface, error", role)
		}
		if err := json.Unmarshal(value, field); err != nil {
			return Scheme{}, fmt.Errorf("%s: %v", role, err)
		}
		for _, c := range []string{field.Light, field.Dark} {
			if !validColor(c) {
				return Scheme{}, fmt.Errorf("%s: %q is not #RRGGBB, #RGB or an ANSI index 0-255", role, c)
			}
		}
	}
	for _, other := range []struct {
		name string
		c    SchemeColor
	}{{"accent", s.Accent}, {"muted", s.Muted}} {
		if strings.EqualFold(s.Error.Light, other.c.Light) || strings.EqualFold(s.Error.Dark, other.c.Dark) {
			return Scheme{}, fmt.Errorf("error must differ from %s, so errors stay distinct", other.name)
		}
	}
	return s, nil
}

// Schemes are the user's colour schemes from themes.json, by name, as the
// last read found them (EnsureConfigSettings, themes.reload).
func (c *Config) Schemes() map[string]Scheme { return maps.Clone(c.schemes) }

// ReloadThemes reads themes.json again: the schemes that validate replace
// the user's, every problem is reported (and the last read's withdrawn),
// and the theme setting's choices follow when the framework declared it.
// A missing file is no schemes and no problem.
func (r *Runtime) ReloadThemes() []string {
	c := r.Config()
	if c.Withdraw != nil {
		for _, p := range c.themeProblems {
			c.Withdraw(p)
		}
	}
	c.themesLoaded = true
	c.themesPath = c.Path(ThemesFile)
	c.schemes, c.themeProblems = nil, nil
	if c.themesPath != "" {
		data, err := os.ReadFile(c.themesPath)
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			c.themeProblems = []string{ThemesFile + ": " + err.Error()}
		default:
			c.schemes, c.themeProblems = ParseSchemes(data)
		}
	}
	for _, p := range c.themeProblems {
		c.report(p)
	}
	if c.themeSetting {
		r.Settings().setEnum(ThemeSetting, c.themeNames())
	}
	return slices.Clone(c.themeProblems)
}

func (c *Config) themeNames() []string {
	return append(slices.Clone(BuiltinThemes), slices.Sorted(maps.Keys(c.schemes))...)
}

// ensureThemeSetting reads themes.json when the config directory changed
// since the last read, and declares the theme setting unless the
// application declared one.
func (r *Runtime) ensureThemeSetting() {
	c := r.Config()
	if !c.themesLoaded || c.themesPath != c.Path(ThemesFile) {
		r.ReloadThemes()
	}
	if r.Settings().find(ThemeSetting) == nil {
		if err := r.DeclareSetting(Setting{Name: ThemeSetting, Type: SettingString, Default: "adaptive", Description: "Colour scheme (themes.json adds more)", Enum: c.themeNames()}); err == nil {
			c.themeSetting = true
		}
	}
}

// FrameworkTheme reports whether the theme setting is the framework's
// (the Model applies it to Appearance.Theme) rather than the
// application's own.
func (c *Config) FrameworkTheme() bool { return c.themeSetting }

// setEnum replaces a string setting's choices; a value the file holds is
// checked against them again.
func (s *Settings) setEnum(name string, enum []string) {
	one := s.find(name)
	if one == nil || slices.Equal(one.Enum, enum) {
		return
	}
	old := s.current(*one)
	one.Enum = slices.Clone(enum)
	if !s.loaded {
		return
	}
	delete(s.values, name)
	s.withdrawFor(name)
	s.parse(*one)
	s.changed(name, old)
}
