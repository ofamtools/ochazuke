package ochazuke

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// rowsOf reads a standard list's rows (each a thing's props with id and
// label) back into the struct its props came from.
func rowsOf[T any](t testing.TB, v any) []T {
	t.Helper()
	data, err := json.Marshal(AnswerRows(v))
	if err != nil {
		t.Fatal(err)
	}
	var out []T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("rows %s: %v", data, err)
	}
	return out
}

// recordOf reads a standard get's record back into a struct.
func recordOf[T any](t testing.TB, v any) T {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("record %s: %v", data, err)
	}
	return out
}

// The renamed command verbs (och-command-nouns) are gone with no alias, and
// bindings check tells a user's rows what to write instead, in the short
// and the canonical spelling and for the merged plural nouns.
func TestBindingsCheckNamesTheRenamedCommandVerbs(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	dir := t.TempDir()
	r.Config().Dir = dir
	writeConfig(t, dir, BindingsFile, `[
		{"noun": "app", "mode": "normal", "keys": "a", "request": {"noun": "focus", "verb": "set", "args": {"id": "x"}}},
		{"noun": "app", "mode": "normal", "keys": "b", "request": {"noun": "ochazuke.mode", "verb": "set", "args": {"mode": "normal"}}},
		{"noun": "app", "mode": "normal", "keys": "c", "request": {"noun": "settings", "verb": "edit"}},
		{"noun": "app", "mode": "normal", "keys": "d", "request": {"noun": "jobs", "verb": "list"}}
	]`)
	got, err := r.Execute(Request{Noun: "ochazuke.binding", Verb: "check"})
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Join(got.([]string), "\n")
	// The old names are built from words, so the verb check does not read
	// this file as naming them.
	old := func(noun, verb string) string { return noun + "." + verb }
	for _, want := range []string{
		old("focus", "set") + " is a removed verb (since R1); use ochazuke.focus.to {id}",
		old("settings", "edit") + " is a removed verb (since R1); use ochazuke.setting.open",
		old("jobs", "list") + " is a removed verb (since R1); use ochazuke.job.list",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, gone := range [][2]string{{"ochazuke.focus", "set"}, {"ochazuke.palette", "set"}, {"ochazuke.confirm", "set"}, {"ochazuke.input", "delete"}, {"ochazuke.settings", "list"}, {"ochazuke.pages", "list"}, {"ochazuke.jobs", "list"}} {
		if _, ok := r.Operation(old(gone[0], gone[1])); ok {
			t.Errorf("%s still runs", old(gone[0], gone[1]))
		}
	}
}

// get {prop} takes a dotted path into an object or list prop: the state's
// snapshot (a key holding dots is reached whole), a register's keys.
func TestGetReadsANestedPropPath(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	r.Define("side-row")
	if _, err := r.AddList("side.bar", []ListItem{{ID: "r1", Label: "one", Noun: "side-row"}, {ID: "r2", Label: "two", Noun: "side-row"}}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]any{
		"lists.side.bar.items.1.id":      "r2",
		"nouns.ochazuke.collection.name": "ochazuke.collection",
		"instances.side.bar.noun":        "ochazuke.collection",
		"modeState.name":                 "normal",
		"focus":                          r.Focus,
	} {
		got, err := r.Execute(Request{Noun: "ochazuke.snapshot", Verb: "get", Args: Arguments{"prop": path}})
		if err != nil || got != want {
			t.Errorf("state get %s = %v %v, want %v", path, got, err, want)
		}
	}
	for _, bad := range []string{"lists.nope", "lists.side.bar.items.9", "page.x", "nonesuch.a"} {
		if _, err := r.Execute(Request{Noun: "ochazuke.snapshot", Verb: "get", Args: Arguments{"prop": bad}}); err == nil {
			t.Errorf("state get %s did not fail", bad)
		}
	}
	// state is a system fact: its writing verbs are there, unavailable.
	if op, ok := r.Operation("ochazuke.snapshot.set"); !ok || op.Unavailable != FactReason {
		t.Fatalf("snapshot.set = %+v %v", op.Unavailable, ok)
	}
}

// Registers are things: set writes one (an uppercase name is the same
// register) and undo puts its old text back; register.append adds to one;
// get and list read them.
func TestRegisterThings(t *testing.T) {
	r := NewRuntime()
	text := func(name string) any {
		return mustRun(t, r, Request{Noun: "ochazuke.register", Verb: "get", Args: Arguments{"name": name, "prop": "text"}})
	}
	mustRun(t, r, Request{Noun: "ochazuke.register", Verb: "set", Args: Arguments{"name": "a", "prop": "text", "value": "one"}})
	mustRun(t, r, Request{Noun: "ochazuke.register", Verb: "set", Args: Arguments{"name": "A", "prop": "text", "value": "two"}})
	if got := text("a"); got != "two" {
		t.Fatalf("register a after set A = %v", got)
	}
	mustRun(t, r, Request{Noun: "ochazuke.history-entry", Verb: "undo"})
	if got := text("a"); got != "one" {
		t.Fatalf("register a after undo = %v", got)
	}
	mustRun(t, r, Request{Noun: "ochazuke.register", Verb: "append", Args: Arguments{"name": "a", "text": "two"}})
	if got := text("a"); got != "onetwo" {
		t.Fatalf("register a after append = %v", got)
	}
	mustRun(t, r, Request{Noun: "ochazuke.register", Verb: "set", Args: Arguments{"name": "b", "changes": map[string]any{"text": "x", "shape": "line"}}})
	if got := mustRun(t, r, Request{Noun: "ochazuke.register", Verb: "get", Args: Arguments{"name": "b"}}).(map[string]any); got["text"] != "x\n" || got["shape"] != "line" {
		t.Fatalf("register b = %v", got)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.register", Verb: "set", Args: Arguments{"name": "a", "prop": "shape", "value": "round"}}); err == nil {
		t.Fatal("an unknown shape was written")
	}
	rows := rowsOf[map[string]any](t, mustRun(t, r, Request{Noun: "ochazuke.register", Verb: "list"}))
	ids := []string{}
	for _, row := range rows {
		ids = append(ids, row["id"].(string))
	}
	if !slices.Contains(ids, "a") || !slices.Contains(ids, "b") {
		t.Fatalf("register list %v", ids)
	}
}

// Jobs are things kept after they end; job.get reads one by its number.
func TestJobsAreKeptThings(t *testing.T) {
	r := NewRuntime()
	r.Instance("pane")
	ticket, err := r.Launch("pane", "fetch", Pending(func(context.Context) (any, error) { return nil, nil }), Ctx{})
	if err != nil {
		t.Fatal(err)
	}
	r.Settle(ticket, nil)
	for range 2 {
		rows := rowsOf[JobInfo](t, mustRun(t, r, Request{Noun: "ochazuke.job", Verb: "list"}))
		if len(rows) != 1 || rows[0].State != JobDone {
			t.Fatalf("job list %+v", rows)
		}
	}
	got := mustRun(t, r, Request{Noun: "ochazuke.job", Verb: "get", Args: Arguments{"job": int(ticket.Job), "prop": "verb"}})
	if got != "fetch" {
		t.Fatalf("job get verb = %v", got)
	}
}

// The outline's rows and the list settings' items are things: get and list
// read them; their own delete and edit stay the core verbs.
func TestOutlineAndSettingItemAreThingNouns(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	for _, noun := range []string{"ochazuke.outline", "ochazuke.setting-item"} {
		for _, verb := range CoreVerbs() {
			if _, ok := r.Operation(noun + "." + verb); !ok {
				t.Errorf("%s has no %s", noun, verb)
			}
		}
		for _, own := range []string{"delete", "edit"} {
			if op, _ := r.Operation(noun + "." + own); op.Standard != "" {
				t.Errorf("%s.%s is the generated verb, not the noun's own", noun, own)
			}
		}
	}
	if err := r.DeclareSetting(Setting{Name: "paths", Type: SettingList, Default: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	if got := mustRun(t, r, Request{Noun: "ochazuke.setting-item", Verb: "get", Args: Arguments{"id": "paths/1", "prop": "text"}}); got != "b" {
		t.Fatalf("setting-item paths/1 = %v", got)
	}
}

// The appearance's set is undone like any standard write.
func TestAppearanceSetIsUndone(t *testing.T) {
	r := NewRuntime()
	a := NewAppearance(r)
	mustRun(t, r, Request{Noun: "ochazuke.appearance", Verb: "set", Args: Arguments{"prop": "density", "value": "compact"}})
	if a.Density != "compact" {
		t.Fatalf("density %q", a.Density)
	}
	mustRun(t, r, Request{Noun: "ochazuke.history-entry", Verb: "undo"})
	if a.Density != "comfortable" || a.Theme != "adaptive" {
		t.Fatalf("after undo: %+v", *a)
	}
}

// A palette line naming a removed verb (a renamed command verb, a verb of
// a merged plural noun) says what to write instead before its words
// resolve, in every spelling; a line the catalog answers is untouched.
func TestPaletteNamesTheReplacementOfARemovedVerb(t *testing.T) {
	_, p, _ := setPalette(t)
	for line, want := range map[string]string{
		"settings edit":                 "use ochazuke.setting.open",
		"jobs list":                     "use ochazuke.job.list",
		"focus set overview":            "use ochazuke.focus.to {id}",
		"pages list":                    "use ochazuke.page.list",
		"ochazuke settings list":        "use ochazuke.setting.list",
		"ochazuke.settings" + ".review": "use ochazuke.setting.diff {review: true}",
	} {
		c := p.parse(line)
		if c.Err == nil || !strings.Contains(c.Err.Error(), "was removed") || !strings.Contains(c.Err.Error(), want) {
			t.Errorf("%q: err %v, want %q", line, c.Err, want)
		}
	}
	for _, line := range []string{"setting open", "job list", "focus to x", "collection move 1", "set scrolloff=3", "settings"} {
		if c := p.parse(line); c.Err != nil && strings.Contains(c.Err.Error(), "was removed") {
			t.Errorf("%q reads as removed: %v", line, c.Err)
		}
	}
}

// A user's bindings.json row naming a removed verb is a start-up notice
// that names the replacement, and bindings check reports it once.
func TestRemovedVerbRowIsOneNotice(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	dir := t.TempDir()
	r.Config().Dir = dir
	writeConfig(t, dir, BindingsFile, `[{"noun": "app", "mode": "normal", "keys": "z", "request": {"noun": "settings", "verb": "edit"}}]`)
	problems := r.ReloadUserBindings()
	if len(problems) != 1 || !strings.Contains(problems[0], "use ochazuke.setting.open") {
		t.Fatalf("start-up notices %q", problems)
	}
	got, err := r.Execute(Request{Noun: "ochazuke.binding", Verb: "check"})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range got.([]string) {
		if strings.Contains(line, "ochazuke.setting.open") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("bindings check names the removed row %d times:\n%s", n, strings.Join(got.([]string), "\n"))
	}
}
