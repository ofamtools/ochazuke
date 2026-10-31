package cli

import (
	"encoding/json"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// An extension's default hook rows and its own events travel in its
// catalog (the framework's events never do); the host mounts them under its
// own nouns with the extension's name as their source, and they run on the
// host's events.
func TestCatalogHooksRoundTrip(t *testing.T) {
	ext := ochazuke.NewRuntime()
	ext.Config().Disable()
	ext.Define("tasks-git").Verb("sync", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "Queue an export and commit")
	if err := ext.DeclareEvent(ochazuke.EventSpec{Name: "tasks-git.committed", Fields: []ochazuke.EventField{{Name: "commit", Type: "string"}}}); err != nil {
		t.Fatal(err)
	}
	if err := ext.AddDefaultHooks(ochazuke.HookRow{On: ochazuke.EventSurfaceLeft, Where: map[string]any{"changed": "task"}, Do: "tasks-git.sync", Args: ochazuke.Arguments{"reason": "edit-session"}}); err != nil {
		t.Fatal(err)
	}
	data, err := Catalog(ext, "tasks-git", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Hooks) != 1 || doc.Hooks[0].Do != "tasks-git.sync" || doc.Hooks[0].Source != "" {
		t.Fatalf("catalog hooks = %+v", doc.Hooks)
	}
	if len(doc.Events) != 1 || doc.Events[0].Name != "tasks-git.committed" {
		t.Fatalf("catalog events = %+v", doc.Events)
	}

	host := ochazuke.NewRuntime()
	host.Config().Disable()
	host.Define("task").Verb("edit", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) })
	synced := 0
	host.Define("ext-tasks-git").Verb("sync", func(ochazuke.Ctx) ochazuke.Result { synced++; return ochazuke.Done(nil) })
	op := host.Operations["ext-tasks-git.sync"]
	op.Parameters = map[string]ochazuke.Parameter{"reason": {Type: "string"}}
	host.Operations["ext-tasks-git.sync"] = op
	if err := MountCatalogHooks(host, doc, "tasks-git", func(n string) string { return "ext-" + n }); err != nil {
		t.Fatal(err)
	}
	rows := host.HookRows()
	if len(rows) != 1 || rows[0].Do != "ext-tasks-git.sync" || rows[0].Source != "tasks-git" {
		t.Fatalf("host rows = %+v", rows)
	}
	if e, ok := host.Event("tasks-git.committed"); !ok || e.Source != "tasks-git" {
		t.Fatalf("host event = %+v %v", e, ok)
	}
	if problems := host.CheckHooks(); len(problems) != 0 {
		t.Fatalf("check %v", problems)
	}
	if _, err := host.OpenModal("tasks", ochazuke.Payload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Execute(ochazuke.Request{Noun: "task", Verb: "edit"}); err != nil {
		t.Fatal(err)
	}
	if err := host.Pop(); err != nil {
		t.Fatal(err)
	}
	if synced != 1 {
		t.Fatalf("synced %d", synced)
	}
}
