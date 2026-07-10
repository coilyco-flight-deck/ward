package main

import (
	"bytes"
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
		ConfigEnv:   map[string]string{"WARD_OLLAMA_URL": "http://host.docker.internal:8082/v1"},
	}
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 861}
	if err := printAgentPlan(cmd, plan, ref, "route agent-proxy", "seed text", "engineer"); err != nil {
		t.Fatalf("printAgentPlan: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"agent-proxy: http://host.docker.internal:8082/v1",
		"WARD_RUN_ID=engineer-opencode-ward-861",
		"WARD_HARNESS=opencode",
		"WARD_ISSUE_REF=coilyco-flight-deck/ward#861",
		"WARD_WORKFLOW=pull-requests-and-merge",
		"WARD_VERSION=1.2.3",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("printAgentPlan output missing %q\n---\n%s", want, got)
		}
	}
}
