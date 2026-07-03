//go:build !windows

package main

import (
	"os"
	"syscall"
)

// fileGID returns info's owning group id on Unix from the underlying stat_t; ok is false
// when the FileInfo carries no Unix stat payload (ward#315, ward#288). See the sibling.
func fileGID(info os.FileInfo) (gid int, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Gid), true
}
