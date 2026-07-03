//go:build !windows

package main

import "testing"

// TestSpawnDetachedScoop_WindowsOnly pins that the detach spawn errors off
// Windows rather than silently claiming a dispatch. See ward#568.
func TestSpawnDetachedScoop_WindowsOnly(t *testing.T) {
	if err := spawnDetachedScoop("powershell", nil); err == nil {
		t.Error("spawnDetachedScoop should error off Windows")
	}
}
