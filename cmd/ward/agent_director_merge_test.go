package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
)

func TestDirectorMergeDecision(t *testing.T) {
	basePR := dispatch.Issue{
		Title: "ship the fix",
		Body:  "closes #729\n",
	}
	baseMeta := directorRunMeta{
		Workflow:   string(workflowPullRequestsAndMerge),
		Review:     "passed: two reviewers agreed",
		Outcome:    backlogOutcome{Status: "done"},
		HasOutcome: true,
	}

	allowed, reason, linked, _ := directorMergeDecision(basePR, 729, baseMeta)
	if !allowed || reason != "" || linked != 729 {
		t.Fatalf("allowed decision = %v %q %d, want true/\"\"/729", allowed, reason, linked)
	}

	for _, tc := range []struct {
		name string
		pr   dispatch.Issue
		meta directorRunMeta
		want string
	}{
		{
			name: "salvage",
			pr:   dispatch.Issue{Title: "ward salvage: residual cleanup", Body: "closes #729"},
			meta: baseMeta,
			want: "salvage PRs are cleanup noise, not merge-authorized work",
		},
		{
			name: "wip",
			pr:   dispatch.Issue{Title: "WIP: ship the fix", Body: "closes #729"},
			meta: baseMeta,
			want: "draft PRs are not merge-authorized",
		},
		{
			name: "needs-done",
			pr:   basePR,
			meta: directorRunMeta{Workflow: string(workflowPullRequestsAndMerge), Review: "passed: ok"},
			want: "linked issue did not finish with WARD-OUTCOME: done",
		},
		{
			name: "needs-merge-workflow",
			pr:   basePR,
			meta: directorRunMeta{HasOutcome: true, Outcome: backlogOutcome{Status: "done"}, Workflow: string(workflowPR), Review: "passed: ok"},
			want: "workflow pr still needs human merge approval",
		},
		{
			name: "needs-review",
			pr:   basePR,
			meta: directorRunMeta{HasOutcome: true, Outcome: backlogOutcome{Status: "done"}, Workflow: string(workflowPullRequestsAndMerge), Review: "blocked: concern"},
			want: "review gate did not pass",
		},
	} {
		allowed, reason, _, _ := directorMergeDecision(tc.pr, 729, tc.meta)
		if allowed {
			t.Fatalf("%s: want deny, got allow", tc.name)
		}
		if reason != tc.want {
			t.Fatalf("%s: reason = %q, want %q", tc.name, reason, tc.want)
		}
	}
}

func TestDirectorLinkedIssueNumber(t *testing.T) {
	for _, tc := range []struct {
		body string
		want int
		ok   bool
	}{
		{body: "closes #12", want: 12, ok: true},
		{body: "Fixes #7\n", want: 7, ok: true},
		{body: "resolves #3", want: 3, ok: true},
		{body: "notes only", want: 0, ok: false},
	} {
		got, ok := directorLinkedIssueNumber(tc.body)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("%q => (%d,%v), want (%d,%v)", tc.body, got, ok, tc.want, tc.ok)
		}
	}
}

func TestMergePullRequestRequestShape(t *testing.T) {
	var gotToken, gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("Authorization")
		gotMethod = r.Method
		gotPath = r.URL.Path
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["do"] != "merge" {
			t.Fatalf("body do = %q, want merge", body["do"])
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	if err := cl.mergePullRequest(context.Background(), "coilyco-flight-deck", "ward", 729); err != nil {
		t.Fatalf("mergePullRequest: %v", err)
	}
	if gotToken != "token secret" {
		t.Fatalf("auth header = %q, want token secret", gotToken)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/coilyco-flight-deck/ward/pulls/729/merge" {
		t.Fatalf("path = %q, want merge endpoint", gotPath)
	}
}

func TestDirectorRunMetaParsesWorkflowAndReview(t *testing.T) {
	body := strings.Join([]string{
		"WARD-OUTCOME: done - merged and pushed",
		"workflow: pull-requests-and-merge; review summary: passed: all green",
	}, "\n")
	meta := parseDirectorRunMeta(body)
	if !meta.HasOutcome || meta.Outcome.Status != "done" {
		t.Fatalf("meta outcome = %+v, want done", meta)
	}
	if meta.Workflow != string(workflowPullRequestsAndMerge) {
		t.Fatalf("meta workflow = %q, want %q", meta.Workflow, workflowPullRequestsAndMerge)
	}
	if meta.Review != "passed: all green" {
		t.Fatalf("meta review = %q, want passed: all green", meta.Review)
	}
	if _, ok := backlogOutcomeOfComment(body); !ok {
		t.Fatal("comment body should parse as an outcome comment")
	}
}
