package ochazuke

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// focusDisplayApp is a page "work" holding a list "files" of two rows of
// noun file: one with a short label, one with a label wider than
// FocusLabelMax.
func focusDisplayApp(t *testing.T) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	r.Define("file")
	if _, err := r.AddPage("work", "Work", func(*Runtime, int, int) Block { return Text("body") }); err != nil {
		t.Fatal(err)
	}
	items := []ListItem{
		{ID: "f1", Label: "notes.md", Noun: "file"},
		{ID: "f2", Label: "a-label-much-wider-than-sixteen", Noun: "file"},
	}
	if _, err := r.AddList("files", items, Parent("work")); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Appearance.Theme = "mono"
	m.Init()
	if err := r.OpenPage("work"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("f1"); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return r, m
}

func titleRowOf(m *Model) string {
	return strings.TrimRight(strings.Split(ansi.Strip(m.View().Content), "\n")[0], " ")
}

// The setting is declared by the framework, off by default, and a change
// shows on the next frame.
func TestFocusDisplaySettingAppliesLive(t *testing.T) {
	r, m := focusDisplayApp(t)
	if got := r.FocusDisplay(); got != FocusDisplayOff {
		t.Fatalf("default focus-display = %q, want off", got)
	}
	if row := titleRowOf(m); !strings.HasSuffix(row, " NORMAL") {
		t.Fatalf("off changes the chrome: %q", row)
	}
	if err := r.Settings().Set(FocusDisplaySetting, "noun"); err != nil {
		t.Fatal(err)
	}
	if row := titleRowOf(m); !strings.HasSuffix(row, " file  NORMAL") {
		t.Fatalf("noun: %q", row)
	}
	if err := r.Settings().Set(FocusDisplaySetting, "stack"); err != nil {
		t.Fatal(err)
	}
	if row := titleRowOf(m); !strings.HasSuffix(row, " Work › list › notes.md  NORMAL") {
		t.Fatalf("stack: %q", row)
	}
	// A focus change redraws the path.
	if err := r.SetFocus("f2"); err != nil {
		t.Fatal(err)
	}
	if row := titleRowOf(m); !strings.HasSuffix(row, " Work › list › file  NORMAL") {
		t.Fatalf("stack after a move: %q", row)
	}
	if err := r.Settings().Set(FocusDisplaySetting, "bogus"); err == nil {
		t.Fatal("focus-display took a value outside its enum")
	}
}

// focus.display with no mode cycles off → noun → stack → off; a mode sets
// that value.
func TestFocusDisplayVerbCycles(t *testing.T) {
	r, _ := focusDisplayApp(t)
	for _, want := range []string{"noun", "stack", "off", "noun"} {
		got, err := r.Execute(Request{Verb: "display", Noun: "ochazuke.focus"})
		if err != nil {
			t.Fatal(err)
		}
		if got != want || r.FocusDisplay() != want {
			t.Fatalf("cycle gave %v (setting %q), want %q", got, r.FocusDisplay(), want)
		}
	}
	if _, err := r.Execute(Request{Verb: "display", Noun: "ochazuke.focus", Args: Arguments{"mode": "stack"}}); err != nil {
		t.Fatal(err)
	}
	if r.FocusDisplay() != "stack" {
		t.Fatalf("mode stack left %q", r.FocusDisplay())
	}
	if _, err := r.Execute(Request{Verb: "display", Noun: "ochazuke.focus", Args: Arguments{"mode": "wide"}}); err == nil {
		t.Fatal("focus.display took a mode outside its enum")
	}
	op, ok := r.Operation("ochazuke.focus.display")
	if !ok || !op.Builtin {
		t.Fatalf("focus.display is not a builtin operation: %+v", op)
	}
	if r.DisplayKey(op.Key()) != "focus.display" {
		t.Fatalf("display key %q", r.DisplayKey(op.Key()))
	}
	// No default key: nothing binds it.
	for _, row := range r.allBindings() {
		if row.Request.Key() == "ochazuke.focus.display" {
			t.Fatalf("focus.display has a default key: %+v", row)
		}
	}
}

// A level shows its label when it has one no wider than FocusLabelMax (a
// page's is its title), else its noun as DisplayNoun; app is left out and
// an open modal follows the focus it opened over.
func TestFocusPathLabelOrNoun(t *testing.T) {
	r, _ := focusDisplayApp(t)
	if got := strings.Join(r.FocusPath(), " › "); got != "Work › list › notes.md" {
		t.Fatalf("short label: %q", got)
	}
	if err := r.SetFocus("f2"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.FocusPath(), " › "); got != "Work › list › file" {
		t.Fatalf("long label: %q", got)
	}
	if got := r.FocusNoun(); got != "file" {
		t.Fatalf("FocusNoun = %q", got)
	}
	if _, err := r.OpenModal("confirm", Payload{Prompt: "Sure?"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.FocusPath(), " › "); got != "Work › list › file › confirm" {
		t.Fatalf("modal: %q", got)
	}
	if got := r.FocusNoun(); got != "confirm" {
		t.Fatalf("FocusNoun in a modal = %q", got)
	}
}

// Cutting drops whole levels from the left behind "… › " and keeps the
// innermost level; an innermost level wider than the room is cut from its
// left behind "…".
func TestFitFocusPathCutsFromTheLeft(t *testing.T) {
	levels := []string{"panes", "pane-list", "pane"}
	cases := []struct {
		width int
		want  string
	}{
		{40, "panes › pane-list › pane"},
		{24, "panes › pane-list › pane"},
		{23, "… › pane-list › pane"},
		{20, "… › pane-list › pane"},
		{19, "… › pane"},
		{8, "… › pane"},
		{7, "…pane"},
		{5, "…pane"},
		{4, "…ane"},
		{3, "…ne"},
		{1, "…"},
		{0, ""},
	}
	for _, c := range cases {
		got := FitFocusPath(levels, c.width)
		if got != c.want {
			t.Errorf("width %d: %q, want %q", c.width, got, c.want)
		}
		if ansi.StringWidth(got) > max(c.width, 0) {
			t.Errorf("width %d: %q is %d cells", c.width, got, ansi.StringWidth(got))
		}
	}
	if got := FitFocusPath(nil, 10); got != "" {
		t.Errorf("no levels: %q", got)
	}
	// One level (the noun) is marked only when it is cut.
	if got := FitFocusPath([]string{"pane"}, 4); got != "pane" {
		t.Errorf("one level that fits: %q", got)
	}
	if got := FitFocusPath([]string{"pane-list"}, 5); got != "…list" {
		t.Errorf("one level cut: %q", got)
	}
}

// In a narrow header the title keeps its first cells and the path is cut
// from the left; the row never overflows.
func TestFocusDisplayNarrowHeaderKeepsInnermost(t *testing.T) {
	r, m := focusDisplayApp(t)
	if err := r.Settings().Set(FocusDisplaySetting, "stack"); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 36, Height: 12})
	row := titleRowOf(m)
	if !strings.Contains(row, "Ochazuke …") || !strings.HasSuffix(row, "es.md  NORMAL") {
		t.Fatalf("narrow: %q", row)
	}
	if ansi.StringWidth(row) > 36 {
		t.Fatalf("title row overflows: %q", row)
	}
}

// A user binds a key to focus.display in bindings.json (the README's row):
// the short noun resolves and the key cycles the setting.
func TestFocusDisplayUserBinding(t *testing.T) {
	r, m := focusDisplayApp(t)
	if _, err := r.LoadBindings([]byte(`[{"noun": "app", "mode": "normal", "keys": "g f", "request": {"noun": "focus", "verb": "display"}}]`)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"g", "f"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if r.FocusDisplay() != FocusDisplayNoun {
		t.Fatalf("g f left focus-display %q, want noun", r.FocusDisplay())
	}
	if row := titleRowOf(m); !strings.HasSuffix(row, " file  NORMAL") {
		t.Fatalf("after g f: %q", row)
	}
}
