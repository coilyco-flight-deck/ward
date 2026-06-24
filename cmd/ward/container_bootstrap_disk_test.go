package main

import (
	"context"
	"strings"
	"testing"
)

func TestDiskBytes(t *testing.T) {
	cases := map[uint64]string{
		0:                       "0B",
		512:                     "512B",
		1024:                    "1.0KiB",
		1536:                    "1.5KiB",
		1024 * 1024:             "1.0MiB",
		1024 * 1024 * 1024:      "1.0GiB",
		50 * 1024 * 1024 * 1024: "50.0GiB",
	}
	for in, want := range cases {
		if got := diskBytes(in); got != want {
			t.Errorf("diskBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestLooksLikeAuthError(t *testing.T) {
	auth := []string{
		"Error: Not logged in. Please run /login",
		"HTTP 401 Unauthorized",
		"invalid api key provided",
		`{"type":"error","error":{"type":"authentication_error"}}`,
		"UNAUTHORIZED",
	}
	for _, s := range auth {
		if !looksLikeAuthError(s) {
			t.Errorf("looksLikeAuthError(%q) = false, want true", s)
		}
	}
	notAuth := []string{
		"",
		"ENOSPC: no space left on device",
		"could not write ~/.claude/config",
		"connection timed out",
		"ok",
	}
	for _, s := range notAuth {
		if looksLikeAuthError(s) {
			t.Errorf("looksLikeAuthError(%q) = true, want false", s)
		}
	}
}

func TestDiskReportAndLowDiskPaths(t *testing.T) {
	// "/" is always stat-able on the test host; a bogus path is skipped.
	rep := diskReport([]string{"/", "/ward-nonexistent-xyz"})
	if !strings.HasPrefix(rep, "/ ") || !strings.Contains(rep, "free of") {
		t.Errorf("diskReport = %q, want a '/ ... free of ...' entry", rep)
	}
	if strings.Contains(rep, "nonexistent") {
		t.Errorf("diskReport leaked an unstattable path: %q", rep)
	}

	// All-unstattable -> the sentinel, never an empty string.
	if got := diskReport([]string{"/ward-nonexistent-xyz"}); got != "disk usage unavailable" {
		t.Errorf("diskReport(bogus) = %q, want sentinel", got)
	}

	// A 0-byte floor flags nothing; a huge floor flags "/".
	if low := lowDiskPaths([]string{"/"}, 0); len(low) != 0 {
		t.Errorf("lowDiskPaths floor=0 = %v, want empty", low)
	}
	if low := lowDiskPaths([]string{"/"}, ^uint64(0)); len(low) != 1 || low[0] != "/" {
		t.Errorf("lowDiskPaths floor=max = %v, want [/]", low)
	}
}

func TestCaptureProbeStdoutStderr(t *testing.T) {
	r := &Runner{}
	out, errOut, rc := r.captureProbe(context.Background(),
		[]string{"sh", "-c", "printf hi; printf oops >&2; exit 3"})
	if strings.TrimSpace(out) != "hi" {
		t.Errorf("stdout = %q, want %q", out, "hi")
	}
	if !strings.Contains(errOut, "oops") {
		t.Errorf("stderr = %q, want it to contain %q", errOut, "oops")
	}
	if rc != 3 {
		t.Errorf("rc = %d, want 3", rc)
	}
}

func TestCapBufferCapsAtMax(t *testing.T) {
	c := &capBuffer{max: 4}
	n, err := c.Write([]byte("abcdefgh"))
	if err != nil || n != 8 {
		t.Fatalf("Write = (%d, %v), want (8, nil)", n, err)
	}
	if c.String() != "abcd" {
		t.Errorf("buffer = %q, want %q", c.String(), "abcd")
	}
	// A second write past the cap is dropped but still reports full length.
	if n, _ := c.Write([]byte("ij")); n != 2 {
		t.Errorf("second Write n = %d, want 2", n)
	}
	if c.String() != "abcd" {
		t.Errorf("buffer after overflow = %q, want %q", c.String(), "abcd")
	}
}
