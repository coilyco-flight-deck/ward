//go:build !unix

package main

import "fmt"

var surfaceScratchDiskFreeBytes = func(path string) (free, total uint64, err error) {
	return 0, 0, fmt.Errorf("disk usage unavailable on this platform")
}

func surfaceScratchBudgetError(string) error { return nil }
