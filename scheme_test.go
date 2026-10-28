package ochazuke

import (
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func sameColor(a, b color.Color) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

// A scheme names roles; a role left out keeps the built-in palette's, a
// single colour is both sides, and a pair splits them.
func TestParseSchemesFillsRolesAndValidates(t *testing.T) {
	schemes, problems := ParseSchemes([]byte(`{
		"kombu": {"background": "dark", "accent": "#C9A227", "error": {"light": "#B3261E", "dark": "#FF6F61"}},
		"ansi": {"accent": "6", "muted": {"dark": "8"}},
		"dark": {"accent": "#111111"},
		"odd": {"accent": "#C9A227", "glow": "#FFFFFF"},
		"bad": {"accent": "teal"},
		"side": {"background": "sepia"},
		"clash": {"accent": "#C9A227", "error": "#C9A227"},
		"hush": {"muted": "#777777", "error": {"light": "#777777", "dark": "#FF0000"}}
	}`))
	k, ok := schemes["kombu"]
	if !ok || k.Background != "dark" || k.Accent != (SchemeColor{"#C9A227", "#C9A227"}) || k.Error != (SchemeColor{"#B3261E", "#FF6F61"}) || k.Text != defaultScheme.Text {
		t.Fatalf("kombu: %+v", k)
	}
	if a := schemes["ansi"]; a.Accent.Dark != "6" || a.Muted != (SchemeColor{"8", "8"}) || a.Background != "adaptive" {
		t.Fatalf("ansi: %+v", a)
	}
	if len(schemes) != 2 {
		t.Fatalf("schemes: %v", schemes)
	}
	want := []string{
		`themes.json: bad: accent: "teal" is not #RRGGBB, #RGB or an ANSI index 0-255 (skipped)`,
		"themes.json: clash: error must differ from accent, so errors stay distinct (skipped)",
		"themes.json: dark: is a built-in theme: it takes only tones (skipped)",
		"themes.json: hush: error must differ from muted, so errors stay distinct (skipped)",
		`themes.json: odd: unknown role "glow": use background, text, muted, accent, border, surface, error, tones (skipped)`,
		"themes.json: side: background is adaptive, light or dark (skipped)",
	}
	if !slices.Equal(problems, want) {
		t.Fatalf("problems:\n%s", strings.Join(problems, "\n"))
	}
	if s, p := ParseSchemes([]byte(`[1]`)); s != nil || len(p) != 1 || !strings.Contains(p[0], "not a JSON object") {
		t.Fatalf("array: %v %v", s, p)
	}
}

// NewStyles draws a user scheme's roles; the accent override goes on top
// and the error colour keeps its own; mono and NO_COLOR draw none; an
// unknown name draws adaptive.
func TestNewStylesDrawsSchemes(t *testing.T) {
	schemes, _ := ParseSchemes([]byte(`{"kombu": {"background": "light", "accent": {"light": "#8A6D0B", "dark": "#C9A227"}, "error": "#FF6F61"}}`))
	set := NewSchemeSet(schemes)
	s := NewStyles(Appearance{Theme: "kombu", Dark: true, Schemes: set})
	if !sameColor(s.ActiveTab.GetForeground(), lipgloss.Color("#8A6D0B")) || !sameColor(s.Error.GetForeground(), lipgloss.Color("#FF6F61")) {
		t.Fatalf("kombu light side: tab %v error %v", s.ActiveTab.GetForeground(), s.Error.GetForeground())
	}
	if !sameColor(s.Muted.GetForeground(), lipgloss.Color(defaultScheme.Muted.Light)) {
		t.Fatalf("a role left out: %v", s.Muted.GetForeground())
	}
	orange := lipgloss.Color("#E8833A")
	over := NewStyles(Appearance{Theme: "kombu", Schemes: set, Accent: orange})
	if !sameColor(over.ActiveTab.GetForeground(), orange) || !sameColor(over.Error.GetForeground(), lipgloss.Color("#FF6F61")) {
		t.Fatal("the accent override on a scheme")
	}
	unknown := NewStyles(Appearance{Theme: "gone", Dark: true, Schemes: set})
	if !sameColor(unknown.ActiveTab.GetForeground(), lipgloss.Color(defaultScheme.Accent.Dark)) {
		t.Fatalf("unknown theme: %v", unknown.ActiveTab.GetForeground())
	}
	t.Setenv("NO_COLOR", "1")
	plain := NewStyles(Appearance{Theme: "kombu", Schemes: set, Accent: orange})
	for _, c := range []color.Color{plain.ActiveTab.GetForeground(), plain.Error.GetForeground(), plain.Panel.GetBorderTopForeground()} {
		if _, ok := c.(lipgloss.NoColor); !ok {
			t.Fatalf("NO_COLOR drew %v", c)
		}
	}
}

func themeModel(t *testing.T, themes, settings string) *Model {
	t.Helper()
	dir := t.TempDir()
	if themes != "" {
		if err := os.WriteFile(filepath.Join(dir, ThemesFile), []byte(themes), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := NewRuntime()
	r.Config().Dir = dir
	m := NewModel(r)
	m.Init()
	return m
}

// The framework declares theme with the built-in themes and themes.json's
// as its choices; settings.json selects one at start, settings.set and
// themes.list follow it.
func TestThemeSettingSelectsUserScheme(t *testing.T) {
	m := themeModel(t, `{"kombu": {"accent": "#C9A227"}}`, `{"theme": "kombu"}`)
	r := m.Runtime
	if !r.Config().FrameworkTheme() || m.Appearance.Theme != "kombu" {
		t.Fatalf("theme %q framework %v notices %q", m.Appearance.Theme, r.Config().FrameworkTheme(), m.Notices)
	}
	v, err := r.Settings().Get(ThemeSetting)
	if err != nil || !slices.Equal(v.Enum, []string{"adaptive", "dark", "light", "mono", "kombu"}) {
		t.Fatalf("enum %v %v", v.Enum, err)
	}
	if got, _ := r.Execute(Request{Verb: "list", Noun: "ochazuke.themes"}); !slices.Equal(got.([]string), v.Enum) {
		t.Fatalf("themes.list %v", got)
	}
	if !sameColor(NewStyles(*m.Appearance).ActiveTab.GetForeground(), lipgloss.Color("#C9A227")) {
		t.Fatal("kombu is not drawn")
	}
	if _, err := r.Execute(Request{Verb: "set", Noun: "ochazuke.settings", Args: Arguments{"name": ThemeSetting, "value": "light"}}); err != nil || m.Appearance.Theme != "light" {
		t.Fatalf("settings.set: %q %v", m.Appearance.Theme, err)
	}
	if _, err := r.Execute(Request{Verb: "set", Noun: "ochazuke.appearance", Args: Arguments{"theme": "nope"}}); err == nil || !strings.Contains(err.Error(), "kombu") {
		t.Fatalf("appearance.set unknown: %v", err)
	}
}

// A malformed scheme is a notice; a theme setting naming it reads the
// default, so the screen falls back to adaptive. Fixing the file and
// themes.reload withdraws the notice and offers the scheme.
func TestMalformedSchemeFallsBack(t *testing.T) {
	m := themeModel(t, `{"kombu": {"accent": "teal"}}`, `{"theme": "kombu"}`)
	r := m.Runtime
	if m.Appearance.Theme != "adaptive" {
		t.Fatalf("theme %q", m.Appearance.Theme)
	}
	joined := strings.Join(m.Notices, "\n")
	if !strings.Contains(joined, `themes.json: kombu: accent: "teal"`) || !strings.Contains(joined, "settings.json: theme: must be one of") {
		t.Fatalf("notices: %q", m.Notices)
	}
	if err := os.WriteFile(r.Config().Path(ThemesFile), []byte(`{"kombu": {"accent": "#C9A227"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := r.Execute(Request{Verb: "reload", Noun: "ochazuke.themes"})
	if err != nil || len(got.([]string)) != 0 {
		t.Fatalf("themes.reload %v %v", got, err)
	}
	if strings.Contains(strings.Join(m.Notices, "\n"), "themes.json") || m.Appearance.Theme != "kombu" {
		t.Fatalf("after reload: theme %q notices %q", m.Appearance.Theme, m.Notices)
	}
}

// An application's own theme setting stays the application's: the
// framework neither declares nor applies one.
func TestApplicationThemeSettingWins(t *testing.T) {
	r := NewRuntime()
	r.Config().Dir = t.TempDir()
	if err := r.DeclareSetting(Setting{Name: ThemeSetting, Type: SettingString, Default: "dark", Enum: []string{"dark", "light"}}); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Init()
	if r.Config().FrameworkTheme() {
		t.Fatal("the framework took the application's theme setting")
	}
	if err := r.Settings().Set(ThemeSetting, "light"); err != nil || m.Appearance.Theme != "adaptive" {
		t.Fatalf("the framework applied the application's setting: %q %v", m.Appearance.Theme, err)
	}
}
