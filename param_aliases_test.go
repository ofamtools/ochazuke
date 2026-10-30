package ochazuke

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// starredAlias is task.star (task.set {prop: starred, toggle: true}) that
// still takes the old --starred for --value.
func starredAlias() ActionAlias {
	return ActionAlias{Alias: "task.star", Action: "task.set", Args: Arguments{"prop": "starred", "toggle": true},
		ParamAliases: []ParamAlias{{Alias: "starred", Param: "value", Since: "v0.57.0", RemoveAfter: "v0.58.0"}}}
}

const starredNotice = "task.star: starred is an old name of value (renamed in v0.57.0; removed after v0.58.0)"

// An action alias's old parameter name fills its parameter from the API,
// before the alias's presets (so value's exclusion drops the toggle
// preset), with one notice per runtime; the parameter's own name wins.
func TestParamAliasFillsItsParameter(t *testing.T) {
	r, s := newWriteRuntime(t)
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	if err := r.AddActionAlias(starredAlias()); err != nil {
		t.Fatal(err)
	}
	starred := func() any { return s.find("t1")["starred"] }
	mustRun(t, r, Request{Noun: "task", Verb: "star", Args: Arguments{"id": "t1", "starred": true}})
	if starred() != true {
		t.Fatalf("starred=true: %v", starred())
	}
	mustRun(t, r, Request{Noun: "task", Verb: "star", Args: Arguments{"id": "t1", "starred": false}})
	if starred() != false {
		t.Fatalf("starred=false: %v", starred())
	}
	mustRun(t, r, Request{Noun: "task", Verb: "star", Args: Arguments{"id": "t1", "starred": false, "value": true}})
	if starred() != true {
		t.Fatalf("value wins over starred: %v", starred())
	}
	if got := sink.Notices(); !slices.Equal(got, []string{starredNotice}) {
		t.Fatalf("notices %q, want one %q", got, starredNotice)
	}
	// The old name belongs to the alias: the action itself refuses it.
	if _, err := r.Execute(Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "starred", "starred": true}}); err == nil {
		t.Fatal("task.set took the alias's old parameter name")
	}
	// A plain, undated old name posts no notice.
	if err := r.AddActionAlias(ActionAlias{Alias: "task.mark", Action: "task.set", Args: Arguments{"prop": "starred"}, ParamAliases: []ParamAlias{{Alias: "on", Param: "value"}}}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "mark", Args: Arguments{"id": "t1", "on": false}})
	if starred() != false || len(sink.Notices()) != 1 {
		t.Fatalf("mark on=false: %v, notices %q", starred(), sink.Notices())
	}
}

// An old parameter name must name a parameter of the action and must not
// be one.
func TestParamAliasChecks(t *testing.T) {
	r, _ := newWriteRuntime(t)
	for _, tc := range []struct {
		params []ParamAlias
		want   string
	}{
		{[]ParamAlias{{Alias: "starred", Param: "nope"}}, "has no parameter nope"},
		{[]ParamAlias{{Alias: "value", Param: "toggle"}}, "is a parameter of task.set"},
		{[]ParamAlias{{Alias: "s", Param: "value"}, {Alias: "s", Param: "toggle"}}, "given twice"},
		{[]ParamAlias{{Alias: "", Param: "value"}}, "needs alias and param"},
		{[]ParamAlias{{Alias: "--s", Param: "value"}}, "must be one name"},
	} {
		err := r.AddActionAlias(ActionAlias{Alias: "task.star", Action: "task.set", ParamAliases: tc.params})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: %v, want %q", tc.params, err, tc.want)
		}
	}
}

// The palette reads --OLD as its parameter on the alias's line, with the
// notice when the line runs; the old name is never completed.
func TestPaletteParamAlias(t *testing.T) {
	r, s := newWriteRuntime(t)
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	if err := r.AddActionAlias(starredAlias()); err != nil {
		t.Fatal(err)
	}
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "task star t1 --starred=false")
	if got := s.find("t1")["starred"]; got != false {
		t.Fatalf("--starred=false: %v", got)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "task star t1 --starred true")
	if got := s.find("t1")["starred"]; got != true {
		t.Fatalf("--starred true: %v", got)
	}
	if got := sink.Notices(); !slices.Equal(got, []string{starredNotice}) {
		t.Fatalf("notices %q", got)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteRequest(t, r, "set", "palette", Arguments{"text": "task star t1 --"})
	for _, e := range p.completions() {
		if strings.Contains(e.Value, "starred") && e.Kind != "instance" {
			t.Errorf("the old name is completed: %+v", e)
		}
	}
}

// actions.json alias rows declare old parameter names too; bindings check
// notes a row whose args use one, and the row runs as its parameter.
func TestParamAliasRowsAndBindingsCheck(t *testing.T) {
	r, s := newWriteRuntime(t)
	dir := t.TempDir()
	r.Config().Dir = dir
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(ActionsFile, `[
		{"alias": "task.star", "action": "task.set", "args": {"prop": "starred", "toggle": true},
		 "paramAliases": [{"alias": "starred", "param": "value", "since": "v0.57.0", "removeAfter": "v0.58.0"}]},
		{"alias": "task.bad", "action": "task.set", "paramAliases": [{"alias": "x", "param": "nope"}]}
	]`)
	problems := r.EnsureUserActions()
	if len(problems) != 1 || !strings.Contains(problems[0], "task.set has no parameter nope") {
		t.Fatalf("actions problems %q", problems)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "star", Args: Arguments{"id": "t1", "starred": true}})
	if got := s.find("t1")["starred"]; got != true {
		t.Fatalf("config alias starred=true: %v", got)
	}
	write(BindingsFile, `[{"noun": "task", "mode": "normal", "keys": "S", "request": {"noun": "task", "verb": "star", "args": {"id": "t1", "starred": false}}}]`)
	if problems := r.EnsureUserBindings(); len(problems) != 0 {
		t.Fatalf("bindings problems %q", problems)
	}
	want := "bindings.json: row 1 S: arg starred of task.star is an old name of value; " + starredNotice + ": rename the arg to value"
	if check := r.CheckUserBindings(); !slices.Contains(check, want) {
		t.Fatalf("bindings check lacks %q:\n%s", want, strings.Join(check, "\n"))
	}
}
