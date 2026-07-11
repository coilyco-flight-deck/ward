package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestCreateIssueBodyIsSigned guards that createIssue's HTTP body carries the
// agent attribution footer (ward#155).
func TestCreateIssueBodyIsSigned(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/repos/coilyco-flight-deck/ward/issues" {
			t.Fatalf("request = %s %s, want issue create endpoint", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":246}`))
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret", mode: modeClaude}
	if _, err := cl.createIssue(context.Background(), "coilyco-flight-deck", "ward", "t", "raw report body"); err != nil {
		t.Fatalf("createIssue: %v", err)
	}
	if !strings.Contains(got["body"], agentSignatureMarker) {
		t.Fatalf("body lost the signature marker: %q", got["body"])
	}
}

func TestForgejoClientGetRawStreamsPlainBody(t *testing.T) {
	var gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/repos/coilyco-flight-deck/ward/actions/runs/123/jobs/456/attempt/7/logs" {
			t.Fatalf("path = %q, want actions log endpoint", r.URL.Path)
		}
		_, _ = w.Write([]byte("line 1\nline 2\n"))
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	body, err := cl.getRaw(context.Background(), []string{"repos", "coilyco-flight-deck", "ward", "actions", "runs", "123", "jobs", "456", "attempt", "7", "logs"}, "text/plain")
	if err != nil {
		t.Fatalf("getRaw: %v", err)
	}
	if got := string(body); got != "line 1\nline 2\n" {
		t.Fatalf("body = %q, want raw text", got)
	}
	if gotAuth != "token secret" {
		t.Fatalf("Authorization = %q, want bearer token header", gotAuth)
	}
	if gotAccept != "text/plain" {
		t.Fatalf("Accept = %q, want text/plain", gotAccept)
	}
}

func TestForgejoClientGetRawReportsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("brew failed"))
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	body, err := cl.getRaw(context.Background(), []string{"repos", "coilyco-flight-deck", "ward", "actions", "runs", "123", "jobs", "456", "attempt", "7", "logs"}, "text/plain")
	if err == nil {
		t.Fatal("getRaw: want error, got nil")
	}
	if string(body) != "brew failed" {
		t.Fatalf("raw body = %q, want response bytes back", string(body))
	}
	for _, want := range []string{"418 I'm a teapot", "/api/v1/repos/coilyco-flight-deck/ward/actions/runs/123/jobs/456/attempt/7/logs", "brew failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestGetPullRequestRetriesEmptyBodyThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if got := atomic.AddInt32(&calls, 1); got == 1 {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(`{"mergeable":true}`))
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	pr, err := cl.getPullRequest(context.Background(), "coilyco-flight-deck", "ward", 862)
	if err != nil {
		t.Fatalf("getPullRequest: %v", err)
	}
	if !pr.Mergeable {
		t.Fatalf("mergeable = %v, want true", pr.Mergeable)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
	}
}

func TestGetPullRequestPersistentEmptyBody(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := cl.getPullRequest(context.Background(), "coilyco-flight-deck", "ward", 863)
	if err == nil {
		t.Fatal("getPullRequest: want error, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("request count = %d, want 3", got)
	}
	for _, want := range []string{"200 OK", "0 byte(s)", "<empty>"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestGetPullRequestReadsBodyLargerThan4096(t *testing.T) {
	body := `{"mergeable":true,"padding":"` + strings.Repeat("x", 5000) + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	pr, err := cl.getPullRequest(context.Background(), "coilyco-flight-deck", "ward", 864)
	if err != nil {
		t.Fatalf("getPullRequest: %v", err)
	}
	if !pr.Mergeable {
		t.Fatalf("mergeable = %v, want true", pr.Mergeable)
	}
}

func TestGetPullRequestReportsNotFoundWithoutRaw404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"The target couldn't be found."}`))
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := cl.getPullRequest(context.Background(), "coilyco-flight-deck", "ward", 865)
	if err == nil {
		t.Fatal("getPullRequest: want error, got nil")
	}
	if strings.Contains(err.Error(), "404 Not Found") {
		t.Fatalf("getPullRequest leaked raw 404 wording: %q", err)
	}
	if !strings.Contains(err.Error(), "pull request coilyco-flight-deck/ward#865 not found") {
		t.Fatalf("getPullRequest not-found wording = %q", err)
	}
}

func TestGetPullRequestMergeabilityReportsNotFoundWithoutRaw404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"The target couldn't be found."}`))
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := cl.getPullRequestMergeability(context.Background(), "coilyco-flight-deck", "ward", 866)
	if err == nil {
		t.Fatal("getPullRequestMergeability: want error, got nil")
	}
	if strings.Contains(err.Error(), "404 Not Found") {
		t.Fatalf("getPullRequestMergeability leaked raw 404 wording: %q", err)
	}
	if !strings.Contains(err.Error(), "pull request coilyco-flight-deck/ward#866 not found") {
		t.Fatalf("getPullRequestMergeability not-found wording = %q", err)
	}
}

// TestForgejoGraftInventory is the ward#407 removal guardrail: every behavior the
// four buildForgejoOps grafts must re-home is asserted present on the built tree.
func TestForgejoGraftInventory(t *testing.T) {
	dir := writeBundleFixture(t)
	t.Setenv(wardConfigRefEnv, "file://"+dir)
	forgejo, err := buildForgejoOps()
	if err != nil {
		t.Fatalf("buildForgejoOps: %v", err)
	}
	issue := subCommandNamed(forgejo, "issue")
	if issue == nil {
		t.Fatal("forgejo group has no `issue` subtree")
	}

	// Graft 1 (overrideForgejoViewIssue): the lean `issue view` action.
	if subCommandNamed(issue, "view") == nil {
		t.Error("graft 1 gone: `issue view` leaf absent")
	}
	// Graft 2 (overrideForgejoCreateIssue): the --quiet machine-output flag.
	if create := subCommandNamed(issue, "create"); create == nil {
		t.Error("graft 2: `issue create` leaf absent")
	} else if !hasFlagNamed(create, flagQuiet) {
		t.Errorf("graft 2 gone: `issue create` no longer accepts --%s", flagQuiet)
	}
	// Graft 3 (overrideForgejoCommentIssue): --body-file re-added onto the shadow.
	if comment := subCommandNamed(issue, "comment"); comment == nil {
		t.Error("graft 3: `issue comment` leaf absent")
	} else if !hasFlagNamed(comment, flagBodyFile) {
		t.Errorf("graft 3 gone: `issue comment` no longer accepts --%s", flagBodyFile)
	}
	pr := subCommandNamed(forgejo, "pr")
	if pr == nil {
		t.Fatal("forgejo group has no `pr` subtree")
	}
	if subCommandNamed(pr, "edit") == nil {
		t.Error("pr edit leaf absent")
	}
	actions := subCommandNamed(forgejo, "actions")
	if actions == nil {
		t.Fatal("graft 4: `actions` group absent")
	}
	if logs := subCommandNamed(actions, "logs"); logs == nil {
		t.Error("graft 4 gone: `actions logs` leaf absent")
	} else {
		if !strings.Contains(logs.Usage, "/repos/{owner}/{repo}/actions/runs/{run}/jobs/{job}/attempt/{attempt}/logs") {
			t.Errorf("actions logs usage = %q, want HTTP path shape", logs.Usage)
		}
		if !strings.Contains(logs.Description, "raw body") {
			t.Errorf("actions logs description = %q, want raw-body fetch wording", logs.Description)
		}
	}
}

// TestForgejoGetIssueFlattensLabels pins that getIssue flattens the Forgejo
// label objects to the name list the ceiling gate reads (agentic-os#246).
func TestForgejoGetIssueFlattensLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/coilyco-flight-deck/agentic-os/issues/246" {
			t.Fatalf("path = %q, want issue endpoint", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"number":246,"title":"t","body":"b","state":"open","html_url":"https://f/246","labels":[{"name":"interactive"},{"name":"P3"}]}`))
	}))
	defer srv.Close()

	c := &forgejoClient{baseURL: srv.URL}
	issue, err := c.getIssue(context.Background(), "coilyco-flight-deck", "agentic-os", 246)
	if err != nil {
		t.Fatalf("getIssue: %v", err)
	}
	if got := strings.Join(issue.Labels, ","); got != "interactive,P3" {
		t.Errorf("issue.Labels = %v, want [interactive P3]", issue.Labels)
	}
	if issue.State != "open" || issue.Number != 246 {
		t.Errorf("issue core fields lost: %+v", issue)
	}
}

func TestListOpenIssuesAndPullRequestsClassifyPullRequestNull(t *testing.T) {
	var issueFeedCalls int
	var typedPullFeedCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/coilyco-flight-deck/ward/issues":
			if got := r.URL.Query().Get("state"); got != "open" {
				t.Fatalf("state query = %q, want open", got)
			}
			switch got := r.URL.Query().Get("type"); got {
			case "":
				issueFeedCalls++
				_ = json.NewEncoder(w).Encode([]map[string]any{
					{
						"number": 982, "title": "normal issue", "body": "body", "state": "open",
						"html_url": "https://f/issues/982", "labels": []map[string]any{},
						"pull_request": nil,
					},
					{
						"number": 983, "title": "open PR", "body": "closes #983", "state": "open",
						"html_url": "https://f/pulls/983", "labels": []map[string]any{},
						"pull_request": map[string]any{"url": "https://f/pulls/983"},
					},
				})
			case "pulls":
				typedPullFeedCalls++
				_ = json.NewEncoder(w).Encode([]map[string]any{
					{
						"number": 983, "title": "open PR", "body": "closes #983", "state": "open",
						"html_url": "https://f/pulls/983", "labels": []map[string]any{},
						"pull_request": map[string]any{"url": "https://f/pulls/983"},
					},
				})
			default:
				t.Fatalf("type query = %q, want empty generic issue feed or pulls", got)
			}
		case "/api/v1/repos/coilyco-flight-deck/ward/pulls/983":
			if got := r.Header.Get("Authorization"); got != "token secret" {
				t.Fatalf("auth header = %q, want token secret", got)
			}
			_, _ = w.Write([]byte(`{"mergeable":true}`))
		default:
			t.Fatalf("unexpected path: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	issues, err := cl.listOpenIssues(context.Background(), "coilyco-flight-deck", "ward", 50)
	if err != nil {
		t.Fatalf("listOpenIssues: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 982 {
		t.Fatalf("issues = %+v, want only the normal issue", issues)
	}
	prs, err := cl.listOpenPullRequests(context.Background(), "coilyco-flight-deck", "ward", 50)
	if err != nil {
		t.Fatalf("listOpenPullRequests: %v", err)
	}
	if len(prs) != 1 || prs[0].Number != 983 || !prs[0].Mergeable || !prs[0].MergeableKnown {
		t.Fatalf("prs = %+v, want only the real PR with mergeability", prs)
	}
	if issueFeedCalls != 1 {
		t.Fatalf("issue feed calls = %d, want 1", issueFeedCalls)
	}
	if typedPullFeedCalls != 1 {
		t.Fatalf("typed pull feed calls = %d, want 1", typedPullFeedCalls)
	}
}

func TestListOpenPullRequestsKeepsTypedPaginationWhenGenericIssuesFillFirstPage(t *testing.T) {
	var genericIssueFeedCalls int
	var typedPullFeedCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/coilyco-flight-deck/ward/issues":
			if got := r.URL.Query().Get("state"); got != "open" {
				t.Fatalf("state query = %q, want open", got)
			}
			if got := r.URL.Query().Get("type"); got == "" {
				genericIssueFeedCalls++
				rows := make([]map[string]any, 0, 50)
				for i := 0; i < 50; i++ {
					rows = append(rows, map[string]any{
						"number": 1000 + i, "title": "normal issue", "body": "body", "state": "open",
						"html_url": fmt.Sprintf("https://f/issues/%d", 1000+i), "labels": []map[string]any{},
						"pull_request": nil,
					})
				}
				_ = json.NewEncoder(w).Encode(rows)
				return
			}
			if got := r.URL.Query().Get("type"); got != "pulls" {
				t.Fatalf("type query = %q, want pulls", got)
			}
			typedPullFeedCalls++
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"number": 1983, "title": "late PR", "body": "closes #1983", "state": "open",
					"html_url": "https://f/pulls/1983", "labels": []map[string]any{},
					"pull_request": map[string]any{"url": "https://f/pulls/1983"},
				},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/pulls/1983":
			_, _ = w.Write([]byte(`{"mergeable":true}`))
		default:
			t.Fatalf("unexpected path: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	prs, err := cl.listOpenPullRequests(context.Background(), "coilyco-flight-deck", "ward", 50)
	if err != nil {
		t.Fatalf("listOpenPullRequests: %v", err)
	}
	if len(prs) != 1 || prs[0].Number != 1983 || !prs[0].Mergeable || !prs[0].MergeableKnown {
		t.Fatalf("prs = %+v, want the typed PR beyond the generic issue page", prs)
	}
	if genericIssueFeedCalls != 0 {
		t.Fatalf("generic issue feed calls = %d, want 0", genericIssueFeedCalls)
	}
	if typedPullFeedCalls != 1 {
		t.Fatalf("typed pull feed calls = %d, want 1", typedPullFeedCalls)
	}
}

func TestFetchIssueIgnoresBadConfigRef(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "file:///definitely/not/a/ward-bundle")
	orig := forgejoBaseURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/coilyco-flight-deck/ward/issues/929" {
			t.Fatalf("path = %q, want direct issue endpoint", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"number":929,"title":"direct","body":"b","state":"open","html_url":"https://f/929","labels":[{"name":"P0"}]}`))
	}))
	defer srv.Close()
	forgejoBaseURL = srv.URL
	t.Cleanup(func() { forgejoBaseURL = orig })

	issue, err := (&Runner{}).fetchIssueByForge(context.Background(), "test", forgeForgejo, modeCodex, "coilyco-flight-deck", "ward", 929)
	if err != nil {
		t.Fatalf("fetchIssueByForge with bad %s: %v", wardConfigRefEnv, err)
	}
	if issue.Number != 929 || strings.Join(issue.Labels, ",") != "P0" {
		t.Fatalf("issue = %+v, want direct HTTP result", issue)
	}
}
