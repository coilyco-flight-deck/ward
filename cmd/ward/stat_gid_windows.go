//go:build windows

package main

import "os"

// fileGID has no meaning on Windows (no POSIX group ownership), so it reports
// ok=false; its Linux-only container callers short-circuit on that (ward#315, #288).
func fileGID(_ os.FileInfo) (gid int, ok bool) {
	return 0, false
}
