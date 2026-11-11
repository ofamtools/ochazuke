package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

func TestFocusedActionsListTheFocusedNounsVerbs(t *testing.T) {
	t.Parallel()
	r, f := pathVerbFixture(t)
	r.Instance("n1", Kind("note"))
	noun, choices := r.FocusedActions("n1")
	if noun != "note" {
		t.Fatalf("noun %q", noun)
	}
	var ids []string
	for _, c := range choices {
		ids = append(ids, c.ID)
	}
	for _, want := range []string{"note.reveal", "note.yank-path", "note.open", "note.open-with", "note.get"} {
		if !slices.Contains(ids, want) {
			t.Errorf("%s missing from %v", want, ids)
		}
	}
	if !slices.IsSortedFunc(choices, func(a, b Choice) int { return strings.Compare(a.Label, b.Label) }) {
		t.Errorf("not by name: %v", ids)
	}
	r.SetFocus("n1")
	ch, err := r.OpenFocusActions()
	if err != nil || ch == nil {
		t.Fatalf("open %v", err)
	}
	if len(f.runs) != 0 {
		t.Fatalf("listing launched %v", f.runs)
	}
}

func TestFocusedActionsPickerOptOut(t *testing.T) {
	t.Parallel()
	r, _ := pathVerbFixture(t)
	r.Instance("n1", Kind("note"))
	r.SetFocus("n1")
	if err := r.Settings().Set(FocusedPickerSetting, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenFocusActions(); err == nil {
		t.Fatal("actions.focused-picker=false must refuse")
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.focus", Verb: "act"}); err == nil {
		t.Fatal("the verb refuses too")
	}
}

func TestFocusedActionsURLVerbs(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Config().Disable()
	r.Define("bookmark").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "url", Parameter: Parameter{Type: PropURL}},
	).Store(Store{Get: func(Ctx, string) (Thing, error) {
		return Thing{"id": "b1", "url": "https://example.test/a"}, nil
	}})
	r.Instance("b1", Kind("bookmark")).Data = Thing{"id": "b1"}
	_, choices := r.FocusedActions("b1")
	var values []string
	for _, c := range choices {
		if slices.Contains([]string{"open", "open-with", "reveal", "yank-path"}, c.Label) {
			values = append(values, c.Label)
		}
	}
	if !slices.Equal(values, []string{"open", "yank-path"}) {
		t.Fatalf("URL actions %v, want open and yank-path", values)
	}
}
