package main

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

const stagingMountProbeTarget = "/opt/ward-staging-check"

// stagingMountProbeArgv verifies the exact assets bind before the real Windows
// run receives credentials (docs/container-staging.md).
func stagingMountProbeArgv(plan upPlan) ([]string, string, bool) {
	for _, mount := range plan.Mounts {
		if mount.Volume || mount.Target != containerWardAssets || strings.TrimSpace(mount.Source) == "" {
			continue
		}
		probe := mountSpec{
			Source:   mount.Source,
			Target:   stagingMountProbeTarget,
			ReadOnly: true,
		}
		return []string{
			"run", "--rm", "--network=none",
			"--entrypoint", "/bin/sh",
			"-v", probe.arg(),
			plan.Image,
			"-c", "test -r " + stagingMountProbeTarget + "/" + containerEntrypointRel,
		}, mount.Source, true
	}
	return nil, "", false
}

func (r *Runner) preflightWindowsStagingMount(ctx context.Context, plan upPlan) error {
	if runtime.GOOS != "windows" || inContainer() {
		return nil
	}
	argv, source, ok := stagingMountProbeArgv(plan)
	if !ok {
		return nil
	}
	if err := r.dockerExec(ctx, argv...); err != nil {
		return stagingMountError(filepath.Dir(source), err)
	}
	return nil
}

func stagingMountError(root string, cause error) error {
	volume := stagingVolumeName(root)
	location := "the configured location"
	if volume != "" {
		location = "drive " + volume
	}
	return fmt.Errorf(
		"ward container: staging directory \"%s\" could not be bind-mounted from %s (%w); "+
			"open Docker Desktop Settings > Resources > File sharing, share %s, and retry",
		root, location, cause, location,
	)
}

func stagingVolumeName(root string) string {
	if volume := filepath.VolumeName(root); volume != "" {
		return volume
	}
	// Keep diagnostics testable on non-Windows builders.
	if len(root) >= 2 && root[1] == ':' &&
		((root[0] >= 'A' && root[0] <= 'Z') || (root[0] >= 'a' && root[0] <= 'z')) {
		return strings.ToUpper(root[:2])
	}
	return ""
}
