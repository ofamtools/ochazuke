package ochazuke

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistedHistoryKeepsDurableIdentityWithoutPrivateValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	thing := Thing{"id": "stable-1", "file": "/before/plan", "content": "PRIVATE DOCUMENT BODY", "archived": false}
	makeRuntime := func() *Runtime {
		r := NewRuntime()
		r.Config().App = "history-identity"
		r.Config().Dir = filepath.Join(dir, "config")
		r.Config().StateDir = dir
		r.Define("document").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "file", Human: true, Parameter: Parameter{Type: PropPath, Path: "file"}}, PropSpec{Name: "content", Multiline: true}, PropSpec{Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}}).Store(Store{
			Get: func(_ Ctx, key string) (Thing, error) {
				if key != thing["id"] && key != thing["file"] {
					return nil, fmt.Errorf("not found")
				}
				return thing, nil
			},
			Patch: func(_ Ctx, _ string, patch Thing) (Thing, error) {
				for k, v := range patch {
					thing[k] = v
				}
				return thing, nil
			},
		}).Verb("move", func(c Ctx) Result { thing["file"] = "/after/plan"; return Done(thing) }).History(HistoryChange)
		op, _ := r.Operation("document.move")
		op.Parameters = map[string]Parameter{"id": {Type: "string", Noun: "document", Free: true}}
		r.Operations.Set(op)
		r.EnsureHistory()
		t.Cleanup(func() { r.Config().Flush() })
		return r
	}
	r := makeRuntime()
	if _, err := r.Execute(Request{Noun: "document", Verb: "move", Args: Arguments{"id": "/before/plan"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Config().Flush(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, HistoryFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PRIVATE DOCUMENT BODY") || !strings.Contains(string(raw), `"things":["document:stable-1"]`) {
		t.Fatalf("identity/privacy persisted incorrectly: %s", raw)
	}
	next := makeRuntime()
	answer, err := next.Execute(Request{Noun: "document", Verb: "history", Args: Arguments{"id": "/after/plan"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	var entries []HistoryEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Request.Verb != "move" || !entries[0].Restored {
		t.Fatalf("history after restart/move: %#v", answer)
	}
}
