package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
	"github.com/urfave/cli/v3"
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
		Mounts: []mountSpec{
			{Source: containerGitcacheVol, Target: containerGitcacheMnt, Volume: true},
		},
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
		"volume create " + containerGitcacheVol,
		"compose -p codex-ab45 -f " + stack.ComposePath + " up -d --wait broker",
	} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("Docker calls missing %q\n%s", want, calls)
		}
	}
	if strings.Index(string(calls), "volume create "+containerGitcacheVol) >
		strings.Index(string(calls), " up -d --wait broker") {
		t.Fatalf("external volume was created after Compose startup\n%s", calls)
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
	for _, want := range []string{"{{.State}}", "{{.Status}}", clusterDockerRowSentinel} {
		if !strings.Contains(args, want) {
			t.Fatalf("cluster query missing %q: %q", want, args)
		}
	}
	listArgs := strings.Join(clusterDockerListArgs("", true), " ")
	if !strings.Contains(listArgs, "label=ward.cluster") || !strings.Contains(listArgs, "label=ward.role=broker") {
		t.Fatalf("cluster list query = %q", listArgs)
	}
}

func TestClusterDockerRowsReportLifecycleStateAndHealth(t *testing.T) {
	rows := []struct {
		name, row, state, status, health string
	}{
		{
			name:   "restarting with exit code",
			row:    "codex-ab45-broker\tRestarting\tRestarting (1) 8 seconds ago\tcodex-ab45\tbroker\tcodex\t\tward-cluster-row",
			state:  "restarting",
			status: "Restarting (1) 8 seconds ago",
		},
		{name: "created", row: "codex-ab45-broker\tcreated\tCreated\tcodex-ab45\tbroker\tcodex\t\tward-cluster-row", state: "created", status: "Created"},
		{name: "healthy", row: "codex-ab45-broker\trunning\tUp 10 seconds (healthy)\tcodex-ab45\tbroker\tcodex\t\tward-cluster-row", state: "running", status: "Up 10 seconds (healthy)", health: "healthy"},
		{name: "unhealthy", row: "codex-ab45-broker\trunning\tUp 10 seconds (unhealthy)\tcodex-ab45\tbroker\tcodex\t\tward-cluster-row", state: "running", status: "Up 10 seconds (unhealthy)", health: "unhealthy"},
		{name: "health starting", row: "codex-ab45-broker\trunning\tUp 2 seconds (health: starting)\tcodex-ab45\tbroker\tcodex\t\tward-cluster-row", state: "running", status: "Up 2 seconds (health: starting)", health: "starting"},
		{name: "removing", row: "codex-ab45-broker\tremoving\tRemoval In Progress\tcodex-ab45\tbroker\tcodex\t\tward-cluster-row", state: "removing", status: "Removal In Progress"},
		{name: "paused", row: "codex-ab45-broker\tpaused\tUp 1 minute (Paused)\tcodex-ab45\tbroker\tcodex\t\tward-cluster-row", state: "paused", status: "Up 1 minute (Paused)"},
		{name: "exited", row: "codex-ab45-broker\texited\tExited (1) 3 seconds ago\tcodex-ab45\tbroker\tcodex\t\tward-cluster-row", state: "exited", status: "Exited (1) 3 seconds ago"},
		{name: "dead", row: "codex-ab45-broker\tdead\tDead\tcodex-ab45\tbroker\tcodex\t\tward-cluster-row", state: "dead", status: "Dead"},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseClusterDockerRow(tc.row)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != tc.state || got.Status != tc.status || got.Health != tc.health {
				t.Fatalf("parsed row = %+v, want state=%q status=%q health=%q", got, tc.state, tc.status, tc.health)
			}
			if got.PeerID != "" {
				t.Fatalf("broker peer id = %q, want empty", got.PeerID)
			}
		})
	}
}

func TestClusterDockerRowsFailClosedWithContext(t *testing.T) {
	for _, row := range []string{
		"missing-fields",
		"codex-ab45-broker\tflying\tFlying\tcodex-ab45\tbroker\tcodex\t\tward-cluster-row",
		"codex-ab45-broker\trunning\tUp 1 second\tbad-cluster\tbroker\tcodex\t\tward-cluster-row",
		"\trunning\tUp 1 second\tcodex-ab45\tbroker\tcodex\t\tward-cluster-row",
		"codex-ab45-broker\trunning\tUp 1 second\tcodex-ab45\tbroker\tcodex\t\twrong-sentinel",
	} {
		if _, err := parseClusterDockerRow(row); err == nil || !strings.Contains(err.Error(), strconv.Quote(row)) {
			t.Fatalf("parse error for %q = %v, want contextual rejection", row, err)
		}
	}
}

func TestClusterLifecycleDiagnosesAndCleansUpFailedBrokerStartup(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	stack, err := resolveDirectorStack("codex-ab45")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stack.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stack.ComposePath, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dockerLog := filepath.Join(t.TempDir(), "docker.log")
	dockerPath := filepath.Join(t.TempDir(), "docker")
	writeTestShellCommand(t, dockerPath, `#!/bin/sh
printf '%s\n' "$*" >> "$WARD_TEST_DOCKER_LOG"
case "$1" in
  ps)
    printf '%b\n' 'codex-ab45-broker\tRestarting\tRestarting (1) 8 seconds ago\tcodex-ab45\tbroker\tcodex\t\tward-cluster-row'
    printf '%b\n' 'codex-ab45-critic\texited\tExited (1) 3 seconds ago\tcodex-ab45\tcritic\tcodex\tcritic-cd67\tward-cluster-row'
    ;;
  logs)
    printf '%s\n' 'broker startup failed'
    ;;
esac
`)
	var dockerOut, dockerErr bytes.Buffer
	r := &Runner{Runner: &shell.Runner{
		Stdout: &dockerOut,
		Stderr: &dockerErr,
		Env:    []string{"WARD_TEST_DOCKER_LOG=" + dockerLog},
		Resolve: func(string) (string, error) {
			return dockerPath, nil
		},
	}}
	var statusOut bytes.Buffer
	statusCmd := &cli.Command{
		Name:   "status",
		Flags:  []cli.Flag{&cli.BoolFlag{Name: "json"}},
		Writer: &statusOut,
		Action: func(ctx context.Context, c *cli.Command) error { return r.runClusterStatus(ctx, c) },
	}
	if err := statusCmd.Run(t.Context(), []string{"status", "--json", "codex-ab45"}); err != nil {
		t.Fatalf("status failed broker diagnosis: %v", err)
	}
	var status []collaborationClusterContainer
	if err := json.Unmarshal(statusOut.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, statusOut.String())
	}
	if len(status) != 2 || status[0].State != "restarting" || status[1].State != "exited" {
		t.Fatalf("status = %+v, want restarting broker and exited peer", status)
	}
	if status[0].Status != "Restarting (1) 8 seconds ago" || status[0].Health != "" {
		t.Fatalf("restarting broker status = %+v", status[0])
	}
	logsCmd := &cli.Command{
		Name:   "logs",
		Flags:  []cli.Flag{&cli.IntFlag{Name: "tail", Value: 100}},
		Action: func(ctx context.Context, c *cli.Command) error { return r.runClusterLogs(ctx, c) },
	}
	if err := logsCmd.Run(t.Context(), []string{"logs", "--tail", "25", "codex-ab45"}); err != nil {
		t.Fatalf("logs failed broker diagnosis: %v", err)
	}
	var stopOut bytes.Buffer
	stopCmd := &cli.Command{
		Name:   "stop",
		Flags:  []cli.Flag{&cli.BoolFlag{Name: "print"}},
		Writer: &stopOut,
		Action: func(ctx context.Context, c *cli.Command) error { return r.runClusterStop(ctx, c) },
	}
	if err := stopCmd.Run(t.Context(), []string{"stop", "codex-ab45"}); err != nil {
		t.Fatalf("stop failed broker cleanup: %v", err)
	}
	if stopOut.String() != "codex-ab45\n" {
		t.Fatalf("stop output = %q", stopOut.String())
	}
	if _, err := os.Stat(stack.Dir); !os.IsNotExist(err) {
		t.Fatalf("cluster state still exists after stop: %v", err)
	}
	calls, err := os.ReadFile(dockerLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ps -a --filter label=ward.cluster=codex-ab45",
		"logs --tail 25 codex-ab45-broker",
		"logs --tail 25 codex-ab45-critic",
		"stop --timeout 30 codex-ab45-broker codex-ab45-critic",
		"rm codex-ab45-broker codex-ab45-critic",
		"compose -p codex-ab45 -f " + stack.ComposePath + " down --remove-orphans",
	} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("Docker calls missing %q\n%s", want, calls)
		}
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
