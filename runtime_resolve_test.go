package ochazuke

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveTargetMapsForeignIDs(t *testing.T) {
	r := NewRuntime()
	var got []string
	r.Define("doc").Verb("open", func(c Ctx) Result {
		got = append(got, c.Instance.ID)
		return Done(nil)
	}).Verb("drop", func(c Ctx) Result { return Done(nil) }).Confirm("Drop {{label}}?")
	r.Instance("doc/1", Kind("doc")).Label = "first"
	if _, err := r.Execute(Request{Noun: "doc", Verb: "open", Target: "db:1"}); err == nil || err.Error() != `unknown target "db:1"` {
		t.Fatalf("without a resolver: %v", err)
	}
	r.ResolveTarget = func(id string) (string, error) {
		switch id {
		case "db:1":
			return "doc/1", nil
		case "db:ghost":
			return "doc/ghost", nil
		case "db:bad":
			return "", errors.New("no such key")
		}
		return "", nil
	}
	if _, err := r.Execute(Request{Noun: "doc", Verb: "open", Target: "db:1"}); err != nil || len(got) != 1 || got[0] != "doc/1" {
		t.Fatalf("resolved: %v %v", err, got)
	}
	if _, err := r.Execute(Request{Noun: "doc", Verb: "open", Target: "doc/1"}); err != nil || got[1] != "doc/1" {
		t.Fatalf("an instance target is not resolved: %v %v", err, got)
	}
	for target, want := range map[string]string{
		"db:bad":   `target "db:bad": no such key`,
		"db:ghost": `target "db:ghost" resolved to "doc/ghost", which is not an instance`,
		"db:none":  `unknown target "db:none"`,
	} {
		if _, err := r.Execute(Request{Noun: "doc", Verb: "open", Target: target}); err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %s", target, err, want)
		}
	}
	// The confirm carries the resolved target and names its instance.
	if _, err := r.Execute(Request{Noun: "doc", Verb: "drop", Target: "db:1"}); err != nil {
		t.Fatal(err)
	}
	top := r.Instances[r.TopModal()]
	if top == nil || !strings.Contains(top.Payload.Prompt, "Drop first?") || top.Payload.Request.Target != "doc/1" {
		t.Fatalf("confirm: %+v", top)
	}
}
