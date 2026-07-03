//go:build windows

package main

import "os"

// fileGID has no meaning on Windows (no POSIX group ownership), so it always
// reports ok=false; its Linux-only callers short-circuit (ward#315, ward#288).
func fileGID(_ os.FileInfo) (gid int, ok bool) {
	return 0, false
}
