package ochazuke

import (
	"slices"
	"testing"
)

// reachFixture: a terminal names panes; a pane names a project (and a peer
// pane); a project names nothing.
func reachFixture(t *testing.T, panes ...Thing) *Runtime {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	add := func(noun string, extra []PropSpec, things ...Thing) {
		table := NewMemDocTable(r, noun)
		props := append([]PropSpec{{Name: "id", Key: true}, {Name: "title", Role: RoleLabel}}, extra...)
		r.Define(noun).Props(props...).Store(DocStore(table, noun))
		for _, thing := range things {
			if err := table.Put(thing["id"].(string), thing); err != nil {
				t.Fatal(err)
			}
		}
	}
	add("project", nil, Thing{"id": "p1", "title": "Home"}, Thing{"id": "p2", "title": "Work"})
	add("pane", []PropSpec{
		{Name: "project", Parameter: Parameter{Noun: "project"}},
		{Name: "peer", Parameter: Parameter{Noun: "pane"}},
	}, panes...)
	r.Define("terminal").Props(PropSpec{Name: "pane", Association: true, Parameter: Parameter{Noun: "pane"}})
	r.Instance("term", Kind("terminal")).Data = map[string]any{"pane": "a"}
	if err := r.SetFocus("term"); err != nil {
		t.Fatal(err)
	}
	return r
}

func reachKeys(r *Runtime, noun string) []string {
	id, things := r.nearestOf(noun, "term")
	keys := []string{}
	if id != "" {
		keys = append(keys, id)
	}
	for _, t := range things {
		keys = append(keys, t.Key)
	}
	slices.Sort(keys)
	return keys
}

func TestReachDepth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		depth int
		want  []string
	}{{0, []string{}}, {1, []string{}}, {2, []string{"p1"}}, {3, []string{"p1"}}} {
		r := reachFixture(t, Thing{"id": "a", "title": "A", "project": "p1"})
		if err := r.Settings().Set(KeysGraphDepthSetting, tc.depth); err != nil {
			t.Fatal(err)
		}
		if got := reachKeys(r, "project"); !slices.Equal(got, tc.want) {
			t.Errorf("depth %d: %v, want %v", tc.depth, got, tc.want)
		}
		if got := reachKeys(r, "pane"); (tc.depth == 0) != (len(got) == 0) {
			t.Errorf("depth %d: pane %v", tc.depth, got)
		}
	}
	if r := reachFixture(t, Thing{"id": "a", "project": "p1"}); r.Settings().Int(KeysGraphDepthSetting) != 3 {
		t.Fatal("default depth is not 3")
	}
}

func TestReachCyclesEnd(t *testing.T) {
	t.Parallel()
	r := reachFixture(t,
		Thing{"id": "a", "peer": "b", "project": "p1"},
		Thing{"id": "b", "peer": "a"},
		Thing{"id": "c", "peer": "c"})
	if err := r.Settings().Set(KeysGraphDepthSetting, 50); err != nil {
		t.Fatal(err)
	}
	if got := reachKeys(r, "pane"); len(got) == 0 {
		t.Fatalf("pane: %v", got)
	}
	if n := len(r.reachFrom("term")); n != 3 { // pane a, pane b, project p1
		t.Fatalf("reached %d things, want 3", n)
	}
	r.Instances["term"].Data = map[string]any{"pane": "c"}
	if n := len(r.reachFrom("term")); n != 1 {
		t.Fatalf("self reference reached %d things, want 1", n)
	}
}

func TestReachKeepsEveryProject(t *testing.T) {
	t.Parallel()
	r := reachFixture(t, Thing{"id": "a", "project": "p1"}, Thing{"id": "b", "project": "p2"})
	r.Instances["term"].Data = map[string]any{"pane": []any{"a", "b"}}
	if got := reachKeys(r, "project"); !slices.Equal(got, []string{"p1", "p2"}) {
		t.Fatalf("projects %v", got)
	}
	// A store change is seen at once, not after the cached walk.
	r.Instances["term"].Data = map[string]any{"pane": "a"}
	if got := reachKeys(r, "project"); !slices.Equal(got, []string{"p1"}) {
		t.Fatalf("one project %v", got)
	}
	if _, err := r.Noun("pane").Store.Patch(r.reachCtx("pane"), "a", Thing{"project": "p2"}); err != nil {
		t.Fatal(err)
	}
	if got := reachKeys(r, "project"); !slices.Equal(got, []string{"p2"}) {
		t.Fatalf("after the pane moved: %v", got)
	}
}
