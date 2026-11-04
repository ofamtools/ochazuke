package demo

import (
	"reflect"
	"testing"

	"ofam.dev/ochazuke"
)

func TestTaskArchiveListRoundTrip(t *testing.T) {
	r := New().Runtime
	r.Config().Disable()
	restore := r.BindActor(ochazuke.Actor{Kind: ochazuke.ActorAgent, Harness: "test"}, false)
	defer restore()
	run := func(verb string, args ochazuke.Arguments) any {
		t.Helper()
		value, err := r.Execute(ochazuke.Request{Noun: "task", Verb: verb, Args: args})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	ids := func(args ochazuke.Arguments) []string {
		t.Helper()
		out := []string{}
		for _, row := range run("list", args).([]map[string]any) {
			out = append(out, row["id"].(string))
		}
		return out
	}
	run("archive", ochazuke.Arguments{"id": "t2"})
	if got := ids(nil); !reflect.DeepEqual(got, []string{"t1", "t3"}) {
		t.Fatal(got)
	}
	if got := ids(ochazuke.Arguments{"filter": "archived"}); !reflect.DeepEqual(got, []string{"t2"}) {
		t.Fatal(got)
	}
	if got := run("get", ochazuke.Arguments{"id": "t2", "prop": "archived"}); got != true {
		t.Fatal(got)
	}
	run("restore", ochazuke.Arguments{"id": "t2"})
	if got := ids(nil); !reflect.DeepEqual(got, []string{"t1", "t2", "t3"}) {
		t.Fatal(got)
	}
}
