package main

import (
	"context"
	"strings"
	"testing"
)

// TestRunGitGrepRemoteRequiresRepoAndPattern checks the verb refuses argv that
// lacks either the repo ref or the pattern, before it touches the filesystem.
func TestRunGitGrepRemoteRequiresRepoAndPattern(t *testing.T) {
	r := &Runner{}
	cases := [][]string{
		nil,
		{"coilyco-flight-deck/ward"}, // repo but no pattern
	}
	for _, argv := range cases {
		if err := r.runGitGrepRemote(context.Background(), argv); err == nil {
			t.Errorf("expected usage error for argv %v, got nil", argv)
		}
	}
}

// TestRunGitGrepRemoteRejectsBadRepoRef checks an unparseable repo ref fails at
// resolution, before any clone is attempted.
func TestRunGitGrepRemoteRejectsBadRepoRef(t *testing.T) {
	r := &Runner{}
	err := r.runGitGrepRemote(context.Background(), []string{"not a repo ref", "pattern"})
	if err == nil {
		t.Fatal("expected error for unparseable repo ref, got nil")
	}
	if !strings.Contains(err.Error(), "grep-remote") {
		t.Errorf("error should name the verb, got %q", err)
	}
}

// TestGitGrepRemoteCommandShape pins the verb's name and that it skips flag
// parsing so git-grep flags reach git unmangled.
func TestGitGrepRemoteCommandShape(t *testing.T) {
	cmd := gitGrepRemoteCommand()
	if cmd.Name != "grep-remote" {
		t.Errorf("name = %q, want grep-remote", cmd.Name)
	}
	if !cmd.SkipFlagParsing {
		t.Error("grep-remote must SkipFlagParsing so git grep flags pass through")
	}
}
