package demo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/keytest"
)

// The demo's task writes its title, its project and its archive flag by
// role through set, and copies itself through create {from}, from its
// props' roles and its store, with no code of its own.
func TestTaskRoleWritesFromTheCLI(t *testing.T) {
	read := func(args ...string) map[string]any {
		t.Helper()
		var out map[string]any
		if err := json.Unmarshal([]byte(runDemo(t, args...)), &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	if got := read("task", "set", "t1", "@label=Feed the cat"); got["title"] != "Feed the cat" {
		t.Fatalf("set @label = %v", got)
	}
	if got := read("task", "set", "t1", "@owner=work"); got["project"] != "work" {
		t.Fatalf("set @owner = %v", got)
	}
	if got := read("task", "create", "--from", "t2", "title=Copy"); got["id"] != "t4" || got["title"] != "Copy" || got["status"] != "doing" || got["project"] != "work" || got["notes"] != "Start from the CHANGELOG." {
		t.Fatalf("create --from = %v", got)
	}
	if got := read("task", "archive", "t3"); got["archived"] != true {
		t.Fatalf("archive = %v", got)
	}
	if got := read("task", "set", "t3", "@archive=false"); got["archived"] != false {
		t.Fatalf("set @archive=false = %v", got)
	}
	// The project noun is a read-only store: the core writes are listed,
	// unavailable with the reason, and the purged ones are absent.
	m := New()
	for _, verb := range []string{"archive", "set", "create", "delete"} {
		if op, ok := m.Runtime.Operations.Find(ochazuke.Key("project." + verb)); !ok || op.Unavailable == "" || m.Runtime.Available(ochazuke.Request{Noun: "project", Verb: verb}, "app", m.Runtime.Mode) {
			t.Fatalf("project.%s: ok %v unavailable %q", verb, ok, op.Unavailable)
		}
	}
	for _, verb := range []string{"rename", "move"} {
		if _, ok := m.Runtime.Operations.Find(ochazuke.Key("project." + verb)); ok {
			t.Fatalf("project.%s exists", verb)
		}
	}
}

// task create from the palette over a focused project row: the slot
// after the title says the project comes from the focus, the new task
// has it, and a notice names it. With create.from-focus off nothing is
// filled, the slot says nothing, and create is not refused.
func TestTaskCreateFillsTheFocusedProject(t *testing.T) {
	for _, on := range []bool{true, false} {
		m := New()
		m.Width, m.Height = 120, 24
		m.Appearance.Theme = "mono"
		r := m.Runtime
		if !on {
			if _, err := r.Settings().SetSession(ochazuke.CreateFromFocusSetting, false); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.OpenPage("task"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.AddList("projects", []ochazuke.ListItem{
			{ID: "p-home", Label: "Home", Noun: "project", Data: map[string]any{"id": "home"}},
			{ID: "p-work", Label: "Work", Noun: "project", Data: map[string]any{"id": "work"}},
		}, ochazuke.Parent("task")); err != nil {
			t.Fatal(err)
		}
		m.View()
		keytest.Press(m, "esc")
		if err := r.SetFocus("p-work"); err != nil {
			t.Fatal(err)
		}
		if r.Focus != "p-work" || r.Mode.Name != "normal" {
			t.Fatalf("focus %q mode %q", r.Focus, r.Mode.Name)
		}
		keytest.Press(m, ":")
		keytest.Type(m, `task create "Buy milk" `)
		frame := ansi.Strip(m.View().Content)
		if got := strings.Contains(frame, "focus: Work"); got != on {
			t.Fatalf("from-focus %v: slot shows the focus %v:\n%s", on, got, frame)
		}
		keytest.Press(m, "enter")
		frame = ansi.Strip(m.View().Content)
		got, err := r.Execute(ochazuke.Request{Noun: "task", Verb: "get", Args: ochazuke.Arguments{"id": "t4"}})
		if err != nil {
			t.Fatalf("from-focus %v: %v\n%s", on, err, frame)
		}
		project := got.(map[string]any)["project"]
		switch {
		case on && (project != "work" || !strings.Contains(frame, "project Work (from the focus")):
			t.Fatalf("filled: project %v\n%s", project, frame)
		case !on && (project != "" || strings.Contains(frame, "from the focus")):
			t.Fatalf("off: project %v\n%s", project, frame)
		}
	}
}

// delete.archive on: delete archives a live task (asking first), and
// deletes an archived one for real; off, delete deletes.
func TestTaskDeleteArchives(t *testing.T) {
	m := New()
	r := m.Runtime
	if _, err := r.Settings().SetSession(ochazuke.DeleteArchiveSetting, true); err != nil {
		t.Fatal(err)
	}
	del := func(id string) (any, error) {
		return r.Execute(ochazuke.Request{Noun: "task", Verb: "delete", Confirmed: true, Args: ochazuke.Arguments{"id": id}})
	}
	if got, err := del("t2"); err != nil || got.(map[string]any)["archived"] != "t2" {
		t.Fatalf("first delete %v %v", got, err)
	}
	if got, _ := r.Execute(ochazuke.Request{Noun: "task", Verb: "get", Args: ochazuke.Arguments{"id": "t2", "prop": "archived"}}); got != true {
		t.Fatalf("archived %v", got)
	}
	if got, err := del("t2"); err != nil || got.(map[string]any)["deleted"] != "t2" {
		t.Fatalf("second delete %v %v", got, err)
	}
	if _, err := r.Settings().SetSession(ochazuke.DeleteArchiveSetting, false); err != nil {
		t.Fatal(err)
	}
	if got, err := del("t3"); err != nil || got.(map[string]any)["deleted"] != "t3" {
		t.Fatalf("off: %v %v", got, err)
	}
}
