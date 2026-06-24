//go:build unix

package main

import "syscall"

// diskFreeBytes returns available and total bytes for the filesystem backing path
// via statfs(2). Unix-only; the container runs as Linux PID 1 (ward#273).
func diskFreeBytes(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	// Bsize is int64 on linux, uint32 on darwin; uint64() spans both.
	bs := uint64(st.Bsize) //nolint:gosec,unconvert // platform-dependent width
	return st.Bavail * bs, st.Blocks * bs, nil
}
