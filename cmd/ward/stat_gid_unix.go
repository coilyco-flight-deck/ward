//go:build !windows

package main

import (
	"os"
	"syscall"
)

// fileGID returns the owning group id of info on Unix, read from the underlying
// stat_t. ok is false when the FileInfo carries no Unix stat payload. Container
// bootstrap uses it to group-grant the docker socket and to verify the bot
// credential's group ownership (ward#315, ward#288). See stat_gid_windows.go for
// the Windows counterpart, which reports ok=false (no POSIX group ownership).
func fileGID(info os.FileInfo) (gid int, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Gid), true
}
