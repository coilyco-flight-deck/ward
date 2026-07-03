//go:build windows

package main

import "os/exec"

// detachProcess is a no-op on Windows: the drain-exit waiter is Linux-only (it blocks
// on `docker wait` in the dispatch path; docs/drain-timing.md), so there is no session.
func detachProcess(_ *exec.Cmd) {}
