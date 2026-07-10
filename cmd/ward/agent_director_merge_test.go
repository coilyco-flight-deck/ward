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
		Workflow:   string(workflowPullRequestAndMerge),
		Review:     "passed: two reviewers agreed",
		Outcome:    backlogOutcome{Status: "merge-ready"},
		HasOutcome: true,
		IssueRef:   "coilyco-flight-deck/ward#729",
		PRRef:      "coilyco-flight-deck/ward#729",
		PRHeadSHA:  "abc123",
		QA: qaCommentMeta{
			Verdict:        "pass",
			ReviewedSHA:    "abc123",
			ReviewerFamily: qaFamilyInternal,
			Workflow:       string(workflowPullRequestAndMerge),
			IssueRef:       "coilyco-flight-deck/ward#729",
			PRRef:          "coilyco-flight-deck/ward#729",
			Reason:         "checks are green",
			RunIdentity:    "ward-qa-1",
		},
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
			want: "workflow pull-requests still needs human merge approval",
		},
		{
			name: "needs-internal-family",
			pr:   basePR,
			meta: directorRunMeta{HasOutcome: true, Outcome: backlogOutcome{Status: "merge-ready"}, Workflow: string(workflowPullRequestAndMerge), Review: "blocked: concern"},
			want: "linked issue QA verdict was not from the internal reviewer family",
		},
		{
			name: "needs-ready-state",
			pr:   basePR,
			meta: directorRunMeta{HasOutcome: true, Outcome: backlogOutcome{Status: "submitted"}, Workflow: string(workflowPullRequestAndMerge), Review: "passed: ok"},
			want: "linked issue did not finish with WARD-OUTCOME: merge-ready",
		},
		{
			name: "done-is-not-merge-ready",
			pr:   basePR,
			meta: directorRunMeta{HasOutcome: true, Outcome: backlogOutcome{Status: "done"}, Workflow: string(workflowPullRequestAndMerge), Review: "passed: ok"},
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

func TestMergePullRequestRefusesFromReadOnlySurface(t *testing.T) {
	t.Setenv("WARD_READONLY", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("read-only merge refusal should not call Forgejo")
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	err := cl.mergePullRequest(context.Background(), "coilyco-flight-deck", "ward", 729)
	if err == nil {
		t.Fatal("mergePullRequest: want read-only refusal, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "read-only surface") || !strings.Contains(got, "director merge") {
		t.Fatalf("mergePullRequest error = %q, want read-only surface refusal", got)
	}
}

func TestMergePullRequestWithHeadPinsHeadCommitID(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/coilyco-flight-deck/ward/pulls/729/merge" {
			t.Fatalf("path = %q, want merge endpoint", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	if err := cl.mergePullRequestWithHead(context.Background(), "coilyco-flight-deck", "ward", 729, "abc123"); err != nil {
		t.Fatalf("mergePullRequestWithHead: %v", err)
	}
	if gotBody["do"] != "merge" {
		t.Fatalf("body do = %q, want merge", gotBody["do"])
	}
	if gotBody["head_commit_id"] != "abc123" {
		t.Fatalf("body head_commit_id = %q, want abc123", gotBody["head_commit_id"])
	}
}

func TestDirectorRunMetaParsesWorkflowAndReview(t *testing.T) {
	body := strings.Join([]string{
		"WARD-OUTCOME: merge-ready",
		"",
		"<details><summary>details</summary>",
		"",
		"workflow: pull-requests-and-merge; review summary: passed: all green",
		"checked head sha: abc123",
		"status context: ci/build=success, ci/test=success",
		"status state: success",
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
	if meta.Status.HeadSHA != "abc123" || meta.Status.State != "success" || len(meta.Status.Checks) != 2 {
		t.Fatalf("meta status = %+v, want checked status data", meta.Status)
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
	if doneMeta.Status.HeadSHA != "abc123" || doneMeta.Status.State != "success" || len(doneMeta.Status.Checks) != 2 {
		t.Fatalf("done meta status = %+v, want checked status data", doneMeta.Status)
	}
	if !strings.Contains(doneBody, "merged PR #729 to main") {
		t.Fatalf("done comment body should name the merged PR, got: %s", doneBody)
	}
	if !strings.Contains(doneBody, "checked head sha: abc123") || !strings.Contains(doneBody, "status context: ci/build=success, ci/test=success") {
		t.Fatalf("done comment should name the checked head SHA and status context, got: %s", doneBody)
	}
}

func TestDirectorMergeEligibilityRequiresMatchingQAVerdict(t *testing.T) {
	currentQA := qaVerdictCommentFrom(modeClaude, qaThoroughness{}, qaFamilyInternal, "inspect the branch", qaLaunchContext{
		IssueRef:       "coilyco-flight-deck/ward#729",
		PRRef:          "coilyco-flight-deck/ward#729",
		ReviewedSHA:    "abc123",
		ReviewerFamily: qaFamilyInternal,
		Workflow:       string(workflowPullRequestAndMerge),
		RunIdentity:    "ward-qa-1",
	}, qaVerdict{Verdict: "pass", Summary: "checks are green"})
	staleQA := qaVerdictCommentFrom(modeClaude, qaThoroughness{}, qaFamilyInternal, "inspect the branch", qaLaunchContext{
		IssueRef:       "coilyco-flight-deck/ward#729",
		PRRef:          "coilyco-flight-deck/ward#729",
		ReviewedSHA:    "deadbeef",
		ReviewerFamily: qaFamilyInternal,
		Workflow:       string(workflowPullRequestAndMerge),
		RunIdentity:    "ward-qa-2",
	}, qaVerdict{Verdict: "fail", Summary: "stale result"})
	prsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token secret" {
			t.Fatalf("auth header = %q, want token secret", got)
		}
		switch r.URL.Path {
		case "/api/v1/repos/coilyco-flight-deck/ward/issues/729":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number":   729,
				"title":    "ship the fix",
				"body":     "closes #729",
				"state":    "open",
				"html_url": "https://f/729",
				"labels":   []map[string]any{},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/issues/729/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"body": "WARD-OUTCOME: merge-ready\n\n<details><summary>details</summary>\n\nworkflow: pull-request-and-merge; review summary: passed: all green\n\n</details>", "created_at": "2026-07-09T00:00:00Z", "user": map[string]any{"login": "coilyco-ops"}},
				{"body": currentQA, "created_at": "2026-07-09T00:05:00Z", "user": map[string]any{"login": "coilyco-ops"}},
				{"body": staleQA, "created_at": "2026-07-09T00:10:00Z", "user": map[string]any{"login": "coilyco-ops"}},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/pulls/729":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number":   729,
				"title":    "ship the fix",
				"body":     "closes #729\n" + directorMergeWorkflowMarker + "\n",
				"state":    "open",
				"draft":    false,
				"html_url": "https://f/pr/729",
				"head": map[string]any{
					"sha": "abc123",
					"ref": "issue-729",
				},
				"base": map[string]any{
					"ref": "main",
				},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/branches/main":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":                  "main",
				"protected":             true,
				"enable_status_check":   true,
				"status_check_contexts": []string{"ci/build", "ci/test"},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/commits/abc123/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":       "success",
				"sha":         "abc123",
				"total_count": 2,
				"statuses": []map[string]any{
					{"context": "ci/build", "state": "success"},
					{"context": "ci/test", "state": "success"},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer prsrv.Close()
	cl := &forgejoClient{baseURL: prsrv.URL, token: "secret"}

	allowed, reason, linked, meta := directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward",
		directorPullRequest{Issue: dispatch.Issue{Number: 729, Title: "ship the fix", Body: "closes #729\n" + directorMergeWorkflowMarker + "\n"}, Mergeable: true, MergeableKnown: true}, cl, cl)
	if !allowed || reason != "" || linked != 729 {
		t.Fatalf("eligible PR = %v %q %d, want true/\"\"/729", allowed, reason, linked)
	}
	if meta.Workflow != string(workflowPullRequestAndMerge) || meta.Review != "passed: all green" || !meta.HasOutcome {
		t.Fatalf("eligible PR meta = %+v, want merge-lane metadata", meta)
	}
	if meta.Status.HeadSHA != "abc123" || meta.Status.State != "success" || len(meta.Status.Checks) != 2 {
		t.Fatalf("eligible PR status = %+v, want live status summary", meta.Status)
	}
	doneBody := directorMergeDoneComment(729, meta)
	if !strings.Contains(doneBody, "checked head sha: abc123") || !strings.Contains(doneBody, "status context: ci/build=success, ci/test=success") {
		t.Fatalf("done comment should name the checked status details, got: %s", doneBody)
	}

	allowed, reason, _, _ = directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward",
		directorPullRequest{Issue: dispatch.Issue{Number: 729, Title: "ship the fix", Body: "closes #729\n"}, Mergeable: true, MergeableKnown: true}, cl, cl)
	if allowed {
		t.Fatal("unmarked PR: want deny, got allow")
	}
	if reason != "PR body missing ward.workflow: pull-requests-and-merge marker" {
		t.Fatalf("unmarked PR reason = %q, want missing-marker denial", reason)
	}
}

func TestDirectorMergeEligibilityRejectsMissingRequiredStatus(t *testing.T) {
	cl, pr := directorMergeEligibilityFixture(t, "abc123", map[string]string{
		"ci/build": "success",
	}, "success")
	allowed, reason, _, _ := directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward", pr, cl, cl)
	if allowed {
		t.Fatal("missing status: want deny, got allow")
	}
	if !strings.Contains(reason, "missing required status context ci/test") {
		t.Fatalf("missing status reason = %q, want missing context denial", reason)
	}
}

func TestDirectorMergeEligibilityRejectsFailingRequiredStatus(t *testing.T) {
	cl, pr := directorMergeEligibilityFixture(t, "abc123", map[string]string{
		"ci/build": "success",
		"ci/test":  "failure",
	}, "failure")
	allowed, reason, _, _ := directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward", pr, cl, cl)
	if allowed {
		t.Fatal("failing status: want deny, got allow")
	}
	if !strings.Contains(reason, "has required status context ci/test=failure") {
		t.Fatalf("failing status reason = %q, want failing context denial", reason)
	}
}

func TestDirectorMergeEligibilityRejectsStaleRequiredStatus(t *testing.T) {
	cl, pr := directorMergeEligibilityFixture(t, "def456", map[string]string{
		"ci/build": "success",
	}, "success")
	allowed, reason, _, _ := directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward", pr, cl, cl)
	if allowed {
		t.Fatal("stale status: want deny, got allow")
	}
	if !strings.Contains(reason, "linked PR head SHA def456 is missing required status context ci/test") {
		t.Fatalf("stale status reason = %q, want current-head denial", reason)
	}
}

func TestDirectorMergeEligibilityFallsBackWhenBaseBranchHasNoRequiredStatusContexts(t *testing.T) {
	cl, pr := directorMergeEligibilityFixtureWithBranchProtection(t, "abc123", map[string]string{
		"test / test (pull_request)": "success",
	}, "success", false, nil)
	allowed, reason, linked, meta := directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward", pr, cl, cl)
	if !allowed || reason != "" || linked != 729 {
		t.Fatalf("fallback PR = %v %q %d, want true/\"\"/729", allowed, reason, linked)
	}
	if meta.Status.HeadSHA != "abc123" || meta.Status.State != "success" || len(meta.Status.Checks) != 2 {
		t.Fatalf("fallback status = %+v, want live status summary", meta.Status)
	}
	want := map[string]bool{
		"ci/build=success":                   false,
		"test / test (pull_request)=success": false,
	}
	for _, got := range meta.Status.Checks {
		want[got.Context+"="+got.State] = true
	}
	for key, seen := range want {
		if !seen {
			t.Fatalf("fallback status missing %s in %+v", key, meta.Status.Checks)
		}
	}
}

func directorMergeEligibilityFixture(t *testing.T, headSHA string, contexts map[string]string, combinedState string) (*forgejoClient, directorPullRequest) {
	return directorMergeEligibilityFixtureWithBranchProtection(t, headSHA, contexts, combinedState, true, []string{"ci/build", "ci/test"})
}

func directorMergeEligibilityFixtureWithBranchProtection(t *testing.T, headSHA string, contexts map[string]string, combinedState string, enableStatusCheck bool, branchContexts []string) (*forgejoClient, directorPullRequest) {
	t.Helper()
	if combinedState == "" {
		combinedState = "success"
	}
	if len(contexts) == 0 {
		contexts = map[string]string{"ci/build": "success", "ci/test": "success"}
	}
	if _, ok := contexts["ci/build"]; !ok {
		contexts["ci/build"] = "success"
	}
	currentQA := qaVerdictCommentFrom(modeClaude, qaThoroughness{}, qaFamilyInternal, "inspect the branch", qaLaunchContext{
		IssueRef:       "coilyco-flight-deck/ward#729",
		PRRef:          "coilyco-flight-deck/ward#729",
		ReviewedSHA:    headSHA,
		ReviewerFamily: qaFamilyInternal,
		Workflow:       string(workflowPullRequestAndMerge),
		RunIdentity:    "ward-qa-1",
	}, qaVerdict{Verdict: "pass", Summary: "checks are green"})
	staleQA := qaVerdictCommentFrom(modeClaude, qaThoroughness{}, qaFamilyInternal, "inspect the branch", qaLaunchContext{
		IssueRef:       "coilyco-flight-deck/ward#729",
		PRRef:          "coilyco-flight-deck/ward#729",
		ReviewedSHA:    "deadbeef",
		ReviewerFamily: qaFamilyInternal,
		Workflow:       string(workflowPullRequestAndMerge),
		RunIdentity:    "ward-qa-2",
	}, qaVerdict{Verdict: "fail", Summary: "stale result"})
	prsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token secret" {
			t.Fatalf("auth header = %q, want token secret", got)
		}
		switch r.URL.Path {
		case "/api/v1/repos/coilyco-flight-deck/ward/issues/729":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number":   729,
				"title":    "ship the fix",
				"body":     "closes #729",
				"state":    "open",
				"html_url": "https://f/729",
				"labels":   []map[string]any{},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/issues/729/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"body": "WARD-OUTCOME: merge-ready\n\n<details><summary>details</summary>\n\nworkflow: pull-request-and-merge; review summary: passed: all green\n\n</details>", "created_at": "2026-07-09T00:00:00Z", "user": map[string]any{"login": "coilyco-ops"}},
				{"body": currentQA, "created_at": "2026-07-09T00:05:00Z", "user": map[string]any{"login": "coilyco-ops"}},
				{"body": staleQA, "created_at": "2026-07-09T00:10:00Z", "user": map[string]any{"login": "coilyco-ops"}},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/pulls/729":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number":   729,
				"title":    "ship the fix",
				"body":     "closes #729\n" + directorMergeWorkflowMarker + "\n",
				"state":    "open",
				"draft":    false,
				"html_url": "https://f/pr/729",
				"head": map[string]any{
					"sha": headSHA,
					"ref": "issue-729",
				},
				"base": map[string]any{
					"ref": "main",
				},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/branches/main":
			contexts := branchContexts
			if contexts == nil {
				contexts = []string{}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":                  "main",
				"protected":             true,
				"enable_status_check":   enableStatusCheck,
				"status_check_contexts": contexts,
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/commits/" + headSHA + "/status":
			statuses := make([]map[string]any, 0, len(contexts))
			for ctx, state := range contexts {
				statuses = append(statuses, map[string]any{"context": ctx, "state": state})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":       combinedState,
				"sha":         headSHA,
				"total_count": len(statuses),
				"statuses":    statuses,
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/commits/abc123/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":       "success",
				"sha":         "abc123",
				"total_count": 2,
				"statuses": []map[string]any{
					{"context": "ci/build", "state": "success"},
					{"context": "ci/test", "state": "success"},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(prsrv.Close)
	return &forgejoClient{baseURL: prsrv.URL, token: "secret"},
		directorPullRequest{Issue: dispatch.Issue{Number: 729, Title: "ship the fix", Body: "closes #729\n" + directorMergeWorkflowMarker + "\n"}, Mergeable: true, MergeableKnown: true}
}

func TestDirectorMergeEligibilityRejectsMergeConflict(t *testing.T) {
	cl := &forgejoClient{}

	allowed, reason, linked, meta := directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward",
		directorPullRequest{Issue: dispatch.Issue{Number: 729, Title: "ship the fix", Body: "closes #729\n" + directorMergeWorkflowMarker + "\n"}, Mergeable: false, MergeableKnown: true}, cl, cl)
	if allowed {
		t.Fatal("conflicting PR: want deny, got allow")
	}
	if linked != 729 {
		t.Fatalf("conflicting PR linked issue = %d, want 729", linked)
	}
	if reason != "PR is not mergeable against the current base branch; rebase or merge base and resolve the conflict first" {
		t.Fatalf("conflicting PR reason = %q, want merge-conflict denial", reason)
	}
	if meta.HasOutcome {
		t.Fatalf("conflicting PR should not need issue metadata, got %+v", meta)
	}
}

func TestListOpenPullRequestsReadsMergeability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/coilyco-flight-deck/ward/issues":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"number": 701, "title": "mergeable", "body": "closes #701", "state": "open", "html_url": "https://f/701", "labels": []map[string]any{}, "pull_request": map[string]any{"url": "https://f/701"}},
				{"number": 702, "title": "conflicted", "body": "closes #702", "state": "open", "html_url": "https://f/702", "labels": []map[string]any{}, "pull_request": map[string]any{"url": "https://f/702"}},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/pulls/701":
			if got := r.Header.Get("Authorization"); got != "token secret" {
				t.Fatalf("auth header = %q, want token secret", got)
			}
			_, _ = w.Write([]byte(`{"mergeable":true}`))
		case "/api/v1/repos/coilyco-flight-deck/ward/pulls/702":
			_, _ = w.Write([]byte(`{"mergeable":false}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	prs, err := cl.listOpenPullRequests(context.Background(), "coilyco-flight-deck", "ward", 50)
	if err != nil {
		t.Fatalf("listOpenPullRequests: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("listOpenPullRequests len = %d, want 2", len(prs))
	}
	if !prs[0].Mergeable || prs[1].Mergeable {
		t.Fatalf("mergeability projection = [%v %v], want [true false]", prs[0].Mergeable, prs[1].Mergeable)
	}
}

func TestDirectorMergeEligibilitySkipsUnknownMergeability(t *testing.T) {
	ok, reason, linked, _ := directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward",
		directorPullRequest{Issue: dispatch.Issue{Title: "ship the fix", Body: "closes #729\n" + directorMergeWorkflowMarker + "\n"}}, &forgejoClient{}, &forgejoClient{})
	if ok {
		t.Fatal("unknown mergeability: want skip, got allow")
	}
	if linked != 729 {
		t.Fatalf("linked issue = %d, want 729", linked)
	}
	if reason != "could not read PR mergeability" {
		t.Fatalf("reason = %q, want unknown-mergeability skip", reason)
	}
}
