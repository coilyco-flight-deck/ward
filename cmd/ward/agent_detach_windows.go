//go:build windows

package main

import "os/exec"

// detachProcess is a no-op on Windows. The drain-exit waiter is part of the
// Linux-only container agent-driver flow (it blocks on `docker wait` inside the
// dispatch path; docs/drain-timing.md), which does not run on Windows hosts, so
// there is no session to detach. Windows has no setsid equivalent that this path
// needs; the waiter, if ever reached, simply starts as an ordinary child.
func detachProcess(_ *exec.Cmd) {}
