//go:build !windows

package main

import "fmt"

// spawnDetachedScoop is Windows-only (the scoop self-block has no brew analogue);
// present so upgrade.go compiles cross-platform, never called off Windows. ward#568.
func spawnDetachedScoop(_ string, _ []string) error {
	return fmt.Errorf("detached scoop update is Windows-only")
}
