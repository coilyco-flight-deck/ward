package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"gopkg.in/yaml.v3"
)

func TestBrokerOnlyClusterComposeHasNoRepositoryOrDirector(t *testing.T) {
	plan := upPlan{
		Image:       "example.invalid/ward:test",
		Name:        "codex-ab45-broker",
		Role:        "broker",
		Mode:        modeCodex,
		ClusterID:   "codex-ab45",
		ForgejoBase: forgejoBaseURL,
		Mounts: []mountSpec{
			{Source: "/tmp/assets", Target: containerWardAssets, ReadOnly: true},
			dockerSockMount(),
		},
	}
	stack := directorStack{Project: "codex-ab45", BrokerName: "codex-ab45-broker"}
	body, err := renderBrokerOnlyStackCompose(plan, stack, "/tmp/broker.env", "/tmp/ward-state")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"broker:",
		"container_name: codex-ab45-broker",
		"restart: unless-stopped",
		"WARD_CLUSTER_ID: codex-ab45",
		"ward.cluster: codex-ab45",
		"ward.role: broker",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("broker-only Compose output missing %q\n%s", want, text)
		}
	}
	for _, forbidden := range []string{
		"director:",
		"WARD_TARGET_REPO",
		"WARD_TARGET_OWNER",
		"WARD_TARGET_NAME",
		"ward.repo:",
		"FORGEJO_TOKEN",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("broker-only Compose output contains %q\n%s", forbidden, text)
		}
	}
}

func TestBrokerOnlyEntrypointBranchesBeforeRepositoryRequirements(t *testing.T) {
	brokerBranch := strings.Index(containerEntrypointScript, `if [[ "${WARD_CONTAINER_SERVICE:-}" == "dispatch-broker" ]]`)
	repositoryCheck := strings.Index(containerEntrypointScript, `: "${WARD_TARGET_OWNER:?missing WARD_TARGET_OWNER}"`)
	if brokerBranch < 0 || repositoryCheck < 0 {
		t.Fatalf("entrypoint is missing broker branch or repository validation\n%s", containerEntrypointScript)
	}
	if brokerBranch > repositoryCheck {
		t.Fatal("broker-only entrypoint still validates repository target before selecting the broker service")
	}
}

func TestBrokerOnlyClusterRuntimeUsesGeneratedHealthWaitWithoutRepositoryTarget(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	stackDir := filepath.Join(home, "cluster")
	if err := os.MkdirAll(stackDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stack := directorStack{
		Project:     "codex-ab45",
		Dir:         stackDir,
		ComposePath: filepath.Join(stackDir, directorStackFile),
		EnvPath:     filepath.Join(stackDir, directorStackEnvFile),
		BrokerName:  "codex-ab45-broker",
	}
	sourceEnv := filepath.Join(t.TempDir(), "source.env")
	if err := os.WriteFile(sourceEnv, []byte("WARD_CONTAINER_SERVICE=dispatch-broker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dockerLog := filepath.Join(t.TempDir(), "docker.log")
	dockerPath := filepath.Join(t.TempDir(), "docker")
	writeTestShellCommand(t, dockerPath, `#!/bin/sh
printf '%s\n' "$*" >> "$WARD_TEST_DOCKER_LOG"
`)
	r := &Runner{Runner: &shell.Runner{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Env:    []string{"WARD_TEST_DOCKER_LOG=" + dockerLog},
		Resolve: func(string) (string, error) {
			return dockerPath, nil
		},
	}}
	plan := upPlan{
		Image:       "example.invalid/ward:test",
		Name:        stack.BrokerName,
		Role:        "broker",
		Mode:        modeCodex,
		ClusterID:   stack.Project,
		ForgejoBase: forgejoBaseURL,
	}
	if err := r.runBrokerOnlyCluster(context.Background(), plan, stack, sourceEnv); err != nil {
		t.Fatalf("run broker-only cluster: %v", err)
	}
	body, err := os.ReadFile(stack.ComposePath)
	if err != nil {
		t.Fatal(err)
	}
	var doc composeDocument
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decode generated Compose plan: %v", err)
	}
	broker, ok := doc.Services["broker"]
	if !ok || len(doc.Services) != 1 {
		t.Fatalf("generated services = %+v, want broker only", doc.Services)
	}
	if broker.Healthcheck == nil || strings.Join(broker.Healthcheck.Test, " ") != "CMD /usr/local/bin/ward container dispatch-broker-probe" {
		t.Fatalf("broker health check = %+v", broker.Healthcheck)
	}
	if strings.Contains(string(body), "WARD_TARGET_") || strings.Contains(string(body), "FORGEJO_TOKEN") {
		t.Fatalf("broker-only runtime projected repository authority\n%s", body)
	}
	calls, err := os.ReadFile(dockerLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"compose version",
		"compose -p codex-ab45 -f " + stack.ComposePath + " up -d --wait broker",
	} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("Docker calls missing %q\n%s", want, calls)
		}
	}
}

func TestClusterLifecycleQueriesAreClusterScoped(t *testing.T) {
	args := strings.Join(clusterDockerListArgs("codex-ab45", false), " ")
	if !strings.Contains(args, "label=ward.cluster=codex-ab45") {
		t.Fatalf("cluster query = %q", args)
	}
	if strings.Contains(args, "codex-cd67") || strings.Contains(args, labelRepo) {
		t.Fatalf("cluster query crosses identity boundary: %q", args)
	}
	listArgs := strings.Join(clusterDockerListArgs("", true), " ")
	if !strings.Contains(listArgs, "label=ward.cluster") || !strings.Contains(listArgs, "label=ward.role=broker") {
		t.Fatalf("cluster list query = %q", listArgs)
	}
}

func TestClusterArgRejectsRepositoryShapedAndWardPrefixedKeys(t *testing.T) {
	for _, value := range []string{"", "coilyco-flight-deck/ward", "ward-ab45", "codex-ward-ab45"} {
		if _, err := requiredClusterArg("status", value); err == nil {
			t.Fatalf("requiredClusterArg accepted %q", value)
		}
	}
	if got, err := requiredClusterArg("status", "codex-ab45"); err != nil || got != "codex-ab45" {
		t.Fatalf("requiredClusterArg = %q, %v", got, err)
	}
}

func TestClusterStopUsesExactComposeProject(t *testing.T) {
	stack := directorStack{Project: "codex-ab45", ComposePath: "/state/codex-ab45/compose.yaml"}
	commands := directorStackComposeArgs(stack)
	got := strings.Join(commands.BrokerUp, " ")
	if got != "compose -p codex-ab45 -f /state/codex-ab45/compose.yaml up -d --wait broker" {
		t.Fatalf("broker start = %q", got)
	}
}
