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
}

// RunMounted executes a catalog verb according to its foreground metadata.
// Foreground uses the framework's terminal handoff (stdin included), without
// a deadline. Captured stdout is decoded as JSON when possible, else text;
// failures include stderr. The caller can attach Label and Then as usual.
// Key and palette dispatch present declared text results in the runtime.
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
		cmd := command(ctx)
		cmd.WaitDelay = call.WaitDelay
		stop := capturedGroup(cmd)
		defer stop()
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil {
			name := v.Name
			if name == "" {
				name = filepath.Base(call.Bin)
			}
			reason := strings.TrimSpace(errOut.String())
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
		if json.Unmarshal(out.Bytes(), &value) == nil {
			return value, nil
		}
		return strings.TrimRight(out.String(), "\r\n"), nil
	})
}
