package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// toneFixture is a page with one table drawn in the theme, its rows the
// given items, focused.
func toneFixture(t *testing.T, theme string, cols []Column, items []ListItem, opts ...InstanceOption) (*Model, *List) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	r.Define("row")
	r.Instance("main", Parent("app"), Kind("ochazuke.page"))
	if len(cols) > 0 {
		opts = append(opts, Columns(cols...))
	}
	l, err := r.AddList("t", items, append([]InstanceOption{Parent("main")}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterPage(&Page{ID: "main", Title: "Main", Focus: "t", Render: func(r *Runtime, w, h int) Block {
		return Text(l.View(w, h))
	}}); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 60, 12
	m.Init()
	m.Appearance.Theme = theme
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	return m, l
}

func fg(hex string) string {
	var r, g, b uint8
	fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	return fmt.Sprintf("38;2;%d;%d;%d", r, g, b)
}

// Tones never change the text: a table, a narrow one that cuts and drops
// columns, a right-aligned column, wide runes and the mark gutter draw
// the same characters with and without tones.
func TestTonesNeverChangeTheText(t *testing.T) {
	cols := []Column{{Title: "pane"}, {Title: "n", Align: AlignRight}, {Title: "state", Priority: 1}, {Title: "cwd", Priority: 2}}
	rows := []ListItem{
		{ID: "a", Label: "日本語のペイン", Noun: "row", Cells: []string{"日本語のペイン", "12", "needs input", "~/proj/ochazuke"}},
		{ID: "b", Label: "work/codex", Noun: "row", Cells: []string{"work/codex", "3", "working", "~/hub"}},
		{ID: "c", Label: "notes", Noun: "row", Cells: []string{"notes", "", "", "~/notes"}},
	}
	toned := slices.Clone(rows)
	toned[0].Tones = []ToneSpan{CellTone(0, ToneStar), CellTone(2, ToneNeedsInput), SpanTone(3, 2, 6, ToneQueued), CellTone(1, "no.such")}
	toned[1].Tones = []ToneSpan{SpanTone(0, 5, 0, ToneWorking), CellTone(1, ToneDone), CellTone(2, ToneWorking), CellTone(9, ToneErrored)}
	for _, theme := range []string{"dark", "mono"} {
		for _, width := range []int{60, 24, 12} {
			_, plain := toneFixture(t, theme, cols, rows)
			_, colour := toneFixture(t, theme, cols, toned)
			for _, mark := range []bool{false, true} {
				if mark {
					_ = plain.ToggleMark("b")
					_ = colour.ToggleMark("b")
				}
				want, got := plain.View(width, 6), colour.View(width, 6)
				if ansi.Strip(got) != ansi.Strip(want) {
					t.Fatalf("%s at %d (marks %v):\n%s\nwant\n%s", theme, width, mark, ansi.Strip(got), ansi.Strip(want))
				}
				if theme == "dark" && width == 60 && got == want {
					t.Fatal("tones drew nothing")
				}
			}
		}
	}
}

// A tone colours exactly its cells: the run starts at the toned text and
// the next cell is plain again; a span covers only its runes.
func TestToneColoursItsRunOnly(t *testing.T) {
	cols := []Column{{Title: "pane"}, {Title: "state"}}
	_, l := toneFixture(t, "dark", cols, []ListItem{
		{ID: "z", Label: "top", Noun: "row", Cells: []string{"top", ""}},
		{ID: "a", Label: "a", Noun: "row", Cells: []string{"★ work", "needs input"}, Tones: []ToneSpan{SpanTone(0, 0, 1, ToneStar), CellTone(1, ToneNeedsInput)}},
	})
	view := l.View(40, 3)
	line := trimLine(strings.Split(view, "\n")[2])
	star := "\x1b[1;" + fg("#F2CC60") + "m★\x1b[m"
	state := "\x1b[1;" + fg("#E3B341") + "mneeds input\x1b[m"
	if want := star + " work  " + state; line != want {
		t.Fatalf("line %q\nwant %q", line, want)
	}
}

// The selected row stays one reverse bar: its tones keep their bold and
// lose their colour and dim; a cell-mode table's current cell keeps its
// underline over a tone.
func TestSelectedRowDrawsToneAttributesOnly(t *testing.T) {
	cols := []Column{{Title: "pane"}, {Title: "state"}}
	_, l := toneFixture(t, "dark", cols, []ListItem{
		{ID: "a", Label: "a", Noun: "row", Cells: []string{"work", "needs input"}, Tones: []ToneSpan{CellTone(0, ToneExited), CellTone(1, ToneNeedsInput)}},
	}, CellSelect())
	line := strings.Split(l.View(30, 2), "\n")[1]
	if strings.Contains(line, "38;") || strings.Contains(line, "\x1b[2") {
		t.Fatalf("the selected row draws a tone's colour or dim: %q", line)
	}
	if !strings.Contains(line, "\x1b[4;7mwork\x1b[m") || !strings.Contains(line, "\x1b[7;1mneeds input\x1b[m") {
		t.Fatalf("selected row %q", line)
	}
	if ansi.StringWidth(line) != 30 {
		t.Fatalf("the bar spans %d cells, want 30", ansi.StringWidth(line))
	}
}

// A page list without the focus fades its tones with it: the tone's
// colour moves toward the background, as the list's text does.
func TestUnfocusedListFadesTones(t *testing.T) {
	cols := []Column{{Title: "pane"}, {Title: "state"}}
	m, l := toneFixture(t, "dark", cols, []ListItem{
		{ID: "z", Label: "top", Noun: "row", Cells: []string{"top", ""}},
		{ID: "a", Label: "a", Noun: "row", Cells: []string{"work", "working"}, Tones: []ToneSpan{CellTone(1, ToneWorking)}},
	})
	full := fg("#7CB7E8")
	if !strings.Contains(l.View(30, 3), full) {
		t.Fatal("the focused list draws the tone's colour")
	}
	m.Runtime.Instance("other", Parent("main"))
	m.Runtime.Focus = "other"
	faded := l.View(30, 3)
	if strings.Contains(faded, full) || !strings.Contains(faded, "working") {
		t.Fatalf("the unfocused list keeps the full colour: %q", faded)
	}
}

// A list without columns tones its label (cell 0) after the mark gutter.
func TestPlainListTonesTheLabel(t *testing.T) {
	_, l := toneFixture(t, "dark", nil, []ListItem{
		{ID: "z", Label: "top", Noun: "row"},
		{ID: "a", Label: "build failed", Noun: "row", Tones: []ToneSpan{SpanTone(0, 6, 0, ToneErrored)}},
	})
	_ = l.ToggleMark("a")
	line := trimLine(strings.Split(l.View(30, 2), "\n")[1])
	if want := "* build \x1b[1;" + fg("#E99B8D") + "mfailed\x1b[m"; line != want {
		t.Fatalf("line %q\nwant %q", line, want)
	}
}

// A wrapped cell keeps a whole-cell tone on every line of it and a span
// on the lines it falls on.
func TestWrappedCellTones(t *testing.T) {
	cols := []Column{{Title: "pane", Width: 6}, {Title: "note", Width: 8, Wrap: true}}
	_, l := toneFixture(t, "dark", cols, []ListItem{
		{ID: "z", Label: "top", Noun: "row", Cells: []string{"top", ""}},
		{ID: "a", Label: "a", Noun: "row", Cells: []string{"build", "one two three"}, Tones: []ToneSpan{CellTone(1, ToneWorking), SpanTone(0, 0, 2, ToneDone)}},
		{ID: "b", Label: "b", Noun: "row", Cells: []string{"lint", "one two three"}, Tones: []ToneSpan{SpanTone(1, 0, 3, ToneWorking)}},
	})
	lines := strings.Split(l.View(30, 8), "\n")
	blue := fg("#7CB7E8")
	if !strings.Contains(lines[2], blue) || !strings.Contains(lines[3], blue) || !strings.Contains(lines[2], fg("#8BC48A")+"mbu") {
		t.Fatalf("whole-cell tone on every line:\n%q\n%q", lines[2], lines[3])
	}
	if !strings.Contains(lines[4], blue+"mone\x1b[m two") || strings.Contains(lines[5], blue) {
		t.Fatalf("a span in a wrapped cell is not on its line:\n%q\n%q", lines[4], lines[5])
	}
}

// Without the Model's styles (a list drawn on a bare runtime) tones draw
// plain.
func TestTonesWithoutStylesDrawPlain(t *testing.T) {
	r := NewRuntime()
	r.Define("row")
	l, err := r.AddList("t", []ListItem{{ID: "a", Label: "x", Noun: "row", Tones: []ToneSpan{CellTone(0, ToneWorking)}}})
	if err != nil {
		t.Fatal(err)
	}
	if v := l.View(10, 1); strings.Contains(v, "38;") {
		t.Fatalf("%q", v)
	}
}

// A tree node's cell-0 tones stay on its label, after the indent and the
// fold marker.
func TestTreeTonesSkipThePrefix(t *testing.T) {
	r := NewRuntime()
	r.Instance("main", Parent("app"), Kind("ochazuke.page"))
	tr, err := r.AddTree("tr", []TreeNode{
		{ID: "a", Label: "build", Tones: []ToneSpan{CellTone(0, ToneWorking)}},
		{ID: "b", Parent: "a", Label: "lint 3", Tones: []ToneSpan{SpanTone(0, 5, 6, ToneNeedsInput), CellTone(1, ToneDone)}},
	}, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	items := tr.List().Items()
	if got := items[0].Tones; !slices.Equal(got, []ToneSpan{{Cell: 0, Tone: ToneWorking, From: 2}}) {
		t.Fatalf("root tones %+v (label %q)", got, items[0].Label)
	}
	if got := items[1].Tones; !slices.Equal(got, []ToneSpan{{Cell: 0, Tone: ToneNeedsInput, From: 9, To: 10}, {Cell: 1, Tone: ToneDone}}) {
		t.Fatalf("child tones %+v (label %q)", got, items[1].Label)
	}
}

// cellSegs measures in cells, clips to what the line shows and skips
// spans outside the text.
func TestCellSegs(t *testing.T) {
	got := cellSegs([]ToneSpan{SpanTone(0, 1, 3, "a"), SpanTone(0, 2, 0, "b"), SpanTone(0, 9, 12, "c"), SpanTone(0, 3, 2, "d")}, "日本語ab", 4, 7)
	want := []toneSeg{{x: 6, w: 4, tone: "a"}, {x: 8, w: 3, tone: "b"}}
	if !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
}

// themes.json tones: a scheme's are laid over the defaults, a built-in
// theme's name holds tones alone (adaptive's reach dark and light), a bad
// tone skips its scheme, and a tones-only entry is not a theme choice.
func TestParseTones(t *testing.T) {
	schemes, problems := ParseSchemes([]byte(`{
		"kombu": {"accent": "#C9A227", "tones": {"state.working": {"fg": "#5FAFD7", "bold": true}, "build.cached": "muted"}},
		"adaptive": {"tones": {"state.done": "#00AA00"}},
		"dark": {"tones": {"state.done": {"dim": true}, "marker.new": {"fg": {"light": "#111111", "dark": "#EEEEEE"}}}},
		"light": {"accent": "#111111", "tones": {}},
		"mono": {"tones": {"state.done": {"bold": true}}},
		"badtone": {"tones": {"state.x": "glow"}},
		"badfield": {"tones": {"state.x": {"colour": "#FFFFFF"}}},
		"badname": {"tones": {"two words": "#FFFFFF"}}
	}`))
	want := []string{
		`themes.json: badfield: tone state.x: a tone is a colour, a role, or {"fg": ..., "bg": ..., "bold": true, "dim": true} (skipped)`,
		`themes.json: badname: tone "two words": a tone name is one word (skipped)`,
		`themes.json: badtone: tone state.x: "glow" is not #RRGGBB, #RGB, an ANSI index 0-255 or a role (text, muted, accent, border, surface, error, mode) (skipped)`,
		"themes.json: light: is a built-in theme: it takes only tones (skipped)",
	}
	if !slices.Equal(problems, want) {
		t.Fatalf("problems:\n%s", strings.Join(problems, "\n"))
	}
	k := schemes["kombu"]
	if k.Tones[ToneWorking] != (ToneStyle{Fg: SchemeColor{"#5FAFD7", "#5FAFD7"}, Bold: true}) || k.Tones[ToneNeedsInput] != defaultTones[ToneNeedsInput] || k.Tones["build.cached"].Fg.Dark != "muted" {
		t.Fatalf("kombu tones %+v", k.Tones)
	}
	set := NewSchemeSet(schemes)
	if names := set.Names(); !slices.Equal(names, []string{"kombu"}) {
		t.Fatalf("names %v", names)
	}
	if names := (Appearance{Schemes: set}).ThemeNames(); !slices.Equal(names, []string{"adaptive", "dark", "light", "mono", "kombu"}) {
		t.Fatalf("theme names %v", names)
	}
	done := func(theme string) lipgloss.Style {
		return NewStyles(Appearance{Theme: theme, Dark: true, Schemes: set}).Tone(ToneDone)
	}
	if !sameColor(done("adaptive").GetForeground(), lipgloss.Color("#00AA00")) || done("adaptive").GetFaint() {
		t.Fatal("adaptive's override")
	}
	if d := done("dark"); d.GetForeground() != nil && !isNoColor(d) || !d.GetFaint() {
		t.Fatalf("dark's override replaces the tone whole (no colour, dim): %v", d.GetForeground())
	}
	if l := done("light"); !sameColor(l.GetForeground(), lipgloss.Color("#00AA00")) {
		t.Fatal("light takes adaptive's override")
	}
	if mono := done("mono"); !mono.GetBold() || !isNoColor(mono) {
		t.Fatal("mono's override: bold, no colour")
	}
	if c := NewStyles(Appearance{Theme: "kombu", Dark: true, Schemes: set}).Tone("build.cached"); !sameColor(c.GetForeground(), lipgloss.Color(defaultScheme.Muted.Dark)) {
		t.Fatal("a role names the scheme's colour")
	}
}

// trimLine drops the padding and closing reset layoutRows adds.
func trimLine(line string) string {
	return strings.TrimSuffix(strings.TrimRight(line, " "), "\x1b[0m")
}

func isNoColor(s lipgloss.Style) bool {
	_, ok := s.GetForeground().(lipgloss.NoColor)
	return ok || s.GetForeground() == nil
}

// Every built-in theme draws the state tones: a colour in adaptive, dark
// and light (the side the theme draws), the attributes alone in mono and
// under NO_COLOR; an unknown tone is the empty style; state.errored
// follows the scheme's error role.
func TestBuiltinThemesDrawStateTones(t *testing.T) {
	state := []string{ToneWorking, ToneNeedsInput, ToneDone, ToneErrored, ToneIdle, ToneExited}
	for _, theme := range []string{"adaptive", "dark", "light"} {
		s := NewStyles(Appearance{Theme: theme, Dark: true})
		for _, tone := range state {
			if isNoColor(s.Tone(tone)) {
				t.Fatalf("%s draws %s without a colour", theme, tone)
			}
		}
		if s.Tone("no.such").GetBold() || !isNoColor(s.Tone("no.such")) {
			t.Fatal("an unknown tone is plain")
		}
	}
	if !sameColor(NewStyles(Appearance{Theme: "light"}).Tone(ToneWorking).GetForeground(), lipgloss.Color("#1F6FB2")) {
		t.Fatal("light draws the light side")
	}
	if !sameColor(NewStyles(Appearance{Theme: "dark"}).Tone(ToneErrored).GetForeground(), lipgloss.Color(defaultScheme.Error.Dark)) {
		t.Fatal("errored is the error role")
	}
	mono := NewStyles(Appearance{Theme: "mono"})
	for _, tone := range state {
		if !isNoColor(mono.Tone(tone)) {
			t.Fatalf("mono colours %s", tone)
		}
	}
	if !mono.Tone(ToneNeedsInput).GetBold() || !mono.Tone(ToneExited).GetFaint() {
		t.Fatal("mono keeps the attributes")
	}
	t.Setenv("NO_COLOR", "1")
	if s := NewStyles(Appearance{Theme: "dark"}); !isNoColor(s.Tone(ToneWorking)) || !s.Tone(ToneErrored).GetBold() {
		t.Fatal("NO_COLOR keeps the attributes alone")
	}
}

// A themes.json file read by the Model: the built-in's tones apply under
// the theme setting, with no notice and no second "dark" among the
// choices.
func TestThemesFileTonesReachTheModel(t *testing.T) {
	m := themeModel(t, `{"dark": {"tones": {"state.working": "#5FAFD7"}}}`, `{"theme": "dark"}`)
	if len(m.Notices) > 0 || m.Appearance.Theme != "dark" {
		t.Fatalf("notices %q theme %q", m.Notices, m.Appearance.Theme)
	}
	v, err := m.Runtime.Settings().Get(ThemeSetting)
	if err != nil || !slices.Equal(v.Enum, BuiltinThemes) {
		t.Fatalf("enum %v %v", v.Enum, err)
	}
	if !sameColor(m.Styles().Tone(ToneWorking).GetForeground(), lipgloss.Color("#5FAFD7")) {
		t.Fatal("the override is not drawn")
	}
}
