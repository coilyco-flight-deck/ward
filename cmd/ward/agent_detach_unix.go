//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the detached drain-exit waiter in its own session (setsid) so
// it outlives this process and holds no controlling terminal (ward#510). This is
// the Unix implementation; see agent_detach_windows.go for the Windows no-op.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
