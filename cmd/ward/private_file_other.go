//go:build !windows

package main

import "os"

func securePrivateFile(f *os.File) error {
	return f.Chmod(0o600)
}
