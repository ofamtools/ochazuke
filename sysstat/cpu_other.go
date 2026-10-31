//go:build !linux && !darwin

package sysstat

import "time"

func readTicks() (ticks, error)            { return ticks{}, ErrUnsupported }
func readMemory() (Memory, error)          { return Memory{}, ErrUnsupported }
func readDisk(string) (Disk, error)        { return Disk{}, ErrUnsupported }
func readProcTime() (time.Duration, error) { return 0, ErrUnsupported }
