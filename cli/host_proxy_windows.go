//go:build windows

package cli

import "os/exec"

func capturedGroup(cmd *exec.Cmd) func() { return func() {} }
