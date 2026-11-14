package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

// choiceRuntime has a verb that answers a ChoiceAnswer.
func choiceRuntime() *ochazuke.Runtime {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.Register(ochazuke.Operation{Key: "tag.use", Run: func(*ochazuke.Runtime, ochazuke.Context, ochazuke.Arguments) (any, error) {
		return nil, &ochazuke.ChoiceAnswer{Needs: "tag", Reason: "unknown tag alphb", Choices: []ochazuke.ChoiceRow{{ID: "alpha", Command: "tag use --tag alpha"}}}
	}})
	return r
}

// A ChoiceAnswer is exit status 3 on the CLI, the commands printed; over
// MCP it is a result with status choose, not a failure.
func TestChoiceAnswerCLIAndMCP(t *testing.T) {
	var stderr bytes.Buffer
	sh := New(choiceRuntime(), "test", nil, nil, &stderr)
	err := sh.Run(context.Background(), []string{"tag", "use"})
	var exit *oden.ExitError
	if !errors.As(err, &exit) || exit.Code != ChoiceExitCode || !strings.Contains(stderr.String(), "tag use --tag alpha") {
		t.Fatalf("cli: %v %q", err, stderr.String())
	}
	r := choiceRuntime()
	answer, err := (&mcpBackend{shell: New(r, "test", nil, nil, nil), actor: r.Actor()}).run(context.Background(), map[string]any{"verb": "tag.use"})
	m, _ := answer.(map[string]any)
	if err != nil || m["status"] != "choose" || m["reason"] != "unknown tag alphb" {
		t.Fatalf("mcp: %v %v", answer, err)
	}
}
