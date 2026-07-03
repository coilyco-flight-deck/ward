//go:build windows

package main

import "os"

// fileGID has no meaning on Windows (NTFS has no POSIX numeric group ownership),
// so it always reports ok=false. Its callers - the docker-socket group grant and
// the bot-credential group-ownership check - are part of the Linux-only container
// agent-driver flow (ward#315, ward#288) and short-circuit on the false return.
// See stat_gid_unix.go for the Unix implementation.
func fileGID(_ os.FileInfo) (gid int, ok bool) {
	return 0, false
}
