//go:build windows

package cli

import (
	"io"
	"os"
)

// protocolStdout keeps stdout for the protocol; the tool's own prints go
// to stderr (os.Stdout points there while serving).
func protocolStdout(w io.Writer) (io.Writer, func()) {
	if w != os.Stdout {
		return w, func() {}
	}
	stdout := os.Stdout
	os.Stdout = os.Stderr
	return stdout, func() { os.Stdout = stdout }
}
