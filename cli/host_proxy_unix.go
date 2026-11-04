//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Give a recorder time to close its record, then kill every descendant.
func capturedGroup(cmd *exec.Cmd) func() {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var kill *time.Timer
	end := func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		kill = time.AfterFunc(time.Second, end)
		return err
	}
	return func() {
		if kill != nil {
			kill.Stop()
		}
		end()
	}
}
