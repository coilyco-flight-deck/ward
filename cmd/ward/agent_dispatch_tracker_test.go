package main

import (
	"context"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/pkg/broker"
)

func signedMachineRecord(body string) string {
	return modeCodex.signBody(body)
}

func TestTrackerMutationRoleAndRecordKindBoundary(t *testing.T) {
	ref := "coilyco-flight-deck/ward#1586"
	for _, tc := range []struct {
		name string
		req  dispatchBrokerRequest
		ok   bool
	}{
		{
			name: "qa verdict",
			req: dispatchBrokerRequest{AuthenticatedRole: roleQA, Tracker: &trackerMutationRequest{
				Operation: trackerMutationComment, RecordKind: recordKindQA, Target: ref,
				Body: signedMachineRecord("WARD-WORKFLOW: qa-pass"),
			}},
			ok: true,
		},
		{
			name: "qa cannot mint outcome",
			req: dispatchBrokerRequest{AuthenticatedRole: roleQA, Tracker: &trackerMutationRequest{
				Operation: trackerMutationComment, RecordKind: recordKindOutcome, Target: ref,
				Body: signedMachineRecord("WARD-WORKFLOW: done"),
			}},
		},
		{
			name: "engineer outcome",
			req: dispatchBrokerRequest{AuthenticatedRole: roleEngineer, Tracker: &trackerMutationRequest{
				Operation: trackerMutationComment, RecordKind: recordKindOutcome, Target: ref,
				Body: signedMachineRecord("WARD-WORKFLOW: done"),
			}},
			ok: true,
		},
		{
			name: "engineer cannot approve",
			req: dispatchBrokerRequest{AuthenticatedRole: roleEngineer, Tracker: &trackerMutationRequest{
				Operation: trackerMutationComment, RecordKind: recordKindApprovalSnapshot, Target: ref,
				Body: signedMachineRecord("WARD-APPROVAL: v1"),
			}},
		},
		{
			name: "body kind mismatch",
			req: dispatchBrokerRequest{AuthenticatedRole: roleEngineer, Tracker: &trackerMutationRequest{
				Operation: trackerMutationComment, RecordKind: recordKindReview, Target: ref,
				Body: signedMachineRecord("WARD-WORKFLOW: done"),
			}},
		},
		{
			name: "missing authenticated role",
			req: dispatchBrokerRequest{Role: roleDirector, Tracker: &trackerMutationRequest{
				Operation: trackerMutationComment, RecordKind: recordKindOutcome, Target: ref,
				Body: signedMachineRecord("WARD-WORKFLOW: done"),
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.Action = dispatchActionTrackerMutation
			err := validateDispatchBrokerTrackerMutation(tc.req)
			if tc.ok && err != nil {
				t.Fatalf("validateDispatchBrokerTrackerMutation: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("validateDispatchBrokerTrackerMutation unexpectedly succeeded")
			}
		})
	}
}

func TestChildBrokerCapabilityBindsRoleAndReadSurface(t *testing.T) {
	const master = "master-secret"
	capability := dispatchBrokerAgentCapability(master, "engineer-codex-ward-1586", roleEngineer)
	identity, role, ok := authenticateDispatchBrokerCaller(capability, master, "director", roleDirector)
	if !ok || identity != "engineer-codex-ward-1586" || role != roleEngineer {
		t.Fatalf("authenticated capability = %q %q %t", identity, role, ok)
	}
	if _, _, ok := authenticateDispatchBrokerCaller(dispatchBrokerAgentCapability(master, "engineer-codex-ward-1586", roleQA), master, "director", roleDirector); !ok {
		t.Fatal("independently signed QA capability did not authenticate")
	}
	parts := strings.Split(capability, ":")
	parts[1] = roleQA
	if _, _, ok := authenticateDispatchBrokerCaller(strings.Join(parts, ":"), master, "director", roleDirector); ok {
		t.Fatal("role-swapped capability authenticated")
	}
	get := dispatchBrokerRequest{Action: dispatchActionForgejo, Forgejo: &nativeForgejoRequest{Method: "GET"}}
	post := dispatchBrokerRequest{Action: dispatchActionForgejo, Forgejo: &nativeForgejoRequest{Method: "POST"}}
	if !dispatchBrokerChildActionAllowed(get) || dispatchBrokerChildActionAllowed(post) {
		t.Fatalf("child raw Forgejo boundary: GET=%t POST=%t", dispatchBrokerChildActionAllowed(get), dispatchBrokerChildActionAllowed(post))
	}
	if !dispatchBrokerChildActionAllowed(dispatchBrokerRequest{Action: dispatchActionTrackerMutation}) {
		t.Fatal("child typed tracker action was not routed to its role gate")
	}
}

func TestEngineerAndQATokenResolutionFailsClosed(t *testing.T) {
	t.Setenv("FORGEJO_TOKEN", "broad-token")
	t.Setenv(envForgejoGitToken, "")
	r := &Runner{}
	if _, err := r.resolveContainerToken(context.Background(), broker.Target{}, forgeForgejo, roleEngineer); err == nil || !strings.Contains(err.Error(), "Git-only") {
		t.Fatalf("missing Git-only token error = %v", err)
	}
	t.Setenv(envForgejoGitToken, "broad-token")
	if _, err := r.resolveContainerToken(context.Background(), broker.Target{}, forgeForgejo, roleQA); err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("shared broad token error = %v", err)
	}
	t.Setenv(envForgejoGitToken, "git-only-token")
	if got, err := r.resolveContainerToken(context.Background(), broker.Target{}, forgeForgejo, roleEngineer); err != nil || got != "git-only-token" {
		t.Fatalf("Git-only token = %q, %v", got, err)
	}
	if got, err := r.resolveContainerToken(context.Background(), broker.Target{}, forgeForgejo, roleDirector); err != nil || got != "broad-token" {
		t.Fatalf("director token = %q, %v", got, err)
	}
	if _, err := r.resolveContainerToken(context.Background(), broker.Target{}, forgeGitHub, roleEngineer); err == nil || !strings.Contains(err.Error(), "role-authenticated tracker broker") {
		t.Fatalf("non-Forgejo engineer boundary error = %v", err)
	}
}

func TestActorIdentityInputsCrossWardEnvironment(t *testing.T) {
	t.Setenv(envTrustedCollaborators, "kai,maintainer")
	t.Setenv(envAutomationActor, "ward-bot")
	env := (upPlan{}).wardEnv()
	if env[envTrustedCollaborators] != "kai,maintainer" || env[envAutomationActor] != "ward-bot" {
		t.Fatalf("actor identity env = %#v", env)
	}
	if _, ok := env[envForgejoGitToken]; ok {
		t.Fatal("secret Git-only token leaked into printable Ward environment")
	}
}
