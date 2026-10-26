package ochazuke

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// taskFields is the schema the tests edit: a required title, a status
// chosen from three, a whole-number estimate and multiline notes.
func taskFields() []RecordField {
	return []RecordField{
		{Name: "title", Parameter: Parameter{Type: "string", Required: true}},
		{Name: "status", Parameter: Parameter{Type: "string", Required: true, Enum: []string{"todo", "doing", "done"}}},
		{Name: "estimate", Parameter: Parameter{Type: "integer"}},
		{Name: "notes", Multiline: true},
	}
}

// recordFixture is a page with a list and a placed record editor beside
// it, the editor focused; writes are logged by their String.
func recordFixture(t *testing.T, save func(RecordWrite) Result) (*Runtime, *Model, *RecordEditor, *[]string) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 80, 24
	var ed *RecordEditor
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block {
		list := r.List("tasks").View(w, 2)
		if ed == nil || ed.Closed() {
			return Text(list)
		}
		b := ed.Block(w, h-3)
		return b.Offset(0, 3).WithText(list + "\n\n" + b.Text)
	}); err != nil {
		t.Fatal(err)
	}
	r.Define("task")
	if _, err := r.AddList("tasks", []ListItem{{ID: "t1", Label: "one", Noun: "task"}, {ID: "t2", Label: "two", Noun: "task"}}, Parent("main")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	var writes []string
	if save == nil {
		save = func(RecordWrite) Result { return Done(nil) }
	}
	spec := RecordSpec{Fields: taskFields(), Debounce: time.Millisecond, Save: func(w RecordWrite) Result {
		res := save(w)
		if res.err == nil && res.work == nil {
			writes = append(writes, w.String())
		}
		return res
	}, Load: func() (Record, error) {
		return Record{"title": "buy milk", "status": "todo"}, nil
	}}
	var err error
	ed, err = r.AddRecordEditor("ed", spec, Parent("main"), FocusOnClose("t1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.Focus(); err != nil {
		t.Fatal(err)
	}
	m.View()
	return r, m, ed, &writes
}

// settle runs a command and feeds every message it produces back to the
// model until nothing is left (ticks are 1ms in these fixtures).
func settle(m *Model, cmd tea.Cmd) {
	for range 20 {
		if cmd == nil {
			return
		}
		msg := cmd()
		var next []tea.Cmd
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c == nil {
					continue
				}
				_, out := m.Update(c())
				next = append(next, out)
			}
		} else {
			_, out := m.Update(msg)
			next = append(next, out)
		}
		cmd = tea.Batch(next...)
	}
}

func screen(m *Model) string { return ansi.Strip(m.View().Content) }

// Typing is written once after the pause, not per key, and leaving insert
// writes what the pause has not.
func TestRecordTypingWritesAfterThePause(t *testing.T) {
	r, m, ed, writes := recordFixture(t, nil)
	keytest.Press(m, "A")
	var last tea.Cmd
	for _, k := range []string{" ", "n", "o", "w"} {
		_, last = m.Update(keytest.Msg(k, true, false))
	}
	if len(*writes) != 0 {
		t.Fatalf("written before the pause: %v", *writes)
	}
	settle(m, last)
	if want := []string{`write title="buy milk now"`}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	m.Update(keytest.Msg("!", true, false))
	keytest.Press(m, "esc")
	if want := `write title="buy milk now!"`; len(*writes) != 2 || (*writes)[1] != want {
		t.Fatalf("writes %v, want %s last", *writes, want)
	}
	if r.Mode.Name != "normal" || r.Focus != "ed/title" || ed.Written()["title"] != "buy milk now!" {
		t.Fatalf("mode %s focus %s written %v", r.Mode.Name, r.Focus, ed.Written())
	}
}

// An invalid value is never written; its label says why, and fixing it
// writes it.
func TestRecordInvalidFieldIsNotWritten(t *testing.T) {
	r, m, ed, writes := recordFixture(t, nil)
	keytest.Press(m, "j")
	keytest.Press(m, "j")
	if r.Focus != "ed/estimate" {
		t.Fatalf("focus %s, want ed/estimate", r.Focus)
	}
	keytest.Press(m, "i")
	m.Update(keytest.Msg("x", true, false))
	keytest.Press(m, "esc")
	if len(*writes) != 0 || ed.Problem("estimate") != "a whole number" {
		t.Fatalf("writes %v problem %q", *writes, ed.Problem("estimate"))
	}
	if s := screen(m); !strings.Contains(s, "estimate · not written: a whole number") {
		t.Fatalf("label lacks why:\n%s", s)
	}
	keytest.Press(m, "k")
	keytest.Press(m, "k")
	keytest.Press(m, "A")
	keytest.Press(m, "ctrl+u")
	keytest.Press(m, "esc")
	if !strings.Contains(screen(m), "title · not written: required") || len(*writes) != 0 {
		t.Fatalf("an empty required title was written: %v\n%s", *writes, screen(m))
	}
	_ = ed
}

// A choice field is edited through the chooser: letters do not type into
// it, enter opens the choices, and a pick is written at once.
func TestRecordChoiceFieldUsesTheChooser(t *testing.T) {
	r, m, ed, writes := recordFixture(t, nil)
	keytest.Press(m, "j")
	if r.Focus != "ed/status" || r.Input(r.Focus) != nil {
		t.Fatalf("focus %s, want the status choice", r.Focus)
	}
	keytest.Press(m, "x")
	if ed.Values()["status"] != "todo" {
		t.Fatalf("a letter changed the choice: %v", ed.Values())
	}
	keytest.Press(m, "enter")
	if r.TopModal() != formChooseID {
		t.Fatalf("top modal %q, want the chooser", r.TopModal())
	}
	if s := screen(m); !strings.Contains(s, "Choose status") || !strings.Contains(s, "doing") {
		t.Fatalf("chooser not drawn:\n%s", s)
	}
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	if r.TopModal() != "" || r.Focus != "ed/status" {
		t.Fatalf("modal %q focus %s after the pick", r.TopModal(), r.Focus)
	}
	if want := []string{"write status=doing"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	// esc leaves the chooser without a write.
	keytest.Press(m, "enter")
	keytest.Press(m, "esc")
	if r.TopModal() != "" || len(*writes) != 1 {
		t.Fatalf("modal %q writes %v", r.TopModal(), *writes)
	}
}

// u undoes the record's last written change (in any field) and ctrl+r
// redoes it; each is written.
func TestRecordUndoRedoAreWritten(t *testing.T) {
	r, m, _, writes := recordFixture(t, nil)
	keytest.Press(m, "A")
	m.Update(keytest.Msg("!", true, false))
	keytest.Press(m, "esc")
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	keytest.Press(m, "j")
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	keytest.Press(m, "u")
	keytest.Press(m, "u")
	if r.Focus != "ed/title" {
		t.Fatalf("focus %s after undoing the title", r.Focus)
	}
	keytest.Press(m, "ctrl+r")
	want := []string{`write title="buy milk!"`, "write status=done", "write status=todo", `write title="buy milk"`, `write title="buy milk!"`}
	if !slices.Equal(*writes, want) {
		t.Fatalf("writes\n%q\nwant\n%q", *writes, want)
	}
}

// Fields are stops: tab jumps between them, tab while typing moves on and
// keeps typing, and q leaves to the row the editor names.
func TestRecordFieldsAreStopsAndQLeaves(t *testing.T) {
	r, m, ed, writes := recordFixture(t, nil)
	keytest.Press(m, "tab")
	if r.Focus != "ed/status" {
		t.Fatalf("tab: focus %s", r.Focus)
	}
	keytest.Press(m, "tab")
	keytest.Press(m, "i")
	m.Update(keytest.Msg("3", true, false))
	keytest.Press(m, "tab")
	if r.Focus != "ed/notes" || r.Mode.Name != "insert" {
		t.Fatalf("focus %s mode %s after tab in insert", r.Focus, r.Mode.Name)
	}
	if want := []string{"write estimate=3"}; !slices.Equal(*writes, want) {
		t.Fatalf("leaving the field did not write: %v", *writes)
	}
	m.Update(keytest.Msg("hi", true, true))
	keytest.Press(m, "esc")
	keytest.Press(m, "q")
	if !ed.Closed() || r.Focus != "t1" || r.Instances["ed"] != nil {
		t.Fatalf("closed %v focus %s", ed.Closed(), r.Focus)
	}
	if want := "write notes=hi"; (*writes)[len(*writes)-1] != want {
		t.Fatalf("writes %v, want %s last", *writes, want)
	}
}

// A pending save runs off the loop, one at a time: a change made while one
// is in flight goes in the next write when the first lands.
func TestRecordPendingWritesAreSerialized(t *testing.T) {
	var sent []string
	release := make(chan struct{}, 4)
	_, m, ed, _ := recordFixture(t, func(w RecordWrite) Result {
		sent = append(sent, w.String())
		return Pending(func(context.Context) (any, error) { <-release; return nil, nil })
	})
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	keytest.Press(m, "j")
	_, first := m.Update(keytest.Msg("enter", false, false))
	keytest.Press(m, "enter")
	keytest.Press(m, "k")
	keytest.Press(m, "enter")
	if want := []string{"write status=doing"}; !slices.Equal(sent, want) {
		t.Fatalf("sent %v while one write is in flight, want %v", sent, want)
	}
	if ed.Written()["status"] != "todo" {
		t.Fatalf("written %v", ed.Written())
	}
	release <- struct{}{}
	release <- struct{}{}
	settle(m, first)
	if want := []string{"write status=doing", "write status=todo"}; !slices.Equal(sent, want) {
		t.Fatalf("sent %v, want %v", sent, want)
	}
}

// A failed write keeps the value, says so and is retried at the next
// write (here leaving the field).
func TestRecordFailedWriteRetries(t *testing.T) {
	fail, calls := 1, 0
	var stored []string
	_, m, ed, _ := recordFixture(t, func(w RecordWrite) Result {
		calls++
		if fail > 0 {
			fail--
			return Failed(errors.New("store unavailable"))
		}
		stored = append(stored, w.String())
		return Done(nil)
	})
	keytest.Press(m, "A")
	m.Update(keytest.Msg("!", true, false))
	keytest.Press(m, "esc")
	if ed.WriteError() == "" || !strings.Contains(screen(m), "! not saved: store unavailable") {
		t.Fatalf("no failure shown:\n%s", screen(m))
	}
	m.Update(nil)
	m.Update(nil)
	if len(stored) != 0 || calls != 1 {
		t.Fatalf("stored %v after %d calls before the next write", stored, calls)
	}
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	if want := []string{`write title="buy milk!"`, "write status=doing"}; !slices.Equal(stored, want) {
		t.Fatalf("stored %v, want %v", stored, want)
	}
	if strings.Contains(screen(m), "not saved") {
		t.Fatal("the failure stayed after a good write")
	}
}

// A modal editor opens over the focus and q gives it back, in its mode.
func TestRecordModalEditorRestoresTheFocus(t *testing.T) {
	r, m, ed, _ := recordFixture(t, nil)
	keytest.Press(m, "q")
	if !ed.Closed() || r.Focus != "t1" {
		t.Fatalf("focus %s", r.Focus)
	}
	var writes []string
	modal, err := r.OpenRecordEditor("edit-task", RecordSpec{Title: "Edit task", Fields: taskFields(), Insert: true, Save: func(w RecordWrite) Result {
		writes = append(writes, w.String())
		return Done(nil)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != "edit-task" || r.Focus != "edit-task/title" || r.Mode.Name != "insert" {
		t.Fatalf("modal %q focus %s mode %s", r.TopModal(), r.Focus, r.Mode.Name)
	}
	m.Update(keytest.Msg("x", true, false))
	if s := screen(m); !strings.Contains(s, "Edit task") || !strings.Contains(s, "title") {
		t.Fatalf("modal editor not drawn:\n%s", s)
	}
	keytest.Press(m, "esc")
	keytest.Press(m, "esc")
	if !modal.Closed() || r.TopModal() != "" || r.Focus != "t1" || r.Mode.Name != "normal" {
		t.Fatalf("closed %v modal %q focus %s", modal.Closed(), r.TopModal(), r.Focus)
	}
	if want := []string{"write title=x"}; !slices.Equal(writes, want) {
		t.Fatalf("writes %v, want %v", writes, want)
	}
}

// A verb's parameters are a form's schema, in CLI order.
func TestRecordFieldsFromAnOperation(t *testing.T) {
	op := Operation{Verb: "edit", Noun: "torrent", Positional: []string{"name"}, Parameters: map[string]Parameter{
		"name":  {Type: "string", Required: true},
		"state": {Type: "string", Enum: []string{"paused", "running"}},
		"limit": {Type: "integer"},
	}}
	var names []string
	for _, f := range RecordFields(op) {
		names = append(names, f.Name)
	}
	if want := []string{"name", "limit", "state"}; !slices.Equal(names, want) {
		t.Fatalf("fields %v, want %v", names, want)
	}
}

// The form's rows are atomic and its verbs show in the help.
func TestRecordBindingsValidate(t *testing.T) {
	r, _, _, _ := recordFixture(t, nil)
	if warnings, err := r.ValidateBindings(); err != nil || len(warnings) > 0 {
		t.Fatal(warnings, err)
	}
	ops := map[string]bool{}
	for _, row := range BindingHelp(r, "ed/status") {
		ops[row.Operation] = true
	}
	for _, op := range []string{"form.leave", "form.undo", "form.choose", "form.next"} {
		if !ops[op] {
			t.Fatalf("help lacks %s: %v", op, ops)
		}
	}
}

// An edit in normal mode (x) is written at once, as its own undo step.
func TestRecordNormalModeEditWritesAtOnce(t *testing.T) {
	_, m, _, writes := recordFixture(t, nil)
	keytest.Press(m, "0")
	keytest.Press(m, "x")
	if want := []string{`write title="uy milk"`}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	keytest.Press(m, "u")
	if want := `write title="buy milk"`; len(*writes) != 2 || (*writes)[1] != want {
		t.Fatalf("writes %v, want %s last", *writes, want)
	}
}
