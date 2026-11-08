package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

// factNouns are the system facts: thing nouns over the registry, read-only.
var factNouns = []string{
	"ochazuke.modes", "ochazuke.operation", "ochazuke.noun", "ochazuke.theme", "ochazuke.glyph", "ochazuke.hook",
	"ochazuke.event", "ochazuke.actions", "ochazuke.bindings", "ochazuke.association", "ochazuke.notice", "ochazuke.segment",
}

func factsRuntime(t *testing.T) *Runtime {
	t.Helper()
	r := NewRuntime()
	NewModel(r)
	return r
}

// Each system fact is a thing noun with a facts store: a key prop (also its
// human key), list and get standard, no base times; create, set and delete
// are on the noun but unavailable, "a system fact", and refuse with it.
func TestSystemFactsAreReadOnlyThingNouns(t *testing.T) {
	r := factsRuntime(t)
	for _, name := range factNouns {
		n := r.Noun(name)
		if n == nil || n.Store == nil || !n.Store.Facts {
			t.Fatalf("%s: no facts store", name)
		}
		key := n.keyProp()
		if key == nil || !key.Human {
			t.Fatalf("%s: key %+v", name, key)
		}
		if n.prop("created") != nil || n.prop("updated") != nil {
			t.Fatalf("%s: a facts store has no times", name)
		}
		for _, verb := range []string{"list", "get"} {
			if op := r.Operations.At(Key(name + "." + verb)); op.Standard != verb || op.Unavailable != "" {
				t.Fatalf("%s.%s: standard %q unavailable %q", name, verb, op.Standard, op.Unavailable)
			}
		}
		for _, verb := range []string{"create", "set", "delete"} {
			op, ok := r.Operations.Find(Key(name + "." + verb))
			if !ok || op.Unavailable != FactReason || FactReason != "a system fact" {
				t.Fatalf("%s.%s: present %v unavailable %q", name, verb, ok, op.Unavailable)
			}
			if r.Available(Request{Noun: name, Verb: verb}, "", r.Mode) {
				t.Fatalf("%s.%s is available", name, verb)
			}
			if _, err := r.Execute(Request{Noun: name, Verb: verb, Confirmed: true}); err == nil || !strings.Contains(err.Error(), "a system fact") {
				t.Fatalf("%s.%s: %v", name, verb, err)
			}
		}
	}
}

// list answers rows with an id and get reads one by its key; the modes are
// the registered ones, pending flagged.
func TestSystemFactsListAndGet(t *testing.T) {
	r := factsRuntime(t)
	for _, name := range factNouns {
		got, err := r.Execute(Request{Noun: name, Verb: "list"})
		if err != nil {
			t.Fatalf("%s.list: %v", name, err)
		}
		for _, row := range AnswerRows(got).([]map[string]any) {
			if row["id"] == nil || row["id"] == "" {
				t.Fatalf("%s.list: row without an id: %v", name, row)
			}
		}
	}
	rows, err := r.Execute(Request{Noun: "ochazuke.modes", Verb: "list", Args: Arguments{"where": "pending=true"}})
	if err != nil {
		t.Fatal(err)
	}
	var pending []string
	for _, row := range AnswerRows(rows).([]map[string]any) {
		pending = append(pending, row["id"].(string))
	}
	if !slices.Contains(pending, quitWaitMode) {
		t.Fatalf("pending modes %v", pending)
	}
	got, err := r.Execute(Request{Noun: "ochazuke.modes", Verb: "get", Args: Arguments{"name": "normal"}})
	if err != nil || got.(map[string]any)["name"] != "normal" {
		t.Fatalf("modes get normal: %v %v", got, err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.modes", Verb: "get", Args: Arguments{"name": "nope"}}); err == nil {
		t.Fatal("get of an unknown mode")
	}
	ops, err := r.Execute(Request{Noun: "ochazuke.operation", Verb: "get", Args: Arguments{"key": "ochazuke.modes.list", "prop": "standard"}})
	if err != nil || ops != "list" {
		t.Fatalf("operations get standard: %v %v", ops, err)
	}
}

// Choices of a noun parameter come from the noun's list verb through the
// standard verb table, not from a name built by hand.
func TestChoicesReadTheStandardListVerb(t *testing.T) {
	r := factsRuntime(t)
	if got := r.ChoicesSource(Parameter{Noun: "ochazuke.modes"}); got != "ochazuke.modes.list" {
		t.Fatalf("choices source %q", got)
	}
	rows, err := r.Choices(r.Context(), Parameter{Noun: "ochazuke.modes"})
	if err != nil || !slices.ContainsFunc(rows, func(c Choice) bool { return c.ID == "normal" }) {
		t.Fatalf("choices %v %v", rows, err)
	}
}
