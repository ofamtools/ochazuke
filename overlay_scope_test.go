package ochazuke

import (
	"errors"
	"slices"
	"testing"
)

// overlayStep runs q through ExecuteOutcome and fails unless it is done.
func overlayStep(t *testing.T, r *Runtime, q Request) Outcome {
	t.Helper()
	o := r.ExecuteOutcome(q)
	if o.Kind != OutcomeDone {
		t.Fatalf("%s.%s %v: %s/%s %s", q.Noun, q.Verb, q.Args, o.Kind, o.Cause, o.Reason)
	}
	return o
}

// overlayUndo undoes the newest change; it must be done.
func overlayUndo(t *testing.T, r *Runtime) {
	t.Helper()
	overlayStep(t, r, Request{Noun: "ochazuke.history-entry", Verb: "undo"})
}

// overlayProp is one prop of a thing read back through get.
func overlayProp(t *testing.T, r *Runtime, noun string, args Arguments, prop string) any {
	t.Helper()
	q := Request{Noun: noun, Verb: "get", Args: Arguments{"prop": prop}}
	for k, v := range args {
		q.Args[k] = v
	}
	v, err := r.Execute(q)
	if err != nil {
		t.Fatalf("get %s.%s: %v", noun, prop, err)
	}
	return v
}

// overlayKeys are the keys (prop key) of the things list answers, in order.
func overlayKeys(t *testing.T, r *Runtime, noun, key string) []string {
	t.Helper()
	v, err := r.Execute(Request{Noun: noun, Verb: "list"})
	if err != nil {
		t.Fatalf("list %s: %v", noun, err)
	}
	var out []string
	for _, row := range v.(ListAnswer).Items {
		s, _ := row[key].(string)
		out = append(out, s)
	}
	return out
}

// A collection store that reads things and writes none (no Patch, Create
// or Delete; not a fact): the overlay fills set, create, archive and
// delete, each read back through get and list, each undone.
func TestOverlayFillsARecordStore(t *testing.T) {
	r := NewRuntime()
	r.Config().Dir = t.TempDir()
	things := []Thing{{"id": "a", "name": "Original", "title": "A"}, {"id": "b", "name": "Bee", "title": "B"}}
	r.Define("shelf").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "name"},
		PropSpec{Name: "title", Role: RoleLabel, ReadOnly: true},
	).Store(Store{
		Get: func(c Ctx, id string) (Thing, error) {
			for _, th := range things {
				if th["id"] == id {
					return Thing{"id": th["id"], "name": th["name"], "title": th["title"]}, nil
				}
			}
			return nil, c.Runtime.NotFound("shelf", id, nil)
		},
		List: func(Ctx, ListQuery) ([]Thing, error) {
			out := make([]Thing, len(things))
			for i, th := range things {
				out[i] = Thing{"id": th["id"], "name": th["name"], "title": th["title"]}
			}
			return out, nil
		},
	})
	a := Arguments{"id": "a"}
	overlayStep(t, r, Request{Noun: "shelf", Verb: "set", Args: Arguments{"id": "a", "prop": "name", "value": "Mine"}})
	if got := overlayProp(t, r, "shelf", a, "name"); got != "Mine" {
		t.Fatalf("set: name %v", got)
	}
	overlayUndo(t, r)
	if got := overlayProp(t, r, "shelf", a, "name"); got != "Original" {
		t.Fatalf("set undone: name %v", got)
	}

	overlayStep(t, r, Request{Noun: "shelf", Verb: "create", Args: Arguments{"id": "mine", "name": "New"}})
	if got := overlayProp(t, r, "shelf", Arguments{"id": "mine"}, "name"); got != "New" {
		t.Fatalf("create: name %v", got)
	}
	if got := overlayKeys(t, r, "shelf", "id"); !slices.Equal(got, []string{"a", "b", "mine"}) {
		t.Fatalf("create: list %v", got)
	}
	overlayUndo(t, r)
	if got := overlayKeys(t, r, "shelf", "id"); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("create undone: list %v", got)
	}

	for _, verb := range []string{"archive", "delete"} {
		overlayStep(t, r, Request{Noun: "shelf", Verb: verb, Args: Arguments{"id": "b"}, Confirmed: true})
		if got := overlayKeys(t, r, "shelf", "id"); !slices.Equal(got, []string{"a"}) {
			t.Fatalf("%s: list %v", verb, got)
		}
		if got := overlayProp(t, r, "shelf", Arguments{"id": "b"}, "name"); got != "Bee" {
			t.Fatalf("%s: the source keeps b (name %v)", verb, got)
		}
		overlayUndo(t, r)
		if got := overlayKeys(t, r, "shelf", "id"); !slices.Equal(got, []string{"a", "b"}) {
			t.Fatalf("%s undone: list %v", verb, got)
		}
	}
}

// A singleton fact with no write of its own: set holds a prop (a computed
// one shows the value written), create is its set, archive marks the one
// thing (its list is empty), delete resets the user's changes; each is
// read back and undone.
func TestOverlayFillsASingletonFact(t *testing.T) {
	r := NewRuntime()
	r.Config().Dir = t.TempDir()
	r.Define("gauge").Props(
		PropSpec{Name: "colour", ReadOnly: true},
		PropSpec{Name: "rights", ReadOnly: true, Read: func(Ctx, Thing) (any, error) { return "system", nil }},
	).Store(Store{Kind: Singleton, Facts: true, Get: func(Ctx, string) (Thing, error) { return Thing{"colour": "grey"}, nil }})
	none := Arguments{}
	overlayStep(t, r, Request{Noun: "gauge", Verb: "set", Args: Arguments{"props": map[string]any{"colour": "red", "rights": "mine"}}})
	if got, rights := overlayProp(t, r, "gauge", none, "colour"), overlayProp(t, r, "gauge", none, "rights"); got != "red" || rights != "mine" {
		t.Fatalf("set: colour %v rights %v", got, rights)
	}
	overlayUndo(t, r)
	if got, rights := overlayProp(t, r, "gauge", none, "colour"), overlayProp(t, r, "gauge", none, "rights"); got != "grey" || rights != "system" {
		t.Fatalf("set undone: colour %v rights %v", got, rights)
	}

	overlayStep(t, r, Request{Noun: "gauge", Verb: "create", Args: Arguments{"colour": "blue"}})
	if got := overlayProp(t, r, "gauge", none, "colour"); got != "blue" {
		t.Fatalf("create (as set): colour %v", got)
	}
	overlayUndo(t, r)
	if got := overlayProp(t, r, "gauge", none, "colour"); got != "grey" {
		t.Fatalf("create undone: colour %v", got)
	}

	overlayStep(t, r, Request{Noun: "gauge", Verb: "archive"})
	if got := overlayKeys(t, r, "gauge", "colour"); len(got) != 0 {
		t.Fatalf("archive: list %v", got)
	}
	overlayUndo(t, r)
	if got := overlayKeys(t, r, "gauge", "colour"); len(got) != 1 {
		t.Fatalf("archive undone: list %v", got)
	}

	overlayStep(t, r, Request{Noun: "gauge", Verb: "set", Args: Arguments{"prop": "colour", "value": "green"}})
	overlayStep(t, r, Request{Noun: "gauge", Verb: "delete", Confirmed: true})
	if got := overlayProp(t, r, "gauge", none, "colour"); got != "grey" {
		t.Fatalf("delete (reset): colour %v", got)
	}
	overlayUndo(t, r)
	if got := overlayProp(t, r, "gauge", none, "colour"); got != "green" {
		t.Fatalf("reset undone: colour %v", got)
	}
	if o := r.ExecuteOutcome(Request{Noun: "gauge", Verb: "delete", Confirmed: true}); o.Kind != OutcomeDone {
		t.Fatalf("reset of a change: %s %s", o.Kind, o.Reason)
	}
	if o := r.ExecuteOutcome(Request{Noun: "gauge", Verb: "delete", Confirmed: true}); o.Kind != OutcomeNothing {
		t.Fatalf("reset with no changes: %s %s, want nothing", o.Kind, o.Reason)
	}
}

// overlay.stores: all (the default) writes for every store it fills;
// facts writes for system facts only, and a write on any other store
// answers the verbs that change it (a ChoiceAnswer), its value unchanged.
func TestOverlayStoresSetting(t *testing.T) {
	for _, scope := range []string{OverlayStoresAll, NounKindFacts} {
		r := overlayFacts(t)
		r.Define("shelf").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name"}).Store(Store{
			Get: func(Ctx, string) (Thing, error) { return Thing{"id": "a", "name": "Original"}, nil },
		})
		if err := r.Settings().Set(OverlayStoresSetting, scope); err != nil {
			t.Fatal(err)
		}
		_, err := r.Execute(Request{Noun: "shelf", Verb: "set", Args: Arguments{"id": "a", "prop": "name", "value": "Mine"}})
		var choice *ChoiceAnswer
		want := "Mine"
		if scope == NounKindFacts {
			want = "Original"
			if !errors.As(err, &choice) {
				t.Fatalf("%s: %v, want a ChoiceAnswer", scope, err)
			}
		} else if err != nil {
			t.Fatalf("%s: %v", scope, err)
		}
		if got := overlayProp(t, r, "shelf", Arguments{"id": "a"}, "name"); got != want {
			t.Fatalf("%s: name %v, want %v", scope, got, want)
		}
		overlayStep(t, r, Request{Noun: "fact", Verb: "set", Args: Arguments{"id": "f1", "prop": "colour", "value": "red"}})
	}
}
