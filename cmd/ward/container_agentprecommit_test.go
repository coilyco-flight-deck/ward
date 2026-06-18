package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestAgentPreCommitConfig checks the generator pins the repo's agentic-os rev
// and enables exactly the two ward#139 commit-msg hooks.
func TestAgentPreCommitConfig(t *testing.T) {
	in := []byte(`repos:
  - repo: https://forgejo.coilysiren.me/coilyco-flight-deck/agentic-os
    rev: v0.19.0
    hooks:
      - id: code-comments
      - id: trufflehog
  - repo: https://github.com/hougesen/kdlfmt
    rev: v0.1.7
    hooks:
      - id: kdlfmt-check
`)
	out, err := agentPreCommitConfig(in)
	if err != nil {
		t.Fatalf("agentPreCommitConfig: %v", err)
	}

	// The output must be valid pre-commit config that pins the same rev and
	// enables only closes-issue + conventional-commit on the agentic-os repo.
	var doc preCommitConfigDoc
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("generated config is not valid yaml: %v\n%s", err, out)
	}
	if len(doc.Repos) != 1 {
		t.Fatalf("want exactly one repo entry, got %d: %s", len(doc.Repos), out)
	}
	if !strings.Contains(doc.Repos[0].Repo, "agentic-os") {
		t.Errorf("repo entry is not agentic-os: %q", doc.Repos[0].Repo)
	}
	if doc.Repos[0].Rev != "v0.19.0" {
		t.Errorf("rev = %q, want v0.19.0 (the repo's pinned rev)", doc.Repos[0].Rev)
	}
	for _, id := range []string{"closes-issue", "conventional-commit"} {
		if !strings.Contains(string(out), "id: "+id) {
			t.Errorf("generated config missing hook %q:\n%s", id, out)
		}
	}
	// Hooks the repo runs for everyone must NOT leak into the agent-only config.
	if strings.Contains(string(out), "trufflehog") || strings.Contains(string(out), "code-comments") {
		t.Errorf("generated config leaked a non-agent hook:\n%s", out)
	}
}

// TestAgentPreCommitConfigErrors covers the no-agentic-os and missing-rev cases
// the entrypoint relies on to skip gracefully.
func TestAgentPreCommitConfigErrors(t *testing.T) {
	cases := map[string]string{
		"no agentic-os repo": "repos:\n  - repo: https://github.com/hougesen/kdlfmt\n    rev: v0.1.7\n    hooks: []\n",
		"agentic-os no rev":  "repos:\n  - repo: https://forgejo.coilysiren.me/coilyco-flight-deck/agentic-os\n    hooks: []\n",
		"empty":              "",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := agentPreCommitConfig([]byte(in)); err == nil {
				t.Errorf("want error for %s, got none", name)
			}
		})
	}
}

// TestAgentPreCommitConfigMatchesRepo runs the generator against ward's own
// checked-in .pre-commit-config.yaml so a rev bump there can't silently desync.
func TestAgentPreCommitConfigMatchesRepo(t *testing.T) {
	data, err := os.ReadFile("../../.pre-commit-config.yaml")
	if err != nil {
		t.Skipf("repo .pre-commit-config.yaml unreadable: %v", err)
	}
	out, err := agentPreCommitConfig(data)
	if err != nil {
		t.Fatalf("agentPreCommitConfig on repo config: %v", err)
	}
	var doc preCommitConfigDoc
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("generated config invalid: %v", err)
	}
	if len(doc.Repos) != 1 || doc.Repos[0].Rev == "" {
		t.Fatalf("generated config did not pin a rev from the repo config:\n%s", out)
	}
}
