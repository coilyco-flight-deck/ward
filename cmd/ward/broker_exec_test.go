package main

import (
	"strings"
	"testing"
	"time"
)

func TestReadOnlyIssueCommentBlockReasonRunningEngineer(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1141}
	reason, ok := readOnlyIssueCommentBlockReason(ref, []string{"engineer-codex-ward-1141"}, nil, time.Now().UTC())
	if !ok {
		t.Fatal("running engineer should block")
	}
	for _, want := range []string{
		"engineer-codex-ward-1141",
		"ward agent list",
		"ward agent logs coilyco-flight-deck/ward#1141",
		"file a separate issue",
	} {
		if !strings.Contains(reason, want) {
			t.Fatalf("running-engineer reason missing %q: %s", want, reason)
		}
	}
}

func TestReadOnlyIssueCommentBlockReasonReservation(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1141}
	now := time.Date(2026, 7, 13, 2, 0, 0, 0, time.UTC)
	comment := issueComment{Body: agentReservationMarker + "\nHolder: container `engineer-codex-ward-1141` on host `director-box`."}
	comment.User.Login = "coilyco-ops"
	comment.CreatedAt = now.Add(-10 * time.Minute)
	comments := []issueComment{comment}
	reason, ok := readOnlyIssueCommentBlockReason(ref, nil, comments, now)
	if !ok {
		t.Fatal("fresh reservation should block")
	}
	for _, want := range []string{
		"engineer-codex-ward-1141@director-box",
		"ward agent list",
		"ward agent logs coilyco-flight-deck/ward#1141",
		"file a separate issue",
	} {
		if !strings.Contains(reason, want) {
			t.Fatalf("reservation reason missing %q: %s", want, reason)
		}
	}
}

func TestReadOnlyIssueCommentBlockReasonNoMatch(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1141}
	reason, ok := readOnlyIssueCommentBlockReason(ref, nil, nil, time.Now().UTC())
	if ok {
		t.Fatalf("unexpected block reason: %s", reason)
	}
}
