//go:build !windows

package ochazuke

import (
	"os"
	"path/filepath"
	"syscall"
)

// lockFile holds an exclusive advisory lock on path (created when
// missing) until the returned unlock runs: processes sharing a config
// directory (the TUI, mcp serve, a one-shot CLI) take turns at a
// read-merge-write of one file.
func lockFile(path string) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
