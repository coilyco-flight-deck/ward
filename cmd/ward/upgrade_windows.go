//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// Detached-child creation flags: no shared console, own process group (so a
// parent-console Ctrl-C never reaches it).
const (
	detachedProcess       = 0x00000008 // DETACHED_PROCESS
	createNewProcessGroup = 0x00000200 // CREATE_NEW_PROCESS_GROUP
)

// spawnDetachedScoop starts the background powershell detached with no inherited
// stdio and Release()s it, so returning here frees ward.exe for scoop. ward#568.
func spawnDetachedScoop(name string, args []string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start detached scoop update: %w", err)
	}
	return cmd.Process.Release()
}
