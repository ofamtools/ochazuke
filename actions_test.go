package ochazuke

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestActionHelperProcess is the fake command the action tests run: the
// test binary re-executed with OCHAZUKE_ACTION_HELPER=1. Its first word
// after -- picks what it does.
func TestActionHelperProcess(t *testing.T) {
	if os.Getenv("OCHAZUKE_ACTION_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	switch args[0] {
	case "echo":
		out, _ := json.Marshal(map[string]any{"argv": args[1:], "target": os.Getenv("OCHAZUKE_TARGET"), "action": os.Getenv("OCHAZUKE_ACTION"), "args": os.Getenv("OCHAZUKE_ARGS")})
		fmt.Println(string(out))
	case "text":
		fmt.Println("hello world")
	case "lines":
		fmt.Println(`{"n":1}`)
		fmt.Println(`{"n":2}`)
	case "fail":
		fmt.Fprintln(os.Stderr, "first words")
		fmt.Fprintln(os.Stderr, "last words")
		os.Exit(3)
	case "sleep":
		time.Sleep(10 * time.Second)
	}
	os.Exit(0)
}

// fakeCommand is the argv prefix that runs TestActionHelperProcess.
func fakeCommand(t *testing.T) []string {
	t.Helper()
	t.Setenv("OCHAZUKE_ACTION_HELPER", "1")
	return []string{os.Args[0], "-test.run=^TestActionHelperProcess$", "--"}
}

// actionsFixture is a runtime with a config directory holding actions.json
// (the entries given), a worktree list with one row and a Model over it.
func actionsFixture(t *testing.T, actions ...map[string]any) (*Runtime, *Model, string) {
	t.Helper()
	r := NewRuntime()
	r.Define("worktree")
	r.Instance("wt")
	if _, err := r.AddList("wt/list", []ListItem{{ID: "wt/main", Label: "main", Noun: "worktree", Data: map[string]any{"path": "/src/main", "branch": "main"}}}, Parent("wt")); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	r.Config().Dir = dir
	data, err := json.Marshal(actions)
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, dir, ActionsFile, string(data))
	return r, NewModel(r), dir
}

// settleJob runs the queued work and delivers it, returning the one
// completion it produced.
func settleJob(t *testing.T, r *Runtime) Completion {
	t.Helper()
	var got []Completion
	for _, msg := range runCommands(r.PendingCommands()) {
		if done, ok := msg.(Completion); ok {
			got = append(got, done)
			if _, err := r.Deliver(msg); err != nil && done.Err == nil {
				t.Fatal(err)
			}
		}
	}
	if len(got) != 1 {
		t.Fatalf("completions %+v", got)
	}
	return got[0]
}

func echoOf(t *testing.T, done Completion) map[string]any {
	t.Helper()
	if done.Err != nil {
		t.Fatal(done.Err)
	}
	raw, ok := done.Value.(json.RawMessage)
	if !ok {
		t.Fatalf("result %T %v", done.Value, done.Value)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// An action is an application verb on an existing noun: its parameters,
// positional order and confirm reach the operation, the argv template
// expands per run, an empty lone optional placeholder is left out, the
// job carries the label and the JSON output is the result.
func TestActionRunsCommandAsLabelledJob(t *testing.T) {
	cmd := fakeCommand(t)
	r, m, _ := actionsFixture(t, map[string]any{
		"noun": "worktree", "verb": "open", "description": "Open in the editor",
		"command":    append(cmd, "echo", "{{target}}", "{{label}}", "{{data.path}}", "--line={{line}}", "{{flag}}", "{{noun}}.{{verb}}"),
		"json":       true,
		"label":      "edit {{label}}",
		"parameters": map[string]any{"line": map[string]any{"type": "integer"}, "flag": map[string]any{"type": "string"}},
		"positional": []string{"line"},
	})
	m.Init()
	if p := r.Config().ActionProblems(); len(p) != 0 {
		t.Fatal(p)
	}
	op := r.Operations["worktree.open"]
	if op.Kind != KindApplication || op.Description != "Open in the editor" || op.Parameters["line"].Type != "integer" || !reflect.DeepEqual(op.Positional, []string{"line"}) {
		t.Fatalf("operation %+v", op)
	}
	if !slices.Contains(r.Nouns["worktree"].Verbs, "open") {
		t.Fatal("verb not listed on the noun")
	}
	if _, err := r.Execute(Request{Noun: "worktree", Verb: "open", Target: "wt/main", Args: Arguments{"line": 7}}); err != nil {
		t.Fatal(err)
	}
	jobs := r.Jobs()
	if len(jobs) != 1 || jobs[0].Verb != "worktree.open" || jobs[0].Label != "edit main" || jobs[0].Owner != "wt/main" {
		t.Fatalf("jobs %+v", jobs)
	}
	out := echoOf(t, settleJob(t, r))
	want := []any{"wt/main", "main", "/src/main", "--line=7", "worktree.open", "--json", `{"line":7}`}
	if !reflect.DeepEqual(out["argv"], want) || out["target"] != "wt/main" || out["action"] != "worktree.open" || out["args"] != `{"line":7}` {
		t.Fatalf("echo %v", out)
	}
	if got := r.Jobs(); got[0].State != JobDone {
		t.Fatalf("job %+v", got[0])
	}
}

// base is the JSON convention: BASE NOUN VERB --json ARGS --target T,
// with call naming another noun and verb; a confirm opens the modal and
// y runs the command.
func TestActionBaseConventionAndConfirm(t *testing.T) {
	cmd := fakeCommand(t)
	r, m, _ := actionsFixture(t, map[string]any{
		"noun": "worktree", "verb": "remove",
		"base":    append(cmd, "echo"),
		"call":    "tree delete",
		"target":  "{{data.branch}}",
		"confirm": "Remove {{label}} ({{target}})?",
		"parameters": map[string]any{
			"force": map[string]any{"type": "boolean"},
		},
	})
	m.Init()
	if _, err := r.Execute(Request{Noun: "worktree", Verb: "remove", Target: "wt/main", Args: Arguments{"force": true}}); err != nil {
		t.Fatal(err)
	}
	top := r.Instances[r.TopModal()]
	if top == nil || top.Noun != "confirm" || top.Payload.Prompt != "Remove main (wt/main)?" {
		t.Fatalf("confirm %+v", top)
	}
	if len(r.Jobs()) != 0 {
		t.Fatal("ran before the confirm")
	}
	press(t, r, "y")
	out := echoOf(t, settleJob(t, r))
	want := []any{"tree", "delete", "--json", `{"force":true}`, "--target", "main"}
	if !reflect.DeepEqual(out["argv"], want) {
		t.Fatalf("argv %v", out["argv"])
	}
	if r.Jobs()[0].Label != filepath.Base(os.Args[0]) {
		t.Fatalf("default label %q", r.Jobs()[0].Label)
	}
}

// Text output is the result text, JSON Lines a list of values, a failure
// names the last line of standard error, and a timeout says so.
func TestActionOutputsFailuresAndTimeout(t *testing.T) {
	cmd := fakeCommand(t)
	r, m, _ := actionsFixture(t,
		map[string]any{"noun": "worktree", "verb": "text", "command": append(cmd, "text")},
		map[string]any{"noun": "worktree", "verb": "lines", "command": append(cmd, "lines")},
		map[string]any{"noun": "worktree", "verb": "fail", "command": append(cmd, "fail")},
		map[string]any{"noun": "worktree", "verb": "slow", "command": append(cmd, "sleep"), "timeout": "50ms"},
	)
	m.Init()
	run := func(verb string) Completion {
		t.Helper()
		if _, err := r.Execute(Request{Noun: "worktree", Verb: verb, Target: "wt/main"}); err != nil {
			t.Fatal(err)
		}
		return settleJob(t, r)
	}
	if got := run("text"); got.Err != nil || got.Value != "hello world" {
		t.Fatalf("text %+v", got)
	}
	got := run("lines")
	lines, ok := got.Value.([]json.RawMessage)
	if !ok || len(lines) != 2 || string(lines[1]) != `{"n":2}` {
		t.Fatalf("lines %+v", got)
	}
	if got := run("fail"); got.Err == nil || !strings.Contains(got.Err.Error(), "exit status 3: last words") {
		t.Fatalf("fail %+v", got)
	}
	if got := run("slow"); got.Err == nil || !strings.Contains(got.Err.Error(), "timed out after 50ms") {
		t.Fatalf("slow %+v", got)
	}
	states := map[string]string{}
	for _, j := range r.Jobs() {
		states[j.Verb] = j.State
	}
	if states["worktree.fail"] != JobFailed || states["worktree.text"] != JobDone {
		t.Fatalf("states %v", states)
	}
}

// A malformed entry is a notice naming it and is skipped; the valid ones
// still install. A file that is not an array installs nothing.
func TestActionMalformedEntriesAreNotices(t *testing.T) {
	r, m, dir := actionsFixture(t,
		map[string]any{"noun": "worktree", "verb": "ok", "command": "true"},
		map[string]any{"noun": "nope", "verb": "x", "command": "true"},
		map[string]any{"noun": "worktree", "verb": "ok", "command": "true"},
		map[string]any{"noun": "worktree", "verb": "both", "command": "true", "base": "tool"},
		map[string]any{"noun": "worktree", "verb": "neither"},
		map[string]any{"noun": "worktree", "verb": "ph", "command": []string{"echo", "{{nosuch}}"}},
		map[string]any{"noun": "worktree", "verb": "typo", "commnd": "true"},
		map[string]any{"noun": "worktree", "verb": "ptype", "command": "true", "parameters": map[string]any{"n": map[string]any{"type": "float"}}},
		map[string]any{"noun": "worktree", "verb": "pos", "command": "true", "positional": []string{"n"}},
		map[string]any{"noun": "worktree", "verb": "time", "command": "true", "timeout": "soon"},
		map[string]any{"noun": "worktree", "verb": "call", "command": "true", "call": "a b"},
		map[string]any{"noun": "list", "verb": "move", "command": "true"},
	)
	m.Init()
	problems := r.Config().ActionProblems()
	for _, want := range []string{
		`action 2 (nope.x): unknown noun "nope"`,
		"action 3 (worktree.ok): worktree.ok is already defined",
		"action 4 (worktree.both): command and base exclude each other",
		"action 5 (worktree.neither): command or base is required",
		"action 6 (worktree.ph): unknown placeholder {{nosuch}}",
		`action 7: json: unknown field "commnd"`,
		"action 8 (worktree.ptype): parameter n: type must be string, integer or boolean",
		`action 9 (worktree.pos): positional "n" is not a declared parameter`,
		`action 10 (worktree.time): timeout "soon"`,
		"action 11 (worktree.call): call and target go with base",
		"action 12 (list.move): list.move is already defined",
	} {
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, want) }) {
			t.Errorf("no problem %q in %q", want, problems)
		}
	}
	if len(problems) != 11 || len(m.Notices) != 11 {
		t.Fatalf("problems %d notices %d: %q", len(problems), len(m.Notices), problems)
	}
	if got := r.UserActions(); len(got) != 1 || got[0].Key() != "worktree.ok" {
		t.Fatalf("installed %+v", got)
	}
	writeConfig(t, dir, ActionsFile, `{"noun": "worktree"}`)
	got := r.ReloadUserActions()
	if len(got) != 1 || !strings.Contains(got[0], "not a JSON array") || r.Operations["worktree.ok"].Noun != "" || len(m.Notices) != 1 {
		t.Fatalf("reload %q notices %q", got, m.Notices)
	}
	if slices.Contains(r.Nouns["worktree"].Verbs, "ok") {
		t.Fatal("reload left the old verb on the noun")
	}
}

// bindings.json rows can name an action's verb: Init installs the actions
// first. actions.reload replaces the verbs and applies the bindings again.
func TestActionBoundFromUserBindingsAndReloaded(t *testing.T) {
	cmd := fakeCommand(t)
	r, m, dir := actionsFixture(t, map[string]any{"noun": "worktree", "verb": "open", "command": append(cmd, "echo", "{{label}}")})
	writeConfig(t, dir, BindingsFile, `[{"noun": "worktree", "mode": "normal", "keys": "o", "request": {"noun": "worktree", "verb": "open"}}]`)
	m.Init()
	if p := r.Config().BindingProblems(); len(p) != 0 {
		t.Fatal(p)
	}
	if err := r.SetFocus("wt/main"); err != nil {
		t.Fatal(err)
	}
	press(t, r, "o")
	if out := echoOf(t, settleJob(t, r)); !reflect.DeepEqual(out["argv"], []any{"main"}) {
		t.Fatalf("argv %v", out["argv"])
	}
	writeConfig(t, dir, ActionsFile, `[{"noun": "worktree", "verb": "open", "command": ["`+strings.Join(cmd, `","`)+`", "echo", "again"]}]`)
	if got, err := r.Execute(Request{Noun: "actions", Verb: "reload"}); err != nil || len(got.([]string)) != 0 {
		t.Fatalf("reload %v %v", got, err)
	}
	press(t, r, "o")
	if out := echoOf(t, settleJob(t, r)); !reflect.DeepEqual(out["argv"], []any{"again"}) {
		t.Fatalf("argv after reload %v", out["argv"])
	}
	listed, err := r.Execute(Request{Noun: "actions", Verb: "list"})
	if err != nil || len(listed.([]Action)) != 1 {
		t.Fatalf("list %v %v", listed, err)
	}
}

// Without a config directory (a sandbox, a test binary) nothing is read.
func TestActionsNeedTheUserConfigDirectory(t *testing.T) {
	r := NewRuntime()
	if got := r.EnsureUserActions(); got != nil || len(r.UserActions()) != 0 {
		t.Fatalf("%v %v", got, r.UserActions())
	}
	if _, err := r.Execute(Request{Noun: "actions", Verb: "path"}); err == nil {
		t.Fatal("path without a directory")
	}
}
