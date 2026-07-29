package main

import (
	"strings"
	"testing"
	"time"
)

func TestQAPromptIncludesInspectionBrief(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 812}
	level, err := parseQAThoroughness("standard")
	if err != nil {
		t.Fatalf("parseQAThoroughness: %v", err)
	}
	ctx := qaLaunchContext{
		IssueRef:        ref.String(),
		PRRef:           "coilyco-flight-deck/ward#729",
		CandidateBranch: "issue-812",
		ReviewedSHA:     "abc123",
		ReviewerFamily:  qaFamilyInternal,
		Workflow:        string(workflowPullRequestAndMerge),
		RunIdentity:     "ward-qa-1",
	}
	got := qaResearchPrompt(
		ref,
		"ship brokered QA",
		"candidate body",
		[]issueComment{{Body: "first thread note"}},
		"inspect the branch and checks",
		level,
		ctx,
	)
	for _, want := range []string{
		"structured verdict",
		"candidate branch",
		"pull request",
		"checks",
		"Current PR ref: coilyco-flight-deck/ward#729",
		"Current candidate branch: issue-812",
		"Current reviewed SHA: abc123",
		"Reviewer family: internal",
		"Run identity: ward-qa-1",
		"candidate body",
		"first thread note",
		"inspect the branch and checks",
		"verdict",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("qaResearchPrompt missing %q\n---\n%s", want, got)
		}
	}
}

func TestQAVerdictCommentSurfacesFailure(t *testing.T) {
	read := `{"verdict":"fail","summary":"checks are red","evidence":["CI failed"],"risks":["merge would regress"],"next_steps":["fix the checks"]}`
	got := qaVerdictComment(modeClaude, qaThoroughness{}, qaFamilyInternal, "inspect the branch", qaLaunchContext{
		IssueRef:        "coilyco-flight-deck/ward#844",
		PRRef:           "coilyco-flight-deck/ward#729",
		CandidateBranch: "issue-844",
		ReviewedSHA:     "abc123",
		ReviewerFamily:  qaFamilyInternal,
		Workflow:        string(workflowPullRequestAndMerge),
		RunIdentity:     "ward-qa-1",
	}, read)
	for _, want := range []string{
		"WARD-WORKFLOW: qa-failed ❌",
		"verdict: fail",
		"reviewed_sha: abc123",
		"reviewer_family: internal",
		"workflow: pull-request-and-merge",
		"issue_ref: coilyco-flight-deck/ward#844",
		"pr_ref: coilyco-flight-deck/ward#729",
		"candidate_branch: issue-844",
		"reason: checks are red",
		"checks are red",
		"CI failed",
		"merge would regress",
		"fix the checks",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("qaVerdictComment missing %q\n---\n%s", want, got)
		}
	}
}

func TestQAVerdictCommentFallbacksOnMalformedOutput(t *testing.T) {
	got := qaVerdictComment(modeClaude, qaThoroughness{}, qaFamilyInternal, "inspect the branch", qaLaunchContext{}, "this is not json")
	if !strings.Contains(got, "WARD-WORKFLOW: qa-failed ❌") {
		t.Fatalf("malformed QA output should surface a failure, got:\n%s", got)
	}
	if !strings.Contains(got, "Could not parse") {
		t.Fatalf("malformed QA output should explain the parse failure, got:\n%s", got)
	}
}

func TestLatestQAVerdictCommentMatchesCurrentHead(t *testing.T) {
	current := qaVerdictCommentFrom(modeClaude, qaThoroughness{}, qaFamilyInternal, "inspect the branch", qaLaunchContext{
		IssueRef:       "coilyco-flight-deck/ward#844",
		PRRef:          "coilyco-flight-deck/ward#729",
		ReviewedSHA:    "abc123",
		ReviewerFamily: qaFamilyInternal,
		Workflow:       string(workflowPullRequestAndMerge),
		RunIdentity:    "ward-qa-1",
	}, qaVerdict{Verdict: "pass", Summary: "looks good"})
	stale := qaVerdictCommentFrom(modeClaude, qaThoroughness{}, qaFamilyInternal, "inspect the branch", qaLaunchContext{
		IssueRef:       "coilyco-flight-deck/ward#844",
		PRRef:          "coilyco-flight-deck/ward#729",
		ReviewedSHA:    "deadbeef",
		ReviewerFamily: qaFamilyInternal,
		Workflow:       string(workflowPullRequestAndMerge),
		RunIdentity:    "ward-qa-2",
	}, qaVerdict{Verdict: "fail", Summary: "stale verdict"})
	meta, ok := latestQAVerdictComment([]issueComment{
		{Body: current, CreatedAt: mustParseTime(t, "2026-07-09T00:00:00Z")},
		{Body: stale, CreatedAt: mustParseTime(t, "2026-07-09T01:00:00Z")},
	}, "coilyco-flight-deck/ward#844", "coilyco-flight-deck/ward#729", "abc123")
	if !ok {
		t.Fatal("latestQAVerdictComment should recognize the current-head verdict")
	}
	if meta.ReviewedSHA != "abc123" || meta.Verdict != "pass" || meta.ReviewerFamily != qaFamilyInternal {
		t.Fatalf("parsed meta = %+v, want the current head verdict", meta)
	}
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	got, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return got
}
