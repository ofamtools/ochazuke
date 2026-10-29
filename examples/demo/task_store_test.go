package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/cli"
	"github.com/unixsuperhero/ochazuke/keytest"
	"github.com/unixsuperhero/oden"
)

// runDemo runs one demo CLI command and answers its stdout.
func runDemo(t *testing.T, args ...string) string {
	t.Helper()
	m := New()
	var stdout, stderr bytes.Buffer
	app := oden.New("demo", args, oden.WithStdio(strings.NewReader(""), &stdout, &stderr), oden.WithPath(t.TempDir()))
	cli.Mount(app, cli.New(m.Runtime, "demo", strings.NewReader(""), &stdout, &stderr))
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("demo %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String()
}

// The demo's task get and task list are standard verbs from its store,
// with no handler of the demo's own: from the CLI, list answers the three
// tasks (the untitled one by its summary) and get one by its key.
func TestTaskStoreFromTheCLI(t *testing.T) {
	var rows []map[string]any
	if err := json.Unmarshal([]byte(runDemo(t, "task", "list")), &rows); err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, row := range rows {
		labels = append(labels, row["label"].(string))
	}
	if want := []string{"Water the plants", "Write the release notes", "Call the plumber about the sink"}; !reflect.DeepEqual(labels, want) {
		t.Fatalf("labels %v, want %v", labels, want)
	}
	var thing map[string]any
	if err := json.Unmarshal([]byte(runDemo(t, "task", "get", "t2")), &thing); err != nil {
		t.Fatal(err)
	}
	if thing["status"] != "doing" || thing["estimate"] != float64(3) || thing["id"] != "t2" {
		t.Fatalf("task get t2 = %v", thing)
	}
	if got := strings.TrimSpace(runDemo(t, "task", "get", "t3", "--prop", "status")); got != `"todo"` && got != "todo" {
		t.Fatalf("task get t3 --prop status = %q", got)
	}
	if out := runDemo(t, "task", "list", "--where", "status=todo", "--props", "status"); strings.Contains(out, "t2") || !strings.Contains(out, "t3") {
		t.Fatalf("task list --where status=todo = %s", out)
	}
}

// From the palette: task get on the Record page reads the task the page
// edits (the page stands for it), and task list opens the results list.
func TestTaskStoreFromThePalette(t *testing.T) {
	m := New()
	m.Width, m.Height = 100, 24
	m.Appearance.Theme = "mono"
	r := m.Runtime
	if err := r.OpenPage("task"); err != nil {
		t.Fatal(err)
	}
	got, err := r.Execute(ochazuke.Request{Noun: "task", Verb: "get", Args: ochazuke.Arguments{"prop": "title"}})
	if err != nil || got != "Water the plants" {
		t.Fatalf("task get on the Record page = %v, %v", got, err)
	}
	m.View()
	keytest.Press(m, "esc")
	keytest.Press(m, ":")
	keytest.Type(m, "task list")
	keytest.Press(m, "enter")
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"task.list", "Water the p", "Call the pl", "label"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame lacks %q:\n%s", want, frame)
		}
	}
}

// The catalog carries the task noun's kind and props as declared, and
// marks get and list standard (and not builtin).
func TestTaskCatalogRoundTrip(t *testing.T) {
	m := New()
	var doc cli.CatalogDoc
	if err := json.Unmarshal([]byte(runDemo(t, "catalog")), &doc); err != nil {
		t.Fatal(err)
	}
	var noun *cli.CatalogNoun
	for i := range doc.Nouns {
		if doc.Nouns[i].Name == "task" {
			noun = &doc.Nouns[i]
		}
	}
	if noun == nil {
		t.Fatal("no task noun")
	}
	want := m.Runtime.Props("task")
	for i := range want {
		want[i].Source = m.Runtime.ChoicesSource(want[i].Parameter)
	}
	if noun.Kind != ochazuke.Collection || !reflect.DeepEqual(noun.Props, want) {
		got, _ := json.Marshal(noun.Props)
		exp, _ := json.Marshal(want)
		t.Fatalf("kind %q props\n%s\nwant\n%s", noun.Kind, got, exp)
	}
	standard := map[string]string{}
	for _, v := range noun.Verbs {
		if v.Builtin {
			t.Fatalf("%s is builtin", v.Name)
		}
		standard[v.Name] = v.Standard
	}
	if want := map[string]string{"edit": "", "get": "get", "list": "list", "watch": "watch", "create": "create", "set": "set", "delete": "delete",
		"rename": "rename", "move": "move", "duplicate": "duplicate", "archive": "archive", "restore": "restore"}; !reflect.DeepEqual(standard, want) {
		t.Fatalf("standard %v, want %v", standard, want)
	}
	if verb, rows := doc.RowSource("task"); verb != "list" || rows != "task" {
		t.Fatalf("row source %s %s", verb, rows)
	}
	if testing.Verbose() {
		b, _ := json.MarshalIndent(noun, "", "  ")
		t.Log("\n" + string(b))
	}
}

// The demo's task creates, sets (patch is set's alias) and deletes from
// the CLI with no handler of its own; delete asks first, naming the task
// by its title, else its summary, and --yes answers.
func TestTaskWritesFromTheCLI(t *testing.T) {
	read := func(args ...string) map[string]any {
		t.Helper()
		var out map[string]any
		if err := json.Unmarshal([]byte(runDemo(t, args...)), &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	if got := read("task", "create", "Buy milk", "--estimate", "2"); got["id"] != "t4" || got["title"] != "Buy milk" || got["status"] != "todo" || got["estimate"] != float64(2) {
		t.Fatalf("create = %v", got)
	}
	if got := read("task", "set", "t2", "status", "done"); got["status"] != "done" {
		t.Fatalf("set = %v", got)
	}
	if got := read("task", "set", "t2", "title=Ship it", "estimate=5"); got["title"] != "Ship it" || got["estimate"] != float64(5) {
		t.Fatalf("set pairs = %v", got)
	}
	if got := read("task", "patch", "t2", "--changes", `{"status": "doing", "notes": "x"}`); got["status"] != "doing" || got["notes"] != "x" {
		t.Fatalf("patch = %v", got)
	}
	if got := read("task", "delete", "t3", "--yes"); got["deleted"] != "t3" {
		t.Fatalf("delete = %v", got)
	}
	m := New()
	var stdout, stderr bytes.Buffer
	app := oden.New("demo", []string{"task", "delete", "t3"}, oden.WithStdio(strings.NewReader("n\n"), &stdout, &stderr), oden.WithPath(t.TempDir()))
	cli.Mount(app, cli.New(m.Runtime, "demo", strings.NewReader("n\n"), &stdout, &stderr))
	if err := app.Run(context.Background()); err == nil || !strings.Contains(stderr.String(), "Delete task Call the plumber about the sink?") {
		t.Fatalf("delete without --yes: %v\n%s", err, stderr.String())
	}
}

// From the palette, the API and a key: on the Record page, :task set
// writes the task the page edits, :task delete asks (y deletes), :task
// create makes one from its title, a JSON prop request reads and writes a
// prop, and x (a prop row) sets the status to done.
func TestTaskWritesFromThePalette(t *testing.T) {
	m := New()
	m.Width, m.Height = 100, 24
	m.Appearance.Theme = "mono"
	r := m.Runtime
	if err := r.OpenPage("task"); err != nil {
		t.Fatal(err)
	}
	m.View()
	keytest.Press(m, "esc")
	get := func(id, prop string) any {
		t.Helper()
		v, err := r.Execute(ochazuke.Request{Noun: "task", Verb: "get", Args: ochazuke.Arguments{"id": id, "prop": prop}})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	palette := func(line string) string {
		keytest.Press(m, ":")
		keytest.Type(m, line)
		keytest.Press(m, "enter")
		return ansi.Strip(m.View().Content)
	}
	palette("task set t1 estimate 7")
	if v := get("t1", "estimate"); v != 7 {
		t.Fatalf("estimate %v", v)
	}
	if frame := palette("task delete t2"); !strings.Contains(frame, "Delete task Write the release notes?") {
		t.Fatalf("no confirm:\n%s", frame)
	}
	keytest.Press(m, "y")
	if _, err := r.Execute(ochazuke.Request{Noun: "task", Verb: "get", Args: ochazuke.Arguments{"id": "t2"}}); err == nil {
		t.Fatal("t2 is still there")
	}
	palette(`task create "Buy milk"`)
	if v := get("t3", "title"); v != "" {
		t.Fatalf("t3 title %v", v)
	}
	rows, err := r.Execute(ochazuke.Request{Noun: "task", Verb: "list", Args: ochazuke.Arguments{"where": "title=Buy"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.([]map[string]any)) != 0 {
		t.Fatalf("where is equality: %v", rows)
	}
	if v := get("t4", "title"); v != "Buy milk" {
		t.Fatalf("created %v", v)
	}
	// The API: a JSON request, prop-shaped, the way a socket client sends it.
	var q ochazuke.Request
	if err := json.Unmarshal([]byte(`{"noun": "task", "prop": "status", "value": "doing", "args": {"id": "t4"}}`), &q); err != nil {
		t.Fatal(err)
	}
	reply := make(chan ochazuke.Response, 1)
	r.HandleCall(ochazuke.CallMsg{Request: q, Reply: reply})
	if res := <-reply; res.Error != "" || get("t4", "status") != "doing" {
		t.Fatalf("API prop write: %+v", res)
	}
	keytest.Press(m, "x")
	if v := get("t1", "status"); v != "done" {
		t.Fatalf("x: status %v", v)
	}
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "x task.status") {
		t.Fatalf("the prop row's hint:\n%s", frame)
	}
}

// history.undo reverts the task writes (the store declares Undo and
// AcceptsKey): a set's old value comes back, a deleted task comes back
// under its key, a created one goes.
func TestTaskUndo(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	r := New().Runtime
	run := func(q ochazuke.Request) any {
		t.Helper()
		v, err := r.Execute(q)
		if err != nil {
			t.Fatalf("%+v: %v", q, err)
		}
		return v
	}
	get := func(id, prop string) any {
		return run(ochazuke.Request{Noun: "task", Verb: "get", Args: ochazuke.Arguments{"id": id, "prop": prop}})
	}
	undo := ochazuke.Request{Noun: "ochazuke.history", Verb: "undo"}
	before := get("t2", "status")
	run(ochazuke.Request{Noun: "task", Verb: "set", Args: ochazuke.Arguments{"id": "t2", "prop": "status", "value": "done"}})
	title := get("t3", "title")
	run(ochazuke.Request{Noun: "task", Verb: "delete", Confirmed: true, Args: ochazuke.Arguments{"id": "t3"}})
	run(ochazuke.Request{Noun: "task", Verb: "create", Args: ochazuke.Arguments{"title": "Buy milk"}})
	run(undo)
	if _, err := r.Execute(ochazuke.Request{Noun: "task", Verb: "get", Args: ochazuke.Arguments{"id": "t4"}}); err == nil {
		t.Fatal("the created task is still there")
	}
	run(undo)
	if got := get("t3", "title"); got != title {
		t.Fatalf("t3 came back as %v, want %v", got, title)
	}
	run(undo)
	if got := get("t2", "status"); got != before {
		t.Fatalf("t2 status %v, want %v", got, before)
	}
}

// The grammar of plan 2.6 reads the same from the CLI and the palette:
// after a noun a verb wins, then a prop (NOUN PROP [VALUE]: a read, or a
// write with a value), then a key; after a verb the key word may be left
// out when the first word is a prop (the subject is --KEY, or the focus).
func TestTaskGrammarFromTheCLIAndThePalette(t *testing.T) {
	if got := strings.TrimSpace(runDemo(t, "task", "status", "--id", "t2")); got != `"doing"` {
		t.Fatalf("task status --id t2 = %s", got)
	}
	if got := strings.TrimSpace(runDemo(t, "task", "st", "--id", "t3")); got != `"todo"` {
		t.Fatalf("task st (a prop prefix) = %s", got)
	}
	if got := strings.TrimSpace(runDemo(t, "task", "get", "status", "--id", "t1")); got != `"todo"` {
		t.Fatalf("task get status --id t1 = %s", got)
	}
	if got := strings.TrimSpace(runDemo(t, "task", "get", "t1", "status")); got != `"todo"` {
		t.Fatalf("task get t1 status = %s", got)
	}
	var thing map[string]any
	if err := json.Unmarshal([]byte(runDemo(t, "task", "status", "done", "--id", "t2")), &thing); err != nil || thing["status"] != "done" {
		t.Fatalf("task status done --id t2 = %v %v", thing, err)
	}
	if err := json.Unmarshal([]byte(runDemo(t, "task", "set", "estimate", "9", "--id", "t1")), &thing); err != nil || thing["estimate"] != float64(9) {
		t.Fatalf("task set estimate 9 --id t1 = %v %v", thing, err)
	}
	if err := json.Unmarshal([]byte(runDemo(t, "task", "set", "title=Ship", "estimate=4", "--id", "t1")), &thing); err != nil || thing["title"] != "Ship" || thing["estimate"] != float64(4) {
		t.Fatalf("task set title=Ship estimate=4 --id t1 = %v %v", thing, err)
	}
	// NOUN PROP takes one value word: the thing is --KEY, never a word.
	{
		m := New()
		var stdout, stderr bytes.Buffer
		app := oden.New("demo", []string{"task", "status", "done", "t3"}, oden.WithStdio(strings.NewReader(""), &stdout, &stderr), oden.WithPath(t.TempDir()))
		cli.Mount(app, cli.New(m.Runtime, "demo", strings.NewReader(""), &stdout, &stderr))
		if err := app.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "--id") {
			t.Fatalf("task status done t3: %v", err)
		}
	}

	// The palette, on the Record page (it stands for t1).
	m := New()
	m.Width, m.Height = 100, 24
	m.Appearance.Theme = "mono"
	r := m.Runtime
	if err := r.OpenPage("task"); err != nil {
		t.Fatal(err)
	}
	m.View()
	keytest.Press(m, "esc")
	get := func(id, prop string) any {
		t.Helper()
		v, err := r.Execute(ochazuke.Request{Noun: "task", Verb: "get", Args: ochazuke.Arguments{"id": id, "prop": prop}})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	palette := func(line string) string {
		keytest.Press(m, ":")
		keytest.Type(m, line)
		keytest.Press(m, "enter")
		return ansi.Strip(m.View().Content)
	}
	palette("task status done")
	if v := get("t1", "status"); v != "done" {
		t.Fatalf(":task status done: %v", v)
	}
	palette("task set estimate 6")
	if v := get("t1", "estimate"); v != 6 {
		t.Fatalf(":task set estimate 6: %v", v)
	}
	palette("task status doing --id t2")
	if v := get("t2", "status"); v != "doing" {
		t.Fatalf(":task status doing --id t2: %v", v)
	}
	if frame := palette("task status"); !strings.Contains(frame, "status") || !strings.Contains(frame, "done") {
		t.Fatalf(":task status shows no value:\n%s", frame)
	}
	keytest.Press(m, "esc")
	if frame := palette("task get"); !strings.Contains(frame, "estimate") || !strings.Contains(frame, "Water the plants") {
		t.Fatalf(":task get shows no record rows:\n%s", frame)
	}
}
