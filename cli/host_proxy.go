package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ofam.dev/ochazuke"
)

// ProxyCommand describes a mounted verb's process. Env is the complete
// environment (nil inherits); Args includes the noun, verb and host arguments.
// Timeout and WaitDelay apply only to captured commands, never foreground.
type ProxyCommand struct {
	Bin       string
	Args      []string
	Env       []string
	Dir       string
	Timeout   time.Duration
	WaitDelay time.Duration
	// Before runs a captured command's preflight off the loop, under the
	// same cancellation and deadline. A failure prevents the command.
	Before func(context.Context) error
	// Pool, when set for the same Bin, runs a captured command on a kept
	// process of the tool (ServePool; exec stays its fallback). A
	// foreground verb always execs: it takes the terminal.
	Pool *ServePool
}

// RunMounted executes a catalog verb according to its foreground metadata.
// Foreground uses the framework's terminal handoff (stdin included), without
// a deadline. Captured stdout is decoded as JSON when possible, else text;
// failures include stderr. The caller can attach Label and Then as usual.
// Key and palette dispatch present a declared result by its kind in the
// runtime (text, Markdown and preformatted in their viewer; rows in the
// results table: keys.text-results, keys.table-results), so a host sets no
// presenter of its own; any other answer is the host's to show.
func RunMounted(v CatalogVerb, call ProxyCommand) ochazuke.Result {
	command := func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, call.Bin, call.Args...)
		cmd.Env, cmd.Dir = call.Env, call.Dir
		return cmd
	}
	if v.Foreground {
		return ochazuke.Foreground(command(context.Background()))
	}
	limit := call.Timeout
	if v.Timeout != "" {
		declared, err := time.ParseDuration(v.Timeout)
		if err != nil || declared < 0 {
			return ochazuke.Failed(fmt.Errorf("invalid execution timeout %q", v.Timeout))
		}
		if declared > 0 {
			limit = declared
		}
	}
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		if limit > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, limit)
			defer cancel()
		}
		if call.Before != nil {
			if err := call.Before(ctx); err != nil {
				return nil, err
			}
		}
		out, errOut, err := call.run(ctx, command, v.History == "fetch")
		if err != nil {
			name := v.Name
			if name == "" {
				name = filepath.Base(call.Bin)
			}
			reason := strings.TrimSpace(string(errOut))
			if i := strings.LastIndex(reason, "\n"); i >= 0 {
				reason = reason[i+1:]
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				reason = fmt.Sprintf("stopped after %s (limit)", limit)
			} else if ctx.Err() != nil {
				reason = "cancelled"
			} else if reason == "" {
				reason = err.Error()
			}
			return nil, fmt.Errorf("%s: %s", name, reason)
		}
		var value any
		if json.Unmarshal(out, &value) == nil {
			return value, nil
		}
		return strings.TrimRight(string(out), "\r\n"), nil
	})
}

// run is one captured run of the command: on the pool when it has one for
// this Bin, else a process of its own. A nonzero exit is an error, as
// exec.Cmd.Run reports it.
func (call ProxyCommand) run(ctx context.Context, command func(context.Context) *exec.Cmd, read bool) (out, errOut []byte, err error) {
	if call.Pool != nil && call.Pool.Bin == call.Bin {
		answer, err := call.Pool.Run(ctx, ServeCall{Argv: call.Args, Env: call.Env, Dir: call.Dir}, read)
		if err == nil && answer.Exit != 0 {
			err = fmt.Errorf("exit status %d", answer.Exit)
		}
		return answer.Stdout, answer.Stderr, err
	}
	cmd := command(ctx)
	cmd.WaitDelay = call.WaitDelay
	stop := capturedGroup(cmd)
	defer stop()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
