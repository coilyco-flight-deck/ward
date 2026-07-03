//go:build windows

package main

import "os/exec"

// detachProcess is a no-op on Windows: the drain-exit waiter is part of the
// Linux-only container agent-driver flow (ward#510), so there is no session to detach.
func detachProcess(_ *exec.Cmd) {}
