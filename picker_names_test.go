package ochazuke

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// pickers PK2: one term. The old chooser noun, its renamed verb, the path
// finder's old verbs, the chooser.* settings and the Go names all run for
// one release, each old name with one notice.

func pickerNotices(sink *RecordingSink) []string {
	var out []string
	for _, m := range sink.Messages() {
		if m.Kind == UINotice && strings.Contains(m.Text, "old name") {
			out = append(out, m.Text)
		}
	}
	return out
}

func TestOldPickerNamesResolve(t *testing.T) {
	r := NewRuntime()
	for name, want := range map[string]string{
		"ochazuke.chooser.pick":            "ochazuke.picker.pick",
		"ochazuke.chooser.move":            "ochazuke.picker.move",
		"ochazuke.chooser.cancel":          "ochazuke.picker.cancel",
		"ochazuke.chooser.up":              "ochazuke.picker.up",
		"ochazuke.chooser.next-source":     "ochazuke.picker.next-source",
		"ochazuke.chooser.previous-source": "ochazuke.picker.previous-source",
		"ochazuke.chooser.clear-props":     "ochazuke.picker.clear-props",
		"ochazuke.chooser.normal":          "ochazuke.picker.normal",
		// chooser.open browsed into a row: it is picker.into, never the
		// picker noun's open (which opens a picker by name).
		"ochazuke.chooser.open":   "ochazuke.picker.into",
		"chooser.open":            "ochazuke.picker.into",
		"chooser.pick":            "ochazuke.picker.pick",
		"ochazuke.picker.choose":  "ochazuke.picker.pick",
		"ochazuke.picker.close":   "ochazuke.picker.cancel",
		"ochazuke.picker.descend": "ochazuke.picker.into",
		"ochazuke.picker.parent":  "ochazuke.picker.up",
		"ochazuke.picker.open":    "ochazuke.picker.open",
	} {
		op, ok := r.Operation(name)
		if !ok || op.Key() != want {
			t.Errorf("%s resolves to %q (%v), want %s", name, op.Key(), ok, want)
		}
	}
	q := r.canonicalRequest(Request{Noun: "ochazuke.chooser", Verb: "open"})
	if q.Key() != "ochazuke.picker.into" || q.Via != "ochazuke.chooser.open" {
		t.Errorf("request ochazuke.chooser open: %+v", q)
	}
	if got := r.CanonicalNoun("ochazuke.chooser"); got != PickerNoun {
		t.Errorf("ochazuke.chooser is %q", got)
	}
	if _, ok := r.DeprecatedNounAlias("chooser"); !ok {
		t.Error("chooser is not a deprecated noun alias")
	}
	if r.Nouns["ochazuke.chooser"] != nil {
		t.Error("the old noun is still registered")
	}
	if got := r.DisplayNoun(PickerNoun); strings.Contains(got, "chooser") {
		t.Errorf("the picker noun is drawn as %q", got)
	}
}

func TestOldChooserNounRunsWithOneNotice(t *testing.T) {
	r, _, _, calls := chooserTest(t, PickerSpec{Rows: []PickerRow{{ID: "a", Label: "alpha"}, {ID: "b", Label: "beta"}}})
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	if _, err := r.Execute(Request{Noun: "ochazuke.chooser", Verb: "move", Args: Arguments{"delta": 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.chooser", Verb: "move", Args: Arguments{"delta": 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.chooser", Verb: "pick"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*calls, []string{"choose b"}) {
		t.Fatalf("calls %v", *calls)
	}
	want := []string{"ochazuke.chooser is an old name of picker (renamed in v0.95.0; removed after v0.96.0)"}
	if got := pickerNotices(sink); !slices.Equal(got, want) {
		t.Fatalf("notices %q, want %q", got, want)
	}
}

// The renamed verb posts its own notice and not the noun's too.
func TestChooserOpenIsPickerIntoWithOneNotice(t *testing.T) {
	r, _, _, _ := chooserTest(t, PickerSpec{Rows: []PickerRow{{ID: "a", Label: "alpha"}}})
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	// No row opens: into is unavailable, and says so as into would.
	_, err := r.Execute(Request{Noun: "ochazuke.chooser", Verb: "open"})
	_, want := r.Execute(Request{Noun: "ochazuke.picker", Verb: "into"})
	if err == nil || want == nil || err.Error() != want.Error() {
		t.Fatalf("chooser.open: %v, picker.into: %v", err, want)
	}
	for _, n := range pickerNotices(sink) {
		if strings.HasPrefix(n, "ochazuke.chooser is an old name") {
			t.Fatalf("the noun's notice beside the verb's: %q", pickerNotices(sink))
		}
	}
}

func TestPathFinderOldVerbsAreAliases(t *testing.T) {
	r := NewRuntime()
	for old, verb := range pickerVerbAliases {
		a, ok := r.ActionAlias(PickerNoun + "." + old)
		if !ok || a.Action != PickerNoun+"."+verb || !a.Deprecated() || a.Since != "v0.95.0" || a.RemoveAfter != "v0.96.0" {
			t.Errorf("%s: %+v %v", old, a, ok)
		}
	}
}

func TestOldChooserSettingNamesRead(t *testing.T) {
	r := NewRuntime()
	dir := t.TempDir()
	r.Config().Dir = dir
	writeConfig(t, dir, SettingsFile, `{"chooser.match": "regex", "chooser.enter": "pick", "edit.field-chooser": false}`)
	s := r.Settings()
	if got := r.ChooserMatch(); got != "regex" {
		t.Errorf("picker.match from chooser.match: %q", got)
	}
	if got := s.String(ChooserEnterSetting); got != "pick" {
		t.Errorf("picker.enter from chooser.enter: %q", got)
	}
	if s.Bool(EditFieldChooserSetting) {
		t.Error("edit.field-picker from edit.field-chooser: still on")
	}
	v, err := s.Get("chooser.remember-source")
	if err != nil || v.Name != ChooserRememberSourceSetting {
		t.Errorf("get chooser.remember-source: %+v %v", v, err)
	}
	for _, name := range []string{"picker.match", "picker.enter", "picker.up-from-none", "picker.prop-filters", "picker.remember-source", "picker.claim-source", "picker.stable-height", "picker.hidden-count", "edit.field-picker"} {
		one, ok := s.Declared()[slices.IndexFunc(s.Declared(), func(d Setting) bool { return d.Name == name })], true
		if !ok || len(one.Deprecated) != 1 || !strings.Contains(one.Deprecated[0], "chooser") {
			t.Errorf("%s: deprecated %v", name, one.Deprecated)
		}
	}
}

// A user's bindings.json row naming the old noun, in its context and its
// action, still answers, and bindings check says what to rename.
func TestUserRowNamingTheOldChooserNoun(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	dir := t.TempDir()
	r.Config().Dir = dir
	rows := `{"bindings": [{"context": "ochazuke.chooser", "bindings": [{"key": "ctrl+x", "action": "ochazuke.chooser.pick"}, {"key": "ctrl+y", "action": "ochazuke.chooser.open"}]}]}`
	if err := os.WriteFile(filepath.Join(dir, BindingsFile), []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	if problems := r.EnsureUserBindings(); len(problems) != 0 {
		t.Fatalf("load: %q", problems)
	}
	check := strings.Join(r.CheckUserBindings(), "\n")
	if !strings.Contains(check, "ochazuke.chooser resolves to ochazuke.picker") {
		t.Errorf("bindings check:\n%s", check)
	}
	var calls []string
	if _, err := r.OpenPicker(PickerSpec{Kind: "k", Rows: []PickerRow{{ID: "a", Label: "alpha"}}, Choose: func(row PickerRow) Result {
		calls = append(calls, row.ID)
		return Done(nil)
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(MacroKey{Key: "ctrl+x"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(calls, []string{"a"}) {
		t.Fatalf("ctrl+x (ochazuke.chooser.pick) picked %v", calls)
	}
}

// The Go names: the old ones are aliases of the new for one release.
func TestPickerGoNamesAlias(t *testing.T) {
	r := NewRuntime()
	var spec ChooserSpec = PickerSpec{Kind: "old", Rows: []ChooserRow{PickerRow{ID: "a"}}, Sources: []ChooserSource{}}
	if _, err := r.OpenChooser(spec); err != nil {
		t.Fatal(err)
	}
	var finder *Picker = NewPicker(r)
	var _ *PathFinder = finder
}
