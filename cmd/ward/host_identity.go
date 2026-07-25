package main

import (
	"os"
	"runtime"
	"strings"
)

const envHostGOOS = "WARD_HOST_GOOS"

// launchHostGOOS preserves the invoking host identity when Ward dispatches from
// the Linux broker container on Docker Desktop.
func launchHostGOOS() string {
	switch goos := strings.ToLower(strings.TrimSpace(os.Getenv(envHostGOOS))); goos {
	case "darwin", "linux", "windows":
		return goos
	default:
		return runtime.GOOS
	}
}
