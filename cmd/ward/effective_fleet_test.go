package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFleetBundle(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, bundleFleetKDLPath), []byte(body), 0o644); err != nil {
		t.Fatalf("write bundle fleet: %v", err)
	}
}

func TestLoadFleetConfigResolvesFrontierDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFleetBundle(t, dir, `
fleet {
    schema-version 2
    agent claude {
    }
}
`)
	t.Setenv(wardConfigRefEnv, "file://"+dir)

	f, err := loadFleetConfig()
	if err != nil {
		t.Fatalf("loadFleetConfig: %v", err)
	}

	if got := len(f.Agents); got < len(frontierAgentOrder) {
		t.Fatalf("len(Agents) = %d, want at least %d", got, len(frontierAgentOrder))
	}
	claude, ok := fleetAgent(f, string(modeClaude))
	if !ok {
		t.Fatal("effective fleet missing claude")
	}
	if claude.Binary != "claude" || claude.ContextLevel != 2 {
		t.Fatalf("claude resolved unexpectedly: %+v", claude)
	}
	if got := strings.Join(claude.Argv.Headless, " "); got != "claude -p --verbose --output-format stream-json" {
		t.Fatalf("claude headless argv = %q", got)
	}
	if _, ok := fleetAgent(f, string(modeCodex)); !ok {
		t.Fatal("effective fleet missing codex")
	}
}

func TestLoadFleetConfigSparseFrontierOverride(t *testing.T) {
	dir := t.TempDir()
	writeFleetBundle(t, dir, `
fleet {
    schema-version 2
    agent claude {
        context-level 1
    }
}
`)
	t.Setenv(wardConfigRefEnv, "file://"+dir)

	f, err := loadFleetConfig()
	if err != nil {
		t.Fatalf("loadFleetConfig: %v", err)
	}

	claude, ok := fleetAgent(f, string(modeClaude))
	if !ok {
		t.Fatal("effective fleet missing claude")
	}
	if claude.Binary != "claude" || claude.ContextLevel != 1 {
		t.Fatalf("claude resolved unexpectedly: %+v", claude)
	}
	if got := strings.Join(claude.Argv.Headless, " "); got != "claude -p --verbose --output-format stream-json" {
		t.Fatalf("claude headless argv = %q", got)
	}
}

func TestLoadFleetConfigRejectsIncompleteCustomAgent(t *testing.T) {
	dir := t.TempDir()
	writeFleetBundle(t, dir, `
fleet {
    schema-version 2
    agent widget {
        context-level 1
        argv {
            headless widget run
            interactive widget
        }
    }
}
`)
	t.Setenv(wardConfigRefEnv, "file://"+dir)

	_, err := loadFleetConfig()
	if err == nil {
		t.Fatal("loadFleetConfig accepted an incomplete custom agent; want a loud failure")
	}
	if !strings.Contains(err.Error(), "has no binary") {
		t.Fatalf("loadFleetConfig error = %v, want missing-binary failure", err)
	}
}
