package main

import (
	"strings"
	"testing"
	"time"
)

func TestAgentRunBudgetNote(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "file://"+writeBundleFixture(t))
	note := agentRunBudgetNote(roleEngineer)
	wantTTL := conciseDuration(agentReservationTTL())
	for _, want := range []string{"Run budget", "execution limit: 90m", "reservation TTL: " + wantTTL} {
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
