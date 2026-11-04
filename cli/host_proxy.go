package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		if call.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, call.Timeout)
			defer cancel()
		}
		cmd := command(ctx)
		cmd.WaitDelay = call.WaitDelay
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("%s failed: %w: %s", strings.Join(cmd.Args[:min(3, len(cmd.Args))], " "), err, strings.TrimSpace(errOut.String()))
		}
		var value any
		if json.Unmarshal(out.Bytes(), &value) == nil {
			return value, nil
		}
		return strings.TrimRight(out.String(), "\r\n"), nil
	})
}
