package main

import (
	"os"
	"testing"
	"time"
)

func commentBy(login, body string) issueComment {
	c := issueComment{Body: body}
	c.User.Login = login
	return c
}

func machineComment(body string, createdAt ...time.Time) issueComment {
	c := commentBy(os.Getenv(envAutomationActor), body)
	if len(createdAt) > 0 {
		c.CreatedAt = createdAt[0]
	}
	return c
}

func machineCommentID(id int, body string, createdAt time.Time) issueComment {
	c := machineComment(body, createdAt)
	c.ID = id
	return c
}

func TestActorAuthorityPolicyFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, collaborators, automation string
	}{
		{name: "missing collaborators", automation: "ward-bot"},
		{name: "missing automation", collaborators: "kai"},
		{name: "ambiguous identity", collaborators: "kai,ward-bot", automation: "WARD-BOT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := actorAuthorityPolicyFromInputs(tc.collaborators, tc.automation)
			if policy.Err == nil {
				t.Fatalf("policy = %+v, want fail-closed error", policy)
			}
			got := classifyActorCommentWithPolicy(commentBy("ward-bot", "WARD-WORKFLOW: done"), policy)
			if got.Class != actorClassInvalid {
				t.Fatalf("classification = %+v, want invalid", got)
			}
		})
	}
}

func TestActorClassifierBindsIdentityAndRecordKind(t *testing.T) {
	policy := actorAuthorityPolicyFromInputs("kai, maintainer", "ward-bot")
	for _, tc := range []struct {
		name, author, body, class, kind string
		direct                          bool
	}{
		{name: "trusted human prose", author: "Kai", body: "ship it", class: actorClassTrustedHuman, direct: true},
		{name: "trusted human cannot mint", author: "kai", body: "WARD-WORKFLOW: done", class: actorClassTrustedHuman, direct: true},
		{name: "trusted machine outcome", author: "WARD-BOT", body: "WARD-WORKFLOW: done", class: actorClassTrustedMachine, kind: recordKindOutcome, direct: true},
		{name: "trusted machine qa", author: "ward-bot", body: "WARD-WORKFLOW: qa-pass", class: actorClassTrustedMachine, kind: recordKindQA, direct: true},
		{name: "automation prose", author: "ward-bot", body: "ordinary note", class: actorClassOrdinaryInput, direct: true},
		{name: "external prose", author: "contributor", body: "please adjust this", class: actorClassOrdinaryInput},
		{name: "external marker forgery", author: "contributor", body: "WARD-WORKFLOW: done", class: actorClassInvalid},
		{name: "external self-signed intent", author: "contributor", body: "WARD-APPROVAL-INTENT: sha256:abc", class: actorClassInvalid},
		{name: "missing author", body: "WARD-WORKFLOW: done", class: actorClassInvalid},
		{name: "unknown machine marker", author: "ward-bot", body: "WARD-BOGUS: pass", class: actorClassInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyActorCommentWithPolicy(commentBy(tc.author, tc.body), policy)
			if got.Class != tc.class || got.RecordKind != tc.kind || got.Direct != tc.direct {
				t.Fatalf("classification = %+v, want class=%q kind=%q direct=%t", got, tc.class, tc.kind, tc.direct)
			}
		})
	}
}

func TestFixedWardRecordKind(t *testing.T) {
	for _, tc := range []struct {
		body, kind string
	}{
		{body: agentReservationMarker + "\nWARD-WORKFLOW: reservation-held", kind: recordKindReservation},
		{body: agentReservationReleaseMarker + "\nWARD-WORKFLOW: reservation-released", kind: recordKindReservationRelease},
		{body: agentNeedsRedispatchMarker + "\nWARD-WORKFLOW: dispatch-deferred", kind: recordKindDispatch},
		{body: "WARD-WORKFLOW: review-pass", kind: recordKindReview},
		{body: "WARD-WORKFLOW: triage", kind: recordKindTriage},
		{body: "WARD-WORKFLOW: routed", kind: recordKindRoute},
		{body: wardApprovalSnapshotMarker + " v1", kind: recordKindApprovalSnapshot},
	} {
		kind, recognized, attempted := fixedWardRecordKind(tc.body)
		if !recognized || !attempted || kind != tc.kind {
			t.Fatalf("fixedWardRecordKind(%q) = %q, %t, %t, want %q, true, true", tc.body, kind, recognized, attempted, tc.kind)
		}
	}
}
