package main

import (
	"errors"
	"fmt"
	"testing"
)

// TestCaptureLeafStdout asserts the redirect helper returns exactly what fn wrote to
// stdout and propagates fn's error without leaking the captured bytes (ward#316).
func TestCaptureLeafStdout(t *testing.T) {
	out, err := captureLeafStdout(func() error {
		fmt.Print("317\n")
		return nil
	})
	if err != nil {
		t.Fatalf("captureLeafStdout: %v", err)
	}
	if out != "317\n" {
		t.Errorf("captureLeafStdout = %q, want %q", out, "317\n")
	}

	sentinel := fmt.Errorf("boom")
	out, err = captureLeafStdout(func() error {
		fmt.Print("leaked")
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("captureLeafStdout error = %v, want %v", err, sentinel)
	}
	if out != "" {
		t.Errorf("captureLeafStdout on error = %q, want empty", out)
	}
}
