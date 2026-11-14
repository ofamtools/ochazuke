package ochazuke

import (
	"reflect"
	"slices"
	"testing"
)

// Any mode name is a mode (MODE-1): the palette's mode set, set mode and
// snapshot mode switch to a name nobody registered, which is listed, has
// its own binding context, and lets keys with no row of its own run
// normal's rows. A bindings.json row can target it first.
func TestModeSetAcceptsAnyName(t *testing.T) {
	r := NewRuntime()
	calls := recorder(t, r, "x", "y")
	r.Define("app").Mode("normal").Bind("x", Do("test.x"))
	if err := r.Bind(Binding{Noun: "app", Mode: "fresh", Keys: "y", Request: Do("test.y").Request}); err != nil {
		t.Fatalf("a row for an unregistered mode: %v", err)
	}
	p := NewPalette(r)
	for _, c := range []struct{ line, want string }{
		{"mode set brand-new", "brand-new"},
		{"set mode other", "other"},
		{"snapshot mode third", "third"},
		{"mode set fresh", "fresh"},
	} {
		if _, err := r.Execute(p.parse(c.line).Request()); err != nil || r.Mode.Name != c.want {
			t.Fatalf("%q: err %v mode %q", c.line, err, r.Mode.Name)
		}
		if !slices.Contains(r.BaseModeNames(), c.want) {
			t.Fatalf("%q: %s is not listed: %v", c.line, c.want, r.BaseModeNames())
		}
	}
	press(t, r, "y", "x")
	if !reflect.DeepEqual(*calls, []string{"y", "x"}) {
		t.Fatalf("fresh's own row, then normal's: %v", *calls)
	}
}

// nested (MODE-2): every key, the leader included, goes to the pane; the
// escape key (terminal.nested-escape) returns to the terminal mode.
func TestTerminalNestedModeForwardsEveryKey(t *testing.T) {
	r, _, stream := terminalFixture(t)
	press(t, r, "ctrl+a", "i")
	if r.Mode.Name != "nested" || len(stream.sent) != 0 {
		t.Fatalf("leader i: mode %q sent %v", r.Mode.Name, stream.sent)
	}
	press(t, r, "ctrl+a", "n", "ctrl+c")
	if !reflect.DeepEqual(stream.sent, [][]string{{"ctrl+a"}, {"n"}, {"ctrl+c"}}) || r.Mode.Name != "nested" {
		t.Fatalf("nested keys: %v mode %q", stream.sent, r.Mode.Name)
	}
	press(t, r, "ctrl+]")
	if r.Mode.Name != "terminal" || len(stream.sent) != 3 {
		t.Fatalf("escape: mode %q sent %v", r.Mode.Name, stream.sent)
	}
}
