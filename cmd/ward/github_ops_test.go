package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

// TestIssueNumberFromURL covers pulling the trailing number off a gh-printed URL.
func TestIssueNumberFromURL(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"https://github.com/owner/repo/issues/123", 123, false},
		{"https://github.com/owner/repo/issues/123\n", 123, false},
		{"  https://github.com/owner/repo/pull/9  ", 9, false},
		{"https://github.com/owner/repo/issues/", 0, true},
		{"", 0, true},
		{"not-a-url", 0, true},
	}
	for _, c := range cases {
		got, err := issueNumberFromURL(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("issueNumberFromURL(%q): err=%v wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("issueNumberFromURL(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestGHIssuePath pins the REST path every read/flip in the client shares, so the
// GitHub surface stays on the REST budget rather than the GraphQL one (ward#466).
func TestGHIssuePath(t *testing.T) {
	if got := ghIssuePath("owner", "repo", 466); got != "/repos/owner/repo/issues/466" {
		t.Errorf("ghIssuePath = %q, want /repos/owner/repo/issues/466", got)
	}
}

// TestGHCommentsToIssueComments checks the gh comment mapping, including a bad
// timestamp degrading to the zero time rather than dropping the row.
func TestGHCommentsToIssueComments(t *testing.T) {
	raw := []ghComment{
		{Body: "first", CreatedAt: "2026-07-01T10:00:00Z"},
		{Body: "second", CreatedAt: "not-a-time"},
	}
	raw[0].User.Login = "alice"
	raw[1].User.Login = "bob"
	got := ghCommentsToIssueComments(raw)
	if len(got) != 2 {
		t.Fatalf("mapped %d comments, want 2", len(got))
	}
	if got[0].Body != "first" || got[0].User.Login != "alice" {
		t.Errorf("comment 0 = %+v", got[0])
	}
	if !got[0].CreatedAt.Equal(time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("comment 0 time = %v", got[0].CreatedAt)
	}
	if !got[1].CreatedAt.IsZero() {
		t.Errorf("bad timestamp should map to zero time, got %v", got[1].CreatedAt)
	}
	if got[1].User.Login != "bob" {
		t.Errorf("comment 1 author = %q", got[1].User.Login)
	}
}

// TestGithubLockUnlockIssue pins ward#494: GitHub's lock/unlock ride REST
// `PUT`/`DELETE .../issues/{n}/lock` through `gh api`, asserted via a stub `gh`.
func TestGithubLockUnlockIssue(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	ghStub := filepath.Join(dir, "gh")
	// The stub appends its args (one per line) so both calls land in one file (ward#494).
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\" >> " + argvFile + "; done\necho '---' >> " + argvFile + "\n"
	if err := os.WriteFile(ghStub, []byte(script), 0o755); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	r := &Runner{Runner: &shell.Runner{Resolve: func(string) (string, error) { return ghStub, nil }}}
	c := &githubClient{r: r, mode: modeClaude}

	if err := c.lockIssue(context.Background(), "coilyco-flight-deck", "ward", 494); err != nil {
		t.Fatalf("lockIssue: %v", err)
	}
	if err := c.unlockIssue(context.Background(), "coilyco-flight-deck", "ward", 494); err != nil {
		t.Fatalf("unlockIssue: %v", err)
	}
	out, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	got := string(out)
	for _, want := range []string{
		"api\n-X\nPUT\n/repos/coilyco-flight-deck/ward/issues/494/lock\n",
		"api\n-X\nDELETE\n/repos/coilyco-flight-deck/ward/issues/494/lock\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("gh argv missing %q\n got:\n%s", want, got)
		}
	}
}

// Compile-time assertion that both concrete clients implement the shared interface
// hostForgeClient returns (ward#489).
var (
	_ issueForge = (*forgejoClient)(nil)
	_ issueForge = (*githubClient)(nil)
)

// TestGitHubEnvEmitted checks a GitHub-forge plan exports WARD_FORGE + WARD_CLONE_BASE
// (github.com), while a Forgejo plan emits neither so its env is unchanged (ward#489).
func TestGitHubEnvEmitted(t *testing.T) {
	gh := sampleUpPlan()
	gh.Forge = forgeGitHub
	env := gh.wardEnv()
	if env["WARD_FORGE"] != "github" {
		t.Errorf("WARD_FORGE = %q, want github", env["WARD_FORGE"])
	}
	if env["WARD_CLONE_BASE"] != githubBaseURL {
		t.Errorf("WARD_CLONE_BASE = %q, want %q", env["WARD_CLONE_BASE"], githubBaseURL)
	}

	fj := sampleUpPlan()
	fjEnv := fj.wardEnv()
	if _, ok := fjEnv["WARD_FORGE"]; ok {
		t.Errorf("a Forgejo plan must not emit WARD_FORGE, got %q", fjEnv["WARD_FORGE"])
	}
	if _, ok := fjEnv["WARD_CLONE_BASE"]; ok {
		t.Errorf("a Forgejo plan must not emit WARD_CLONE_BASE, got %q", fjEnv["WARD_CLONE_BASE"])
	}
}
