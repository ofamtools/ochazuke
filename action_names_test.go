package ochazuke

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// aliasFixture is a runtime with a pane noun whose kill verb has the alias
// forget, an old name close (deprecated), and the same operation reachable
// as project.associate beside worker.associate. Every run records the name
// it was invoked by.
func aliasFixture(t *testing.T) (*Runtime, *[]Invocation, *RecordingSink) {
	t.Helper()
	r := NewRuntime()
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	var runs []Invocation
	record := func(c Ctx) Result {
		runs = append(runs, c.Invoked)
		return Done(c.Invoked.Name)
	}
	r.Define("project")
	r.Define("worker").
		Verb("kill", record, "Kill the worker").Alias("forget").Renamed("close", "v0.27.0", "v0.28.0").
		Verb("associate", record, "File the worker under a project").AliasOn("project", "associate").
		Verb("add-base", record, "Add a base").Renamed("base.add", "v0.27.0", "v0.28.0")
	if _, err := r.AddList("panes", []ListItem{{ID: "p1", Label: "one", Noun: "worker"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("p1"); err != nil {
		t.Fatal(err)
	}
	return r, &runs, sink
}

func TestAliasRunsTheCanonicalActionAndTellsTheHandlerItsName(t *testing.T) {
	r, runs, sink := aliasFixture(t)
	for _, name := range []string{"worker.kill", "worker.forget", "worker.close", "project.associate", "worker.associate", "worker.base.add"} {
		noun, verb := SplitKey(name)
		if name == "worker.base.add" {
			noun, verb = "worker", "base.add"
		}
		if _, err := r.Execute(Request{Noun: noun, Verb: verb}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	want := []Invocation{
		{Name: "worker.kill", Noun: "worker", Verb: "kill"},
		{Name: "worker.forget", Noun: "worker", Verb: "forget", Alias: true},
		{Name: "worker.close", Noun: "worker", Verb: "close", Alias: true},
		{Name: "project.associate", Noun: "project", Verb: "associate", Alias: true},
		{Name: "worker.associate", Noun: "worker", Verb: "associate"},
		{Name: "worker.base.add", Noun: "worker", Verb: "base.add", Alias: true},
	}
	if !slices.Equal(*runs, want) {
		t.Fatalf("invocations:\n got %+v\nwant %+v", *runs, want)
	}
	// A dotted alias spelled as Do splits it at its last dot; the table
	// still finds it.
	if q := r.canonicalRequest(Do("worker.base.add").Request); q.Key() != "worker.add-base" || q.Via != "worker.base.add" {
		t.Fatalf("Do through a dotted alias: %+v", q)
	}
	// The deprecated aliases each said so once; the permanent one never.
	var notices []string
	for _, m := range sink.Messages() {
		if m.Kind == UINotice {
			notices = append(notices, m.Text)
		}
	}
	if _, err := r.Execute(Request{Noun: "worker", Verb: "close"}); err != nil {
		t.Fatal(err)
	}
	wantNotices := []string{
		"worker.close is an old name of worker.kill (renamed in v0.27.0; removed after v0.28.0)",
		"worker.base.add is an old name of worker.add-base (renamed in v0.27.0; removed after v0.28.0)",
	}
	if !slices.Equal(notices, wantNotices) || len(sink.Messages()) != 2 {
		t.Fatalf("notices %q (messages %d), want %q", notices, len(sink.Messages()), wantNotices)
	}
	// History records the canonical request and the name it was spelled by.
	last := r.History(1)[0]
	if last.Request.Key() != "worker.kill" || last.Request.Via != "worker.close" {
		t.Fatalf("history: %+v", last.Request)
	}
	if r.Invoked() != (Invocation{}) {
		t.Fatalf("invocation outlives the request: %+v", r.Invoked())
	}
}

func TestInteractionHandlersReadInvoked(t *testing.T) {
	r := NewRuntime()
	var got Invocation
	r.Register(Operation{Noun: "tool", Verb: "spin", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		got = r.Invoked()
		return nil, nil
	}})
	if err := r.AliasAction("tool.twirl", "tool.spin"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "tool", Verb: "twirl"}); err != nil {
		t.Fatal(err)
	}
	if got != (Invocation{Name: "tool.twirl", Noun: "tool", Verb: "twirl", Alias: true}) {
		t.Fatalf("Invoked: %+v", got)
	}
}

func TestAliasRules(t *testing.T) {
	r, _, _ := aliasFixture(t)
	for _, c := range []struct {
		alias, action, want string
	}{
		{"worker.kill", "worker.associate", "would shadow the action worker.kill"},
		{"worker.forget", "worker.associate", "already an alias of worker.kill"},
		{"worker.nothing", "worker.missing", `action "worker.missing" is not registered`},
		{"ghost.kill", "worker.kill", "names no registered noun"},
		{"", "worker.kill", "required"},
	} {
		err := r.AliasAction(c.alias, c.action)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s -> %s: %v, want %q", c.alias, c.action, err, c.want)
		}
	}
	// The same alias for the same action again is a no-op; an alias of an
	// alias names the action itself.
	if err := r.AliasAction("worker.forget", "worker.kill"); err != nil {
		t.Fatal(err)
	}
	if err := r.AliasAction("worker.zap", "worker.forget"); err != nil {
		t.Fatal(err)
	}
	if a, ok := r.ActionAlias("worker.zap"); !ok || a.Action != "worker.kill" {
		t.Fatalf("alias of an alias: %+v %v", a, ok)
	}
	// A short framework noun name in an alias is canonical.
	if err := r.AliasAction("list.walk", "list.move"); err != nil {
		t.Fatal(err)
	}
	if a, _ := r.ActionAlias("list.walk"); a.Alias != "ochazuke.list.walk" || a.Noun != "ochazuke.list" {
		t.Fatalf("framework noun alias: %+v", a)
	}
	// An action registered later under an alias's name takes it, and the
	// drop is recorded instead of a panic.
	r.Define("worker").Verb("zap", func(Ctx) Result { return Done("own") })
	if _, ok := r.ActionAlias("worker.zap"); ok {
		t.Fatal("alias survived an action of its name")
	}
	if problems := r.ActionAliasProblems(); len(problems) != 1 || !strings.Contains(problems[0], "alias worker.zap of worker.kill dropped") {
		t.Fatalf("problems %q", problems)
	}
	if v, err := r.Execute(Request{Noun: "worker", Verb: "zap"}); err != nil || v != "own" {
		t.Fatalf("zap: %v %v", v, err)
	}
}

func TestAliasesAreListedOnceUnderTheCanonicalName(t *testing.T) {
	r, _, _ := aliasFixture(t)
	r.Define("worker").Mode("normal").Bind("X", Do("worker.forget")).Bind("x", Do("worker.kill"))
	var kills int
	for _, op := range r.Catalog() {
		if strings.HasSuffix(op.Key(), ".forget") || op.Key() == "project.associate" || op.Key() == "worker.close" {
			t.Fatalf("alias listed as an operation: %s", op.Key())
		}
		if op.Key() == "worker.kill" {
			kills++
			if !slices.Equal(op.Aliases, []string{"worker.close", "worker.forget"}) {
				t.Fatalf("catalog aliases %q", op.Aliases)
			}
		}
		if op.Key() == "worker.associate" && !slices.Equal(op.Aliases, []string{"project.associate"}) {
			t.Fatalf("associate aliases %q", op.Aliases)
		}
	}
	if kills != 1 {
		t.Fatalf("worker.kill listed %d times", kills)
	}
	// Rows store the canonical request, the alias kept in Via; help and
	// bindings list name the canonical operation and its aliases.
	var help []HelpRow
	for _, row := range BindingHelp(r, "p1") {
		if row.Key == "X" || row.Key == "x" {
			help = append(help, row)
		}
	}
	if len(help) != 2 {
		t.Fatalf("help rows %+v", help)
	}
	for _, row := range help {
		if row.Operation != "worker.kill" || !slices.Equal(row.Aliases, []string{"close", "forget"}) || !strings.HasSuffix(row.Description, "· also: close, forget") {
			t.Fatalf("help row %+v", row)
		}
	}
	for _, row := range r.ListBindings("p1") {
		if row.Keys == "X" && (row.Request.Key() != "worker.kill" || row.Request.Via != "worker.forget" || !slices.Equal(row.Aliases, []string{"worker.close", "worker.forget"})) {
			t.Fatalf("bindings list row %+v", row)
		}
	}
}

// A user's actions.json adds alias rows; a clash is skipped and reported
// the way a bad bindings row is, and bindings check says what a row
// spelled by an alias resolves to.
func TestUserAliasRowsAndBindingsCheck(t *testing.T) {
	r, runs, _ := aliasFixture(t)
	dir := t.TempDir()
	r.Config().Dir = dir
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(ActionsFile, `[
		{"alias": "worker.k", "action": "worker.kill", "note": "mine"},
		{"alias": "worker.kill", "action": "worker.associate"},
		{"alias": "worker.forget", "action": "worker.associate"},
		{"alias": "worker.close", "action": "worker.kill"},
		{"alias": "worker.k", "action": "worker.associate"},
		{"alias": "worker.x", "action": "worker.kill", "extra": true},
		{"alias": "worker.echo", "action": "worker.say"},
		{"noun": "worker", "verb": "say", "command": ["echo", "hi"]}
	]`)
	problems := r.EnsureUserActions()
	want := []string{
		`actions.json: alias 6: json: unknown field "extra" (skipped)`,
		"actions.json: alias 2 (worker.kill): alias worker.kill would shadow the action worker.kill (skipped)",
		"actions.json: alias 3 (worker.forget): alias worker.forget is already an alias of worker.kill (app) (skipped)",
		"actions.json: alias 4 (worker.close): alias worker.close is already an alias of worker.kill (app) (skipped)",
		"actions.json: alias 5 (worker.k): alias worker.k is already declared by entry 1 (skipped)",
	}
	if !slices.Equal(problems, want) {
		t.Fatalf("problems:\n%s\nwant:\n%s", strings.Join(problems, "\n"), strings.Join(want, "\n"))
	}
	if got := r.UserAliases(); len(got) != 2 || got[0].Alias != "worker.k" || got[0].Note != "mine" || got[0].Source != AliasConfig || got[1].Action != "worker.say" {
		t.Fatalf("user aliases %+v", got)
	}
	if _, err := r.Execute(Request{Noun: "worker", Verb: "k"}); err != nil {
		t.Fatal(err)
	}
	if last := (*runs)[len(*runs)-1]; last.Name != "worker.k" {
		t.Fatalf("user alias ran as %+v", last)
	}
	if got := r.CheckUserActions(); !slices.Equal(got, want) {
		t.Fatalf("check:\n%s", strings.Join(got, "\n"))
	}
	write(BindingsFile, `[{"noun": "worker", "mode": "normal", "keys": "K", "request": {"noun": "worker", "verb": "k"}},
		{"noun": "worker", "mode": "normal", "keys": "C", "request": {"noun": "worker", "verb": "close"}},
		{"noun": "worker", "mode": "normal", "keys": "N", "request": {"noun": "worker", "verb": "nope"}}]`)
	if problems := r.EnsureUserBindings(); len(problems) != 1 || !strings.Contains(problems[0], `unknown operation "worker.nope"`) {
		t.Fatalf("bindings problems %q", problems)
	}
	check := r.CheckUserBindings()
	for _, line := range []string{
		"bindings.json: row 1 K: worker.k resolves to worker.kill (an alias, config)",
		"bindings.json: row 2 C: worker.close resolves to worker.kill (an alias, app); worker.close is an old name of worker.kill (renamed in v0.27.0; removed after v0.28.0): rename the row to worker.kill",
		want[1],
	} {
		if !slices.Contains(check, line) {
			t.Errorf("bindings check lacks %q:\n%s", line, strings.Join(check, "\n"))
		}
	}
	// A reload drops the file's aliases before reading it again.
	write(ActionsFile, `[]`)
	if problems := r.ReloadUserActions(); len(problems) != 0 {
		t.Fatal(problems)
	}
	if _, ok := r.ActionAlias("worker.k"); ok {
		t.Fatal("a removed alias row survived reload")
	}
	if _, ok := r.ActionAlias("worker.forget"); !ok {
		t.Fatal("reload dropped the application's alias")
	}
}

// A write with a required slot left out is refused, never filled from
// the focus or a guess: the API and CLI fail naming the slot, the palette
// asks for it and runs nothing.
func TestWritesAreNeverSilentlyFilled(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	var ran []Arguments
	r.Define("worker").Verb("rename", func(c Ctx) Result {
		ran = append(ran, c.Args)
		return Done(nil)
	}, "Rename the worker").Alias("title")
	op := r.Operations["worker.rename"]
	op.Parameters = map[string]Parameter{"name": {Type: "string", Required: true}}
	r.Operations["worker.rename"] = op
	if _, err := r.AddList("panes", []ListItem{{ID: "p1", Label: "build", Noun: "worker", Data: map[string]any{"name": "build"}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("p1"); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"rename", "title"} {
		_, err := r.Execute(Request{Noun: "worker", Verb: verb})
		var argErr *ArgumentError
		if !errors.As(err, &argErr) || !strings.Contains(err.Error(), "name is required") {
			t.Fatalf("%s without name: %v", verb, err)
		}
	}
	// The palette: the line names the verb (by an alias) but not the
	// name; enter leaves the palette open on the missing slot.
	if _, err := r.Execute(Request{Noun: "palette", Verb: "open"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Palette.setText("title"); err != nil {
		t.Fatal(err)
	}
	c := m.Palette.parse("title")
	if c.Op.Key() != "worker.rename" || c.Via != "worker.title" || c.Missing != "name" || c.Complete() {
		t.Fatalf("palette parse %+v", c)
	}
	if err := m.Palette.run(); err == nil || !strings.Contains(err.Error(), "missing name") || !m.Palette.IsOpen() {
		t.Fatalf("palette run without the slot: %v (open %v)", err, m.Palette.IsOpen())
	}
	if len(ran) != 0 {
		t.Fatalf("a write ran without its slot: %v", ran)
	}
	if _, err := r.Execute(Request{Noun: "worker", Verb: "title", Args: Arguments{"name": "web"}}); err != nil || len(ran) != 1 || ran[0]["name"] != "web" {
		t.Fatalf("with its slot: %v %v", ran, err)
	}
}
