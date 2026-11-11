package ochazuke

import (
	"fmt"
	"testing"
)

func TestCanonicalSelectorKeepsReferencesAcrossMove(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Config().Disable()
	r.SetRelationStore(NewMemRelations())
	thing := Thing{"id": "stable-1", "name": "Plan", "file": "/before/plan.txt"}
	note := Thing{"id": "n1", "about": "document:/before/plan.txt"}
	r.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "about", Parameter: Parameter{Noun: AnyNoun}}).Store(Store{
		List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{note}, nil },
		Patch: func(_ Ctx, _ string, changes Thing) (Thing, error) {
			for k, v := range changes {
				note[k] = v
			}
			return note, nil
		},
	})
	r.Define("document").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "name", Role: RoleLabel},
		PropSpec{Name: "file", Human: true, Parameter: Parameter{Type: PropPath, Path: "file"}},
	).Store(Store{
		Undo: true,
		Get: func(_ Ctx, key string) (Thing, error) {
			if key != thing["id"] && key != thing["file"] {
				return nil, fmt.Errorf("not found: %s", key)
			}
			return thing, nil
		},
		Patch: func(_ Ctx, key string, changes Thing) (Thing, error) {
			if key != "stable-1" {
				return nil, fmt.Errorf("patch did not use durable key: %s", key)
			}
			for k, v := range changes {
				thing[k] = v
			}
			return thing, nil
		},
		CanonicalKey: func(c Ctx, selector, key string) error { return r.RekeyReferences(c, "document", selector, key) },
	})
	if _, err := r.Relate(Ctx{Runtime: r}, Relation{From: "project:p1", To: "document:/before/plan.txt", Text: "kept"}); err != nil {
		t.Fatal(err)
	}
	value, err := r.Execute(Request{Noun: "document", Verb: "set", Args: Arguments{"id": "/before/plan.txt", "props": map[string]any{"file": "/after/plan.txt"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := value.(map[string]any); got["id"] != "stable-1" || got["file"] != "/after/plan.txt" {
		t.Fatalf("write result: %v", got)
	}
	if note["about"] != "document:stable-1" {
		t.Fatalf("note reference stranded: %v", note)
	}
	rows, err := r.RelationsOf(RelationQuery{Thing: "document:stable-1"})
	if err != nil || len(rows) != 1 || rows[0].Text != "kept" {
		t.Fatalf("relations: %v %v", rows, err)
	}
	if _, err := r.Execute(Request{Noun: "document", Verb: "get", Args: Arguments{"id": "/before/plan.txt"}}); err == nil {
		t.Fatal("stale path remained a selector")
	}
	if _, err := r.Execute(Request{Noun: "document", Verb: "get", Args: Arguments{"id": "/after/plan.txt"}}); err != nil {
		t.Fatal(err)
	}
}
