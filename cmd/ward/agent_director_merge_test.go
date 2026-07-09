package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/dispatch"
	"forgejo.coilysiren.me/coilyco-flight-deck/cli-guard/cli/shell"
)

func TestDirectorMergeDecision(t *testing.T) {
	basePR := dispatch.Issue{
		Title: "ship the fix",
		Body:  "closes #729\n",
	}
	baseMeta := directorRunMeta{
		Workflow:   string(workflowPullRequestAndMerge),
		Review:     "passed: two reviewers agreed",
		Outcome:    backlogOutcome{Status: "merge-ready"},
		HasOutcome: true,
	}

	allowed, reason, linked, _ := directorMergeDecision(basePR, 729, baseMeta)
	if !allowed || reason != "" || linked != 729 {
		t.Fatalf("allowed decision = %v %q %d, want true/\"\"/729", allowed, reason, linked)
	}

	cases := []struct {
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
			name: "needs-merge-workflow",
			pr:   basePR,
			meta: directorRunMeta{HasOutcome: true, Outcome: backlogOutcome{Status: "merge-ready"}, Workflow: string(workflowPullRequest), Review: "passed: ok"},
			want: "workflow pull-request still needs human merge approval",
		},
		{
			name: "needs-review",
			pr:   basePR,
			meta: directorRunMeta{HasOutcome: true, Outcome: backlogOutcome{Status: "merge-ready"}, Workflow: string(workflowPullRequestAndMerge), Review: "blocked: concern"},
			want: "review gate did not pass",
		},
		{
			name: "needs-ready-state",
			pr:   basePR,
			meta: directorRunMeta{HasOutcome: true, Outcome: backlogOutcome{Status: "submitted"}, Workflow: string(workflowPullRequestAndMerge), Review: "passed: ok"},
			want: "linked issue did not finish with WARD-OUTCOME: merge-ready",
		},
	}
	for _, tc := range cases {
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
		"WARD-OUTCOME: merge-ready",
		"",
		"<details><summary>details</summary>",
		"",
		"workflow: pull-request-and-merge; review summary: passed: all green",
		"",
		"</details>",
	}, "\n")
	meta := parseDirectorRunMeta(body)
	if !meta.HasOutcome || meta.Outcome.Status != "merge-ready" {
		t.Fatalf("meta outcome = %+v, want merge-ready", meta)
	}
	if meta.Workflow != string(workflowPullRequestAndMerge) {
		t.Fatalf("meta workflow = %q, want %q", meta.Workflow, workflowPullRequestAndMerge)
	}
	if meta.Review != "passed: all green" {
		t.Fatalf("meta review = %q, want passed: all green", meta.Review)
	}
	if _, ok := backlogOutcomeOfComment(body); !ok {
		t.Fatal("comment body should parse as an outcome comment")
	}
	doneBody := directorMergeDoneComment(729, meta)
	doneMeta := parseDirectorRunMeta(doneBody)
	if !doneMeta.HasOutcome || doneMeta.Outcome.Status != "done" {
		t.Fatalf("done meta outcome = %+v, want done", doneMeta)
	}
	if doneMeta.Workflow != string(workflowPullRequestAndMerge) {
		t.Fatalf("done meta workflow = %q, want %q", doneMeta.Workflow, workflowPullRequestAndMerge)
	}
	if !strings.Contains(doneBody, "merged PR #729 to main") {
		t.Fatalf("done comment body should name the merged PR, got: %s", doneBody)
	}
}

func TestDirectorMergeEligibilityRequiresWorkflowMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake exe is POSIX-only")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-ward")
	script := `#!/bin/sh
case "$3 $4" in
"issue get")
cat <<'JSON'
{"number":729,"title":"ship the fix","body":"closes #729","state":"open","html_url":"https://f/729","labels":[]}
JSON
;;
"issue-comment list")
cat <<'JSON'
[{"body":"WARD-OUTCOME: merge-ready\n\n<details><summary>details</summary>\n\nworkflow: pull-request-and-merge; review summary: passed: all green\n\n</details>","created_at":"2026-07-09T00:00:00Z","user":{"login":"coilyco-ops"}}]
JSON
;;
*)
echo "unexpected args: $@" >&2
exit 1
;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil { // #nosec G306 -- test-only executable
		t.Fatalf("write fake ward: %v", err)
	}
	cl := &forgejoClient{r: &Runner{Runner: &shell.Runner{}}, exe: fake}

	allowed, reason, linked, meta := directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward",
		dispatch.Issue{Title: "ship the fix", Body: "closes #729\n" + directorMergeWorkflowMarker + "\n"}, cl)
	if !allowed || reason != "" || linked != 729 {
		t.Fatalf("eligible PR = %v %q %d, want true/\"\"/729", allowed, reason, linked)
	}
	if meta.Workflow != string(workflowPullRequestAndMerge) || meta.Review != "passed: all green" || !meta.HasOutcome {
		t.Fatalf("eligible PR meta = %+v, want merge-lane metadata", meta)
	}

	allowed, reason, _, _ = directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward",
		dispatch.Issue{Title: "ship the fix", Body: "closes #729\n"}, cl)
	if allowed {
		t.Fatal("unmarked PR: want deny, got allow")
	}
	if reason != "PR body missing ward.workflow: pull-request-and-merge marker" {
		t.Fatalf("unmarked PR reason = %q, want missing-marker denial", reason)
	}
}
