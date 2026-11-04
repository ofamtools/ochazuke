package cli

import (
	"errors"
	"testing"

	"ofam.dev/ochazuke"
)

func TestMountedDestructiveBeatsDeclaredAllow(t *testing.T) {
	child := ochazuke.NewRuntime()
	ran := 0
	child.Define("task").Verb("delete", func(ochazuke.Ctx) ochazuke.Result { ran++; return ochazuke.Done(nil) }).Destructive().Agent(ochazuke.AgentAllow)
	host := ochazuke.NewRuntime()
	got := MountNouns(host, []CatalogNoun{{Name: "task", Verbs: []CatalogVerb{{Name: "delete", Destructive: true, Agent: ochazuke.AgentAllow, AgentDeclared: ochazuke.AgentAllow}}}}, HostMount{
		Local: func(n string) string { return "ext-" + n },
		Proxy: func(_ string, v CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				restore := child.BindActor(c.Actor(), false)
				defer restore()
				value, err := child.Execute(ochazuke.Request{Noun: "task", Verb: v.Name, Confirmed: true})
				if err != nil {
					return ochazuke.Failed(err)
				}
				return ochazuke.Done(value)
			}
		},
	})
	if len(got.Warnings) > 0 {
		t.Fatal(got.Warnings)
	}
	restore := host.BindActor(ochazuke.Actor{Kind: ochazuke.ActorAgent, Harness: "test"}, false)
	defer restore()
	call := func() error {
		return Settle(host, func() error {
			_, err := host.Execute(ochazuke.Request{Noun: "ext-task", Verb: "delete", Confirmed: true})
			return err
		})
	}
	var refused *ochazuke.RefusedError
	if err := call(); !errors.As(err, &refused) || refused.Verb != "ext-task.delete" {
		t.Fatalf("host gate: %v", err)
	}
	// Owner rows on the host cannot widen the child's independent gate.
	owner := host.BindActor(ochazuke.Owner(), false)
	err := host.Settings().Set(ochazuke.AgentRightsSetting, `[{"match":"ext-task.delete","right":"allow"}]`)
	owner()
	if err != nil {
		t.Fatal(err)
	}
	if err := call(); err == nil || ran != 0 {
		t.Fatalf("child gate: %v ran=%d", err, ran)
	}
	if err := child.Settings().Set(ochazuke.AgentRightsSetting, `[{"match":"task.delete","right":"allow"}]`); err != nil {
		t.Fatal(err)
	}
	if err := call(); err != nil || ran != 1 {
		t.Fatalf("owner override: %v ran=%d", err, ran)
	}
}
