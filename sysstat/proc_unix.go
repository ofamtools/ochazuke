//go:build linux || darwin

package sysstat

import (
	"time"

	"golang.org/x/sys/unix"
)

// readProcTime is this process's user plus system CPU time
// (getrusage RUSAGE_SELF).
func readProcTime() (time.Duration, error) {
	var ru unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &ru); err != nil {
		return 0, err
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano()), nil
}
