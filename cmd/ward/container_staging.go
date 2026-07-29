package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// launchEnvFilePrefix is shared by every credential env-file writer and its
	// stale-orphan sweeps.
	launchEnvFilePrefix = "ward-forgejo-env-"

	// WARD_STAGING_DIR is the public one-process override. The longer legacy
	// name remains readable for persistent brokers created by older Ward builds.
	envStagingDir               = "WARD_STAGING_DIR"
	envLaunchStagingDir         = "WARD_LAUNCH_STAGING_DIR"
	envInternalLaunchStagingDir = "WARD_INTERNAL_LAUNCH_STAGING_DIR"
)

// launchStagingDir resolves the operator-local root; config wins public env,
// while a persistent broker keeps its private container-local override.
func launchStagingDir() (string, error) {
	// A Linux broker sees the host's mounted config.yaml, whose Windows path is
	// meaningless inside the broker. Its composing host sets this private path.
	if os.Getenv(envPersistentDispatchBroker) == "1" {
		if dir := strings.TrimSpace(os.Getenv(envInternalLaunchStagingDir)); dir != "" {
			return dir, nil
		}
	}
	cfg, err := loadWardGlobalConfig()
	if err != nil {
		return "", fmt.Errorf("ward container: read container.staging-dir: %w", err)
	}
	if dir := strings.TrimSpace(cfg.Container.StagingDir); dir != "" {
		return dir, nil
	}
	if dir := strings.TrimSpace(os.Getenv(envStagingDir)); dir != "" {
		return dir, nil
	}
	if dir := strings.TrimSpace(os.Getenv(envLaunchStagingDir)); dir != "" {
		return dir, nil
	}

	home, _ := os.UserHomeDir()
	cache, _ := os.UserCacheDir()
	return defaultLaunchStagingDir(runtime.GOOS, home, cache, os.TempDir()), nil
}

func defaultLaunchStagingDir(goos, home, cache, temp string) string {
	switch goos {
	case "windows":
		if strings.TrimSpace(cache) != "" {
			return filepath.Join(cache, "ward", "staging")
		}
	case "darwin", "linux":
		if strings.TrimSpace(home) != "" {
			return filepath.Join(home, ".ward", "staging")
		}
	default:
		if strings.TrimSpace(cache) != "" {
			return filepath.Join(cache, "ward", "staging")
		}
	}
	return filepath.Join(temp, "ward", "staging")
}

func ensureLaunchStagingDir() (string, error) {
	dir, err := launchStagingDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("ward container: create launch staging dir: %w", err)
	}

	// Old builds staged directly in $HOME (the Windows profile root). Continue
	// checking that one legacy location until every past-TTL token file is gone.
	if legacy, ok := legacyLaunchStagingDir(dir); ok {
		sweepStaleLaunchEnvFiles(legacy)
	}
	return dir, nil
}

func legacyLaunchStagingDir(current string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" || sameLaunchPath(home, current) {
		return "", false
	}
	return home, true
}

func sameLaunchPath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// sweepStaleLaunchEnvFiles best-effort removes past-TTL credential-file
// orphans. Each orphan may hold a live forge or harness token.
func sweepStaleLaunchEnvFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), launchEnvFilePrefix) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil || time.Since(info.ModTime()) < containerAssetsTTL() {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
