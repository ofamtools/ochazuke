package ochazuke

import (
	"slices"
	"testing"
)

// choices.user-values: a status the task noun does not list is written
// either way; register also adds it to the prop's choices, keep leaves the
// list as declared.
func TestUserValuesSetting(t *testing.T) {
	for _, tc := range []struct {
		value  string
		listed bool
	}{{"register", true}, {"keep", false}} {
		r := NewRuntime()
		table := NewMemDocTable(r, "task")
		r.Define("task").Props(taskTestProps()...).Store(DocStore(table, "task"))
		if err := r.Settings().Set(UserValuesSetting, tc.value); err != nil {
			t.Fatal(err)
		}
		got, err := r.Execute(Request{Noun: "task", Verb: "create", Args: Arguments{"title": "x", "status": "blocked"}})
		if thing, _ := got.(map[string]any); err != nil || thing["status"] != "blocked" {
			t.Fatalf("%s: %#v %v", tc.value, got, err)
		}
		if got := slices.Contains(r.Noun("task").prop("status").Enum, "blocked"); got != tc.listed {
			t.Fatalf("%s: listed %v, want %v", tc.value, got, tc.listed)
		}
	}
}

// A setting value outside its enum is kept as written and the typed getter
// answers the default; a register setting grows its enum; a name nothing
// declares is kept as the user's own row.
func TestSettingsAcceptValues(t *testing.T) {
	r := NewRuntime()
	if err := r.DeclareSetting(
		Setting{Name: "look", Type: SettingString, Default: "plain", Enum: []string{"plain", "fancy"}},
		Setting{Name: "tag", Type: SettingString, Default: "a", Enum: []string{"a"}, Accept: AcceptRegister},
	); err != nil {
		t.Fatal(err)
	}
	s := r.Settings()
	if err := s.Set("look", "loud"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Value("look"); v != "loud" || s.String("look") != "plain" {
		t.Fatalf("look kept %v, reads %q", v, s.String("look"))
	}
	if err := s.Set("tag", "b"); err != nil || !slices.Contains(s.find("tag").Enum, "b") || s.String("tag") != "b" {
		t.Fatalf("tag: %v %v %q", err, s.find("tag").Enum, s.String("tag"))
	}
	got, err := r.Execute(Request{Noun: "ochazuke.setting", Verb: "set", Args: Arguments{"name": "nosuch", "value": "1"}})
	if kept, ok := got.(SettingKept); err != nil || !ok || !kept.Kept || kept.Value != float64(1) {
		t.Fatalf("set nosuch: %#v %v", got, err)
	}
}
