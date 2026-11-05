package ochazuke

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke/keytest"
)

func TestStandardEditChangeFlushes(t *testing.T) {
	t.Parallel()
	for _, boundary := range []string{"debounce", "field", "close", "quit", "escape"} {
		t.Run(boundary, func(t *testing.T) {
			t.Parallel()
			r, s := editFixture(t)
			m := NewModel(r)
			mustRun(t, r, Request{Noun: "parcel", Verb: "edit"})
			e := r.RecordEditor(editFormID)
			for _, key := range []string{"A", "x"} {
				keytest.Press(m, key)
				if s.find("t1")["title"] != "Original" {
					t.Fatalf("before debounce after %q: %v", key, s.find("t1"))
				}
			}
			tick, pending := e.DebounceTick()
			if !pending {
				t.Fatal("typing did not arm debounce")
			}
			switch boundary {
			case "debounce":
				m.Update(tick)
			case "field":
				keytest.Press(m, "tab")
			case "close":
				if err := e.Leave(); err != nil {
					t.Fatal(err)
				}
			case "quit":
				keytest.Press(m, "ctrl+c")
			case "escape":
				keytest.Press(m, "esc")
				keytest.Press(m, "esc")
			}
			if s.find("t1")["title"] != "Originalx" {
				t.Fatalf("%s did not flush typing: %v", boundary, s.find("t1"))
			}
			if boundary != "quit" {
				if !e.Closed() {
					if err := e.Leave(); err != nil {
						t.Fatal(err)
					}
				}
				mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo"})
				if s.find("t1")["title"] != "Original" {
					t.Fatalf("undo %v", s.find("t1"))
				}
			}
		})
	}
}

func TestStandardEditChangeRejectsOneField(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	st := s.store()
	st.Undo = true
	patch := st.Patch
	st.Patch = func(c Ctx, key string, changes Thing) (Thing, error) {
		if changes["title"] == "Rejected" {
			return nil, errors.New("title refused")
		}
		return patch(c, key, changes)
	}
	r.Define("parcel").Props(PropSpec{Name: "details", Parameter: Parameter{Type: PropObject}}).Store(st)
	m := NewModel(r)
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit"})
	e := r.RecordEditor(editFormID)
	for _, key := range []string{"i", "ctrl+u", "Rejected", "esc"} {
		keytest.Press(m, key)
	}
	if s.find("t1")["title"] != "Original" || !strings.Contains(e.Problem("title"), "title refused") {
		t.Fatalf("refusal store %v problem %q", s.find("t1"), e.Problem("title"))
	}
	if err := r.SetFocus(editFormID + "/details"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"i", "not-json", "esc"} {
		keytest.Press(m, key)
	}
	if !strings.Contains(e.Problem("details"), "object") {
		t.Fatalf("invalid prop problem %q", e.Problem("details"))
	}
	if err := r.SetFocus(editFormID + "/active"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"space", "k", "enter"} {
		keytest.Press(m, key)
	}
	want := Thing{"id": "t1", "title": "Original", "status": "todo", "active": true, "stamp": "fixed"}
	if !reflect.DeepEqual(s.find("t1"), want) || e.Closed() {
		t.Fatalf("independent field store %v closed %t", s.find("t1"), e.Closed())
	}
	if !strings.Contains(e.Problem("title"), "title refused") {
		t.Fatalf("other write hid title failure: %q", e.Problem("title"))
	}
}

func TestStandardEditPendingWriteFlushes(t *testing.T) {
	t.Parallel()
	for _, boundary := range []string{"close", "quit", "failed-older-value"} {
		t.Run(boundary, func(t *testing.T) {
			t.Parallel()
			r, s := editFixture(t)
			op, _ := r.Operation("parcel.set")
			sent := 0
			r.Register(Operation{Noun: "parcel", Verb: "set", Parameters: op.Parameters, HistoryKind: HistoryChange, App: func(c Ctx) Result {
				sent++
				changes := maps.Clone(c.Args["changes"].(map[string]any))
				return Pending(func(context.Context) (any, error) {
					if boundary == "failed-older-value" && changes["title"] == "Originalx" {
						return nil, errors.New("remote refused older value")
					}
					maps.Copy(s.find("t1"), changes)
					return maps.Clone(s.find("t1")), nil
				})
			}})
			m := NewModel(r)
			mustRun(t, r, Request{Noun: "parcel", Verb: "edit"})
			e := r.RecordEditor(editFormID)
			press := func(key string) {
				if _, err := r.HandleBinding(key); err != nil {
					t.Fatal(err)
				}
				e.PendingCommand() // debounce ticks are delivered explicitly in these tests
			}
			press("A")
			press("x")
			if boundary == "quit" {
				runQuitCommands(m, keytest.Press(m, "ctrl+c"))
				if s.find("t1")["title"] != "Originalx" {
					t.Fatalf("quit lost pending write: %v", s.find("t1"))
				}
				return
			}
			press("esc")
			if sent != 1 || s.find("t1")["title"] != "Original" {
				t.Fatalf("pending write ran inline: sent=%d store=%v", sent, s.find("t1"))
			}
			if boundary == "failed-older-value" {
				for _, key := range []string{"A", "y", "esc"} {
					press(key)
				}
				drainEditWrites(r)
				if s.find("t1")["title"] != "Originalxy" || e.Problem("title") != "" {
					t.Fatalf("newer value lost: %v problem %q", s.find("t1"), e.Problem("title"))
				}
			} else {
				for _, key := range []string{"j", "space", "j", "enter", "esc"} {
					press(key)
				}
				drainEditWrites(r)
				if s.find("t1")["title"] != "Originalx" || s.find("t1")["status"] != "done" || r.TopModal() != "" {
					t.Fatalf("close lost pending fields: %v modal %s", s.find("t1"), r.TopModal())
				}
			}
		})
	}
}

func drainEditWrites(r *Runtime) {
	for cmd := r.PendingCommands(); cmd != nil; cmd = r.PendingCommands() {
		deliverEditCommand(r, cmd)
	}
}

func deliverEditCommand(r *Runtime, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, next := range batch {
			deliverEditCommand(r, next)
		}
	} else {
		r.Deliver(msg)
	}
}

func runQuitCommands(m *Model, cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); ok {
		return true
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, next := range batch {
			if runQuitCommands(m, next) {
				return true
			}
		}
		return false
	}
	_, next := m.Update(msg)
	return runQuitCommands(m, next)
}

func TestStandardEditModelReturnsPendingSave(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	op, _ := r.Operation("parcel.set")
	r.Register(Operation{Noun: "parcel", Verb: "set", Parameters: op.Parameters, App: func(c Ctx) Result {
		return Pending(func(context.Context) (any, error) {
			maps.Copy(s.find("t1"), c.Args["changes"].(map[string]any))
			return nil, nil
		})
	}})
	m := NewModel(r)
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit"})
	keytest.Press(m, "A")
	keytest.Press(m, "x")
	deliverEditCommand(r, keytest.Press(m, "esc"))
	if s.find("t1")["title"] != "Originalx" {
		t.Fatalf("model did not return its pending save command: %v", s.find("t1"))
	}
}

func TestStandardEditChangeWalk(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	m := NewModel(r)
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit"})
	for _, step := range []struct {
		key, title, status string
		active             bool
	}{
		{"i", "Original", "todo", false}, {"ctrl+u", "Original", "todo", false}, {"Edited", "Original", "todo", false},
		{"esc", "Edited", "todo", false}, {"j", "Edited", "todo", false}, {"space", "Edited", "todo", false},
		{"j", "Edited", "todo", false}, {"enter", "Edited", "done", false}, {"j", "Edited", "done", false},
		{"space", "Edited", "done", false}, {"k", "Edited", "done", false}, {"enter", "Edited", "done", true},
		{"esc", "Edited", "done", true},
	} {
		keytest.Press(m, step.key)
		want := Thing{"id": "t1", "title": step.title, "status": step.status, "active": step.active, "stamp": "fixed"}
		if !reflect.DeepEqual(s.find("t1"), want) {
			t.Fatalf("after %q store %v want %v", step.key, s.find("t1"), want)
		}
	}
	for _, want := range []Thing{
		{"id": "t1", "title": "Edited", "status": "done", "active": false, "stamp": "fixed"},
		{"id": "t1", "title": "Edited", "status": "todo", "active": false, "stamp": "fixed"},
		{"id": "t1", "title": "Original", "status": "todo", "active": false, "stamp": "fixed"},
	} {
		mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo"})
		if !reflect.DeepEqual(s.find("t1"), want) {
			t.Fatalf("undo store %v want %v", s.find("t1"), want)
		}
	}
}

func TestStandardEditReferenceChoice(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	r.Define("project")
	r.Instance("p1", Kind("project")).Label = "Alpha"
	r.Instance("p2", Kind("project")).Label = "Beta"
	s.find("t1")["project"] = "p1"
	r.Define("parcel").Props(PropSpec{Name: "project", Parameter: Parameter{Noun: "project", Free: true}})
	m := NewModel(r)
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit"})
	if err := r.SetFocus(editFormID + "/project"); err != nil {
		t.Fatal(err)
	}
	e := r.RecordEditor(editFormID)
	if text := e.Block(60, 20).Text; !strings.Contains(text, "Alpha") || strings.Contains(text, "p1") {
		t.Fatalf("reference label: %s", text)
	}
	for _, key := range []string{"i", "ctrl+u", "zz", "esc"} {
		keytest.Press(m, key)
		if s.find("t1")["project"] != "p1" {
			t.Fatalf("partial reference after %q: %v", key, s.find("t1"))
		}
	}
	for _, key := range []string{"i", "Beta", "enter"} {
		keytest.Press(m, key)
	}
	if s.find("t1")["project"] != "p2" {
		t.Fatalf("chosen reference: %v", s.find("t1"))
	}
	for _, key := range []string{"i", "(none)", "enter"} {
		keytest.Press(m, key)
	}
	if s.find("t1")["project"] != nil {
		t.Fatalf("clear reference: %v", s.find("t1"))
	}
	keytest.Press(m, "u")
	if s.find("t1")["project"] != "p2" {
		t.Fatalf("undo clear: %v", s.find("t1"))
	}
}
