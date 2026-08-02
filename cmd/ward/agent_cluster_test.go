package main

import (
	"strings"
	"testing"
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
