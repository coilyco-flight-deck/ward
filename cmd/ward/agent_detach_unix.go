//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the detached drain-exit waiter in its own session (setsid) so it
// outlives this process, holding no controlling terminal (ward#510; Windows is a no-op).
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
