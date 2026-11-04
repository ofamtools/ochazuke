//go:build !windows

package ochazuke

import (
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// A PTY tee preserves isatty(2) for editors and full-screen programs.
func foregroundStderr(cmd *exec.Cmd, tail *stderrTail) (func(), error) {
	dest := cmd.Stderr
	if dest == nil {
		dest = os.Stderr
	}
	if f, ok := dest.(*os.File); ok && term.IsTerminal(f.Fd()) {
		master, slave, err := pty.Open()
		if err != nil {
			return nil, err
		}
		fd, err := unix.FcntlInt(master.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if err == nil {
			err = unix.SetNonblock(fd, true)
		}
		if err != nil {
			if fd >= 0 {
				_ = unix.Close(fd)
			}
			master.Close()
			slave.Close()
			return nil, err
		}
		_ = master.Close()
		master = os.NewFile(uintptr(fd), "foreground-stderr")
		if _, err = term.MakeRaw(slave.Fd()); err != nil {
			master.Close()
			slave.Close()
			return nil, err
		}
		_ = pty.InheritSize(f, slave)
		cmd.Stderr = slave
		done := make(chan struct{})
		go func() { defer close(done); _, _ = io.Copy(io.MultiWriter(dest, tail), master) }()
		return func() {
			_ = slave.Close()
			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
				_ = master.Close()
				<-done
			}
			_ = master.Close()
		}, nil
	}
	cmd.Stderr = io.MultiWriter(dest, tail)
	return func() {}, nil
}
