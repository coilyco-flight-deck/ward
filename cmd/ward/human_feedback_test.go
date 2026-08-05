package main

import (
	"testing"
	"time"
)

func TestWardAuthoredCommentNeedsStructuredMarkerByDefault(t *testing.T) {
	setTestHome(t, t.TempDir())
	human := issueComment{
		Body: "plain human note",
		User: struct {
			Login string `json:"login"`
		}{Login: forgeForgejo.gitPushUser()},
	}
	if wardAuthoredComment(human) {
		t.Fatalf("plain comment from push user should be treated as human by default")
	}
	warded := issueComment{
		Body: "WARD-WORKFLOW: done ✅",
		User: struct {
			Login string `json:"login"`
		}{Login: forgeForgejo.gitPushUser()},
	}
	if !wardAuthoredComment(warded) {
		t.Fatalf("structured ward comment should still be treated as automation")
	}
}

func TestHumanFeedbackRejectsCustomAuthorAndMarkerBypasses(t *testing.T) {
	t.Setenv(envTrustedCollaborators, "repo-owner")
	t.Setenv(envAutomationActor, "ward-bot")
	for _, comment := range []issueComment{
		commentBy("helper-bot", "plain note"),
		commentBy("someone", "custom-automation: synthesized acknowledgement"),
	} {
		if wardAuthoredComment(comment) {
			t.Fatalf("comment %+v bypassed the fixed actor and marker policy", comment)
		}
	}
}

func TestParseBacklogOutcomeBlocksWhenHumanFeedbackIsNewer(t *testing.T) {
	setTestHome(t, t.TempDir())
	comments := []issueComment{
		{
			Body:      "WARD-WORKFLOW: done ✅\n\n<details><summary>details</summary>\n\nfinished\n\n</details>",
			CreatedAt: time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC),
			User: struct {
				Login string `json:"login"`
			}{Login: forgeForgejo.gitPushUser()},
		},
		{
			Body:      "this still needs a follow-up",
			CreatedAt: time.Date(2026, 7, 15, 8, 5, 0, 0, time.UTC),
			User: struct {
				Login string `json:"login"`
			}{Login: "repo-owner"},
		},
	}
	if got := parseBacklogOutcome(comments); got != nil {
		t.Fatalf("parseBacklogOutcome returned %+v, want nil when newer human feedback exists", got)
	}
}
