package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseWorkflow covers the --workflow parse gate (ward#508).
// It checks the known modes, the default, and the transitional aliases.
func TestParseWorkflow(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want workflowMode
	}{
		{"", workflowPullRequest},
		{"direct-to-main", workflowDirectToMain},
		{"direct-main", workflowDirectToMain},
		{"pull-request", workflowPullRequest},
		{"pull-requests", workflowPullRequest},
		{"pull-request-and-merge", workflowPullRequestAndMerge},
		{"pull-requests-and-merge", workflowPullRequestAndMerge},
		{"remote-branch-only", workflowRemoteBranchOnly},
		{"patch-only", workflowRemoteBranchOnly},
	} {
		got, err := parseWorkflow(tc.in)
		if err != nil {
			t.Errorf("parseWorkflow(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseWorkflow(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := parseWorkflow("pr"); err == nil {
		t.Fatal("parseWorkflow accepted the removed pr short form")
	}
	if _, err := parseWorkflow("merge-everything"); err == nil {
		t.Fatal("parseWorkflow accepted an unknown mode")
	} else if !strings.Contains(err.Error(), "direct-to-main|pull-request|pull-request-and-merge|remote-branch-only") {
		t.Errorf("unknown-mode error should list the choices, got %v", err)
	}
}

// TestWorkflowLandsOnMain pins the reaper-facing predicate: only direct-to-main may
// push/merge main. The empty default now resolves to pull-request.
func TestWorkflowLandsOnMain(t *testing.T) {
	if !workflowDirectToMain.landsOnMain() {
		t.Error("direct-to-main must land on main")
	}
	if workflowMode("").landsOnMain() || workflowPullRequest.landsOnMain() || workflowPullRequestAndMerge.landsOnMain() || workflowRemoteBranchOnly.landsOnMain() {
		t.Error("default/pull-request/pull-request-and-merge/remote-branch-only must NOT land on main")
	}
}

// TestWorkflowCarryClauseDirectToMain keeps the fast path generic across forges.
func TestWorkflowCarryClauseDirectToMain(t *testing.T) {
	for _, ref := range []agentIssueRef{
		{Owner: "o", Repo: "r", Number: 7},                     // Forgejo
		{Owner: "o", Repo: "r", Number: 7, Forge: forgeGitHub}, // GitHub
	} {
		if got, want := workflowCarryClause(ref, workflowDirectToMain), directToMainCarryClause(ref); got != want {
			t.Errorf("direct-to-main carry clause diverged from the direct fast path:\n got: %s\nwant: %s", got, want)
		}
	}
}

// TestWorkflowCarryClausePullRequest checks the PR carry clause.
func TestWorkflowCarryClausePullRequest(t *testing.T) {
	got := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 12}, workflowPullRequest)
	for _, want := range []string{"pull request", "closes #12", "watching its CI/checks", "director is encouraged to merge it later"} {
		if !strings.Contains(got, want) {
			t.Errorf("pull-request carry clause missing %q\n got: %s", want, got)
		}
	}
	for _, want := range []string{
		"post the same actionable failure comment to both the linked issue and the PR",
		"reservation-lock release/clear/hand-back wording",
		"signature/idempotency marker",
		"skip the PR comment",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("pull-request carry clause missing PR-failure steer %q\n got: %s", want, got)
		}
	}
	if strings.Contains(got, "merge to main, push - and close") {
		t.Fatalf("pull-request carry clause regressed to the direct-to-main fast path:\n%s", got)
	}
}

// TestWorkflowCarryClausePullRequestAndMerge proves the merge-authorized lane keeps
// the PR flow and says the run is not done until the merge lands.
func TestWorkflowCarryClausePullRequestAndMerge(t *testing.T) {
	got := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 17}, workflowPullRequestAndMerge)
	for _, want := range []string{"pull request", "closes #17", directorMergeWorkflowMarker, "director-merge authorized", "the pull request is merged"} {
		if !strings.Contains(got, want) {
			t.Errorf("pull-request-and-merge carry clause missing %q\n got: %s", want, got)
		}
	}
	for _, want := range []string{
		"post the same actionable failure comment to both the linked issue and the PR",
		"reservation-lock release/clear/hand-back wording",
		"signature/idempotency marker",
		"skip the PR comment",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("pull-request-and-merge carry clause missing PR-failure steer %q\n got: %s", want, got)
		}
	}
	if strings.Contains(got, "merge to main, push - and close") {
		t.Fatalf("pull-request-and-merge carry clause regressed to the direct-to-main fast path:\n%s", got)
	}
}

// TestWorkflowCarryClauseRemoteBranchOnly: remote-branch-only pushes a branch and
// lands nothing else.
func TestWorkflowCarryClauseRemoteBranchOnly(t *testing.T) {
	got := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 99}, workflowRemoteBranchOnly)
	for _, want := range []string{"remote-branch-only", "push the branch to origin", "do not open a pull request", "do not write a `closes #99`"} {
		if !strings.Contains(got, want) {
			t.Errorf("remote-branch-only carry clause missing %q\n got: %s", want, got)
		}
	}
}

// TestAgentSeedPromptWorkflow ties the whole seam together: the seed's carry clause
// AND the reflection's landing phrase both shift with the mode (ward#508).
func TestAgentSeedPromptWorkflow(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 508}

	direct := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowDirectToMain, true, "")
	if !strings.Contains(direct, "merge to main, push") {
		t.Errorf("direct-to-main seed should carry the merge-to-main clause\n got: %s", direct)
	}
	if !strings.Contains(direct, "the work is committed, merged to main, and pushed") {
		t.Errorf("direct-to-main reflection should name the merge-and-push landing\n got: %s", direct)
	}

	pr := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPullRequest, true, "")
	if !strings.Contains(pr, "pull request") || strings.Contains(pr, "merge to main, push - and close") {
		t.Errorf("pull-request seed should carry a PR clause, not the merge-to-main fast path\n got: %s", pr)
	}
	if !strings.Contains(pr, "WARD-OUTCOME: submitted") {
		t.Errorf("pull-request reflection should end with submitted, not done\n got: %s", pr)
	}
	if strings.Contains(pr, "WARD-OUTCOME: done") {
		t.Errorf("pull-request reflection must not ask the engineer to post done\n got: %s", pr)
	}
	if !strings.Contains(pr, "the branch is pushed, the pull request is open, and the required checks are green") {
		t.Errorf("pull-request reflection should require green checks before done\n got: %s", pr)
	}
	for _, want := range []string{
		"post the same actionable failure comment to both the linked issue and the PR",
		"reservation-lock release/clear/hand-back wording",
		"signature/idempotency marker",
	} {
		if !strings.Contains(pr, want) {
			t.Errorf("pull-request reflection should steer PR-failure comments with %q\n got: %s", want, pr)
		}
	}
	merge := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPullRequestAndMerge, true, "")
	if !strings.Contains(merge, "director-merge authorized") {
		t.Errorf("pull-request-and-merge seed should mark the PR as director-merge authorized\n got: %s", merge)
	}
	if !strings.Contains(merge, directorMergeWorkflowMarker) {
		t.Errorf("pull-request-and-merge seed should carry the PR-body marker\n got: %s", merge)
	}
	if !strings.Contains(merge, "workflow: <mode>; review summary: <summary or skip state>") {
		t.Errorf("headless reflection should include the machine-readable workflow/review line\n got: %s", merge)
	}
	if workflowCarryClause(ref, "") != workflowCarryClause(ref, workflowPullRequest) {
		t.Error("empty workflow should resolve to the pull-request carry clause")
	}
	prMerge := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPullRequestAndMerge, true, "")
	if !strings.Contains(prMerge, "director-merge authorized") {
		t.Errorf("pull-request-and-merge seed should carry the director merge lane\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "WARD-OUTCOME: merge-ready") {
		t.Errorf("pull-request-and-merge reflection should end with merge-ready, not done\n got: %s", prMerge)
	}
	if strings.Contains(prMerge, "WARD-OUTCOME: done") {
		t.Errorf("pull-request-and-merge reflection must not ask the engineer to post done\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "the pull request is reviewed and merge-ready") {
		t.Errorf("pull-request-and-merge reflection should require merge-ready before done\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "skip the PR comment") {
		t.Errorf("pull-request-and-merge reflection should tell the worker to skip PR comments when no PR exists\n got: %s", prMerge)
	}

	branchOnly := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowRemoteBranchOnly, true, "")
	if !strings.Contains(branchOnly, "remote-branch-only") {
		t.Errorf("remote-branch-only seed should say it has no PR or merge authority\n got: %s", branchOnly)
	}
	if !strings.Contains(branchOnly, "the remote branch is pushed") {
		t.Errorf("remote-branch-only reflection should name the branch landing\n got: %s", branchOnly)
	}
	if strings.Contains(branchOnly, "post the same actionable failure comment to both the linked issue and the PR") {
		t.Errorf("remote-branch-only reflection must not ask for PR comments when no PR exists\n got: %s", branchOnly)
	}

	// The plain agentSeedPrompt wrapper follows the safe pull-request default.
	if agentSeedPrompt(ref, "reframe ward", "do it", "", true, nil) != pr {
		t.Error("agentSeedPrompt should equal agentSeedPromptWorkflow(..., pull-request)")
	}
}

// TestWorkflowEnvAndLabels pins the container plumbing: a non-default workflow rides
// WARD_WORKFLOW + a ward.workflow label; direct-to-main leaves both untouched (ward#508).
func TestWorkflowEnvAndLabels(t *testing.T) {
	repo := targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}

	direct := upPlan{Role: roleEngineer, Mode: modeClaude, Repo: repo, Issue: 508, Workflow: workflowDirectToMain}
	if _, ok := direct.wardEnv()["WARD_WORKFLOW"]; ok {
		t.Error("direct-to-main plan must NOT export WARD_WORKFLOW")
	}
	if strings.Contains(strings.Join(direct.labels(), " "), labelWorkflow) {
		t.Error("direct-to-main plan must NOT carry a ward.workflow label")
	}
	// The zero value behaves like the pull-request default.
	if got := (upPlan{Repo: repo}).wardEnv()["WARD_WORKFLOW"]; got != "pull-request" {
		t.Errorf("a plan with no workflow set WARD_WORKFLOW = %q, want pull-request", got)
	}

	branchOnly := upPlan{Role: roleEngineer, Mode: modeClaude, Repo: repo, Issue: 508, Workflow: workflowRemoteBranchOnly}
	if got := branchOnly.wardEnv()["WARD_WORKFLOW"]; got != "remote-branch-only" {
		t.Errorf("remote-branch-only plan WARD_WORKFLOW = %q, want remote-branch-only", got)
	}
	if !strings.Contains(strings.Join(branchOnly.labels(), " "), labelWorkflow+"=remote-branch-only") {
		t.Errorf("remote-branch-only plan should carry %s=remote-branch-only, got %v", labelWorkflow, branchOnly.labels())
	}
	prMerge := upPlan{Role: roleEngineer, Mode: modeClaude, Repo: repo, Issue: 508, Workflow: workflowPullRequestAndMerge}
	if got := prMerge.wardEnv()["WARD_WORKFLOW"]; got != "pull-request-and-merge" {
		t.Errorf("pull-request-and-merge plan WARD_WORKFLOW = %q, want pull-request-and-merge", got)
	}
	if !strings.Contains(strings.Join(prMerge.labels(), " "), labelWorkflow+"=pull-request-and-merge") {
		t.Errorf("pull-request-and-merge plan should carry %s=pull-request-and-merge, got %v", labelWorkflow, prMerge.labels())
	}
}

func TestAgentWorkflowSmartDefaults(t *testing.T) {
	dir := t.TempDir()
	body := `smart-defaults {
    agent-workflow default="direct-to-main" {
        repo "coilyco-flight-deck/ward" workflow="pull-request"
    }
}`
	if err := os.WriteFile(filepath.Join(dir, bundleDefaultsKDLPath), []byte(body), 0o644); err != nil {
		t.Fatalf("write defaults bundle: %v", err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+dir)

	cmd := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", "coilyco-flight-deck/agentic-os#1"})
	wf, err := agentWorkflow(cmd, "coilyco-flight-deck/agentic-os")
	if err != nil {
		t.Fatalf("agentWorkflow default: %v", err)
	}
	if wf != workflowDirectToMain {
		t.Errorf("default workflow = %q, want direct-to-main", wf)
	}

	wf, err = agentWorkflow(cmd, "coilyco-flight-deck/ward")
	if err != nil {
		t.Fatalf("agentWorkflow repo override: %v", err)
	}
	if wf != workflowPullRequest {
		t.Errorf("repo override workflow = %q, want pull-request", wf)
	}

	cli := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", "coilyco-flight-deck/ward#1", "--workflow", "remote-branch-only"})
	wf, err = agentWorkflow(cli, "coilyco-flight-deck/ward")
	if err != nil {
		t.Fatalf("agentWorkflow CLI override: %v", err)
	}
	if wf != workflowRemoteBranchOnly {
		t.Errorf("CLI workflow = %q, want remote-branch-only", wf)
	}
}

func TestSkipPreflightPropagatesSmokeGateSkip(t *testing.T) {
	repo := targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}
	plan := upPlan{Role: roleEngineer, Mode: modeCodex, Repo: repo, Issue: 703, SkipPreflight: true}
	if got := plan.wardEnv()["WARD_SMOKE_TEST_SKIP"]; got != "1" {
		t.Errorf("skip-preflight plan WARD_SMOKE_TEST_SKIP = %q, want 1", got)
	}
}

// TestReapEnvReadsWorkflow: the reaper picks WARD_WORKFLOW off the container env so
// its main-push guard sees the run's landing policy (ward#508).
func TestReapEnvReadsWorkflow(t *testing.T) {
	t.Setenv("WARD_TARGET_OWNER", "coilyco-flight-deck")
	t.Setenv("WARD_TARGET_NAME", "ward")
	t.Setenv("WARD_FORGEJO_BASE", "https://forgejo.example")
	t.Setenv("WARD_WORKFLOW", "remote-branch-only")
	env, err := readReapEnv()
	if err != nil {
		t.Fatalf("readReapEnv: %v", err)
	}
	if env.Workflow != workflowRemoteBranchOnly {
		t.Errorf("reapEnv.Workflow = %q, want remote-branch-only", env.Workflow)
	}
	if env.Workflow.landsOnMain() {
		t.Error("a remote-branch-only reap must not be allowed to land on main")
	}
}
