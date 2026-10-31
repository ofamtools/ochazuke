package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// typingPlaces are the framework's places that take text, each reached the
// way a person reaches it: a page input in insert mode, a modal prompt, the
// palette, a chooser, a list's filter line, an outline's open line and a
// form field. Each returns the runtime with the place focused.
var typingPlaces = []struct {
	name  string
	reach func(t *testing.T) *Runtime
}{
	{"page input", func(t *testing.T) *Runtime {
		r, _, _ := helpInsertFixture(t)
		return r
	}},
	{"modal prompt", func(t *testing.T) *Runtime {
		r, _, _ := helpInsertFixture(t)
		ask := NewInput("answer", "yes", false)
		if _, err := r.OpenModal("ask", Payload{}, WithInput(ask, Kind("ochazuke.prompt"))); err != nil {
			t.Fatal(err)
		}
		return r
	}},
	{"palette", func(t *testing.T) *Runtime {
		r, m, _ := helpInsertFixture(t)
		if err := r.SetMode("normal"); err != nil {
			t.Fatal(err)
		}
		keytest.Press(m, ":")
		if r.Focus != paletteLineID {
			t.Fatalf("palette focus %q", r.Focus)
		}
		return r
	}},
	{"chooser", func(t *testing.T) *Runtime {
		r, _, _ := helpInsertFixture(t)
		if _, err := r.OpenChooser(ChooserSpec{Kind: "k", Rows: []ChooserRow{{ID: "a", Label: "alpha"}}}); err != nil {
			t.Fatal(err)
		}
		return r
	}},
	{"list filter", func(t *testing.T) *Runtime {
		r, m, _ := filterFixture(t)
		keytest.Press(m, "/")
		if r.Mode.Name != ListFilterMode {
			t.Fatalf("/ left mode %q", r.Mode.Name)
		}
		return r
	}},
	{"outline line", func(t *testing.T) *Runtime {
		r, m, _, _ := treeFixture(t)
		keytest.Press(m, "i")
		if r.Mode.Name != "insert" {
			t.Fatalf("i left mode %q", r.Mode.Name)
		}
		return r
	}},
	{"form field", func(t *testing.T) *Runtime {
		r, m, _, _ := recordFixture(t, nil)
		keytest.Press(m, "q")
		if _, err := r.OpenRecordEditor("edit-task", RecordSpec{Title: "Edit task", Fields: taskFields(), Insert: true, Save: func(RecordWrite) Result { return Done(nil) }}); err != nil {
			t.Fatal(err)
		}
		if r.Input(r.Focus) == nil {
			t.Fatalf("form focus %q is no input", r.Focus)
		}
		return r
	}},
}

// While a field takes text, no default row claims a key a person types:
// every printable character and space reaches the field, in every place
// the framework types and every mode it can be in (Runtime.TypingShadows).
// A row on a plain letter, digit or punctuation key there (a page's z,
// an app's q) would run in place of the character.
func TestNoPrintableKeysWhileTyping(t *testing.T) {
	for _, place := range typingPlaces {
		r := place.reach(t)
		for _, line := range r.TypingShadows() {
			t.Errorf("%s: %s", place.name, line)
		}
	}
}

// The check sees what the resolver does: a mode-less page row on q, an
// app capture row on z and a page sequence starting with g are each
// reported for an input typing on that page, and the focus and mode are
// left as they were.
func TestTypingShadowsReportsOffenders(t *testing.T) {
	r, _, _ := helpInsertFixture(t)
	page := r.Instances["p"]
	page.Bindings = append(page.Bindings,
		Binding{Keys: "q", Request: Do("ochazuke.focus.next").Request},
		Binding{Mode: "insert", Keys: "g x", Request: Do("ochazuke.focus.next").Request},
	)
	app := r.Instances["app"]
	app.Bindings = append(app.Bindings, Binding{Mode: "insert", Keys: "z", Request: Do("ochazuke.focus.next").Request, Capture: true})
	focus, mode := r.Focus, r.Mode
	lines := r.TypingShadows()
	if r.Focus != focus || r.Mode.Name != mode.Name {
		t.Fatalf("the check moved the focus or mode: %q %q", r.Focus, r.Mode.Name)
	}
	for _, want := range []string{`focus note mode insert: typing "q"`, `focus note mode insert: typing "z"`, `focus note mode insert: typing "g"`} {
		if !slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, want) }) {
			t.Errorf("not reported: %s\n%s", want, strings.Join(lines, "\n"))
		}
	}
	for _, l := range lines {
		if strings.Contains(l, `typing "x"`) {
			t.Errorf("x is not bound alone: %s", l)
		}
	}
}

// Typing z and q into a list's filter line and into an input in insert
// mode puts them in the text: the keys bound in normal mode (q records a
// macro, z starts the column rows) never run there.
func TestTypingZAndQInsertsThem(t *testing.T) {
	r, m, l := filterFixture(t)
	for _, k := range []string{"/", "z", "q"} {
		keytest.Press(m, k)
	}
	if r.Mode.Name != ListFilterMode || l.Filter() != "zq" {
		t.Fatalf("filter: mode %q query %q", r.Mode.Name, l.Filter())
	}
	r, m, in := helpInsertFixture(t)
	for _, k := range []string{"z", "q", "space", "g", "/"} {
		keytest.Press(m, k)
	}
	if got := string(in.Text); r.Mode.Name != "insert" || got != "zq g/hi" {
		t.Fatalf("input: mode %q text %q", r.Mode.Name, got)
	}
}
