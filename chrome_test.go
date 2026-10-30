package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// off is data, not a branch: no chrome.off.* row is built in, so every
// role falls back to the look before the levels, and a panel, a toned
// one too, draws byte for byte as with the bare styles.
func TestChromeOffDrawsAsBefore(t *testing.T) {
	for name := range defaultTones {
		if strings.HasPrefix(name, "chrome.off.") {
			t.Fatalf("built-in row %s: off has no rows", name)
		}
	}
	for _, theme := range []string{"dark", "light", "adaptive", "mono"} {
		bare := NewStyles(Appearance{Theme: theme, Dark: true})
		off := bare.WithChrome(ChromeOff, []string{"mode.insert", "mode"})
		for _, focused := range []bool{false, true} {
			want := Layout{Styles: bare}.Panel("pane", "body", 30, 5, focused)
			got := Layout{Styles: off}.WithPanelTone(ToneNeedsInput).Panel("pane", "body", 30, 5, focused)
			if got != want {
				t.Errorf("%s focused=%v: off draws\n%q\nwant\n%q", theme, focused, got, want)
			}
		}
		if off.Badge.Render("NORMAL") != bare.Badge.Render("NORMAL") {
			t.Errorf("%s: off changes the badge", theme)
		}
		if _, ok := off.hintLine("", []HintCell{{Key: "q", Text: "app.quit"}}, 40); ok {
			t.Errorf("%s: off draws a coloured hint line", theme)
		}
	}
}

func panelText(s Styles, tone string, focused bool) string {
	return Layout{Styles: s}.WithPanelTone(tone).Panel("pane", "body", 30, 5, focused)
}

// A toned panel's border is its level's border.<tone> row: rich colours
// every agent state, subtle only the alert ones; a focused toned panel
// keeps its heavy lines, bold.
func TestPanelToneByLevel(t *testing.T) {
	dark := NewStyles(Appearance{Theme: "dark"})
	rich, subtle := dark.WithChrome(ChromeRich, nil), dark.WithChrome(ChromeSubtle, nil)
	cases := []struct {
		s       Styles
		tone    string
		focused bool
		want    string
		not     []string
	}{
		{rich, ToneWorking, false, "#7CB7E8", nil},
		{rich, ToneNeedsInput, false, "#E3B341", nil},
		{rich, ToneDone, false, "#8BC48A", nil},
		{rich, "", false, "#4F8F7A", nil},       // the rich plain border
		{rich, ToneIdle, false, "#4F8F7A", nil}, // no row: the plain border
		{rich, "", true, "#78BFA5", []string{"#4F8F7A"}},
		{subtle, ToneWorking, false, "#465D55", []string{"#7CB7E8"}},
		{subtle, ToneNeedsInput, false, "#E3B341", nil},
		{subtle, "", true, "#78BFA5", nil},
	}
	for _, c := range cases {
		got := panelText(c.s, c.tone, c.focused)
		if !strings.Contains(got, fg(c.want)) {
			t.Errorf("%s %q focused=%v: border lacks %s:\n%q", c.s.chrome, c.tone, c.focused, c.want, got)
		}
		for _, hex := range c.not {
			if strings.Contains(got, fg(hex)) {
				t.Errorf("%s %q focused=%v: draws %s", c.s.chrome, c.tone, c.focused, hex)
			}
		}
	}
	focused := panelText(rich, ToneWorking, true)
	if !strings.Contains(focused, "1;"+fg("#7CB7E8")) || !strings.Contains(ansi.Strip(focused), "━") {
		t.Errorf("a focused toned panel is bold heavy lines in its tone:\n%q", focused)
	}
	// The text never changes for colour.
	for _, tone := range []string{"", ToneWorking, ToneNeedsInput} {
		if ansi.Strip(panelText(rich, tone, false)) != ansi.Strip(panelText(NewStyles(Appearance{Theme: "dark"}), "", false)) {
			t.Errorf("tone %q changes the panel's text", tone)
		}
	}
}

// On a border a pane's state row wins over an application's accent, which
// wins over the mode tint and the plain rich border (owner question 4).
func TestChromeAccentPrecedence(t *testing.T) {
	s := NewStyles(Appearance{Theme: "dark", Accent: lipgloss.Color("#E8833A")}).WithChrome(ChromeRich, []string{"mode.insert", "mode"})
	if got := panelText(s, ToneNeedsInput, true); !strings.Contains(got, fg("#E3B341")) {
		t.Errorf("needs input under the accent: %q", got)
	}
	for _, focused := range []bool{false, true} {
		got := panelText(s, "", focused)
		if !strings.Contains(got, fg("#E8833A")) || strings.Contains(got, fg("#4F8F7A")) || strings.Contains(got, fg("#7CB7E8")) {
			t.Errorf("focused=%v: an untoned border takes the accent, not the mode or the rich border: %q", focused, got)
		}
	}
	// The mode role is the accent too: a modal's border.
	if got := s.Chrome("border.modal", lipgloss.NewStyle()).GetForeground(); got != lipgloss.Color("#E8833A") {
		t.Errorf("border.modal under the accent: %v", got)
	}
}

// The badge's role chain: mode.<name>, mode.pending for a pending mode,
// mode.<extends>, mode.
func TestModeRoles(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	cases := []struct {
		mode ModeState
		want []string
	}{
		{ModeState{Name: "normal"}, []string{"mode.normal", "mode"}},
		{ModeState{Name: "visual-line"}, []string{"mode.visual-line", "mode.normal", "mode"}},
		{ModeState{Name: "operator_pending"}, []string{"mode.operator_pending", "mode.pending", "mode"}},
		{ModeState{Name: "normal g", Payload: Payload{Prefix: "g"}}, []string{"mode.normal", "mode"}},
	}
	for _, c := range cases {
		r.Mode = c.mode
		if got := r.modeRoles(); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.mode.Name, got, c.want)
		}
	}
	s := NewStyles(Appearance{Theme: "dark"})
	pending := s.WithChrome(ChromeRich, []string{"mode.no-such", "mode.pending", "mode"})
	if pending.Badge.GetBackground() != lipgloss.Color("#E3B341") || pending.modeColor != lipgloss.Color("#E3B341") {
		t.Errorf("a pending mode's badge: bg %v, mode %v", pending.Badge.GetBackground(), pending.modeColor)
	}
	if got := pending.Chrome("border.focused", lipgloss.NewStyle()).GetForeground(); got != lipgloss.Color("#E3B341") {
		t.Errorf("the focused border follows the mode: %v", got)
	}
	unknown := s.WithChrome(ChromeRich, []string{"mode.no-such", "mode"})
	if unknown.Badge.GetBackground() != lipgloss.Color("#78BFA5") {
		t.Errorf("a mode with no row takes the mode row: %v", unknown.Badge.GetBackground())
	}
	if pending.Badge.GetPaddingLeft() != 1 || pending.Badge.GetPaddingRight() != 1 {
		t.Error("a row keeps the badge's padding")
	}
}

// themes.json reaches every chrome row: an entry for a built-in theme
// replaces one (rich draws done panels in the border role again) and a
// row the defaults lack adds a role (subtle's working border).
func TestChromeRowsFromThemes(t *testing.T) {
	over := map[string]ToneStyle{
		"chrome.rich.border.state.done":      {Fg: SchemeColor{"border", "border"}},
		"chrome.subtle.border.state.working": {Fg: SchemeColor{"#5FAFD7", "#5FAFD7"}, Bold: true},
	}
	a := Appearance{Theme: "dark", Schemes: NewSchemeSet(map[string]Scheme{"adaptive": {builtin: true, Tones: over}})}
	s := NewStyles(a)
	if got := panelText(s.WithChrome(ChromeRich, nil), ToneDone, false); !strings.Contains(got, fg("#465D55")) || strings.Contains(got, fg("#8BC48A")) {
		t.Errorf("rich done from themes.json: %q", got)
	}
	if got := panelText(s.WithChrome(ChromeSubtle, nil), ToneWorking, false); !strings.Contains(got, "1;"+fg("#5FAFD7")) {
		t.Errorf("subtle working from themes.json: %q", got)
	}
}

// The hint cells join to BindingHints' text, and the coloured line reads
// as ansi.Truncate cuts the plain one, at every width.
func TestHintCells(t *testing.T) {
	m := hintFixture(t)
	cells := HintCells(m.Runtime, m.Runtime.Focus)
	if len(cells) == 0 {
		t.Fatal("no hints")
	}
	if got, want := JoinHints(cells), BindingHints(m.Runtime, m.Runtime.Focus); got != want {
		t.Fatalf("JoinHints %q, BindingHints %q", got, want)
	}
	s := NewStyles(Appearance{Theme: "dark"}).WithChrome(ChromeRich, nil)
	for _, lead := range []string{"", "Notice: hi"} {
		plain := JoinHints(cells)
		if lead != "" {
			plain = lead + hintSep + plain
		}
		for w := 0; w <= ansi.StringWidth(plain)+2; w++ {
			line, ok := s.hintLine(lead, cells, w)
			if !ok {
				t.Fatal("rich has hint rows")
			}
			if got, want := ansi.Strip(line), ansi.Truncate(plain, w, "…"); got != want {
				t.Fatalf("lead %q width %d: %q, want %q", lead, w, got, want)
			}
		}
	}
	line, _ := s.hintLine("", cells, 200)
	if !strings.Contains(line, "1;"+fg("#78BFA5")+"m"+cells[0].Key) {
		t.Errorf("rich draws the keys in the accent, bold: %q", line)
	}
}

// hintFixture is a page with one list, focused, its rows of the demo noun.
func hintFixture(t *testing.T) *Model {
	t.Helper()
	m, _ := toneFixture(t, "dark", nil, []ListItem{{ID: "a", Label: "a", Noun: "row"}})
	return m
}

// The model draws in the chrome.color level and the current mode.
func TestModelStylesFollowTheSetting(t *testing.T) {
	m := hintFixture(t)
	if got := m.Styles().ChromeLevel(); got != ChromeRich {
		t.Fatalf("default level %q, want rich", got)
	}
	for _, level := range ChromeLevels {
		if err := m.Runtime.Settings().Set(ChromeColorSetting, level); err != nil {
			t.Fatal(err)
		}
		if got := m.Styles().ChromeLevel(); got != level {
			t.Errorf("level %q, want %q", got, level)
		}
	}
	if err := m.Runtime.Settings().Set(ChromeColorSetting, "loud"); err == nil {
		t.Error("chrome.color takes only off, subtle and rich")
	}
}
