//go:build !windows

package main

import (
	"os"
	"syscall"
)

// fileGID returns the owning group id of info on Unix (ok=false with no Unix stat
// payload); container bootstrap group-grants the docker socket with it (ward#315, #288).
func fileGID(info os.FileInfo) (gid int, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Gid), true
}
