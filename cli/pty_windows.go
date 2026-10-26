//go:build windows

package cli

import "os/exec"

// configureCommand leaves the command as it is: ConPTY owns the console.
func configureCommand(*exec.Cmd) {}

// endGroup kills the process; Windows has no process group to hang up.
func endGroup(cmd *exec.Cmd, _ <-chan struct{}) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
