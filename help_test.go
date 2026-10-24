package ochazuke

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// helpFixture is a page holding a list of widget rows; widget has open on
// enter and edit on i.
func helpFixture(t *testing.T) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	noop := func(Ctx) Result { return Done(nil) }
	r.Define("widget").
		Verb("open", noop, "Open the widget").
		Verb("edit", noop, "Edit the widget").
		Mode("normal").
		Bind("enter", Do("widget.open")).
		Bind("i", Do("widget.edit"))
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return Text(r.List("widgets").View(w, h)) }); err != nil {
		t.Fatal(err)
	}
	list, err := r.AddList("widgets", []ListItem{{ID: "w1", Label: "one", Noun: "widget"}, {ID: "w2", Label: "two", Noun: "widget"}}, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	m.Width, m.Height = 80, 24
	return r, m
}

// ? on a focused row opens a read-only list of the focus's bindings, the
// row's own verbs first and the framework (app) rows last; esc closes it
// and gives the focus back to the row.
func TestHelpListsTheFocusedRowsVerbsFirst(t *testing.T) {
	r, m := helpFixture(t)
	if r.Focus != "w1" {
		t.Fatalf("focus %q", r.Focus)
	}
	keytest.Press(m, "?")
	if r.TopModal() != helpID {
		t.Fatalf("top modal %q, status %q", r.TopModal(), m.Status)
	}
	items := r.List(helpListID).Items()
	if len(items) < 3 {
		t.Fatalf("items: %+v", items)
	}
	fields := func(i int) []string { return strings.Fields(items[i].Label) }
	if f := fields(0); f[0] != "enter" || f[1] != "widget.open" || strings.Join(f[2:], " ") != "Open the widget" {
		t.Fatalf("first row %q", items[0].Label)
	}
	if f := fields(1); f[0] != "i" || f[1] != "widget.edit" {
		t.Fatalf("second row %q", items[1].Label)
	}
	last := items[len(items)-1].Data.(HelpRow)
	if last.Noun != "app" {
		t.Fatalf("last row is not an app row: %+v", last)
	}
	for _, it := range items {
		if row := it.Data.(HelpRow); row.Key == "ctrl+c" && row.Noun != "app" {
			t.Fatalf("ctrl+c grouped under %q", row.Noun)
		}
	}
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Keys") || !strings.Contains(strings.Join(strings.Fields(frame), " "), "> enter widget.open Open the widget") {
		t.Fatalf("frame:\n%s", frame)
	}
	// Read-only: enter does not run widget.open beneath the modal.
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	if r.TopModal() != helpID {
		t.Fatalf("enter left the help: %q", r.TopModal())
	}
	keytest.Press(m, "esc")
	if r.TopModal() != "" || r.Focus != "w1" || r.Instances[helpListID] != nil {
		t.Fatalf("esc: modal %q focus %q", r.TopModal(), r.Focus)
	}
}

// ? is a normal-mode row: in insert mode it is typed, and in a passthrough
// mode (an attached terminal) it reaches the pane.
func TestHelpDoesNotShadowInsertOrTerminal(t *testing.T) {
	r := NewRuntime()
	in := NewInput("field", "", false)
	if err := r.AddInput(in); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.SetFocus("field"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "i")
	keytest.Press(m, "?")
	if r.TopModal() != "" || string(in.Text) != "?" {
		t.Fatalf("insert ?: modal %q text %q", r.TopModal(), string(in.Text))
	}

	r, _, stream := terminalFixture(t)
	m = NewModel(r)
	keytest.Press(m, "?")
	if r.TopModal() != "" || len(stream.sent) == 0 || stream.sent[len(stream.sent)-1][0] != "?" {
		t.Fatalf("terminal ?: modal %q sent %v", r.TopModal(), stream.sent)
	}
}

var updateHelp = flag.Bool("update-help", false, "rewrite testdata/help-80.golden")

// The help modal at 80 columns for a task-like row: the key column is as
// wide as the longest literal key, pattern rows read short (1-9), the
// operation column is fixed, and every description reads in the rest.
func TestHelpGoldenAt80Columns(t *testing.T) {
	r := NewRuntime()
	noop := func(Ctx) Result { return Done(nil) }
	r.Define("task").
		Verb("open", noop, "Open the task's page").
		Verb("done", noop, "Mark the task done and move it to the archive").
		Verb("edit", noop, "Edit the task's title in a prompt").
		Verb("note", noop, "Add a note to the task").
		Verb("delete", noop, "Delete the task after a confirm").
		Mode("normal").
		Bind("enter", Do("task.open")).
		Bind("x", Do("task.done")).
		Bind("e", Do("task.edit")).
		Bind("n", Do("task.note")).
		Bind("d d", Do("task.delete"))
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return Text(r.List("tasks").View(w, h)) }); err != nil {
		t.Fatal(err)
	}
	list, err := r.AddList("tasks", []ListItem{{ID: "t1", Label: "write the brief", Noun: "task"}}, Parent("main"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	m.Width, m.Height = 80, 72
	keytest.Press(m, "?")
	if r.TopModal() != helpID {
		t.Fatalf("top modal %q, status %q", r.TopModal(), m.Status)
	}
	var lines []string
	for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		lines = append(lines, strings.TrimRight(line, " "))
	}
	got := []byte(strings.Join(lines, "\n") + "\n")
	golden := filepath.Join("testdata", "help-80.golden")
	if *updateHelp {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("help differs from %s (go test -run TestHelpGoldenAt80Columns -update-help):\n%s", golden, got)
	}
}

// Pattern rows read short in the key column; literal keys stay as they are.
func TestHelpKeyShortensPatterns(t *testing.T) {
	for key, want := range map[string]string{"<digit>": "0-9", "<digit-0>": "1-9", "<alpha>": "a-z", "<alpha-q>": "a-z -q", "<printable>": "char", "<any>": "any", "<one-of:h l>": "h/l", "<": "<", "shift+tab": "shift+tab"} {
		got, literal := helpKey(key)
		if got != want || literal != (got == key) {
			t.Fatalf("helpKey(%q) = %q, %v; want %q", key, got, literal, want)
		}
	}
}
