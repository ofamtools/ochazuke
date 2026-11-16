package cli

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

// outcomeRuntime has one verb of each outcome kind (tag.use done,
// tag.keep nothing / kept, tag.pick needs a choice, tag.burn failed) and
// tag.name, which needs its required name.
func outcomeRuntime() *ochazuke.Runtime {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	tag := r.Define("tag")
	tag.Verb("use", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("used") }, "Use")
	tag.Verb("keep", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Nothing(ochazuke.CauseKept, "the root stays") }, "Keep")
	tag.Verb("pick", func(ochazuke.Ctx) ochazuke.Result {
		return ochazuke.Failed(&ochazuke.ChoiceAnswer{Needs: "tag", Reason: "unknown tag alphb", Choices: []ochazuke.ChoiceRow{{ID: "alpha", Command: "tag pick --tag alpha"}}})
	}, "Pick")
	tag.Verb("burn", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Failed(errors.New("disk on fire")) }, "Burn")
	tag.Verb("name", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "Name")
	op := r.Operations.At("tag.name")
	op.Parameters = map[string]ochazuke.Parameter{"name": {Type: "string", Required: true}}
	r.Operations.Set(op)
	return r
}

// The CLI answers each kind by its encoder: done exit 0 with the value;
// nothing exit 0 with the wording as a status: line; needs exit 3 with one
// command per candidate on stderr and the outcome as JSON on a piped
// stdout (a missing argument too, through the socket); failed exit 1.
func TestCLIOutcomes(t *testing.T) {
	for _, c := range []struct {
		verb   string
		exit   int
		stderr string
		stdout string
	}{
		{"use", 0, "", `"used"`},
		{"keep", 0, "status: tag.keep: kept: the root stays", ""},
		{"pick", ChoiceExitCode, "tag pick --tag alpha", `"outcome":"needs"`},
		{"name", ChoiceExitCode, "", `"cause":"value"`},
		{"burn", 1, "", ""},
	} {
		var stdout, stderr bytes.Buffer
		sh := New(outcomeRuntime(), "test", nil, &stdout, &stderr)
		err := sh.Run(context.Background(), []string{"tag", c.verb})
		if oden.ExitCode(err) != c.exit || !strings.Contains(stderr.String(), c.stderr) || !strings.Contains(stdout.String(), c.stdout) {
			t.Errorf("%s: %v (exit %d) stdout %q stderr %q", c.verb, err, oden.ExitCode(err), stdout.String(), stderr.String())
		}
	}
}

// MCP run answers each kind with its status: done, nothing, choose (with
// the choices) and failed.
func TestMCPOutcomes(t *testing.T) {
	for _, c := range []struct{ verb, status, reason string }{
		{"tag.use", "done", ""},
		{"tag.keep", "nothing", "tag.keep: kept: the root stays"},
		{"tag.pick", "choose", "unknown tag alphb"},
		{"tag.name", "choose", "tag.name needs name"},
		{"tag.burn", "failed", "disk on fire"},
	} {
		r := outcomeRuntime()
		answer, err := (&mcpBackend{shell: New(r, "test", nil, nil, nil), actor: r.Actor()}).run(context.Background(), map[string]any{"verb": c.verb})
		m, _ := answer.(map[string]any)
		reason, _ := m["reason"].(string)
		if err != nil || m["status"] != c.status || reason != c.reason || (c.status == "choose" && m["choices"] == nil) {
			t.Errorf("%s: %v %v", c.verb, answer, err)
		}
	}
}

// cli.needs-status: choose answers needs as one (exit 3, MCP choose),
// error as the plain error (exit 1, MCP failed).
func TestNeedsStatusSetting(t *testing.T) {
	for _, c := range []struct {
		value  string
		exit   int
		status string
	}{
		{"choose", ChoiceExitCode, "choose"},
		{"error", 1, "failed"},
	} {
		r := outcomeRuntime()
		if err := r.Settings().Set(ochazuke.NeedsStatusSetting, c.value); err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		err := New(r, "test", nil, &bytes.Buffer{}, &stderr).Run(context.Background(), []string{"tag", "pick"})
		answer, _ := (&mcpBackend{shell: New(r, "test", nil, nil, nil), actor: r.Actor()}).run(context.Background(), map[string]any{"verb": "tag.pick"})
		if oden.ExitCode(err) != c.exit || answer.(map[string]any)["status"] != c.status {
			t.Errorf("%s: exit %d status %v", c.value, oden.ExitCode(err), answer)
		}
	}
}

// Every candidate command a failed command prints runs: a thing whose
// instance id is not its key (a store's row, live as pane:s/w1) is named
// by the instance the CLI's --target resolves, never by the bare key it
// would refuse (unknown target "w1").
func TestCLIPrintedTargetCommandsRun(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	things := []ochazuke.Thing{{"id": "w1"}, {"id": "w2"}}
	var ran []string
	r.Define("pane").Props(ochazuke.PropSpec{Name: "id", Key: true}).Store(ochazuke.Store{
		Kind: ochazuke.KindCollection,
		Get: func(_ ochazuke.Ctx, key string) (ochazuke.Thing, error) {
			for _, th := range things {
				if th["id"] == key {
					return th, nil
				}
			}
			return nil, errors.New("no pane " + key)
		},
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) { return things, nil },
		Query: func(ochazuke.Ctx, ochazuke.ListQuery) (ochazuke.ListPage, error) {
			return ochazuke.ListPage{Items: things, Total: len(things)}, nil
		},
	}).Verb("preview", func(c ochazuke.Ctx) ochazuke.Result {
		if c.Instance == nil {
			return ochazuke.Failed(errors.New("no pane"))
		}
		if c.Instance.Noun != "pane" {
			return ochazuke.Failed(errors.New("no pane at " + c.Instance.ID + " (" + c.Instance.Noun + ")"))
		}
		ran = append(ran, c.Instance.ID)
		return ochazuke.Done(nil)
	}, "Preview a pane")
	page, err := r.AddPage("panes", "Panes", func(*ochazuke.Runtime, int, int) ochazuke.Block { return ochazuke.Block{} })
	if err != nil {
		t.Fatal(err)
	}
	items := []ochazuke.ListItem{
		{ID: "pane:s/w1", Label: "one", Noun: "pane", Data: map[string]any{"id": "w1"}},
		{ID: "pane:s/w2", Label: "two", Noun: "pane", Data: map[string]any{"id": "w2"}},
	}
	if _, err := r.AddList("panes/list", items, ochazuke.Parent(page.ID)); err != nil {
		t.Fatal(err)
	}
	err = New(r, "test", nil, &bytes.Buffer{}, &bytes.Buffer{}).Run(context.Background(), []string{"pane", "preview"})
	if err == nil {
		t.Fatal("pane preview without a target ran")
	}
	var commands [][]string
	for _, line := range strings.Split(err.Error(), "\n") {
		command, _, _ := strings.Cut(strings.TrimSpace(line), "  # ")
		if strings.HasPrefix(command, "pane preview --target ") {
			commands = append(commands, strings.Fields(command))
		}
	}
	if len(commands) == 0 {
		t.Fatalf("no candidate commands: %v", err)
	}
	for _, words := range commands {
		if err := New(r, "test", nil, &bytes.Buffer{}, &bytes.Buffer{}).Run(context.Background(), words); err != nil {
			t.Errorf("%s: %v", strings.Join(words, " "), err)
		}
	}
	if !slices.Contains(ran, "pane:s/w1") || !slices.Contains(ran, "pane:s/w2") {
		t.Fatalf("ran %v from %v", ran, commands)
	}
}
