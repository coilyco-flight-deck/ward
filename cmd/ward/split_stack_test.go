package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitStackIssueURLKeepsTrackerThroughBroker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(`repos {
    repo-authority default=forgejo {
        trusted-owner "coilysiren"
        repo "coilysiren/coilysiren" forge=github tracker=forgejo landing=github mirrors="forgejo"
    }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+dir)

	issueURL := forgejoBaseURL + "/coilysiren/coilysiren/issues/18"
	ref, err := parseAgentIssueRef(issueURL)
	if err != nil {
		t.Fatalf("parseAgentIssueRef: %v", err)
	}
	if ref.Forge != forgeGitHub || ref.Tracker != trackerForgejo {
		t.Fatalf("split-stack ref = %+v, want GitHub checkout + Forgejo tracker", ref)
	}
	if got := ref.url(); got != issueURL {
		t.Fatalf("tracker URL = %q, want %q", got, issueURL)
	}

	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{"engineer", issueURL, "--harness", "codex"})
	argv := brokerEngineerArgv(cmd, modeCodex, ref)
	if got := argv[1]; got != issueURL {
		t.Fatalf("broker target = %q, want tracker-qualified URL %q", got, issueURL)
	}
	forwarded, err := parseAgentIssueRef(argv[1])
	if err != nil {
		t.Fatalf("parse broker target: %v", err)
	}
	if forwarded.Forge != forgeGitHub || forwarded.Tracker != trackerForgejo {
		t.Fatalf("forwarded split-stack ref = %+v, want GitHub checkout + Forgejo tracker", forwarded)
	}
}

func TestSplitStackCompactRefIgnoresEdgeConfiguredTracker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(`repos {
    repo-authority default=forgejo {
        trusted-owner "coilysiren"
        repo "coilysiren/inbox" forge=forgejo tracker=forgejo
    }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+dir)
	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		t.Fatal(err)
	}
	if got := defs.authorityForRepo("coilysiren", "inbox").Tracker; got != trackerGitHub {
		t.Fatalf("native tracker = %s, want baked github", got)
	}

	ref, err := parseAgentIssueRef("coilysiren/inbox#247")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Tracker != trackerGitHub {
		t.Fatalf("compact ref tracker = %s, want baked github", ref.Tracker)
	}
}
