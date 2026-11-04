package ochazuke

import (
	"errors"
	"strings"
	"testing"
)

func TestAgentAllowPassesPrompts(t *testing.T) {
	for _, live := range []bool{false, true} {
		for _, override := range []bool{false, true} {
			r := NewRuntime()
			ran := 0
			b := r.Define("task").Verb("write", func(Ctx) Result { ran++; return Done("written") }).Confirm("Write?")
			if override {
				b.Destructive().Agent(AgentNever)
				if err := r.Settings().Set(AgentRightsSetting, `[{"match":"task.write","right":"allow"}]`); err != nil {
					t.Fatal(err)
				}
			} else {
				b.Agent(AgentAllow)
			}
			if err := r.AddVerbCheck("task.write", VerbCheck{Name: "ask", Check: func(*Runtime, Context, Arguments) Verdict { return AskFirst("Check?") }}); err != nil {
				t.Fatal(err)
			}
			restore := r.BindActor(testAgent("p"), live)
			got, err := r.Execute(Request{Noun: "task", Verb: "write"})
			restore()
			if err != nil || got != "written" || ran != 1 || r.TopModal() != "" {
				t.Fatalf("live=%v override=%v: %v %v ran=%d", live, override, got, err, ran)
			}
		}
	}
}

func TestAgentDestructiveArchiveRefusal(t *testing.T) {
	r := NewRuntime()
	r.Define("task").Verb("delete", func(Ctx) Result { t.Fatal("deleted"); return Done(nil) }).Destructive().Agent(AgentAllow).
		Verb("archive", func(Ctx) Result { return Done(nil) })
	restore := r.BindActor(testAgent("p"), false)
	defer restore()
	_, err := r.Execute(Request{Noun: "task", Verb: "delete", Confirmed: true})
	var refused *RefusedError
	if !errors.As(err, &refused) || refused.Rule != "destructive verb" || !strings.Contains(refused.Reason, "agents never delete; run task.archive") {
		t.Fatal(err)
	}
}

func TestAgentConfigurationReadsAndWrites(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	for _, key := range []string{"settings.get", "settings.set", "settings.reset", "setting.set", "setting-item.delete", "bindings.load", "bindings.reload", "actions.reload", "themes.reload", "config.reload"} {
		op, ok := r.Operation(key)
		if !ok || r.AgentRight(op, false).Right != AgentAllow {
			t.Errorf("%s: %+v", key, op)
		}
	}
	for _, key := range []string{"bindings.reload", "actions.reload", "settings.reload", "themes.reload", "config.reload", "bindings.defaults", "settings.diff", "settings.owners", "settings.sections", "setting.show"} {
		op, ok := r.Operation(key)
		if !ok || op.HistoryKindOf() != HistoryFetch {
			t.Errorf("%s: %+v", key, op)
		}
	}
}
