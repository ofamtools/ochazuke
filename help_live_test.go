package ochazuke

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// A change to the bindings that does not come through the help's own bind
// (a hand-edited bindings.json read by binding.reload) shows in every open
// view of the help without reopening it: the table's rows and the here
// view's rows, through the bindings generation, not a call wired to a view.
func TestHelpFollowsAnyBindingChange(t *testing.T) {
	r, m, _ := tableOpen(t)
	dir := t.TempDir()
	r.Config().Dir = dir
	row := `{"bindings":[{"context":"widget/normal","bindings":[{"key":"p","action":"widget.ping"}]}]}`
	if err := os.WriteFile(filepath.Join(dir, BindingsFile), []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.binding", Verb: "reload"}); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "ctrl+g") // any update settles the help
	tableHas := func() bool {
		for _, item := range r.List(helpListID).Items() {
			if item.Cells[0] == "p" {
				return true
			}
		}
		return false
	}
	if !tableHas() {
		t.Fatal("the table view does not show p after the reload")
	}
	keytest.Press(m, "S")
	for _, row := range r.helpOpen().here {
		if row.Operation == "widget.ping" && row.Key == "p" {
			return
		}
	}
	t.Fatal("the here view does not show p after the reload")
}

func boundKeys(t *testing.T, r *Runtime) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.Config().Dir, BindingsFile))
	if err != nil {
		return ""
	}
	return string(data)
}

// b captures a sequence as it is pressed, showing the keys so far: g starts
// sequences that are bound, so it goes on, and z ends the capture (nothing
// goes on from g z). The row written is a sequence row, and g z runs the
// verb at once. A key that starts nothing still binds at once.
func TestHelpBindCapturesASequence(t *testing.T) {
	r, m, ran := unboundFixture(t)
	r.Config().Dir = t.TempDir()
	keytest.Press(m, "?")
	selectHelpRow(t, r, "widget.ping")
	keytest.Press(m, "b")
	keytest.Press(m, "g")
	if r.Mode.Name != "help_bind_pending" || !strings.Contains(m.View().Content, "bind widget.ping: g _") {
		t.Fatalf("not capturing after g: mode %q\n%s", r.Mode.Name, m.View().Content)
	}
	keytest.Press(m, "z")
	if r.Mode.Name == "help_bind_pending" || !strings.Contains(boundKeys(t, r), `"key": "g z"`) {
		t.Fatalf("g z not bound: mode %q file %q", r.Mode.Name, boundKeys(t, r))
	}
	keytest.Press(m, "q")
	keytest.Press(m, "g")
	keytest.Press(m, "z")
	if !slices.Equal(*ran, []string{"ping"}) {
		t.Fatalf("g z ran %v", *ran)
	}
}

// B takes any keys in order (three here) until enter; backspace drops the
// last key and ctrl+v takes the next key literally, so esc can be bound.
func TestHelpBindManualKeysDropAndLiteral(t *testing.T) {
	r, m, ran := unboundFixture(t)
	r.Config().Dir = t.TempDir()
	keytest.Press(m, "?")
	selectHelpRow(t, r, "widget.ping")
	for _, k := range []string{"B", "x", "y", "backspace", "ctrl+v", "esc", "z"} {
		keytest.Press(m, k)
	}
	if !strings.Contains(m.View().Content, "bind widget.ping: x esc z _") {
		t.Fatalf("keys so far not shown:\n%s", m.View().Content)
	}
	keytest.Press(m, "enter")
	if !strings.Contains(boundKeys(t, r), `"key": "x esc z"`) {
		t.Fatalf("file %q", boundKeys(t, r))
	}
	keytest.Press(m, "q")
	for _, k := range []string{"x", "esc", "z"} {
		keytest.Press(m, k)
	}
	if !slices.Equal(*ran, []string{"ping"}) {
		t.Fatalf("x esc z ran %v", *ran)
	}
}

// A key that would cut a bound sequence short (g, with g g bound) is not
// refused: enter asks, naming the sequence, and yes replaces.
func TestHelpBindPrefixConflictOffersReplace(t *testing.T) {
	r, m, _ := unboundFixture(t)
	r.Config().Dir = t.TempDir()
	keytest.Press(m, "?")
	selectHelpRow(t, r, "widget.ping")
	keytest.Press(m, "b")
	keytest.Press(m, "g")
	keytest.Press(m, "enter")
	if view := m.View().Content; !strings.Contains(view, "g starts the sequence g g") {
		t.Fatalf("no question:\n%s", view)
	}
	if boundKeys(t, r) != "" {
		t.Fatal("written before the yes")
	}
	keytest.Press(m, "y")
	if !strings.Contains(boundKeys(t, r), `"key": "g"`) {
		t.Fatalf("yes did not bind g: %q", boundKeys(t, r))
	}
}

// help.bind-sequences off: b binds the first key at once, g included (it
// asks about g g as it did before sequences), and B is not offered.
func TestHelpBindSequencesOff(t *testing.T) {
	r, m, _ := unboundFixture(t)
	r.EnsureConfigSettings()
	r.Config().Dir = t.TempDir()
	if err := r.Settings().Set(HelpBindSequencesSetting, false); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "?")
	selectHelpRow(t, r, "widget.ping")
	keytest.Press(m, "B")
	if r.Mode.Name == "help_bind_pending" {
		t.Fatal("B captured with sequences off")
	}
	keytest.Press(m, "b")
	keytest.Press(m, "g")
	if r.Mode.Name == "help_bind_pending" || !strings.Contains(m.View().Content, "g starts the sequence g g") {
		t.Fatalf("g did not bind at once: mode %q", r.Mode.Name)
	}
}
