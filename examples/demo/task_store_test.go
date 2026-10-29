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
	"github.com/unixsuperhero/ochazuke/internal/keytest"
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
	if want := map[string]string{"edit": "", "get": "get", "list": "list"}; !reflect.DeepEqual(standard, want) {
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
