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
		{"", workflowDirectToMain},
		{"direct-main", workflowDirectToMain},
		{"direct-to-main", workflowDirectToMain},
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
	} else if !strings.Contains(err.Error(), "merge-remote-main|pull-request|pull-request-and-merge|remote-branch-only") {
		t.Errorf("unknown-mode error should list the choices, got %v", err)
	}
}

// TestWorkflowLandsOnMain pins the reaper-facing predicate: only merge-remote-main may
// push/merge main. The empty default now resolves to merge-remote-main.
func TestWorkflowLandsOnMain(t *testing.T) {
	if !workflowDirectToMain.landsOnMain() {
		t.Error("merge-remote-main must land on main")
	}
	if !workflowMode("").landsOnMain() || workflowPullRequest.landsOnMain() || workflowPullRequestAndMerge.landsOnMain() || workflowRemoteBranchOnly.landsOnMain() {
		t.Error("default/merge-remote-main must land on main; pull-request/pull-request-and-merge/remote-branch-only must NOT")
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
	for _, want := range []string{"pull request", "closes #12", "paragraph or two", "small bullet list", "watching its CI/checks", "director is encouraged to merge it later"} {
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
		t.Fatalf("pull-request carry clause regressed to the merge-remote-main fast path:\n%s", got)
	}
}

// TestWorkflowCarryClauseGitLabMR checks the same lane speaks in merge-request
// terms when the target forge is GitLab.
func TestWorkflowCarryClauseGitLabMR(t *testing.T) {
	got := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 12, Forge: forgeGitLab}, workflowPullRequest)
	for _, want := range []string{"merge request", "closes #12", "paragraph or two", "small bullet list", "watching its CI/checks", "director is encouraged to merge it later"} {
		if !strings.Contains(got, want) {
			t.Errorf("gitlab pull-request carry clause missing %q\n got: %s", want, got)
		}
	}
	if strings.Contains(got, "pull request") {
		t.Errorf("gitlab carry clause should not use pull request wording\n got: %s", got)
	}
	if !strings.Contains(got, "skip the merge request comment") {
		t.Errorf("gitlab carry clause should steer failure comments to merge requests\n got: %s", got)
	}
}

// TestWorkflowCarryClausePullRequestAndMerge proves the merge-authorized lane keeps
// the PR flow and says the run is not done until the merge lands.
func TestWorkflowCarryClausePullRequestAndMerge(t *testing.T) {
	got := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 17}, workflowPullRequestAndMerge)
	for _, want := range []string{"pull request", "closes #17", "paragraph or two", "small bullet list", directorMergeWorkflowMarker, "director-merge authorized", "WARD-OUTCOME: merge-ready"} {
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
		t.Fatalf("pull-request-and-merge carry clause regressed to the merge-remote-main fast path:\n%s", got)
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
		t.Errorf("merge-remote-main seed should carry the merge-to-main clause\n got: %s", direct)
	}
	if !strings.Contains(direct, "the work is committed, merged to main, and pushed") {
		t.Errorf("merge-remote-main reflection should name the merge-and-push landing\n got: %s", direct)
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
	gl := agentSeedPromptWorkflow(agentIssueRef{Owner: "group", Repo: "proj", Number: 12, Forge: forgeGitLab, MergeRequest: true}, "reframe ward", "do it", "", true, nil, workflowPullRequest, true, "")
	if !strings.Contains(gl, "merge request") || strings.Contains(gl, "pull request") {
		t.Errorf("gitlab seed should use merge request wording\n got: %s", gl)
	}
	merge := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPullRequestAndMerge, true, "")
	if !strings.Contains(merge, "director-merge authorized") {
		t.Errorf("pull-request-and-merge seed should mark the PR as director-merge authorized\n got: %s", merge)
	}
	if !strings.Contains(merge, directorMergeWorkflowMarker) {
		t.Errorf("pull-request-and-merge seed should carry the PR-body marker\n got: %s", merge)
	}
	if !strings.Contains(merge, "workflow: pull-request-and-merge; review summary: <summary or skip state>") {
		t.Errorf("headless reflection should include the canonical machine-readable workflow/review line\n got: %s", merge)
	}
	if workflowCarryClause(ref, "") != workflowCarryClause(ref, workflowDirectToMain) {
		t.Error("empty workflow should resolve to the merge-remote-main carry clause")
	}
	prMerge := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPullRequestAndMerge, true, "")
	if !strings.Contains(prMerge, "director-merge authorized") {
		t.Errorf("pull-request-and-merge seed should carry the director merge lane\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "WARD-OUTCOME: merge-ready") {
		t.Errorf("pull-request-and-merge reflection should end with merge-ready, not done\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "the engineer's final visible outcome is `WARD-OUTCOME: merge-ready`") {
		t.Errorf("pull-request-and-merge reflection should announce merge-ready before done\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "the pull request is reviewed and merge-ready") {
		t.Errorf("pull-request-and-merge reflection should require merge-ready before done\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "workflow: pull-request-and-merge; review summary: <summary or skip state>") {
		t.Errorf("pull-request-and-merge reflection should use the canonical workflow token in the machine-readable line\n got: %s", prMerge)
	}
	if strings.Contains(prMerge, "WARD-OUTCOME: done") {
		t.Errorf("pull-request-and-merge reflection must not ask the engineer to post done\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "skip the PR comment") {
		t.Errorf("pull-request-and-merge reflection should tell the worker to skip PR comments when no PR exists\n got: %s", prMerge)
	}

	skipped := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPullRequestAndMerge, false, "review gate skipped by ~/.ward/config.yaml default")
	if !strings.Contains(skipped, "WARD-OUTCOME: submitted") {
		t.Errorf("skipped review must not claim merge-ready\n got: %s", skipped)
	}
	if !strings.Contains(skipped, "workflow: pull-request-and-merge; review summary: <summary or skip state>") {
		t.Errorf("skipped review should still use the canonical machine-readable workflow token\n got: %s", skipped)
	}
	if !strings.Contains(skipped, "review gate skipped by ~/.ward/config.yaml default") {
		t.Errorf("skipped review should name the skip reason explicitly\n got: %s", skipped)
	}
	if !strings.Contains(skipped, "the pull request is open and the review gate was intentionally skipped") {
		t.Errorf("skipped review should change the landing phrase away from merge-ready\n got: %s", skipped)
	}

	branchOnly := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowRemoteBranchOnly, true, "")
	if !strings.Contains(branchOnly, "remote-branch-only") {
		t.Errorf("remote-branch-only seed should say it has no PR or merge authority\n got: %s", branchOnly)
	}
	if !strings.Contains(branchOnly, "the remote branch is pushed") {
		t.Errorf("remote-branch-only reflection should name the branch landing\n got: %s", branchOnly)
	}
	if strings.Contains(branchOnly, "paragraph or two") || strings.Contains(branchOnly, "small bullet list") {
		t.Errorf("remote-branch-only reflection must not ask for a PR description\n got: %s", branchOnly)
	}
	if strings.Contains(branchOnly, "post the same actionable failure comment to both the linked issue and the PR") {
		t.Errorf("remote-branch-only reflection must not ask for PR comments when no PR exists\n got: %s", branchOnly)
	}

	// The plain agentSeedPrompt wrapper follows the merge-remote-main default.
	if agentSeedPrompt(ref, "reframe ward", "do it", "", true, nil) != direct {
		t.Error("agentSeedPrompt should equal agentSeedPromptWorkflow(..., merge-remote-main)")
	}
}

// TestWorkflowEnvAndLabels pins the container plumbing for WARD_WORKFLOW
// and ward.workflow labels; merge-remote-main leaves both untouched (ward#508).
func TestWorkflowEnvAndLabels(t *testing.T) {
	repo := targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}

	direct := upPlan{Role: roleEngineer, Mode: modeClaude, Repo: repo, Issue: 508, Workflow: workflowDirectToMain}
	if _, ok := direct.wardEnv()["WARD_WORKFLOW"]; ok {
		t.Error("merge-remote-main plan must NOT export WARD_WORKFLOW")
	}
	if strings.Contains(strings.Join(direct.labels(), " "), labelWorkflow) {
		t.Error("merge-remote-main plan must NOT carry a ward.workflow label")
	}
	// The zero value behaves like the merge-remote-main default.
	if got := (upPlan{Repo: repo}).wardEnv()["WARD_WORKFLOW"]; got != "" {
		t.Errorf("a plan with no workflow set WARD_WORKFLOW = %q, want empty", got)
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
	defaultsBody := `defaults {
    agent-reservation-ttl "3h"
}
workflow default="merge-remote-main" {
    repo "coilyco-flight-deck/ward" workflow="pull-request-and-merge"
}
`
	reposBody := `repos {
    repo-authority default=forgejo {
        trusted-owner "coilysiren"
        trusted-owner "coilyco-flight-deck"
        repo "coilysiren/*" forge=github
        repo "coilyco-flight-deck/*" forge=forgejo
    }
}`
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureDefaultsPath), []byte(defaultsBody), 0o644); err != nil {
		t.Fatalf("write defaults bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundleFixtureReposPath), []byte(reposBody), 0o644); err != nil {
		t.Fatalf("write repos bundle: %v", err)
	}
	t.Setenv(wardConfigRefEnv, "file://"+dir)

	cmd := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", "coilyco-flight-deck/agentic-os#1"})
	wf, err := agentWorkflow(cmd, "coilyco-flight-deck/agentic-os")
	if err != nil {
		t.Fatalf("agentWorkflow default: %v", err)
	}
	if wf != workflowDirectToMain {
		t.Errorf("default workflow = %q, want merge-remote-main", wf)
	}

	wf, err = agentWorkflow(cmd, "coilyco-flight-deck/ward")
	if err != nil {
		t.Fatalf("agentWorkflow repo override: %v", err)
	}
	if wf != workflowPullRequestAndMerge {
		t.Errorf("repo override workflow = %q, want pull-request-and-merge", wf)
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

func TestAgentWorkflowRejectsBadConfigRef(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "not-a-resolvable-ref")

	cmd := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", "coilyco-flight-deck/agentic-os#1"})
	if _, err := agentWorkflow(cmd, "coilyco-flight-deck/agentic-os"); err == nil {
		t.Fatal("agentWorkflow with bad ref: want a loud config-source error")
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
