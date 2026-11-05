package ochazuke

import (
	"fmt"
	"testing"
)

func TestUndoReachesChangeUnderAppVerb(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Config().Disable()
	_ = NewModel(r)
	data := map[string]Thing{"a": {"id": "a", "title": "A", "status": "todo"}, "b": {"id": "b", "title": "B", "status": "todo"}}
	calls := []string{}
	r.Define("thing").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}, PropSpec{Name: "status"}).Store(Store{Undo: true,
		Get: func(_ Ctx, id string) (Thing, error) {
			calls = append(calls, "Get "+id)
			if d, ok := data[id]; ok {
				c := Thing{}
				for k, v := range d {
					c[k] = v
				}
				return c, nil
			}
			return nil, fmt.Errorf("no %q", id)
		},
		List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{data["a"], data["b"]}, nil },
		Create: func(_ Ctx, n Thing) (Thing, error) {
			calls = append(calls, fmt.Sprintf("Create %v", n))
			return n, nil
		},
		Patch: func(_ Ctx, id string, ch Thing) (Thing, error) {
			calls = append(calls, fmt.Sprintf("Patch %q %v", id, ch))
			if d, ok := data[id]; ok {
				for k, v := range ch {
					d[k] = v
				}
				return d, nil
			}
			return nil, fmt.Errorf("no %q", id)
		},
		Delete: func(_ Ctx, id string) error {
			calls = append(calls, fmt.Sprintf("Delete %q", id))
			delete(data, id)
			return nil
		},
	}).
		Verb("noop", func(c Ctx) Result { return Done(nil) }, "an app verb that writes nothing").
		Verb("toggle", func(c Ctx) Result {
			_, err := c.Runtime.ExecuteAt(c.Context, Request{Noun: "thing", Verb: "set", Args: Arguments{"id": "a", "changes": map[string]any{"status": "done"}}})
			if err != nil {
				return Failed(err)
			}
			return Done(nil)
		}, "app verb running set").
		Verb("choose", func(c Ctx) Result {
			_, err := c.Runtime.OpenChooser(ChooserSpec{Kind: "st", Title: "st", Rows: []ChooserRow{{ID: "done", Label: "done"}}, Choose: func(row ChooserRow) Result {
				_, err := c.Runtime.Execute(Request{Noun: "thing", Verb: "set", Args: Arguments{"id": "a", "changes": map[string]any{"status": row.ID}}})
				if err != nil {
					return Failed(err)
				}
				return Done(nil)
			}})
			if err != nil {
				return Failed(err)
			}
			return Done(nil)
		}, "app verb opening chooser").Mode("normal").
		Bind("x", Do("thing.toggle")).Bind("s", Do("thing.choose")).Bind("u", Do("ochazuke.history.undo"))
	r.Instance("row", Kind("thing")).Data = "a"
	if err := r.SetFocus("row"); err != nil {
		t.Fatal(err)
	}

	undo := func(label string) {
		calls = nil
		v, err := r.HandleBinding("u")
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		t.Logf("%s: undo v=%v calls=%v status=%v keys=%d", label, v, calls, data["a"]["status"], len(data))
	}
	_, err := r.Execute(Request{Noun: "thing", Verb: "set", Args: Arguments{"id": "a", "changes": map[string]any{"title": "A2"}}})
	t.Logf("set err=%v", err)
	_, err = r.Execute(Request{Noun: "thing", Verb: "noop"})
	t.Logf("noop err=%v", err)
	undo("after noop app verb")
	t.Logf("title=%v data=%v", data["a"]["title"], data)
	if data["a"]["title"] != "A" {
		t.Fatalf("no-op blocked title undo: %v", data["a"])
	}

	_, err = r.Execute(Request{Noun: "thing", Verb: "set", Args: Arguments{"id": "a", "changes": map[string]any{"title": "A3"}}})
	_, err = r.HandleBinding("x")
	if err != nil || data["a"]["status"] != "done" {
		t.Fatalf("x did not write status: %v %v", err, data["a"])
	}
	undo("after app verb that ran set")
	if data["a"]["status"] != "todo" {
		t.Errorf("undo after an app verb that ran set: status %v, want todo", data["a"]["status"])
	}
	t.Logf("data=%v", data["a"])

	data["a"]["status"] = "todo"
	_, err = r.HandleBinding("s")
	if err != nil || r.TopModal() == "" {
		t.Fatalf("s did not open chooser: %v %s", err, r.TopModal())
	}
	_, err = r.HandleBinding("enter")
	if err != nil || data["a"]["status"] != "done" {
		t.Fatalf("chooser pick did not write status: %v %v", err, data["a"])
	}
	undo("after chooser pick")
	if data["a"]["status"] != "todo" {
		t.Errorf("undo after chooser pick: status %v, want todo", data["a"]["status"])
	}
	t.Logf("data=%v", data["a"])
}
