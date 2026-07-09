package main

import (
	"strings"
	"testing"
)

func TestQAPromptIncludesInspectionBrief(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 812}
	level, err := parseReplyThoroughness("standard")
	if err != nil {
		t.Fatalf("parseReplyThoroughness: %v", err)
	}
	got := qaResearchPrompt(
		ref,
		"ship brokered QA",
		"candidate body",
		[]issueComment{{Body: "first thread note"}},
		"inspect the branch and checks",
		level,
	)
	for _, want := range []string{
		"structured verdict",
		"candidate branch",
		"pull request",
		"checks",
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
	got := qaVerdictComment(modeClaude, qaThoroughness{}, "inspect the branch", read)
	for _, want := range []string{
		"WARD-QA: failed ❌",
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
	got := qaVerdictComment(modeClaude, qaThoroughness{}, "inspect the branch", "this is not json")
	if !strings.Contains(got, "WARD-QA: failed ❌") {
		t.Fatalf("malformed QA output should surface a failure, got:\n%s", got)
	}
	if !strings.Contains(got, "Could not parse") {
		t.Fatalf("malformed QA output should explain the parse failure, got:\n%s", got)
	}
}
