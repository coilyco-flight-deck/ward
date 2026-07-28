//go:build unix

package main

import (
	"os"
	"path/filepath"
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
		"backing mount:",
		"df -h",
		"/gitcache/config-bundle",
		"/gitcache/surface-scratch",
		diskBytes(surfaceScratchFloorBytes),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("low-budget error %q missing %q", err, want)
		}
	}
}

func TestSurfaceScratchMountUsesDeepestMountInfoEntry(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, "scratch")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	mountInfo := filepath.Join(root, "mountinfo")
	content := "1 0 0:1 / / rw,relatime - overlay overlay rw\n" +
		"2 1 8:1 /docker/volumes/ward-gitcache/_data " + root + " rw,relatime - ext4 /dev/vda1 rw\n" +
		"3 1 0:2 / " + filepath.Join(root, "scratch") + " rw,relatime - tmpfs tmpfs rw\n"
	if err := os.WriteFile(mountInfo, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := surfaceScratchMountInfoPath
	surfaceScratchMountInfoPath = mountInfo
	t.Cleanup(func() { surfaceScratchMountInfoPath = prev })

	got, ok := surfaceScratchMount(scratch)
	if !ok {
		t.Fatal("surfaceScratchMount did not resolve a mount")
	}
	if got.Source != "tmpfs" || got.Target != scratch || got.FSType != "tmpfs" {
		t.Fatalf("surfaceScratchMount = %+v, want tmpfs on %s", got, scratch)
	}
}

func TestMountInfoUnescape(t *testing.T) {
	if got := mountInfoUnescape(`/docker/volumes/ward\040gitcache/_data`); got != `/docker/volumes/ward gitcache/_data` {
		t.Fatalf("mountInfoUnescape = %q", got)
	}
}
