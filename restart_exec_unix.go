//go:build unix

package ochazuke

import "syscall"

// platformExec replaces the running process (RestartHook.Exec's default).
var platformExec = syscall.Exec
