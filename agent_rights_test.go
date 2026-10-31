package ochazuke

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAgentRightsTable(t *testing.T) {
	r := NewRuntime()
	r.Define("task").Verb("read", func(Ctx) Result { return Done(nil) }).Fetch().Verb("write", func(Ctx) Result { return Done(nil) }).Verb("delete", func(Ctx) Result { return Done(nil) }).Destructive().Verb("private", func(Ctx) Result { return Done(nil) }).Agent(AgentNever)
	tests := []struct {
		verb, rows, right string
		live              bool
	}{
		{"task.read", "[]", AgentAllow, false}, {"task.write", "[]", AgentAllow, false}, {"task.delete", "[]", AgentConfirm, false}, {"task.private", "[]", AgentNever, true},
		{"focus.next", "[]", AgentAllow, true}, {"settings.get", "[]", AgentNever, false}, {"config.export", "[]", AgentNever, true},
		{"task.write", `[{"match":"task.*","right":"never","where":"live"}]`, AgentAllow, false},
		{"task.write", `[{"match":"task.*","right":"never","where":"live"}]`, AgentNever, true},
		{"task.delete", `[{"match":"task.delete","right":"allow"},{"match":"*","right":"never"}]`, AgentAllow, false},
		{"settings.get", `[{"match":"settings.*","right":"allow"}]`, AgentAllow, false},
	}
	for _, tt := range tests {
		t.Run(tt.verb+tt.rows, func(t *testing.T) {
			if err := r.Settings().Set(AgentRightsSetting, tt.rows); err != nil {
				t.Fatal(err)
			}
			op, _ := r.Operation(tt.verb)
			got := r.AgentRight(op, tt.live)
			if got.Right != tt.right {
				t.Fatalf("%+v want %s", got, tt.right)
			}
		})
	}
	for _, raw := range []string{`null`, `[{"match":"task.*.*","right":"never"}]`, `[{"match":"*","right":"root"}]`, `[{"match":"*","right":"never","where":"remote"}]`, `[{"match":"*","right":"never","extra":1}]`} {
		if err := r.Settings().Set(AgentRightsSetting, raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestAgentRightsCannotBypass(t *testing.T) {
	for _, live := range []bool{false, true} {
		for _, confirmed := range []bool{false, true} {
			r := NewRuntime()
			ran := false
			r.Register(Operation{Noun: "work", Verb: "private", Agent: AgentNever, Parameters: map[string]Parameter{"force": {Type: "boolean"}}, Run: func(*Runtime, Context, Arguments) (any, error) { ran = true; return nil, nil }})
			restore := r.BindActor(testAgent("p"), live)
			owner := Owner()
			_, err := r.Execute(Request{Noun: "work", Verb: "private", Actor: &owner, Confirmed: confirmed, Args: Arguments{"force": true}})
			var refused *RefusedError
			if !errors.As(err, &refused) || ran {
				t.Fatalf("bypass: %v ran=%v", err, ran)
			}
			restore()
		}
	}
	r := NewRuntime()
	agent := testAgent("p")
	for _, verb := range []string{"settings.set", "config.reload", "confirm.accept"} {
		n, v := SplitKey(verb)
		_, err := r.Execute(Request{Noun: n, Verb: v, Actor: &agent, Args: Arguments{"name": AgentRightsGateSetting, "value": false}})
		if err == nil {
			t.Fatalf("agent ran %s", verb)
		}
	}
}

func TestAgentTransportIdentity(t *testing.T) {
	r := NewRuntime()
	var got Actor
	r.Define("work").Verb("who", func(c Ctx) Result { got = c.Actor(); return Done(got) })
	agent := testAgent("p")
	owner := Owner()
	reply := make(chan Response, 1)
	r.HandleCall(CallMsg{Actor: agent, Headless: true, Request: Request{Noun: "work", Verb: "who", Actor: &owner}, Reply: reply})
	if res := <-reply; res.Error != "" || got != agent {
		t.Fatalf("%+v actor=%+v", res, got)
	}
	_, err := r.Execute(Request{Noun: "work", Verb: "who", Actor: &owner})
	if err != nil || got != owner {
		t.Fatal("API actor or transport restoration", err, got)
	}
}

func TestAgentConfirmOwnerOnly(t *testing.T) {
	for _, attribution := range []bool{true, false} {
		r := NewRuntime()
		NewModel(r)
		var got Actor
		ran := 0
		r.Define("work").Verb("delete", func(c Ctx) Result { got = c.Actor(); ran++; return Done(nil) }).Destructive()
		if err := r.Settings().Set(AgentConfirmAttributionSetting, attribution); err != nil {
			t.Fatal(err)
		}
		a := testAgent("p")
		restore := r.BindActor(a, false)
		_, err := r.Execute(Request{Noun: "work", Verb: "delete", Confirmed: true})
		restore()
		if err == nil || !strings.Contains(err.Error(), "needs the owner's confirmation") || r.TopModal() != "" || ran != 0 {
			t.Fatalf("headless %v", err)
		}
		_, err = r.Execute(Request{Noun: "work", Verb: "delete", Actor: &a, Confirmed: true})
		if err != nil || r.TopModal() == "" || ran != 0 {
			t.Fatalf("live: %v", err)
		}
		prompt := r.Instances[r.TopModal()].Payload.Prompt
		if strings.Contains(prompt, "agent test (pane p) asks:") != attribution {
			t.Fatalf("prompt: %s", prompt)
		}
		_, err = r.Execute(Request{Noun: "confirm", Verb: "accept", Actor: &a})
		if err == nil || ran != 0 {
			t.Fatalf("agent accepted: %v", err)
		}
		_, err = r.Execute(Request{Noun: "confirm", Verb: "accept"})
		if err != nil || ran != 1 || got != a {
			t.Fatalf("owner acceptance: %v ran=%d actor=%+v", err, ran, got)
		}
		data, _ := json.Marshal(r.History(0))
		if !strings.Contains(string(data), `"kind":"agent"`) {
			t.Fatal("missing actor")
		}
	}
}

func TestAgentConfirmStatusOptOut(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	r.Define("work").Verb("delete", func(Ctx) Result { return Done(nil) }).Destructive()
	a := testAgent("p")
	if _, err := r.Execute(Request{Noun: "work", Verb: "delete", Actor: &a}); err != nil {
		t.Fatal(err)
	}
	if r.agentActivity() != "" {
		t.Fatal("status must default off")
	}
	if err := r.Settings().Set("agents.status-segment", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.agentActivity(), "asks") {
		t.Fatal("no pending attribution")
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.agentActivity() != "" {
		t.Fatal("stale attribution")
	}
}

func TestAgentConfirmComposite(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	ran := 0
	r.Define("task").Verb("delete", func(Ctx) Result { ran++; return Done(nil) }).Confirm("Delete {{label}}?").Destructive()
	_, err := r.AddPage("tasks", "Tasks", func(*Runtime, int, int) Block { return Text("") })
	if err != nil {
		t.Fatal(err)
	}
	l, err := r.AddList("tasks/list", []ListItem{{ID: "t1", Noun: "task", Label: "One"}, {ID: "t2", Noun: "task", Label: "Two"}}, Parent("tasks"))
	if err != nil {
		t.Fatal(err)
	}
	l.marks = map[string]bool{"t1": true, "t2": true}
	a := testAgent("p")
	q := Request{Noun: "list", Verb: "apply", Target: "tasks/list", Actor: &a, Args: Arguments{"id": "tasks/list", "verb": "delete"}}
	restore := r.BindActor(a, false)
	_, err = r.Execute(q)
	restore()
	var refused *RefusedError
	if !errors.As(err, &refused) || ran != 0 || r.TopModal() != "" {
		t.Fatalf("headless composite: %v ran %d modal %s", err, ran, r.TopModal())
	}
	if _, err = r.Execute(q); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Execute(Request{Noun: "confirm", Verb: "accept"}); err != nil {
		t.Fatal(err)
	}
	if ran != 2 || r.TopModal() != "" {
		t.Fatalf("approved composite: ran %d modal %s", ran, r.TopModal())
	}
	op := r.Operations["task.delete"]
	op.Agent = AgentNever
	r.Operations[op.Key()] = op
	_, err = r.Execute(q)
	if !errors.As(err, &refused) || r.TopModal() != "" || ran != 2 {
		t.Fatalf("never composite: %v ran %d modal %s", err, ran, r.TopModal())
	}

}
