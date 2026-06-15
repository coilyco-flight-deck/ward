package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLocalRepoPathFallback verifies localRepoPath falls back to the
// canonical-owner path when no checkout exists under any primary org.
func TestLocalRepoPathFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	r := &Runner{}
	got, err := r.localRepoPath("nonexistent-repo")
	if err != nil {
		t.Fatalf("localRepoPath: %v", err)
	}
	want := filepath.Join(home, "projects", allowedOwner, "nonexistent-repo")
	if got != want {
		t.Fatalf("fallback path = %q, want %q", got, want)
	}
}

// TestLocalRepoPathScansOrgs verifies localRepoPath returns the first
// existing checkout across the primary-org set, not the fallback.
func TestLocalRepoPathScansOrgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	r := &Runner{}
	orgs := r.primaryOrgs()
	if len(orgs) < 2 {
		t.Skip("need at least two primary orgs to test the scan")
	}
	// Place the checkout under the second org so the fallback (first/canonical)
	// would be wrong.
	target := filepath.Join(home, "projects", orgs[1], "found-repo")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := r.localRepoPath("found-repo")
	if err != nil {
		t.Fatalf("localRepoPath: %v", err)
	}
	if got != target {
		t.Fatalf("scan path = %q, want %q", got, target)
	}
}

// TestDispatchRootsUnderCanonicalOwner pins the worktree/log roots outside any
// repo, under the canonical owner's projects dir.
func TestDispatchRootsUnderCanonicalOwner(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wt, err := dispatchWorktreeRoot()
	if err != nil {
		t.Fatalf("dispatchWorktreeRoot: %v", err)
	}
	if want := filepath.Join(home, "projects", allowedOwner, ".dispatch-worktrees"); wt != want {
		t.Fatalf("worktree root = %q, want %q", wt, want)
	}
	logRoot, err := dispatchLogRoot()
	if err != nil {
		t.Fatalf("dispatchLogRoot: %v", err)
	}
	if want := filepath.Join(home, "projects", allowedOwner, ".dispatch-logs"); logRoot != want {
		t.Fatalf("log root = %q, want %q", logRoot, want)
	}
}
