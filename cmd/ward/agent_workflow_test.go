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
	for _, want := range []string{"pull request", "closes o/r#12", "paragraph or two", "small bullet list", "observe its CI/checks", "director is encouraged to merge it later"} {
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
	for _, want := range []string{"merge request", "closes o/r#12", "paragraph or two", "small bullet list", "observe its CI/checks", "director is encouraged to merge it later"} {
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

func TestEngineerPRSeedsSealLiveCIIteration(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1587}
	for _, tc := range []struct {
		name       string
		workflow   workflowMode
		reviewGate bool
	}{
		{name: "direct workflow with review gate", workflow: workflowDirectToMain, reviewGate: true},
		{name: "pull request", workflow: workflowPullRequest},
		{name: "pull request with review gate", workflow: workflowPullRequest, reviewGate: true},
		{name: "director merge lane", workflow: workflowPullRequestAndMerge},
		{name: "director merge lane with review gate", workflow: workflowPullRequestAndMerge, reviewGate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := agentSeedPromptWorkflow(ref, "seal live CI", "keep the worker local", "", true, nil, tc.workflow, tc.reviewGate, "")
			for _, want := range []string{
				"live CI as read-only evidence",
				"at most one corrective push",
				"only when local repository evidence proves the change",
				"separate `interactive` issue",
				"exact run",
				"first actionable error",
				"local proof state",
				"operator verification request",
				"Director and Ops retain live remediation authority",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("engineer seed missing sealed CI boundary %q\n%s", want, got)
				}
			}
			assertSeedRejectsLiveCIPushLoop(t, got)
		})
	}
}

func assertSeedRejectsLiveCIPushLoop(t *testing.T, seed string) {
	t.Helper()
	for _, sentence := range strings.FieldsFunc(strings.ToLower(seed), func(r rune) bool {
		switch r {
		case '.', '\n', '!', '?':
			return true
		default:
			return false
		}
	}) {
		if !strings.Contains(sentence, "push") {
			continue
		}
		for _, loop := range []string{"repeat", "retry", "iterate", "keep pushing", "push again", "until"} {
			if strings.Contains(sentence, loop) {
				t.Errorf("seed combines a push with live-iteration wording %q\n%s", loop, seed)
			}
		}
	}
}

// TestWorkflowCarryClausePullRequestAndMerge proves the merge-authorized lane keeps
// the PR flow and says the run is not done until the merge lands.
func TestWorkflowCarryClausePullRequestAndMerge(t *testing.T) {
	got := workflowCarryClause(agentIssueRef{Owner: "o", Repo: "r", Number: 17}, workflowPullRequestAndMerge)
	for _, want := range []string{"pull request", "closes o/r#17", "paragraph or two", "small bullet list", directorMergeWorkflowMarker, "director-merge authorized", pullRequestWorkflowOutcomeMarker} {
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
	for _, want := range []string{"remote-branch-only", "push the branch to origin", "do not open a pull request", "do not write a `closes o/r#99`"} {
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
	if !strings.Contains(pr, pullRequestWorkflowOutcomeMarker) {
		t.Errorf("pull-request reflection should end with the PR URL marker, not done\n got: %s", pr)
	}
	if strings.Contains(pr, "WARD-WORKFLOW: done") {
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
	if !strings.Contains(merge, pullRequestWorkflowOutcomeMarker) {
		t.Errorf("pull-request-and-merge seed should start with the PR URL marker\n got: %s", merge)
	}
	if !strings.Contains(merge, "director merge authorization: reviewed-and-ready") {
		t.Errorf("pull-request-and-merge seed should carry the reviewed-and-ready authorization line\n got: %s", merge)
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
	if !strings.Contains(prMerge, pullRequestWorkflowOutcomeMarker) {
		t.Errorf("pull-request-and-merge reflection should end with the PR URL marker, not done\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "the engineer's final visible outcome is `WARD-WORKFLOW: <fully-qualified pull request link>`") {
		t.Errorf("pull-request-and-merge reflection should announce the PR URL outcome before done\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "director merge authorization: reviewed-and-ready") {
		t.Errorf("pull-request-and-merge reflection should carry the reviewed-and-ready authorization line\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "the pull request is reviewed and merge-ready") {
		t.Errorf("pull-request-and-merge reflection should require merge-ready before done\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "workflow: pull-request-and-merge; review summary: <summary or skip state>") {
		t.Errorf("pull-request-and-merge reflection should use the canonical workflow token in the machine-readable line\n got: %s", prMerge)
	}
	if strings.Contains(prMerge, "WARD-WORKFLOW: done") {
		t.Errorf("pull-request-and-merge reflection must not ask the engineer to post done\n got: %s", prMerge)
	}
	if !strings.Contains(prMerge, "skip the PR comment") {
		t.Errorf("pull-request-and-merge reflection should tell the worker to skip PR comments when no PR exists\n got: %s", prMerge)
	}

	skipped := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPullRequestAndMerge, false, "review gate skipped by ~/.ward/config.yaml default")
	if !strings.Contains(skipped, pullRequestWorkflowOutcomeMarker) {
		t.Errorf("skipped review must keep the PR URL marker, not claim merge-ready\n got: %s", skipped)
	}
	if !strings.Contains(skipped, "workflow: pull-request-and-merge; review summary: <summary or skip state>") {
		t.Errorf("skipped review should still use the canonical machine-readable workflow token\n got: %s", skipped)
	}
	if strings.Contains(skipped, "director merge authorization: reviewed-and-ready") {
		t.Errorf("skipped review should not claim reviewed-and-ready authorization\n got: %s", skipped)
	}
	if !strings.Contains(skipped, "review gate skipped by ~/.ward/config.yaml default") {
		t.Errorf("skipped review should name the skip reason explicitly\n got: %s", skipped)
	}
	if !strings.Contains(skipped, "the pull request is open and the review gate was intentionally skipped") {
		t.Errorf("skipped review should change the landing phrase away from merge-ready\n got: %s", skipped)
	}
	prMergeSkipped := agentSeedPromptWorkflow(ref, "reframe ward", "do it", "", true, nil, workflowPullRequestAndMerge, false, "")
	if strings.Contains(prMergeSkipped, "pending brokered QA") {
		t.Errorf("pull-request-and-merge skipped review must not claim brokered QA is pending\n got: %s", prMergeSkipped)
	}
	if !strings.Contains(prMergeSkipped, "QA is a separate, opt-in exact-commit verification role") {
		t.Errorf("pull-request-and-merge skipped review should describe the exact-commit QA role\n got: %s", prMergeSkipped)
	}
	if !strings.Contains(prMergeSkipped, "the pull request is open and the review gate was intentionally skipped") {
		t.Errorf("pull-request-and-merge skipped review should keep the skip landing phrase\n got: %s", prMergeSkipped)
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

func TestAgentSeedPromptWorkflowUsesCarriedIssueRefForPRContinuations(t *testing.T) {
	prRef := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1293, Forge: forgeForgejo, MergeRequest: true}
	carryRef := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1224, Forge: forgeForgejo}
	got := agentSeedPromptWorkflowWithCarry(prRef, carryRef, "repair the replacement PR", "work it", "", true, nil, workflowPullRequestAndMerge, true, "")
	for _, want := range []string{
		"closes coilyco-flight-deck/ward#1224",
		"Carried issue number: 1224.",
		"ward.workflow: pull-request-and-merge",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("PR continuation prompt missing %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "closes coilyco-flight-deck/ward#1293") {
		t.Fatalf("PR continuation prompt must not close the PR number itself\n%s", got)
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

// TestAgentWorkflowPrecedence covers agentWorkflow's order: an explicit
// --workflow beats a declared repo lane, which beats the built-in default.
func TestAgentWorkflowPrecedence(t *testing.T) {
	// Empty dir on purpose. applyRepoRuntimeConfig walks up for
	// .ward/ward.yaml, and reading ward's own lane broke this (ward#1652).
	t.Chdir(t.TempDir())

	cmd := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", "coilyco-flight-deck/sample-tooling#1"})
	wf, err := agentWorkflow(cmd, "coilyco-flight-deck/sample-tooling")
	if err != nil {
		t.Fatalf("agentWorkflow default: %v", err)
	}
	if wf != workflowDirectToMain {
		t.Errorf("default workflow = %q, want %q", wf, workflowDirectToMain)
	}

	wf, err = agentWorkflow(cmd, "coilyco-flight-deck/ward")
	if err != nil {
		t.Fatalf("agentWorkflow repo override: %v", err)
	}
	if wf != workflowDirectToMain {
		t.Errorf("unmapped repo workflow = %q, want the built-in %q", wf, workflowDirectToMain)
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

// TestAgentWorkflowRepoConfigOverridesDefault covers the layer ward#1652
// exposed, using its own fixture rather than ward's committed ward.yaml.
func TestAgentWorkflowRepoConfigOverridesDefault(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".ward"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "commands: {}\nagent:\n  workflow: pull-request-and-merge\n"
	if err := os.WriteFile(filepath.Join(repoRoot, ".ward", "ward.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repoRoot)

	cmd := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", "coilyco-flight-deck/sample-tooling#1"})
	wf, err := agentWorkflow(cmd, "coilyco-flight-deck/sample-tooling")
	if err != nil {
		t.Fatalf("agentWorkflow with repo config: %v", err)
	}
	if wf != workflowPullRequestAndMerge {
		t.Errorf("workflow = %q, want the repository-declared %q", wf, workflowPullRequestAndMerge)
	}

	// An explicit flag still outranks the repository declaration.
	cli := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", "coilyco-flight-deck/sample-tooling#1", "--workflow", "remote-branch-only"})
	wf, err = agentWorkflow(cli, "coilyco-flight-deck/sample-tooling")
	if err != nil {
		t.Fatalf("agentWorkflow CLI over repo config: %v", err)
	}
	if wf != workflowRemoteBranchOnly {
		t.Errorf("CLI over repo config = %q, want remote-branch-only", wf)
	}
}

func TestSmokeTestBypassesPropagateToSmokeGate(t *testing.T) {
	repo := targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}
	for _, plan := range []upPlan{
		{Role: roleEngineer, Mode: modeCodex, Repo: repo, Issue: 703, SkipPreflight: true},
		{Role: roleEngineer, Mode: modeCodex, Repo: repo, Issue: 703, SkipSmokeTest: true},
	} {
		if got := plan.wardEnv()["WARD_SMOKE_TEST_SKIP"]; got != "1" {
			t.Errorf("bypass plan WARD_SMOKE_TEST_SKIP = %q, want 1", got)
		}
	}
	plain := upPlan{Role: roleEngineer, Mode: modeCodex, Repo: repo, Issue: 703}
	if _, ok := plain.wardEnv()["WARD_SMOKE_TEST_SKIP"]; ok {
		t.Error("plain plan unexpectedly skipped the smoke test")
	}
}

func TestSmokeTestSkippedAcceptsFlagAndEnvironment(t *testing.T) {
	t.Setenv("WARD_SMOKE_TEST_SKIP", "")
	flagged := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", "owner/repo#1", "--skip-smoke-test"})
	if !smokeTestSkipped(flagged) {
		t.Fatal("--skip-smoke-test was not recognized")
	}

	t.Setenv("WARD_SMOKE_TEST_SKIP", "1")
	plain := parseCommandForTest(t, agentSurfaceFlags(), []string{"engineer", "owner/repo#1"})
	if !smokeTestSkipped(plain) {
		t.Fatal("WARD_SMOKE_TEST_SKIP=1 was not recognized")
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
