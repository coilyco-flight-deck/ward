package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListActionRunsReadsRunFeed pins the native Actions run list: path, limit,
// and the per-run conclusion projection (ward#1067).
func TestListActionRunsReadsRunFeed(t *testing.T) {
	var gotPath, gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotLimit = r.URL.Query().Get("limit")
		_, _ = w.Write([]byte(`{"workflow_runs":[
			{"id":6778,"index_in_repo":1689,"title":"t1","status":"success","workflow_id":"test.yml","prettyref":"#1036","event":"pull_request"},
			{"id":6779,"index_in_repo":1690,"title":"t2","status":"failure","workflow_id":"release.yml","prettyref":"main","event":"push"}
		]}`))
	}))
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	runs, err := cl.listActionRuns(context.Background(), "coilyco-flight-deck", "ward", 2)
	if err != nil {
		t.Fatalf("listActionRuns: %v", err)
	}
	if gotPath != "/api/v1/repos/coilyco-flight-deck/ward/actions/runs" {
		t.Errorf("path = %q, want the actions runs endpoint", gotPath)
	}
	if gotLimit != "2" {
		t.Errorf("limit = %q, want 2", gotLimit)
	}
	if len(runs) != 2 || runs[0].ID != 6778 || runs[1].Status != "failure" || runs[1].WorkflowID != "release.yml" {
		t.Errorf("runs = %+v, want two projected rows", runs)
	}
}

// TestPullRequestMergedStates pins the merged-state check: 204 merged, 404 not.
func TestPullRequestMergedStates(t *testing.T) {
	status := http.StatusNoContent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/coilyco-flight-deck/ward/pulls/7/merge" {
			t.Fatalf("path = %q, want the pulls merge endpoint", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	merged, err := cl.pullRequestMerged(context.Background(), "coilyco-flight-deck", "ward", 7)
	if err != nil || !merged {
		t.Fatalf("pullRequestMerged on 204 = (%v, %v), want (true, nil)", merged, err)
	}
	status = http.StatusNotFound
	merged, err = cl.pullRequestMerged(context.Background(), "coilyco-flight-deck", "ward", 7)
	if err != nil || merged {
		t.Fatalf("pullRequestMerged on 404 = (%v, %v), want (false, nil)", merged, err)
	}
}
