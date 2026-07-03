//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the drain-exit waiter in its own session (setsid) so it
// outlives this process, holding no controlling terminal (ward#510). Windows: no-op.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
