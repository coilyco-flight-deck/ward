//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

var surfaceScratchMountInfoPath = "/proc/self/mountinfo"

func surfaceScratchBudgetError(scratchDir string) error {
	free, total, err := surfaceScratchDiskFreeBytes(scratchDir)
	if err != nil {
		return fmt.Errorf("prepare scratch/cache root %s: could not inspect free space: %w", scratchDir, err)
	}
	if free >= surfaceScratchFloorBytes {
		return nil
	}
	return fmt.Errorf("⚠️ Docker resource constraint for %s: only %s free of %s; need at least %s for focused Go verification; recommended cache/temp location is %s (Go cache root %s)%s",
		scratchDir, diskBytes(free), diskBytes(total), diskBytes(surfaceScratchFloorBytes), scratchDir, surfaceScratchGoCacheDir(scratchDir), surfaceScratchBudgetDiagnostic(scratchDir))
}

func surfaceScratchBudgetDiagnostic(scratchDir string) string {
	var parts []string
	if m, ok := surfaceScratchMount(scratchDir); ok {
		parts = append(parts, fmt.Sprintf("backing mount: %s on %s (%s)", m.Source, m.Target, m.FSType))
	}
	parts = append(parts,
		"an empty visible scratch directory can still fail when its backing filesystem is full",
		fmt.Sprintf("inspect Ward-owned cache on the same host disk with `df -h %s %s; du -h --max-depth=1 %s %s 2>/dev/null | sort -h`", scratchDir, containerGitcacheMnt, scratchDir, containerGitcacheMnt),
		fmt.Sprintf("safe cleanup candidates are stale/cache state under %s, %s, and repo mirrors %s/<owner>__<repo>.git", rootedPathJoin(containerGitcacheMnt, "config-bundle"), rootedPathJoin(containerGitcacheMnt, "surface-scratch"), containerGitcacheMnt),
	)
	return "; " + strings.Join(parts, "; ")
}

type scratchMount struct {
	Source string
	Target string
	FSType string
}

func surfaceScratchMount(path string) (scratchMount, bool) {
	target, err := filepath.Abs(path)
	if err != nil {
		target = filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	data, err := os.ReadFile(surfaceScratchMountInfoPath) // #nosec G304 -- fixed proc path, test-overridable.
	if err != nil {
		return scratchMount{}, false
	}
	var best scratchMount
	bestLen := -1
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+2 >= len(fields) {
			continue
		}
		mountPoint := mountInfoUnescape(fields[4])
		if !pathWithinMount(target, mountPoint) || len(mountPoint) <= bestLen {
			continue
		}
		best = scratchMount{Source: mountInfoUnescape(fields[sep+2]), Target: mountPoint, FSType: fields[sep+1]}
		bestLen = len(mountPoint)
	}
	return best, bestLen >= 0
}

func pathWithinMount(path, mountPoint string) bool {
	mountPoint = filepath.Clean(mountPoint)
	path = filepath.Clean(path)
	if mountPoint == string(os.PathSeparator) {
		return strings.HasPrefix(path, string(os.PathSeparator))
	}
	return path == mountPoint || strings.HasPrefix(path, mountPoint+string(os.PathSeparator))
}

func mountInfoUnescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+3 >= len(s) || !isOctal(s[i+1]) || !isOctal(s[i+2]) || !isOctal(s[i+3]) {
			b.WriteByte(s[i])
			continue
		}
		b.WriteByte((s[i+1]-'0')*64 + (s[i+2]-'0')*8 + (s[i+3] - '0'))
		i += 3
	}
	return b.String()
}

func isOctal(b byte) bool {
	return b >= '0' && b <= '7'
}
