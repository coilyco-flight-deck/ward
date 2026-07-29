package main

import (
	"fmt"
	"os"
)

// writePrivateFile creates or truncates path, applies the platform's private
// file boundary while it is still empty, and only then writes secret content.
func writePrivateFile(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) // #nosec G304 -- caller supplies a Ward-owned state path
	if err != nil {
		return err
	}
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(path)
	}
	if err := securePrivateFile(f); err != nil {
		cleanup()
		return fmt.Errorf("secure private file: %w", err)
	}
	if _, err := f.Write(body); err != nil {
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}
