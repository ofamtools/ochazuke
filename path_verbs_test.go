package ochazuke

import (
	"reflect"
	"testing"
)

func pathVerbFixture(t *testing.T) (*Runtime, *fakeOpener) {
	t.Helper()
	r := NewRuntime()
	f := &fakeOpener{}
	r.SetOpener(f.opener("darwin"))
	things := []Thing{{"id": "d1", "title": "Notes", "file": "/tmp/notes.md", "dir": "/tmp"}, {"id": "d2", "title": "Empty"}}
	r.Define("note").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "file", Parameter: Parameter{Type: "path"}},
		PropSpec{Name: "dir", Parameter: Parameter{Type: "path", Path: "dir"}},
	).Store(Store{Get: func(_ Ctx, id string) (Thing, error) {
		for _, th := range things {
			if th["id"] == id {
				return th, nil
			}
		}
		return nil, nil
	}})
	r.Define("plain").Props(PropSpec{Name: "id", Key: true}).Store(Store{Get: func(Ctx, string) (Thing, error) { return Thing{}, nil }})
	return r, f
}

func TestPathPropGivesFreeVerbs(t *testing.T) {
	t.Parallel()
	r, f := pathVerbFixture(t)
	for _, verb := range []string{"reveal", "yank-path", "open", "open-with"} {
		if _, ok := r.Operations.Find(Key("note." + verb)); !ok {
			t.Errorf("note.%s missing", verb)
		}
		if _, ok := r.Operations.Find(Key("plain." + verb)); ok {
			t.Errorf("plain.%s exists without a path prop", verb)
		}
	}
	rights := map[string]string{"reveal": AgentAllow, "yank-path": AgentAllow, "open": AgentConfirm, "open-with": AgentConfirm}
	for verb, want := range rights {
		op, _ := r.Operations.Find(Key("note." + verb))
		if got := op.AgentRightOf(false); got != want {
			t.Errorf("note.%s agent right %s, want %s", verb, got, want)
		}
	}
	if _, err := r.Execute(Request{Noun: "note", Verb: "reveal", Args: Arguments{"id": "d1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "note", Verb: "open", Args: Arguments{"id": "d1", "prop": "dir"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "note", Verb: "open-with", Args: Arguments{"id": "d1", "with": "Preview"}}); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"open", "-R", "--", "/tmp/notes.md"}, {"open", "--", "/tmp"}, {"open", "-a", "Preview", "--", "/tmp/notes.md"}}
	if !reflect.DeepEqual(f.runs, want) {
		t.Fatalf("ran %v, want %v", f.runs, want)
	}
	if got, err := r.Execute(Request{Noun: "note", Verb: "yank-path", Args: Arguments{"id": "d1"}}); err != nil || got != "/tmp/notes.md" {
		t.Fatalf("yank-path %v %v", got, err)
	}
}

func TestPathVerbsRefuseWithoutAPathAndWhenOff(t *testing.T) {
	t.Parallel()
	r, f := pathVerbFixture(t)
	if _, err := r.Execute(Request{Noun: "note", Verb: "reveal", Args: Arguments{"id": "d2"}}); err == nil {
		t.Fatal("a thing with no path value cannot be revealed")
	}
	if err := r.Settings().Set(PathFreeVerbsSetting, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "note", Verb: "open", Args: Arguments{"id": "d1"}}); err == nil || len(f.runs) != 0 {
		t.Fatalf("paths.free-verbs off: %v ran %v", err, f.runs)
	}
}

func TestApplicationOpenVerbWinsOverTheFreeOne(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	ran := false
	r.Register(Operation{Key: "note.open", Description: "app", Run: func(*Runtime, Context, Arguments) (any, error) { ran = true; return nil, nil }})
	r.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "f", Parameter: Parameter{Type: "path"}}).Store(Store{Get: func(Ctx, string) (Thing, error) { return Thing{}, nil }})
	if _, err := r.Execute(Request{Noun: "note", Verb: "open"}); err != nil || !ran {
		t.Fatalf("the application's open: %v ran %v", err, ran)
	}
}
