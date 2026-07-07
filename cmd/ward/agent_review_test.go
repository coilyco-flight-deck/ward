package main

import (
	"bytes"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"github.com/coilyco-flight-deck/ward/internal/reviewpanel"
	"github.com/urfave/cli/v3"
)

// TestPanelLogRoundTrip proves a persisted panel row reads back and aggregates.
func TestPanelLogRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	rows := []reviewpanel.PanelResult{
		{Timestamp: 1, Issue: "o/r#1", Class: reviewpanel.ClassDefault, Gate: reviewpanel.GatePass, Passes: 2, Threshold: 2},
		{Timestamp: 2, Issue: "o/r#2", Class: reviewpanel.ClassRefactor, Gate: reviewpanel.GateBlock, Passes: 1, Threshold: 2},
		{Timestamp: 3, Issue: "o/r#3", Class: reviewpanel.ClassLintCleanup, Gate: reviewpanel.GateAdvisory},
	}
	for _, r := range rows {
		if err := appendPanelRecord(r); err != nil {
			t.Fatalf("appendPanelRecord: %v", err)
		}
	}
	got, err := readPanelLog()
	if err != nil {
		t.Fatalf("readPanelLog: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("read %d rows; want 3", len(got))
	}

	stats := computeReviewStats(got, parseRevertedSet("o/r#1"))
	if stats.Total != 3 || stats.Passed != 1 || stats.Blocked != 1 || stats.Advisory != 1 {
		t.Errorf("stats = %+v; want 3/1/1/1", stats)
	}
	// o/r#1 passed and is in the reverted set => a false negative (1/1 = 100%).
	if !stats.FalseNegKnown || stats.Reverted != 1 || stats.FalseNegRate != 1.0 {
		t.Errorf("false-negative not computed: %+v", stats)
	}
}

// TestReviewIssueRefFromEnv proves the ref/URL come off the container target env.
func TestReviewIssueRefFromEnv(t *testing.T) {
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_TARGET_ISSUE", "134")
	t.Setenv("WARD_FORGE", "")
	if got := reviewIssueRef(); got != "coilyco-flight-deck/ward#134" {
		t.Errorf("reviewIssueRef = %q", got)
	}
	if got := reviewIssueURL(); !strings.Contains(got, "/coilyco-flight-deck/ward/issues/134") {
		t.Errorf("reviewIssueURL = %q", got)
	}
}

// TestReviewerCandidatesExcludeClaude proves claude (the worker) is never in the
// reviewer pool and the pool carries a free + a paid tier.
func TestReviewerCandidatesExcludeClaude(t *testing.T) {
	var free, paid int
	for _, rv := range reviewerCandidates() {
		if rv.Family == "claude" {
			t.Fatalf("claude must never be a reviewer candidate")
		}
		if rv.Paid {
			paid++
		} else {
			free++
		}
	}
	if free == 0 || paid == 0 {
		t.Errorf("want at least one free and one paid tier; got free=%d paid=%d", free, paid)
	}
}

// TestReviewGateClauseInSeed proves the review gate is wired into a headless
// landing seed, skipped for patch-only, and suppressed by reviewGate=false.
func TestReviewGateClauseInSeed(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 134}

	direct := agentSeedPromptWorkflow(ref, "t", "b", "", true, nil, workflowDirectMain, true)
	if !strings.Contains(direct, "REVIEW GATE") || !strings.Contains(direct, "ward agent review") {
		t.Errorf("direct-main headless seed missing the review gate clause")
	}
	if !strings.Contains(direct, "merge to `main`") {
		t.Errorf("direct-main Forgejo landing phrase missing from the gate clause")
	}

	patch := agentSeedPromptWorkflow(ref, "t", "b", "", true, nil, workflowPatchOnly, true)
	if strings.Contains(patch, "REVIEW GATE") {
		t.Errorf("patch-only lands nothing; it must not carry the review gate")
	}

	off := agentSeedPromptWorkflow(ref, "t", "b", "", true, nil, workflowDirectMain, false)
	if strings.Contains(off, "REVIEW GATE") {
		t.Errorf("--no-review-gate (reviewGate=false) must suppress the clause")
	}
}

// TestReportPanelMachineLine proves the machine-readable WARD-REVIEW line and the
// advisory PR-body note reach stdout/stderr for the worker's seed to grep.
func TestReportPanelMachineLine(t *testing.T) {
	for _, tc := range []struct {
		gate     reviewpanel.Gate
		wantLine string
	}{
		{reviewpanel.GatePass, "WARD-REVIEW: pass"},
		{reviewpanel.GateBlock, "WARD-REVIEW: block"},
		{reviewpanel.GateAdvisory, "WARD-REVIEW: advisory"},
	} {
		var out, errb bytes.Buffer
		r := &Runner{Runner: &shell.Runner{Stdout: &out, Stderr: &errb}}
		res := reviewpanel.PanelResult{
			Worker: "claude", Class: reviewpanel.ClassDefault, Gate: tc.gate,
			Note:      "ADVISORY-ONLY REVIEW: nope",
			Reviewers: []reviewpanel.ReviewerResult{{Family: "codex", Verdict: reviewpanel.Block, Reason: "bug"}},
		}
		r.reportPanel(&cli.Command{}, res)
		if !strings.Contains(out.String(), tc.wantLine) {
			t.Errorf("gate %s: stdout missing %q\n got: %s", tc.gate, tc.wantLine, out.String())
		}
		if tc.gate == reviewpanel.GateAdvisory && !strings.Contains(errb.String(), "PR-BODY-NOTE:") {
			t.Errorf("advisory must print PR-BODY-NOTE; got: %s", errb.String())
		}
	}
}
