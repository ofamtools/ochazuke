//go:build !linux && !darwin

package sysstat

func readTicks() (ticks, error)     { return ticks{}, ErrUnsupported }
func readMemory() (Memory, error)   { return Memory{}, ErrUnsupported }
func readDisk(string) (Disk, error) { return Disk{}, ErrUnsupported }
