//go:build unix

package main

import (
	"os"
	"syscall"
)

// flockExclusive / flockUnlock wrap the BSD advisory flock the bash used
// (`flock 9`) for substrate-warm mutual exclusion. Unix-only (PID 1 = Linux).

func flockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func flockUnlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
