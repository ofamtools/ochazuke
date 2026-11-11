package ochazuke

import "testing"

// A row guard is data in its name: row:FIELD (present), row:!FIELD
// (absent), row:FIELD=VALUE (its text). It reads the focused row's Data
// and then its list item's props, as a "row:FIELD" binding source does,
// and a binding row takes it with no DefineGuard.
func TestRowGuardReadsTheFocusedRow(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	r.DefineOwn("thing")
	list, err := r.AddList("things", []ListItem{
		{ID: "things/a", Label: "a", Noun: "thing", Data: map[string]any{"id": "q1", "kind": "question"}},
		{ID: "things/b", Label: "b", Noun: "thing", Data: "b", Props: Thing{"item": "x", "count": 2}},
		{ID: "things/c", Label: "c", Noun: "thing", Data: "c"},
	})
	if err != nil || list == nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		focus, guard string
		want         bool
	}{
		{"things/a", "row:id", true},
		{"things/c", "row:id", false},
		{"things/a", "row:!item", true},
		{"things/b", "row:!item", false},
		{"things/b", "row:item", true},
		{"things/a", "row:kind=question", true},
		{"things/a", "row:kind=topic", false},
		{"things/b", "row:count=2", true},
		{"things/c", "row:kind=question", false},
	} {
		g := r.rawGuard(c.guard)
		if g == nil {
			t.Fatalf("%s: no guard", c.guard)
		}
		if got := g(r, KeyEvent{Mode: r.Mode, Focus: c.focus}); got != c.want {
			t.Errorf("%s on %s = %v, want %v", c.guard, c.focus, got, c.want)
		}
	}
	for _, bad := range []string{"row:", "row:!", "row:!a=b", "row:a..b"} {
		if r.rawGuard(bad) != nil || PortableGuard(bad) {
			t.Errorf("%q is a guard", bad)
		}
	}
	if err := r.Bind(Binding{Noun: "thing", Mode: "normal", Keys: "z", When: "row:!item", Request: Request{Noun: "ochazuke.focus", Verb: "next"}}); err != nil {
		t.Fatalf("a row guard binds with no registration: %v", err)
	}
	if err := r.Bind(Binding{Noun: "thing", Mode: "normal", Keys: "y", When: "row:", Request: Request{Noun: "ochazuke.focus", Verb: "next"}}); err == nil {
		t.Fatal("an empty row guard binds")
	}
}

// PortableGuard: the built-ins and row guards mean the same in any
// runtime; an application's guard and an op: guard do not.
func TestPortableGuard(t *testing.T) {
	r := NewRuntime()
	r.DefineGuard("app-only", func(*Runtime, KeyEvent) bool { return true })
	for name, want := range map[string]bool{
		"no-input": true, "count": true, "row:id": true, "row:!item": true, "row:kind=a": true,
		"app-only": false, "op:ochazuke.delete": false, "": false, "row:": false,
	} {
		if got := PortableGuard(name); got != want {
			t.Errorf("PortableGuard(%q) = %v, want %v", name, got, want)
		}
	}
}
