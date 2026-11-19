package ochazuke

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// modal.mode-border draws a modal's border in the mode's own hue even
// under the application's accent (insert's blue here), and in the
// cooling tone while esc is cooling; off, the border keeps the accent.
func TestModalModeBorderSetting(t *testing.T) {
	t.Parallel()
	accent, insert := "#E8833A", "#7CB7E8"
	border := func(on bool) string {
		r := NewRuntime()
		r.EnsureConfigSettings()
		if err := r.Settings().Set(ModalModeBorderSetting, on); err != nil {
			t.Fatal(err)
		}
		if err := r.Settings().Set(ChromeColorSetting, ChromeRich); err != nil {
			t.Fatal(err)
		}
		m := NewModel(r)
		m.Init()
		m.Width, m.Height = 60, 12
		m.Appearance.Theme, m.Appearance.Accent = "dark", lipgloss.Color(accent)
		if _, err := r.OpenModal("ask", Payload{}, WithInput(NewInput("ask-field", "x", false))); err != nil {
			t.Fatal(err)
		}
		press(t, r, "i")
		for _, line := range strings.Split(m.View().Content, "\n") {
			if strings.Contains(ansi.Strip(line), "╭") {
				return line
			}
		}
		t.Fatal("no modal border drawn")
		return ""
	}
	if got := border(true); !strings.Contains(got, fg(insert)) {
		t.Fatalf("on, insert: border %q lacks the insert hue", got)
	}
	if got := border(false); !strings.Contains(got, fg(accent)) || strings.Contains(got, fg(insert)) {
		t.Fatalf("off: border %q is not the accent", got)
	}
	s := NewStyles(Appearance{Theme: "dark", Accent: lipgloss.Color(accent)}).WithChrome(ChromeRich, []string{"mode.normal", "mode"})
	if cool, calm := s.ModalBorder(true), s.ModalBorder(false); cool == nil || cool == calm {
		t.Fatalf("cooling %v, normal %v", cool, calm)
	}
}
