//go:build windows

package ochazuke

import (
	"io"
	"os"
	"os/exec"
)

func foregroundStderr(cmd *exec.Cmd, tail *stderrTail) (func(), error) {
	dest := cmd.Stderr
	if dest == nil {
		dest = os.Stderr
	}
	cmd.Stderr = io.MultiWriter(dest, tail)
	return func() {}, nil
}
