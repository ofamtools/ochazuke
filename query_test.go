package ochazuke

import (
	"encoding/json"
	"reflect"
	"testing"
)

// queryFixture builds app > sidebar{wt1, grp{wt2}}, main{wt3, note}, added
// out of tree order so the depth-first order is what is asserted.
func queryFixture(t *testing.T) *Runtime {
	t.Helper()
	r := NewRuntime()
	r.Define("panel").Region()
	r.Define("group").Focusable()
	r.Instance("sidebar", Kind("panel"))
	r.Instance("main", Kind("panel"))
	r.Instance("wt3", Kind("worktree"), Parent("main"))
	r.Instance("wt1", Kind("worktree"), Parent("sidebar"))
	r.Instance("grp", Kind("group"), Parent("sidebar"))
	r.Instance("wt2", Kind("worktree"), Parent("grp"))
	if err := r.AddInput(NewInput("note", "Note", false), Parent("main")); err != nil {
		t.Fatal(err)
	}
	return r
}

func queryIDs(r *Runtime, preds ...Predicate) []string {
	out := []string{}
	for _, inst := range r.Query(preds...) {
		out = append(out, inst.ID)
	}
	return out
}

func TestQueryPredicatesAndCombinators(t *testing.T) {
	r := queryFixture(t)
	marked := map[string]bool{"wt2": true, "wt3": true}
	r.RegisterPredicate("marked", func(inst *Instance, _ *Runtime, _ string) bool { return marked[inst.ID] })
	for _, tc := range []struct {
		name  string
		preds []Predicate
		want  []string
	}{
		{"noun in tree order", []Predicate{ByNoun("worktree")}, []string{"wt1", "wt2", "wt3"}},
		{"id", []Predicate{ByID("grp")}, []string{"grp"}},
		{"kind input", []Predicate{ByKind("input")}, []string{"note"}},
		{"kind instance", []Predicate{ByKind("instance"), Within("sidebar")}, []string{"wt1", "grp", "wt2"}},
		{"within is any ancestor", []Predicate{ByNoun("worktree"), Within("sidebar")}, []string{"wt1", "wt2"}},
		{"within excludes self", []Predicate{Within("grp")}, []string{"wt2"}},
		{"child-of is the direct parent", []Predicate{ChildOf("sidebar")}, []string{"wt1", "grp"}},
		{"focusable", []Predicate{IsFocusable()}, []string{"grp", "note"}},
		{"region", []Predicate{IsRegion()}, []string{"sidebar", "main"}},
		{"state matches the global mode", []Predicate{InState("normal"), ByID("wt1")}, []string{"wt1"}},
		{"state other mode", []Predicate{InState("insert")}, []string{}},
		{"any", []Predicate{AnyOf(ByID("wt3"), ByID("wt1"))}, []string{"wt1", "wt3"}},
		{"all", []Predicate{AllOf(ByNoun("worktree"), Within("main"))}, []string{"wt3"}},
		{"not", []Predicate{ByNoun("worktree"), Not(Within("sidebar"))}, []string{"wt3"}},
		{"registered predicate", []Predicate{Where("marked", ""), Within("sidebar")}, []string{"wt2"}},
		{"unknown predicate matches nothing", []Predicate{Where("missing", "")}, []string{}},
	} {
		if got := queryIDs(r, tc.preds...); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
	if err := r.SetMode("insert"); err != nil {
		t.Fatal(err)
	}
	if got := queryIDs(r, InState("insert"), ByID("main")); !reflect.DeepEqual(got, []string{"main"}) {
		t.Fatalf("state follows the mode: %v", got)
	}
}

// instances.query takes the wire form: arrays of [name, arg], nested for
// any, all and not; the result is id, noun and parent per match.
func TestInstancesQueryOperation(t *testing.T) {
	r := queryFixture(t)
	var args Arguments
	if err := json.Unmarshal([]byte(`{"where": [["noun", "worktree"], ["not", [["within", "sidebar"]]]]}`), &args); err != nil {
		t.Fatal(err)
	}
	got, err := r.Execute(Request{Verb: "query", Noun: "ochazuke.instances", Args: args})
	if err != nil {
		t.Fatal(err)
	}
	if want := []QueryMatch{{ID: "wt3", Noun: "worktree", Parent: "main"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("query: %+v", got)
	}
	if err := json.Unmarshal([]byte(`{"where": [["any", [["id", "wt1"], ["id", "grp"]]]]}`), &args); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Execute(Request{Verb: "query", Noun: "ochazuke.instances", Args: args}); err != nil || len(got.([]QueryMatch)) != 2 {
		t.Fatalf("any: %+v %v", got, err)
	}
	for _, raw := range []string{`{"where": [["nope", "x"]]}`, `{"where": [["not"]]}`, `{"where": ["noun"]}`, `{"where": [["noun", 3]]}`, `{"where": "noun"}`} {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Execute(Request{Verb: "query", Noun: "ochazuke.instances", Args: args}); err == nil {
			t.Errorf("%s: expected an error", raw)
		}
	}
	if all, err := r.Execute(Request{Verb: "query", Noun: "ochazuke.instances"}); err != nil || all.([]QueryMatch)[0].ID != "app" {
		t.Fatalf("no predicates lists every instance from app: %v %v", all, err)
	}
}
