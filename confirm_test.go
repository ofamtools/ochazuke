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
