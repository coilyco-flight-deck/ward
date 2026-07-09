package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseWorkflow covers the --workflow parse gate (ward#508): the three modes,
// empty defaulting to pr, and an unknown value erroring with the choices.
func TestParseWorkflow(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want workflowMode
	}{
		{"", workflowPR},
		{"direct-main", workflowDirectMain},
		{"pr", workflowPR},
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
	} else if !strings.Contains(err.Error(), "direct-main|pr|patch-only") {
		t.Errorf("unknown-mode error should list the choices, got %v", err)
	}
}

// TestWorkflowLandsOnMain pins the reaper-facing predicate: only direct-main may
// push/merge main; the empty default now resolves to pr and never does (ward#707).
func TestWorkflowLandsOnMain(t *testing.T) {
	if !workflowDirectMain.landsOnMain() {
		t.Error("direct-main must land on main")
	}
	if workflowMode("").landsOnMain() || workflowPR.landsOnMain() || workflowPatchOnly.landsOnMain() {
		t.Error("default/pr/patch-only must NOT land on main")
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

// TestWorkflowCarryClausePR: pr forces a pull request on Forgejo (overriding the
// merge-to-main fast path) and keeps GitHub's native branch+PR flow (ward#508).
func TestWorkflowCarryClausePR(t *testing.T) {
	fj := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 12}, workflowPR)
	for _, want := range []string{"pull request", "closes #12", "keep watching its CI/checks", "failing check is not done", "Do NOT push to `main` directly"} {
		if !strings.Contains(fj, want) {
			t.Errorf("pr Forgejo carry clause missing %q\n got: %s", want, fj)
		}
	}
	gh := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 12, Forge: forgeGitHub}, workflowPR)
	if !strings.Contains(gh, "gh pr create") {
		t.Errorf("pr GitHub carry clause should keep the native gh pr flow\n got: %s", gh)
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
	for _, want := range []string{
		"the branch is pushed, the pull request opened, and the required CI checks are green",
		"Keep watching the PR checks after it opens",
		"A failing check is not done",
	} {
		if !strings.Contains(pr, want) {
			t.Errorf("pr seed missing %q\n got: %s", want, pr)
		}
	}
	if workflowCarryClause(ref, "") != workflowCarryClause(ref, workflowPR) {
		t.Error("empty workflow should resolve to the PR carry clause")
	}

	patch := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPatchOnly, true, "")
	if !strings.Contains(patch, "no landing authority") {
		t.Errorf("patch-only seed should say it has no landing authority\n got: %s", patch)
	}
	if !strings.Contains(patch, "the patch is produced and posted as a comment") {
		t.Errorf("patch-only reflection should name the patch landing\n got: %s", patch)
	}

	// The plain agentSeedPrompt wrapper follows the safe PR default.
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
	// The zero value behaves like the PR default.
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
}

func TestAgentWorkflowSmartDefaults(t *testing.T) {
	dir := t.TempDir()
	body := `smart-defaults {
    agent-workflow default="direct-main" {
        repo "coilyco-flight-deck/ward" workflow="pr"
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
	if wf != workflowPR {
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
