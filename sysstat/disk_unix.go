//go:build linux || darwin

package sysstat

import "syscall"

func readDisk(path string) (Disk, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Disk{}, err
	}
	bs := uint64(st.Bsize)
	return Disk{Free: uint64(st.Bavail) * bs, Total: uint64(st.Blocks) * bs}, nil
}
