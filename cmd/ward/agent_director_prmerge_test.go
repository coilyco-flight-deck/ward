package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDirectorMergeEligibility(t *testing.T) {
	entry := &backlogEntry{
		Num:   42,
		State: "done",
		LastOutcome: &backlogOutcome{
			Status: "done",
			Text:   "done - review summary: passed: green and clear",
		},
	}
	basePR := forgejoPullRequest{
		Number: 42,
		Body:   "ward.workflow: pull-requests-and-merge\ncloses #42\n",
		State:  "open",
	}
	basePR.Head.Ref = "issue-42"

	if num, ok, reason := directorMergeEligibility("o/r", basePR, entry); !ok || num != 42 || reason != "" {
		t.Fatalf("eligible PR rejected: num=%d ok=%v reason=%q", num, ok, reason)
	}

	cases := []struct {
		name string
		pr   forgejoPullRequest
		ent  *backlogEntry
	}{
		{name: "draft", pr: func() forgejoPullRequest { pr := basePR; pr.Draft = true; return pr }()},
		{name: "wrong branch", pr: func() forgejoPullRequest { pr := basePR; pr.Head.Ref = "feature-42"; return pr }()},
		{name: "missing marker", pr: func() forgejoPullRequest { pr := basePR; pr.Body = "closes #42"; return pr }()},
		{name: "salvage branch", pr: func() forgejoPullRequest { pr := basePR; pr.Head.Ref = "ward-salvage/ward-42"; return pr }()},
		{name: "issue not done", pr: basePR, ent: &backlogEntry{Num: 42, State: "queued"}},
		{name: "review not passed", pr: basePR, ent: &backlogEntry{Num: 42, State: "done", LastOutcome: &backlogOutcome{Status: "done", Text: "done - review summary: skipped: intentionally skipped"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok, _ := directorMergeEligibility("o/r", tc.pr, tc.ent); ok {
				t.Fatalf("expected %s to be denied", tc.name)
			}
		})
	}
}

func TestForgejoClientPullRequestEndpoints(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/o/r/pulls":
			if got := r.URL.Query().Get("state"); got != "open" {
				t.Fatalf("list query state = %q, want open", got)
			}
			if got := r.URL.Query().Get("limit"); got != "50" {
				t.Fatalf("list query limit = %q, want 50", got)
			}
			if got := r.Header.Get("Authorization"); got != "token tok" {
				t.Fatalf("list auth header = %q, want token tok", got)
			}
			_ = json.NewEncoder(w).Encode([]forgejoPullRequest{{
				Number: 42,
				Body:   "ward.workflow: pull-requests-and-merge\ncloses #42",
				State:  "open",
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/o/r/pulls/42/merge":
			if got := r.Header.Get("Authorization"); got != "token tok" {
				t.Fatalf("merge auth header = %q, want token tok", got)
			}
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode merge payload: %v", err)
			}
			if payload["Do"] != "merge" {
				t.Fatalf("merge payload = %#v, want Do=merge", payload)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "tok"}
	prs, err := cl.listOpenPullRequests(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("listOpenPullRequests: %v", err)
	}
	if len(prs) != 1 || prs[0].Number != 42 {
		t.Fatalf("listOpenPullRequests = %#v, want one #42", prs)
	}
	if err := cl.mergePullRequest(context.Background(), "o", "r", 42); err != nil {
		t.Fatalf("mergePullRequest: %v", err)
	}
}

func TestDirectorMergeEligibilityString(t *testing.T) {
	if !strings.Contains(directorPRMergeMarker, "pull-requests-and-merge") {
		t.Fatalf("merge marker = %q, want the workflow name", directorPRMergeMarker)
	}
}
