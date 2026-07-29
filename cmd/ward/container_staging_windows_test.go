//go:build windows

package main

import (
	"path/filepath"
	"testing"
)

func TestLaunchStagingDirWindowsDefaultUsesLocalAppData(t *testing.T) {
	home := t.TempDir()
	cache := t.TempDir()
	setTestHome(t, home)
	t.Setenv("LocalAppData", cache)
	t.Setenv(envStagingDir, "")
	t.Setenv(envLaunchStagingDir, "")
	t.Setenv(envInternalLaunchStagingDir, "")

	got, err := launchStagingDir()
	if err != nil {
		t.Fatalf("launchStagingDir: %v", err)
	}
	want := filepath.Join(cache, "ward", "staging")
	if got != want {
		t.Fatalf("launchStagingDir = %q, want LocalAppData root %q", got, want)
	}
	if filepath.Dir(filepath.Dir(got)) != cache {
		t.Fatalf("staging escaped LocalAppData: %q", got)
	}
}
