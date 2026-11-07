package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// editSelectionFixture is three parcels in a list (Alpha todo, Beta done,
// locked todo, whose store refuses every change) with S bound to the
// status form (edit {field: status} without its chooser) and a count of
// the writes tried on locked.
func editSelectionFixture(t *testing.T) (*Runtime, *Model, *derivedStore, *int) {
	t.Helper()
	r := NewRuntime()
	s := &derivedStore{key: "id", things: []Thing{
		{"id": "t1", "title": "Alpha", "status": "todo"},
		{"id": "t2", "title": "Beta", "status": "done"},
		{"id": "t3", "title": "locked", "status": "todo"},
	}}
	st := s.store()
	st.Undo = true
	patch := st.Patch
	tries := 0
	st.Patch = func(c Ctx, id string, changes Thing) (Thing, error) {
		if id == "t3" {
			tries++
			return nil, fmt.Errorf("parcel %s is locked", id)
		}
		return patch(c, id, changes)
	}
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}, PropSpec{Name: "status", Parameter: Parameter{Enum: []string{"todo", "done", "blocked"}}}).Store(st)
	var items []ListItem
	for _, id := range []string{"t1", "t2", "t3"} {
		items = append(items, ListItem{ID: id, Noun: "parcel", Data: Thing{"id": id}})
	}
	if _, err := r.AddList("rows", items); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if err := r.SetFocus("t1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Bind(Binding{Noun: "parcel", Mode: "normal", Keys: "S", Request: Request{Noun: "parcel", Verb: "edit", Args: Arguments{"field": "status", "chooser": false}}}); err != nil {
		t.Fatal(err)
	}
	return r, m, s, &tries
}

// The status form over a selection whose focused row (locked) refuses
// the pick: the others are written and the form's field says it was not
// written and why, as it does over one row (UX R2 N3a).
func TestEditFormSelectionFocusedRefused(t *testing.T) {
	t.Parallel()
	r, m, s, _ := editSelectionFixture(t)
	for _, key := range []string{"V", "j", "j", "S"} {
		keytest.Press(m, key)
	}
	e := r.RecordEditor(editFormID)
	if e == nil || r.Focus != editFormID+"/status" {
		t.Fatalf("form: %v focus %q", e, r.Focus)
	}
	// The choice list opens on todo, the value locked holds: two down is blocked.
	notices := r.UI().capture()
	for _, key := range []string{"enter", "j", "j", "enter"} {
		keytest.Press(m, key)
	}
	if s.find("t1")["status"] != "blocked" || s.find("t2")["status"] != "blocked" || s.find("t3")["status"] != "todo" {
		t.Fatalf("written: %v %v %v", s.find("t1"), s.find("t2"), s.find("t3"))
	}
	if got := e.Problem("status"); !strings.Contains(got, "parcel t3 is locked") {
		t.Fatalf("the focused row's field is marked not written: %q", got)
	}
	// Rows applied, so the save stands: one notice counts the done and
	// the failed, and the form shows no "not saved" line (UX R3 F2).
	if got := notices(); len(got) == 0 || !strings.Contains(got[len(got)-1].Text, "2 done, 1 failed (t3: ") || !strings.Contains(got[len(got)-1].Text, "parcel t3 is locked") {
		t.Fatalf("notice %+v", got)
	}
	if e.WriteError() != "" {
		t.Fatalf("the save is not failed: %q", e.WriteError())
	}
}

// form.undo over a selection puts each row back to its own earlier value
// (as history.undo does), not the focused row's, and never tries a row
// that refused the write; form.redo writes again only the rows the undo
// reached (UX R2 N3b, N3c).
func TestEditFormUndoSelection(t *testing.T) {
	t.Parallel()
	r, m, s, tries := editSelectionFixture(t)
	for _, key := range []string{"V", "j", "j", "S"} {
		keytest.Press(m, key)
	}
	if r.RecordEditor(editFormID) == nil {
		t.Fatalf("form: modal %q", r.TopModal())
	}
	for _, key := range []string{"enter", "j", "j", "enter"} {
		keytest.Press(m, key)
	}
	if s.find("t1")["status"] != "blocked" || s.find("t2")["status"] != "blocked" || *tries != 1 {
		t.Fatalf("written: %v %v tries %d", s.find("t1"), s.find("t2"), *tries)
	}
	keytest.Press(m, "u")
	if s.find("t1")["status"] != "todo" || s.find("t2")["status"] != "done" || s.find("t3")["status"] != "todo" || *tries != 1 {
		t.Fatalf("undo: %v %v %v tries %d", s.find("t1"), s.find("t2"), s.find("t3"), *tries)
	}
	keytest.Press(m, "ctrl+r")
	if s.find("t1")["status"] != "blocked" || s.find("t2")["status"] != "blocked" || *tries != 1 {
		t.Fatalf("redo: %v %v tries %d", s.find("t1"), s.find("t2"), *tries)
	}
	keytest.Press(m, "u")
	if s.find("t1")["status"] != "todo" || s.find("t2")["status"] != "done" || *tries != 1 {
		t.Fatalf("undo again: %v %v tries %d", s.find("t1"), s.find("t2"), *tries)
	}
}
