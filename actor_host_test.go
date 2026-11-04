package ochazuke

import (
	"encoding/json"
	"testing"
)

func TestHostCallerNarrowsActorAndKeepsProvenance(t *testing.T) {
	t.Parallel()
	for _, detected := range []bool{false, true} {
		t.Run(map[bool]string{false: "owner", true: "detected-agent"}[detected], func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"OCHAZUKE_ACTOR": "owner", "OCHAZUKE_CALLER": "lane"}
			if detected {
				env["CODEX_CI"] = "1"
			}
			actor, err := ActorFromEnv(func(k string) string { return env[k] })
			if err != nil {
				t.Fatal(err)
			}
			if actor.Kind != ActorAgent || actor.Name != "lane" || actor.Harness != "extension" || actor.Via == nil {
				t.Fatalf("actor: %+v", actor)
			}
			want := ActorOwner
			if detected {
				want = ActorAgent
			}
			if actor.Via.Kind != want {
				t.Fatalf("via: %+v", actor.Via)
			}
			data, err := json.Marshal(actor)
			if err != nil {
				t.Fatal(err)
			}
			var restored Actor
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			if !actor.SameAgent(restored) {
				t.Fatal("persisted extension actor lost its identity")
			}
		})
	}
}
