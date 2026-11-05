package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDetectGlyphEnv(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"unknown", nil, "unicode"},
		{"utf8", map[string]string{"LANG": "en_US.UTF-8"}, "unicode"},
		{"utf8 variant", map[string]string{"LANG": "C.utf8"}, "unicode"},
		{"C", map[string]string{"LANG": "C"}, "ascii"},
		{"POSIX", map[string]string{"LC_ALL": "POSIX"}, "ascii"},
		{"precedence", map[string]string{"LC_ALL": "C", "LANG": "en_US.UTF-8"}, "ascii"},
		{"ctype", map[string]string{"LC_CTYPE": "en_US.UTF-8", "LANG": "C"}, "unicode"},
		{"linux", map[string]string{"LANG": "en_US.UTF-8", "TERM": "linux"}, "ascii"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, reason := DetectGlyphEnv(func(k string) string { return c.env[k] })
			if got != c.want || reason == "" {
				t.Fatalf("got %q (%s), want %q", got, reason, c.want)
			}
		})
	}
}

func TestASCIIFrameCells(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ input, want string }{
		{"╭─╮┏━┓│┃", "+-++=+||"},
		{"◆●○›‹▾▸▲▼·…✓✗📌🔒", "**o><v>^v.~xx* * "},
		{"1–6 / 10 • ◦ ▌ — × ↵", "1-6 / 10 * o | - x <"},
		{"界 é e\u0301 👩‍💻", "?? ? e ??"},
		{"\x1b[31m◆\x1b[0m", "\x1b[31m*\x1b[0m"},
		{"plain\ntext", "plain\ntext"},
	} {
		got := asciiFrame(c.input)
		if got != c.want {
			t.Errorf("%q: got %q, want %q", c.input, got, c.want)
		}
		if ansi.StringWidth(got) != ansi.StringWidth(c.input) {
			t.Errorf("cell width changed: %q to %q", c.input, got)
		}
	}
}

func TestGlyphRoleWidths(t *testing.T) {
	t.Parallel()
	for role, row := range chromeGlyphs {
		if !unicodeGlyph(row.Unicode) {
			t.Errorf("%s requires nonstandard Unicode font: %q", role, row.Unicode)
		}
		for _, set := range []string{"auto", "unicode", "nerd", "ascii"} {
			value := (Styles{glyphSet: set}).Glyph(role)
			if set == "ascii" {
				value = asciiFrame(value)
			}
			if ansi.StringWidth(value) != 1 {
				t.Errorf("%s/%s width: %q", role, set, value)
			}
		}
	}
	a := Appearance{}
	if err := a.SetGlyph("app.mark", GlyphRow{"◇", "\uf219"}); err != nil {
		t.Fatal(err)
	}
	if got := NewStyles(a).Glyph("app.mark"); got != "◇" {
		t.Errorf("application role = %q", got)
	}
	for _, value := range []string{"📌", "\uf219", "two", "\x1b[31mx"} {
		if err := a.SetGlyph("invalid", GlyphRow{value, "x"}); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestChromeBorderShapes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ set, panel, focused string }{
		{"rounded", "╭─╮", "┏━┓"}, {"square", "┌─┐", "┏━┓"}, {"classic", "╭─╮", "╭━╮"},
	} {
		s := NewStyles(Appearance{Theme: "mono"}).WithBorder(c.set)
		for _, focused := range []bool{false, true} {
			b := s.Border(focused)
			got, want := b.TopLeft+b.Top+b.TopRight, c.panel
			if focused {
				want = c.focused
			}
			if got != want {
				t.Errorf("%s focused=%v: %q != %q", c.set, focused, got, want)
			}
		}
	}
}

func TestGlyphSettingChangesLiveFrame(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 50, 18
	if err := r.Settings().Set(ChromeGlyphsSetting, "nerd"); err != nil {
		t.Fatal(err)
	}
	nerd := m.View()
	if !strings.Contains(nerd.Content, "\uf219") {
		t.Fatal("title mark did not switch to Nerd Font")
	}
	if err := r.Settings().Set(ChromeGlyphsSetting, "ascii"); err != nil {
		t.Fatal(err)
	}
	plain := m.View()
	for _, char := range plain.Content {
		if char > 127 {
			t.Fatalf("ASCII frame contains %U", char)
		}
	}
	if err := r.Settings().Set(ChromeGlyphsSetting, "unicode"); err != nil {
		t.Fatal(err)
	}
	if got := m.View().Content; !strings.Contains(got, "◆") {
		t.Fatal("Unicode opt-out did not restore title")
	}
}
