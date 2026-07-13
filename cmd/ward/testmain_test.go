package main

import (
	"os"
	"runtime"
	"testing"
)

// TestMain neutralizes the operator's live config environment before any test
// runs. Tests that need these variables set them explicitly with t.Setenv.
func TestMain(m *testing.M) {
	for _, k := range []string{
		"WARD_CONFIG_REF", "WARD_CONFIG_TTL",
		"WARD_TARGET_OWNER", "WARD_TARGET_REPO",
	} {
		os.Unsetenv(k)
	}
	// Point the user cache dir at a throwaway so config-bundle cache writes
	// never land in (or poison) the operator's real cache.
	tmp, err := os.MkdirTemp("", "ward-test-cache-*")
	if err == nil {
		if runtime.GOOS == "windows" {
			os.Setenv("LocalAppData", tmp)
		} else {
			os.Setenv("XDG_CACHE_HOME", tmp)
		}
	}
	code := m.Run()
	if tmp != "" {
		_ = os.RemoveAll(tmp)
	}
	os.Exit(code)
}
