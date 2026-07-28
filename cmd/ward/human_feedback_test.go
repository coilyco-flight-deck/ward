package main

import (
	"os"
	"path/filepath"
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

func TestHumanFeedbackConfigExtendsAutomationAndIgnoredAuthors(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	cfgPath := filepath.Join(home, ".ward", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(`agent:
  human-feedback:
    ignore-authors:
      - repo-owner
      - helper-bot
    automation-markers:
      - "custom-automation:"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if !wardAuthoredComment(issueComment{
		Body: "plain note",
		User: struct {
			Login string `json:"login"`
		}{Login: "repo-owner"},
	}) {
		t.Fatalf("ignored author should count as automation")
	}
	if !wardAuthoredComment(issueComment{
		Body: "custom-automation: synthesized acknowledgement",
		User: struct {
			Login string `json:"login"`
		}{Login: "someone"},
	}) {
		t.Fatalf("configured automation marker should count as automation")
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
