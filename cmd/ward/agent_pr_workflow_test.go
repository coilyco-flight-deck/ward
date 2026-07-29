package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestPRWorkflowMutationBoundaryIsRoleIndependent proves the fixed broker gate
// reads workflow mechanics, never behavioral role metadata.
func TestPRWorkflowMutationBoundaryIsRoleIndependent(t *testing.T) {
	cases := []struct {
		role    string
		wf      workflowMode
		allowed bool
	}{
		{roleDirector, workflowPullRequest, false},
		{roleDirector, workflowPullRequestAndMerge, true},
		{roleDirector, workflowRemoteBranchOnly, false},
		{roleDirector, workflowDirectToMain, false},
		{roleEngineer, workflowPullRequest, false},
		{roleEngineer, workflowPullRequestAndMerge, true},
		{roleEngineer, workflowRemoteBranchOnly, false},
		{roleEngineer, workflowDirectToMain, false},
		{roleQA, workflowPullRequest, false},
		{roleQA, workflowPullRequestAndMerge, true},
		{"external-role", workflowPullRequestAndMerge, true},
	}
	for _, tc := range cases {
		err := prWorkflowPermitted(tc.role, tc.wf, prOpMerge)
		if tc.allowed && err != nil {
			t.Errorf("prWorkflowPermitted(%s, %s, merge) = %v, want allowed", tc.role, tc.wf, err)
		}
		if !tc.allowed && err == nil {
			t.Errorf("prWorkflowPermitted(%s, %s, merge) = nil, want denied", tc.role, tc.wf)
		}
	}
}

// TestPRWorkflowReadAndRerunGates pins the role-independent fixed broker ops.
func TestPRWorkflowReadAndRerunGates(t *testing.T) {
	for _, role := range []string{roleEngineer, roleDirector, roleQA, "external-role"} {
		for _, op := range []prWorkflowOp{prOpStatus, prOpRuns, prOpRecover, prOpRerun} {
			if err := prWorkflowPermitted(role, "", op); err != nil {
				t.Errorf("prWorkflowPermitted(%s, %s) = %v, want allowed", role, op, err)
			}
		}
	}
	for _, op := range []prWorkflowOp{prOpClose, prOpReopen} {
		if err := prWorkflowPermitted(roleDirector, workflowPullRequest, op); err == nil {
			t.Errorf("prWorkflowPermitted(%s, pull-request, %s) = nil, want denied", roleDirector, op)
		}
		if err := prWorkflowPermitted(roleEngineer, workflowPullRequestAndMerge, op); err != nil {
			t.Errorf("prWorkflowPermitted(%s, pull-request-and-merge, %s) = %v, want allowed", roleEngineer, op, err)
		}
		if err := prWorkflowPermitted("external-role", workflowPullRequestAndMerge, op); err != nil {
			t.Errorf("prWorkflowPermitted(external-role, pull-request-and-merge, %s) = %v, want allowed", op, err)
		}
	}
}

// TestPRWorkflowMarkerMode pins the marker fallback: a PR without a
// ward.workflow marker is the plain pull-request lane.
func TestPRWorkflowMarkerMode(t *testing.T) {
	if got := prWorkflowMarkerMode("closes #12\n\nward.workflow: pull-request-and-merge\n"); got != workflowPullRequestAndMerge {
		t.Errorf("marker mode = %s, want %s", got, workflowPullRequestAndMerge)
	}
	if got := prWorkflowMarkerMode("closes #12\n\nward.workflow: pull-request-and-merge\n"); got != workflowPullRequestAndMerge {
		t.Errorf("legacy marker mode = %s, want %s", got, workflowPullRequestAndMerge)
	}
	if got := prWorkflowMarkerMode("closes #12\n"); got != workflowPullRequest {
		t.Errorf("unmarked mode = %s, want %s", got, workflowPullRequest)
	}
}

func TestPRWorkflowCloseBlocksOnHumanFeedback(t *testing.T) {
	f := &prWorkflowFakeForge{
		prBody:  "closes #6\n\nward.workflow: pull-request-and-merge\n",
		prState: "open",
		comments: []issueComment{
			{
				Body:      "WARD-WORKFLOW: done ✅",
				CreatedAt: time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC),
				User: struct {
					Login string `json:"login"`
				}{Login: forgeForgejo.gitPushUser()},
			},
			{
				Body:      "please keep this open",
				CreatedAt: time.Date(2026, 7, 15, 8, 5, 0, 0, time.UTC),
				User: struct {
					Login string `json:"login"`
				}{Login: "repo-owner"},
			},
		},
	}
	srv := f.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	if _, err := prWorkflowCloseExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "superseded by #8", ""); err == nil || !strings.Contains(err.Error(), "human comment by @repo-owner") {
		t.Fatalf("prWorkflowCloseExec error = %v, want human-feedback block", err)
	}
	if f.prState != "open" {
		t.Fatalf("close gate should not patch the PR state, got %q", f.prState)
	}
}

// TestPRWorkflowRoleDefaultsToDirectorOnHost pins the acting-role resolution.
func TestPRWorkflowRoleDefaultsToDirectorOnHost(t *testing.T) {
	t.Setenv("WARD_ROLE", "")
	if got := prWorkflowRole(); got != roleDirector {
		t.Errorf("prWorkflowRole() = %q, want %q", got, roleDirector)
	}
	t.Setenv("WARD_ROLE", roleEngineer)
	if got := prWorkflowRole(); got != roleEngineer {
		t.Errorf("prWorkflowRole() = %q, want %q", got, roleEngineer)
	}
}

// prWorkflowFakeForge serves the minimal Forgejo API surface the merge executor
// walks: PR read, merged-state, base branch, combined status, merge.
type prWorkflowFakeForge struct {
	prBody                        string
	prState                       string
	prHeadSHA                     string
	prHeadRef                     string
	prBaseRef                     string
	prAdditions                   int
	prDeletions                   int
	prDiffKnown                   bool
	updatedAt                     time.Time
	comments                      []issueComment
	prStateAfterMerge             string
	combinedState                 string
	contextState                  string
	combinedStateAfterMerge       string
	contextStateAfterMerge        string
	defaultMergeStyle             string
	defaultDeleteBranchAfterMerge bool
	allowMergeCommits             bool
	allowSquashMerge              bool
	allowFastForwardOnlyMerge     bool
	allowRebase                   bool
	allowRebaseExplicit           bool
	branchCommitSHA               string
	commitTrees                   map[string]string
	mergeResponses                []int
	statusFailureAfterMergeCalls  int
	merged                        bool
	markMergedOnSuccess           *bool
	mergeCalls                    int
	mergeDo                       string
	mergeDeleteBranchAfterMerge   bool
	mergedChecks                  int
}

func (f *prWorkflowFakeForge) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/pulls/7", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			state := f.prState
			if state == "" {
				state = "open"
			}
			headSHA := f.prHeadSHA
			if headSHA == "" {
				headSHA = "headsha"
			}
			headRef := f.prHeadRef
			if headRef == "" {
				headRef = "issue-7"
			}
			baseRef := f.prBaseRef
			if baseRef == "" {
				baseRef = "main"
			}
			additions := 1
			deletions := 1
			if f.prDiffKnown {
				additions = f.prAdditions
				deletions = f.prDeletions
			}
			body := `{"number":7,"title":"t","body":` + jsonString(f.prBody) + `,"state":"` + state + `","head":{"sha":"` + headSHA + `","ref":"` + headRef + `"},"base":{"ref":"` + baseRef + `"}`
			if !f.updatedAt.IsZero() {
				body += `,"updated_at":` + jsonString(f.updatedAt.UTC().Format(time.RFC3339))
			}
			body += `,"additions":` + strconv.Itoa(additions) + `,"deletions":` + strconv.Itoa(deletions)
			body += `}`
			_, _ = w.Write([]byte(body))
		case http.MethodPatch:
			var body struct {
				State string `json:"state"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode patch body: %v", err)
			}
			f.prState = body.State
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"number":7}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"allow_merge_commits":` + jsonBool(f.allowMergeCommits) + `,"allow_squash_merge":` + jsonBool(f.allowSquashMerge) + `,"allow_fast_forward_only_merge":` + jsonBool(f.allowFastForwardOnlyMerge) + `,"allow_rebase":` + jsonBool(f.allowRebase) + `,"allow_rebase_explicit":` + jsonBool(f.allowRebaseExplicit) + `,"default_merge_style":` + jsonString(f.defaultMergeStyle) + `,"default_delete_branch_after_merge":` + jsonBool(f.defaultDeleteBranchAfterMerge) + `}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/pulls/7/merge", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			f.mergeCalls++
			status := http.StatusAccepted
			if len(f.mergeResponses) > 0 {
				status = f.mergeResponses[0]
				f.mergeResponses = f.mergeResponses[1:]
			}
			var body struct {
				Do                     string `json:"do"`
				HeadCommitID           string `json:"head_commit_id"`
				DeleteBranchAfterMerge bool   `json:"delete_branch_after_merge"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			f.mergeDo = body.Do
			f.mergeDeleteBranchAfterMerge = body.DeleteBranchAfterMerge
			if status >= 200 && status < 300 {
				if f.prStateAfterMerge != "" {
					f.prState = f.prStateAfterMerge
				} else if f.markMergedOnSuccess == nil || *f.markMergedOnSuccess {
					f.prState = "closed"
				}
				if f.markMergedOnSuccess == nil || *f.markMergedOnSuccess {
					f.merged = true
				}
			}
			w.WriteHeader(status)
			if status == http.StatusMethodNotAllowed {
				_, _ = w.Write([]byte(`{"message":"Please try again later","url":"https://forgejo.coilysiren.me/api/swagger"}`))
			}
			return
		default:
			f.mergedChecks++
			if f.merged {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/issues/7", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			body := `{"number":7,"title":"t","body":"closes #7","state":"open","html_url":"https://f/issues/7"`
			if !f.updatedAt.IsZero() {
				body += `,"updated_at":` + jsonString(f.updatedAt.UTC().Format(time.RFC3339))
			}
			body += `}`
			_, _ = w.Write([]byte(body))
		case http.MethodPatch:
			f.prState = "open"
			w.WriteHeader(http.StatusOK)
			return
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/issues/7/comments", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(f.comments)
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/issues/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/repos/coilyco-flight-deck/ward/issues/")
		if rest == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.HasSuffix(rest, "/comments") {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			_ = json.NewEncoder(w).Encode(f.comments)
			return
		}
		num, err := strconv.Atoi(rest)
		if err != nil || num <= 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			body := `{"number":` + strconv.Itoa(num) + `,"title":"t","body":"closes #` + strconv.Itoa(num) + `","state":"open","html_url":"https://f/issues/` + strconv.Itoa(num) + `"`
			if !f.updatedAt.IsZero() {
				body += `,"updated_at":` + jsonString(f.updatedAt.UTC().Format(time.RFC3339))
			}
			body += `}`
			_, _ = w.Write([]byte(body))
		case http.MethodPatch:
			f.prState = "open"
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/branches/main", func(w http.ResponseWriter, _ *http.Request) {
		commitID := f.branchCommitSHA
		if commitID == "" {
			commitID = "basesha"
		}
		_, _ = w.Write([]byte(`{"name":"main","protected":true,"enable_status_check":true,"status_check_contexts":["test"],"commit":{"id":` + jsonString(commitID) + `}}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/commits/", func(w http.ResponseWriter, r *http.Request) {
		sha := strings.TrimPrefix(r.URL.Path, "/api/v1/repos/coilyco-flight-deck/ward/commits/")
		if sha == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		tree := sha
		if f.commitTrees != nil {
			if got, ok := f.commitTrees[sha]; ok {
				tree = got
			}
		}
		_, _ = w.Write([]byte(`{"tree":{"sha":` + jsonString(tree) + `}}`))
	})
	// Per-context state rides the `status` key on live Forgejo (gitea-compat),
	// not `state` - the fake serves the live shape to pin effectiveState.
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/commits/headsha/status", func(w http.ResponseWriter, _ *http.Request) {
		combinedState := f.combinedState
		contextState := f.contextState
		if f.statusFailureAfterMergeCalls > 0 && f.mergeCalls >= f.statusFailureAfterMergeCalls {
			combinedState = f.combinedStateAfterMerge
			contextState = f.contextStateAfterMerge
		}
		_, _ = w.Write([]byte(`{"state":"` + combinedState + `","sha":"headsha","total_count":1,"statuses":[{"context":"test","status":"` + contextState + `"}]}`))
	})
	return httptest.NewServer(mux)
}

func jsonBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// TestPRWorkflowMergeExecEngineerSelfMerge drives the engineer self-merge lane
// end to end against a fake forge, head-pinned and merged-state confirmed.
func TestPRWorkflowMergeExecEngineerSelfMerge(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		prDiffKnown:               true,
		prAdditions:               1,
		prDeletions:               1,
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowMergeExec(context.Background(), cl, roleEngineer, "coilyco-flight-deck", "ward", 7, "")
	if err != nil {
		t.Fatalf("prWorkflowMergeExec: %v", err)
	}
	if fake.mergeCalls != 1 {
		t.Fatalf("merge calls = %d, want 1", fake.mergeCalls)
	}
	if !strings.Contains(out, "merged coilyco-flight-deck/ward#7") || !strings.Contains(out, "merged-state check: merged") {
		t.Fatalf("merge output = %q, want merged + merged-state confirmation", out)
	}
	if fake.mergeDo != "merge" {
		t.Fatalf("merge do = %q, want merge", fake.mergeDo)
	}
}

// TestPRWorkflowMergeExecAlreadyMergedNoAction pins the merged-state shortcut:
// once Forgejo reports the PR merged, ward must stop without touching the merge endpoint.
func TestPRWorkflowMergeExecAlreadyMergedNoAction(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:            "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:     "success",
		contextState:      "success",
		defaultMergeStyle: "merge",
		allowMergeCommits: true,
		merged:            true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowMergeExec(context.Background(), cl, roleEngineer, "coilyco-flight-deck", "ward", 7, "")
	if err != nil {
		t.Fatalf("prWorkflowMergeExec: %v", err)
	}
	if fake.mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want 0", fake.mergeCalls)
	}
	if !strings.Contains(out, "already merged, no action") {
		t.Fatalf("merge output = %q, want already-merged no-op", out)
	}
}

// TestPRWorkflowMergeExecSkipsSameTreeAfterPRMerge pins the tree-equality
// fallback: if the PR head tree is already in main, the worker must not merge again.
func TestPRWorkflowMergeExecSkipsSameTreeAfterPRMerge(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:            "closes #6\n\nward.workflow: pull-request-and-merge\n",
		prState:           "open",
		combinedState:     "success",
		contextState:      "success",
		defaultMergeStyle: "merge",
		allowMergeCommits: true,
		branchCommitSHA:   "basesha",
		commitTrees: map[string]string{
			"headsha": "tree-sha",
			"basesha": "tree-sha",
		},
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowMergeExec(context.Background(), cl, roleEngineer, "coilyco-flight-deck", "ward", 7, "")
	if err != nil {
		t.Fatalf("prWorkflowMergeExec: %v", err)
	}
	if fake.mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want 0", fake.mergeCalls)
	}
	if !strings.Contains(out, "already merged, no action") {
		t.Fatalf("merge output = %q, want already-merged no-op", out)
	}
}

// TestPRWorkflowMergeExecIgnoresEmptySummaryWhenTreesDiffer pins the stale summary case.
func TestPRWorkflowMergeExecIgnoresEmptySummaryWhenTreesDiffer(t *testing.T) {
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
	out, err := prWorkflowMergeExec(context.Background(), cl, roleEngineer, "coilyco-flight-deck", "ward", 7, "")
	if err != nil {
		t.Fatalf("prWorkflowMergeExec: %v", err)
	}
	if fake.mergeCalls != 1 {
		t.Fatalf("merge calls = %d, want 1", fake.mergeCalls)
	}
	if !strings.Contains(out, "merged coilyco-flight-deck/ward#7") {
		t.Fatalf("merge output = %q, want merge to proceed", out)
	}
}

// TestPRWorkflowCloseExecDirectorClosesWithReason pins the close mutation:
// the director can close an open PR with a reason, and the head SHA stays pinned.
func TestPRWorkflowCloseExecDirectorClosesWithReason(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:            "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:     "success",
		contextState:      "success",
		defaultMergeStyle: "merge",
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowCloseExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "superseded by #1163", "")
	if err != nil {
		t.Fatalf("prWorkflowCloseExec: %v", err)
	}
	if fake.prState != "closed" {
		t.Fatalf("pr state = %q, want closed", fake.prState)
	}
	for _, want := range []string{
		"closed coilyco-flight-deck/ward#7",
		"head before headsha",
		"head after headsha",
		"merged=false",
		"reason: superseded by #1163",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("close output %q missing %q", out, want)
		}
	}
}

// TestPRWorkflowCloseExecRequiresReasonOrSupersedes keeps the close verb from
// ambiguously closing a PR without operator intent.
func TestPRWorkflowCloseExecRequiresReasonOrSupersedes(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:            "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:     "success",
		contextState:      "success",
		defaultMergeStyle: "merge",
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowCloseExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "", "")
	if err == nil || !strings.Contains(err.Error(), "requires a reason or superseding issue/PR reference") {
		t.Fatalf("close without intent = %v, want explicit-reason refusal", err)
	}
	if fake.prState != "" {
		t.Fatalf("close without intent mutated the PR state: %q", fake.prState)
	}
}

// TestPRWorkflowCloseExecAcceptsBareSupersedesRef pins the same-repo ref
// normalization path used by close requests and broker validation.
func TestPRWorkflowCloseExecAcceptsBareSupersedesRef(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:            "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:     "success",
		contextState:      "success",
		defaultMergeStyle: "merge",
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowCloseExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "", "#1163")
	if err != nil {
		t.Fatalf("prWorkflowCloseExec bare supersedes: %v", err)
	}
	if !strings.Contains(out, "superseded by coilyco-flight-deck/ward#1163") {
		t.Fatalf("close output = %q, want canonical superseding ref", out)
	}
}

// TestPRWorkflowReopenExecRestoresAClosedPR pins the reopen mutation and its
// head-pin postcondition.
func TestPRWorkflowReopenExecRestoresAClosedPR(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:            "closes #6\n\nward.workflow: pull-request-and-merge\n",
		prState:           "closed",
		combinedState:     "success",
		contextState:      "success",
		defaultMergeStyle: "merge",
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowReopenExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7)
	if err != nil {
		t.Fatalf("prWorkflowReopenExec: %v", err)
	}
	if fake.prState != "open" {
		t.Fatalf("pr state = %q, want open", fake.prState)
	}
	for _, want := range []string{
		"reopened coilyco-flight-deck/ward#7",
		"head before headsha",
		"head after headsha",
		"merged=false",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("reopen output %q missing %q", out, want)
		}
	}
}

func TestPRWorkflowMergeExecBlocksOnHumanComment(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:            "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:     "success",
		contextState:      "success",
		defaultMergeStyle: "merge",
		allowMergeCommits: true,
		comments: []issueComment{{
			Body:      "please stop, this misses the real need",
			CreatedAt: time.Date(2026, 7, 10, 9, 30, 0, 0, time.UTC),
			User: struct {
				Login string `json:"login"`
			}{Login: "coilysiren"},
		}},
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "")
	if err == nil || !strings.Contains(err.Error(), "human comment by @coilysiren") {
		t.Fatalf("merge with human comment = %v, want human-feedback block", err)
	}
	if fake.mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want 0", fake.mergeCalls)
	}
}

func TestPRWorkflowReopenExecBlocksOnManualCloseSnapshot(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:            "closes #6\n\nward.workflow: pull-request-and-merge\n",
		prState:           "closed",
		updatedAt:         time.Date(2026, 7, 10, 9, 30, 0, 0, time.UTC),
		combinedState:     "success",
		contextState:      "success",
		defaultMergeStyle: "merge",
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowReopenExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7)
	if err == nil || !strings.Contains(err.Error(), "manual close/update snapshot") {
		t.Fatalf("reopen with manual close snapshot = %v, want blocked", err)
	}
	if fake.prState != "closed" {
		t.Fatalf("reopen blocked but PR state mutated to %q", fake.prState)
	}
}

// TestPRWorkflowRecoverReportClosedUnmerged pins the closed-unmerged diagnosis:
// the report names the head SHA, linked issue, and the next safe action.
func TestPRWorkflowRecoverReportClosedUnmerged(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:            "closes #6\n",
		prState:           "closed",
		combinedState:     "success",
		contextState:      "success",
		defaultMergeStyle: "merge",
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowRecoverReport(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7)
	if err != nil {
		t.Fatalf("prWorkflowRecoverReport: %v", err)
	}
	for _, want := range []string{
		"state=closed",
		"merged=false",
		"head=headsha",
		"linked issue=coilyco-flight-deck/ward#6",
		"next safe action: reopen the PR, then re-run status and merge",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("recover output %q missing %q", out, want)
		}
	}
}

// TestPRWorkflowRecoverReportHighlightsMergedOpenLinkedIssue pins the repair path
// for an already-merged PR whose wrong trailer left the carried issue open.
func TestPRWorkflowRecoverReportHighlightsMergedOpenLinkedIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/coilyco-flight-deck/ward/pulls/7":
			_, _ = w.Write([]byte(`{"number":7,"title":"repair branch","body":"closes #6\n","state":"closed","head":{"sha":"headsha","ref":"issue-7"},"base":{"ref":"main"}}`))
		case "/api/v1/repos/coilyco-flight-deck/ward/pulls/7/merge":
			w.WriteHeader(http.StatusNoContent)
		case "/api/v1/repos/coilyco-flight-deck/ward/issues/6":
			_, _ = w.Write([]byte(`{"number":6,"title":"carried issue","body":"body","state":"open","html_url":"https://f/issues/6"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowRecoverReport(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7)
	if err != nil {
		t.Fatalf("prWorkflowRecoverReport: %v", err)
	}
	for _, want := range []string{
		"merged=true",
		"linked issue=coilyco-flight-deck/ward#6",
		"the PR is merged, but the carried issue coilyco-flight-deck/ward#6 is still open",
		"repair the carried issue trailer or close the issue by hand",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("recover output %q missing %q", out, want)
		}
	}
}

// TestPRWorkflowMergeExecExplicitSquash pins the explicit style path.
func TestPRWorkflowMergeExecExplicitSquash(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	if _, err := prWorkflowMergeExec(context.Background(), cl, roleEngineer, "coilyco-flight-deck", "ward", 7, "squash"); err != nil {
		t.Fatalf("prWorkflowMergeExec squash: %v", err)
	}
	if fake.mergeDo != "squash" {
		t.Fatalf("merge do = %q, want squash", fake.mergeDo)
	}
}

// TestPRWorkflowMergeExecUsesRepoDefaultStyle pins the no-flag path on a
// squash-only repo that defaults to squash.
func TestPRWorkflowMergeExecUsesRepoDefaultStyle(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                        "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:                 "success",
		contextState:                  "success",
		defaultMergeStyle:             "squash",
		defaultDeleteBranchAfterMerge: true,
		allowSquashMerge:              true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	if _, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, ""); err != nil {
		t.Fatalf("prWorkflowMergeExec default style: %v", err)
	}
	if fake.mergeDo != "squash" {
		t.Fatalf("merge do = %q, want squash", fake.mergeDo)
	}
	if !fake.mergeDeleteBranchAfterMerge {
		t.Fatalf("merge delete_branch_after_merge = %t, want true", fake.mergeDeleteBranchAfterMerge)
	}
}

// TestPRWorkflowMergeExecIgnoresOperatorBundleStyle keeps native merge policy baked.
func TestPRWorkflowMergeExecIgnoresOperatorBundleStyle(t *testing.T) {
	t.Setenv(wardConfigRefEnv, "ignored")

	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	if _, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, ""); err != nil {
		t.Fatalf("prWorkflowMergeExec baked style: %v", err)
	}
	if fake.mergeDo != "merge" {
		t.Fatalf("merge do = %q, want baked merge", fake.mergeDo)
	}
}

// TestPRWorkflowMergeExecRetriesTransient405 pins the bounded settle loop.
// The first 405 should retry and land once the forge accepts it.
func TestPRWorkflowMergeExecRetriesTransient405(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
		mergeResponses:            []int{http.StatusMethodNotAllowed, http.StatusAccepted},
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "")
	if err != nil {
		t.Fatalf("prWorkflowMergeExec: %v", err)
	}
	if fake.mergeCalls != 2 {
		t.Fatalf("merge calls = %d, want 2", fake.mergeCalls)
	}
	if !strings.Contains(out, "merged coilyco-flight-deck/ward#7") {
		t.Fatalf("merge output = %q, want merged", out)
	}
}

// TestPRWorkflowMergeExecRetriesSeveralTransient405s keeps the settle window
// wide enough for Forgejo merge-queue lag before the PR is declared stuck.
func TestPRWorkflowMergeExecRetriesSeveralTransient405s(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
		mergeResponses:            []int{http.StatusMethodNotAllowed, http.StatusMethodNotAllowed, http.StatusMethodNotAllowed, http.StatusAccepted},
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "")
	if err != nil {
		t.Fatalf("prWorkflowMergeExec: %v", err)
	}
	if fake.mergeCalls != 4 {
		t.Fatalf("merge calls = %d, want 4", fake.mergeCalls)
	}
	if !strings.Contains(out, "merged coilyco-flight-deck/ward#7") {
		t.Fatalf("merge output = %q, want merged", out)
	}
}

// TestPRWorkflowMergeExecFailsClosedUnmergedPostcondition pins the hard fail:
// Forgejo cannot close the PR without the merged-state proof.
func TestPRWorkflowMergeExecFailsClosedUnmergedPostcondition(t *testing.T) {
	markMergedOnSuccess := false
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		prStateAfterMerge:         "closed",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
		mergeResponses:            []int{http.StatusMethodNotAllowed, http.StatusAccepted},
		markMergedOnSuccess:       &markMergedOnSuccess,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "")
	if err == nil {
		t.Fatal("prWorkflowMergeExec: want closed-unmerged postcondition failure, got nil")
	}
	for _, want := range []string{"state closed", "merged=false", "head headsha"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want substring %q", err, want)
		}
	}
	if fake.mergeCalls != 2 {
		t.Fatalf("merge calls = %d, want 2", fake.mergeCalls)
	}
}

// TestPRWorkflowMergeExecExplainsLostEligibilityAfter405 pins the diagnostic
// path when Forgejo keeps rejecting the merge and the live gate goes red.
func TestPRWorkflowMergeExecExplainsLostEligibilityAfter405(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                       "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:                "success",
		contextState:                 "success",
		combinedStateAfterMerge:      "failure",
		contextStateAfterMerge:       "failure",
		defaultMergeStyle:            "merge",
		allowMergeCommits:            true,
		allowSquashMerge:             true,
		allowFastForwardOnlyMerge:    true,
		allowRebase:                  true,
		allowRebaseExplicit:          true,
		mergeResponses:               []int{http.StatusMethodNotAllowed},
		statusFailureAfterMergeCalls: 1,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "")
	if err == nil {
		t.Fatal("prWorkflowMergeExec: want diagnostic error, got nil")
	}
	if !strings.Contains(err.Error(), "test=failure") {
		t.Fatalf("error = %q, want failing required context", err)
	}
	if fake.mergeCalls != 1 {
		t.Fatalf("merge calls = %d, want 1", fake.mergeCalls)
	}
}

// TestPRWorkflowMergeExecCarriesRepoDeleteBranchDefault pins the merge payload's
// delete-branch flag for both repo-default values.
func TestPRWorkflowMergeExecCarriesRepoDeleteBranchDefault(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		defaultDeleteBranchAfterMerge bool
	}{
		{name: "false", defaultDeleteBranchAfterMerge: false},
		{name: "true", defaultDeleteBranchAfterMerge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &prWorkflowFakeForge{
				prBody:                        "closes #6\n\nward.workflow: pull-request-and-merge\n",
				combinedState:                 "success",
				contextState:                  "success",
				defaultMergeStyle:             "merge",
				defaultDeleteBranchAfterMerge: tc.defaultDeleteBranchAfterMerge,
				allowMergeCommits:             true,
				allowSquashMerge:              true,
				allowFastForwardOnlyMerge:     true,
				allowRebase:                   true,
				allowRebaseExplicit:           true,
			}
			srv := fake.server(t)
			defer srv.Close()
			cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
			if _, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "squash"); err != nil {
				t.Fatalf("prWorkflowMergeExec delete-branch default: %v", err)
			}
			if fake.mergeDeleteBranchAfterMerge != tc.defaultDeleteBranchAfterMerge {
				t.Fatalf("merge delete_branch_after_merge = %t, want %t", fake.mergeDeleteBranchAfterMerge, tc.defaultDeleteBranchAfterMerge)
			}
		})
	}
}

// TestPRWorkflowMergeExecRejectsUnsupportedStyle pins the explicit style
// validation and its error wording.
func TestPRWorkflowMergeExecRejectsUnsupportedStyle(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "manual-rocket")
	if err == nil {
		t.Fatal("want unsupported style refusal, got nil")
	}
	if !strings.Contains(err.Error(), "supported styles: merge, squash, fast-forward-only, rebase, rebase-merge") {
		t.Fatalf("error = %v, want supported-style list", err)
	}
	if fake.mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want 0", fake.mergeCalls)
	}
}

// TestPRWorkflowMergeExecDeniesEngineerWithoutMarker pins the mode gate: an
// unmarked PR is the pull-request lane, engineer denied, no forge mutation.
func TestPRWorkflowMergeExecDeniesEngineerWithoutMarker(t *testing.T) {
	fake := &prWorkflowFakeForge{prBody: "closes #6\n", combinedState: "success", contextState: "success"}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowMergeExec(context.Background(), cl, roleEngineer, "coilyco-flight-deck", "ward", 7, "")
	if err == nil {
		t.Fatal("want engineer denial in the pull-request lane, got nil")
	}
	if !strings.Contains(err.Error(), "workflow pull-request does not permit") {
		t.Fatalf("denial = %v, want workflow-only wording", err)
	}
	if fake.mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want 0 (gate must precede mutation)", fake.mergeCalls)
	}
}

// TestPRWorkflowMergeExecDirectorCannotElevateUnmarkedPR proves the director
// label cannot elevate an unmarked pull-request lane.
func TestPRWorkflowMergeExecDirectorCannotElevateUnmarkedPR(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	if _, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, ""); err == nil || !strings.Contains(err.Error(), "workflow pull-request does not permit") {
		t.Fatalf("director merge in pull-request lane = %v, want workflow refusal", err)
	}
	if fake.mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want 0", fake.mergeCalls)
	}
}

// TestPRWorkflowMergeExecRefusesRedStatus pins the live status gate: a failing
// required context stops the merge before any mutation.
func TestPRWorkflowMergeExecRefusesRedStatus(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "failure",
		contextState:              "failure",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	_, err := prWorkflowMergeExec(context.Background(), cl, roleDirector, "coilyco-flight-deck", "ward", 7, "")
	if err == nil {
		t.Fatal("want red-status refusal, got nil")
	}
	if !strings.Contains(err.Error(), "test=failure") {
		t.Fatalf("refusal = %v, want the failing required context named", err)
	}
	if fake.mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want 0", fake.mergeCalls)
	}
}

// TestPRWorkflowStatusReportRendersCombinedStatus pins the native per-PR CI
// status read a director surface depends on (infrastructure#538).
func TestPRWorkflowStatusReportRendersCombinedStatus(t *testing.T) {
	fake := &prWorkflowFakeForge{prBody: "closes #6\n", combinedState: "failure", contextState: "failure"}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := prWorkflowStatusReport(context.Background(), cl, "coilyco-flight-deck", "ward", 7)
	if err != nil {
		t.Fatalf("prWorkflowStatusReport: %v", err)
	}
	for _, want := range []string{"combined status: failure", "test = failure", "required on main: test"} {
		if !strings.Contains(out, want) {
			t.Errorf("status report %q missing %q", out, want)
		}
	}
}

func TestPRWorkflowStatusJSONCarriesRunsAndLogHooks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/pulls/7", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"number":7,"title":"status branch","body":"closes #6","state":"open","draft":false,"mergeable":true,"head":{"sha":"headsha","ref":"issue-7"},"base":{"ref":"main"}}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/branches/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"main","protected":true,"enable_status_check":true,"status_check_contexts":["test"],"commit":{"id":"basesha"}}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/commits/headsha/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"state":"failure","sha":"headsha","total_count":1,"statuses":[{"context":"test","status":"failure","description":"failed","target_url":"https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/77/jobs/3/attempt/1/logs"}]}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/actions/runs", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"workflow_runs":[{"id":77,"index_in_repo":11,"title":"test","status":"failure","workflow_id":"test.yml","prettyref":"#7","commit_sha":"headsha","event":"pull_request","html_url":"https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/77"}]}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/actions/runs/77/jobs/3/attempt/1/logs", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("log line one\nlog line two\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	body, err := prWorkflowStatusJSONReport(context.Background(), cl, "coilyco-flight-deck", "ward", 7)
	if err != nil {
		t.Fatalf("prWorkflowStatusJSONReport: %v", err)
	}
	var st prCIStatus
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("unmarshal status JSON: %v", err)
	}
	if st.Status.Required != "failure" || st.Status.Combined != "failure" {
		t.Fatalf("status = %+v, want failure snapshot", st.Status)
	}
	if st.NextAction != "fetch_logs" {
		t.Fatalf("next action = %q, want fetch_logs", st.NextAction)
	}
	if st.Status.LatestRunConclusion != "failure" {
		t.Fatalf("latest run conclusion = %q, want failure", st.Status.LatestRunConclusion)
	}
	if len(st.LatestRuns) != 1 || st.LatestRuns[0].ID != 77 {
		t.Fatalf("latest runs = %+v, want one matching run", st.LatestRuns)
	}
	if len(st.LogHooks) == 0 || !st.LogHooks[0].Available || st.LogHooks[0].RunID != 77 {
		t.Fatalf("log hooks = %+v, want an available hook for run 77", st.LogHooks)
	}
	if st.LogHooks[0].DisplayRunIndex != 77 {
		t.Fatalf("log hook display run = %d, want 77", st.LogHooks[0].DisplayRunIndex)
	}
	if len(st.Contexts) == 0 || st.Contexts[0].RunID != 77 || st.Contexts[0].LogHook == nil || !st.Contexts[0].LogHook.Available {
		t.Fatalf("context = %+v, want run-linked log hook", st.Contexts)
	}
	logs, err := prWorkflowLogsDirect(context.Background(), cl, "coilyco-flight-deck", "ward", 7, "test")
	if err != nil {
		t.Fatalf("prWorkflowLogsDirect: %v", err)
	}
	if !strings.Contains(logs, "log line one") || !strings.Contains(logs, "log line two") {
		t.Fatalf("logs = %q, want raw body", logs)
	}
}

func TestPRWorkflowStatusJSONRejectsPlaceholderLogHooks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/pulls/1388", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"number":1388,"title":"status mismatch branch","body":"closes #1388","state":"open","draft":false,"mergeable":true,"head":{"sha":"headsha1388","ref":"issue-1388"},"base":{"ref":"main"}}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/branches/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"main","protected":true,"enable_status_check":true,"status_check_contexts":["test / test (pull_request)"],"commit":{"id":"basesha1388"}}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/commits/headsha1388/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"state":"failure","sha":"headsha1388","total_count":1,"statuses":[{"context":"test / test (pull_request)","status":"failure","description":"failed","target_url":"https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/2209/jobs/0/attempt/1/logs"}]}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/actions/runs", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"workflow_runs":[{"id":8470,"index_in_repo":2209,"title":"test","status":"failure","workflow_id":"test.yml","prettyref":"#1388","commit_sha":"headsha1388","event":"pull_request","html_url":"https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/8470"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	body, err := prWorkflowStatusJSONReport(context.Background(), cl, "coilyco-flight-deck", "ward", 1388)
	if err != nil {
		t.Fatalf("prWorkflowStatusJSONReport: %v", err)
	}
	var st prCIStatus
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("unmarshal status JSON: %v", err)
	}
	if len(st.LatestRuns) != 1 || st.LatestRuns[0].ID != 8470 || st.LatestRuns[0].Index != 2209 {
		t.Fatalf("latest runs = %+v, want api id 8470 with display index 2209", st.LatestRuns)
	}
	if st.Status.Required != "failure" || st.NextAction != "repair_pr" {
		t.Fatalf("status = %+v, want failure without an executable hook", st.Status)
	}
	if len(st.LogHooks) != 1 {
		t.Fatalf("log hooks = %+v, want one placeholder hook", st.LogHooks)
	}
	hook := st.LogHooks[0]
	if hook.Available {
		t.Fatalf("log hook = %+v, want unavailable", hook)
	}
	if hook.DisplayRunIndex != 2209 {
		t.Fatalf("log hook display_run_index = %d, want 2209", hook.DisplayRunIndex)
	}
	if hook.RunID != 0 {
		t.Fatalf("log hook run_id = %d, want 0 for an unavailable hook", hook.RunID)
	}
	if !strings.Contains(hook.Reason, "placeholder") {
		t.Fatalf("log hook reason = %q, want placeholder rejection", hook.Reason)
	}
	if len(st.Contexts) == 0 || st.Contexts[0].RunID != 8470 || st.Contexts[0].LogHook == nil || st.Contexts[0].LogHook.Available {
		t.Fatalf("context = %+v, want an unavailable hook tied to the real run id", st.Contexts)
	}
	logs, err := prWorkflowLogsDirect(context.Background(), cl, "coilyco-flight-deck", "ward", 1388, "test / test (pull_request)")
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("prWorkflowLogsDirect error = %v, want placeholder rejection", err)
	}
	if logs != "" {
		t.Fatalf("logs = %q, want no body for an unavailable hook", logs)
	}
}

func TestPRWorkflowStatusJSONSurfacesMissingRequiredContexts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/pulls/8", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"number":8,"title":"pending branch","body":"closes #6","state":"open","draft":false,"mergeable":true,"head":{"sha":"headsha8","ref":"issue-8"},"base":{"ref":"main"}}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/branches/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"main","protected":true,"enable_status_check":true,"status_check_contexts":["test","lint"],"commit":{"id":"basesha"}}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/commits/headsha8/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"state":"pending","sha":"headsha8","total_count":1,"statuses":[{"context":"test","status":"success","description":"passed","target_url":"https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/78"}]}`))
	})
	mux.HandleFunc("/api/v1/repos/coilyco-flight-deck/ward/actions/runs", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"workflow_runs":[{"id":78,"index_in_repo":12,"title":"test","status":"success","workflow_id":"test.yml","prettyref":"#8","commit_sha":"headsha8","event":"pull_request","html_url":"https://forgejo.coilysiren.me/coilyco-flight-deck/ward/actions/runs/78"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	body, err := prWorkflowStatusJSONReport(context.Background(), cl, "coilyco-flight-deck", "ward", 8)
	if err != nil {
		t.Fatalf("prWorkflowStatusJSONReport: %v", err)
	}
	var st prCIStatus
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("unmarshal status JSON: %v", err)
	}
	if st.Status.Required != "pending" {
		t.Fatalf("required status = %q, want pending", st.Status.Required)
	}
	if len(st.PendingContexts) != 1 || st.PendingContexts[0] != "lint" {
		t.Fatalf("pending contexts = %+v, want missing required lint", st.PendingContexts)
	}
	found := false
	for _, ctx := range st.Contexts {
		if ctx.Name == "lint" && ctx.Required && ctx.State == "pending" && !ctx.Available {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("contexts = %+v, want synthetic pending lint context", st.Contexts)
	}
	if st.NextAction != "wait" {
		t.Fatalf("next action = %q, want wait", st.NextAction)
	}
}

// TestValidateDispatchBrokerPRWorkflowShapes pins the broker request gate for
// the ward#1067 actions.
func TestValidateDispatchBrokerPRWorkflowShapes(t *testing.T) {
	valid := []dispatchBrokerRequest{
		{Action: dispatchActionPRStatus, Role: roleDirector, Target: "coilyco-flight-deck/ward#7"},
		{Action: dispatchActionPRLogs, Role: roleDirector, Target: "coilyco-flight-deck/ward#7"},
		{Action: dispatchActionPRMerge, Role: roleDirector, Target: "coilyco-flight-deck/ward#7"},
		{Action: dispatchActionPRClose, Role: roleDirector, Target: "coilyco-flight-deck/ward#7", Reason: "superseded by #1163"},
		{Action: dispatchActionPRReopen, Role: roleDirector, Target: "coilyco-flight-deck/ward#7"},
		{Action: dispatchActionPRRecover, Role: roleDirector, Target: "coilyco-flight-deck/ward#7"},
		{Action: dispatchActionCIRuns, Role: roleDirector, Target: "coilyco-flight-deck/ward", Limit: 5},
		{Action: dispatchActionCIRerun, Role: roleDirector, Target: "coilyco-flight-deck/ward", RunID: 42},
		{Action: dispatchActionPRMerge, Role: "external-role", Target: "coilyco-flight-deck/ward#7"},
	}
	for _, req := range valid {
		if err := validateDispatchBrokerPRWorkflow(req); err != nil {
			t.Errorf("validate(%s) = %v, want ok", req.Action, err)
		}
	}
	invalid := []struct {
		name string
		req  dispatchBrokerRequest
	}{
		{"missing role", dispatchBrokerRequest{Action: dispatchActionPRMerge, Target: "coilyco-flight-deck/ward#7"}},
		{"launch argv", dispatchBrokerRequest{Action: dispatchActionPRMerge, Role: roleDirector, Target: "coilyco-flight-deck/ward#7", Argv: []string{"x"}}},
		{"no target", dispatchBrokerRequest{Action: dispatchActionPRStatus, Role: roleDirector}},
		{"non-ref target", dispatchBrokerRequest{Action: dispatchActionPRMerge, Role: roleDirector, Target: "coilyco-flight-deck/ward"}},
		{"out-of-scope owner", dispatchBrokerRequest{Action: dispatchActionPRMerge, Role: roleDirector, Target: "evil/ward#7"}},
		{"close without reason", dispatchBrokerRequest{Action: dispatchActionPRClose, Role: roleDirector, Target: "coilyco-flight-deck/ward#7"}},
		{"close invalid supersedes", dispatchBrokerRequest{Action: dispatchActionPRClose, Role: roleDirector, Target: "coilyco-flight-deck/ward#7", Reason: "superseded", Supersedes: "not a ref"}},
		{"runs with ref target", dispatchBrokerRequest{Action: dispatchActionCIRuns, Role: roleDirector, Target: "coilyco-flight-deck/ward#7"}},
		{"rerun without run id", dispatchBrokerRequest{Action: dispatchActionCIRerun, Role: roleDirector, Target: "coilyco-flight-deck/ward"}},
		{"rerun out-of-scope owner", dispatchBrokerRequest{Action: dispatchActionCIRerun, Role: roleDirector, Target: "evil/ward", RunID: 1}},
	}
	for _, tc := range invalid {
		if err := validateDispatchBrokerPRWorkflow(tc.req); err == nil {
			t.Errorf("validate(%s) = nil, want refusal", tc.name)
		}
	}
}

// TestExecDispatchBrokerPRWorkflowRerunIsRoleIndependent pins the host-side
// gate without coupling the test to Forgejo's rerun API.
func TestExecDispatchBrokerPRWorkflowRerunIsRoleIndependent(t *testing.T) {
	for _, role := range []string{roleDirector, roleEngineer, roleQA, "external-role"} {
		if err := prWorkflowPermitted(role, "", prOpRerun); err != nil {
			t.Fatalf("%s rerun = %v, want fixed operation grant", role, err)
		}
	}
}

// TestExecDispatchBrokerPRWorkflowMergeRoundTrip drives the brokered merge on
// the fake forge - the read-only surface path with the specgen bundle absent.
func TestExecDispatchBrokerPRWorkflowMergeRoundTrip(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:                    "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:             "success",
		contextState:              "success",
		defaultMergeStyle:         "merge",
		allowMergeCommits:         true,
		allowSquashMerge:          true,
		allowFastForwardOnlyMerge: true,
		allowRebase:               true,
		allowRebaseExplicit:       true,
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := execDispatchBrokerPRWorkflowWith(context.Background(), cl, dispatchBrokerRequest{
		Action: dispatchActionPRMerge, Role: roleDirector, Target: "coilyco-flight-deck/ward#7", MergeStyle: "squash",
	})
	if err != nil {
		t.Fatalf("brokered merge: %v", err)
	}
	if fake.mergeCalls != 1 || !strings.Contains(out, "merged coilyco-flight-deck/ward#7") {
		t.Fatalf("brokered merge output = %q (mergeCalls %d), want one merge", out, fake.mergeCalls)
	}
	if fake.mergeDo != "squash" {
		t.Fatalf("merge do = %q, want squash", fake.mergeDo)
	}
}

// TestExecDispatchBrokerPRWorkflowCloseRoundTrip drives the brokered close on the
// fake forge and verifies the close reason survives the request boundary.
func TestExecDispatchBrokerPRWorkflowCloseRoundTrip(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:            "closes #6\n\nward.workflow: pull-request-and-merge\n",
		combinedState:     "success",
		contextState:      "success",
		defaultMergeStyle: "merge",
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := execDispatchBrokerPRWorkflowWith(context.Background(), cl, dispatchBrokerRequest{
		Action: dispatchActionPRClose, Role: roleDirector, Target: "coilyco-flight-deck/ward#7", Reason: "superseded by #1163",
	})
	if err != nil {
		t.Fatalf("brokered close: %v", err)
	}
	if fake.prState != "closed" || !strings.Contains(out, "reason: superseded by #1163") {
		t.Fatalf("brokered close output = %q, prState=%q, want closed with reason", out, fake.prState)
	}
}

// TestExecDispatchBrokerPRWorkflowRecoverRoundTrip drives the brokered recover
// diagnosis on the fake forge and verifies the closed-unmerged report shape.
func TestExecDispatchBrokerPRWorkflowRecoverRoundTrip(t *testing.T) {
	fake := &prWorkflowFakeForge{
		prBody:            "closes #6\n",
		prState:           "closed",
		combinedState:     "success",
		contextState:      "success",
		defaultMergeStyle: "merge",
	}
	srv := fake.server(t)
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	out, err := execDispatchBrokerPRWorkflowWith(context.Background(), cl, dispatchBrokerRequest{
		Action: dispatchActionPRRecover, Role: roleDirector, Target: "coilyco-flight-deck/ward#7",
	})
	if err != nil {
		t.Fatalf("brokered recover: %v", err)
	}
	if !strings.Contains(out, "next safe action: reopen the PR, then re-run status and merge") {
		t.Fatalf("brokered recover output = %q, want reopen guidance", out)
	}
}

// TestForgeRerunGapSurfacesLoudly pins the agentic-os#434 degradation: a forge
// without the rerun API yields the distinct unsupported error, never silence.
func TestForgeRerunGapSurfacesLoudly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	cl := &forgejoClient{baseURL: srv.URL, token: "secret"}
	err := cl.RerunActionRun(context.Background(), "coilyco-flight-deck", "ward", 42)
	if !errors.Is(err, errForgeRerunUnsupported) {
		t.Fatalf("rerun on gap forge = %v, want errForgeRerunUnsupported", err)
	}
}
