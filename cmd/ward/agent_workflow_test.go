package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseWorkflow covers the --workflow parse gate (ward#508).
// It covers the spelled-out modes, the pr alias, and the unknown-value error.
func TestParseWorkflow(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want workflowMode
	}{
		{"", workflowDirectMain},
		{"direct-main", workflowDirectMain},
		{"pull-requests", workflowPullRequests},
		{"pull-requests-and-merge", workflowPullRequestsAndMerge},
		{"pr", workflowPullRequests},
		{"patch-only", workflowPatchOnly},
		{"  pull-requests  ", workflowPullRequests},
	} {
		got, err := parseWorkflow(tc.in)
		if err != nil {
			t.Errorf("parseWorkflow(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseWorkflow(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := parseWorkflow("merge-everything"); err == nil {
		t.Fatal("parseWorkflow accepted an unknown mode")
	} else if !strings.Contains(err.Error(), "direct-main|pull-requests|pull-requests-and-merge|patch-only") {
		t.Errorf("unknown-mode error should list the choices, got %v", err)
	}
}

// TestWorkflowLandsOnMain pins the reaper-facing predicate.
// Only direct-main, including the empty default, may push/merge main.
func TestWorkflowLandsOnMain(t *testing.T) {
	if !workflowDirectMain.landsOnMain() {
		t.Error("direct-main must land on main")
	}
	if !workflowMode("").landsOnMain() || workflowPullRequests.landsOnMain() || workflowPullRequestsAndMerge.landsOnMain() || workflowPatchOnly.landsOnMain() {
		t.Error("default/direct-main-only modes/patch-only must NOT land on main")
	}
}

// TestWorkflowUsesReviewGate pins the landing gate split: the autonomous PR lane
// and direct-main keep the review gate; the human-gated PR lane does not.
func TestWorkflowUsesReviewGate(t *testing.T) {
	if !workflowDirectMain.workflowUsesReviewGate() || !workflowPullRequestsAndMerge.workflowUsesReviewGate() {
		t.Error("direct-main and pull-requests-and-merge must use the review gate")
	}
	if workflowPullRequests.workflowUsesReviewGate() || workflowPatchOnly.workflowUsesReviewGate() {
		t.Error("pull-requests and patch-only must not use the review gate")
	}
}

// TestWorkflowCarryClauseDirectMain: the default is byte-for-byte the forge clause,
// so direct-main preserves existing behavior on both forges (ward#508 acceptance).
func TestWorkflowCarryClauseDirectMain(t *testing.T) {
	for _, ref := range []agentIssueRef{
		{Owner: "o", Repo: "r", Number: 7},                     // Forgejo
		{Owner: "o", Repo: "r", Number: 7, Forge: forgeGitHub}, // GitHub
	} {
		if got, want := workflowCarryClause(ref, workflowDirectMain), forgeCarryClause(ref); got != want {
			t.Errorf("direct-main carry clause diverged from the forge clause:\n got: %s\nwant: %s", got, want)
		}
	}
}

// TestWorkflowCarryClausePullRequests: the human-gated lane forces a pull request
// and stops there on both forges (ward#508).
func TestWorkflowCarryClausePullRequests(t *testing.T) {
	fj := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 12}, workflowPullRequests)
	for _, want := range []string{"pull request", "closes #12", "Do NOT push to `main` directly", "pull-requests"} {
		if !strings.Contains(fj, want) {
			t.Errorf("pull-requests Forgejo carry clause missing %q\n got: %s", want, fj)
		}
	}
	gh := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 12, Forge: forgeGitHub}, workflowPullRequests)
	if !strings.Contains(gh, "gh pr create") {
		t.Errorf("pull-requests GitHub carry clause should keep the native gh pr flow\n got: %s", gh)
	}
}

// TestWorkflowCarryClausePullRequestsAndMerge: the autonomous PR lane opens a
// pull request, then merges it with a merge commit after CI and review pass.
func TestWorkflowCarryClausePullRequestsAndMerge(t *testing.T) {
	fj := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 12}, workflowPullRequestsAndMerge)
	for _, want := range []string{"pull request", "closes #12", "merge it with a merge commit"} {
		if !strings.Contains(fj, want) {
			t.Errorf("pull-requests-and-merge Forgejo carry clause missing %q\n got: %s", want, fj)
		}
	}
	gh := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 12, Forge: forgeGitHub}, workflowPullRequestsAndMerge)
	for _, want := range []string{"gh pr create", "merge it with a merge commit"} {
		if !strings.Contains(gh, want) {
			t.Errorf("pull-requests-and-merge GitHub carry clause missing %q\n got: %s", want, gh)
		}
	}
}

// TestWorkflowCarryClausePatchOnly: patch-only produces a patch and lands nothing -
// no push, no merge, no closing trailer (ward#508).
func TestWorkflowCarryClausePatchOnly(t *testing.T) {
	got := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 99}, workflowPatchOnly)
	for _, want := range []string{"no landing authority", "git format-patch", "do NOT push", "Do not write a `closes #99`"} {
		if !strings.Contains(got, want) {
			t.Errorf("patch-only carry clause missing %q\n got: %s", want, got)
		}
	}
}

// TestAgentSeedPromptWorkflow ties the whole seam together: the seed's carry clause
// AND the reflection's landing phrase both shift with the mode (ward#508).
func TestAgentSeedPromptWorkflow(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 508}

	direct := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowDirectMain, true, "")
	if !strings.Contains(direct, "merge to main, push") {
		t.Errorf("direct-main seed should carry the merge-to-main clause\n got: %s", direct)
	}
	if !strings.Contains(direct, "the work is committed, merged to main, and pushed") {
		t.Errorf("direct-main reflection should name the merge-and-push landing\n got: %s", direct)
	}

	pr := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPullRequests, true, "")
	if !strings.Contains(pr, "pull request") || strings.Contains(pr, "merge to main, push - and close") {
		t.Errorf("pull-requests seed should carry a PR clause, not the merge-to-main fast path\n got: %s", pr)
	}
	if !strings.Contains(pr, "the branch is pushed and the pull request opened") {
		t.Errorf("pull-requests reflection should name the branch+PR landing\n got: %s", pr)
	}
	if !strings.Contains(pr, "workflow stops at PR open") {
		t.Errorf("pull-requests seed should say the review gate does not run\n got: %s", pr)
	}
	if workflowCarryClause(ref, "") != workflowCarryClause(ref, workflowDirectMain) {
		t.Error("empty workflow should resolve to the direct-main carry clause")
	}

	merge := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPullRequestsAndMerge, true, "")
	if !strings.Contains(merge, "merge it with a merge commit") {
		t.Errorf("pull-requests-and-merge seed should carry the merge-after-review clause\n got: %s", merge)
	}
	if !strings.Contains(merge, "the branch is pushed, the pull request is opened, the checks go green, the review gate passes, and the merge commit lands") {
		t.Errorf("pull-requests-and-merge reflection should name the autonomous PR landing\n got: %s", merge)
	}

	patch := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPatchOnly, true, "")
	if !strings.Contains(patch, "no landing authority") {
		t.Errorf("patch-only seed should say it has no landing authority\n got: %s", patch)
	}
	if !strings.Contains(patch, "the patch is produced and posted as a comment") {
		t.Errorf("patch-only reflection should name the patch landing\n got: %s", patch)
	}

	// The plain agentSeedPrompt wrapper follows the baked default.
	if agentSeedPrompt(ref, "reframe ward", "do it", "", true, nil) != direct {
		t.Error("agentSeedPrompt should equal agentSeedPromptWorkflow(..., direct-main)")
	}
}

// TestWorkflowEnvAndLabels pins the container plumbing: a non-default workflow rides
// WARD_WORKFLOW + a ward.workflow label; direct-main leaves both untouched (ward#508).
func TestWorkflowEnvAndLabels(t *testing.T) {
	repo := targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}

	direct := upPlan{Role: roleEngineer, Mode: modeClaude, Repo: repo, Issue: 508, Workflow: workflowDirectMain}
	if _, ok := direct.wardEnv()["WARD_WORKFLOW"]; ok {
		t.Error("direct-main plan must NOT export WARD_WORKFLOW")
	}
	if strings.Contains(strings.Join(direct.labels(), " "), labelWorkflow) {
		t.Error("direct-main plan must NOT carry a ward.workflow label")
	}
	if _, ok := (upPlan{Repo: repo}).wardEnv()["WARD_WORKFLOW"]; ok {
		t.Error("a plan with no workflow set must not export WARD_WORKFLOW")
	}

	pr := upPlan{Role: roleEngineer, Mode: modeClaude, Repo: repo, Issue: 508, Workflow: workflowPullRequests}
	if got := pr.wardEnv()["WARD_WORKFLOW"]; got != "pull-requests" {
		t.Errorf("pull-requests plan WARD_WORKFLOW = %q, want pull-requests", got)
	}
	if !strings.Contains(strings.Join(pr.labels(), " "), labelWorkflow+"=pull-requests") {
		t.Errorf("pull-requests plan should carry %s=pull-requests, got %v", labelWorkflow, pr.labels())
	}

	merge := upPlan{Role: roleEngineer, Mode: modeClaude, Repo: repo, Issue: 508, Workflow: workflowPullRequestsAndMerge}
	if got := merge.wardEnv()["WARD_WORKFLOW"]; got != "pull-requests-and-merge" {
		t.Errorf("pull-requests-and-merge plan WARD_WORKFLOW = %q, want pull-requests-and-merge", got)
	}
	if !strings.Contains(strings.Join(merge.labels(), " "), labelWorkflow+"=pull-requests-and-merge") {
		t.Errorf("pull-requests-and-merge plan should carry %s=pull-requests-and-merge, got %v", labelWorkflow, merge.labels())
	}

	patch := upPlan{Role: roleEngineer, Mode: modeClaude, Repo: repo, Issue: 508, Workflow: workflowPatchOnly}
	if got := patch.wardEnv()["WARD_WORKFLOW"]; got != "patch-only" {
		t.Errorf("patch-only plan WARD_WORKFLOW = %q, want patch-only", got)
	}
	if !strings.Contains(strings.Join(patch.labels(), " "), labelWorkflow+"=patch-only") {
		t.Errorf("patch-only plan should carry %s=patch-only, got %v", labelWorkflow, patch.labels())
	}
}

func TestAgentWorkflowSmartDefaults(t *testing.T) {
	dir := t.TempDir()
	body := `smart-defaults {
    agent-workflow default="direct-main" {
        repo "coilyco-flight-deck/ward" workflow="pull-requests-and-merge"
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
	if wf != workflowDirectMain {
		t.Errorf("default workflow = %q, want direct-main", wf)
	}

	wf, err = agentWorkflow(cmd, "coilyco-flight-deck/ward")
	if err != nil {
		t.Fatalf("agentWorkflow repo override: %v", err)
	}
	if wf != workflowPullRequestsAndMerge {
		t.Errorf("repo override workflow = %q, want pull-requests-and-merge", wf)
	}

	cli := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", "coilyco-flight-deck/ward#1", "--workflow", "pull-requests"})
	wf, err = agentWorkflow(cli, "coilyco-flight-deck/ward")
	if err != nil {
		t.Fatalf("agentWorkflow CLI override: %v", err)
	}
	if wf != workflowPullRequests {
		t.Errorf("CLI workflow = %q, want pull-requests", wf)
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
	t.Setenv("WARD_WORKFLOW", "patch-only")
	env, err := readReapEnv()
	if err != nil {
		t.Fatalf("readReapEnv: %v", err)
	}
	if env.Workflow != workflowPatchOnly {
		t.Errorf("reapEnv.Workflow = %q, want patch-only", env.Workflow)
	}
	if env.Workflow.landsOnMain() {
		t.Error("a patch-only reap must not be allowed to land on main")
	}
}
