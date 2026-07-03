//go:build windows

package main

import "os/exec"

// detachProcess is a no-op on Windows: the drain-exit waiter is Linux-only
// container-driver flow, and Windows has no setsid to mirror (see agent_detach_unix.go).
func detachProcess(_ *exec.Cmd) {}
