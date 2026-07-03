//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the detached drain-exit waiter in its own session (setsid) so
// it outlives this process and holds no controlling terminal (Unix; ward#510).
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
