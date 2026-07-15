//go:build unix

package main

import (
	"fmt"
	"syscall"
)

var surfaceScratchDiskFreeBytes = func(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bs := uint64(st.Bsize) //nolint:gosec,unconvert -- platform-dependent width
	return st.Bavail * bs, st.Blocks * bs, nil
}

func surfaceScratchBudgetError(scratchDir string) error {
	free, total, err := surfaceScratchDiskFreeBytes(scratchDir)
	if err != nil {
		return fmt.Errorf("prepare scratch/cache root %s: could not inspect free space: %w", scratchDir, err)
	}
	if free >= surfaceScratchFloorBytes {
		return nil
	}
	return fmt.Errorf("⚠️ Docker resource constraint for %s: only %s free of %s; need at least %s for focused Go verification; recommended cache/temp location is %s (Go cache root %s)",
		scratchDir, diskBytes(free), diskBytes(total), diskBytes(surfaceScratchFloorBytes), scratchDir, surfaceScratchGoCacheDir(scratchDir))
}
