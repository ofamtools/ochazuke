package ochazuke

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// hooksFixture is a runtime with a config directory holding hooks.json
// (the rows given, none when nil), an application noun task with a change
// verb edit, and a verb sync.run that records its arguments.
func hooksFixture(t *testing.T, rows ...map[string]any) (*Runtime, *[]Arguments) {
	t.Helper()
	r := NewRuntime()
	r.Define("task").Verb("edit", func(Ctx) Result { return Done(nil) })
	r.Define("tasks")
	var ran []Arguments
	r.Register(Operation{Noun: "sync", Verb: "run", Parameters: map[string]Parameter{
		"reason": {Type: "string"}, "noun": {Type: "string"}, "changes": {Type: "integer"}, "event": {Type: "string"},
	}, Run: func(_ *Runtime, _ Context, a Arguments) (any, error) {
		ran = append(ran, a)
		return nil, nil
	}})
	dir := t.TempDir()
	r.Config().Dir = dir
	if rows != nil {
		data, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		writeConfig(t, dir, HooksFile, string(data))
	}
	r.EnsureHooks()
	return r, &ran
}

func openTestModal(t *testing.T, r *Runtime, kind string) {
	t.Helper()
	if _, err := r.OpenModal(kind, Payload{}); err != nil {
		t.Fatal(err)
	}
}

// Closing a modal after a change runs the row's verb once, with its args
// and the from fields; closing one with no change runs nothing.
func TestHookRunsOnceWhenModalLeftAfterChange(t *testing.T) {
	r, ran := hooksFixture(t, map[string]any{"on": "surface.left", "where": map[string]any{"changed": "task"}, "do": "sync.run", "args": map[string]any{"reason": "edit-session"}, "from": map[string]any{"noun": "noun", "changes": "changes"}})
	openTestModal(t, r, "tasks")
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if len(*ran) != 0 {
		t.Fatalf("no change, no hook: %v", *ran)
	}
	openTestModal(t, r, "tasks")
	for range 2 {
		if _, err := r.Execute(Request{Noun: "task", Verb: "edit"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(*ran) != 0 {
		t.Fatalf("a change runs no hook while the modal is open: %v", *ran)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if len(*ran) != 1 {
		t.Fatalf("one close, one run: %v", *ran)
	}
	got := (*ran)[0]
	if got["reason"] != "edit-session" || got["noun"] != "tasks" || got["changes"] != 2 {
		t.Fatalf("args %v", got)
	}
}

// where filters: a row on another noun's change does not run; a list where
// value matches any of its values; a scalar field compares as text.
func TestHookWhereFilters(t *testing.T) {
	r, ran := hooksFixture(t,
		map[string]any{"on": "surface.left", "where": map[string]any{"changed": "project"}, "do": "sync.run", "args": map[string]any{"reason": "project"}},
		map[string]any{"on": "surface.left", "where": map[string]any{"changed": []any{"project", "task"}}, "do": "sync.run", "args": map[string]any{"reason": "either"}},
		map[string]any{"on": "surface.left", "where": map[string]any{"kind": "modal", "changes": 1}, "do": "sync.run", "args": map[string]any{"reason": "one"}},
		map[string]any{"on": "surface.left", "where": map[string]any{"kind": "page"}, "do": "sync.run", "args": map[string]any{"reason": "page"}},
	)
	openTestModal(t, r, "tasks")
	if _, err := r.Execute(Request{Noun: "task", Verb: "edit"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	var reasons []string
	for _, a := range *ran {
		reasons = append(reasons, a["reason"].(string))
	}
	if !slices.Equal(reasons, []string{"either", "one"}) {
		t.Fatalf("reasons %v", reasons)
	}
}

// Nested modals: a change in the inner one counts for both; the page under
// them counts it too, and leaving the page for another emits its
// surface.left and the new page's surface.opened.
func TestSurfaceCountsNestedAndPages(t *testing.T) {
	r, ran := hooksFixture(t,
		map[string]any{"on": "surface.left", "do": "sync.run", "from": map[string]any{"noun": "id", "changes": "changes"}},
		map[string]any{"on": "surface.opened", "where": map[string]any{"kind": "page"}, "do": "sync.run", "args": map[string]any{"reason": "opened"}, "from": map[string]any{"noun": "id"}},
	)
	for _, id := range []string{"one", "two"} {
		if _, err := r.AddPage(id, id, func(*Runtime, int, int) Block { return Text("") }); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.OpenPage("one"); err != nil {
		t.Fatal(err)
	}
	*ran = nil
	openTestModal(t, r, "outer")
	openTestModal(t, r, "inner")
	if _, err := r.Execute(Request{Noun: "task", Verb: "edit"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("two"); err != nil {
		t.Fatal(err)
	}
	type run struct {
		id      string
		changes any
	}
	var got []run
	for _, a := range *ran {
		got = append(got, run{a["noun"].(string), a["changes"]})
	}
	want := []run{{"inner", 1}, {"outer", 1}, {"one", 1}, {"two", nil}}
	if !slices.Equal(got, want) {
		t.Fatalf("runs %v, want %v", got, want)
	}
}

// Runtime.Changed counts (an editor's write); the framework's own nouns and
// what a hook's verb changes do not; a failed change verb does not.
func TestChangedCountsAndWhatDoesNot(t *testing.T) {
	r, ran := hooksFixture(t, map[string]any{"on": "surface.left", "do": "sync.run", "from": map[string]any{"changes": "changes"}})
	r.Define("task").Verb("fail", func(Ctx) Result { return Failed(errors.New("no")) })
	openTestModal(t, r, "tasks")
	r.Changed("task")
	r.Changed("ochazuke.input")
	_, _ = r.Execute(Request{Noun: "task", Verb: "fail"})
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if len(*ran) != 1 || (*ran)[0]["changes"] != 1 {
		t.Fatalf("runs %v", *ran)
	}
}

// A hook whose verb itself changes something does not count toward the
// next surface.left: hooks never feed themselves.
func TestHookChangesAreNotCounted(t *testing.T) {
	r, _ := hooksFixture(t)
	writes := 0
	r.Define("store").Verb("write", func(Ctx) Result { writes++; return Done(nil) })
	if err := r.AddDefaultHooks(HookRow{On: "surface.opened", Do: "store.write"}); err != nil {
		t.Fatal(err)
	}
	openTestModal(t, r, "tasks")
	if writes != 1 {
		t.Fatalf("writes %d", writes)
	}
	if c := r.events.open["tasks"]; c == nil || c.changes != 0 {
		t.Fatalf("a hook's change counted: %+v", c)
	}
}

// hooks.enabled off runs nothing.
func TestHooksEnabledOff(t *testing.T) {
	r, ran := hooksFixture(t, map[string]any{"on": "surface.left", "do": "sync.run"})
	if err := r.Settings().Set(HooksEnabledSetting, false); err != nil {
		t.Fatal(err)
	}
	openTestModal(t, r, "tasks")
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if len(*ran) != 0 {
		t.Fatalf("hooks off ran %v", *ran)
	}
}

// A failing hook is a notice and the next row still runs; an application
// verb runs as a job off the loop, and its failure is a notice too.
func TestFailingHookIsANotice(t *testing.T) {
	r, ran := hooksFixture(t,
		map[string]any{"on": "surface.left", "do": "boom.now"},
		map[string]any{"on": "surface.left", "do": "boom.later"},
		map[string]any{"on": "surface.left", "do": "sync.run"},
	)
	r.Register(Operation{Noun: "boom", Verb: "now", Run: func(*Runtime, Context, Arguments) (any, error) { return nil, errors.New("disk full") }})
	r.Define("boom").Verb("later", func(Ctx) Result {
		return Pending(func(context.Context) (any, error) { return nil, errors.New("offline") })
	})
	r.ReloadHooks()
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	openTestModal(t, r, "tasks")
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if len(*ran) != 1 {
		t.Fatalf("the row after a failure must run: %v", *ran)
	}
	for _, msg := range runCommands(r.PendingCommands()) {
		_, _ = r.Deliver(msg)
	}
	notices := strings.Join(sink.Notices(), "\n")
	for _, want := range []string{"hook surface.left → boom.now failed: disk full", "hook surface.left → boom.later failed: offline"} {
		if !strings.Contains(notices, want) {
			t.Fatalf("notices %q lack %q", notices, want)
		}
	}
}

// check: an unknown event and a bad where key warn (the row loads); an
// unknown verb, an argument the verb does not take and a missing do skip
// the row.
func TestHooksCheck(t *testing.T) {
	r, _ := hooksFixture(t,
		map[string]any{"on": "surface.closed", "do": "sync.run"},
		map[string]any{"on": "surface.left", "where": map[string]any{"title": "x"}, "do": "sync.run"},
		map[string]any{"on": "surface.left", "do": "nope.verb"},
		map[string]any{"on": "surface.left", "do": "sync.run", "args": map[string]any{"color": "red"}},
		map[string]any{"on": "surface.left"},
		map[string]any{"on": "surface.left", "do": "sync.run", "bogus": true},
	)
	got := r.CheckHooks()
	want := []string{
		`hooks.json: row 6: json: unknown field "bogus" (skipped)`,
		`warning: hooks.json: row 1: surface.closed → sync.run: unknown event "surface.closed" (events list shows the declared ones)`,
		`warning: hooks.json: row 2: surface.left → sync.run: where key "title" is not a field of surface.left`,
		`hooks.json: row 3: surface.left → nope.verb: unknown verb "nope.verb" (skipped)`,
		`hooks.json: row 4: surface.left → sync.run: unknown argument "color" (skipped)`,
		`hooks.json: row 5: surface.left: a row needs do (the verb, noun.verb) (skipped)`,
	}
	if len(got) != len(want) {
		t.Fatalf("problems:\n%s", strings.Join(got, "\n"))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Fatalf("problem %d:\n got %s\nwant %s", i+1, got[i], want[i])
		}
	}
	rows := r.HookRows()
	if len(rows) != 2 {
		t.Fatalf("the warned rows load, the bad ones are skipped: %+v", rows)
	}
	op, _ := r.Execute(Request{Noun: "ochazuke.hooks", Verb: "check"})
	if len(op.([]string)) != len(want) {
		t.Fatalf("hooks check verb %v", op)
	}
}

// The user's file replaces a default with the same on, where and do in
// place, drops defaults with an off row, and adds its other rows after.
func TestHooksFileOverDefaults(t *testing.T) {
	r, _ := hooksFixture(t,
		map[string]any{"on": "surface.left", "where": map[string]any{"changed": "task"}, "do": "sync.run", "args": map[string]any{"reason": "mine"}},
		map[string]any{"on": "app.quitting", "do": "sync.run", "off": true},
		map[string]any{"on": "surface.opened", "do": "sync.run"},
	)
	if err := r.AddDefaultHooks(
		HookRow{On: "surface.left", Where: map[string]any{"changed": "task"}, Do: "sync.run", Args: Arguments{"reason": "default"}, Source: "tasks-git"},
		HookRow{On: "app.quitting", Do: "sync.run"},
		HookRow{On: "surface.left", Where: map[string]any{"changed": "project"}, Do: "sync.run"},
	); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, row := range r.HookRows() {
		got = append(got, row.On+" "+row.Source+" "+StringArg(row.Args, "reason", ""))
	}
	want := []string{"surface.left user mine", "surface.left app ", "surface.opened user "}
	if !slices.Equal(got, want) {
		t.Fatalf("rows %q, want %q", got, want)
	}
}

// Each application reads its own hooks.json: two runtimes with different
// app names load only their own files, from $XDG_CONFIG_HOME/<app>.
func TestHooksFilePerApp(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	for app, verb := range map[string]string{"matomeru": "sync.mtm", "o-json": "sync.json"} {
		if err := os.MkdirAll(filepath.Join(base, app), 0o755); err != nil {
			t.Fatal(err)
		}
		writeConfig(t, filepath.Join(base, app), HooksFile, `[{"on": "surface.left", "do": "`+verb+`"}] // the app's own
`)
	}
	load := func(app string) []string {
		r := NewRuntime()
		r.Register(Operation{Noun: "sync", Verb: "mtm", Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
		r.Register(Operation{Noun: "sync", Verb: "json", Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
		r.Config().App = app
		if p := r.EnsureHooks(); len(p) > 0 {
			t.Fatalf("%s: %v", app, p)
		}
		if got := r.HooksPath(); got != filepath.Join(base, app, HooksFile) {
			t.Fatalf("%s path %s", app, got)
		}
		var out []string
		for _, row := range r.HookRows() {
			out = append(out, row.Do)
		}
		return out
	}
	if got := load("matomeru"); !slices.Equal(got, []string{"sync.mtm"}) {
		t.Fatalf("matomeru %v", got)
	}
	if got := load("o-json"); !slices.Equal(got, []string{"sync.json"}) {
		t.Fatalf("o-json %v", got)
	}
	if got := load("nabe"); len(got) != 0 {
		t.Fatalf("an app with no file has no rows: %v", got)
	}
}

// export writes the effective rows to <config dir>/reference by default, in
// a form hooks.json reads back, and never over the live file without force.
func TestHooksExport(t *testing.T) {
	r, _ := hooksFixture(t, map[string]any{"on": "surface.left", "do": "sync.run"})
	if err := r.AddDefaultHooks(HookRow{On: "app.quitting", Do: "sync.run"}); err != nil {
		t.Fatal(err)
	}
	path, err := r.ExportHooks("", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(r.Config().Directory(), ReferenceDir, HooksReferenceFile); path != want {
		t.Fatalf("path %s, want %s", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, problems := parseHooks(data)
	if len(problems) > 0 || len(rows) != 2 || rows[0].row.Source != "app" || rows[1].row.Source != "user" {
		t.Fatalf("export %s: %+v %v", data, rows, problems)
	}
	if _, err := r.ExportHooks(r.HooksPath(), false); err == nil || !strings.Contains(err.Error(), "live hooks.json") {
		t.Fatalf("export over the live file: %v", err)
	}
}

// Apps declare events; emit runs their rows; events list shows them after
// the framework's.
func TestDeclaredAppEvent(t *testing.T) {
	r, ran := hooksFixture(t, map[string]any{"on": "agent.turn-ended", "where": map[string]any{"source": "hook"}, "do": "sync.run", "from": map[string]any{"event": "event"}})
	if err := r.DeclareEvent(EventSpec{Name: "agent.turn-ended", Fields: []EventField{{Name: "source"}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeclareEvent(EventSpec{Name: EventSurfaceLeft}); err == nil {
		t.Fatal("an app may not replace a framework event")
	}
	if p := r.CheckHooks(); len(p) != 0 {
		t.Fatalf("declared event warns: %v", p)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.events", Verb: "emit", Args: Arguments{"event": "agent.turn-ended", "fields": `{"source": "pane-state"}`}}); err != nil {
		t.Fatal(err)
	}
	r.Emit("agent.turn-ended", map[string]any{"source": "hook"})
	if len(*ran) != 1 || (*ran)[0]["event"] != "agent.turn-ended" {
		t.Fatalf("runs %v", *ran)
	}
	names := []string{}
	for _, e := range r.Events() {
		names = append(names, e.Name+":"+e.Source)
	}
	if !slices.Equal(names, []string{"surface.opened:ochazuke", "surface.left:ochazuke", "app.quitting:ochazuke", "agent.turn-ended:app"}) {
		t.Fatalf("events %v", names)
	}
}

// Two rows that feed each other stop at HookChainLimit with a notice.
func TestHookChainLimit(t *testing.T) {
	r, _ := hooksFixture(t)
	r.Register(Operation{Noun: "ping", Verb: "pong", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		r.Emit("ping.again", nil)
		return nil, nil
	}})
	if err := r.AddDefaultHooks(HookRow{On: "ping.again", Do: "ping.pong"}); err != nil {
		t.Fatal(err)
	}
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	r.Emit("ping.again", nil)
	if n := len(sink.Notices()); n != 1 || !strings.Contains(sink.Notices()[0], "more than 32 events") {
		t.Fatalf("notices %v", sink.Notices())
	}
}

// Quitting emits app.quitting with what changed since the last
// surface.left; the Model waits for the jobs its hooks started, then quits.
func TestAppQuittingWaitsForHookJobs(t *testing.T) {
	r, _ := hooksFixture(t)
	release := make(chan struct{})
	var got Arguments
	r.Define("tasks").Verb("sync", func(c Ctx) Result {
		got = c.Args
		return Pending(func(context.Context) (any, error) { <-release; return nil, nil })
	})
	r.Operations["tasks.sync"] = withParam(r.Operations["tasks.sync"], "changes", Parameter{Type: "integer"})
	if err := r.AddDefaultHooks(HookRow{On: EventAppQuitting, Where: map[string]any{"changed": "task"}, Do: "tasks.sync", From: map[string]string{"changes": "changes"}}); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if _, err := r.Execute(Request{Noun: "task", Verb: "edit"}); err != nil {
		t.Fatal(err)
	}
	r.Quit = true
	cmd, wait := m.quitHooks()
	if !wait || cmd == nil || got["changes"] != 1 {
		t.Fatalf("quit must wait for the hook's job: wait=%v args=%v", wait, got)
	}
	if _, wait := m.quitHooks(); !wait {
		t.Fatal("still running: still waiting")
	}
	later := time.Now().Add(QuitHookWait + time.Second)
	m.Now = func() time.Time { return later }
	if _, wait := m.quitHooks(); wait {
		t.Fatal("past QuitHookWait the Model quits")
	}
	close(release)
}

func withParam(op Operation, name string, p Parameter) Operation {
	params := map[string]Parameter{}
	for k, v := range op.Parameters {
		params[k] = v
	}
	params[name] = p
	op.Parameters = params
	return op
}
