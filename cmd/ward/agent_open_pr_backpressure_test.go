package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestOpenPRBackpressureApplies(t *testing.T) {
	ref := agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1}
	if !openPRBackpressureApplies(nil, resolvedWork{Ref: ref}) {
		t.Fatal("plain issue work should be subject to open-PR backpressure")
	}
	if openPRBackpressureApplies(nil, resolvedWork{Ref: agentIssueRef{Owner: "coilyco-flight-deck", Repo: "ward", Number: 1, MergeRequest: true}}) {
		t.Fatal("PR-repair work should bypass open-PR backpressure")
	}
	cmd := parseCommandForTest(t, agentEngineerFlags(), []string{"engineer", ref.String(), "--branch", "repair/branch"})
	if openPRBackpressureApplies(cmd, resolvedWork{Ref: ref}) {
		t.Fatal("explicit --branch work should bypass open-PR backpressure")
	}
}

func TestDispatchBrokerLaunchHasContinuationBranch(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want bool
	}{
		{"none", []string{"engineer", "coilyco-flight-deck/ward#1"}, false},
		{"pr-ref", []string{"engineer", "coilyco-flight-deck/ward!1"}, true},
		{"branch", []string{"engineer", "coilyco-flight-deck/ward#1", "--branch", "repair/branch"}, true},
		{"pr", []string{"engineer", "coilyco-flight-deck/ward#1", "--pr"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dispatchBrokerLaunchHasContinuationBranch(tc.argv); got != tc.want {
				t.Fatalf("dispatchBrokerLaunchHasContinuationBranch(%v) = %v, want %v", tc.argv, got, tc.want)
			}
		})
	}
}

func TestLaunchOpenPRBackpressureCheck(t *testing.T) {
	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "pulls" {
			t.Fatalf("unexpected issue feed query: %s", r.URL.RawQuery)
		}
		rows := make([]map[string]any, 0, 7)
		for i := 1; i <= 7; i++ {
			rows = append(rows, map[string]any{
				"number":       i,
				"title":        "PR",
				"body":         "body",
				"state":        "open",
				"html_url":     "https://forgejo.example/coilyco-flight-deck/ward/pulls/1",
				"pull_request": map[string]any{"url": "https://forgejo.example/coilyco-flight-deck/ward/pulls/1"},
				"labels":       []map[string]any{},
			})
		}
		_ = json.NewEncoder(w).Encode(rows)
	})
	for i := 1; i <= 7; i++ {
		i := i
		mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/pulls/"+strconv.Itoa(i), func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"mergeable": true})
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	forgejoBaseURL = srv.URL

	r := &Runner{}
	err := r.launchOpenPRBackpressureCheck(context.Background(), "ward agent engineer", "coilyco-flight-deck/ward", false)
	if err == nil {
		t.Fatal("launchOpenPRBackpressureCheck() on a full PR queue = nil, want backpressure")
	}
	if !isOpenPRBackpressureError(err) {
		t.Fatalf("launchOpenPRBackpressureCheck() error = %T %v, want open-PR backpressure", err, err)
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"7 open PR branch", "limit 6"}) {
		t.Fatalf("backpressure error = %q", got)
	}
	if got := err.Error(); !containsAll(got, []string{"owner/repo!N", "--branch"}) {
		t.Fatalf("backpressure error should mention documented continuation forms, got %q", got)
	}
	if err := r.launchOpenPRBackpressureCheck(context.Background(), "ward agent engineer", "coilyco-flight-deck/ward", true); err != nil {
		t.Fatalf("launchOpenPRBackpressureCheck() with continuation branch = %v", err)
	}
}

func TestDispatchBrokerOpenPRBackpressureCheck(t *testing.T) {
	oldBase := forgejoBaseURL
	defer func() { forgejoBaseURL = oldBase }()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "pulls" {
			t.Fatalf("unexpected issue feed query: %s", r.URL.RawQuery)
		}
		rows := make([]map[string]any, 0, 7)
		for i := 1; i <= 7; i++ {
			rows = append(rows, map[string]any{
				"number":       i,
				"title":        "PR",
				"body":         "body",
				"state":        "open",
				"html_url":     "https://forgejo.example/coilyco-flight-deck/ward/pulls/1",
				"pull_request": map[string]any{"url": "https://forgejo.example/coilyco-flight-deck/ward/pulls/1"},
				"labels":       []map[string]any{},
			})
		}
		_ = json.NewEncoder(w).Encode(rows)
	})
	for i := 1; i <= 7; i++ {
		i := i
		mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/pulls/"+strconv.Itoa(i), func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"mergeable": true})
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	forgejoBaseURL = srv.URL

	r := &Runner{}
	freshReq := dispatchBrokerRequest{
		Role: "engineer",
		Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--harness", "claude"},
	}
	if err := r.dispatchBrokerOpenPRBackpressureCheck(context.Background(), freshReq, "ward agent engineer"); err == nil {
		t.Fatal("dispatchBrokerOpenPRBackpressureCheck() on a fresh launch = nil, want backpressure")
	} else if !isOpenPRBackpressureError(err) {
		t.Fatalf("dispatchBrokerOpenPRBackpressureCheck() error = %T %v, want open-PR backpressure", err, err)
	}

	for _, tc := range []struct {
		name string
		req  dispatchBrokerRequest
	}{
		{
			name: "branch continuation",
			req: dispatchBrokerRequest{
				Role: "engineer",
				Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--harness", "claude", "--branch", "repair/branch"},
			},
		},
		{
			name: "pr continuation",
			req: dispatchBrokerRequest{
				Role: "engineer",
				Argv: []string{"engineer", "coilyco-flight-deck/ward#1", "--harness", "claude", "--pr"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := r.dispatchBrokerOpenPRBackpressureCheck(context.Background(), tc.req, "ward agent engineer"); err != nil {
				t.Fatalf("dispatchBrokerOpenPRBackpressureCheck() with continuation = %v", err)
			}
		})
	}
}

func containsAll(s string, want []string) bool {
	for _, w := range want {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}
