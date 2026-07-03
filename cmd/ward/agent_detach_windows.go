//go:build windows

package main

import "os/exec"

// detachProcess is a no-op on Windows: the drain-exit waiter is part of the Linux-only
// container flow (docs/drain-timing.md), so there is no session to detach here.
func detachProcess(_ *exec.Cmd) {}
