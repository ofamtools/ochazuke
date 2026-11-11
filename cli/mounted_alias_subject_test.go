package cli

import (
	"encoding/json"
	"fmt"
	gomaps "maps"
	"testing"

	"ofam.dev/ochazuke"
)

// aliasNounMount mounts an extension with two stored nouns over the same
// things, ticket and verdict (a ticket seen as a verdict): the ticket verb
// approve takes an optional target of its own noun and is named on verdict
// by an alias, as a tool's page of one noun runs the other noun's verbs.
// The host's proxy records the row each call ran on and its arguments.
func aliasNounMount(t *testing.T) (*ochazuke.Runtime, *[]string) {
	t.Helper()
	ext := ochazuke.NewRuntime()
	ext.Config().Disable()
	thing := ochazuke.Thing{"id": "t1", "title": "Ship it"}
	store := ochazuke.Store{
		Get: func(ochazuke.Ctx, string) (ochazuke.Thing, error) { return gomaps.Clone(thing), nil },
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			return []ochazuke.Thing{gomaps.Clone(thing)}, nil
		},
	}
	props := []ochazuke.PropSpec{{Name: "id", Key: true}, {Name: "title", Role: ochazuke.RoleLabel}}
	ext.Define("verdict").Props(props...).Store(store)
	ext.Define("ticket").Props(props...).Store(store).
		Verb("approve", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "Approve the ticket").
		Params(map[string]ochazuke.Parameter{"target": {Type: "string", Noun: "ticket", Source: "ticket.list", Description: "Ticket; omitted uses the selected row"}}).
		AliasOn("verdict", "approve")
	raw, err := Catalog(ext, "extension", "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	calls := new([]string)
	mounted := MountNouns(r, doc.AppNouns(), HostMount{
		Local: func(noun string) string { return "extension." + noun },
		Proxy: func(noun string, verb CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				if verb.Name == "list" {
					return ochazuke.Done([]ochazuke.Thing{gomaps.Clone(thing)})
				}
				// The host's proxy sends the row the verb ran on as its
				// target, as a mount does.
				*calls = append(*calls, fmt.Sprintf("%s.%s on %s %v %v", noun, verb.Name, c.Instance.ID, c.Instance.Data, c.Args["target"]))
				return ochazuke.Done(nil)
			}
		},
	})
	if len(mounted.Warnings) != 0 {
		t.Fatal(mounted.Warnings)
	}
	if _, ok := r.ActionAlias("extension.verdict.approve"); !ok {
		t.Fatal("the mount left out the alias on the other noun")
	}
	if _, err := r.AddList("verdicts", []ochazuke.ListItem{{ID: "verdicts/t1", Noun: "extension.verdict", Label: "Ship it", Data: "verdict/t1"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddInput(ochazuke.NewInput("elsewhere", "", false)); err != nil {
		t.Fatal(err)
	}
	if err := r.Bind(
		ochazuke.Binding{Noun: "extension.verdict", Mode: "normal", Keys: "a", Request: ochazuke.Request{Noun: "extension.ticket", Verb: "approve", Via: "extension.verdict.approve"}},
		ochazuke.Binding{Noun: "app", Mode: "normal", Keys: "A", Request: ochazuke.Request{Noun: "extension.ticket", Verb: "approve"}},
	); err != nil {
		t.Fatal(err)
	}
	return r, calls
}

// A key on a focused mounted row acts on that row when the verb it runs
// belongs to another mounted noun that the row's noun names by an alias:
// the row stands for the verb's subject, so nothing is asked. v0.124.0
// opened "Run verdict.approve" asking for the target; v0.123.0 ran the
// verb on the row.
func TestMountedAliasNounRowSuppliesTheSubject(t *testing.T) {
	for _, ask := range []bool{true, false} {
		t.Run(fmt.Sprintf("subject.ask=%v", ask), func(t *testing.T) {
			r, calls := aliasNounMount(t)
			if err := r.Settings().Set(ochazuke.SubjectAskSetting, ask); err != nil {
				t.Fatal(err)
			}
			if err := r.SetFocus("verdicts/t1"); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"a", "A"} {
				*calls = nil
				if _, err := r.HandleBinding(key); err != nil {
					t.Fatalf("key %s: %v", key, err)
				}
				if err := Settle(r, nil); err != nil {
					t.Fatal(err)
				}
				want := "ticket.approve on verdicts/t1 verdict/t1 <nil>"
				if r.TopModal() != "" || len(*calls) != 1 || (*calls)[0] != want {
					t.Fatalf("key %s on the focused row: modal %q, calls %v, want [%s]", key, r.TopModal(), *calls, want)
				}
			}
		})
	}
}

// Away from any row of the verb's noun or of a noun that names it, the same
// verb still asks for its target (the where-from ask), and with subject.ask
// off it runs as it did before v0.124.0.
func TestMountedAliasNounVerbAsksAwayFromItsRows(t *testing.T) {
	r, calls := aliasNounMount(t)
	if err := r.SetFocus("elsewhere"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetMode("normal"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.HandleBinding("A"); err != nil {
		t.Fatal(err)
	}
	if r.RecordEditor(ochazuke.ArgsFormID) == nil || len(*calls) != 0 {
		t.Fatalf("away from its rows the verb did not ask: modal %q, calls %v", r.TopModal(), *calls)
	}
}
