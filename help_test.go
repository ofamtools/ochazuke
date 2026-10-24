package ochazuke

import (
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
