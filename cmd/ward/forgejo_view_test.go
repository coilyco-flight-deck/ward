package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// fatIssueJSON is a Forgejo issue payload carrying the full nested user/label/
// assignee profiles the lean projection must discard (ward#225).
const fatIssueJSON = `{
  "number": 225,
  "title": "fj issue output contains multiple full copies",
  "state": "open",
  "html_url": "https://forgejo.coilysiren.me/coilyco-flight-deck/ward/issues/225",
  "created_at": "2026-06-19T09:36:45Z",
  "updated_at": "2026-06-24T04:35:45Z",
  "comments": 2,
  "body": "the body",
  "user": {"login": "coilysiren", "email": "secret@example.com", "description": "a long bio", "avatar_url": "https://x/y.png", "followers_count": 99},
  "labels": [{"name": "P1", "color": "ff0000", "description": "high"}],
  "assignees": [{"login": "coilyco-ops", "email": "bot@example.com", "description": "bot bio"}]
}`

// fatCommentsJSON repeats the same full profile in every comment - the bloat the
// projection collapses to a login literal.
const fatCommentsJSON = `[
  {"body": "first", "created_at": "2026-06-19T09:36:45Z", "user": {"login": "coilysiren", "email": "secret@example.com", "description": "a long bio", "avatar_url": "https://x/y.png"}},
  {"body": "second", "created_at": "2026-06-24T04:35:45Z", "user": {"login": "coilysiren", "email": "secret@example.com", "description": "a long bio", "avatar_url": "https://x/y.png"}}
]`

// TestViewIssueProjectsLogins asserts leanView (the pure half of viewIssue)
// collapses every user to a login and the marshalled payload leaks no profile (ward#225).
func TestViewIssueProjectsLogins(t *testing.T) {
	var raw forgejoIssueRaw
	if err := json.Unmarshal([]byte(fatIssueJSON), &raw); err != nil {
		t.Fatalf("unmarshal issue: %v", err)
	}
	var comments []issueComment
	if err := json.Unmarshal([]byte(fatCommentsJSON), &comments); err != nil {
		t.Fatalf("unmarshal comments: %v", err)
	}
	view := leanView(raw, comments)
	if view.Issue.User != "coilysiren" {
		t.Errorf("issue user = %q, want %q", view.Issue.User, "coilysiren")
	}
	if len(view.Issue.Labels) != 1 || view.Issue.Labels[0] != "P1" {
		t.Errorf("labels = %v, want [P1]", view.Issue.Labels)
	}
	if len(view.Issue.Assignees) != 1 || view.Issue.Assignees[0] != "coilyco-ops" {
		t.Errorf("assignees = %v, want [coilyco-ops]", view.Issue.Assignees)
	}
	if len(view.Comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(view.Comments))
	}
	for i, c := range view.Comments {
		if c.User != "coilysiren" {
			t.Errorf("comment %d user = %q, want %q", i, c.User, "coilysiren")
		}
	}

	marshaled, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, leaked := range []string{"description", "avatar_url", "email", "followers_count"} {
		if strings.Contains(string(marshaled), leaked) {
			t.Errorf("lean payload leaked profile field %q: %s", leaked, marshaled)
		}
	}
}

// TestOverrideForgejoViewIssueSwapsLeaf asserts the built forgejo tree exposes
// `issue view` with an action installed (the lean override, not the engine leaf).
func TestOverrideForgejoViewIssueSwapsLeaf(t *testing.T) {
	forgejo, err := buildForgejoOps()
	if err != nil {
		t.Fatalf("buildForgejoOps: %v", err)
	}
	issue := subCommandNamed(forgejo, "issue")
	if issue == nil {
		t.Fatalf("forgejo group missing issue command")
	}
	view := subCommandNamed(issue, "view")
	if view == nil {
		t.Fatalf("issue command missing view leaf")
	}
	if view.Action == nil {
		t.Errorf("issue view leaf has no action after override")
	}
}
