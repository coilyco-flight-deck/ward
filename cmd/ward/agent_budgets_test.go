package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentRunBudgetNote(t *testing.T) {
	dir := writeBundleFixture(t)
	if err := os.WriteFile(filepath.Join(dir, bundleDefaultsKDLPath), []byte(`
defaults {
    agent-reservation-ttl "3h"
}
`), 0o644); err != nil {
		t.Fatalf("override bundle defaults: %v", err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+dir)
	note := agentRunBudgetNote(roleEngineer)
	for _, want := range []string{"Run budget", "execution limit: 90m", "reservation TTL: 3h"} {
		if !strings.Contains(note, want) {
			t.Fatalf("budget note missing %q\n got: %s", want, note)
		}
	}
}

func TestAgentRunBudgetSummary(t *testing.T) {
	if got := agentRunBudgetSummary(roleDirector, 30*time.Minute); got != "" {
		t.Fatalf("director budget summary = %q, want empty", got)
	}
	if got := agentRunBudgetSummary(roleEngineer, 30*time.Minute); !strings.Contains(got, "remaining of 90m limit") {
		t.Fatalf("live budget summary = %q, want a remaining countdown", got)
	}
	if got := agentRunBudgetSummary(roleQA, 45*time.Minute); !strings.Contains(got, "expired by 15m against 30m limit") {
		t.Fatalf("expired budget summary = %q, want an expiry countdown", got)
	}
}
