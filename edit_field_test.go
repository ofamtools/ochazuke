package ochazuke

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// edit {field} reaches the one-field form on a noun whose application
// registered its own edit (in either order), and with forms.edit off,
// which turns off only the generated full form.
func TestEditFieldOnAppEditAndFormsEditOff(t *testing.T) {
	t.Parallel()
	own := func(Ctx) Result { return Done("own edit") }
	for _, c := range []struct {
		name  string
		setup func(r *Runtime)
	}{
		{"app edit before the store", func(r *Runtime) { r.Define("parcel").Verb("edit", own, "The app's own edit") }},
		{"forms.edit off", func(r *Runtime) {
			if _, err := r.Settings().SetSession(EditFormsSetting, false); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		r := NewRuntime()
		c.setup(r)
		s := &derivedStore{key: "id", things: []Thing{{"id": "t1", "title": "Original", "status": "todo"}}}
		r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}, PropSpec{Name: "status"}).Store(s.store())
		r.Instance("row", Kind("parcel")).Data = Thing{"id": "t1"}
		if err := r.SetFocus("row"); err != nil {
			t.Fatal(err)
		}
		field := Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "@label"}}
		if !r.Available(field, "row", r.Mode) {
			t.Fatalf("%s: edit {field} is not available", c.name)
		}
		if err := r.Bind(Binding{Noun: "parcel", Mode: "normal", Keys: "r", Request: field}); err != nil {
			t.Fatalf("%s: the r row: %v", c.name, err)
		}
		mustRun(t, r, field)
		e := r.RecordEditor(editFormID)
		if e == nil || len(e.fields) != 1 || e.Values()["title"] != "Original" {
			t.Fatalf("%s: one-field form %+v", c.name, e)
		}
		if err := e.Leave(); err != nil {
			t.Fatal(err)
		}
		plain, err := r.Execute(Request{Noun: "parcel", Verb: "edit"})
		switch c.name {
		case "forms.edit off":
			if err == nil || !strings.Contains(err.Error(), "forms.edit is off") {
				t.Fatalf("%s: the full form ran: %v %v", c.name, plain, err)
			}
		default:
			if err != nil || plain != "own edit" {
				t.Fatalf("%s: the app's edit lost: %v %v", c.name, plain, err)
			}
		}
	}
	// The app registers its edit after the store: it still adopts field.
	r := NewRuntime()
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store((&derivedStore{key: "id", things: []Thing{{"id": "t1", "title": "x"}}}).store())
	r.Define("parcel").Verb("edit", own, "The app's own edit")
	if _, ok := r.Operations.At(Key("parcel.edit")).Parameters["field"]; !ok {
		t.Fatal("an edit registered after the store has no field")
	}
}

// enter in a one-field form writes and closes it; in a form of several
// fields it moves on as before.
func TestEnterClosesAOneFieldForm(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	m := NewModel(r)
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "title"}})
	keytest.Type(m, "Typed")
	keytest.Press(m, "enter")
	if r.RecordEditor(editFormID) != nil || s.find("t1")["title"] != "Typed" {
		t.Fatalf("form open %v, title %v", r.RecordEditor(editFormID) != nil, s.find("t1")["title"])
	}
}

// A removed verb's request, from a program or a binding row, says what to
// write instead; a role the noun lacks says so.
func TestRemovedVerbAndMissingRoleSayWhatNext(t *testing.T) {
	t.Parallel()
	r, _ := editFixture(t)
	if _, err := r.Execute(Request{Noun: "parcel", Verb: "rename"}); err == nil || !strings.Contains(err.Error(), `parcel rename was removed in R1: use edit {field: "@label"}`) {
		t.Fatalf("rename: %v", err)
	}
	if err := r.Bind(Binding{Noun: "parcel", Mode: "normal", Keys: "R", Request: Request{Noun: "parcel", Verb: "move"}}); err == nil || !strings.Contains(err.Error(), "move was removed") {
		t.Fatalf("bound move: %v", err)
	}
	if _, err := r.Execute(Request{Noun: "parcel", Verb: "set", Args: Arguments{"props": map[string]any{"@owner": "x"}}}); err == nil || !strings.Contains(err.Error(), "no writable prop with the role owner") {
		t.Fatalf("missing role: %v", err)
	}
}

// edit's bare words are the key, the field and chooser, in that order: a
// prop is given only as prop=value, so a word after the key is the field
// (refused when it names no prop) and never a prop's value, and a word
// past chooser is refused (UX R3 F3).
func TestEditPositionalWords(t *testing.T) {
	t.Parallel()
	r, m, _, _ := editSelectionFixture(t)
	p := m.Palette
	p.origin = r.Context()
	for line, want := range map[string]Arguments{
		"parcel edit t2 status":         {"id": "t2", "field": "status"},
		"parcel edit t2 status false":   {"id": "t2", "field": "status", "chooser": false},
		"parcel edit status false":      {"field": "status", "chooser": false},
		"parcel edit t2 hello":          {"id": "t2", "field": "hello"},
		"parcel edit t2 status=blocked": {"id": "t2", "status": "blocked"},
	} {
		c := p.parse(line)
		if c.Err != nil || len(c.Args) != len(want) {
			t.Fatalf("%q: %v %v", line, c.Args, c.Err)
		}
		for k, v := range want {
			if c.Args[k] != v {
				t.Fatalf("%q: %s=%#v, want %#v (%v)", line, k, c.Args[k], v, c.Args)
			}
		}
	}
	for _, line := range []string{"parcel edit t2 status done", "parcel edit t2 status false more"} {
		if c := p.parse(line); c.Err == nil {
			t.Fatalf("%q read: %v", line, c.Args)
		}
	}
	if q := (Request{Noun: "parcel", Verb: "edit", Args: Arguments{"id": "t2", "title": "New name"}}); p.commandText(q) != `edit parcel t2 "title=New name"` {
		t.Fatalf("spelled back: %s", p.commandText(q))
	}
}
