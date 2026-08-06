package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintAgentPlanShowsAgentProxyAndCorrelation(t *testing.T) {
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{"engineer", "coilyco-flight-deck/ward#861", "--harness", "opencode", "--print"})
	var out bytes.Buffer
	cmd.Root().Writer = &out

	plan := upPlan{
		Mode:        modeOpencode,
		Role:        roleEngineer,
		Repo:        targetRepo{Owner: "coilyco-flight-deck", Name: "ward"},
		Name:        "engineer-opencode-ward-861",
		Branch:      "issue-861",
		Issue:       861,
		Workflow:    workflowPullRequestAndMerge,
		WardVersion: "1.2.3",
		ExtraRepos:  []targetRepo{{Owner: "coilyco-gaming", Name: ".github"}},
		ConfigEnv:   map[string]string{"WARD_OLLAMA_URL": "http://host.docker.internal:8082/v1"},
	}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 861}
	if err := printAgentPlan(cmd, plan, ref, "route agent-proxy", "seed text", "engineer"); err != nil {
		t.Fatalf("printAgentPlan: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"PLAN ONLY - no launch was accepted",
		"agent-proxy: http://host.docker.internal:8082/v1",
		"WARD_RUN_ID=engineer-opencode-ward-861",
		"WARD_HARNESS=opencode",
		"WARD_ISSUE_REF=coilyco-flight-deck/ward#861",
		"WARD_WORKFLOW=pull-request-and-merge",
		"WARD_VERSION=1.2.3",
		"coilyco-flight-deck/ward -> /workspace/ward",
		"coilyco-gaming/.github -> /workspace/coilyco-gaming/.github",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("printAgentPlan output missing %q\n---\n%s", want, got)
		}
	}
}

func TestLocalAgentPrintSkipsLaunchAdmissionAndStaging(t *testing.T) {
	setTestHome(t, t.TempDir())
	staging := filepath.Join(t.TempDir(), "launch-staging")
	t.Setenv(envStagingDir, staging)
	t.Setenv("WARD_GIT_NAME", "Ward Test")
	t.Setenv("WARD_GIT_EMAIL", "ward-test@example.invalid")
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{
		"engineer", "coilyco-flight-deck/ward#1593", "--harness", "codex", "--print", "--skip-review",
	})
	var out bytes.Buffer
	cmd.Root().Writer = &out
	w := resolvedWork{
		Ref:      agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1593},
		Title:    "Make brokered print a real preview",
		Seed:     "synthetic seed",
		Workflow: workflowDirectToMain,
	}
	if err := (&Runner{}).launchAgentContainer(t.Context(), cmd, modeCodex, "engineer", w, "", preflightOutcome{}, ""); err != nil {
		t.Fatalf("local print: %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("local print created launch staging at %s", staging)
	}
	got := out.String()
	for _, want := range []string{"PLAN ONLY - no launch was accepted", previewContainerAssetsDir} {
		if !strings.Contains(got, want) {
			t.Fatalf("local print output missing %q\n---\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"broker Ward launch started", "launch accepted", "startup is pending"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("local print output contains launch language %q\n---\n%s", forbidden, got)
		}
	}
}
