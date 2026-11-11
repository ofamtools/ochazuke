//go:build !windows

package ochazuke

import (
	"os/exec"
	"syscall"
)

// An opener is an intentional handoff, not captured work. Keep it outside
// the caller's group so a mounted extension's cleanup cannot kill it.
func detachOpener(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
