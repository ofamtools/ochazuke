package ochazuke

import (
	"fmt"
	"strings"
	"testing"
)

func bgSGR(hex string) string {
	var r, g, b uint8
	fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	return fmt.Sprintf("48;2;%d;%d;%d", r, g, b)
}

// The built-in error tone fills its cell with the theme's error
// background, bold; its side follows the theme (dark, light); mono draws
// the bold alone.
func TestErrorToneFillsItsBackground(t *testing.T) {
	cols := []Column{{Title: "key"}, {Title: "value"}}
	rows := []ListItem{
		{ID: "z", Label: "top", Noun: "row", Cells: []string{"top", ""}},
		{ID: "a", Label: "a", Noun: "row", Cells: []string{"id", "1"}, Tones: []ToneSpan{CellTone(0, ToneError)}},
	}
	for theme, hex := range map[string]string{"dark": "#5E2722", "light": "#F6D3CE"} {
		_, l := toneFixture(t, theme, cols, rows)
		line := strings.Split(l.View(30, 3), "\n")[2]
		if !strings.Contains(line, "\x1b["+bgSGR(hex)+";1mid\x1b[m") {
			t.Fatalf("%s: error cell %q", theme, line)
		}
	}
	_, l := toneFixture(t, "mono", cols, rows)
	if line := strings.Split(l.View(30, 3), "\n")[2]; strings.Contains(line, "48;") || !strings.Contains(line, "\x1b[1mid") {
		t.Fatalf("mono: %q", line)
	}
}

// A span's Bg lays a tone's background under another tone's foreground;
// Bg alone only fills.
func TestSpanBgComposesWithTone(t *testing.T) {
	cols := []Column{{Title: "key"}, {Title: "value"}}
	_, l := toneFixture(t, "dark", cols, []ListItem{
		{ID: "z", Label: "top", Noun: "row", Cells: []string{"top", ""}},
		{ID: "a", Label: "a", Noun: "row", Cells: []string{"name", "x"}, Tones: []ToneSpan{{Cell: 0, Tone: ToneWorking, Bg: ToneError}, {Cell: 1, Bg: ToneError}}},
	})
	line := strings.Split(l.View(30, 3), "\n")[2]
	if !strings.Contains(line, bgSGR("#5E2722")+";"+fg("#7CB7E8")+"mname") {
		t.Fatalf("composed run %q", line)
	}
	if !strings.Contains(line, "\x1b["+bgSGR("#5E2722")+"mx") {
		t.Fatalf("bg-only run %q", line)
	}
}

// The selected row drops a tone's background by default (one reverse
// bar); tones.selected-background keep draws the run on its background,
// bold, out of the bar.
func TestSelectedRowBackgroundSetting(t *testing.T) {
	cols := []Column{{Title: "key"}, {Title: "value"}}
	m, l := toneFixture(t, "dark", cols, []ListItem{
		{ID: "a", Label: "a", Noun: "row", Cells: []string{"id", "1"}, Tones: []ToneSpan{CellTone(0, ToneError)}},
	})
	line := strings.Split(l.View(30, 2), "\n")[1]
	if strings.Contains(line, "48;") || !strings.Contains(line, "\x1b[7;1mid") {
		t.Fatalf("drop: %q", line)
	}
	m.Runtime.EnsureConfigSettings()
	if err := m.Runtime.Settings().Set(SelectedBackgroundSetting, "keep"); err != nil {
		t.Fatal(err)
	}
	line = strings.Split(l.View(30, 2), "\n")[1]
	if !strings.Contains(line, "\x1b["+bgSGR("#5E2722")+";1mid") || !strings.Contains(line, "\x1b[7m") {
		t.Fatalf("keep: %q", line)
	}
}

// themes.json tones take bg (a colour or a role); a bad bg skips the
// scheme.
func TestThemesToneBg(t *testing.T) {
	schemes, problems := ParseSchemes([]byte(`{
		"kombu": {"tones": {"error": {"bg": "error", "fg": "surface"}, "warn": {"bg": {"light": "#FFF1C2", "dark": "#4A3A00"}}}},
		"bad": {"tones": {"x": {"bg": "glow"}}}
	}`))
	if len(problems) != 1 || !strings.Contains(problems[0], `bad: tone x: "glow"`) {
		t.Fatalf("problems %q", problems)
	}
	k := schemes["kombu"].Tones
	if k[ToneError].Bg.Dark != "error" || k["warn"].Bg.Light != "#FFF1C2" {
		t.Fatalf("tones %+v", k)
	}
	st := NewStyles(Appearance{Theme: "kombu", Schemes: NewSchemeSet(schemes), Dark: true})
	if bg := st.Tone(ToneError).GetBackground(); bg == nil || fmt.Sprint(bg) == fmt.Sprint(st.Tone("none").GetBackground()) {
		t.Fatalf("Styles.Tone has no background: %v", bg)
	}
}

// The chip tones: a dim background, and the focused chip the surface on
// the accent, bold; both follow a scheme's roles.
func TestChipTones(t *testing.T) {
	cols := []Column{{Title: "q"}}
	_, l := toneFixture(t, "dark", cols, []ListItem{
		{ID: "z", Label: "top", Noun: "row", Cells: []string{"top"}},
		{ID: "a", Label: "a", Noun: "row", Cells: []string{".items select"}, Tones: []ToneSpan{SpanTone(0, 0, 6, ToneChip), SpanTone(0, 7, 0, ToneChipFocused)}},
	})
	line := strings.Split(l.View(30, 3), "\n")[2]
	if !strings.Contains(line, bgSGR("#26362F")+"m.items") {
		t.Fatalf("chip %q", line)
	}
	if !strings.Contains(line, bgSGR("#78BFA5")+";1;"+fg("#172720")+"mselect") {
		t.Fatalf("focused chip %q", line)
	}
}
