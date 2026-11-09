package ochazuke

import (
	"reflect"
	"testing"
)

// referrerRuntime has a noun "project" and three nouns that name it, one
// as an owner, one as a plain reference, one read-only, plus a noun that
// names another noun and a noun that names the project twice.
func referrerRuntime(t *testing.T) *Runtime {
	t.Helper()
	r := NewRuntime()
	r.Define("project").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel})
	r.Define("area").Props(PropSpec{Name: "id", Key: true})
	r.Define("task").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "project", Role: RoleOwner, Parameter: Parameter{Noun: "project"}})
	r.Define("schedule").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "project", Parameter: Parameter{Noun: "project"}}, PropSpec{Name: "area", Parameter: Parameter{Noun: "area"}})
	r.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "origin", ReadOnly: true, Parameter: Parameter{Noun: "project"}}, PropSpec{Name: "project", Role: RoleOwner, Parameter: Parameter{Noun: "project"}})
	return r
}

func TestReferrersListNounsInRegistrationOrder(t *testing.T) {
	t.Parallel()
	r := referrerRuntime(t)
	var got []string
	for _, f := range r.Referrers("project") {
		got = append(got, f.ID())
	}
	want := []string{"task.project", "schedule.project", "note.origin", "note.project"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("referrers %v, want %v", got, want)
	}
	f := r.Referrers("project")
	if f[0].Role != RoleOwner || f[0].Label != "Tasks" || f[1].Role != "" || f[1].Label != "Schedules (project)" || !f[2].ReadOnly {
		t.Fatalf("referrers %+v", f)
	}
	if got := r.Referrers("area"); len(got) != 1 || got[0].ID() != "schedule.area" {
		t.Fatalf("area referrers %+v", got)
	}
	if got := r.Referrers("nothing"); len(got) != 0 {
		t.Fatalf("a noun nothing names: %+v", got)
	}
	// A later noun that names the project is listed last, with no other change.
	r.Define("link").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "project", Parameter: Parameter{Noun: "project"}})
	if f := r.Referrers("project"); f[len(f)-1].ID() != "link.project" {
		t.Fatalf("a later noun: %+v", f)
	}
}

func TestReferrerFactNounLists(t *testing.T) {
	t.Parallel()
	r := referrerRuntime(t)
	NewModel(r)
	got, err := r.Execute(Request{Noun: "ochazuke.referrer", Verb: "list", Args: Arguments{"noun": "project"}})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range AnswerRows(got).([]map[string]any) {
		ids = append(ids, row["id"].(string))
	}
	if want := []string{"task.project", "schedule.project", "note.origin", "note.project"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("referrer list %v, want %v", ids, want)
	}
	row, err := r.Execute(Request{Noun: "ochazuke.referrer", Verb: "get", Args: Arguments{"id": "task.project"}})
	if err != nil || row == nil {
		t.Fatalf("get: %v %v", row, err)
	}
}
