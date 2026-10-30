package ochazuke

import "testing"

// A confirmed verb called with arguments and no instance of its noun to
// address (the CLI's positional words) names the request in its prompt,
// not app: {{label}}, {{id}} and {{target}} read the argument of that name,
// else the first argument in parameter order, and {{noun}} is the verb's
// noun. With no arguments they name the focus as before; addressed at a
// row, the prompt names the row.
func TestConfirmWithoutTargetReadsArguments(t *testing.T) {
	r := NewRuntime()
	noop := func(Ctx) Result { return Done(nil) }
	r.Define("worktree").Verb("remove", noop).Confirm("Remove {{noun}} {{label}} ({{id}}, {{branch}})?").Positional("path")
	op := r.Operations["worktree.remove"]
	op.Parameters = map[string]Parameter{"path": {Type: "string"}, "branch": {Type: "string"}, "force": {Type: "boolean"}, "label": {Type: "string"}}
	r.Operations["worktree.remove"] = op
	prompt := func(q Request) any {
		t.Helper()
		v, err := r.Execute(q)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Pop(); err != nil {
			t.Fatal(err)
		}
		return v.(map[string]any)["prompt"]
	}
	q := Request{Noun: "worktree", Verb: "remove", Args: Arguments{"branch": "fix", "path": "/src/a", "force": true}}
	if got := prompt(q); got != "Remove worktree /src/a (/src/a, fix)?" {
		t.Fatalf("no target, positional first: %v", got)
	}
	q.Args = Arguments{"force": true, "branch": "fix"}
	if got := prompt(q); got != "Remove worktree fix (fix, fix)?" {
		t.Fatalf("no target, required-then-name order: %v", got)
	}
	q.Args = Arguments{"branch": "fix", "label": "the fix tree"}
	if got := prompt(q); got != "Remove worktree the fix tree (fix, fix)?" {
		t.Fatalf("no target, label argument: %v", got)
	}
	q.Args = nil
	if got := prompt(q); got != "Remove worktree app (app, {{branch}})?" {
		t.Fatalf("no target, no arguments (the focus, as before): %v", got)
	}
	r.Instance("page", Kind("ochazuke.page"))
	if _, err := r.AddList("trees", []ListItem{{ID: "wt1", Label: "main tree", Noun: "worktree"}}, Parent("page")); err != nil {
		t.Fatal(err)
	}
	q = Request{Noun: "worktree", Verb: "remove", Target: "wt1", Args: Arguments{"branch": "fix"}}
	if got := prompt(q); got != "Remove worktree main tree (wt1, fix)?" {
		t.Fatalf("targeted row: %v", got)
	}
}

// A request that names its thing by key argument and targets another
// instance (a CLI call, task delete --id t2, the shell's owner of task
// rows being their list): the prompt names the request's noun and thing,
// never the targeted list ("Delete list t2?" was the bug, mtm note
// delete). The focus on another row (t1) does not leak in either.
func TestConfirmNamesTheRequestsNounNotTheTarget(t *testing.T) {
	r, _ := newWriteRuntime(t)
	if err := r.SetFocus("row-t1"); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"tasks/list", "", "row-t1"} {
		v, err := r.Execute(Request{Noun: "task", Verb: "delete", Target: target, Args: Arguments{"id": "t2"}})
		if err != nil {
			t.Fatal(err)
		}
		if got := v.(map[string]any)["prompt"]; got != "Delete task Write notes?" {
			t.Errorf("target %q: prompt %q, want Delete task Write notes?", target, got)
		}
		if err := r.Pop(); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.SetConfirm("task.delete", "Drop {{noun}} {{id}}?"); err != nil {
		t.Fatal(err)
	}
	v, err := r.Execute(Request{Noun: "task", Verb: "delete", Target: "tasks/list", Args: Arguments{"id": "t2"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := v.(map[string]any)["prompt"]; got != "Drop task t2?" {
		t.Errorf("{{id}} with a key argument: %q, want Drop task t2?", got)
	}
}
