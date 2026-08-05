package main

import (
	"strings"
	"testing"
)

func TestAgentFrameworkGenericBrokerLaunchAcceptsArbitraryRole(t *testing.T) {
	req := dispatchBrokerRequest{
		Role:    "story-architect",
		AgentID: "architect",
		Argv:    []string{"run", "--role", "story-architect", "--agent-id", "architect", "shape the premise"},
	}
	if err := validateDispatchBrokerLaunch(req); err != nil {
		t.Fatalf("validate generic launch: %v", err)
	}
	req.Role = "StoryArchitect"
	if err := validateDispatchBrokerLaunch(req); err == nil {
		t.Fatal("expected unsafe role to fail")
	}
}

func TestAgentFrameworkGenericBrokerLaunchBindsRoleAndAgentID(t *testing.T) {
	req := dispatchBrokerRequest{
		Role:    "critic",
		AgentID: "critic-one",
		Argv:    []string{"run", "--role", "engineer", "--agent-id", "critic-one", "review this"},
	}
	if err := validateDispatchBrokerLaunch(req); err == nil {
		t.Fatal("accepted mismatched role metadata")
	}
	req.Argv = []string{"run", "--role", "critic", "--agent-id", "critic-two", "review this"}
	if err := validateDispatchBrokerLaunch(req); err == nil {
		t.Fatal("accepted mismatched agent identity")
	}
}

func TestAgentFrameworkBrokerCallerCapabilityStampsAgentIdentity(t *testing.T) {
	const master = "master-capability"
	capability := dispatchBrokerAgentCapability(master, "critic.one", "critic")
	identity, role, ok := authenticateDispatchBrokerCaller(capability, master, "director", roleDirector)
	if !ok || identity != "critic.one" || role != "critic" {
		t.Fatalf("authenticate child = %q, %q, %t", identity, role, ok)
	}
	if _, _, ok := authenticateDispatchBrokerCaller(capability+"x", master, "director", roleDirector); ok {
		t.Fatal("accepted modified capability")
	}
}

func TestAgentFrameworkChildCapabilityCannotSelectFixedWorkflow(t *testing.T) {
	if dispatchBrokerChildActionAllowed(dispatchBrokerRequest{
		Role: roleEngineer,
		Argv: []string{"engineer", "coilyco-flight-deck/ward#1"},
	}) {
		t.Fatal("child capability selected the engineer workflow")
	}
	if !dispatchBrokerChildActionAllowed(dispatchBrokerRequest{
		Role: "critic",
		Argv: []string{"run", "--role", "critic", "review this"},
	}) {
		t.Fatal("child capability could not launch a generic peer")
	}
}

func TestAgentFrameworkBrokerMessageRoundTripFiltersRecipient(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	brokerID := "test-broker"
	first := dispatchBrokerMessage{ID: newDispatchBrokerRequestID(), From: "architect", To: "critic", Body: "pressure-test this"}
	second := dispatchBrokerMessage{ID: newDispatchBrokerRequestID(), From: "architect", To: "editor", Body: "tighten this"}
	if err := appendDispatchBrokerMessage(brokerID, first); err != nil {
		t.Fatal(err)
	}
	if err := appendDispatchBrokerMessage(brokerID, second); err != nil {
		t.Fatal(err)
	}
	got, err := readDispatchBrokerMessages(brokerID, "critic", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Body != first.Body || got[0].From != "architect" {
		t.Fatalf("messages = %#v", got)
	}
}

func TestAgentFrameworkBrokerChildPlanInheritsPeerCapability(t *testing.T) {
	t.Setenv(envChildBrokerAddr, "broker:7420")
	t.Setenv(envChildBrokerCapability, "critic.signature")
	t.Setenv(envChildBrokerNetwork, "ward-director_default")
	t.Setenv(envChildAgentID, "critic")
	t.Setenv(envClusterID, "codex-ab45")
	command := parseCommandForTest(t, agentRunCommand().Flags, []string{
		"work", "--role", "critic", "--repo", "coilyco-flight-deck/ward",
	})
	plan, err := buildUpPlan(command, targetRepo{Owner: "coilyco-flight-deck", Name: "ward"}, modeCodex, "critic", t.TempDir(), t.TempDir(), []string{"work"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.AgentID != "critic" || plan.DispatchBrokerAddr != "broker:7420" ||
		plan.DispatchBrokerNetwork != "ward-director_default" || plan.ClusterID != "codex-ab45" {
		t.Fatalf("broker child plan = %#v", plan)
	}
	if !strings.Contains(strings.Join(plan.labels(), " "), labelCluster+"=codex-ab45") {
		t.Fatalf("broker child labels = %v", plan.labels())
	}
}

func TestAgentFrameworkHostComposedRunJoinsMatchingBroker(t *testing.T) {
	plan := upPlan{AgentID: "architect"}
	stack := directorStack{Project: "codex-ab45"}
	applyDispatchBrokerAttachment(&plan, stack, "architect.signature")
	if plan.DispatchBrokerAddr != dispatchBrokerServiceAddress ||
		plan.DispatchBrokerToken != "architect.signature" ||
		plan.DispatchBrokerNetwork != "codex-ab45_default" || plan.ClusterID != "codex-ab45" {
		t.Fatalf("attached plan = %#v", plan)
	}
}

func TestAgentFrameworkGenericForwardedLineDoesNotTreatRoleFlagAsIssue(t *testing.T) {
	got := dispatchBrokerForwardedLine(
		[]string{"run", "--role", "critic", "--agent-id", "critic-one", "review this"},
		"/tmp/dispatch",
	)
	if strings.Contains(got, "logs --role") || !strings.Contains(got, "generic peer startup is pending") {
		t.Fatalf("forwarded line = %q", got)
	}
}
