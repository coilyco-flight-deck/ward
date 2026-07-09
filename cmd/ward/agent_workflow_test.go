package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseWorkflow covers the --workflow parse gate (ward#508).
// It checks the known modes, the default, and the aliases.
func TestParseWorkflow(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want workflowMode
	}{
		{"", workflowPR},
		{"direct-main", workflowDirectMain},
		{"pr", workflowPR},
		{"pull-requests", workflowPullRequests},
		{"pull-requests-and-merge", workflowPullRequestsAndMerge},
		{"patch-only", workflowPatchOnly},
		{"  pr  ", workflowPR},
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
	} else if !strings.Contains(err.Error(), "direct-main|pr|pull-requests|pull-requests-and-merge|patch-only") {
		t.Errorf("unknown-mode error should list the choices, got %v", err)
	}
}

// TestWorkflowLandsOnMain pins the reaper-facing predicate: only direct-main may
// push/merge main. The empty default now resolves to pr.
func TestWorkflowLandsOnMain(t *testing.T) {
	if !workflowDirectMain.landsOnMain() {
		t.Error("direct-main must land on main")
	}
	if workflowMode("").landsOnMain() || workflowPR.landsOnMain() || workflowPullRequests.landsOnMain() || workflowPullRequestsAndMerge.landsOnMain() || workflowPatchOnly.landsOnMain() {
		t.Error("default/pr/pull-requests/pull-requests-and-merge/patch-only must NOT land on main")
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

// TestWorkflowCarryClausePR checks the PR carry clause on Forgejo and GitHub.
// It also covers the director merge marker.
func TestWorkflowCarryClausePR(t *testing.T) {
	fj := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 12}, workflowPR)
	for _, want := range []string{"pull request", "closes #12", "watching its CI/checks", "failing check is not a done state"} {
		if !strings.Contains(fj, want) {
			t.Errorf("pr Forgejo carry clause missing %q\n got: %s", want, fj)
		}
	}
	if strings.Contains(fj, directorMergeWorkflowMarker) {
		t.Errorf("pr Forgejo carry clause must not carry the director marker\n got: %s", fj)
	}
	gh := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 12, Forge: forgeGitHub}, workflowPR)
	for _, want := range []string{"gh pr create", "watching its CI/checks", "final `WARD-OUTCOME: done` comment is not allowed until the PR is green"} {
		if !strings.Contains(gh, want) {
			t.Errorf("pr GitHub carry clause missing %q\n got: %s", want, gh)
		}
	}
	if !strings.Contains(gh, "gh pr create") {
		t.Errorf("pr GitHub carry clause should keep the native gh pr flow\n got: %s", gh)
	}
	merge := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 12}, workflowPRAndMerge)
	for _, want := range []string{"director-merge authorized", "watching its CI/checks", "failing check is not a done state"} {
		if !strings.Contains(merge, want) {
			t.Errorf("pull-requests-and-merge carry clause missing %q\n got: %s", want, merge)
		}
	}
	if !strings.Contains(merge, "director-merge authorized") {
		t.Errorf("pull-requests-and-merge carry clause must name the director merge lane\n got: %s", merge)
	}
}

// TestWorkflowCarryClausePullRequestsAndMerge proves the long-form merge-authorized
// lane keeps the PR flow and names the director merge policy in the prompt.
func TestWorkflowCarryClausePullRequestsAndMerge(t *testing.T) {
	got := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 17}, workflowPullRequestsAndMerge)
	for _, want := range []string{"pull request", "closes #17", directorMergeWorkflowMarker, "director-merge authorized"} {
		if !strings.Contains(got, want) {
			t.Errorf("pull-requests-and-merge carry clause missing %q\n got: %s", want, got)
		}
	}
	if got := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 17, Forge: forgeGitHub}, workflowPullRequestsAndMerge); !strings.Contains(got, "gh pr create") || !strings.Contains(got, directorMergeWorkflowMarker) {
		t.Errorf("pull-requests-and-merge GitHub carry clause must keep the PR body marker\n got: %s", got)
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

	pr := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPR, true, "")
	if !strings.Contains(pr, "pull request") || strings.Contains(pr, "merge to main, push - and close") {
		t.Errorf("pr seed should carry a PR clause, not the merge-to-main fast path\n got: %s", pr)
	}
	if !strings.Contains(pr, "the branch is pushed, the pull request is open, and the required checks are green") {
		t.Errorf("pr reflection should require green checks before done\n got: %s", pr)
	}
	merge := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPullRequestsAndMerge, true, "")
	if !strings.Contains(merge, "director-merge authorized") {
		t.Errorf("pull-requests-and-merge seed should mark the PR as director-merge authorized\n got: %s", merge)
	}
	if !strings.Contains(merge, directorMergeWorkflowMarker) {
		t.Errorf("pull-requests-and-merge seed should carry the PR-body marker\n got: %s", merge)
	}
	if !strings.Contains(merge, "workflow: <mode>; review summary: <summary or skip state>") {
		t.Errorf("headless reflection should include the machine-readable workflow/review line\n got: %s", merge)
	}
	if workflowCarryClause(ref, "") != workflowCarryClause(ref, workflowPR) {
		t.Error("empty workflow should resolve to the pr carry clause")
	}
	prMerge := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPRAndMerge, true, "")
	if !strings.Contains(prMerge, "director-merge authorized") {
		t.Errorf("pull-requests-and-merge seed should carry the director merge lane\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, directorMergeWorkflowMarker) {
		t.Errorf("pull-requests-and-merge seed should carry the PR-body marker\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "the branch is pushed, the pull request is open, and the required checks are green") {
		t.Errorf("pull-requests-and-merge reflection should require green checks before done\n got: %s", prMerge)
	}

	patch := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPatchOnly, true, "")
	if !strings.Contains(patch, "no landing authority") {
		t.Errorf("patch-only seed should say it has no landing authority\n got: %s", patch)
	}
	if !strings.Contains(patch, "the patch is produced and posted as a comment") {
		t.Errorf("patch-only reflection should name the patch landing\n got: %s", patch)
	}

	// The plain agentSeedPrompt wrapper follows the safe pr default.
	if agentSeedPrompt(ref, "reframe ward", "do it", "", true, nil) != pr {
		t.Error("agentSeedPrompt should equal agentSeedPromptWorkflow(..., pr)")
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
	// The zero value behaves like the pr default.
	if got := (upPlan{Repo: repo}).wardEnv()["WARD_WORKFLOW"]; got != "pr" {
		t.Errorf("a plan with no workflow set WARD_WORKFLOW = %q, want pr", got)
	}

	patch := upPlan{Role: roleEngineer, Mode: modeClaude, Repo: repo, Issue: 508, Workflow: workflowPatchOnly}
	if got := patch.wardEnv()["WARD_WORKFLOW"]; got != "patch-only" {
		t.Errorf("patch-only plan WARD_WORKFLOW = %q, want patch-only", got)
	}
	if !strings.Contains(strings.Join(patch.labels(), " "), labelWorkflow+"=patch-only") {
		t.Errorf("patch-only plan should carry %s=patch-only, got %v", labelWorkflow, patch.labels())
	}
	prMerge := upPlan{Role: roleEngineer, Mode: modeClaude, Repo: repo, Issue: 508, Workflow: workflowPRAndMerge}
	if got := prMerge.wardEnv()["WARD_WORKFLOW"]; got != "pull-requests-and-merge" {
		t.Errorf("pull-requests-and-merge plan WARD_WORKFLOW = %q, want pull-requests-and-merge", got)
	}
	if !strings.Contains(strings.Join(prMerge.labels(), " "), labelWorkflow+"=pull-requests-and-merge") {
		t.Errorf("pull-requests-and-merge plan should carry %s=pull-requests-and-merge, got %v", labelWorkflow, prMerge.labels())
	}
}

func TestAgentWorkflowSmartDefaults(t *testing.T) {
	dir := t.TempDir()
	body := `smart-defaults {
    agent-workflow default="direct-main" {
        repo "coilyco-flight-deck/ward" workflow="pull-requests"
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
	if wf != workflowPullRequests {
		t.Errorf("repo override workflow = %q, want pr", wf)
	}

	cli := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", "coilyco-flight-deck/ward#1", "--workflow", "patch-only"})
	wf, err = agentWorkflow(cli, "coilyco-flight-deck/ward")
	if err != nil {
		t.Fatalf("agentWorkflow CLI override: %v", err)
	}
	if wf != workflowPatchOnly {
		t.Errorf("CLI workflow = %q, want patch-only", wf)
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
