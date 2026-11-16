package ochazuke

import (
	"strings"
	"testing"
)

// A verb whose change a fixed thing keeps itself from (outline.delete on
// the root, Fixed delete) answers nothing / kept by every route: no error,
// nothing written, the node and the focus where they were, the reason as
// information.
func TestFixedKeptFromEveryRoute(t *testing.T) {
	r, _, tree, writes := fixedTree(t)
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	for _, source := range []RequestSource{"key:d d", RequestFromPalette, RequestFromCLI, RequestFromMCP} {
		restore := r.forceSource(string(source))
		o := r.ExecuteOutcome(Request{Noun: "ochazuke.outline", Verb: "delete"})
		restore()
		if o.Kind != OutcomeNothing || o.Cause != CauseKept || o.Err != nil {
			t.Fatalf("%s: outcome %+v", source, o)
		}
		if len(*writes) != 0 || len(tree.Nodes()) != 4 || r.Focus != "a" {
			t.Fatalf("%s: writes %v nodes %d focus %q", source, *writes, len(tree.Nodes()), r.Focus)
		}
		msgs := sink.Messages()
		if last := msgs[len(msgs)-1]; last.Kind != UIInfo || !strings.Contains(last.Text, `kept: "alpha" refuses deletion`) {
			t.Fatalf("%s: status %+v", source, last)
		}
	}
}

// A store thing keeps itself by its RoleFixed prop: the standard delete's
// key is not offered on its row (offered on a free one), and delete naming
// it by key answers nothing / kept with the thing's own reason, the store
// untouched.
func TestFixedRolePropKeepsAStoreThing(t *testing.T) {
	r := NewRuntime()
	things := map[string]Thing{
		"inbox": {"id": "inbox", "label": "Inbox", "fixed": map[string]any{TreeDelete: "the inbox is where new notes land"}},
		"misc":  {"id": "misc", "label": "Misc"},
	}
	deleted := []string{}
	r.Define("folder").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "label", Role: RoleLabel},
		PropSpec{Name: "fixed", Role: RoleFixed, ReadOnly: true, Parameter: Parameter{Type: PropObject}},
	).Store(Store{
		Kind: KindCollection,
		Get:  func(_ Ctx, id string) (Thing, error) { return things[id], nil },
		List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{things["inbox"], things["misc"]}, nil },
		Delete: func(_ Ctx, id string) error {
			deleted = append(deleted, id)
			return nil
		},
	})
	if _, err := r.AddList("folders", []ListItem{{ID: "inbox", Label: "Inbox", Noun: "folder", Data: things["inbox"]}, {ID: "misc", Label: "Misc", Noun: "folder", Data: things["misc"]}}); err != nil {
		t.Fatal(err)
	}
	del := Request{Noun: "folder", Verb: "delete"}
	if r.Available(del, "inbox", r.Mode) || !r.Available(del, "misc", r.Mode) {
		t.Fatalf("delete offered on inbox %v, on misc %v", r.Available(del, "inbox", r.Mode), r.Available(del, "misc", r.Mode))
	}
	defer r.forceSource(string(RequestFromCLI))()
	o := r.ExecuteOutcome(Request{Noun: "folder", Verb: "delete", Args: Arguments{"id": "inbox"}})
	if o.Kind != OutcomeNothing || o.Cause != CauseKept || o.Reason != "the inbox is where new notes land" || len(deleted) != 0 {
		t.Fatalf("outcome %+v deleted %v", o, deleted)
	}
}
