package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// edit {field} on a choice field opens its chooser, not the one-field
// form: the pick writes, closes it and returns the focus; history-entry.undo
// takes the pick back in one step.
func TestEditFieldChooserPickWritesAndCloses(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	m := NewModel(r)
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "status"}})
	c := r.Chooser(EditChooserKind)
	if c == nil || r.RecordEditor(editFormID) != nil || r.TopModal() != EditChooserKind {
		t.Fatalf("chooser %v form %v modal %q", c, r.RecordEditor(editFormID), r.TopModal())
	}
	if got := c.Title(); got != "Edit parcel status: t1" {
		t.Fatalf("title %q", got)
	}
	keytest.Type(m, "done")
	keytest.Press(m, "enter")
	if s.find("t1")["status"] != "done" || r.TopModal() != "" || r.Focus != "row" || r.Mode.Name != "normal" {
		t.Fatalf("pick: %v modal %q focus %q mode %q", s.find("t1"), r.TopModal(), r.Focus, r.Mode.Name)
	}
	mustRun(t, r, Request{Noun: "ochazuke.history-entry", Verb: "undo"})
	if s.find("t1")["status"] != "todo" {
		t.Fatalf("undo: %v", s.find("t1"))
	}
}

// esc on the chooser closes it with nothing written and the focus back.
func TestEditFieldChooserEscWritesNothing(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	m := NewModel(r)
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "active"}})
	if r.Chooser(EditChooserKind) == nil {
		t.Fatal("a boolean field opens its chooser")
	}
	keytest.Type(m, "true")
	keytest.Press(m, "esc")
	keytest.Press(m, "q")
	if r.TopModal() != "" || r.Focus != "row" || s.find("t1")["active"] != false {
		t.Fatalf("esc: modal %q focus %q %v", r.TopModal(), r.Focus, s.find("t1"))
	}
	// Picking the value it holds writes nothing (no history entry).
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "active"}})
	before := len(r.HistoryOf(0, HistoryAction))
	keytest.Type(m, "false")
	keytest.Press(m, "enter")
	if r.TopModal() != "" || len(r.HistoryOf(0, HistoryAction)) != before {
		t.Fatalf("same value: modal %q history %d -> %d", r.TopModal(), before, len(r.HistoryOf(0, HistoryAction)))
	}
}

// "(none)" clears an optional choice field.
func TestEditFieldChooserNoneClears(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	m := NewModel(r)
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "status"}})
	keytest.Type(m, "(none)")
	keytest.Press(m, "enter")
	if got := s.find("t1"); got["status"] != nil || r.TopModal() != "" {
		t.Fatalf("none: %v modal %q", got, r.TopModal())
	}
}

// The form stays where the field takes no choice, where the setting is
// off, or where edit.field-only is off; a request's chooser argument
// overrides the settings either way.
func TestEditFieldChooserOptOutAndOverride(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		settings map[string]any
		args     Arguments
		chooser  bool
	}{
		{"text field keeps its form", nil, Arguments{"field": "title"}, false},
		{"text field ignores chooser true", nil, Arguments{"field": "title", "chooser": true}, false},
		{"setting off", map[string]any{EditFieldChooserSetting: false}, Arguments{"field": "status"}, false},
		{"field-only off", map[string]any{EditFieldOnlySetting: false}, Arguments{"field": "status"}, false},
		{"row says form", nil, Arguments{"field": "status", "chooser": false}, false},
		{"row says chooser over the setting", map[string]any{EditFieldChooserSetting: false}, Arguments{"field": "status", "chooser": true}, true},
		{"row says chooser over field-only off", map[string]any{EditFieldOnlySetting: false}, Arguments{"field": "status", "chooser": true}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r, _ := editFixture(t)
			for name, v := range c.settings {
				if _, err := r.Settings().SetSession(name, v); err != nil {
					t.Fatal(err)
				}
			}
			mustRun(t, r, Request{Noun: "parcel", Verb: "edit", Args: c.args})
			chooser, form := r.Chooser(EditChooserKind) != nil, r.RecordEditor(editFormID) != nil
			if chooser != c.chooser || form == c.chooser {
				t.Fatalf("chooser %t form %t", chooser, form)
			}
		})
	}
}

// A binding row with chooser false keeps the one-field form for that key
// while other rows open the chooser: the override lives in bindings.json.
func TestEditFieldChooserBindingOverride(t *testing.T) {
	t.Parallel()
	r, _ := editFixture(t)
	m := NewModel(r)
	for _, b := range []Binding{
		{Noun: "parcel", Mode: "normal", Keys: "m", Request: Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "status"}}},
		{Noun: "parcel", Mode: "normal", Keys: "M", Request: Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "status", "chooser": false}}},
	} {
		if err := r.Bind(b); err != nil {
			t.Fatal(err)
		}
	}
	keytest.Press(m, "m")
	if r.Chooser(EditChooserKind) == nil {
		t.Fatalf("m: modal %q", r.TopModal())
	}
	keytest.Press(m, "esc")
	keytest.Press(m, "q")
	keytest.Press(m, "M")
	if r.RecordEditor(editFormID) == nil || r.Chooser(EditChooserKind) != nil {
		t.Fatalf("M: modal %q", r.TopModal())
	}
}

// Over a visual selection the pick goes to every selected row, one undo
// step, as the form's writes do.
func TestEditFieldChooserWritesTheSelection(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	s := &derivedStore{key: "id", things: []Thing{{"id": "t1", "status": "todo"}, {"id": "t2", "status": "todo"}}}
	st := s.store()
	st.Undo = true
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "status", Parameter: Parameter{Enum: []string{"todo", "done"}}}).Store(st)
	if _, err := r.AddList("rows", []ListItem{{ID: "t1", Noun: "parcel", Data: Thing{"id": "t1"}}, {ID: "t2", Noun: "parcel", Data: Thing{"id": "t2"}}}); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.SetFocus("t1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Bind(Binding{Noun: "parcel", Mode: "normal", Keys: "m", Request: Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "status"}}}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"V", "j", "m"} {
		keytest.Press(m, key)
	}
	if r.Chooser(EditChooserKind) == nil {
		t.Fatalf("chooser over a selection: modal %q mode %q", r.TopModal(), r.Mode.Name)
	}
	keytest.Type(m, "done")
	keytest.Press(m, "enter")
	if s.find("t1")["status"] != "done" || s.find("t2")["status"] != "done" {
		t.Fatalf("selection: %v %v", s.find("t1"), s.find("t2"))
	}
	mustRun(t, r, Request{Noun: "ochazuke.history-entry", Verb: "undo"})
	if s.find("t1")["status"] != "todo" || s.find("t2")["status"] != "todo" {
		t.Fatalf("one undo: %v %v", s.find("t1"), s.find("t2"))
	}
}

// A row that refuses the pick does not stop the others, the focused one
// as any: the rest are written, one notice counts the done and the
// failed, and history-entry.undo takes back what applied in one step. The
// chooser opens on the value the focused row holds.
func TestEditFieldChooserFocusedRowRefuses(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	s := &derivedStore{key: "id", things: []Thing{{"id": "t1", "status": "todo"}, {"id": "t2", "status": "todo"}}}
	st := s.store()
	st.Undo = true
	patch := st.Patch
	st.Patch = func(c Ctx, id string, changes Thing) (Thing, error) {
		if id == "t1" && changes["status"] == "done" {
			return nil, fmt.Errorf("parcel %s is locked", id)
		}
		return patch(c, id, changes)
	}
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "status", Parameter: Parameter{Enum: []string{"todo", "done"}}}).Store(st)
	if _, err := r.AddList("rows", []ListItem{{ID: "t1", Noun: "parcel", Data: Thing{"id": "t1"}}, {ID: "t2", Noun: "parcel", Data: Thing{"id": "t2"}}}); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.SetFocus("t1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Bind(Binding{Noun: "parcel", Mode: "normal", Keys: "m", Request: Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "status"}}}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"V", "j", "m"} {
		keytest.Press(m, key)
	}
	c := r.Chooser(EditChooserKind)
	if c == nil {
		t.Fatalf("chooser: modal %q", r.TopModal())
	}
	if row, ok := c.Highlighted(); !ok || row.ID != "todo" {
		t.Fatalf("opens on %+v, want the value t1 holds", row)
	}
	notices := r.UI().capture()
	keytest.Type(m, "done")
	keytest.Press(m, "enter")
	if s.find("t1")["status"] != "todo" || s.find("t2")["status"] != "done" || r.TopModal() != "" {
		t.Fatalf("one refused: %v %v modal %q", s.find("t1"), s.find("t2"), r.TopModal())
	}
	if got := notices(); len(got) == 0 || !strings.Contains(got[len(got)-1].Text, "1 done, 1 failed (t1: ") || !strings.Contains(got[len(got)-1].Text, "parcel t1 is locked") {
		t.Fatalf("notice %+v", got)
	}
	mustRun(t, r, Request{Noun: "ochazuke.history-entry", Verb: "undo"})
	if s.find("t2")["status"] != "todo" {
		t.Fatalf("one undo: %v", s.find("t2"))
	}
}

// An application's own edit adopts chooser with field: edit {field} on a
// choice prop opens the chooser there too, and chooser false the form.
func TestEditFieldChooserOnAppEdit(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Define("parcel").Verb("edit", func(Ctx) Result { return Done("own edit") }, "The app's own edit")
	s := &derivedStore{key: "id", things: []Thing{{"id": "t1", "status": "todo"}}}
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "status", Parameter: Parameter{Enum: []string{"todo", "done"}}}).Store(s.store())
	r.Instance("row", Kind("parcel")).Data = Thing{"id": "t1"}
	if err := r.SetFocus("row"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "status"}})
	if r.Chooser(EditChooserKind) == nil {
		t.Fatalf("app edit: modal %q", r.TopModal())
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "status", "chooser": false}})
	if r.RecordEditor(editFormID) == nil {
		t.Fatalf("app edit chooser false: modal %q", r.TopModal())
	}
}
