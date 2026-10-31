package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

func replacementPress(m *Model, keys ...string) {
	for _, key := range keys {
		keytest.Press(m, key)
	}
}

func replacementRow(key, verb string) Binding {
	return Binding{Noun: "app", Mode: "normal", Keys: key, Request: Request{Noun: "test", Verb: verb}}
}

func TestSetDefaultBindingsReappliesUserLayer(t *testing.T) {
	t.Parallel()
	r, m, calls, dir := configFixture(t)
	if err := r.LoadDefaultBindings("dynamic", []byte(`[{"noun":"app","mode":"normal","keys":"s","request":{"noun":"test","verb":"alpha"}},{"noun":"app","mode":"normal","keys":"t","request":{"noun":"test","verb":"alpha"}}]`)); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, dir, BindingsFile, `[
		{"noun":"app","mode":"normal","keys":"s","request":{"noun":"test","verb":"gamma"}},
		{"noun":"app","mode":"normal","keys":"t","request":null}
	]`)
	if problems := r.EnsureUserBindings(); len(problems) != 0 {
		t.Fatal(problems)
	}
	replacement := []Binding{replacementRow("s", "beta"), replacementRow("t", "beta"), replacementRow("z z", "alpha")}
	if err := r.SetDefaultBindings("dynamic", replacement); err != nil {
		t.Fatal(err)
	}
	replacementPress(m, "s", "t")
	if !slices.Equal(*calls, []string{"gamma"}) {
		t.Fatalf("user override/unbind lost: %v", *calls)
	}
	if keys := helpKeys(r); keys["s"] != "test.gamma" || keys["t"] != "" || keys["z z"] != "test.alpha" {
		t.Fatalf("help was not refreshed: %v", keys)
	}
	var dynamic []DefaultBinding
	for _, b := range r.DefaultBindings() {
		if b.Source == "dynamic" {
			dynamic = append(dynamic, b)
		}
	}
	if len(dynamic) != 3 || dynamic[0].Request.Verb != "beta" || dynamic[1].Request.Verb != "beta" {
		t.Fatalf("default layer contains stale rows: %+v", dynamic)
	}
	before := r.DefaultBindings()
	undo := slices.Clone(r.Config().applied)
	for range 3 {
		if err := r.SetDefaultBindings("dynamic", replacement); err != nil {
			t.Fatal(err)
		}
		if p := r.ReloadUserBindings(); len(p) != 0 {
			t.Fatal(p)
		}
		if !reflect.DeepEqual(before, r.DefaultBindings()) || !reflect.DeepEqual(undo, r.Config().applied) {
			t.Fatal("replacement/reload changed defaults or accumulated undo entries")
		}
	}
	writeConfig(t, dir, BindingsFile, `[]`)
	r.ReloadUserBindings()
	replacementPress(m, "s", "t", "z", "z")
	if !slices.Equal((*calls)[1:], []string{"beta", "beta", "alpha"}) {
		t.Fatalf("undo restored old defaults: %v", *calls)
	}
}

func TestSetDefaultBindingsRemovedUserRowsAreKeptAndReported(t *testing.T) {
	t.Parallel()
	r, m, calls, dir := configFixture(t)
	if err := r.SetDefaultBindings("dynamic", []Binding{replacementRow("s", "alpha"), replacementRow("t", "alpha"), replacementRow("z z", "alpha")}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetDefaultBindings("other", []Binding{replacementRow("o", "beta")}); err != nil {
		t.Fatal(err)
	}
	const user = `[
		{"noun":"app","mode":"normal","keys":"s","request":{"noun":"test","verb":"gamma"}},
		{"noun":"app","mode":"normal","keys":"t","request":null}
	]`
	writeConfig(t, dir, BindingsFile, user)
	r.EnsureUserBindings()
	if err := r.SetDefaultBindings("dynamic", nil); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		p := r.Config().BindingProblems()
		if len(p) != 2 || !strings.Contains(p[0], "default from dynamic was removed; user row kept") || !strings.Contains(p[1], "default from dynamic was removed; user row kept") || len(m.Notices) != 2 {
			t.Fatalf("removed defaults not reported consistently: %v; %v", p, m.Notices)
		}
		if len(r.Config().applied) != 2 || len(r.Config().applied[0].replaced) != 0 || len(r.Config().applied[1].replaced) != 0 {
			t.Fatalf("undo log retained removed defaults: %+v", r.Config().applied)
		}
		if p := r.CheckUserBindings(); len(p) != 2 {
			t.Fatalf("check lost removed-default warnings: %v", p)
		}
		r.ReloadUserBindings()
	}
	for _, row := range r.DefaultBindings() {
		if row.Source == "dynamic" || row.Noun == "app" && (row.Keys == "z z" || row.Keys == "z esc") {
			t.Fatalf("removed row or generated cancel survived: %+v", row)
		}
	}
	replacementPress(m, "s", "t", "o")
	if !slices.Equal(*calls, []string{"gamma", "beta"}) {
		t.Fatalf("surviving override or other source broken: %v", *calls)
	}
	if err := r.SetDefaultBindings("dynamic", []Binding{replacementRow("s", "beta"), replacementRow("t", "beta")}); err != nil {
		t.Fatal(err)
	}
	if p := r.Config().BindingProblems(); len(p) != 0 || len(m.Notices) != 0 {
		t.Fatalf("returned defaults retained notices: %v; %v", p, m.Notices)
	}
	replacementPress(m, "s", "t")
	if !slices.Equal((*calls)[2:], []string{"gamma"}) {
		t.Fatalf("returning default lost user override/unbind: %v", *calls)
	}
	writeConfig(t, dir, BindingsFile, `[]`)
	r.ReloadUserBindings()
	replacementPress(m, "s", "t")
	if !slices.Equal((*calls)[3:], []string{"beta", "beta"}) {
		t.Fatalf("returning defaults missing from undo log: %v", *calls)
	}
}

func TestSetDefaultBindingsBadBatchIsAtomic(t *testing.T) {
	t.Parallel()
	r, m, calls, dir := configFixture(t)
	if err := r.SetDefaultBindings("dynamic", []Binding{replacementRow("s s", "alpha")}); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, dir, BindingsFile, `[{"noun":"app","mode":"normal","keys":"t","request":{"noun":"test","verb":"gamma"}}]`)
	r.EnsureUserBindings()
	replacementPress(m, "s")
	pending := r.Pending
	if pending == "" {
		t.Fatal("fixture did not enter a sequence")
	}
	before := r.DefaultBindings()
	undo := slices.Clone(r.Config().applied)
	for _, source := range []string{"", " ", SourceGo, SourceGenerated, "dynamic"} {
		rows := []Binding{replacementRow("x", "beta")}
		if source == "dynamic" {
			rows = append(rows, replacementRow("y", "missing"))
		}
		if err := r.SetDefaultBindings(source, rows); err == nil {
			t.Fatalf("bad source/batch %q accepted", source)
		}
		if !reflect.DeepEqual(before, r.DefaultBindings()) || !reflect.DeepEqual(undo, r.Config().applied) || r.Pending != pending {
			t.Fatal("bad replacement changed tables, pending keys or the user undo log")
		}
	}
	replacementPress(m, "s", "t")
	if !slices.Equal(*calls, []string{"alpha", "gamma"}) {
		t.Fatalf("bad batch changed dispatch: %v", *calls)
	}
	replacementPress(m, "s")
	if err := r.SetDefaultBindings("dynamic", []Binding{replacementRow("x", "beta")}); err != nil {
		t.Fatal(err)
	}
	if r.Pending != "" {
		t.Fatal("removed prefix stayed pending")
	}
}

func TestSetDefaultBindingsInstanceRowsAndAlternatives(t *testing.T) {
	t.Parallel()
	r, _, _, dir := configFixture(t)
	r.Instance("panel")
	a := replacementRow("s", "alpha")
	a.Noun, a.Instance = "", "panel"
	b := a
	b.Request.Verb = "beta"
	if err := r.SetDefaultBindings("dynamic", []Binding{a, b}); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, dir, BindingsFile, `[{"instance":"panel","mode":"normal","keys":"s","request":null}]`)
	r.EnsureUserBindings()
	if err := r.SetDefaultBindings("dynamic", []Binding{b, a}); err != nil {
		t.Fatal(err)
	}
	if len(r.Instances["panel"].Bindings) != 0 {
		t.Fatal("instance unbind lost")
	}
	writeConfig(t, dir, BindingsFile, `[]`)
	r.ReloadUserBindings()
	got := r.Instances["panel"].Bindings
	if len(got) != 2 || got[0].Request.Verb != "beta" || got[1].Request.Verb != "alpha" {
		t.Fatalf("instance alternatives or undo order lost: %+v", got)
	}
}

func TestSetDefaultBindingsRefreshesOpenHelp(t *testing.T) {
	t.Parallel()
	for _, view := range []string{HelpHere, HelpPick, HelpTable} {
		t.Run(view, func(t *testing.T) {
			t.Parallel()
			r, _, _, _ := configFixture(t)
			if err := r.SetDefaultBindings("dynamic", []Binding{replacementRow("s", "alpha")}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "help"}); err != nil {
				t.Fatal(err)
			}
			h := r.helpOpen()
			h.view = view
			if err := h.show(); err != nil {
				t.Fatal(err)
			}
			// The update can arrive while a chooser is above the help.
			if _, err := r.OpenChooser(ChooserSpec{Kind: "test-help-chooser", Title: "Choose", Rows: []ChooserRow{{ID: "one", Label: "One"}}}); err != nil {
				t.Fatal(err)
			}
			focus, mode := r.Focus, r.Mode
			if err := r.SetDefaultBindings("dynamic", []Binding{replacementRow("t", "gamma")}); err != nil {
				t.Fatal(err)
			}
			if r.Focus != focus || !reflect.DeepEqual(r.Mode, mode) {
				t.Fatal("help refresh moved focus or changed the chooser's mode")
			}
			var found bool
			for _, item := range r.List(helpListID).Items() {
				if view == HelpTable {
					key := item.Data.(HelpKey)
					if key.Key == "s" {
						t.Fatal("open help table kept the removed key")
					}
					found = found || key.Key == "t"
				} else {
					row := item.Data.(HelpRow)
					if row.Key == "s" {
						t.Fatal("open help kept the removed key")
					}
					found = found || row.Key == "t" && row.Operation == "test.gamma"
				}
			}
			if !found {
				t.Fatal("open help did not show the new binding")
			}
			for range 2 {
				if err := r.Pop(); err != nil {
					t.Fatal(err)
				}
			}
			if hints := BindingHints(r, "app"); !strings.Contains(hints, "t test.gamma") || strings.Contains(hints, "s test.alpha") {
				t.Fatalf("hints did not follow the replacement: %s", hints)
			}
		})
	}
}
