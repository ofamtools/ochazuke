package ochazuke

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func overlayFacts(t *testing.T) *Runtime {
	t.Helper()
	r := NewRuntime()
	r.defineFacts(factsSpec{Noun: "fact", Key: "id", Label: "title",
		Extra:  []PropSpec{{Name: "id"}, {Name: "title"}, {Name: "colour"}},
		Routes: map[string]string{"title": "fact.retitle"},
		List: func(Ctx, ListQuery) ([]Thing, error) {
			return []Thing{{"id": "f1", "title": "One"}, {"id": "f2", "title": "Two"}}, nil
		}})
	return r
}

// A fact's props are the overlay's: set keeps the user's value beside the
// fact and get and list merge it; create records a thing of the user's
// own; delete archives (a list hides it). facts.overlay off answers the
// write with a ChoiceAnswer instead.
func TestFactsOverlay(t *testing.T) {
	for _, on := range []bool{true, false} {
		r := overlayFacts(t)
		if err := r.Settings().Set(FactsOverlaySetting, on); err != nil {
			t.Fatal(err)
		}
		_, err := r.Execute(Request{Noun: "fact", Verb: "set", Args: Arguments{"id": "f1", "prop": "colour", "value": "red"}})
		if !on {
			var choice *ChoiceAnswer
			if !errors.As(err, &choice) {
				t.Fatalf("off: %v, want a ChoiceAnswer", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := r.Execute(Request{Noun: "fact", Verb: "get", Args: Arguments{"id": "f1", "prop": "colour"}}); got != "red" {
			t.Fatalf("get colour %v", got)
		}
		if _, err := r.Execute(Request{Noun: "fact", Verb: "create", Args: Arguments{"id": "mine", "colour": "blue"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Execute(Request{Noun: "fact", Verb: "delete", Args: Arguments{"id": "f2"}, Confirmed: true}); err != nil {
			t.Fatal(err)
		}
		got, err := r.Execute(Request{Noun: "fact", Verb: "list"})
		if err != nil {
			t.Fatal(err)
		}
		var ids []any
		for _, row := range got.(ListAnswer).Items {
			ids = append(ids, row["id"])
		}
		if len(ids) != 2 || ids[0] != "f1" || ids[1] != "mine" {
			t.Fatalf("list %v", ids)
		}
	}
}

// A prop a verb changes (Routes, PropSpec.WrittenBy): set runs that verb.
func TestFactsRouteRunsTheOwnerVerb(t *testing.T) {
	r := overlayFacts(t)
	var got, thing any
	r.Register(Operation{Key: "fact.retitle", Parameters: map[string]Parameter{"id": {Type: "string", Noun: "fact"}, "title": {Type: "string"}},
		Run: func(_ *Runtime, _ Context, a Arguments) (any, error) {
			got, thing = a["title"], a["id"]
			return nil, nil
		}})
	if _, err := r.Execute(Request{Noun: "fact", Verb: "set", Args: Arguments{"id": "f2", "prop": "title", "value": "Uno"}}); err != nil {
		t.Fatal(err)
	}
	if got != "Uno" || thing != "f2" {
		t.Fatalf("retitle ran with %v on %v, want Uno on f2", got, thing)
	}
}

// A singleton lists as its one thing.
func TestSingletonLists(t *testing.T) {
	r := NewRuntime()
	got, err := r.Execute(Request{Noun: "ochazuke.snapshot", Verb: "list"})
	if err != nil || len(got.(ListAnswer).Items) != 1 {
		t.Fatalf("snapshot list %v %v", got, err)
	}
}

// A facts noun whose keys hold only in this process (the default) keeps
// its overlay rows for the session; one with stable keys writes them to
// overlay.json.
func TestOverlayFileHoldsStableKeysOnly(t *testing.T) {
	for _, stable := range []bool{false, true} {
		r := NewRuntime()
		r.Config().Dir = t.TempDir()
		r.defineFacts(factsSpec{Noun: "fact", Key: "id", StableKeys: stable, Extra: []PropSpec{{Name: "id"}, {Name: "colour"}},
			List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{{"id": "f1"}}, nil }})
		mustRun(t, r, Request{Noun: "fact", Verb: "set", Args: Arguments{"id": "f1", "prop": "colour", "value": "red"}})
		if got, _ := r.Execute(Request{Noun: "fact", Verb: "get", Args: Arguments{"id": "f1", "prop": "colour"}}); got != "red" {
			t.Fatalf("stable %v: colour %v", stable, got)
		}
		data, _ := os.ReadFile(r.Config().Path(OverlayFile))
		if inFile := strings.Contains(string(data), `"f1"`); inFile != stable {
			t.Fatalf("stable %v: in the file %v: %s", stable, inFile, data)
		}
	}
}
