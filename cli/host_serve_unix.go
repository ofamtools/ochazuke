//go:build !windows

package cli

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// protocolStdout keeps the process's stdout for the protocol and points
// descriptor 1 (and os.Stdout) at stderr, so a stray print of the tool's
// own code cannot corrupt the stream. A writer that is not the process's
// stdout (a test) is the protocol as it is.
func protocolStdout(w io.Writer) (io.Writer, func()) {
	if w != os.Stdout {
		return w, func() {}
	}
	fd, err := unix.Dup(int(os.Stdout.Fd()))
	if err != nil {
		return w, func() {}
	}
	if err := unix.Dup2(int(os.Stderr.Fd()), int(os.Stdout.Fd())); err != nil {
		unix.Close(fd)
		return w, func() {}
	}
	protocol := os.NewFile(uintptr(fd), "protocol")
	stdout := os.Stdout
	os.Stdout = os.Stderr
	return protocol, func() {
		os.Stdout = stdout
		_ = unix.Dup2(fd, int(stdout.Fd()))
		protocol.Close()
	}
}
