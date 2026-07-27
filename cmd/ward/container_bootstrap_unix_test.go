//go:build unix

package main

import (
	"strings"
	"testing"
)

func TestPrepareScratchSpaceLowBudget(t *testing.T) {
	r := &Runner{}
	scratch := t.TempDir()
	preserveScratchEnv(t)
	prev := surfaceScratchDiskFreeBytes
	surfaceScratchDiskFreeBytes = func(string) (uint64, uint64, error) {
		return surfaceScratchFloorBytes - 1, surfaceScratchFloorBytes, nil
	}
	t.Cleanup(func() { surfaceScratchDiskFreeBytes = prev })

	err := r.prepareScratchSpace(scratch)
	if err == nil {
		t.Fatal("prepareScratchSpace should refuse a low-budget scratch root")
	}
	for _, want := range []string{
		"⚠️",
		"Docker resource constraint",
		scratch,
		"focused Go verification",
		"recommended cache/temp location",
		diskBytes(surfaceScratchFloorBytes),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("low-budget error %q missing %q", err, want)
		}
	}
}
