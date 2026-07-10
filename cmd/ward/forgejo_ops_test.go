package main

import (
	"context"
	"encoding/json"
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
	// Graft 4 (graftForgejoAdminExec): the admin/doctor remote-exec subtrees.
	for parent, leaves := range map[string][]string{
		"admin":  {"user", "auth"},
		"doctor": {"check"},
	} {
		group := subCommandNamed(forgejo, parent)
		if group == nil {
			t.Errorf("graft 4 gone: `%s` subtree absent from the forgejo group", parent)
			continue
		}
		for _, leaf := range leaves {
			if subCommandNamed(group, leaf) == nil {
				t.Errorf("graft 4 gone: `%s %s` leaf absent", parent, leaf)
			}
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
