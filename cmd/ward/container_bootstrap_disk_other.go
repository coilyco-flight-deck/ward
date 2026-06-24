//go:build !unix

package main

import "errors"

// diskFreeBytes fallback for non-unix builds: the container only runs as Linux
// PID 1, so disk diagnostics degrade to "unavailable" not a stub statfs (ward#273).
func diskFreeBytes(_ string) (free, total uint64, err error) {
	return 0, 0, errors.New("disk free unavailable on this platform")
}
