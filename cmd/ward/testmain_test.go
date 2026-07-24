package main

import (
	"os"
	"runtime"
	"testing"
)

// TestMain neutralizes compatibility env before tests run. Tests use t.Setenv
// when they prove that stale operator config cannot affect native policy.
func TestMain(m *testing.M) {
	for _, k := range []string{
		"WARD_CONFIG_REF", "WARD_CONFIG_TTL",
		envAgentImage, envAgentTag,
		"WARD_TARGET_OWNER", "WARD_TARGET_REPO",
	} {
		os.Unsetenv(k)
	}
	// Point the user cache dir at a throwaway so test cache writes never land in
	// (or poison) the operator's real cache.
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
