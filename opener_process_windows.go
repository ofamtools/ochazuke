//go:build windows

package ochazuke

import "os/exec"

// Windows captured-command cleanup does not kill a process group.
func detachOpener(cmd *exec.Cmd) {}
