package ochazuke

import "testing"

func TestAgentMetadataDefaults(t *testing.T) {
	for _, tc := range []struct {
		op   Operation
		want string
	}{
		{Operation{}, AgentAllow}, {Operation{HistoryKind: HistoryFetch}, AgentAllow},
		{Operation{HistoryKind: HistoryChange}, AgentAllow}, {Operation{Motion: true}, AgentAllow},
		{Operation{Destructive: true}, AgentConfirm}, {Operation{Confirm: "Sure?"}, AgentConfirm},
		{Operation{Destructive: true, Agent: AgentAllow}, AgentAllow}, {Operation{Agent: AgentNever}, AgentNever},
	} {
		for _, live := range []bool{false, true} {
			if got := tc.op.AgentRightOf(live); got != tc.want {
				t.Errorf("%+v live %v: %s", tc.op, live, got)
			}
		}
	}
	r := NewRuntime()
	r.Define("thing").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "archived", Parameter: Parameter{Type: "boolean"}, Role: RoleArchive}).Store(Store{Delete: func(Ctx, string) error { return nil }, Patch: func(Ctx, string, Thing) (Thing, error) { return nil, nil }})
	for _, verb := range []string{"delete", "archive"} {
		if op, ok := r.Operation("thing." + verb); !ok || !op.Destructive {
			t.Errorf("%s not destructive: %+v", verb, op)
		}
	}
	op := Operation{Parameters: map[string]Parameter{"required": {Required: true}, "optional": {}}}
	if op.ExamplesOf()[0].Args["required"] != "<required>" || len(op.ExamplesOf()[0].Args) != 1 {
		t.Fatal(op.ExamplesOf())
	}
}
