//go:build !windows

package main

import (
	"os"
	"syscall"
)

// fileGID returns the owning group id of info on Unix from the underlying stat_t; ok is
// false when the FileInfo has no Unix stat payload (ward#315, ward#288; see _windows).
func fileGID(info os.FileInfo) (gid int, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Gid), true
}
