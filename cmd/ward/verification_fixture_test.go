package main

import (
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func verificationFixtureTestConfig(t *testing.T) {
	t.Helper()
	writeTestWardGlobalConfig(t, "{}\n")
	root := writeRepoRuntimeConfig(t, `
commands: {}
agent:
  verification:
    fixtures:
      - repository: example/ward-qa-fixture
        issue-label: qa-fixture
`)
	chdirForRuntimeConfigTest(t, root)
}

func TestVerificationFixtureTargetAdmission(t *testing.T) {
	verificationFixtureTestConfig(t)
	cmd := parseCommandForTest(t, []cli.Flag{verificationFixtureFlag()}, []string{"qa", "--verification-fixture"})
	ref := agentIssueRef{Owner: "example", Repo: "ward-qa-fixture", Number: 7}

	if err := validateVerificationFixtureTarget(cmd, ref, &Issue{Labels: []string{"qa-fixture"}}); err != nil {
		t.Fatalf("admitted fixture refused: %v", err)
	}
	if err := validateVerificationFixtureTarget(cmd, ref, &Issue{Labels: []string{"headless"}}); err == nil ||
		!strings.Contains(err.Error(), "requires one configured fixture label") {
		t.Fatalf("missing-label fixture error = %v", err)
	}
	foreign := agentIssueRef{Owner: "example", Repo: "production", Number: 7}
	if err := validateVerificationFixtureTarget(cmd, foreign, &Issue{Labels: []string{"qa-fixture"}}); err == nil ||
		!strings.Contains(err.Error(), "is not admitted") {
		t.Fatalf("non-fixture target error = %v", err)
	}
}

func TestVerificationFixtureForcesRemoteBranchOnly(t *testing.T) {
	verificationFixtureTestConfig(t)
	flags := []cli.Flag{workflowFlag(), verificationFixtureFlag()}
	cmd := parseCommandForTest(t, flags, []string{"engineer", "--verification-fixture"})
	got, err := agentWorkflow(cmd, "example/ward-qa-fixture")
	if err != nil {
		t.Fatalf("resolve fixture workflow: %v", err)
	}
	if got != workflowRemoteBranchOnly {
		t.Fatalf("fixture workflow = %q, want %q", got, workflowRemoteBranchOnly)
	}

	conflict := parseCommandForTest(t, flags, []string{"engineer", "--verification-fixture", "--workflow", "pull-request"})
	if _, err := agentWorkflow(conflict, "example/ward-qa-fixture"); err == nil ||
		!strings.Contains(err.Error(), "requires --workflow remote-branch-only") {
		t.Fatalf("conflicting fixture workflow error = %v", err)
	}
}

func TestVerificationFixtureDirectorDispatchPropagation(t *testing.T) {
	dispatch := dispatchEngineer{harness: modeCodex, verificationFixture: true}
	argv := dispatch.engineerArgv(agentIssueRef{Owner: "example", Repo: "ward-qa-fixture", Number: 9})
	got := strings.Join(argv, " ")
	for _, want := range []string{"--verification-fixture", "--workflow remote-branch-only"} {
		if !strings.Contains(got, want) {
			t.Fatalf("fixture dispatch argv missing %q: %s", want, got)
		}
	}
}

func TestVerificationFixturePlanEvidence(t *testing.T) {
	plan := upPlan{
		Role:                roleEngineer,
		Mode:                modeCodex,
		Repo:                targetRepo{Owner: "example", Name: "ward-qa-fixture"},
		Issue:               9,
		Workflow:            workflowRemoteBranchOnly,
		VerificationFixture: true,
	}
	if got := plan.wardEnv()["WARD_VERIFICATION_FIXTURE"]; got != "1" {
		t.Fatalf("WARD_VERIFICATION_FIXTURE = %q, want 1", got)
	}
	labels := strings.Join(plan.labels(), " ")
	for _, want := range []string{"ward.verification-fixture=true", "ward.workflow=remote-branch-only"} {
		if !strings.Contains(labels, want) {
			t.Fatalf("fixture labels missing %q: %s", want, labels)
		}
	}
}

func TestVerificationFixtureQAPlanChecksOutIssueBranch(t *testing.T) {
	ref := agentIssueRef{Owner: "example", Repo: "ward-qa-fixture", Number: 9}
	plan := qaResearchPlan(upPlan{
		Mode: modeCodex,
		Repo: targetRepo{Owner: ref.Owner, Name: ref.Repo},
	}, ref, true)
	if plan.Branch != "issue-9" {
		t.Fatalf("fixture QA branch = %q, want issue-9", plan.Branch)
	}
}

func TestVerificationFixtureConfigRejectsInvalidRules(t *testing.T) {
	if _, err := normalizeVerificationFixtureRules([]verificationFixtureRule{{
		Repository: "not-a-slug",
		IssueLabel: "qa-fixture",
	}}); err == nil {
		t.Fatal("invalid fixture repository accepted")
	}
	if _, err := normalizeVerificationFixtureRules([]verificationFixtureRule{{
		Repository: "example/fixture",
	}}); err == nil {
		t.Fatal("empty fixture issue label accepted")
	}
}
