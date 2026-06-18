package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// TestCapturePreflightRunsInNeutralDir pins the ward#169 lever: the read runs in a
// fresh empty dir, never the dispatch cwd, and restores the cwd afterward.
func TestCapturePreflightRunsInNeutralDir(t *testing.T) {
	if _, err := os.Stat("/bin/pwd"); err != nil {
		t.Skip("/bin/pwd not present")
	}
	r := &Runner{Runner: &shell.Runner{Stderr: io.Discard}}

	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	// /bin/pwd computes the physical cwd of the child (its $PWD env is the stale
	// dispatch dir, so it falls back to the real one) - i.e. where the read ran.
	out, err := r.capturePreflight(context.Background(), []string{"/bin/pwd"})
	if err != nil {
		t.Fatalf("capturePreflight: %v", err)
	}
	got := strings.TrimSpace(string(out))

	// The read ran somewhere other than the dispatch cwd...
	if got == before {
		t.Errorf("pre-flight ran in the dispatch cwd %q; want a neutral dir", before)
	}
	// ...and that somewhere is a per-run temp dir, not a leftover path.
	if base := filepath.Base(got); !strings.HasPrefix(base, "ward-preflight-") {
		t.Errorf("pre-flight dir %q is not a ward-preflight temp dir", got)
	}
	// The temp dir is cleaned up after the read (it no longer exists).
	if _, statErr := os.Stat(got); !os.IsNotExist(statErr) {
		t.Errorf("pre-flight temp dir %q should be removed after the read (stat err: %v)", got, statErr)
	}
	// And the process cwd is restored for the launch that follows.
	if after, _ := os.Getwd(); after != before {
		t.Errorf("cwd not restored: before=%q after=%q", before, after)
	}
}
