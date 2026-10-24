package ochazuke

import (
	"slices"
	"testing"
)

// applicabilityFixture: page > row3 (noun row, labelled) > cell (focused),
// and other-row beside it; panel.poke is guarded by "open".
func applicabilityFixture(t *testing.T) (*Runtime, *bool, *[]string) {
	t.Helper()
	r := NewRuntime()
	open := false
	r.DefineGuard("open", func(*Runtime, KeyEvent) bool { return open })
	var acted []string
	r.Register(Operation{Verb: "poke", Noun: "row", Run: func(_ *Runtime, c Context, _ Arguments) (any, error) {
		acted = append(acted, focusOf(c))
		return nil, nil
	}})
	r.Register(Operation{Verb: "delete", Noun: "row", Confirm: "Delete row {{id}} ({{label}})?", Run: func(_ *Runtime, c Context, _ Arguments) (any, error) {
		acted = append(acted, "deleted "+focusOf(c))
		return nil, nil
	}})
	r.Instance("sheet")
	r.Instance("row3", Kind("row"), Parent("sheet")).Label = "Row 3"
	r.Instance("other-row", Kind("row"), Parent("sheet"))
	r.Instance("cell", Parent("row3"))
	if err := r.SetFocus("cell"); err != nil {
		t.Fatal(err)
	}
	return r, &open, &acted
}

// bindings.list (and the hints built from it) leaves out a row whose
// operation is guarded off at the focus.
func TestBindingRowsHideGuardedOffVerbs(t *testing.T) {
	r, open, _ := applicabilityFixture(t)
	r.Define("cell").Mode("normal").Bind("z", Do("row.poke"))
	guardVerb(r, "row.poke", "open")
	listed := func() bool {
		return slices.ContainsFunc(r.ListBindings(r.Focus), func(b Binding) bool { return b.Keys == "z" })
	}
	if listed() {
		t.Fatal("a guarded-off verb's row is listed")
	}
	*open = true
	if !listed() {
		t.Fatal("the row is listed once its guard holds")
	}
}

// A verb guarded off is refused from the API, the CLI and keys, and the
// refusal is the request's error in history; unguarded verbs are untouched.
func TestExecuteRefusesGuardedOffVerb(t *testing.T) {
	r, open, acted := applicabilityFixture(t)
	guardVerb(r, "row.poke", "open")
	if _, err := r.Execute(Request{Verb: "poke", Noun: "row"}); err == nil || err.Error() != "row.poke is not applicable now" {
		t.Fatalf("api: %v", err)
	}
	reply := make(chan Response, 1)
	r.HandleCall(CallMsg{Request: Request{Verb: "poke", Noun: "row"}, Reply: reply})
	if resp := <-reply; resp.Error != "row.poke is not applicable now" {
		t.Fatalf("cli: %+v", resp)
	}
	if e := r.History(1)[0]; e.Result.OK || e.Context.Source != "cli" {
		t.Fatalf("history: %+v", e)
	}
	if len(*acted) != 0 {
		t.Fatalf("ran: %v", *acted)
	}
	*open = true
	if _, err := r.Execute(Request{Verb: "poke", Noun: "row"}); err != nil || len(*acted) != 1 {
		t.Fatalf("guard holds: %v %v", err, *acted)
	}
	if _, err := r.Execute(Request{Verb: "ignore", Noun: "key"}); err != nil {
		t.Fatal(err)
	}
}

// A request without a target acts for the nearest instance of its noun on
// the focus path; its confirm names that instance ({{id}}, {{label}}); an
// explicit target is kept; a binding row's own request keeps the row rule.
func TestRequestAddressesItsNounOnTheFocusPath(t *testing.T) {
	r, _, acted := applicabilityFixture(t)
	if _, err := r.Execute(Request{Verb: "poke", Noun: "row"}); err != nil || (*acted)[0] != "row3" {
		t.Fatalf("poke from the cell: %v %v", err, *acted)
	}
	if _, err := r.Execute(Request{Verb: "delete", Noun: "row"}); err != nil {
		t.Fatal(err)
	}
	confirm := r.Instances[r.TopModal()]
	if confirm == nil || confirm.Payload.Prompt != "Delete row row3 (Row 3)?" || confirm.Payload.Request.Target != "row3" {
		t.Fatalf("confirm: %+v", confirm)
	}
	if _, err := r.Execute(Request{Verb: "accept", Noun: "confirm"}); err != nil || (*acted)[1] != "deleted row3" {
		t.Fatalf("accept: %v %v", err, *acted)
	}
	if _, err := r.Execute(Request{Verb: "poke", Noun: "row", Target: "other-row"}); err != nil || (*acted)[2] != "other-row" {
		t.Fatalf("explicit target: %v %v", err, *acted)
	}
	r.Define("app").Mode("normal").Bind("v", Do("row.poke"))
	press(t, r, "v")
	if (*acted)[3] != "cell" {
		t.Fatalf("an app row's shared request keeps the focus: %v", *acted)
	}
	// A list row's label is its instance label.
	r2 := NewRuntime()
	r2.Define("item")
	if _, err := r2.AddList("things", []ListItem{{ID: "a", Label: "Alpha", Noun: "item"}}); err != nil {
		t.Fatal(err)
	}
	if r2.Instances["a"].Label != "Alpha" {
		t.Fatalf("list row label: %q", r2.Instances["a"].Label)
	}
}
