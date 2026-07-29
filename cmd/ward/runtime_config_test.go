package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli/v3"
)

func chdirForRuntimeConfigTest(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

func writeRepoRuntimeConfig(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".ward")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create .ward: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ward.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write repository runtime config: %v", err)
	}
	return root
}

func TestRuntimeConfigUnconfiguredLaunchUsesTypedDefaults(t *testing.T) {
	writeTestWardGlobalConfig(t, "{}\n")
	chdirForRuntimeConfigTest(t, t.TempDir())

	cfg, err := loadLaunchConfig()
	if err != nil {
		t.Fatalf("load unconfigured launch: %v", err)
	}
	if cfg.DefaultAgent != string(modeClaude) {
		t.Fatalf("default harness = %q, want claude", cfg.DefaultAgent)
	}
	for _, name := range frontierAgentNames() {
		agent := launchAgentByName(cfg, name)
		if agent.Name == "" || agent.Binary == "" || len(agent.Argv.Headless) == 0 {
			t.Fatalf("typed adapter %q is incomplete: %#v", name, agent)
		}
		if agent.Model != "" || agent.Effort != "" {
			t.Fatalf("typed adapter %q owns model policy: %#v", name, agent)
		}
	}
}

func TestRuntimeConfigPrecedence(t *testing.T) {
	writeTestWardGlobalConfig(t, `
default-harness: goose
agent:
  image: operator.example/ward
  release-channel: operator
  workflow:
    default: pull-request
    repositories:
      owner/repo: remote-branch-only
director:
  max-parallel: 4
`)
	root := writeRepoRuntimeConfig(t, `
commands: {}
agent:
  workflow: pull-request-and-merge
  image: repo.example/ward
  release-channel: candidate
`)
	chdirForRuntimeConfigTest(t, root)

	cfg, err := loadLaunchConfig()
	if err != nil {
		t.Fatalf("load operator launch preferences: %v", err)
	}
	if cfg.DefaultAgent != string(modeGoose) {
		t.Fatalf("operator default harness = %q, want goose", cfg.DefaultAgent)
	}

	defs, err := currentSmartDefaultsWithError()
	if err != nil {
		t.Fatalf("load runtime defaults: %v", err)
	}
	if defs.agentImage != "repo.example/ward" || defs.agentTag != "candidate" {
		t.Fatalf("repository image = %s:%s, want repo.example/ward:candidate", defs.agentImage, defs.agentTag)
	}
	if defs.agentWorkflowDefault != workflowPullRequestAndMerge {
		t.Fatalf("repository workflow = %q, want %q", defs.agentWorkflowDefault, workflowPullRequestAndMerge)
	}
	if defs.agentWorkflowRepos["owner/repo"] != workflowRemoteBranchOnly {
		t.Fatalf("operator repository workflow disappeared: %#v", defs.agentWorkflowRepos)
	}
	if defs.directorMaxParallel != 4 {
		t.Fatalf("operator max-parallel = %d, want 4", defs.directorMaxParallel)
	}

	var got workflowMode
	cmd := &cli.Command{
		Name: "probe",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "workflow"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			var resolveErr error
			got, resolveErr = agentWorkflow(c, "owner/repo")
			return resolveErr
		},
	}
	if err := cmd.Run(context.Background(), []string{"probe", "--workflow", "merge-remote-main"}); err != nil {
		t.Fatalf("resolve explicit workflow: %v", err)
	}
	if got != workflowDirectToMain {
		t.Fatalf("explicit workflow = %q, want %q", got, workflowDirectToMain)
	}
}

func TestRuntimeConfigRoleMetadataCannotChangeBrokerAuthority(t *testing.T) {
	roles := []string{"", roleDirector, roleEngineer, roleQA, "operator", "external"}
	ops := []prWorkflowOp{prOpStatus, prOpLogs, prOpRuns, prOpRecover, prOpRerun, prOpClose, prOpReopen, prOpMerge}
	for _, op := range ops {
		var baseline bool
		for i, role := range roles {
			permitted := prWorkflowPermitted(role, workflowPullRequestAndMerge, op) == nil
			if i == 0 {
				baseline = permitted
			} else if permitted != baseline {
				t.Errorf("role %q changed permission for %s", role, op)
			}
		}
	}
}
