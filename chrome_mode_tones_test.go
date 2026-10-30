package ochazuke

import (
	"strings"
	"testing"
)

// A mode's badge hue is a tone row (mode.<name>): the built-in hues are
// default rows, an application declares its own (SetModeTone), a
// themes.json theme recolours either, and both levels derive their badge
// from the hue while off stays as before.
func TestChromeModeTonesDeclared(t *testing.T) {
	orange := SchemeColor{"#984410", "#F0A060"}
	a := Appearance{Theme: "dark", Dark: true}
	a.SetModeTone("jq", orange)
	roles := []string{"mode.jq", "mode"}
	badge := func(a Appearance, level string) string {
		return NewStyles(a).WithChrome(level, roles).Badge.Render("JQ")
	}
	if got := badge(a, ChromeRich); !strings.Contains(got, "48;2;240;160;96") {
		t.Errorf("rich: app hue does not fill the badge: %q", got)
	}
	if got := badge(a, ChromeSubtle); !strings.Contains(got, "38;2;240;160;96") {
		t.Errorf("subtle: app hue is not the badge ink: %q", got)
	}
	bare := Appearance{Theme: "dark", Dark: true}
	if badge(a, ChromeOff) != badge(bare, ChromeOff) {
		t.Errorf("off: an app hue changed the badge")
	}
	// A themes.json theme's hue row recolours the app's, and a theme's
	// own chrome row replaces a level's badge whole.
	over := map[string]ToneStyle{
		"mode.jq":                 {Fg: SchemeColor{"#112233", "#112233"}},
		"chrome.subtle.mode.jq":   {Fg: SchemeColor{"#445566", "#445566"}},
		"mode.insert":             {Fg: SchemeColor{"#778899", "#778899"}},
		"chrome.rich.mode.normal": {Fg: SchemeColor{"#010203", "#010203"}},
	}
	themed := a
	themed.Schemes = NewSchemeSet(map[string]Scheme{"adaptive": {builtin: true, Tones: over}})
	if got := badge(themed, ChromeRich); !strings.Contains(got, "48;2;17;34;51") {
		t.Errorf("rich: the theme's hue row does not win over the app's: %q", got)
	}
	if got := badge(themed, ChromeSubtle); !strings.Contains(got, "38;2;68;85;102") {
		t.Errorf("subtle: the theme's chrome row does not win: %q", got)
	}
	insert := NewStyles(themed).WithChrome(ChromeRich, []string{"mode.insert", "mode"}).Badge.Render("INSERT")
	if !strings.Contains(insert, "48;2;119;136;153") {
		t.Errorf("a built-in mode's hue is not recoloured by the theme: %q", insert)
	}
	// A user scheme: the app's row sits under the scheme's own.
	schemes, problems := ParseSchemes([]byte(`{"mine": {"accent": "#336699", "tones": {"mode.other": "#ABCDEF"}}}`))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	user := Appearance{Theme: "mine", Dark: true, Schemes: NewSchemeSet(schemes), AppTones: a.AppTones}
	if got := badge(user, ChromeRich); !strings.Contains(got, "48;2;240;160;96") {
		t.Errorf("user scheme: the app's hue row is missing: %q", got)
	}
	// Built-in hue rows are tones like any other.
	if _, ok := DefaultTones()["mode.insert"]; !ok {
		t.Error("mode.insert is not a built-in tone row")
	}
	if _, ok := DefaultTones()["chrome.rich.mode.insert"]; ok {
		t.Error("the badge rows are derived, not built in")
	}
}
