//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
	"time"
)

// groupGrace is how long a hung-up process group has to exit before it is
// killed.
const groupGrace = 500 * time.Millisecond

// configureCommand makes the command the leader of a new session whose
// controlling terminal is the pty (the slave is its fd 0), so its process
// group id is its pid and Release reaches every process it starts.
func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
}

// endGroup hangs up the command's process group at once and kills what is
// left of it after groupGrace unless done closes first. It never blocks.
func endGroup(cmd *exec.Cmd, done <-chan struct{}) {
	if cmd.Process == nil {
		return
	}
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGHUP)
	_ = syscall.Kill(-pgid, syscall.SIGCONT)
	go func() {
		select {
		case <-done:
		case <-time.After(groupGrace):
		}
		// The leader may be gone while children it started hold on.
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}()
}
