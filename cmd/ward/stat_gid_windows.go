//go:build windows

package main

import "os"

// fileGID has no meaning on Windows (NTFS has no POSIX group ownership), so it always
// reports ok=false; its Linux-only callers short-circuit on that (ward#315, ward#288).
func fileGID(_ os.FileInfo) (gid int, ok bool) {
	return 0, false
}
