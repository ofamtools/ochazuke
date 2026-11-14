package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

// redirectRuntime is a tree of a "box" row with an inner box, a "tag" and
// a "part" row drawn under it; box binds x, z and d d, part binds z and
// t, tag binds nothing; part's and tag's unbound keys go to their parent
// and box's to its first part child (the inner box and the tag come
// first), by default data rows. ran
// records "verb@instance".
func redirectRuntime(t *testing.T) (*Runtime, *[]string) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	var ran []string
	record := func(name string) func(Ctx) Result {
		return func(c Ctx) Result {
			id := ""
			if c.Instance != nil {
				id = c.Instance.ID
			}
			ran = append(ran, name+"@"+id)
			return Done(nil)
		}
	}
	r.Define("box").Verb("poke", record("box.poke"), "Poke the box").Verb("kick", record("box.kick"), "Kick the box").
		Mode("normal").Bind("x", Do("box.poke")).Bind("z", Do("box.poke")).Bind("w", Do("box.poke")).Bind("d d", Do("box.kick"))
	r.Define("part").Kind(NounKindPart).DefaultKeys(false).Verb("peek", record("part.peek"), "Peek at the part").
		Verb("tap", record("part.tap"), "Tap the part").
		Mode("normal").Bind("z", Do("part.peek")).Bind("t", Do("part.tap"))
	r.Define("tag").Kind(NounKindPart).DefaultKeys(false)
	if err := r.LoadDefaultBindings("test/redirect.json", []byte(`{"bindings": [
		{"context": "part/normal", "bindings": [
			{"key": "unbound", "action": "ochazuke.key.redirect", "args": {"to": "parent"}, "hidden": true}
		]},
		{"context": "tag/normal", "bindings": [
			{"key": "unbound", "action": "ochazuke.key.redirect", "args": {"to": "parent"}, "hidden": true}
		]},
		{"context": "box/normal", "bindings": [
			{"key": "unbound", "action": "ochazuke.key.redirect", "args": {"to": "child", "of": "part"}, "hidden": true}
		]}
	]}`)); err != nil {
		t.Fatal(err)
	}
	r.Instance("p")
	tr, err := r.AddTree("p/t", []TreeNode{{ID: "b1", Label: "box", Noun: "box"}, {ID: "b2", Parent: "b1", Label: "inner box", Noun: "box"}, {ID: "g1", Parent: "b1", Label: "tag", Noun: "tag"}, {ID: "s1", Parent: "b1", Label: "part", Noun: "part"}}, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	// The list both rows share binds w and v: the related row's w answers
	// before it, its v answers from the focus.
	for _, k := range []string{"w", "v"} {
		row := Do("part.peek")
		row.Instance, row.Mode, row.Keys = tr.List().id, "normal", k
		if err := r.Bind(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.SetFocus("s1"); err != nil {
		t.Fatal(err)
	}
	return r, &ran
}

// A key the focus does not bind goes to the thing its redirect row names
// by relation (parent: the row it is drawn under; child: the first row
// under it) and acts on it; a key the focus binds stays its own; the
// related row answers before the list they share, which answers the rest
// from the focus; a sequence reaches the related thing's rows too; a user row unbinding the
// redirect turns it off.
func TestKeyRedirectByRelation(t *testing.T) {
	t.Parallel()
	r, ran := redirectRuntime(t)
	for _, k := range []string{"x", "z", "d", "d", "w", "v"} {
		if _, err := r.HandleBinding(k); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"box.poke@b1", "part.peek@s1", "box.kick@b1", "box.poke@b1", "part.peek@s1"}; !slices.Equal(*ran, want) {
		t.Fatalf("ran %v, want %v", *ran, want)
	}
	if r.Focus != "s1" {
		t.Fatalf("focus moved to %q", r.Focus)
	}
	// child of part: the box row's unbound t is its first part child's,
	// past the inner box and the tag; the part's redirect back to the box
	// ends the chain.
	if err := r.SetFocus("b1"); err != nil {
		t.Fatal(err)
	}
	*ran = nil
	if _, err := r.HandleBinding("t"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"part.tap@s1"}; !slices.Equal(*ran, want) {
		t.Fatalf("child ran %v, want %v", *ran, want)
	}
	// Two hops: the tag's keys go to its parent box, then to the box's
	// first part child: x is the box's, t only the part binds; the part's
	// redirect back to the box ends the chain, so q answers nowhere.
	if err := r.SetFocus("g1"); err != nil {
		t.Fatal(err)
	}
	if got, want := r.KeyRedirects("g1", "normal"), []string{"b1", "s1"}; !slices.Equal(got, want) {
		t.Fatalf("chain %v, want %v", got, want)
	}
	*ran = nil
	for _, k := range []string{"x", "t"} {
		if _, err := r.HandleBinding(k); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"box.poke@b1", "part.tap@s1"}; !slices.Equal(*ran, want) {
		t.Fatalf("two hops ran %v, want %v", *ran, want)
	}
	if handled, _ := r.HandleBinding("q"); handled {
		t.Fatal("q handled through the cycle")
	}
	if err := r.SetFocus("s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LoadBindings([]byte(`{"bindings": [{"context": "part/normal", "bindings": [{"key": "unbound", "unbind": true}]}]}`)); err != nil {
		t.Fatal(err)
	}
	*ran = nil
	if handled, _ := r.HandleBinding("x"); handled || len(*ran) != 0 {
		t.Fatalf("unbound redirect: handled %v ran %v", handled, *ran)
	}
}

// The redirect row is a row: binding list shows it at the focus, and the
// check warns of an unknown relation, another action on the unbound
// pattern and the redirect action on a key (each loads and does nothing).
func TestKeyRedirectRowListedAndChecked(t *testing.T) {
	t.Parallel()
	r, _ := redirectRuntime(t)
	if !slices.ContainsFunc(r.ListBindings("s1"), func(b Binding) bool {
		return b.Pattern == Unbound && b.Request.Key() == KeyRedirectAction
	}) {
		t.Fatal("binding list lacks the redirect row")
	}
	for _, tc := range []struct{ row, want string }{
		{`{"key": "unbound", "action": "ochazuke.key.redirect", "args": {"to": "cousin"}}`, `"cousin" is no relation`},
		{`{"key": "unbound", "action": "part.peek"}`, "never answers"},
		{`{"key": "q", "action": "ochazuke.key.redirect", "args": {"to": "parent"}}`, "only names"},
	} {
		file := `{"bindings": [{"context": "part/normal", "bindings": [` + tc.row + `]}]}`
		got := strings.Join(r.CheckDefaultBindings("t.json", []byte(file)), "\n")
		if !strings.Contains(got, tc.want) {
			t.Fatalf("check %s: %q, want %q", tc.row, got, tc.want)
		}
	}
}
