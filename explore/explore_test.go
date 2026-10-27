package explore_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/explore"
)

// buggy is an application with a bug per check: b panics, f fails, s
// enters a mode nothing leaves (static and dynamic traps), and selecting
// the last row breaks the application's own check (lastRow).
func buggy() (*ochazuke.Model, error) {
	r := ochazuke.NewRuntime()
	m := ochazuke.NewModel(r)
	m.Appearance.Theme = "mono"
	if err := r.DefineMode("stuck", false); err != nil {
		return nil, err
	}
	r.Define("item").
		Verb("boom", func(ochazuke.Ctx) ochazuke.Result { panic("boom") }).
		Verb("fail", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Failed(errors.New("broken")) }).
		Mode("normal").
		Bind("b", ochazuke.Do("item.boom")).
		Bind("f", ochazuke.Do("item.fail")).
		Bind("s", ochazuke.Do("mode.set", ochazuke.Arguments{"mode": "stuck"}))
	var list *ochazuke.List
	if _, err := r.AddPage("p", "Page", func(r *ochazuke.Runtime, w, h int) ochazuke.Block {
		return ochazuke.Text(list.View(w, h))
	}); err != nil {
		return nil, err
	}
	l, err := r.AddList("l", []ochazuke.ListItem{{ID: "a", Label: "a", Noun: "item"}, {ID: "b", Label: "b", Noun: "item"}, {ID: "c", Label: "c", Noun: "item"}}, ochazuke.Parent("p"))
	if err != nil {
		return nil, err
	}
	list = l
	if err := r.OpenPage("p"); err != nil {
		return nil, err
	}
	return m, r.SetFocus("a")
}

// lastRow is the application's own invariant, broken on purpose: the last
// row must never be selected.
var lastRow = explore.Check{Name: "not-last", Fn: func(m *ochazuke.Model) error {
	if sel, ok := m.Runtime.List("l").Selected(); ok && sel.ID == "c" {
		return errors.New("the last row is selected")
	}
	return nil
}}

func subject() explore.Subject {
	return explore.Subject{Name: "buggy", New: buggy, Checks: []explore.Check{lastRow}}
}

func TestRunFindsEachCheck(t *testing.T) {
	rep := explore.Run(subject(), explore.Budget{Depth: 3, Steps: 1500, TrapSteps: 60})
	if len(rep.Errors) > 0 {
		t.Fatalf("explorer errors: %v", rep.Errors)
	}
	by := rep.ByCheck()
	for _, check := range []string{explore.CheckPanic, explore.CheckErrors, explore.CheckStaticTrap, explore.CheckTrap, "not-last"} {
		if len(by[check]) == 0 {
			t.Errorf("no %s finding:\n%s", check, rep.Text(0))
		}
	}
	for _, f := range by["not-last"] {
		if !slices.Equal(f.Path, []string{"G"}) {
			t.Errorf("not-last finding path %v, want the shortest, G", f.Path)
		}
	}
	for _, f := range by[explore.CheckPanic] {
		if !slices.Equal(f.Path, []string{"b"}) {
			t.Errorf("panic path %v, want b", f.Path)
		}
		if err := explore.Reproduce(subject(), explore.CheckPanic, f.Path); err == nil {
			t.Errorf("Reproduce(%v) found no panic", f.Path)
		}
	}
	if err := explore.Reproduce(subject(), explore.CheckPanic, []string{"j"}); err != nil {
		t.Errorf("Reproduce(j) = %v, want nil", err)
	}
	if got := by[explore.CheckTrap]; len(got) > 0 && !strings.Contains(got[0].Summary, "stuck") {
		t.Errorf("trap finding %q does not name the stuck mode", got[0].Summary)
	}
}

// TestShrink: a finding found late is shrunk to the keys that matter.
func TestShrink(t *testing.T) {
	seed := []string{"k", "k", "j", "k", "j", "j"}
	s := subject()
	s.Seeds = [][]string{seed}
	rep := explore.Run(s, explore.Budget{Depth: 1, Steps: 50})
	found := false
	for _, f := range rep.ByCheck()["not-last"] {
		found = true
		if !f.Shrunk || f.Found != len(seed) || !slices.Equal(f.Path, []string{"j", "j"}) {
			t.Errorf("not-last finding: path %v found %d shrunk %v, want j j shrunk from %d", f.Path, f.Found, f.Shrunk, len(seed))
		}
	}
	if !found {
		t.Errorf("no not-last finding:\n%s", rep.Text(0))
	}
}

func TestRunIsDeterministic(t *testing.T) {
	b := explore.Budget{Depth: 2, Steps: 300, CompareFull: true, TrapSteps: 30}
	a, c := explore.Run(subject(), b), explore.Run(subject(), b)
	if string(a.JSON()) != string(c.JSON()) {
		t.Fatalf("two runs differ:\n%s\n---\n%s", a.Text(0), c.Text(0))
	}
	if a.States["full"] < a.States["abstract"] {
		t.Errorf("full keying merged more than abstract: %v", a.States)
	}
}

func TestGoTestSnippet(t *testing.T) {
	f := &explore.Finding{Check: "panic", Summary: "update: boom", Path: []string{"j", "b"}}
	got := f.GoTest("subject()")
	for _, want := range []string{"func TestExplorePanicUpdateBoom(t *testing.T)", `explore.Reproduce(subject(), "panic", []string{"j", "b"})`} {
		if !strings.Contains(got, want) {
			t.Errorf("snippet lacks %q:\n%s", want, got)
		}
	}
}
