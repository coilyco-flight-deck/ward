package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDirectorMergeDecision(t *testing.T) {
	basePR := Issue{
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
		pr   Issue
		meta directorRunMeta
		want string
	}{
		{
			name: "salvage",
			pr:   Issue{Title: "ward salvage: residual cleanup", Body: "closes #729"},
			meta: baseMeta,
			want: "salvage PRs are cleanup noise, not merge-authorized work",
		},
		{
			name: "wip",
			pr:   Issue{Title: "WIP: ship the fix", Body: "closes #729"},
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
			name: "needs-internal-family",
			pr:   basePR,
			meta: directorRunMeta{HasOutcome: true, Outcome: backlogOutcome{Status: "merge-ready"}, Workflow: string(workflowPullRequestAndMerge), Review: "blocked: concern"},
			want: "linked issue QA verdict was not from the internal reviewer family",
		},
		{
			name: "needs-ready-state",
			pr:   basePR,
			meta: directorRunMeta{HasOutcome: true, Outcome: backlogOutcome{Status: "submitted"}, Workflow: string(workflowPullRequestAndMerge), Review: "passed: ok"},
			want: "linked issue did not finish with WARD-WORKFLOW: merge-ready",
		},
		{
			name: "done-is-not-merge-ready",
			pr:   basePR,
			meta: directorRunMeta{HasOutcome: true, Outcome: backlogOutcome{Status: "done"}, Workflow: string(workflowPullRequestAndMerge), Review: "passed: ok"},
			want: "linked issue did not finish with WARD-WORKFLOW: merge-ready",
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

func TestDirectorMergeEligibilityBlocksOnHumanFeedback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				{"body": "WARD-WORKFLOW: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/729", "created_at": "2026-07-09T00:00:00Z", "user": map[string]any{"login": "coilyco-ops"}},
				{"body": "this is still missing the actual need", "created_at": "2026-07-09T00:10:00Z", "user": map[string]any{"login": "coilysiren"}},
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
				"name":                "main",
				"protected":           true,
				"enable_status_check": true,
				"status_check_contexts": []string{
					"ci/build",
				},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/commits/abc123/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":       "success",
				"sha":         "abc123",
				"total_count": 1,
				"statuses": []map[string]any{
					{"context": "ci/build", "state": "success"},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	allowed, reason, _, _ := directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward",
		directorPullRequest{Issue: Issue{Number: 729, Title: "ship the fix", Body: "closes #729\n" + directorMergeWorkflowMarker + "\n"}, Mergeable: true, MergeableKnown: true}, cl, cl)
	if allowed {
		t.Fatal("human-feedback PR: want deny, got allow")
	}
	if !strings.Contains(reason, "human comment by @coilysiren") {
		t.Fatalf("reason = %q, want human-feedback denial", reason)
	}
}

func TestRecordDirectorMergeDoneBlocksOnHumanFeedback(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	f := &prWorkflowFakeForge{
		updatedAt: now,
		comments: []issueComment{
			{
				Body:      "WARD-WORKFLOW: done ✅",
				CreatedAt: now,
				User: struct {
					Login string `json:"login"`
				}{Login: forgeForgejo.gitPushUser()},
			},
			{
				Body:      "this still needs attention",
				CreatedAt: now.Add(5 * time.Minute),
				User: struct {
					Login string `json:"login"`
				}{Login: "repo-owner"},
			},
		},
	}
	srv := f.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	meta := directorRunMeta{
		Workflow:   string(workflowPullRequestAndMerge),
		Review:     "passed: all green",
		Outcome:    backlogOutcome{Status: "merge-ready"},
		HasOutcome: true,
		IssueRef:   "coilyco-flight-deck/ward#7",
		PRRef:      "coilyco-flight-deck/ward#7",
		PRHeadSHA:  "abc123",
		Status:     directorMergeStatusSummary{HeadSHA: "abc123", State: "success"},
	}
	if err := recordDirectorMergeDone(context.Background(), cl.withMode(modeGoose), cl, "coilyco-flight-deck", "ward", 7, 7, meta); err == nil || !strings.Contains(err.Error(), "human feedback remains newer") {
		t.Fatalf("recordDirectorMergeDone error = %v, want human-feedback block", err)
	}
}

func TestDirectorLinkedIssueNumber(t *testing.T) {
	for _, tc := range []struct {
		body string
		want int
		ok   bool
	}{
		{body: "closes #12", want: 12, ok: true},
		{body: "closes coilyco-flight-deck/ward#12", want: 12, ok: true},
		{body: "closes coilyco-flight-deck/umbra#12\ncloses coilyco-flight-deck/ward#13", want: 13, ok: true},
		{body: "Fixes #7\n", want: 7, ok: true},
		{body: "resolves #3", want: 3, ok: true},
		{body: "closes coilyco-flight-deck/umbra#12", want: 0, ok: false},
		{body: "notes only", want: 0, ok: false},
	} {
		got, ok := directorLinkedIssueNumber("coilyco-flight-deck", "ward", tc.body)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("%q => (%d,%v), want (%d,%v)", tc.body, got, ok, tc.want, tc.ok)
		}
	}
}

func TestMergePullRequestRequestShape(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		defaultDeleteBranchAfterMerge bool
	}{
		{name: "preserves-false", defaultDeleteBranchAfterMerge: false},
		{name: "preserves-true", defaultDeleteBranchAfterMerge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotToken, gotMethod, gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward" {
					_, _ = w.Write([]byte(`{"allow_merge_commits":true,"default_merge_style":"merge","default_delete_branch_after_merge":` + jsonBool(tc.defaultDeleteBranchAfterMerge) + `}`))
					return
				}
				if r.URL.Path != "/api/v1/repos/coilyco-flight-deck/ward/pulls/729/merge" {
					t.Fatalf("path = %q, want merge endpoint", r.URL.Path)
				}
				gotToken = r.Header.Get("Authorization")
				gotMethod = r.Method
				gotPath = r.URL.Path
				var body struct {
					Do                     string `json:"do"`
					DeleteBranchAfterMerge bool   `json:"delete_branch_after_merge"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body.Do != "merge" {
					t.Fatalf("body do = %q, want merge", body.Do)
				}
				if body.DeleteBranchAfterMerge != tc.defaultDeleteBranchAfterMerge {
					t.Fatalf("body delete_branch_after_merge = %t, want %t", body.DeleteBranchAfterMerge, tc.defaultDeleteBranchAfterMerge)
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
		})
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
	var gotBody struct {
		Do                     string `json:"do"`
		HeadCommitID           string `json:"head_commit_id"`
		DeleteBranchAfterMerge bool   `json:"delete_branch_after_merge"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/repos/coilyco-flight-deck/ward" {
			_, _ = w.Write([]byte(`{"allow_merge_commits":true,"default_merge_style":"merge","default_delete_branch_after_merge":true}`))
			return
		}
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
	if gotBody.Do != "merge" {
		t.Fatalf("body do = %q, want merge", gotBody.Do)
	}
	if gotBody.HeadCommitID != "abc123" {
		t.Fatalf("body head_commit_id = %q, want abc123", gotBody.HeadCommitID)
	}
	if !gotBody.DeleteBranchAfterMerge {
		t.Fatalf("body delete_branch_after_merge = %t, want true", gotBody.DeleteBranchAfterMerge)
	}
}

func TestDirectorRunMetaParsesWorkflowAndReview(t *testing.T) {
	body := strings.Join([]string{
		"WARD-OUTCOME: merge-ready",
		"",
		"<details><summary>details</summary>",
		"",
		"workflow: pull-request-and-merge; review summary: passed: all green",
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
	if meta.MergeAuthorization != "" {
		t.Fatalf("legacy meta merge authorization = %q, want empty", meta.MergeAuthorization)
	}
	if meta.Status.HeadSHA != "abc123" || meta.Status.State != "success" || len(meta.Status.Checks) != 2 {
		t.Fatalf("meta status = %+v, want checked status data", meta.Status)
	}
	if _, ok := backlogOutcomeOfComment(body); !ok {
		t.Fatal("comment body should parse as an outcome comment")
	}
	urlBody := strings.Join([]string{
		"WARD-WORKFLOW: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/729",
		"",
		"<details><summary>details</summary>",
		"",
		"director merge authorization: reviewed-and-ready",
		"workflow: pull-request-and-merge; review summary: passed: all green",
		"checked head sha: abc123",
		"status context: ci/build=success, ci/test=success",
		"status state: success",
		"",
		"</details>",
	}, "\n")
	urlMeta := parseDirectorRunMeta(urlBody)
	if !urlMeta.HasOutcome || urlMeta.Outcome.Status != "merge-ready" {
		t.Fatalf("url meta outcome = %+v, want merge-ready", urlMeta)
	}
	if urlMeta.MergeAuthorization != "reviewed-and-ready" {
		t.Fatalf("url meta merge authorization = %q, want reviewed-and-ready", urlMeta.MergeAuthorization)
	}
	if urlMeta.Outcome.PRNumber != 729 {
		t.Fatalf("url meta PR number = %d, want 729", urlMeta.Outcome.PRNumber)
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

func TestDirectorMergeDecisionRejectsSkippedReview(t *testing.T) {
	body := strings.Join([]string{
		"WARD-WORKFLOW: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/729",
		"",
		"<details><summary>details</summary>",
		"",
		"workflow: pull-request-and-merge; review summary: review gate skipped by ~/.ward/config.yaml default",
		"checked head sha: abc123",
		"status context: ci/build=success, ci/test=success",
		"status state: success",
		"",
		"</details>",
	}, "\n")
	meta := parseDirectorRunMeta(body)
	if !meta.HasOutcome || meta.Outcome.Status != "submitted" {
		t.Fatalf("meta outcome = %+v, want submitted", meta)
	}
	if meta.Outcome.PRNumber != 729 {
		t.Fatalf("meta outcome PRNumber = %d, want 729", meta.Outcome.PRNumber)
	}
	if meta.Workflow != string(workflowPullRequestAndMerge) {
		t.Fatalf("meta workflow = %q, want %q", meta.Workflow, workflowPullRequestAndMerge)
	}
	if meta.Review != "review gate skipped by ~/.ward/config.yaml default" {
		t.Fatalf("meta review = %q, want skipped-review summary", meta.Review)
	}
	if meta.MergeAuthorization != "" {
		t.Fatalf("skipped-review merge authorization = %q, want empty", meta.MergeAuthorization)
	}
	allowed, reason, _, _ := directorMergeDecision(Issue{Title: "ship the fix", Body: "closes #729\n"}, 729, meta)
	if allowed {
		t.Fatal("skipped-review run: want deny, got allow")
	}
	if reason != "linked issue did not finish with WARD-WORKFLOW: merge-ready" {
		t.Fatalf("skipped-review reason = %q, want merge-ready denial", reason)
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
				{"body": "WARD-WORKFLOW: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/729\n\n<details><summary>details</summary>\n\ndirector merge authorization: reviewed-and-ready\nworkflow: pull-request-and-merge; review summary: passed: all green\n\n</details>", "created_at": "2026-07-09T00:00:00Z", "user": map[string]any{"login": "coilyco-ops"}},
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
		directorPullRequest{Issue: Issue{Number: 729, Title: "ship the fix", Body: "closes #729\n" + directorMergeWorkflowMarker + "\n"}, Mergeable: true, MergeableKnown: true}, cl, cl)
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
		directorPullRequest{Issue: Issue{Number: 729, Title: "ship the fix", Body: "closes #729\n"}, Mergeable: true, MergeableKnown: true}, cl, cl)
	if allowed {
		t.Fatal("unmarked PR: want deny, got allow")
	}
	if reason != "PR body missing ward.workflow: pull-request-and-merge marker" {
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

func TestDirectorMergeEligibilityUsesLatestStatusHistoryEntry(t *testing.T) {
	var currentQA, staleQA string
	currentQA = qaVerdictCommentFrom(modeClaude, qaThoroughness{}, qaFamilyInternal, "inspect the branch", qaLaunchContext{
		IssueRef:       "coilyco-flight-deck/ward#729",
		PRRef:          "coilyco-flight-deck/ward#729",
		ReviewedSHA:    "abc123",
		ReviewerFamily: qaFamilyInternal,
		Workflow:       string(workflowPullRequestAndMerge),
		RunIdentity:    "ward-qa-1",
	}, qaVerdict{Verdict: "pass", Summary: "checks are green"})
	staleQA = qaVerdictCommentFrom(modeClaude, qaThoroughness{}, qaFamilyInternal, "inspect the branch", qaLaunchContext{
		IssueRef:       "coilyco-flight-deck/ward#729",
		PRRef:          "coilyco-flight-deck/ward#729",
		ReviewedSHA:    "deadbeef",
		ReviewerFamily: qaFamilyInternal,
		Workflow:       string(workflowPullRequestAndMerge),
		RunIdentity:    "ward-qa-2",
	}, qaVerdict{Verdict: "fail", Summary: "stale result"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				{"body": "WARD-WORKFLOW: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/729\n\n<details><summary>details</summary>\n\ndirector merge authorization: reviewed-and-ready\nworkflow: pull-request-and-merge; review summary: passed: all green\n\n</details>", "created_at": "2026-07-09T00:00:00Z", "user": map[string]any{"login": "coilyco-ops"}},
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
				"name":                "main",
				"protected":           true,
				"enable_status_check": true,
				"status_check_contexts": []string{
					"test / test (pull_request)",
				},
			})
		case "/api/v1/repos/coilyco-flight-deck/ward/commits/abc123/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":       "success",
				"sha":         "abc123",
				"total_count": 2,
				"statuses": []map[string]any{
					{"context": "test / test (pull_request)", "state": "pending"},
					{"context": "test / test (pull_request)", "status": "success", "description": "Successful in 28s"},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	allowed, reason, linked, meta := directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward",
		directorPullRequest{Issue: Issue{Number: 729, Title: "ship the fix", Body: "closes #729\n" + directorMergeWorkflowMarker + "\n"}, Mergeable: true, MergeableKnown: true}, cl, cl)
	if !allowed || reason != "" || linked != 729 {
		t.Fatalf("eligible PR = %v %q %d, want true/\"\"/729", allowed, reason, linked)
	}
	if len(meta.Status.Checks) != 1 || meta.Status.Checks[0].Context != "test / test (pull_request)" || meta.Status.Checks[0].State != "success" {
		t.Fatalf("eligible PR status = %+v, want latest successful status", meta.Status)
	}
	doneBody := directorMergeDoneComment(729, meta)
	if !strings.Contains(doneBody, "status context: test / test (pull_request)=success") {
		t.Fatalf("done comment should name the latest successful status, got: %s", doneBody)
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

func TestBuildDirectorMergeStatusSummaryReportsLatestStateValue(t *testing.T) {
	combined := &forgejoCommitCombinedStatus{
		State: "failure",
		Statuses: []forgejoCommitStatus{
			{Context: "ci/test", State: "pending"},
			{Context: "ci/test", Status: "failure"},
		},
	}
	summary, reason, ok := buildDirectorMergeStatusSummary("abc123", "main", []string{"ci/test"}, combined)
	if ok {
		t.Fatalf("summary = %+v, want denial", summary)
	}
	if summary.State != "failure" {
		t.Fatalf("summary state = %q, want failure", summary.State)
	}
	if !strings.Contains(reason, "ci/test=failure") {
		t.Fatalf("reason = %q, want failure value", reason)
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
				{"body": "WARD-WORKFLOW: https://forgejo.coilysiren.me/coilyco-flight-deck/ward/pulls/729\n\n<details><summary>details</summary>\n\ndirector merge authorization: reviewed-and-ready\nworkflow: pull-request-and-merge; review summary: passed: all green\n\n</details>", "created_at": "2026-07-09T00:00:00Z", "user": map[string]any{"login": "coilyco-ops"}},
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
		directorPullRequest{Issue: Issue{Number: 729, Title: "ship the fix", Body: "closes #729\n" + directorMergeWorkflowMarker + "\n"}, Mergeable: true, MergeableKnown: true}
}

func TestDirectorMergeEligibilityRejectsMergeConflict(t *testing.T) {
	allowed, reason, linked, meta := directorMergeEligibility(context.Background(), "coilyco-flight-deck", "ward",
		directorPullRequest{Issue: Issue{Number: 729, Title: "ship the fix", Body: "closes #729\n" + directorMergeWorkflowMarker + "\n"}, Mergeable: false, MergeableKnown: true}, &forgejoClient{}, mergeConflictTracker{})
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

type mergeConflictTracker struct{}

func (mergeConflictTracker) GetIssue(context.Context, string, string, int) (*Issue, error) {
	return &Issue{}, nil
}

func (mergeConflictTracker) ListIssueComments(context.Context, string, string, int) ([]issueComment, error) {
	return nil, context.Canceled
}

func (mergeConflictTracker) CreateIssue(context.Context, string, string, string, string) (int, error) {
	return 0, nil
}

func (mergeConflictTracker) CommentIssue(context.Context, string, string, int, string) error {
	return nil
}
func (mergeConflictTracker) DeleteIssueComment(context.Context, string, string, int) error {
	return nil
}
func (mergeConflictTracker) CloseIssue(context.Context, string, string, int) error  { return nil }
func (mergeConflictTracker) ReopenIssue(context.Context, string, string, int) error { return nil }
func (mergeConflictTracker) LockIssue(context.Context, string, string, int) error   { return nil }
func (mergeConflictTracker) UnlockIssue(context.Context, string, string, int) error { return nil }

func TestDirectorMergeConflictReasonFromComments(t *testing.T) {
	now := time.Date(2026, 7, 10, 18, 0, 0, 0, time.UTC)
	pr := directorPullRequest{
		Issue:     Issue{Number: 729, Title: "ship the fix", Body: "closes #729\n" + directorMergeWorkflowMarker + "\n"},
		UpdatedAt: now.Add(-30 * time.Minute),
	}
	active := directorMergeConflictReasonFromComments(pr, nil, now)
	if !strings.Contains(active, "active worker branch with no WARD-WORKFLOW yet") || !strings.Contains(active, "30m ago") {
		t.Fatalf("active reason = %q, want active worker classification", active)
	}

	pr.UpdatedAt = now.Add(-3 * time.Hour)
	stale := directorMergeConflictReasonFromComments(pr, nil, now)
	if !strings.Contains(stale, "stale worker branch with no WARD-WORKFLOW yet") || !strings.Contains(stale, "3h0m ago") {
		t.Fatalf("stale reason = %q, want stale worker classification", stale)
	}

	blocked := directorMergeConflictReasonFromComments(pr, []issueComment{machineComment(strings.Join([]string{
		"WARD-OUTCOME: blocked 🛑",
		"",
		"<details><summary>details</summary>",
		"",
		"workflow: pull-request-and-merge; review summary: review gate skipped by ~/.ward/config.yaml default",
		"",
		"</details>",
	}, "\n"), now.Add(-time.Minute))}, now)
	if !strings.Contains(blocked, "linked issue is blocked") || !strings.Contains(blocked, "review gate skipped by ~/.ward/config.yaml default") {
		t.Fatalf("blocked reason = %q, want review-blocked classification", blocked)
	}
}

func TestRecoverClosedUnmergedDirectorMergeReopensAndRetries(t *testing.T) {
	markMergedOnSuccess := true
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		prState:                   "closed",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
		markMergedOnSuccess:       &markMergedOnSuccess,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	postErr := &prMergePostconditionError{Owner: "coilyco-flight-deck", Repo: "ward", Index: 7, State: "closed", HeadSHA: "headsha"}
	head, err := recoverClosedUnmergedDirectorMerge(context.Background(), cl, "coilyco-flight-deck", "ward", 7, "", postErr)
	if err != nil {
		t.Fatalf("recoverClosedUnmergedDirectorMerge: %v", err)
	}
	if head != "headsha" {
		t.Fatalf("head = %q, want headsha", head)
	}
	if fake.mergedChecks != 1 {
		t.Fatalf("merged-state checks = %d, want 1", fake.mergedChecks)
	}
	if fake.mergeCalls != 1 {
		t.Fatalf("merge calls = %d, want 1", fake.mergeCalls)
	}
	if fake.prState != "closed" {
		t.Fatalf("PR state = %q, want closed after retry lands", fake.prState)
	}
}

func TestMergeDirectorPullRequestIgnoresEmptySummaryWhenTreesDiffer(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		prDiffKnown:               true,
		prAdditions:               0,
		prDeletions:               0,
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
		branchCommitSHA:           "basesha",
		commitTrees: map[string]string{
			"headsha": "head-tree",
			"basesha": "base-tree",
		},
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	status := &directorMergeStatusCheck{}
	head, err := mergeDirectorPullRequest(context.Background(), cl, "coilyco-flight-deck", "ward", 7, "headsha", "", status)
	if err != nil {
		t.Fatalf("mergeDirectorPullRequest: %v", err)
	}
	if head != "headsha" {
		t.Fatalf("head = %q, want headsha", head)
	}
	if fake.mergeCalls != 1 {
		t.Fatalf("merge calls = %d, want 1", fake.mergeCalls)
	}
}

func TestListOpenPullRequestsReadsMergeability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/coilyco-flight-deck/ward/issues":
			if got := r.URL.Query().Get("state"); got != "open" {
				t.Fatalf("state query = %q, want open", got)
			}
			if got := r.URL.Query().Get("type"); got != "pulls" {
				t.Fatalf("type query = %q, want pulls", got)
			}
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
	prs, err := cl.ListOpenPullRequests(context.Background(), "coilyco-flight-deck", "ward", 50)
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
		directorPullRequest{Issue: Issue{Title: "ship the fix", Body: "closes #729\n" + directorMergeWorkflowMarker + "\n"}}, &forgejoClient{}, &forgejoClient{})
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
