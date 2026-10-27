package ochazuke

import (
	"slices"
	"testing"
)

// bindings.stops walks every page's stops in drawing order, then app; each
// entry is read at the focus a jump lands on, in the mode it would have
// there, and the live focus and mode are untouched.
func TestStopBindingsAppWide(t *testing.T) {
	r := NewRuntime()
	noop := func(Ctx) Result { return Done(nil) }
	r.Define("row").Verb("open", noop, "Open a row").Mode("normal").Bind("o", Do("row.open"))
	r.Define("other").Verb("kill", noop, "Kill").Mode("normal").Bind("x", Do("other.kill"))
	if _, err := r.AddPage("main", "Main", func(*Runtime, int, int) Block { return Text("main") }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddList("rows", []ListItem{{ID: "r1", Label: "one", Noun: "row"}}, Parent("main")); err != nil {
		t.Fatal(err)
	}
	if err := r.AddInput(NewInput("ask", "", false), Parent("main"), Kind("app.prompt")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("two", "Two", func(*Runtime, int, int) Block { return Text("two") }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddList("others", []ListItem{{ID: "o1", Label: "o", Noun: "other"}}, Parent("two")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("r1"); err != nil {
		t.Fatal(err)
	}
	focus, mode := r.Focus, r.Mode
	value, err := r.Execute(Request{Noun: "app.bindings", Verb: "stops"})
	if err != nil {
		t.Fatal(err)
	}
	stops := value.([]StopBindings)
	var got []string
	for _, s := range stops {
		got = append(got, s.Page+"|"+s.Stop+"|"+s.Focus+"|"+s.Mode)
	}
	want := []string{"main|rows|r1|normal", "main|ask|ask|insert", "two|others|o1|normal", "|app|app|normal"}
	if !slices.Equal(got, want) {
		t.Fatalf("stops = %q, want %q", got, want)
	}
	has := func(s StopBindings, keys, op string) bool {
		return slices.ContainsFunc(s.Bindings, func(b Binding) bool { return b.Keys == keys && b.Request.Key() == op })
	}
	if !has(stops[0], "o", "row.open") || has(stops[0], "x", "other.kill") {
		t.Errorf("rows stop: %+v", stops[0].Bindings)
	}
	if !has(stops[2], "x", "other.kill") || has(stops[2], "o", "row.open") {
		t.Errorf("others stop: %+v", stops[2].Bindings)
	}
	if has(stops[3], "o", "row.open") || !has(stops[3], "}", "app.focus.next") {
		t.Errorf("app entry: %+v", stops[3].Bindings)
	}
	// The prompt is read in insert: printable keys type, they run no verb.
	if has(stops[1], "o", "row.open") {
		t.Errorf("prompt stop answers the row's key: %+v", stops[1].Bindings)
	}
	if r.Focus != focus || r.Mode.Name != mode.Name {
		t.Fatalf("focus/mode moved: %s %s, want %s %s", r.Focus, r.Mode.Name, focus, mode.Name)
	}
	// The listing is the same on every call.
	again, _ := r.Execute(Request{Noun: "app.bindings", Verb: "stops"})
	if len(again.([]StopBindings)) != len(stops) {
		t.Fatal("bindings.stops is not stable")
	}
}
